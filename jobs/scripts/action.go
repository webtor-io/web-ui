package scripts

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/helpers"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/web"

	log "github.com/sirupsen/logrus"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/template"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"
)

type StreamContent struct {
	ExportTag           *ra.ExportTag
	Resource            *ra.ResourceResponse
	Item                *ra.ListItem
	MediaProbe          *api.MediaProbe
	OpenSubtitles       []api.OpenSubtitleTrack
	VideoStreamUserData *models.VideoStreamUserData
	Settings            *models.StreamSettings
	ExternalData        *models.ExternalData
	DomainSettings      *embed.DomainSettingsData
}

const (
	bandwidthTestSize   = 30 * 1024 * 1024 // 30MB
	bandwidthMultiplier = 1.5
)

type SlowDownloadData struct {
	MeasuredSpeedMbps float64
	RequiredSpeedMbps float64
	BitrateMbps       float64
	IsRateLimited     bool
	RateLimitMbps     float64
	TierName          string
}

type SlowDownloadError struct {
	Data SlowDownloadData
}

func (e *SlowDownloadError) Error() string {
	return "download speed too slow for streaming"
}

type firstByteReader struct {
	r         io.Reader
	firstByte time.Time
	started   bool
}

func (r *firstByteReader) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)
	if n > 0 && !r.started {
		r.firstByte = time.Now()
		r.started = true
	}
	return
}

func getVideoBitrate(mp *api.MediaProbe) int64 {
	if mp.Format.BitRate != "" {
		br, err := strconv.ParseInt(mp.Format.BitRate, 10, 64)
		if err == nil && br > 0 {
			return br
		}
	}
	var total int64
	for _, s := range mp.Streams {
		if s.BitRate != "" {
			br, err := strconv.ParseInt(s.BitRate, 10, 64)
			if err == nil {
				total += br
			}
		}
	}
	return total
}

func parseRateLimit(rate string) int64 {
	rate = strings.TrimSpace(rate)
	if !strings.HasSuffix(rate, "M") || len(rate) < 2 {
		return 0
	}
	n, err := strconv.ParseInt(rate[:len(rate)-1], 10, 64)
	if err != nil {
		return 0
	}
	return n * 1_000_000
}

func isRateLimited(measuredBytesPerSec float64, rateLimitBitsPerSec int64) bool {
	return measuredBytesPerSec*8 >= float64(rateLimitBitsPerSec)*0.9
}

func contentProbeURL(downloadURL string) string {
	if i := strings.IndexByte(downloadURL, '?'); i >= 0 {
		return downloadURL[:i] + "~cp" + downloadURL[i:]
	}
	return downloadURL + "~cp"
}

