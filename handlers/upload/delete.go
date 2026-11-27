package upload

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/uploads"
	"github.com/webtor-io/web-ui/services/web"
)

// delete deletes the uploads a form names (Level 1: HTTP).
func (h *Handler) delete(c *gin.Context) {
	ids, err := web.FormBulkIDs(c)
	if err == nil {
		err = h.deleteUploads(c.Request.Context(), auth.GetUserFromContext(c), api.GetClaimsFromContext(c), ids)
	}
	if err != nil {
		web.RedirectWithError(c, err)
		return
	}
	c.Request.Header.Set("X-Return-Url", returnAfterDelete(c.GetHeader("X-Return-Url"), ids))
	web.RedirectWithSuccessAndMessage(c, "toast.uploadsDeleted")
}

// returnAfterDelete is where the owner goes once the uploads ids are deleted:
// ret, the page the form was on, unless it is the page of one of them, which
// now answers 404 — then the list of their uploads, in the page's language.
func returnAfterDelete(ret string, ids []string) string {
	u, err := url.Parse(ret)
	if err != nil {
		return ret
	}
	for _, id := range ids {
		prefix, ok := strings.CutSuffix(u.Path, "/"+id)
		if !ok {
			continue
		}
		if _, lang := i18n.LangNames[strings.TrimPrefix(prefix, "/")]; prefix == "" || lang {
			return prefix + "/lib/uploads"
		}
	}
	return ret
}

// deleteUploads deletes every upload it is given, as the viewer u with the
// api claims ac. One that fails does not stop the rest; the first failure is
// what the viewer is told.
func (h *Handler) deleteUploads(ctx context.Context, u *auth.User, ac *api.Claims, ids []string) error {
	if len(ids) == 0 {
		return errors.New("resource_id is required")
	}
	var first error
	failed := 0
	for _, id := range ids {
		if err := h.deleteUpload(ctx, u, ac, id); err != nil {
			failed++
			if first == nil {
				first = err
			}
		}
	}
	if first != nil && len(ids) > 1 {
		return errors.Wrapf(first, "failed to delete %d of %d uploads", failed, len(ids))
	}
	return first
}

// deleteUpload deletes the upload id: its Vault copy, its files on the
// seeder, then everything web-ui keeps of it. An upload rest-api does not
// show the viewer is gone or not theirs. Every step can be repeated: after a
// failure the same button carries on from where it stopped.
func (h *Handler) deleteUpload(ctx context.Context, u *auth.User, ac *api.Claims, id string) error {
	if !uploads.HomeHosted(id) {
		return errors.Errorf("not an upload: %v", id)
	}
	db := h.pg.Get()
	if db == nil {
		return errors.New("no database")
	}
	if !uploads.Owned(id) {
		return models.RemoveFromLibrary(ctx, db, u.ID, id)
	}
	// Without the viewer's claims rest-api would answer as to anyone: a
	// refusal that reads as "not yours".
	if ac == nil {
		return errors.New("no api claims")
	}
	res, err := h.api.GetResource(ctx, ac, id)
	var pe *api.PrivateError
	switch {
	case err == nil && res == nil, errors.As(err, &pe) && pe.Status == http.StatusForbidden:
		return h.deleteMissing(ctx, db, u, id)
	case err != nil:
		return errors.Wrap(err, "failed to get resource")
	}
	if h.vault != nil {
		pledges, err := vaultModels.GetResourcePledges(ctx, db, id)
		if err != nil {
			return err
		}
		for i := range pledges {
			if err := h.vault.RemovePledge(ctx, &pledges[i]); err != nil {
				return err
			}
		}
		r, err := h.vault.GetResource(ctx, id)
		if err != nil {
			return err
		}
		if r != nil {
			if err := h.vault.RemoveResource(ctx, id); err != nil {
				return err
			}
		}
	}
	tombstoned, err := h.up.Delete(ctx, id)
	if err != nil {
		return err
	}
	// The files are gone: a mark that failed is left to the live check of the
	// next token, a retry would not write it either.
	if err := models.MarkUploadDeleted(ctx, db, u.ID, id, tombstoned); err != nil {
		log.WithError(err).WithField("user", auth.LogHash(u.ID)).Warn("upload delete: reserve mark failed")
	}
	// Pictures first: the rows the purge deletes are the only record of their
	// objects.
	if err := h.thumb.Erase(ctx, id); err != nil {
		return err
	}
	return models.PurgeResourceByID(ctx, db, id)
}

// deleteMissing deletes an upload rest-api does not show the viewer. Its
// owner, the account a token for it was issued to, gets back the pledge of
// a copy that never finished and loses everything web-ui keeps of it; anyone
// else, only their own library row. The library row goes last: until then
// the same button can try again.
func (h *Handler) deleteMissing(ctx context.Context, db *pg.DB, u *auth.User, id string) error {
	if err := h.releaseOwnPledge(ctx, db, u, id); err != nil {
		return err
	}
	owns, err := models.HasUploadReserve(ctx, db, u.ID, id)
	if err != nil {
		return err
	}
	if !owns {
		return models.RemoveFromLibrary(ctx, db, u.ID, id)
	}
	if err := h.thumb.Erase(ctx, id); err != nil {
		return err
	}
	return models.PurgeResourceByID(ctx, db, id)
}

// releaseOwnPledge removes the viewer's own pledge of id, and its
// vault.resource once no pledge is left. Without a pledge of theirs Vault is
// not touched.
func (h *Handler) releaseOwnPledge(ctx context.Context, db *pg.DB, u *auth.User, id string) error {
	if h.vault == nil {
		return nil
	}
	res, err := h.vault.GetResource(ctx, id)
	if err != nil || res == nil {
		return err
	}
	p, err := h.vault.GetPledge(ctx, u, res)
	if err != nil || p == nil {
		return err
	}
	if err := h.vault.RemovePledge(ctx, p); err != nil {
		return err
	}
	left, err := vaultModels.GetResourcePledges(ctx, db, id)
	if err != nil || len(left) > 0 {
		return err
	}
	return h.vault.RemoveResource(ctx, id)
}
