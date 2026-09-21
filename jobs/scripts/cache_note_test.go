package scripts

import (
	"context"
	"fmt"
	"testing"
	"time"

	ra "github.com/webtor-io/rest-api/services"

	"github.com/webtor-io/web-ui/models"
)

type noteRecorder struct{ ch chan string }

func (r *noteRecorder) MarkAsCached(_ context.Context, b models.StreamingBackendType, id string, idx int) error {
	r.ch <- fmt.Sprintf("mark %s %s %d", b, id, idx)
	return nil
}

func (r *noteRecorder) Unmark(_ context.Context, b models.StreamingBackendType, id string, idx *int) error {
	if idx == nil {
		r.ch <- fmt.Sprintf("unmark %s %s ALL", b, id)
		return nil
	}
	r.ch <- fmt.Sprintf("unmark %s %s %d", b, id, *idx)
	return nil
}

func TestNoteCache(t *testing.T) {
	file := func(i int) ra.ListItem { return ra.ListItem{Type: ra.ListTypeFile, Index: i} }
	for _, tc := range []struct {
		name   string
		src    ra.ListItem
		cached bool
		want   string // "" = the index must not hear of it
	}{
		{"cached file", file(3), true, "mark webtor h 3"},
		{"cached first file", file(0), true, "mark webtor h 0"},
		// One file, never the whole torrent: a directory listing has Index 0
		// too, and an "unmark all" from it would wipe the torrent's entries.
		{"uncached file", file(3), false, "unmark webtor h 3"},
		{"directory download, cached", ra.ListItem{Type: ra.ListTypeDirectory}, true, ""},
		{"directory download, uncached", ra.ListItem{Type: ra.ListTypeDirectory}, false, ""},
	} {
		r := &noteRecorder{ch: make(chan string, 1)}
		noteCache(r, "h", tc.src, tc.cached)
		got := ""
		select {
		case got = <-r.ch:
		case <-time.After(200 * time.Millisecond):
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	// No index configured: nothing to call, nothing to panic on.
	noteCache(nil, "h", file(1), true)
}
