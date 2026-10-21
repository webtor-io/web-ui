package poster_resolver

import (
	"context"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// panicMagic starts an image whose decoder panics, standing in for
// imaging.Decode on a crafted TIFF (CVE-2023-36308, no upstream fix). A magic
// of its own, so no real format is shadowed.
const panicMagic = "\x00POSTER-DECODE-PANIC\x00"

func init() {
	image.RegisterFormat("decode-panic", panicMagic,
		func(io.Reader) (image.Image, error) { panic("crafted image") },
		func(io.Reader) (image.Config, error) { panic("crafted image") })
}

// TestPosterDecodePanicFailsTheKeyNotWedgesIt: a poster whose bytes make the
// decoder panic answers an error, and the next request for the same poster
// runs again instead of waiting forever on the first. Service.Get needs
// Postgres to reach the fetch (miss), so the test drives the service's own
// map, keyed as Get keys it, with the fetch and render miss runs.
func TestPosterDecodePanicFailsTheKeyNotWedgesIt(t *testing.T) {
	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_, _ = io.WriteString(w, panicMagic)
	}))
	defer srv.Close()

	s := New(nil, nil, srv.Client(), nil, "")
	src := posterSource(SourceIMDbMovie, "tt0000001", srv.URL+"/poster.jpg", srv.Client())
	mode, err := ParseFileMode("500.jpg")
	if err != nil {
		t.Fatal(err)
	}
	key := "resource/" + mode.CacheTag()
	get := func() (err error) {
		// What the router's recovery does with a panic that gets out.
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic escaped: %v", r)
			}
		}()
		_, err = s.lm.Get(key, func() (*Result, error) {
			img, err := src.Fetch(context.Background())
			if err != nil {
				return nil, err
			}
			buf, err := render(img, mode)
			if err != nil {
				return nil, err
			}
			return newResult(buf.Bytes()), nil
		})
		return err
	}

	for i := 1; i <= 2; i++ {
		done := make(chan error, 1)
		go func() { done <- get() }()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "crafted image") {
				t.Fatalf("request %d: err = %v, want the decoder's panic as an error", i, err)
			}
			if strings.HasPrefix(err.Error(), "panic escaped") {
				t.Errorf("request %d: %v", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("request %d for the same poster still waiting after 5s", i)
		}
	}
	if n := fetches.Load(); n != 2 {
		t.Errorf("poster fetched %d times, want 2: a failed render is not kept", n)
	}
}