func (s *ActionScript) streamContent(ctx context.Context, j *job.Job, c *web.Context, resourceID string, itemID string, template string, settings *models.StreamSettings, vsud *models.VideoStreamUserData, dsd *embed.DomainSettingsData) (err error) {
	sc := &StreamContent{
		Settings:       settings,
		ExternalData:   &models.ExternalData{},
		DomainSettings: dsd,
	}
	j.InProgress("retrieving resource data")
	resCtx, resCancel := context.WithTimeout(ctx, 30*time.Second)
	defer resCancel()
	resourceResponse, err := s.api.GetResource(resCtx, c.ApiClaims, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to retrieve resource")
	}
	j.Done()
	sc.Resource = resourceResponse
	j.InProgress("retrieving stream url")
	exportCtx, exportCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer exportCancel()
	exportResponse, err := s.api.ExportResourceContent(exportCtx, c.ApiClaims, resourceID, itemID, settings.ImdbID)
	if err != nil {
		return errors.Wrap(err, "failed to retrieve stream url")
	}
	j.Done()
	sc.ExportTag = exportResponse.ExportItems["stream"].Tag
	sc.Item = &exportResponse.Source
	se := exportResponse.ExportItems["stream"]

	var downloadSpeed float64
	fileSize := int(exportResponse.Source.Size)
	warmupSize := bandwidthTestSize
	if half := fileSize / 2; half > 0 && warmupSize > half {
		warmupSize = half
	}
	downloadURL := exportResponse.ExportItems["download"].URL

	// Step 1: Torrent warmup (if original content not cached)
	needTorrentWarmup := !se.ExportMetaItem.Meta.Cache && (!se.Meta.Transcode || !se.Meta.TranscodeCache)
	if needTorrentWarmup {
		if downloadSpeed, err = s.warmUp(ctx, j, "warming up torrent client", downloadURL, exportResponse.ExportItems["torrent_client_stat"].URL, fileSize, warmupSize, 500*1024, "file", true); err != nil {
			return
		}
	}

	// Step 2: Content probe via ~cp (before transcoder warmup)
	j.InProgress("probing content media info")
	mpCtx, mpCancel := context.WithTimeout(ctx, 1*time.Minute)
	defer mpCancel()
	probeURL := contentProbeURL(downloadURL)
	mp, probeErr := s.api.GetMediaProbe(mpCtx, probeURL)
	if probeErr != nil {
		if mpItem, ok := exportResponse.ExportItems["media_probe"]; ok {
			mp, probeErr = s.api.GetMediaProbe(mpCtx, mpItem.URL)
		}
	}
	if probeErr != nil {
		if se.Meta.Transcode {
			return errors.Wrap(probeErr, "failed to get probe data")
		}
		log.WithError(probeErr).Warn("failed to get content probe")
	} else {
		sc.MediaProbe = mp
		log.Infof("got media probe %+v", mp)
	}
	j.Done()

	// Step 3: Bandwidth check
	if downloadSpeed > 0 && sc.MediaProbe != nil {
		j.InProgress("checking bandwidth")
		bitrate := getVideoBitrate(sc.MediaProbe)
		if bitrate > 0 && downloadSpeed*8 < float64(bitrate)*bandwidthMultiplier {
			sdd := SlowDownloadData{
				MeasuredSpeedMbps: downloadSpeed * 8 / 1_000_000,
				RequiredSpeedMbps: float64(bitrate) * bandwidthMultiplier / 1_000_000,
				BitrateMbps:       float64(bitrate) / 1_000_000,
			}
			if c.ApiClaims != nil && c.ApiClaims.Rate != "" {
				rateLimitBps := parseRateLimit(c.ApiClaims.Rate)
				if rateLimitBps > 0 && isRateLimited(downloadSpeed, rateLimitBps) {
					sdd.IsRateLimited = true
					sdd.RateLimitMbps = float64(rateLimitBps) / 1_000_000
				}
			}
			if c.Claims != nil && c.Claims.Context != nil && c.Claims.Context.Tier != nil {
				sdd.TierName = c.Claims.Context.Tier.Name
			}
			if sdd.TierName == "" {
				sdd.TierName = "free"
			}
			return &SlowDownloadError{Data: sdd}
		}
		j.Done()
	}

	// Step 4: Transcoder warmup (after bandwidth check)
	if se.Meta.Transcode && !se.Meta.TranscodeCache {
		if _, err = s.warmUp(ctx, j, "warming up transcoder", exportResponse.ExportItems["stream"].URL, exportResponse.ExportItems["torrent_client_stat"].URL, 0, -1, -1, "stream", false); err != nil {
			return
		}
	}
	if exportResponse.Source.MediaFormat == ra.Video {
		sc.VideoStreamUserData = vsud
		if subtitles, ok := exportResponse.ExportItems["subtitles"]; ok {
			if osEnabled, ok := settings.Features["opensubtitles"]; (ok && osEnabled) || !ok {
				j.InProgress("loading OpenSubtitles")
				osCtx, osCancel := context.WithTimeout(ctx, 30*time.Second)
				defer osCancel()
				subs, err := s.api.GetOpenSubtitles(osCtx, subtitles.URL)
				if err != nil {
					j.Warn(errors.Wrap(err, "failed to get OpenSubtitles"))
				} else {
					sc.OpenSubtitles = subs
					j.Done()
				}
			}
		}
	}
	if se.Meta.Transcode && exportResponse.Source.MediaFormat == ra.Video {
		if err = s.bufferHLS(ctx, j, exportResponse.ExportItems["stream"].URL, 5*time.Minute); err != nil {
			j.Warn(errors.Wrap(err, "failed to buffer video content"))
		}
	}
	if settings.Poster != "" {
		sc.ExternalData.Poster = s.api.AttachExternalFile(se, settings.Poster)
	}
	for _, v := range settings.Subtitles {
		sc.ExternalData.Tracks = append(sc.ExternalData.Tracks, models.ExternalTrack{
			Src:     s.api.AttachExternalSubtitle(se, v.Src),
			Label:   v.Label,
			SrcLang: v.SrcLang,
			Default: v.Default != nil,
		})
	}
	err = s.renderActionTemplate(j, c, sc, template)
	if err != nil {
		return errors.Wrap(err, "failed to render resource")
	}
	j.InProgress("waiting player initialization")
	return
}

