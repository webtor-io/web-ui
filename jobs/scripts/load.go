package scripts

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/private_torrent"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/job"

	ra "github.com/webtor-io/rest-api/services"
)

type LoadArgs struct {
	Query       string
	File        []byte
	HintVideoID string
	// MagnetWait is how long a magnet may take to resolve; zero means the
	// ordinary MagnetWaitDefault. The dead-magnet card offers MagnetWaitLong.
	MagnetWait time.Duration
	// Debug is a dev-only switch (see CLAUDE.md, Debugging): "magnet_dead",
	// "magnet_dead_long", "magnet_invalid", "magnet_private" play the failure
	// without a network.
	Debug string
	// Source is the door the request came in by, "site" or "embed". For a
	// File it labels the private-torrent count (services/private_torrent).
	// For a Query from an embed, a known private hash is never stored again
	// with the magnet's tr=: an embed has no uid, so rest-api could only
	// refuse it with "sign in".
	Source string
}

const (
	// MagnetWaitDefault is the first attempt: measured 2026-09-04, a magnet
	// that has not resolved in a minute almost never does later (5 of 60 on
	// a warm client), so a longer default only makes everyone wait.
	MagnetWaitDefault = 60 * time.Second
	// MagnetWaitLong is what "try again for 10 minutes" asks for. rest-api and
	// magnet2torrent cap a resolution at the same ten minutes.
	MagnetWaitLong = 10 * time.Minute
)

// MagnetError is a magnet that did not become a torrent. Kind is "dead" (no
// peer had the metadata within Waited), "invalid" (the link itself is
// broken) or "private" (a magnet without a tracker resolved to a private
// torrent rest-api has not stored: api.ErrPrivateMagnet). The Jobs layer
// renders it as a card; only "dead" has a retry.
type MagnetError struct {
	Kind   string
	Waited time.Duration
	Long   bool
	Cause  error
}

func (e *MagnetError) Error() string {
	if e.Cause != nil {
		return "failed to magnetize: " + e.Cause.Error()
	}
	return "failed to magnetize: " + e.Kind
}

func (e *MagnetError) Unwrap() error { return e.Cause }

type LoadScript struct {
	api  *api.Api
	i18n *i18n.Service
	args *LoadArgs
	c    *web.Context
}

func (s *LoadScript) t(key string) string {
	return i18n.TranslateWithLocalizer(s.i18n.Localizer(s.c.Lang), key)
}

func (s *LoadScript) tp(key string, data map[string]any) string {
	return i18n.TranslateWithLocalizerData(s.i18n.Localizer(s.c.Lang), key, data)
}

