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
	// A load job is keyed by hash+lang and its log is kept in Redis, so a
	// second visit replays the first run — including a failure. The
	// dead-magnet retry must run again, not replay the card it was clicked
	// on; the dev playback likewise. So must an upload: a magnet card asks
	// for the .torrent, which has the same infohash. Purge drops the stored
	// log first.
	purge := args.MagnetWait > 0 || args.Debug != "" || args.File != nil
	j = s.q.GetOrCreate("load").Enqueue(ctx, cancel, id, job.NewScript(func(j *job.Job) (err error) {
		err = ls.Run(ctx, j)
		if me, ok := err.(*scripts.MagnetError); ok {
			// A magnet that did not become a torrent gets a card, not a red
			// line: what happened, how long we looked, and a retry that
			// waits ten minutes — the ordinary minute is rarely the reason
			// (measured 2026-09-04: 5 of 60 resolve on a warm client), but
			// the user decides. "user error shown" keeps the err_key metric.
			return s.renderMagnetError(c, j, args, me, hash)
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
	}), purge, s.errorFormatter(c))
	return
}

// MagnetErrorData feeds templates/views/load/errors/magnet.html.
type MagnetErrorData struct {
	Kind      string // dead | invalid | private
	WaitedSec int
	Long      bool // the ten-minute attempt is what failed
	// Magnet is what the retry submits when the browser cannot give it the
	// query it submitted: the infohash alone, never the query. The card is
	// kept in the job store and replayed to whoever loads the same torrent
	// in the same language, through a log endpoint that needs no account,
	// and a magnet's tr= can carry a private tracker's passkey. The retry
	// takes the query from the log host, which was rendered for this
	// browser only (partials/load/progress.html, progressLog.js).
	Magnet      string
	HintVideoID string
}

func newMagnetErrorData(args *scripts.LoadArgs, me *scripts.MagnetError, hash string) *MagnetErrorData {
	return &MagnetErrorData{
		Kind:        me.Kind,
		WaitedSec:   int(me.Waited.Round(time.Second).Seconds()),
		Long:        me.Long,
		Magnet:      "magnet:?xt=urn:btih:" + hash,
		HintVideoID: args.HintVideoID,
	}
}

// magnetErrKey is the err_key a magnet card is counted under in "user error
// shown": the key the same failure reads as a plain line.
func magnetErrKey(me *scripts.MagnetError) string {
	switch me.Kind {
	case "invalid":
		return "error.magnet_invalid"
	case "private":
		return "error.private_magnet"
	}
	return "error.magnet_no_metadata"
}

func (s *Jobs) renderMagnetError(c *web.Context, j *job.Job, args *scripts.LoadArgs, me *scripts.MagnetError, hash string) error {
	key := magnetErrKey(me)
	if me.Kind == "private" {
		// The refusal answers the link that was submitted (no tracker in
		// it), not the torrent. The stored card goes when this run retires,
		// so a later magnet with the tracker runs instead of replaying it.
		j.DoNotCache()
	}
	log.WithError(me).WithField("err_key", key).WithField("long", me.Long).WithField("waited_s", int(me.Waited.Seconds())).Info("user error shown")
	data := newMagnetErrorData(args, me, hash)
	tpl := s.tb.Build("load/errors/magnet").WithLayoutBody(`{{ template "main" . }}`)
	str, terr := tpl.ToString(c.WithData(data))
	if terr != nil {
		return terr
	}
	j.Fail()
	j.Custom("load/errors/magnet", strings.TrimSpace(str))
	return nil
}
