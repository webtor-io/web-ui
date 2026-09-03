package job

import (
	"context"
	"testing"

	log "github.com/sirupsen/logrus"
)

func itemsOf(j *Job) []LogItem {
	j.lmux.Lock()
	defer j.lmux.Unlock()
	return append([]LogItem(nil), j.l...)
}

// The warm-up watchdog redraws the status line once a second. A redraw that
// changes nothing must not become a new log item (it would be published,
// logged and pushed to every observer per tick); a changed line, or the same
// line after another step, must.
func TestStatusUpdate_DropsIdenticalRedraw(t *testing.T) {
	j := New(context.Background(), "id", "q", nil, &NilStorage{}, true, nil)
	j.InProgress("warming up")
	j.StatusUpdate("waiting for peers · 60s")
	j.StatusUpdate("waiting for peers · 60s")
	j.StatusUpdate("waiting for peers · 60s")
	if got := itemsOf(j); len(got) != 2 {
		t.Fatalf("identical redraws must collapse: got %d items, want 2 (inprogress + one status)", len(got))
	}
	j.StatusUpdate("waiting for peers · 59s")
	if got := itemsOf(j); len(got) != 3 {
		t.Fatalf("changed line must be logged: got %d items, want 3", len(got))
	}
	j.InProgress("downloading")
	j.StatusUpdate("waiting for peers · 59s")
	got := itemsOf(j)
	if len(got) != 5 {
		t.Fatalf("same text under a new step must be logged again: got %d items, want 5", len(got))
	}
	if got[4].Tag != "downloading" {
		t.Fatalf("status must carry the current step tag, got %q", got[4].Tag)
	}
}

func TestStatusUpdate_LogsAtDebug(t *testing.T) {
	if levelMap[StatusUpdate] != log.DebugLevel {
		t.Fatalf("status redraws must stay out of prod (Info) logs, got %v", levelMap[StatusUpdate])
	}
	for _, lvl := range []LogItemLevel{Info, InProgress, Done, Error, Warn} {
		if levelMap[lvl] == log.DebugLevel {
			t.Fatalf("%s must remain visible in prod logs", lvl)
		}
	}
}
