package script

import (
	"context"
	"crypto/sha1"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"

	ra "github.com/webtor-io/rest-api/services"
)

type LoadArgs struct {
	Query string
	File  []byte
}

type LoadScript struct {
	api  *api.Api
	args *LoadArgs
	c    *web.Context
}

func NewLoadScript(api *api.Api, c *web.Context, args *LoadArgs) *LoadScript {
	return &LoadScript{
		api:  api,
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
	j.InProgress("uploading file")
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
	j.InProgress("checking magnet")
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
	j.Info("sadly, we don't have torrent, so we have to magnetize it from peers")
	j.InProgress("magnetizing")
	magnetizeCtx, magnetizeCancel := context.WithTimeout(ctx, 60*time.Second)
	defer magnetizeCancel()
	res, err = s.api.StoreResource(magnetizeCtx, s.c.ApiClaims, []byte(query))
	if err != nil || res == nil {
		return nil, errors.Wrap(err, "failed to magnetize")
	}
	j.Done()
	return
}

func Load(api *api.Api, c *web.Context, args *LoadArgs) (r job.Runnable, hash string, err error) {
	h := sha1.New()
	h.Write(args.File)
	h.Write([]byte(args.Query))
	hash = fmt.Sprintf("%x", h.Sum(nil))
	r = NewLoadScript(api, c, args)
	return
}
