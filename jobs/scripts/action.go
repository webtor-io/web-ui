package scripts

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/dustin/go-humanize"
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

type TorrentDownload struct {
	Data     []byte
	Infohash string
	Name     string
	Size     int
}

func (s *ActionScript) streamContent(ctx context.Context, j *job.Job, c *web.Context, resourceID string, itemID string, template string, settings *models.StreamSettings, vsud *models.VideoStreamUserData, dsd *embed.DomainSettingsData) (err error) {
	sc := &StreamContent{
		Settings:       settings,
		ExternalData:   &models.ExternalData{},
		DomainSettings: dsd,
	}
	j.InProgress("retrieving resource data")
	resCtx, resCancel := context.WithTimeout(ctx, 10*time.Second)
	defer resCancel()
	resourceResponse, err := s.api.GetResource(resCtx, c.ApiClaims, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to retrieve resource")
	}
	j.Done()
	sc.Resource = resourceResponse
	j.InProgress("retrieving stream url")
	exportCtx, exportCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer exportCancel()
	exportResponse, err := s.api.ExportResourceContent(exportCtx, c.ApiClaims, resourceID, itemID, settings.ImdbID)
	if err != nil {
		return errors.Wrap(err, "failed to retrieve stream url")
	}
	j.Done()
	sc.ExportTag = exportResponse.ExportItems["stream"].Tag
	sc.Item = &exportResponse.Source
	se := exportResponse.ExportItems["stream"]

	if se.Meta.Transcode {
		if !se.Meta.TranscodeCache {
			if !se.ExportMetaItem.Meta.Cache {
				if err = s.warmUp(ctx, j, "warming up torrent client", exportResponse.ExportItems["download"].URL, exportResponse.ExportItems["torrent_client_stat"].URL, int(exportResponse.Source.Size), 1_000_000, 500_000, "file", true); err != nil {
					return
				}
			}
			if err = s.warmUp(ctx, j, "warming up transcoder", exportResponse.ExportItems["stream"].URL, exportResponse.ExportItems["torrent_client_stat"].URL, 0, -1, -1, "stream", false); err != nil {
				return
			}
		}
		j.InProgress("probing content media info")
		mpCtx, mpCancel := context.WithTimeout(ctx, 1*time.Minute)
		defer mpCancel()
		mp, err := s.api.GetMediaProbe(mpCtx, exportResponse.ExportItems["media_probe"].URL)
		if err != nil {
			return errors.Wrap(err, "failed to get probe data")
		}
		sc.MediaProbe = mp
		log.Infof("got media probe %+v", mp)
		j.Done()
	} else {
		if !se.ExportMetaItem.Meta.Cache {
			if err = s.warmUp(ctx, j, "warming up torrent client", exportResponse.ExportItems["download"].URL, exportResponse.ExportItems["torrent_client_stat"].URL, int(exportResponse.Source.Size), 1_000_000, 500_000, "file", true); err != nil {
				return
			}
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
	exportCtx, exportCancel := context.WithTimeout(ctx, 10*time.Second)
	defer exportCancel()
	resp, err := s.api.ExportResourceContent(exportCtx, c.ApiClaims, resourceID, itemID, "")
	if err != nil {
		return errors.Wrap(err, "failed to retrieve download link")
	}
	j.Done()
	de := resp.ExportItems["download"]
	//url := de.URL
	if !de.ExportMetaItem.Meta.Cache {
		if err := s.warmUp(ctx, j, "warming up torrent client", resp.ExportItems["download"].URL, resp.ExportItems["torrent_client_stat"].URL, int(resp.Source.Size), 1_000_000, 0, "", true); err != nil {
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

func (s *ActionScript) downloadTorrent(ctx context.Context, j *job.Job, c *web.Context, resourceID string) (err error) {
	j.InProgress("retrieving torrent")
	apiCtx, apiCancel := context.WithTimeout(ctx, 10*time.Second)
	defer apiCancel()
	resp, err := s.api.GetTorrent(apiCtx, c.ApiClaims, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to retrieve for 30 seconds")
	}
	defer func(resp io.ReadCloser) {
		_ = resp.Close()
	}(resp)
	torrent, err := io.ReadAll(resp)
	if err != nil {
		return errors.Wrap(err, "failed to read torrent")
	}
	mi, err := metainfo.Load(bytes.NewBuffer(torrent))
	if err != nil {
		return errors.Wrap(err, "failed to load torrent metainfo")
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return errors.Wrap(err, "failed to unmarshal torrent metainfo")
	}
	j.DoneWithMessage("success! download should start right now!")
	tpl := s.tb.Build("action/download_torrent").WithLayoutBody(`{{ template "main" . }}`)
	name := info.Name
	if name == "" {
		name = resourceID
	}
	str, err := tpl.ToString(c.WithData(&TorrentDownload{
		Data:     torrent,
		Infohash: resourceID,
		Name:     name + ".torrent",
		Size:     len(torrent),
	}))
	if err != nil {
		return err
	}
	j.Custom("action/download_torrent", strings.TrimSpace(str))
	return nil
}

func (s *ActionScript) warmUp(ctx context.Context, j *job.Job, m string, u string, su string, size int, limitStart int, limitEnd int, tagSuff string, useStatus bool) (err error) {
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
		j.InProgress(fmt.Sprintf("%v, downloading %v", m, humanize.Bytes(uint64(limitStart+limitEnd))))
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
		return errors.Wrap(err, "failed to start download")
	}
	defer func(b io.ReadCloser) {
		_ = b.Close()
	}(b)

	_, err = io.Copy(io.Discard, b)

	if limitEnd > 0 {
		b2, err := s.api.DownloadWithRange(warmupCtx, u, size-limitEnd, -1)
		if err != nil {
			return errors.Wrap(err, "failed to start download")
		}
		defer func(b2 io.ReadCloser) {
			_ = b2.Close()
		}(b2)
		_, err = io.Copy(io.Discard, b2)
	}
	if errors.Is(errors.Cause(err), context.DeadlineExceeded) {
		return errors.Wrap(err, fmt.Sprintf("failed to download within %v minutes", s.warmupTimeoutMin))
	} else if err != nil {
		return errors.Wrap(err, "failed to download")
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
	case "download-torrent":
		return s.downloadTorrent(ctx, j, s.c, s.resourceId)
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
	id = fmt.Sprintf("%x", sha1.Sum([]byte(resourceID+"/"+itemID+"/"+action+"/"+c.ApiClaims.Role+"/"+settingsID+"/"+vsudID)))
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