func (s *ActionScript) previewImage(ctx context.Context, j *job.Job, c *web.Context, resourceID string, itemID string, settings *models.StreamSettings, vsud *models.VideoStreamUserData, dsd *embed.DomainSettingsData) error {
	return s.streamContent(ctx, j, c, resourceID, itemID, "preview_image", settings, vsud, dsd)
}

func (s *ActionScript) streamAudio(ctx context.Context, j *job.Job, c *web.Context, resourceID string, itemID string, settings *models.StreamSettings, vsud *models.VideoStreamUserData, dsd *embed.DomainSettingsData) error {
	return s.streamContent(ctx, j, c, resourceID, itemID, "stream_audio", settings, vsud, dsd)
}

func (s *ActionScript) streamVideo(ctx context.Context, j *job.Job, c *web.Context, resourceID string, itemID string, settings *models.StreamSettings, vsud *models.VideoStreamUserData, dsd *embed.DomainSettingsData) error {
	return s.streamContent(ctx, j, c, resourceID, itemID, "stream_video", settings, vsud, dsd)
}

func (s *ActionScript) renderActionTemplate(j *job.Job, c *web.Context, sc *StreamContent, name string) error {
	actionTemplate := "action/" + name
	tpl := s.tb.Build(actionTemplate).WithLayoutBody(`{{ template "main" . }}`)
	str, err := tpl.ToString(c.WithData(sc))
	if err != nil {
		return err
	}
	j.RenderTemplate("rendering action", actionTemplate, strings.TrimSpace(str))
	return nil
}

type FileDownload struct {
	URL    string
	HasAds bool
}

func (s *ActionScript) download(ctx context.Context, j *job.Job, c *web.Context, resourceID string, itemID string) (err error) {
	j.InProgress("retrieving download link")
	exportCtx, exportCancel := context.WithTimeout(ctx, 30*time.Second)
	defer exportCancel()
	resp, err := s.api.ExportResourceContent(exportCtx, c.ApiClaims, resourceID, itemID, "")
	if err != nil {
		return errors.Wrap(err, "failed to retrieve download link")
	}
	j.Done()
	de := resp.ExportItems["download"]
	//url := de.URL
	if !de.ExportMetaItem.Meta.Cache {
		if _, err := s.warmUp(ctx, j, "warming up torrent client", resp.ExportItems["download"].URL, resp.ExportItems["torrent_client_stat"].URL, int(resp.Source.Size), 1024*1024, 0, "", true); err != nil {
			return err
		}
	}
	j.DoneWithMessage("success! file is ready for download!")
	tpl := s.tb.Build("action/download_file").WithLayoutBody(`{{ template "main" . }}`)
	hasAds := false
	if c.Claims != nil && c.Claims.Claims != nil {
		hasAds = !c.Claims.Claims.Site.NoAds
	}
	str, err := tpl.ToString(c.WithData(&FileDownload{
		URL:    de.URL,
		HasAds: hasAds,
	}))
	if err != nil {
		return err
	}
	j.Custom("action/download_file", strings.TrimSpace(str))
	return
}

func (s *ActionScript) warmUp(ctx context.Context, j *job.Job, m string, u string, su string, size int, limitStart int, limitEnd int, tagSuff string, useStatus bool) (downloadSpeed float64, err error) {
	tag := "download"
	if tagSuff != "" {
		tag += "-" + tagSuff
	}
	if limitStart > size {
		limitStart = size
	}
	if limitEnd > size-limitStart {
		limitEnd = size - limitStart
	}
	if size > 0 {
		j.InProgress(fmt.Sprintf("%v, downloading %v", m, helpers.Bytes(uint64(limitStart+limitEnd))))
	} else {
		j.InProgress(m)
	}
	warmupCtx, warmupCancel := context.WithTimeout(ctx, time.Duration(s.warmupTimeoutMin)*time.Minute)
	defer warmupCancel()

	if useStatus {
		j.StatusUpdate("waiting for peers")
		go func() {
			ch, err := s.api.Stats(warmupCtx, su)
			if err != nil {
				log.WithError(err).Error("failed to get stats")
				return
			}
			for {
				select {
				case ev, ok := <-ch:
					if !ok {
						return
					}
					j.StatusUpdate(fmt.Sprintf("%v peers", ev.Peers))
				case <-warmupCtx.Done():
					return
				}
			}
		}()
	}

	b, err := s.api.DownloadWithRange(warmupCtx, u, 0, limitStart)
	if err != nil {
		return 0, errors.Wrap(err, "failed to start download")
	}
	defer func(b io.ReadCloser) {
		_ = b.Close()
	}(b)

	fbr := &firstByteReader{r: b}
	n, err := io.Copy(io.Discard, fbr)
	if fbr.started {
		elapsed := time.Since(fbr.firstByte)
		if elapsed > 0 && n > 0 {
			downloadSpeed = float64(n) / elapsed.Seconds()
		}
	}

	if limitEnd > 0 {
		b2, err := s.api.DownloadWithRange(warmupCtx, u, size-limitEnd, -1)
		if err != nil {
			return 0, errors.Wrap(err, "failed to start download")
		}
		defer func(b2 io.ReadCloser) {
			_ = b2.Close()
		}(b2)
		_, err = io.Copy(io.Discard, b2)
	}
	if errors.Is(errors.Cause(err), context.DeadlineExceeded) {
		return 0, errors.Wrap(err, fmt.Sprintf("failed to download within %v minutes", s.warmupTimeoutMin))
	} else if err != nil {
		return 0, errors.Wrap(err, "failed to download")
	}

	j.Done()
	return
}

