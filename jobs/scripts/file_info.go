package scripts

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	ra "github.com/webtor-io/rest-api/services"
	"golang.org/x/text/language"

	"github.com/webtor-io/web-ui/helpers"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/statusview"
	"github.com/webtor-io/web-ui/services/web"
)

// fileInfoProbeTimeout bounds the probe as streamContent bounds its own.
const fileInfoProbeTimeout = time.Minute

// fileInfo is the "File info" job (docs/file_info.md): export, then the ~cp
// probe, then the card. No warm-up, the way services/thumbnail probes: a
// probe content-prober already holds costs nothing, and since content-prober
// 030c8ee a probe this job gives up on is not cached as the file's failure,
// so it cannot break the next Watch. No thumbnail, no transcoder session,
// nothing stored -- and no URL in the card: the job log is public (ACAO *).
func (s *ActionScript) fileInfo(ctx context.Context, j *job.Job, c *web.Context, resourceID string, itemID string) error {
	j.InProgress(s.t("job.retrievingDownloadLink"))
	exportCtx, exportCancel := context.WithTimeout(ctx, 30*time.Second)
	defer exportCancel()
	resp, err := s.api.ExportResourceContent(exportCtx, c.ApiClaims, resourceID, itemID, "")
	if err != nil {
		// A stoplisted file is refused here (403, error.forbidden).
		return errors.Wrap(err, "failed to retrieve download link")
	}
	// rest-api's 404 comes back as no error and no exports (api.doRequest).
	if resp == nil || resp.ExportItems["download"].URL == "" {
		return errors.New("resource not found: no download link")
	}
	// The button is only on video and audio; this is a hand-made POST.
	if f := resp.Source.MediaFormat; f != ra.Video && f != ra.Audio {
		return web.NewUserError("error.probe_failed", errors.Errorf("file info for media format %q", f))
	}
	j.Done()
	j.InProgress(s.t("job.probingMedia"))
	probeCtx, probeCancel := context.WithTimeout(ctx, fileInfoProbeTimeout)
	defer probeCancel()
	mp, raw, err := s.api.GetMediaProbeRaw(probeCtx, contentProbeURL(resp.ExportItems["download"].URL))
	if err != nil {
		return probeError(err)
	}
	j.Done()
	fi := buildFileInfo(mp)
	fi.Raw = rawProbeJSON(raw)
	return s.renderActionTemplate(j, c, fi, "file_info")
}

// probeError says whose failure a failed probe is:
//   - content-prober's 500 "probing failed" is the file's (error.probe_failed)
//     -- unless ffprobe was cut off (canceled, deadline), which says nothing
//     about the file;
//   - anything else is ours (error.upstream_unavailable): thp's 502 when
//     content-prober is down, thp's empty 500 when it cannot resolve the
//     source, a transport error. error.probe_failed would blame the file.
//
// Our own 60 s deadline (the swarm did not deliver the head in time) still
// reaches ErrorWrapperScript as a deadline through UserError's Unwrap, and
// becomes the no-peers "timeout" card with a retry
// (TestFileInfoProbeError).
func probeError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "probing failed") &&
		!strings.Contains(msg, "context canceled") && !strings.Contains(msg, "deadline exceeded") {
		return errors.Wrap(err, "failed to get probe data")
	}
	// UserError's Error() is its key, so the top-level "user error shown"
	// line would carry only error.upstream_unavailable: the status, body or
	// transport error that tells these cases apart is logged here, its
	// credentials redacted (an unparsable 200 quotes the probe body, whose
	// format.filename is the source URL) and then capped.
	msg = helpers.RedactURL(msg)
	if len(msg) > probeErrorLogMax {
		msg = msg[:probeErrorLogMax]
	}
	log.WithField("cause", msg).Warn("file info: media prober unavailable")
	return web.NewUserError("error.upstream_unavailable", errors.Wrap(err, "media prober unavailable"))
}

const probeErrorLogMax = 200

// FileInfo is what the "File info" card (action/file_info) shows of a
// file's probe: what the FILE declares, for someone about to download it and
// play it elsewhere (VLC). It is not the player's view: the player's
// "default" is the viewer's own pick or language match
// (handlers/action/helper.go GetAudioTracks), and its picker hides picture
// subtitles the browser cannot draw. Nothing of it is stored (docs/file_info.md).
type FileInfo struct {
	// Duration is the container's, as H:MM:SS; "" when the probe has none.
	Duration  string
	Video     []FileInfoVideo
	Audio     []FileInfoTrack
	Subtitles []FileInfoTrack
	// ClosedCaptions: a video stream carries CEA-608/708 captions -- no
	// subtitle stream, yet VLC shows them.
	ClosedCaptions bool
	// Raw is ffprobe's JSON for the card's raw view (rawProbeJSON).
	Raw string
}

