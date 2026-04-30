package scripts

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/helpers"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/i18n"
	us "github.com/webtor-io/web-ui/services/user_subtitle"
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
	UserSubtitles       []models.UserSubtitleTrack
	UserSubtitlesEnabled bool
	// EIURL is the ExportItem "stream" URL — the torrent-http-proxy
	// origin carrying whatever auth the cluster embeds (subdomain,
	// path, query). Handed to the upload form as a hidden field so the
	// async re-render can wrap newly uploaded user subtitles through
	// /ext/ with the same credentials as the initial render.
	EIURL               string
	VideoStreamUserData *models.VideoStreamUserData
	Settings            *models.StreamSettings
	ExternalData        *models.ExternalData
	DomainSettings      *embed.DomainSettingsData
	TranscoderSession   *api.TranscoderSession
	SessionSeekURL      string
	// GraceDurationSec is non-zero only for free-tier users when grace rules
	// are enabled. Surfaced to the player JS so it knows when to show the
	// soft signup CTA after the grace window passes.
	GraceDurationSec int
	// GraceFreeRateMbps is the user's plan-cap rate (in Mbps) parsed from
	// ApiClaims.Rate. Shown on the "Continue at X Mbps" secondary CTA. Zero
	// when the claim is missing/unparseable — template hides the line.
	GraceFreeRateMbps int
}

const (
	bandwidthTestSize = 50 * 1024 * 1024 // 50MB
	bandwidthSkipSize = 10 * 1024 * 1024 // 10MB — skip for speed measurement
)

// WarmupSettings groups all torrent warmup tuning knobs so call-sites pass a
// single value rather than three loose ints. Wired from CLI/env flags in
// jobs.New() and threaded through Action/Embed scripts unchanged.
type WarmupSettings struct {
	// TimeoutMin is the hard warmup deadline (overall context.WithTimeout).
	TimeoutMin int
	// NoPeersTimeoutSec is the watchdog cutoff for "no bytes AND no peers" —
	// when no peers have appeared within this window we surface the no_peers
	// CTA early instead of waiting the full TimeoutMin.
	NoPeersTimeoutSec int
	// SlowPeersTimeoutSec is the watchdog cutoff for "peers exist but the
	// rate is below the early-min threshold" — surfaces the no_peers CTA
	// before probe hangs on its own deadline.
	SlowPeersTimeoutSec int
}

type SlowDownloadData struct {
	MeasuredSpeedMbps float64
	RequiredSpeedMbps float64
	BitrateMbps       float64
	IsRateLimited     bool
	RateLimitMbps     float64
	TierName          string
	// Form-resubmit context — populated in ErrorWrapperScript right before
	// rendering. Lets the "Continue at slow speed" button POST back to the
	// originating action endpoint with force-slow=true and target the same
	// progress-log container so the new job replaces the failed one in place.
	Action      string
	Endpoint    string
	ResourceID  string
	ItemID      string
	LogTargetID string
}

type SlowDownloadError struct {
	Data SlowDownloadData
}

func (e *SlowDownloadError) Error() string {
	return "download speed too slow for streaming"
}

type NoPeersError struct{}

func (e *NoPeersError) Error() string {
	return "no peers / nothing downloaded during warmup"
}

type speedReader struct {
	r            io.Reader
	bytesRead    atomic.Int64
	skipBytes    int64
	measureStart atomic.Int64 // unix nano, set once when started
	started      atomic.Bool
}

