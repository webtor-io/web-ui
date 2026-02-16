package scripts

import (
	"context"
	"fmt"
	"io"
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

func fetchBody(ctx context.Context, a *api.Api, u string) (string, error) {
	rc, err := a.Download(ctx, u)
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", errors.Wrap(err, "failed to read response body")
	}
	return string(data), nil
}

func (s *ActionScript) bufferHLS(ctx context.Context, j *job.Job, streamURL string, bufferDuration time.Duration) error {
	bufferCtx, cancel := context.WithTimeout(ctx, time.Duration(s.warmupTimeoutMin)*time.Minute)
	defer cancel()

	j.InProgress("buffering video content")

	masterBody, err := fetchBody(bufferCtx, s.api, streamURL)
	if err != nil {
		return errors.Wrap(err, "failed to fetch master playlist")
	}

	variantRel, err := parseMasterVideoVariantURL(masterBody)
	if err != nil {
		return err
	}

	variantURL, err := resolveURL(streamURL, variantRel)
	if err != nil {
		return errors.Wrap(err, "failed to resolve variant URL")
	}

	target := bufferDuration.Seconds()
	var buffered float64
	segmentsDownloaded := 0

	for buffered < target {
		select {
		case <-bufferCtx.Done():
			return errors.Wrap(bufferCtx.Err(), "buffer timeout exceeded")
		default:
		}

		playlistBody, err := fetchBody(bufferCtx, s.api, variantURL)
		if err != nil {
			return errors.Wrap(err, "failed to fetch video playlist")
		}

		segments, endList, err := parseMediaPlaylist(playlistBody)
		if err != nil {
			return errors.Wrap(err, "failed to parse video playlist")
		}

		if endList {
			log.Info("HLS stream complete, no buffering needed")
			break
		}

		newSegments := segments[segmentsDownloaded:]
		if len(newSegments) == 0 {
			select {
			case <-time.After(2 * time.Second):
				continue
			case <-bufferCtx.Done():
				return errors.Wrap(bufferCtx.Err(), "buffer timeout exceeded")
			}
		}

		for _, seg := range newSegments {
			segURL, err := resolveURL(variantURL, seg.URL)
			if err != nil {
				return errors.Wrap(err, "failed to resolve segment URL")
			}
			rc, err := s.api.Download(bufferCtx, segURL)
			if err != nil {
				return errors.Wrap(err, "failed to download segment")
			}
			_, copyErr := io.Copy(io.Discard, rc)
			_ = rc.Close()
			if copyErr != nil {
				return errors.Wrap(copyErr, "failed to read segment")
			}
			segmentsDownloaded++
			buffered += seg.Duration
			j.StatusUpdate(fmt.Sprintf("%.0f%%", buffered/target*100))
			if buffered >= target {
				break
			}
		}
	}

	j.Done()
	return nil
}