type FileInfoVideo struct {
	Codec  string
	Width  int
	Height int
	// HDR is "HDR10" (PQ) or "HLG" from color_transfer; "" for SDR or
	// unknown. Dolby Vision needs side_data_list and is not read yet.
	HDR  string
	Mbps float64
	// Approx: the file records no bitrate for this stream; Mbps is the
	// container's less the other streams' (buildFileInfo).
	Approx bool
}

// BitrateLabel is the stream's bitrate printed as the transfer status prints
// a speed ("8.4", ru "8,4"); "" when the file records none.
func (v FileInfoVideo) BitrateLabel(lang string) string {
	q := statusview.Quantize(v.Mbps)
	if q == 0 {
		return ""
	}
	return statusview.FormatNumber(lang, q)
}

type FileInfoTrack struct {
	// Lang is the canonical tag ("en" for "eng"); "" when the file does not
	// say. Never guessed by us: an untagged track is not English. (The
	// Matroska spec defaults an absent Language element to "eng"; whether
	// ffprobe then reports "eng" is not checked.)
	Lang  string
	Title string
	Codec string
	// Channels is the audio channel layout ("5.1(side)"); audio only.
	Channels string
	// Picture: a picture-based subtitle (PGS, VobSub, DVB) -- an external
	// player shows it, the browser player does not.
	Picture bool
	Default bool
	Forced  bool
	SDH     bool
}

// pictureSubtitleCodecs is handlers/action's bitmapSubtitleCodecs (that
// package cannot be imported from here).
var pictureSubtitleCodecs = map[string]bool{
	"hdmv_pgs_subtitle": true,
	"dvd_subtitle":      true,
	"dvb_subtitle":      true,
	"xsub":              true,
}

var subtitleCodecNames = map[string]string{
	"subrip":            "SRT",
	"ass":               "ASS",
	"ssa":               "SSA",
	"hdmv_pgs_subtitle": "PGS",
	"dvd_subtitle":      "VobSub",
	"dvb_subtitle":      "DVB",
	"mov_text":          "TX3G",
	"webvtt":            "WebVTT",
}

// forcedTitleRe is handlers/action's regex of the same name: "forced" as a
// word of its own, underscores counting as separators.
var forcedTitleRe = regexp.MustCompile(`(?i)(^|[^a-z0-9])forced($|[^a-z0-9])`)

// sdhTitleRe: "SDH" or "CC" as a word of its own -- not a ".cc" domain. 27
// of 303 live probes (2026-10-18) named SDH in the title, 19 set the
// disposition flag.
var sdhTitleRe = regexp.MustCompile(`(?i)(^|[^a-z0-9.])(sdh|cc)($|[^a-z0-9])`)

// The same words denied: "Non-SDH", "No SDH", "not forced" name the track
// a viewer has to rule out. (The player's forcedTitleRe has no such guard.)
var (
	forcedDeniedRe = regexp.MustCompile(`(?i)(^|[^a-z0-9])(non|no|not)[\s_-]*forced($|[^a-z0-9])`)
	sdhDeniedRe    = regexp.MustCompile(`(?i)(^|[^a-z0-9])(non|no|not)[\s_-]*(sdh|cc)($|[^a-z0-9])`)
)

// titleSays: the title names the word and does not deny it.
func titleSays(title string, re, denied *regexp.Regexp) bool {
	return re.MatchString(title) && !denied.MatchString(title)
}