func (r *speedReader) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)
	if n > 0 {
		newBytes := r.bytesRead.Add(int64(n))
		if !r.started.Load() && newBytes >= r.skipBytes {
			if r.started.CompareAndSwap(false, true) {
				r.measureStart.Store(time.Now().UnixNano())
			}
		}
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

func buildSlowDownloadData(c *web.Context, measuredBytesPerSec float64, bitrate int64) SlowDownloadData {
	sdd := SlowDownloadData{
		MeasuredSpeedMbps: measuredBytesPerSec * 8 / 1_000_000,
		RequiredSpeedMbps: float64(bitrate) / 1_000_000,
		BitrateMbps:       float64(bitrate) / 1_000_000,
	}
	if c.ApiClaims != nil && c.ApiClaims.Rate != "" {
		if rateLimitBps := parseRateLimit(c.ApiClaims.Rate); rateLimitBps > 0 && isRateLimited(measuredBytesPerSec, rateLimitBps) {
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
	return sdd
}

// checkCachedRateLimit decides whether a cached stream should raise the
// slow-download warning based purely on the user's subscription-tier rate cap.
// Cached content comes from CDN/S3 fast enough to saturate the cap, so the cap
// itself is the effective throughput — no probe download needed.
func checkCachedRateLimit(c *web.Context, bitrate int64) (SlowDownloadData, bool) {
	if c.ApiClaims == nil || c.ApiClaims.Rate == "" {
		return SlowDownloadData{}, false
	}
	rateLimitBps := parseRateLimit(c.ApiClaims.Rate)
	if rateLimitBps == 0 {
		return SlowDownloadData{}, false
	}
	if float64(rateLimitBps) >= float64(bitrate) {
		return SlowDownloadData{}, false
	}
	return buildSlowDownloadData(c, float64(rateLimitBps)/8, bitrate), true
}

func contentProbeURL(downloadURL string) string {
	if i := strings.IndexByte(downloadURL, '?'); i >= 0 {
		return downloadURL[:i] + "~cp" + downloadURL[i:]
	}
	return downloadURL + "~cp"
}

func sessionBaseURL(streamURL string) (string, error) {
	u, err := url.Parse(streamURL)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse stream URL")
	}
	idx := strings.Index(u.Path, "~hls/")
	if idx < 0 {
		return "", errors.New("stream URL does not contain ~hls/ suffix")
	}
	u.Path = u.Path[:idx] + "~hls"
	return u.String(), nil
}

func sessionHLSURL(baseURL string, sessionID string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse base URL")
	}
	u.Path += "/session/" + sessionID + "/index.m3u8"
	return u.String(), nil
}

func sessionSeekURL(baseURL string, sessionID string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse base URL")
	}
	u.Path += "/session/" + sessionID + "/seek"
	return u.String(), nil
}

