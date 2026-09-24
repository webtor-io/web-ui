package scripts

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"
)

type hlsSegment struct {
	URL      string
	Duration float64
}

func parseMasterVideoVariantURL(body string) (string, error) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if next != "" && !strings.HasPrefix(next, "#") {
					return next, nil
				}
			}
		}
	}
	return "", errors.New("no video variant found in master playlist")
}

func parseMediaPlaylist(body string) (segments []hlsSegment, endList bool, err error) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "#EXT-X-ENDLIST" {
			endList = true
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			if idx := strings.IndexByte(durStr, ','); idx >= 0 {
				durStr = durStr[:idx]
			}
			dur, perr := strconv.ParseFloat(durStr, 64)
			if perr != nil {
				err = errors.Wrapf(perr, "failed to parse EXTINF duration %q", durStr)
				return
			}
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if next != "" && !strings.HasPrefix(next, "#") {
					segments = append(segments, hlsSegment{URL: next, Duration: dur})
					break
				}
			}
		}
	}
	return
}

func resolveURL(base, target string) (string, error) {
	t, err := url.Parse(target)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse target URL")
	}
	if t.IsAbs() {
		return target, nil
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse base URL")
	}
	return b.ResolveReference(t).String(), nil
}

func fetchBody(ctx context.Context, a *api.Api, u string) (string, int, error) {
	rc, status, err := a.Download(ctx, u)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", 0, errors.Wrap(err, "failed to read response body")
	}
	return string(data), status, nil
}

// transcoderRestartLimitBody is content-transcoder's answer to a session
// playlist request once FFmpeg has died on the source six times in a row
// without producing a segment (the first run plus maxConsecutiveRestarts=5):
// a 503 with this text. Nothing on our side will change that — the source
// itself cannot be converted (a broken subtitle mapping, some AVI and m4b
// files) — and the session stays capped until a seek.
const transcoderRestartLimitBody = "transcoder restart limit reached"

// pollSessionPlaylist fetches the session's video playlist once, for the
// buffering loop.
//
// The restart cap is the one answer that ends the loop. Until 2026-09 the
// status was not looked at: the cap's body parsed as a playlist with no
// segments, the loop asked ~80 more times until the buffer deadline, and the
// viewer got the no-peers modal three minutes later — 338 of the 352 capped
// webtor.io sessions in the 7 days to 2026-09-24 (~48 a day) ended on that
// wrong modal. It is matched by the text and the 503 together: a bare 503 is
// also what torrent-http-proxy's stubTransport answers, with an empty body,
// while it has no reachable transcoder for the file — that one is transient.
//
// Every other answer is read as a playlist, which for a non-200 means no
// segments yet and another poll, on purpose:
//   - 504 "playlist timeout": FFmpeg died before writing one. The next poll is
//     what restarts it, so the restart budget only works if we keep asking;
//   - 503 with an empty body: the thp stub above;
//   - 404 "session not found": this poll reached a pod that does not hold the
//     session. thp picks the pod from its own view of the endpoints, with a
//     15 s location cache and a 30 s ignore list after a failed probe, per
//     replica, so a probe blip misroutes polls for tens of seconds and then
//     heals: a 404 is not proof the session is gone. When it is gone (its pod
//     was replaced) the poll still runs to the deadline, as before.
func pollSessionPlaylist(ctx context.Context, a *api.Api, u string) (segments []hlsSegment, endList bool, err error) {
	body, status, err := fetchBody(ctx, a, u)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to fetch session video playlist")
	}
	if status == http.StatusServiceUnavailable && strings.Contains(body, transcoderRestartLimitBody) {
		// Classified as error.transcode_failed (services/web/user_error.go).
		return nil, false, errors.Errorf("%v status=%d", transcoderRestartLimitBody, status)
	}
	segments, endList, err = parseMediaPlaylist(body)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to parse session video playlist")
	}
	return segments, endList, nil
}

type SessionBufferResult struct {
	Session *api.TranscoderSession
	BaseURL string
	HLSURL  string
	SeekURL string
}

func (s *ActionScript) bufferSessionHLS(ctx context.Context, j *job.Job, streamURL string, bufferDuration time.Duration) (*SessionBufferResult, error) {
	bufferCtx, cancel := context.WithTimeout(ctx, time.Duration(s.warmup.TimeoutMin)*time.Minute)
	defer cancel()

	baseURL, err := sessionBaseURL(streamURL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to derive session base URL")
	}

	j.InProgress(s.t("job.creatingTranscoder"))
	session, err := s.api.CreateTranscoderSession(bufferCtx, baseURL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create transcoder session")
	}
	j.Done()

	hlsURL, err := sessionHLSURL(baseURL, session.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to construct session HLS URL")
	}

	j.InProgress(s.t("job.bufferingContent"))

	// The status is not needed here: a non-200 carries no variant and fails
	// just below, and the restart cap never answers the master playlist
	// (content-transcoder serves it from disk, without touching FFmpeg).
	masterBody, _, err := fetchBody(bufferCtx, s.api, hlsURL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch session master playlist")
	}

	variantRel, err := parseMasterVideoVariantURL(masterBody)
	if err != nil {
		return nil, err
	}

	variantURL, err := resolveURL(hlsURL, variantRel)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve variant URL")
	}

	target := bufferDuration.Seconds()

	for {
		select {
		case <-bufferCtx.Done():
			return nil, errors.Wrap(bufferCtx.Err(), "session buffer timeout exceeded")
		default:
		}

		segments, endList, err := pollSessionPlaylist(bufferCtx, s.api, variantURL)
		if err != nil {
			return nil, err
		}

		if endList {
			log.Info("session HLS stream complete, no buffering needed")
			break
		}

		var buffered float64
		for _, seg := range segments {
			buffered += seg.Duration
		}

		j.StatusUpdate(fmt.Sprintf("%.0f%%", buffered/target*100))

		if buffered >= target {
			break
		}

		select {
		case <-time.After(2 * time.Second):
		case <-bufferCtx.Done():
			return nil, errors.Wrap(bufferCtx.Err(), "session buffer timeout exceeded")
		}
	}

	j.Done()

	seekURL, err := sessionSeekURL(baseURL, session.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to construct session seek URL")
	}

	return &SessionBufferResult{
		Session: session,
		BaseURL: baseURL,
		HLSURL:  hlsURL,
		SeekURL: seekURL,
	}, nil
}
