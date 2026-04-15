package scripts

import (
	"bytes"
	"context"
	"io"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"

	ra "github.com/webtor-io/rest-api/services"
)

type LoadArgs struct {
	Query       string
	File        []byte
	HintVideoID string
}

type LoadScript struct {
	api  *api.Api
	i18n *i18n.Service
	args *LoadArgs
	c    *web.Context
}

func (s *LoadScript) t(key string) string {
	return i18n.TranslateWithLocalizer(s.i18n.Localizer(s.c.Lang), key)
}

func NewLoadScript(api *api.Api, i18nSvc *i18n.Service, c *web.Context, args *LoadArgs) *LoadScript {
	return &LoadScript{
		api:  api,
		i18n: i18nSvc,
		c:    c,
		args: args,
	}
}

func (s *LoadScript) Run(ctx context.Context, j *job.Job) (err error) {
	var res *ra.ResourceResponse
	if s.args.File != nil {
		res, err = s.storeFile(ctx, j, s.args.File)
	} else if s.args.Query != "" {
		res, err = s.storeQuery(ctx, j, s.args.Query)
	}
	if err != nil {
		return err
	}
	if res == nil {
		return errors.New("resource not found")
	}
	j.Context = context.WithValue(j.Context, "respID", res.ID)
	return
}

func (s *LoadScript) storeFile(ctx context.Context, j *job.Job, file []byte) (res *ra.ResourceResponse, err error) {
	j.InProgress(s.t("job.uploadingFile"))
	apiCtx, apiCancel := context.WithTimeout(ctx, 60*time.Second)
	defer apiCancel()
	res, err = s.api.StoreResource(apiCtx, s.c.ApiClaims, file)
	if err != nil {
		return nil, errors.Wrap(err, "failed to upload file")
	}
	j.Done()
	return
}

func (s *LoadScript) storeQuery(ctx context.Context, j *job.Job, query string) (res *ra.ResourceResponse, err error) {
	j.InProgress(s.t("job.checkingMagnet"))
	sha1Hash := common.SHA1R.Find([]byte(query))
	if sha1Hash == nil {
		return nil, errors.Wrap(err, "wrong resource provided")
	}
	hash := strings.ToLower(string(sha1Hash))
	if !strings.HasPrefix(query, "magnet:") {
		query = "magnet:?xt=urn:btih:" + hash
	}
	apiCtx, apiCancel := context.WithTimeout(ctx, 60*time.Second)
	defer apiCancel()
	res, err = s.api.GetResource(apiCtx, s.c.ApiClaims, hash)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load resource by magnet")
	}
	if res != nil {
		j.Done()
		return
	}
	j.Done()
	j.Info(s.t("job.magnetizing.info"))
	j.InProgress(s.t("job.magnetizing"))
	magnetizeCtx, magnetizeCancel := context.WithTimeout(ctx, 60*time.Second)
	defer magnetizeCancel()
	res, err = s.api.StoreResource(magnetizeCtx, s.c.ApiClaims, []byte(query))
	if err != nil || res == nil {
		return nil, errors.Wrap(err, "failed to magnetize")
	}
	j.Done()
	return
}

func Load(api *api.Api, i18nSvc *i18n.Service, c *web.Context, args *LoadArgs) (r job.Runnable, hash string, err error) {
	if args.Query != "" {
		sha1Hash := common.SHA1R.Find([]byte(args.Query))
		if sha1Hash == nil {
			return nil, "", errors.Errorf("wrong resource provided query=%v", args.Query)
		}
		hash = strings.ToLower(string(sha1Hash))
	} else if args.File != nil {
		b := io.NopCloser(bytes.NewReader(args.File))
		mi, err := metainfo.Load(b)
		if err != nil {
			return nil, "", err
		}
		hash = mi.HashInfoBytes().HexString()
	}
	r = NewLoadScript(api, i18nSvc, c, args)
	return
}