func (s *ActionScript) streamContent(ctx context.Context, j *job.Job, c *web.Context, resourceID string, itemID string, template string, settings *models.StreamSettings, vsud *models.VideoStreamUserData, dsd *embed.DomainSettingsData) (err error) {
	sc := &StreamContent{
		Settings:       settings,
		ExternalData:   &models.ExternalData{},
		DomainSettings: dsd,
	}
	// Dev-only short-circuit: render the slow_download / no_peers error
	// modals without any rest-api work. Wired from the resource-page hash
	// (`#action=stream&debug=slow_download|slow_download_bt|no_peers`);
	// the handler gates on gin.Mode != release so this never fires in
	// prod even if a client posts the field.
	switch s.debug {
	case "slow_download":
		// Cap-modal variant (IsRateLimited=true) — under grace=ON this
		// branch is unreachable for free; debug is the only way to see it.
		// Rate is force-pinned to 5 Mbps so the modal renders even for
		// paid tiers whose real cap sits above the synthetic bitrate.
		// TierName still comes from real claims so the rendered tier
		// label matches the logged-in user.
		sdd := SlowDownloadData{
			MeasuredSpeedMbps: 5,
			RequiredSpeedMbps: 10,
			BitrateMbps:       10,
			IsRateLimited:     true,
			RateLimitMbps:     5,
			TierName:          "free",
		}
		if c.Claims != nil && c.Claims.Context != nil && c.Claims.Context.Tier != nil && c.Claims.Context.Tier.Name != "" {
			sdd.TierName = c.Claims.Context.Tier.Name
		}
		return &SlowDownloadError{Data: sdd}
	case "slow_download_bt":
		// BT-slow variant (IsRateLimited=false) — swarm-bottleneck path.
		sdd := SlowDownloadData{
			MeasuredSpeedMbps: 1,
			RequiredSpeedMbps: 10,
			BitrateMbps:       10,
			IsRateLimited:     false,
			TierName:          "free",
		}
		if c.Claims != nil && c.Claims.Context != nil && c.Claims.Context.Tier != nil && c.Claims.Context.Tier.Name != "" {
			sdd.TierName = c.Claims.Context.Tier.Name
		}
		return &SlowDownloadError{Data: sdd}
	case "no_peers":
		return &NoPeersError{}
	}
	// Free-tier grace: attach rules to the outgoing primary claims BEFORE
	// the export call so rest-api carries them into every signed URL token
	// it returns. THP reads them off the segment-request token. See
	// docs/grace_token.md.
	graceMode := s.grace.Enabled && isFreeTier(c)
	if graceMode {
		s.applyGraceRules(sc, resourceID, c)
	}
	j.InProgress(s.t("job.retrievingData"))
	resCtx, resCancel := context.WithTimeout(ctx, 30*time.Second)
	defer resCancel()
	resourceResponse, err := s.api.GetResource(resCtx, c.ApiClaims, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to retrieve resource")
	}
	j.Done()
	sc.Resource = resourceResponse
	j.InProgress(s.t("job.retrievingStreamUrl"))
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

	// Step 1: Torrent warmup (skip for cached/vault content; also skipped on
	// forceSlow — the user already accepted slow playback, so we save the warmup
	// budget and let the transcoder pull cold instead).
	if !se.Meta.Cache {
		if s.forceSlow {
			j.Skip(s.t("job.warmingUp"))
		} else {
			skipBytes := bandwidthSkipSize
			if warmupSize <= skipBytes {
				skipBytes = 0
			}
			if downloadSpeed, err = s.warmUp(ctx, j, s.t("job.warmingUp"), downloadURL, exportResponse.ExportItems["torrent_client_stat"].URL, fileSize, warmupSize, 500*1024, skipBytes, "file", true); err != nil {
				return
			}
		}
	}

	// Step 2: Content probe via ~cp (before transcoder warmup)
	j.InProgress(s.t("job.probingMedia"))
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

	// Step 3: Bandwidth check.
	//
	// BT-slow path: when probe is needed (non-cached), we compare measured
	// download speed against required bitrate. Even under grace mode this is
	// kept — grace rate won't help if the user's own internet is the bottleneck.
	//
	// Cap-modal path (cached content + plan cap below bitrate): kept under
	// flag-off, skipped under graceMode. Under grace, THP delivers the first
	// DurationSec at full grace rate, and the soft CTA replaces the upfront
	// modal as the conversion surface.
	//
	// On forceSlow we emit Skip instead of running the gate — the user already
	// opted into slow playback.
	if sc.MediaProbe != nil {
		bitrate := getVideoBitrate(sc.MediaProbe)
		if bitrate > 0 {
			if s.forceSlow {
				j.Skip(s.t("job.checkingBandwidth"))
			} else if se.Meta.Cache && !graceMode {
				j.InProgress(s.t("job.checkingBandwidth"))
				if sdd, limited := checkCachedRateLimit(c, bitrate); limited {
					return &SlowDownloadError{Data: sdd}
				}
				j.Done()
			} else if !se.Meta.Cache && downloadSpeed > 0 {
				j.InProgress(s.t("job.checkingBandwidth"))
				if downloadSpeed*8 < float64(bitrate) {
					return &SlowDownloadError{Data: buildSlowDownloadData(c, downloadSpeed, bitrate)}
				}
				j.Done()
			}
		}
	}

	// Step 4: Session transcoder (after bandwidth check)
	if se.Meta.Transcode && (exportResponse.Source.MediaFormat == ra.Video || exportResponse.Source.MediaFormat == ra.Audio) {
		result, serr := s.bufferSessionHLS(ctx, j, exportResponse.ExportItems["stream"].URL, 30*time.Second)
		if serr != nil {
			return errors.Wrap(serr, "failed to buffer session HLS")
		}
		sc.TranscoderSession = result.Session
		sc.ExportTag.Sources = []ra.ExportSource{{
			Src:  result.HLSURL,
			Type: "application/vnd.apple.mpegurl",
		}}
		sc.SessionSeekURL = result.SeekURL
	}
	sc.VideoStreamUserData = vsud
	sc.UserSubtitlesEnabled = s.userSubtitles.Enabled()
	sc.EIURL = se.URL
	if exportResponse.Source.MediaFormat == ra.Video {
		if s.userSubtitles.Enabled() && c.User != nil && c.User.HasAuth() {
			usCtx, usCancel := context.WithTimeout(ctx, 10*time.Second)
			// DB stores bindings keyed by Source.PathStr (matching the
			// convention used by watch_history and the upload form).
			// itemID here is ra.ListItem.ID, which is NOT the same.
			list, listErr := s.userSubtitles.List(usCtx, c.User.ID, resourceID, exportResponse.Source.PathStr)
			usCancel()
			if listErr != nil {
				log.WithError(listErr).Warn("failed to load user subtitles")
			} else {
				for _, sub := range list {
					publicURL := s.userSubtitles.PublicURL(sub.Hash, sub.OriginalName)
					sc.UserSubtitles = append(sc.UserSubtitles, models.UserSubtitleTrack{
						ID:           us.TrackID(sub.UserSubtitleID),
						Src:          s.api.AttachExternalSubtitle(se, publicURL),
						Label:        sub.OriginalName,
						Format:       sub.Format,
						Size:         sub.Size,
						OriginalName: sub.OriginalName,
						DeleteURL:    us.DeleteURL(sub.UserSubtitleID),
					})
				}
			}
		}
		if subtitles, ok := exportResponse.ExportItems["subtitles"]; ok {
			if osEnabled, ok := settings.Features["opensubtitles"]; (ok && osEnabled) || !ok {
				j.InProgress(s.t("job.loadingSubtitles"))
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
	j.InProgress(s.t("job.waitingPlayer"))
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
	URL      string
	HasAds   bool
	TierName string
}

type NoPeersData struct {
	TierName string
}

func (s *ActionScript) download(ctx context.Context, j *job.Job, c *web.Context, resourceID string, itemID string) (err error) {
	j.InProgress(s.t("job.retrievingDownloadLink"))
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
		if _, err := s.warmUp(ctx, j, s.t("job.warmingUp"), resp.ExportItems["download"].URL, resp.ExportItems["torrent_client_stat"].URL, int(resp.Source.Size), 1024*1024, 0, 0, "", true); err != nil {
			return err
		}
	}
	j.DoneWithMessage(s.t("job.downloadReady"))
	tpl := s.tb.Build("action/download_file").WithLayoutBody(`{{ template "main" . }}`)
	hasAds := false
	tierName := "free"
	if c.Claims != nil {
		if c.Claims.Claims != nil {
			hasAds = !c.Claims.Claims.Site.NoAds
		}
		if c.Claims.Context != nil && c.Claims.Context.Tier != nil && c.Claims.Context.Tier.Name != "" {
			tierName = c.Claims.Context.Tier.Name
		}
	}
	str, err := tpl.ToString(c.WithData(&FileDownload{
		URL:      de.URL,
		HasAds:   hasAds,
		TierName: tierName,
	}))
	if err != nil {
		return err
	}
	j.Custom("action/download_file", strings.TrimSpace(str))
	return
}

func (s *ActionScript) warmUp(ctx context.Context, j *job.Job, m string, u string, su string, size int, limitStart int, limitEnd int, skipBytes int, tagSuff string, useStatus bool) (downloadSpeed float64, err error) {
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
		downloading := s.tp("job.downloading", map[string]any{
			"Bytes": helpers.Bytes(uint64(limitStart + limitEnd)),
		})
		j.InProgress(fmt.Sprintf("%v, %v", m, downloading))
	} else {
		j.InProgress(m)
	}
	warmupCtx, warmupCancel := context.WithTimeout(ctx, time.Duration(s.warmup.TimeoutMin)*time.Minute)
	defer warmupCancel()

	var peerCount atomic.Int32
	var noPeersFlag atomic.Bool
	// statsEverSeen flips on the first SSE statupdate from torrent-web-seeder.
	// The watchdog requires this before firing the no-peers predicate so a
	// late SSE stream (cold-started seeder pod, proxy hop) cannot be
	// misread as "no peers" — absence of stats is not evidence of absence
	// of peers.
	var statsEverSeen atomic.Bool

	if useStatus {
		j.StatusUpdate(s.t("job.waitingForPeers"))
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
					statsEverSeen.Store(true)
					peerCount.Store(int32(ev.Peers))
					j.StatusUpdate(s.tp("job.peers", map[string]any{"Peers": ev.Peers}))
				case <-warmupCtx.Done():
					return
				}
			}
		}()
	}

	sr := &speedReader{skipBytes: int64(skipBytes)}
	warmupStart := time.Now()

	// Watchdog: surface no_peers CTA early instead of waiting the full warmup
	// deadline. Two thresholds (both configurable via env):
	//   - WARMUP_NO_PEERS_TIMEOUT_SEC + zero bytes + zero peers — torrent has
	//     no peers at all. Gated on statsEverSeen so we don't conflate a slow
	//     SSE stream (cold-start seeder, premium-edge buffering) with absence
	//     of peers; if no statupdate has arrived we fall through to the slow
	//     peers branch which measures throughput directly.
	//   - WARMUP_SLOW_PEERS_TIMEOUT_SEC + bytes < earlyMinBytes — peers serve,
	//     but the rate is so low (<17 KB/s avg for 1 MB threshold) that probe
	//     will hang on its own 1-min deadline anyway. Surface CTA now instead
	//     of waiting.
	const earlyMinBytes = 1 * 1024 * 1024
	noPeersAfter := time.Duration(s.warmup.NoPeersTimeoutSec) * time.Second
	slowPeersAfter := time.Duration(s.warmup.SlowPeersTimeoutSec) * time.Second
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-warmupCtx.Done():
				return
			case <-ticker.C:
				bytes := sr.bytesRead.Load()
				elapsed := time.Since(warmupStart)
				if elapsed > noPeersAfter && bytes == 0 && peerCount.Load() == 0 && statsEverSeen.Load() {
					noPeersFlag.Store(true)
					warmupCancel()
					return
				}
				if elapsed > slowPeersAfter && bytes < earlyMinBytes {
					noPeersFlag.Store(true)
					warmupCancel()
					return
				}
			}
		}
	}()

	b, err := s.api.DownloadWithRange(warmupCtx, u, 0, limitStart)
	if err != nil {
		if noPeersFlag.Load() {
			return 0, &NoPeersError{}
		}
		return 0, errors.Wrap(err, "failed to start download")
	}
	defer func(b io.ReadCloser) {
		_ = b.Close()
	}(b)

	sr.r = b
	_, err = io.Copy(io.Discard, sr)
	if noPeersFlag.Load() {
		return 0, &NoPeersError{}
	}
	if sr.started.Load() {
		measured := sr.bytesRead.Load() - sr.skipBytes
		elapsed := time.Since(time.Unix(0, sr.measureStart.Load()))
		if elapsed > 0 && measured > 0 {
			downloadSpeed = float64(measured) / elapsed.Seconds()
		}
	} else if bytes := sr.bytesRead.Load(); bytes > 0 {
		// Hard deadline hit before measurement window opened — rough estimate
		// over the whole warmup span so the bandwidth-check has *some* number
		// to classify against.
		elapsed := time.Since(warmupStart)
		if elapsed > 0 {
			downloadSpeed = float64(bytes) / elapsed.Seconds()
		}
	}

	if errors.Is(errors.Cause(err), context.DeadlineExceeded) {
		// Hard warmup timeout. If we didn't even reach the measurement window
		// (skipBytes), the torrent is effectively dead — probe will hang on
		// its own deadline too. Surface no_peers immediately. Otherwise pass
		// the measured speed to probe + bandwidth-check.
		bytes := sr.bytesRead.Load()
		if bytes < int64(skipBytes) {
			log.WithField("elapsed", time.Since(warmupStart)).
				WithField("bytes", bytes).
				Warn("warmup hard timeout, insufficient data — surfacing no_peers")
			return 0, &NoPeersError{}
		}
		log.WithField("elapsed", time.Since(warmupStart)).
			WithField("bytes", bytes).
			WithField("speed_bps", downloadSpeed).
			Warn("warmup hard timeout, continuing with partial measurement")
		err = nil
	} else if err != nil {
		return 0, errors.Wrap(err, "failed to download")
	}

	if limitEnd > 0 {
		b2, err2 := s.api.DownloadWithRange(warmupCtx, u, size-limitEnd, -1)
		if err2 != nil {
			// Tail prefetch is best-effort (used for seek warmup); don't fail.
			log.WithError(err2).Warn("warmup tail download failed")
		} else {
			defer func(b2 io.ReadCloser) { _ = b2.Close() }(b2)
			_, _ = io.Copy(io.Discard, b2)
		}
	}

	j.Done()
	return
}