// buildFileInfo turns a probe into the card. The default rule is what a
// player with no preferred language does: the first track flagged default,
// and for audio, where something always plays, the first track when none is
// flagged. Matroska's FlagDefault defaults to 1, so "every track default" is
// common (5 of 51 multi-audio files, 2026-10-18): only the first wins. A
// subtitle nobody flagged is not shown by default, so it gets no fallback.
func buildFileInfo(mp *api.MediaProbe) *FileInfo {
	fi := &FileInfo{Duration: formatDuration(mp.Format.Duration)}
	audioDefault, subDefault := -1, -1
	for i, s := range mp.Streams {
		switch s.CodecType {
		case "video":
			if s.Disposition.AttachedPic == 1 {
				continue
			}
			if s.ClosedCaptions == 1 {
				fi.ClosedCaptions = true
			}
			fi.Video = append(fi.Video, FileInfoVideo{
				Codec:  strings.ToUpper(s.CodecName),
				Width:  s.Width,
				Height: s.Height,
				HDR:    hdrKind(s.ColorTransfer),
				Mbps:   statusview.BitsToMbps(float64(streamBitrate(mp, i))),
			})
		case "audio":
			if audioDefault < 0 && s.Disposition.Default == 1 {
				audioDefault = len(fi.Audio)
			}
			fi.Audio = append(fi.Audio, FileInfoTrack{
				Lang:     canonicalLang(s.Tags.Language),
				Title:    s.Tags.Title,
				Codec:    strings.ToUpper(s.CodecName),
				Channels: s.ChannelLayout,
			})
		case "subtitle":
			if subDefault < 0 && s.Disposition.Default == 1 {
				subDefault = len(fi.Subtitles)
			}
			codec, ok := subtitleCodecNames[s.CodecName]
			if !ok {
				codec = strings.ToUpper(s.CodecName)
			}
			fi.Subtitles = append(fi.Subtitles, FileInfoTrack{
				Lang:    canonicalLang(s.Tags.Language),
				Title:   s.Tags.Title,
				Codec:   codec,
				Picture: pictureSubtitleCodecs[s.CodecName],
				Forced:  s.Disposition.Forced == 1 || titleSays(s.Tags.Title, forcedTitleRe, forcedDeniedRe),
				SDH:     s.Disposition.HearingImpaired == 1 || titleSays(s.Tags.Title, sdhTitleRe, sdhDeniedRe),
			})
		}
	}
	if audioDefault < 0 && len(fi.Audio) > 0 {
		audioDefault = 0
	}
	if audioDefault >= 0 {
		fi.Audio[audioDefault].Default = true
	}
	if subDefault >= 0 {
		fi.Subtitles[subDefault].Default = true
	}
	// A Matroska video without mkvmerge's statistics tags records no bitrate
	// of its own. The container's is the whole file's; less what the other
	// streams record it is the video's, high by whatever an untagged one
	// carries -- hence approximate. One video stream only: two would share
	// the remainder in a way the file does not say.
	if len(fi.Video) == 1 && fi.Video[0].Mbps == 0 {
		if total := parseBitrate(mp.Format.BitRate); total > 0 {
			var rest int64
			for i, s := range mp.Streams {
				if s.CodecType != "video" {
					rest += streamBitrate(mp, i)
				}
			}
			if v := total - rest; v > 0 {
				fi.Video[0].Mbps = statusview.BitsToMbps(float64(v))
				fi.Video[0].Approx = true
			}
		}
	}
	return fi
}

// rawProbeLimit caps the raw view: the card is replayed from the job's
// storage and its body logged with the job (services/job logToLogger).
const rawProbeLimit = 32 << 10

// rawProbeJSON is the probe as ffprobe printed it, for the card's raw view:
// indented, format.filename -- the ~cp source URL, credentials and all --
// dropped (the whole format when it is not an object), anything else that
// names a credential redacted, capped. "" when the body is not a JSON object.
func rawProbeJSON(raw []byte) string {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return ""
	}
	if f, ok := top["format"]; ok {
		var format map[string]json.RawMessage
		if err := json.Unmarshal(f, &format); err != nil {
			delete(top, "format")
		} else {
			delete(format, "filename")
			b, err := json.Marshal(format)
			if err != nil {
				return ""
			}
			top["format"] = b
		}
	}
	b, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return ""
	}
	out := helpers.RedactURL(string(b))
	if len(out) > rawProbeLimit {
		out = strings.ToValidUTF8(out[:rawProbeLimit], "") + "\n…"
	}
	return out
}

// canonicalLang is "en" for "eng" (the canonizeSrcLangs rule of
// handlers/action), "" for a track that does not say. A tag x/text cannot
// read is kept as written: it is still what the file says.
func canonicalLang(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" || strings.EqualFold(tag, "und") {
		return ""
	}
	t, err := language.Parse(tag)
	if err != nil {
		return strings.ToLower(tag)
	}
	if s := t.String(); s != "und" {
		return s
	}
	return ""
}

func hdrKind(colorTransfer string) string {
	switch colorTransfer {
	case "smpte2084":
		return "HDR10"
	case "arib-std-b67":
		return "HLG"
	}
	return ""
}

// formatDuration prints ffprobe's seconds ("6723.4") as H:MM:SS ("1:52:03").
func formatDuration(sec string) string {
	f, err := strconv.ParseFloat(sec, 64)
	if err != nil || !(f > 0) || math.IsInf(f, 0) {
		return ""
	}
	n := int64(math.Round(f))
	return fmt.Sprintf("%d:%02d:%02d", n/3600, n/60%60, n%60)
}
