package library

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The proxy against a fake TMDB CDN: what it fetches, what it answers, and
// that a name it does not accept never leaves the server.
func TestPersonPhoto(t *testing.T) {
	var portrait bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 185, 278))
	for y := 0; y < 278; y++ {
		for x := 0; x < 185; x++ {
			img.Set(x, y, color.RGBA{R: uint8(y), A: 255})
		}
	}
	if err := jpeg.Encode(&portrait, img, nil); err != nil {
		t.Fatal(err)
	}
	var asked []string
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		if r.URL.Path != "/w185/abcdefgh123.jpg" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(portrait.Bytes())
	}))
	defer cdn.Close()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &Handler{cl: cdn.Client(), tmdbImageBaseURL: cdn.URL}
	r.GET("/lib/person/photo/:name/:file", h.personPhoto)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	w := get("/lib/person/photo/abcdefgh123.jpg/144.jpg")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("photo: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	got, err := jpeg.Decode(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b := got.Bounds(); b.Dx() != 144 || b.Dy() != 216 {
		t.Errorf("photo is %v, want the 144×216 portrait", b.Size())
	}

	if w := get("/lib/person/photo/missing12345.jpg/144.jpg"); w.Code != http.StatusNotFound {
		t.Errorf("a photo TMDB does not have: %d, want 404", w.Code)
	}

	asked = nil
	for _, path := range []string{
		"/lib/person/photo/abcdefgh123.gif/144.jpg",
		"/lib/person/photo/short.jpg/144.jpg",
		"/lib/person/photo/..%2Fsecret1234.jpg/144.jpg",
		"/lib/person/photo/abcdefgh123.jpg/9000.jpg",
		"/lib/person/photo/abcdefgh123.jpg/1600.jpg",
		"/lib/person/photo/abcdefgh123.jpg/32.jpg",
		"/lib/person/photo/abcdefgh123.jpg/96.jpg",
		"/lib/person/photo/abcdefgh123.png/144.jpg",
		"/lib/person/photo/abcdefgh123.jpg/144.png",
	} {
		// 400 from the handler, or 404 from the router for a path that
		// does not even match the route: either way nothing is served.
		if w := get(path); w.Code < 400 {
			t.Errorf("%s: %d, want a refusal", path, w.Code)
		}
	}
	if len(asked) != 0 {
		t.Errorf("refused requests still reached the CDN: %v", asked)
	}
}
