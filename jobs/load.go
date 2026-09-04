package j

import (
	"context"
	"crypto/sha1"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/webtor-io/web-ui/services/job"
)

func (s *Jobs) Load(c *web.Context, args *scripts.LoadArgs) (j *job.Job, err error) {
	ls, hash, err := scripts.Load(s.api, s.i18n, c, args)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	id := fmt.Sprintf("%x", sha1.Sum([]byte(hash+"/"+c.Lang)))
	j = s.q.GetOrCreate("load").Enqueue(ctx, cancel, id, job.NewScript(func(j *job.Job) (err error) {
		err = ls.Run(ctx, j)
		if me, ok := err.(*scripts.MagnetError); ok {
			// A magnet that did not become a torrent gets a card, not a red
			// line: what happened, how long we looked, and a retry that
			// waits ten minutes — the ordinary minute is rarely the reason
			// (measured 2026-09-04: 5 of 60 resolve on a warm client), but
			// the user decides. "user error shown" keeps the err_key metric.
			return s.renderMagnetError(c, j, args, me)
		}
		if err != nil {
			return
		}
		rID := j.Context.Value("respID").(string)
		if s.enricher.HasMappers() {
			j.InProgress(s.T(c, "job.enrichingContent"))
			enrichErr := s.enricher.Enrich(ctx, rID, c.ApiClaims, false, args.HintVideoID)
			if enrichErr != nil {
				j.Warn(enrichErr)
			} else {
				j.Done()
			}
		}
		j.Redirect(web.LangURL(c.Lang, "/"+rID), s.T(c, "job.redirecting"))
		return
	}), false, s.errorFormatter(c))
	return
}

// MagnetErrorData feeds templates/views/load/errors/magnet.html.
type MagnetErrorData struct {
	Kind        string // dead | invalid
	WaitedSec   int
	Long        bool // the ten-minute attempt is what failed
	Query       string
	HintVideoID string
}

func (s *Jobs) renderMagnetError(c *web.Context, j *job.Job, args *scripts.LoadArgs, me *scripts.MagnetError) error {
	key := "error.magnet_no_metadata"
	if me.Kind == "invalid" {
		key = "error.magnet_invalid"
	}
	log.WithError(me).WithField("err_key", key).WithField("long", me.Long).WithField("waited_s", int(me.Waited.Seconds())).Info("user error shown")
	data := &MagnetErrorData{
		Kind:        me.Kind,
		WaitedSec:   int(me.Waited.Round(time.Second).Seconds()),
		Long:        me.Long,
		Query:       args.Query,
		HintVideoID: args.HintVideoID,
	}
	tpl := s.tb.Build("load/errors/magnet").WithLayoutBody(`{{ template "main" . }}`)
	str, terr := tpl.ToString(c.WithData(data))
	if terr != nil {
		return terr
	}
	j.Fail()
	j.Custom("load/errors/magnet", strings.TrimSpace(str))
	return nil
}
