package upload

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/uploads"
)

// tokenRequest asks for a write token: Name creates an upload, ID adds to
// one or renews its token. Want is the size of the files to add; with ID, 0
// or none renews the token at what the upload already holds.
type tokenRequest struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Want int64  `json:"want"`
}

// valid: exactly one of Name and ID, Want not negative, and at least a byte
// for a new upload, which holds nothing yet.
func (r *tokenRequest) valid() bool {
	return r.Want >= 0 && (r.Name == "") != (r.ID == "") && (r.ID != "" || r.Want >= 1)
}

// refusal answers a token request without a token: status, and body when it
// has one. err, when set, is a failure to log.
type refusal struct {
	status int
	body   gin.H
	err    error
}

func (r *refusal) Error() string {
	if r.err != nil {
		return r.err.Error()
	}
	return http.StatusText(r.status)
}

func failure(status int, err error) *refusal {
	return &refusal{status: status, err: err}
}

func (h *Handler) token(c *gin.Context) {
	var req tokenRequest
	if err := c.ShouldBindJSON(&req); err != nil || !req.valid() {
		c.Status(http.StatusBadRequest)
		return
	}
	u := auth.GetUserFromContext(c)
	g, err := h.issue(c.Request.Context(), u, claims.GetFromContext(c), api.GetClaimsFromContext(c), &req)
	if err == nil {
		c.JSON(http.StatusOK, g)
		return
	}
	var rf *refusal
	if !errors.As(err, &rf) {
		rf = failure(http.StatusInternalServerError, err)
	}
	if rf.err != nil {
		log.WithError(rf.err).WithField("user", auth.LogHash(u.ID)).Warn("failed to issue an upload token")
	}
	if rf.body != nil {
		c.JSON(rf.status, rf.body)
	} else {
		c.Status(rf.status)
	}
}

// issue reserves the account's quota for the request and signs its token. A
// new upload gets its resource and library rows in the transaction of its
// reserve, so a refusal leaves none of them.
func (h *Handler) issue(ctx context.Context, u *auth.User, cd *claims.Data, ac *api.Claims, req *tokenRequest) (*uploads.Grant, error) {
	if ac == nil {
		return nil, failure(http.StatusInternalServerError, errors.New("no api claims"))
	}
	if _, err := uploads.Limit(cd); err != nil {
		return nil, failure(http.StatusInternalServerError, err)
	}
	exp := time.Now().Add(uploads.TokenTTL)
	id, up, reported := req.ID, "add", int64(0)
	var err error
	if id != "" {
		reported, err = h.reported(ctx, ac, id)
	} else {
		id, err = uploads.NewID()
		up = "new"
	}
	if err != nil {
		return nil, err
	}
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	_, cp, err := h.up.Quota(qctx, u, cd, ac, req.ID, true)
	cancel()
	if err != nil {
		return nil, failure(http.StatusServiceUnavailable, err)
	}
	var lim, used int64
	err = h.pg.Get().RunInTransaction(ctx, func(tx *pg.Tx) error {
		var err error
		lim, used, err = uploads.Reserve(ctx, tx, u.ID, id, cp, reported, req.Want, exp)
		switch {
		case errors.Is(err, uploads.ErrFrozen):
			return &refusal{status: http.StatusConflict, body: gin.H{"code": "frozen"}}
		case err != nil:
			return err
		case lim <= 0:
			return &refusal{status: http.StatusRequestEntityTooLarge, body: gin.H{"code": "quota", "used": used, "cap": cp}}
		case req.Name != "":
			return models.CreateUpload(ctx, tx, u.ID, id, req.Name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	g, err := h.up.Grant(ac, id, up, lim, cp, used, exp)
	if err != nil {
		return nil, err
	}
	log.WithFields(log.Fields{"user": auth.LogHash(u.ID), "id": id, "up": up, "lim": lim}).Info("upload token")
	return g, nil
}

// reported is what the seeder holds of the upload id, as rest-api tells the
// viewer ac. rest-api refuses an upload that is not the viewer's, and does
// not know one whose files are gone.
func (h *Handler) reported(ctx context.Context, ac *api.Claims, id string) (int64, error) {
	if !uploads.Owned(id) {
		return 0, &refusal{status: http.StatusNotFound}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := h.api.GetResource(ctx, ac, id)
	var pe *api.PrivateError
	switch {
	case errors.As(err, &pe) && pe.Status == http.StatusForbidden:
		return 0, &refusal{status: http.StatusNotFound}
	case err != nil:
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, failure(http.StatusServiceUnavailable, err)
	case res == nil:
		return 0, &refusal{status: http.StatusGone}
	case res.Reserved == nil:
		return 0, failure(http.StatusServiceUnavailable, errors.New("rest-api answered an upload without reserved"))
	}
	return *res.Reserved, nil
}
