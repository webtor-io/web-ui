package poster_resolver

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pkg/errors"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 6))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A provider's placeholder URL that answers 404 with an HTML page used to be
// decoded as an image and surface as a 500 ("image: unknown format").
func TestPosterSourceGoneIsNotFound(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusGone} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(code)
			_, _ = w.Write([]byte("<html><body>404 Not Found</body></html>"))
		}))
		_, err := posterSource(SourceKind("movie"), "tt1", srv.URL+"/no-poster.png", srv.Client()).Fetch(context.Background())
		srv.Close()
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("status %d: want ErrNotFound (the handler's 404), got %v", code, err)
		}
	}
}

// Anything else that is not a poster stays an error the logs can see -- a
// provider outage must not read as "this film has no artwork".
func TestPosterSourceOtherFailuresStayErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := posterSource(SourceKind("movie"), "tt1", srv.URL, srv.Client()).Fetch(context.Background())
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("502 must be an error and not ErrNotFound, got %v", err)
	}
}

func TestPosterSourceDecodesAPoster(t *testing.T) {
	body := pngBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	img, err := posterSource(SourceKind("movie"), "tt1", srv.URL, srv.Client()).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 4 || b.Dy() != 6 {
		t.Fatalf("unexpected bounds %v", b)
	}
}
