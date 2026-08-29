package scripts

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/egress"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/template"

	ra "github.com/webtor-io/rest-api/services"
)

var (
	sampleReg = regexp.MustCompile("/sample/i")
)

type EmbedScript struct {
	api      *api.Api
	i18n     *i18n.Service
	enricher *enrich.Enricher
	settings *models.EmbedSettings
	file     string
	tb       template.Builder[*web.Context]
	c        *web.Context
	cl       *http.Client
	dsd      *embed.DomainSettingsData
	warmup   WarmupSettings
}

type EmbedAdsData struct {
	DomainSettings *embed.DomainSettingsData
}

func NewEmbedScript(tb template.Builder[*web.Context], cl *http.Client, c *web.Context, api *api.Api, i18nSvc *i18n.Service, enricher *enrich.Enricher, settings *models.EmbedSettings, file string, dsd *embed.DomainSettingsData, warmup WarmupSettings) *EmbedScript {
	return &EmbedScript{
		c:        c,
		api:      api,
		i18n:     i18nSvc,
		enricher: enricher,
		settings: settings,
		file:     file,
		tb:       tb,
		cl:       cl,
		dsd:      dsd,
		warmup:   warmup,
	}
}

func (s *EmbedScript) t(key string) string {
	return i18n.TranslateWithLocalizer(s.i18n.Localizer(s.c.Lang), key)
}

// maxEmbedTorrentBytes bounds the .torrent fetched below. Same figure the
// JSON API uses for an uploaded torrent (handlers/api/resource.go).
const maxEmbedTorrentBytes = 8 << 20

// embedTorrentClient fetches settings.TorrentURL.
//
// It is deliberately not the process-wide http.DefaultClient: this URL comes
// from the embed POST body, which is anonymous, so the fetch is an attacker's
// choice of destination. The dialer refuses private and link-local addresses
// (cluster services, the metadata endpoint), redirects are not followed so a
// public host cannot bounce the request inward, and there is a timeout —
// DefaultClient has none, and a server that answers one byte per minute would
// otherwise hold the goroutine forever.
var embedTorrentClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			Control:   egress.DialControl(false),
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func (s *EmbedScript) makeLoadArgs(settings *models.EmbedSettings) (*LoadArgs, error) {
	la := &LoadArgs{}
	if settings.TorrentURL != "" {
		u, err := url.Parse(settings.TorrentURL)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse torrent url")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, errors.Errorf("torrent url must be http or https, got %q", u.Scheme)
		}
		resp, err := embedTorrentClient.Get(settings.TorrentURL)
		if err != nil {
			return nil, err
		}
		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return nil, errors.Errorf("torrent url returned HTTP %d", resp.StatusCode)
		}
		// Bounded: an unbounded ReadAll here let one anonymous request point
		// the pod at an endless response and grow a single slice until the
		// memory limit tripped.
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxEmbedTorrentBytes+1))
		if err != nil {
			return nil, err
		}
		if len(body) > maxEmbedTorrentBytes {
			return nil, errors.Errorf("torrent is larger than %d bytes", maxEmbedTorrentBytes)
		}
		la.File = body
	} else if settings.Magnet != "" {
		la.Query = settings.Magnet
	}
	return la, nil
}

func (s *EmbedScript) Run(ctx context.Context, j *job.Job) (err error) {
	if s.dsd.Forbidden {
		forbiddenTemplate := "embed/forbidden"
		tpl := s.tb.Build(forbiddenTemplate)
		str, err := tpl.ToString(s.c)
		if err != nil {
			return err
		}
		j.RenderTemplate("rendering forbidden", forbiddenTemplate, strings.TrimSpace(str))
		return err
	}
	if s.dsd.Unauthorized {
		unauthorizedTemplate := "embed/unauthorized"
		tpl := s.tb.Build(unauthorizedTemplate)
		str, err := tpl.ToString(s.c)
		if err != nil {
			return err
		}
		j.RenderTemplate("rendering not authorized", unauthorizedTemplate, strings.TrimSpace(str))
		return err
	}
	args, err := s.makeLoadArgs(s.settings)
	if err != nil {
		return
	}
	ls, _, err := Load(s.api, s.i18n, s.c, args)
	if err != nil {
		return err
	}
	err = ls.Run(ctx, j)
	if err != nil {
		return err
	}
	id := j.Context.Value("respID").(string)
	i, err := s.getBestItem(ctx, j, id, s.settings)
	if err != nil {
		return err
	}
	var action string
	if i.MediaFormat == ra.Video {
		action = "stream-video"
	} else if i.MediaFormat == ra.Audio {
		action = "stream-audio"
	}
	err = s.renderAds(j, s.c, s.dsd)
	if err != nil {
		return err
	}
	vsud := models.NewVideoStreamUserData(id, i.ID, &s.settings.StreamSettings)
	// Pass nil for user-subtitles and thumbnails: the embed flow omits
	// the My Subtitles tab (no account context on third-party sites)
	// and the inline share button isn't surfaced in embed players, so
	// neither service is needed. Enricher is plumbed through so the
	// player overlay's title respects IMDb-matched metadata even on
	// embed pages — falls back to the file basename when nil.
	as, _ := Action(s.tb, s.api, s.i18n, nil, nil, s.enricher, s.c, id, i.ID, action, &s.settings.StreamSettings, s.dsd, vsud, s.warmup, GraceSettings{}, false, "", "", nil)
	err = as.Run(ctx, j)
	if err != nil {
		return err
	}
	return
}

