package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// A seeder since 2026-10 puts have: and span: before each data:; one
// before them sends data: alone, and that frame reads -1 for both -- also
// right after a frame that had them (a pod rotated mid-stream to an older
// seeder must not keep the last frame's numbers).
func TestWarmup_HaveAndSpan(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "have: 4194304\nspan: 16777216\ndata: 0\n\n")
		_, _ = fmt.Fprint(w, "data: 1048576\n\n")
		_, _ = fmt.Fprint(w, ": keep-alive\nhave: 16777216\nspan: 16777216\ndata: 10485760\n\n")
	}))
	defer srv.Close()
	a := &Api{cl: srv.Client()}
	ch, err := a.Warmup(context.Background(), srv.URL+"/abc/f.mkv?stats=true", 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	var got []WarmupEvent
	deadline := time.After(2 * time.Second)
	for done := false; !done; {
		select {
		case ev, ok := <-ch:
			if !ok {
				done = true
				break
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("stream did not close (got %v)", got)
		}
	}
	want := []WarmupEvent{
		{Verified: 0, Have: 4 << 20, Span: 16 << 20},
		{Verified: 1 << 20, Have: -1, Span: -1},
		{Verified: 10 << 20, Have: 16 << 20, Span: 16 << 20},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}
