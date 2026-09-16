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

// recordingStorage counts Drop so the retirement decision is observable.
type recordingStorage struct {
	NilStorage
	drops int
}

func (s *recordingStorage) Drop(_ context.Context, _ string, _ string) error {
	s.drops++
	return nil
}

// TestRetireDropsAResultTheScriptRefusedToCache: the job queue is also a
// cache — a finished run's log is replayed to every later request with the
// same id, which for the stream job is ten minutes of viewers. A run that
// reached a correct page from an incomplete answer (OpenSubtitles never
// answered, so the page has no such tracks) must not be that cache's
// content: DoNotCache retires it the same way a failure is retired, grace
// period included.
func TestRetireDropsAResultTheScriptRefusedToCache(t *testing.T) {
	for _, c := range []struct {
		name    string
		err     error
		noCache bool
		want    int
	}{
		{name: "an ordinary success is kept", want: 0},
		{name: "a failure is dropped, as before", err: context.Canceled, want: 1},
		{name: "and so is a success the script refused", noCache: true, want: 1},
		{name: "both at once drops once", err: context.Canceled, noCache: true, want: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			storage := &recordingStorage{}
			jobs := newJobs("q", storage)
			j := New(context.Background(), "id", "q", nil, storage, true, nil)
			if c.noCache {
				if got := j.DoNotCache(); got != j {
					t.Error("DoNotCache must return the job so it can be called in an expression")
				}
			}
			jobs.jobs["id"] = j

			jobs.retire("id", j, c.err)
			if storage.drops != c.want {
				t.Errorf("Drop called %d times, want %d", storage.drops, c.want)
			}
			if _, ok := jobs.jobs["id"]; ok {
				t.Error("a retired job must leave the map either way")
			}
		})
	}
}

// A restart replaces the entry under the same id. The old run retiring
// afterwards must not drop the new run's storage or take it out of the map —
// the check predates this change and is easy to lose when the body moves.
func TestRetireLeavesAReplacementAlone(t *testing.T) {
	storage := &recordingStorage{}
	jobs := newJobs("q", storage)
	old := New(context.Background(), "id", "q", nil, storage, true, nil)
	replacement := New(context.Background(), "id", "q", nil, storage, true, nil)
	jobs.jobs["id"] = replacement

	jobs.retire("id", old.DoNotCache(), context.Canceled)
	if storage.drops != 0 {
		t.Errorf("Drop called %d times for a job that is no longer current", storage.drops)
	}
	if jobs.jobs["id"] != replacement {
		t.Error("the replacement must stay in the map")
	}
}
