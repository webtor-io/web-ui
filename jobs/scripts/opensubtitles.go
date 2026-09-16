package scripts

import (
	"context"
	"time"

	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/services/api"
)

// openSubtitlesRetryCap bounds the one wait this step will do. The whole
// OpenSubtitles step runs under a 30 s context with a viewer watching a
// spinner, and the rest of the render is queued behind it: five seconds is
// what a seeder that is nearly ready needs, and a service asking for more
// than that is not going to be ready inside this page load either.
const openSubtitlesRetryCap = 5 * time.Second

// openSubtitlesFetch is api.Api.GetOpenSubtitles, as a function so the
// retry rule can be tested without an Api.
type openSubtitlesFetch func(ctx context.Context, u string) ([]api.OpenSubtitleTrack, error)

// fetchOpenSubtitles asks once, and asks a second time when the answer was
// "not ready yet" (a 200 carrying a Retry-After: video-info still waiting
// on a search leg, usually a seeder that has not warmed up).
//
// Exactly one retry, after the Retry-After capped at
// openSubtitlesRetryCap -- and the cap itself when the header is absent or
// unreadable, which is why this is not a min() -- and never past the
// caller's deadline. The alternative -- reading the empty
// body as "this file has no subtitles" -- is worse than the 404 it replaced:
// the stream job caches its rendered result for ten minutes, so one early
// answer would take OpenSubtitles away from every viewer of that file for
// the rest of the bucket, silently and with the job step marked done.
//
// notReady says the second attempt was not ready either. The page then
// renders without OpenSubtitles tracks, and the caller does two things with
// that: job.Job.DoNotCache(), so the result is not replayed to the rest of
// the ten-minute bucket, and a log line plus notReady on subtitle-resolved,
// which keeps "no subtitles were found" and "we did not get to look" apart
// in the measurement.
func fetchOpenSubtitles(ctx context.Context, fetch openSubtitlesFetch, u string) (subs []api.OpenSubtitleTrack, notReady bool, err error) {
	subs, err = fetch(ctx, u)
	var nr *api.SubtitlesNotReadyError
	if !errors.As(err, &nr) {
		return subs, false, err
	}
	t := time.NewTimer(openSubtitlesRetryWait(nr.RetryAfter))
	defer t.Stop()
	select {
	case <-ctx.Done():
		// The budget is gone; asking again would only fail slower.
		return nil, true, err
	case <-t.C:
	}
	subs, err = fetch(ctx, u)
	if errors.As(err, &nr) {
		return nil, true, err
	}
	return subs, false, err
}

// openSubtitlesRetryWait is min(hint, cap), with one wrinkle: a missing or
// unusable hint (no Retry-After, an HTTP-date, garbage) means the cap and
// not zero. The service did say it needed time; only the amount was
// unreadable, and retrying instantly would ask the same unfinished search
// the same question.
func openSubtitlesRetryWait(hint time.Duration) time.Duration {
	if hint <= 0 || hint > openSubtitlesRetryCap {
		return openSubtitlesRetryCap
	}
	return hint
}
