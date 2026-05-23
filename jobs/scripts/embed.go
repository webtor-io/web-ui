package scripts

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/embed"
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

func NewEmbedScript(tb template.Builder[*web.Context], cl *http.Client, c *web.Context, api *api.Api, i18nSvc *i18n.Service, settings *models.EmbedSettings, file string, dsd *embed.DomainSettingsData, warmup WarmupSettings) *EmbedScript {
	return &EmbedScript{
		c:        c,
		api:      api,
		i18n:     i18nSvc,
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

func (s *EmbedScript) makeLoadArgs(settings *models.EmbedSettings) (*LoadArgs, error) {
	la := &LoadArgs{}
	if settings.TorrentURL != "" {
		resp, err := s.cl.Get(settings.TorrentURL)
		if err != nil {
			return nil, err
		}
		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp.Body)
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
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
	// Pass nil for user-subtitles and thumbnails: the embed flow
	// intentionally omits the My Subtitles tab (no account context on
	// third-party sites) and embed pages don't expose share previews,
	// so neither service is needed.
	as, _ := Action(s.tb, s.api, s.i18n, nil, nil, s.c, id, i.ID, action, &s.settings.StreamSettings, s.dsd, vsud, s.warmup, GraceSettings{}, false, "")
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

func Embed(tb template.Builder[*web.Context], cl *http.Client, c *web.Context, api *api.Api, i18nSvc *i18n.Service, settings *models.EmbedSettings, file string, dsd *embed.DomainSettingsData, warmup WarmupSettings) (r job.Runnable, hash string, err error) {
	geoHash := ""
	if c.Geo != nil {
		geoHash = c.Geo.Country
	}
	hourKey := time.Now().UTC().Format("2006010215")
	hash = fmt.Sprintf("%x", sha1.Sum([]byte(geoHash+"/"+fmt.Sprintf("%+v", dsd)+"/"+c.ApiClaims.Role+"/"+fmt.Sprintf("%+v", settings)+"/"+hourKey+"/"+c.Lang)))
	r = NewEmbedScript(tb, cl, c, api, i18nSvc, settings, file, dsd, warmup)
	return
}
