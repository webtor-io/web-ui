package uploads

import (
	"context"
	"net/http"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"golang.org/x/sync/errgroup"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
)

const (
	// WriteGrace is how long past its write token's end an upload may still
	// grow: the seeder's clock and a request body in flight. The quota SQL
	// spells it as interval '5 minutes'.
	WriteGrace = 5 * time.Minute
	// FreeCap and PaidCap are an account's upload quota: 10 GiB and 50 GiB.
	FreeCap int64 = 10737418240
	PaidCap int64 = 53687091200
)

// Limit is the account's upload quota by its tier.
func Limit(c *claims.Data) (int64, error) {
	if c == nil {
		return 0, errors.New("no claims")
	}
	if c.GetContext().GetTier().GetId() != 0 {
		return PaidCap, nil
	}
	return FreeCap, nil
}

// Usage is what a page shows: Used of Cap.
type Usage struct{ Used, Cap int64 }

// Service counts and reserves the accounts' upload quota, signs their
// write tokens and makes web-ui's own calls to the seeder.
type Service struct {
	pg  *cs.PG
	api *api.Api
	url string // UPLOAD_URL
	key string // WEBTOR_API_KEY
	cl  *http.Client
	// Pictures erases an upload's pictures; nil where there is no thumbnail bucket. Set once at
	// wiring, before the server starts.
	Pictures PictureEraser
}

// afterQuotaLock runs between taking the account's lock and counting: tests
// hold transactions there.
var afterQuotaLock = func() {}

// checkTimeout bounds one live check.
var checkTimeout = 3 * time.Second

// usedTimeout bounds the sum after the checks. The sum runs on a context
// their deadline does not cancel: past that deadline it still answers, with
// the last reserves known.
const usedTimeout = 2 * time.Second

const checkParallel = 4

// Quota is the account's upload quota use without exceptID, and its cap.
// With check it first asks rest-api, as the viewer ac, about every counted
// upload whose token is over, and writes down what it learns: the bytes the
// seeder holds, or that the upload is gone. A check that fails writes
// nothing, so the upload keeps its last known reserve. The checks share
// ctx's deadline.
func (s *Service) Quota(ctx context.Context, u *auth.User, cd *claims.Data, ac *api.Claims, exceptID string, check bool) (used, cap int64, err error) {
	cap, err = Limit(cd)
	if err != nil {
		return 0, 0, err
	}
	db := s.pg.Get()
	if db == nil {
		return 0, 0, errors.New("no database")
	}
	if check {
		if ac == nil {
			return 0, 0, errors.New("no api claims")
		}
		if err = s.check(ctx, db, u, ac, exceptID); err != nil {
			return 0, 0, err
		}
	}
	uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), usedTimeout)
	defer cancel()
	used, err = models.UploadUsed(uctx, db, u.ID, exceptID)
	if err != nil {
		return 0, 0, err
	}
	return used, cap, nil
}

// Usage is the account's quota use as a page shows it, from the reserves
// alone, without asking rest-api. Nil where uploads are off (s nil), without
// claims, or when the count fails: the page then has no line, not an error.
func (s *Service) Usage(ctx context.Context, u *auth.User, cd *claims.Data) *Usage {
	if s == nil {
		return nil
	}
	used, cap, err := s.Quota(ctx, u, cd, nil, "", false)
	if err != nil {
		log.WithError(err).WithField("user_hash", auth.LogHash(u.ID)).Warn("failed to get upload quota")
		return nil
	}
	return &Usage{Used: used, Cap: cap}
}

// check asks rest-api about each candidate row. Only rest-api's answers
// write: the seeder's reserved bytes, or gone on a 404 or on no_record for
// the owner, and then what web-ui derived from the upload is forgotten.
// Anything else, a 200 without reserved included, is a failure.
func (s *Service) check(ctx context.Context, db *pg.DB, u *auth.User, ac *api.Claims, exceptID string) error {
	rows, err := models.UploadCheckCandidates(ctx, db, u.ID, exceptID)
	if err != nil {
		return err
	}
	var g errgroup.Group
	g.SetLimit(checkParallel)
	for i := range rows {
		r := &rows[i]
		g.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			cctx, cancel := context.WithTimeout(ctx, checkTimeout)
			defer cancel()
			res, err := s.api.GetResource(cctx, ac, r.ResourceID)
			var pe *api.PrivateError
			switch {
			case err == nil && res == nil,
				errors.As(err, &pe) && pe.Code == api.CodePrivateTorrent && pe.Reason == api.ReasonNoRecord:
				err = models.MarkUploadGone(ctx, db, r)
				// Gone is inferred from rest-api here, so the user's own rows stay.
				if err == nil {
					if ferr := Forget(ctx, db, s.Pictures, r.ResourceID, false); ferr != nil {
						log.WithError(ferr).WithField("user_hash", auth.LogHash(u.ID)).Warn("upload quota check: failed to forget the upload")
					}
				}
			case err == nil && res.Reserved != nil:
				err = models.SetUploadReserved(ctx, db, r, *res.Reserved)
			default:
				return nil
			}
			if err != nil {
				log.WithError(err).WithField("user_hash", auth.LogHash(u.ID)).Warn("upload quota check write failed")
			}
			return nil
		})
	}
	_ = g.Wait()
	return nil
}

// QuotaLock serialises the account's token requests until tx ends.
func QuotaLock(ctx context.Context, tx *pg.Tx, userID uuid.UUID) error {
	return models.AdvisoryXactLock(ctx, tx, "upload-quota:"+userID.String())
}

// Reserve gives id a write limit under the account's lock and records it.
// own is what id already holds: reported, the seeder's bytes, or its live
// reserve when that is larger, so a renewal or a second tab never shrinks
// it. On top of own come want more bytes, as far as the cap minus the other
// uploads allows; want 0 renews at own. lim <= 0 records nothing. An upload
// saved to Vault gets ErrFrozen.
func Reserve(ctx context.Context, tx *pg.Tx, userID uuid.UUID, id string, cap, reported, want int64, exp time.Time) (lim, used int64, err error) {
	if err = QuotaLock(ctx, tx, userID); err != nil {
		return 0, 0, err
	}
	afterQuotaLock()
	frozen, err := models.UploadFrozen(ctx, tx, id)
	if err != nil {
		return 0, 0, err
	}
	if frozen {
		return 0, 0, ErrFrozen
	}
	if used, err = models.UploadUsed(ctx, tx, userID, id); err != nil {
		return 0, 0, err
	}
	live, err := models.UploadLiveReserve(ctx, tx, userID, id)
	if err != nil {
		return 0, 0, err
	}
	own := max(reported, live)
	lim = max(own, min(cap-used, own+want))
	if lim <= 0 {
		return lim, used, nil
	}
	if err = models.UpsertUploadReserve(ctx, tx, userID, id, lim, exp); err != nil {
		return 0, 0, err
	}
	return lim, used, nil
}