type ActionScript struct {
	api              *api.Api
	c                *web.Context
	resourceId       string
	itemId           string
	action           string
	tb               template.Builder[*web.Context]
	settings         *models.StreamSettings
	vsud             *models.VideoStreamUserData
	dsd              *embed.DomainSettingsData
	warmupTimeoutMin int
}

func (s *ActionScript) Run(ctx context.Context, j *job.Job) (err error) {
	switch s.action {
	case "download":
		return s.download(ctx, j, s.c, s.resourceId, s.itemId)
	case "download-dir":
		return s.download(ctx, j, s.c, s.resourceId, s.itemId)
	case "preview-image":
		return s.previewImage(ctx, j, s.c, s.resourceId, s.itemId, s.settings, s.vsud, s.dsd)
	case "stream-audio":
		return s.streamAudio(ctx, j, s.c, s.resourceId, s.itemId, s.settings, s.vsud, s.dsd)
	case "stream-video":
		return s.streamVideo(ctx, j, s.c, s.resourceId, s.itemId, s.settings, s.vsud, s.dsd)
	}
	return
}

type ErrorWrapperScript struct {
	tb     template.Builder[*web.Context]
	Script job.Runnable
	c      *web.Context
}

func (s *ErrorWrapperScript) Run(ctx context.Context, j *job.Job) (err error) {
	err = s.Script.Run(ctx, j)
	if sde, ok := err.(*SlowDownloadError); ok {
		tpl := s.tb.Build("action/errors/slow_download").WithLayoutBody(`{{ template "main" . }}`)
		str, terr := tpl.ToString(s.c.WithData(&sde.Data))
		if terr != nil {
			return terr
		}
		log.WithError(err).WithField("data", sde.Data).Warn("bandwidth check failed")
		j.Fail()
		j.Custom("action/errors/slow_download", strings.TrimSpace(str))
		return nil
	}
	if errors.Is(errors.Cause(err), context.DeadlineExceeded) {
		tpl := s.tb.Build("action/errors/no_peers").WithLayoutBody(`{{ template "main" . }}`)
		str, terr := tpl.ToString(s.c)
		if terr != nil {
			return terr
		}
		_ = j.Error(err)
		j.Custom("action/errors/no_peers", strings.TrimSpace(str))
		return nil
	}
	return err
}

func Action(tb template.Builder[*web.Context], api *api.Api, c *web.Context, resourceID string, itemID string, action string, settings *models.StreamSettings, dsd *embed.DomainSettingsData, vsud *models.VideoStreamUserData, warmupTimeoutMin int) (r job.Runnable, id string) {
	vsudID := vsud.AudioID + "/" + vsud.SubtitleID + "/" + fmt.Sprintf("%+v", vsud.AcceptLangTags)
	settingsID := fmt.Sprintf("%+v", settings)
	hourKey := time.Now().UTC().Format("2006010215")
	id = fmt.Sprintf("%x", sha1.Sum([]byte(resourceID+"/"+itemID+"/"+action+"/"+c.ApiClaims.Role+"/"+settingsID+"/"+vsudID+"/"+hourKey)))
	return &ErrorWrapperScript{
		tb: tb,
		c:  c,
		Script: &ActionScript{
			tb:               tb,
			api:              api,
			c:                c,
			resourceId:       resourceID,
			itemId:           itemID,
			action:           action,
			settings:         settings,
			vsud:             vsud,
			dsd:              dsd,
			warmupTimeoutMin: warmupTimeoutMin,
		},
	}, id
}