type ActionScript struct {
	api           *api.Api
	c             *web.Context
	i18n          *i18n.Service
	userSubtitles *us.Service
	resourceId    string
	itemId        string
	action        string
	tb            template.Builder[*web.Context]
	settings      *models.StreamSettings
	vsud          *models.VideoStreamUserData
	dsd           *embed.DomainSettingsData
	warmup        WarmupSettings
	grace         GraceSettings
	forceSlow     bool
	debug         string
}

func (s *ActionScript) t(key string) string {
	return i18n.TranslateWithLocalizer(s.i18n.Localizer(s.c.Lang), key)
}

func (s *ActionScript) tp(key string, data map[string]any) string {
	return i18n.TranslateWithLocalizerData(s.i18n.Localizer(s.c.Lang), key, data)
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
	tb         template.Builder[*web.Context]
	Script     job.Runnable
	c          *web.Context
	action     string
	resourceId string
	itemId     string
}

// actionEndpoint maps the internal action id to the public POST route the
// "Continue at slow speed" form re-submits to. Kept in the wrapper because it
// is the layer that knows about HTTP route names; ActionScript itself is
// transport-agnostic.
func actionEndpoint(action string) string {
	switch action {
	case "stream-video":
		return "/stream-video"
	case "stream-audio":
		return "/stream-audio"
	case "preview-image":
		return "/preview-image"
	case "download":
		return "/download-file"
	}
	return ""
}