func (s *EmbedScript) getBestItem(ctx context.Context, j *job.Job, id string, settings *models.EmbedSettings) (i *ra.ListItem, err error) {
	j.InProgress(s.t("job.searchingContent"))
	apiCtx, apiCancel := context.WithTimeout(ctx, 30*time.Second)
	defer apiCancel()
	pwd := settings.PWD
	file := settings.File
	if settings.Path != "" {
		parts := strings.Split(settings.Path, "/")
		file = parts[len(parts)-1]
		pwd = strings.Join(parts[:len(parts)-1], "/")
	}
	l, err := s.api.ListResourceContentCached(apiCtx, s.c.ApiClaims, id, &api.ListResourceContentArgs{
		Path:   pwd,
		Output: api.OutputTree,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list resource content")
	}
	if len(l.Items) == 1 && l.Items[0].Type == ra.ListTypeDirectory {
		apiCtx2, apiCancel2 := context.WithTimeout(ctx, 30*time.Second)
		defer apiCancel2()
		l, err = s.api.ListResourceContentCached(apiCtx2, s.c.ApiClaims, id, &api.ListResourceContentArgs{
			Path:   l.Items[0].PathStr,
			Output: api.OutputTree,
		})
		if err != nil {
			return nil, errors.Wrap(err, "failed to list resource content")
		}
	}
	if file != "" {
		for _, f := range l.Items {
			if f.Name == file {
				i = &f
				break
			}
		}
	} else {
		i = s.findBestItem(l)
	}
	if i == nil {
		// errors.Wrap(nil, ...) returns nil — every err != nil branch above
		// already returned, so err is guaranteed nil here. Caller saw
		// (nil, nil), dereferenced i.MediaFormat, panicked. See embed.go:124.
		return nil, errors.New("failed to find stream content")
	}
	j.Done()
	return
}

func (s *EmbedScript) findBestItem(l *ra.ListResponse) *ra.ListItem {
	for _, v := range l.Items {
		if v.MediaFormat == ra.Video && !sampleReg.MatchString(v.Name) {
			return &v
		}
	}
	for _, v := range l.Items {
		if v.MediaFormat == ra.Audio && !sampleReg.MatchString(v.Name) {
			return &v
		}
	}
	for _, v := range l.Items {
		if v.Type == ra.ListTypeFile {
			return &v
		}
	}
	return nil
}

func (s *EmbedScript) renderAds(j *job.Job, c *web.Context, dsd *embed.DomainSettingsData) (err error) {
	if !dsd.Ads {
		return
	}
	adsTemplate := "embed/ads"
	tpl := s.tb.Build(adsTemplate)
	str, err := tpl.ToString(c.WithData(&EmbedAdsData{
		DomainSettings: dsd,
	}))
	if err != nil {
		return err
	}
	j.RenderTemplate("rendering ads", adsTemplate, strings.TrimSpace(str))
	return
}

func Embed(tb template.Builder[*web.Context], cl *http.Client, c *web.Context, api *api.Api, i18nSvc *i18n.Service, enricher *enrich.Enricher, settings *models.EmbedSettings, file string, dsd *embed.DomainSettingsData, warmup WarmupSettings) (r job.Runnable, hash string, err error) {
	geoHash := ""
	if c.Geo != nil {
		geoHash = c.Geo.Country
	}
	hourKey := time.Now().UTC().Format("2006010215")
	// The visitor's session identity is part of the key for the same reason
	// it is in the action key: without it every visitor of a given embed
	// shared one rendered player for the whole hour, and that HTML carries a
	// signed token minted from the first visitor's claims. This key was the
	// coarser of the two — it had no visitor component at all, and an hour
	// bucket rather than ten minutes.
	sessionKey := ""
	if c.ApiClaims != nil {
		sessionKey = c.ApiClaims.SessionID
	}
	hash = fmt.Sprintf("%x", sha1.Sum([]byte(geoHash+"/"+fmt.Sprintf("%+v", dsd)+"/"+c.ApiClaims.Role+"/"+fmt.Sprintf("%+v", settings)+"/"+hourKey+"/"+c.Lang+"/"+sessionKey)))
	r = NewEmbedScript(tb, cl, c, api, i18nSvc, enricher, settings, file, dsd, warmup)
	return
}
