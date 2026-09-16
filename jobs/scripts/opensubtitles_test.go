package scripts

import (
	"context"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/services/api"
)

func track(id string) api.OpenSubtitleTrack {
	return api.OpenSubtitleTrack{ID: id}
}

// TestFetchOpenSubtitlesRetriesOnceWhenNotReady: video-info answers a 200
// with a Retry-After while a search leg is still waiting on the seeder.
// Asking a second time is worth one short wait; reading the empty body as
// "this file has no subtitles" is not, because the render is cached for ten
// minutes for every viewer of the file.
func TestFetchOpenSubtitlesRetriesOnceWhenNotReady(t *testing.T) {
	calls := 0
	fetch := func(ctx context.Context, u string) ([]api.OpenSubtitleTrack, error) {
		calls++
		if calls == 1 {
			return nil, &api.SubtitlesNotReadyError{RetryAfter: 10 * time.Millisecond}
		}
		return []api.OpenSubtitleTrack{track("7")}, nil
	}
	subs, notReady, err := fetchOpenSubtitles(context.Background(), fetch, "u")
	if err != nil || notReady {
		t.Fatalf("err=%v notReady=%v", err, notReady)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want exactly one retry", calls)
	}
	if len(subs) != 1 || subs[0].ID != "7" {
		t.Fatalf("subs=%+v", subs)
	}
}

// TestFetchOpenSubtitlesGivesUpAfterTheRetry: still not ready the second
// time. The page renders without the tracks -- there is no way to keep this
// one job result out of the cache -- so the flag is what carries the
// difference into the log and into subtitle-resolved.
func TestFetchOpenSubtitlesGivesUpAfterTheRetry(t *testing.T) {
	calls := 0
	fetch := func(ctx context.Context, u string) ([]api.OpenSubtitleTrack, error) {
		calls++
		return nil, &api.SubtitlesNotReadyError{RetryAfter: time.Millisecond}
	}
	subs, notReady, err := fetchOpenSubtitles(context.Background(), fetch, "u")
	if calls != 2 {
		t.Fatalf("calls=%d, want two attempts and no more", calls)
	}
	if !notReady {
		t.Error("the second refusal must be reported as not-ready")
	}
	if !errors.Is(err, api.ErrSubtitlesNotReady) {
		t.Errorf("err=%v, want the not-ready sentinel", err)
	}
	if subs != nil {
		t.Errorf("subs=%+v", subs)
	}
}

// TestOpenSubtitlesRetryWait: the hint is honoured only up to the cap, and
// a missing or unusable one means the cap rather than no wait at all -- the
// service did say it needed time. A pure function so the rule is pinned
// without any test sleeping through it.
func TestOpenSubtitlesRetryWait(t *testing.T) {
	for _, c := range []struct {
		hint, want time.Duration
	}{
		{time.Second, time.Second},
		{openSubtitlesRetryCap, openSubtitlesRetryCap},
		{time.Hour, openSubtitlesRetryCap},
		{0, openSubtitlesRetryCap},
		{-time.Second, openSubtitlesRetryCap},
	} {
		if got := openSubtitlesRetryWait(c.hint); got != c.want {
			t.Errorf("wait(%v)=%v want %v", c.hint, got, c.want)
		}
	}
}

// TestFetchOpenSubtitlesDoesNotOutliveTheBudget: the retry waits inside the
// caller's context, never past it. The whole step runs under a 30 s
// deadline with a viewer on a spinner, and a wait that outlived it would
// only fail slower.
func TestFetchOpenSubtitlesDoesNotOutliveTheBudget(t *testing.T) {
	calls := 0
	fetch := func(ctx context.Context, u string) ([]api.OpenSubtitleTrack, error) {
		calls++
		return nil, &api.SubtitlesNotReadyError{RetryAfter: time.Hour}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, notReady, _ := fetchOpenSubtitles(ctx, fetch, "u")
	if elapsed := time.Since(started); elapsed >= openSubtitlesRetryCap {
		t.Errorf("waited %v: the cap must not outrank the deadline", elapsed)
	}
	if calls != 1 {
		t.Errorf("calls=%d: no second attempt on a spent budget", calls)
	}
	if !notReady {
		t.Error("giving up on the budget is still not-ready")
	}
}

// TestFetchOpenSubtitlesPassesEverythingElseThrough: only "not ready" is
// retried. A 502, a refusal, a parse error and a plain empty list all reach
// the caller on the first attempt, exactly as before.
func TestFetchOpenSubtitlesPassesEverythingElseThrough(t *testing.T) {
	for _, c := range []struct {
		name string
		subs []api.OpenSubtitleTrack
		err  error
	}{
		{name: "a status error", err: &api.StatusError{Status: 502}},
		{name: "any other error", err: errors.New("boom")},
		{name: "an honest empty list"},
		{name: "tracks", subs: []api.OpenSubtitleTrack{track("1")}},
	} {
		t.Run(c.name, func(t *testing.T) {
			calls := 0
			fetch := func(ctx context.Context, u string) ([]api.OpenSubtitleTrack, error) {
				calls++
				return c.subs, c.err
			}
			subs, notReady, err := fetchOpenSubtitles(context.Background(), fetch, "u")
			if calls != 1 {
				t.Fatalf("calls=%d, want one", calls)
			}
			if notReady {
				t.Error("nothing here is a not-ready answer")
			}
			if err != c.err {
				t.Errorf("err=%v want %v", err, c.err)
			}
			if len(subs) != len(c.subs) {
				t.Errorf("subs=%+v want %+v", subs, c.subs)
			}
		})
	}
}