func (s *ErrorWrapperScript) Run(ctx context.Context, j *job.Job) (err error) {
	err = s.Script.Run(ctx, j)
	if sde, ok := err.(*SlowDownloadError); ok {
		sde.Data.Action = s.action
		sde.Data.Endpoint = actionEndpoint(s.action)
		sde.Data.ResourceID = s.resourceId
		sde.Data.ItemID = s.itemId
		// Streaming buttons (MakeAudio/MakeVideo) wire data-async-target to
		// "#log-{ItemID}" because MakeButton sets ButtonItem.ID = Item.ID.
		// Mirroring that here keeps the resubmit landing in the same
		// progress-log container that just rendered this modal.
		sde.Data.LogTargetID = s.itemId
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
	if _, ok := err.(*NoPeersError); ok || errors.Is(errors.Cause(err), context.DeadlineExceeded) {
		tpl := s.tb.Build("action/errors/no_peers").WithLayoutBody(`{{ template "main" . }}`)
		tierName := "free"
		if s.c.Claims != nil && s.c.Claims.Context != nil && s.c.Claims.Context.Tier != nil && s.c.Claims.Context.Tier.Name != "" {
			tierName = s.c.Claims.Context.Tier.Name
		}
		str, terr := tpl.ToString(s.c.WithData(&NoPeersData{TierName: tierName}))
		if terr != nil {
			return terr
		}
		log.WithError(err).Warn("no peers / warmup deadline — surfacing CTA")
		j.Fail()
		j.Custom("action/errors/no_peers", strings.TrimSpace(str))
		return nil
	}
	return err
}

func Action(tb template.Builder[*web.Context], api *api.Api, i18nSvc *i18n.Service, userSubtitles *us.Service, c *web.Context, resourceID string, itemID string, action string, settings *models.StreamSettings, dsd *embed.DomainSettingsData, vsud *models.VideoStreamUserData, warmup WarmupSettings, grace GraceSettings, forceSlow bool, debug string) (r job.Runnable, id string) {
	vsudID := vsud.AudioID + "/" + vsud.SubtitleID + "/" + fmt.Sprintf("%+v", vsud.AcceptLangTags)
	settingsID := fmt.Sprintf("%+v", settings)
	now := time.Now().UTC()
	// Cache key includes the authenticated user's id so two users on the
	// same file don't share each other's rendered template through the
	// job-queue cache; and the concatenated hashes of their uploaded
	// subtitles for this file so an upload or delete invalidates the cache
	// immediately — otherwise the new <track> element would only appear
	// after the 10-minute cache bucket rolled over.
	userKey := ""
	userSubsKey := ""
	if c != nil && c.User != nil && c.User.HasAuth() {
		userKey = c.User.ID.String()
		if userSubtitles.Enabled() {
			// Cache-key lookup intentionally scopes to resource, not
			// (resource, path): ListItem.ID (itemID) and ListItem.PathStr
			// are different identifiers and we'd need an extra API call
			// to resolve Source.PathStr here. Hashing by resource is a
			// slight over-invalidation (a subtitle upload for file A
			// also invalidates file B's cached render under the same
			// torrent) but eliminates any id/path mismatch and keeps
			// this path synchronous.
			listCtx, listCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if hashes, err := userSubtitles.ListHashesForResource(listCtx, c.User.ID, resourceID); err == nil {
				sort.Strings(hashes)
				userSubsKey = strings.Join(hashes, ",")
			}
			listCancel()
		}
	}
	cacheKey := fmt.Sprintf("%s%d", now.Format("2006010215"), now.Minute()/10)
	forceSlowKey := ""
	if forceSlow {
		forceSlowKey = "fs"
	}
	debugKey := ""
	if debug != "" {
		debugKey = "dbg-" + debug
	}
	id = fmt.Sprintf("%x", sha1.Sum([]byte(resourceID+"/"+itemID+"/"+action+"/"+c.ApiClaims.Role+"/"+settingsID+"/"+vsudID+"/"+cacheKey+"/"+c.Lang+"/"+userKey+"/"+userSubsKey+"/"+forceSlowKey+"/"+debugKey)))
	return &ErrorWrapperScript{
		tb:         tb,
		c:          c,
		action:     action,
		resourceId: resourceID,
		itemId:     itemID,
		Script: &ActionScript{
			tb:            tb,
			api:           api,
			i18n:          i18nSvc,
			userSubtitles: userSubtitles,
			c:             c,
			resourceId:    resourceID,
			itemId:        itemID,
			action:        action,
			settings:      settings,
			vsud:          vsud,
			dsd:           dsd,
			warmup:        warmup,
			grace:         grace,
			forceSlow:     forceSlow,
			debug:         debug,
		},
	}, id
}