// magnetCountdown is the status line while a magnet resolves: seconds left
// below a minute ("43 s", the warm-up's own line), m:ss above it — the same
// shape as buffering's percent, and it moves every second so a silent wait
// never reads as stuck.
func magnetCountdown(tp func(string, map[string]any) string, left time.Duration) string {
	if left <= 0 {
		return ""
	}
	// Its own short key: the warm-up's countdown says "looking for peers ·
	// 43 s left" since 2026-10-14, and the magnet line reads "Getting the
	// torrent from peers   43 s". Rounded up in both forms, so 60 is "1:00"
	// and never shown twice.
	sec := int(math.Ceil(left.Seconds()))
	if sec < 60 {
		return tp("job.magnetCountdown", map[string]any{"Seconds": sec})
	}
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

// countdown redraws the job status line every second until ctx ends or the
// deadline passes; stop() ends it early.
func countdown(ctx context.Context, j *job.Job, tp func(string, map[string]any) string, until time.Time) (stop func()) {
	cctx, cancel := context.WithCancel(ctx)
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			if line := magnetCountdown(tp, time.Until(until)); line != "" {
				j.StatusUpdate(line)
			}
			select {
			case <-cctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	return cancel
}

func NewLoadScript(api *api.Api, i18nSvc *i18n.Service, c *web.Context, args *LoadArgs) *LoadScript {
	return &LoadScript{
		api:  api,
		i18n: i18nSvc,
		c:    c,
		args: args,
	}
}

// Run puts the resource id into the job's context as "respID", and as
// "respPrivate" whether the lookup found the hash private (false for a file).
func (s *LoadScript) Run(ctx context.Context, j *job.Job) (err error) {
	var res *api.Resource
	if s.args.File != nil {
		var r *ra.ResourceResponse
		if r, err = s.storeFile(ctx, j, s.args.File); r != nil {
			res = &api.Resource{ResourceResponse: *r}
		}
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
	j.Context = context.WithValue(j.Context, "respPrivate", res.Private)
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

// storeQuery returns the resource of the query's hash. The lookup that
// decides "new or known" is the shared run's, so it reads as internal. A new
// hash, or a known private one asked with the user's own tracker (tr=), is
// stored as the user: that is how the tracker reaches rest-api. Not from an
// embed: it has no uid, and the lookup already says private, which the embed
// renders itself. A known hash returns the lookup's answer, private flag
// included, even after the store.
func (s *LoadScript) storeQuery(ctx context.Context, j *job.Job, query string) (*api.Resource, error) {
	j.InProgress(s.t("job.checkingMagnet"))
	hash, magnet, err := common.ResolveQueryHash(query)
	if err != nil {
		return nil, errors.Wrap(err, "wrong resource provided")
	}
	query = magnet
	apiCtx, apiCancel := context.WithTimeout(ctx, 60*time.Second)
	defer apiCancel()
	known, err := s.api.LookupResource(apiCtx, api.InternalClaims(), hash)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load resource by magnet")
	}
	j.Done()
	if known != nil && !(known.Private && s.args.Source != "embed" && common.QueryHasTrackers(magnet)) {
		return known, nil
	}
	j.Info(s.t("job.magnetizing.info"))
	j.InProgress(s.t("job.magnetizing"))
	wait := s.args.MagnetWait
	if wait <= 0 {
		wait = MagnetWaitDefault
	}
	long := wait > MagnetWaitDefault
	if s.args.Debug != "" && gin.Mode() != gin.ReleaseMode {
		// Dev-only: play the wait and the failure without a network.
		return nil, s.debugMagnet(ctx, j, s.args.Debug)
	}
	magnetizeCtx, magnetizeCancel := context.WithTimeout(ctx, wait)
	defer magnetizeCancel()
	stop := countdown(magnetizeCtx, j, s.tp, time.Now().Add(wait))
	started := time.Now()
	res, err := s.api.StoreResource(magnetizeCtx, s.c.ApiClaims, []byte(query))
	stop()
	if errors.Is(err, api.ErrForbidden) {
		// torrent-store refused the hash (stoplisted, banned): no peer will
		// change that, so not the dead-magnet card and its ten-minute retry,
		// but error.forbidden -- what the same hash reads once it is stored.
		return nil, errors.Wrap(err, "failed to store resource by magnet")
	}
	if errors.Is(err, api.ErrPrivateMagnet) {
		// The magnet did resolve, to a private torrent, but carries no
		// tracker, and rest-api stored nothing. Its own card -- the .torrent
		// or a magnet with the tracker -- not the dead one: no peer and no
		// ten-minute retry change that answer.
		return nil, &MagnetError{Kind: "private", Waited: time.Since(started), Long: long, Cause: err}
	}
	if err != nil || res == nil {
		kind := "dead"
		if err != nil && strings.Contains(err.Error(), "failed to parse magnet") {
			kind = "invalid"
		}
		return nil, &MagnetError{Kind: kind, Waited: time.Since(started), Long: long, Cause: err}
	}
	j.Done()
	if known != nil {
		return known, nil
	}
	return &api.Resource{ResourceResponse: *res}, nil
}

// debugMagnet plays the magnet failure for review (dev only): a short
// countdown, then the card. "magnet_dead_long" says the ten-minute attempt
// failed too; "magnet_invalid" is the broken link; "magnet_private" is the
// private torrent rest-api would not take as a bare magnet (no countdown: the
// card is what there is to look at).
func (s *LoadScript) debugMagnet(ctx context.Context, j *job.Job, mode string) error {
	switch mode {
	case "magnet_dead", "magnet_dead_long", "magnet_invalid", "magnet_private":
	default:
		return errors.Errorf("unknown debug mode %q", mode)
	}
	if mode == "magnet_private" {
		return &MagnetError{Kind: "private", Cause: fmt.Errorf("%w url=debug", api.ErrPrivateMagnet)}
	}
	if mode != "magnet_invalid" {
		wait := 8 * time.Second
		cctx, cancel := context.WithTimeout(ctx, wait)
		stop := countdown(cctx, j, s.tp, time.Now().Add(wait))
		<-cctx.Done()
		stop()
		cancel()
	}
	if mode == "magnet_invalid" {
		return &MagnetError{Kind: "invalid", Waited: 0, Cause: errors.New("failed to parse magnet: error parsing v1 infohash")}
	}
	return &MagnetError{Kind: "dead", Waited: MagnetWaitDefault, Long: mode == "magnet_dead_long", Cause: errors.New("context deadline exceeded")}
}

func Load(api *api.Api, i18nSvc *i18n.Service, c *web.Context, args *LoadArgs) (r job.Runnable, hash string, err error) {
	if args.Query != "" {
		hash, _, err = common.ResolveQueryHash(args.Query)
		if err != nil {
			// Logged as the job's error: RefusedQuery keeps a private
			// tracker's key out of it.
			return nil, "", common.RefusedQuery(err, args.Query)
		}
	} else if args.File != nil {
		b := io.NopCloser(bytes.NewReader(args.File))
		mi, err := metainfo.Load(b)
		if err != nil {
			return nil, "", err
		}
		hash = mi.HashInfoBytes().HexString()
		private_torrent.LogUpload(args.File, c.User, args.Source)
	}
	r = NewLoadScript(api, i18nSvc, c, args)
	return
}
