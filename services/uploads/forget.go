package uploads

import (
	"context"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
)

// PictureEraser is thumbnail.Service seen through the one call Forget makes: services/thumbnail
// imports this package for the ID predicates, so the dependency cannot go the other way.
type PictureEraser interface {
	Erase(ctx context.Context, resourceID string) error
}

// Forget erases what web-ui derived from the files of an unsaved upload whose directory is gone.
// Always: its enrichment rows and its pictures — the owner's next page view and playback bring
// them back. Only when certain: its watch history and user subtitle bindings — the user made
// them and nothing brings them back. certain is true where the directory is known to be gone
// (the cleaner removed it) and false where that is inferred from a rest-api answer, which is
// wrong while the upload's home node is away. The library row, the name in torrent_resource and
// upload_reserve stay — the owner still sees "this upload is gone". An upload whose Vault copy
// is paid for or stored (vault.resource funded or vaulted) is left alone: it outlives its
// directory. Safe to repeat.
func Forget(ctx context.Context, db *pg.DB, pics PictureEraser, id string, certain bool) error {
	return forget(ctx, db, pics, id, true, certain)
}

// forget is Forget's body. keepSaved false erases an upload saved in Vault too: account deletion.
// The rows go before the pictures: they carry the file names and need no bucket.
func forget(ctx context.Context, db *pg.DB, pics PictureEraser, id string, keepSaved, certain bool) error {
	if !Owned(id) {
		return nil
	}
	if db == nil {
		return errors.New("no db")
	}
	if keepSaved {
		res, err := vaultModels.GetResource(ctx, db, id)
		if err != nil {
			return err
		}
		// A row neither funded nor vaulted is no copy: it is written before
		// the VP check, and without the directory the bytes are nowhere.
		if res != nil && (res.Funded || res.Vaulted) {
			return nil
		}
	}
	if err := models.DropUploadTraces(ctx, db, id, certain); err != nil {
		return err
	}
	if pics != nil {
		return pics.Erase(ctx, id)
	}
	return nil
}

// ForgetUser erases all of it — the watch history and the subtitle bindings too — for every
// upload the account ever got a write token for, saved in Vault or not: once the account is
// gone, nobody can pay for a copy or erase what is left of it. A nil service does nothing.
// Every upload is tried; the first error is returned.
func (s *Service) ForgetUser(ctx context.Context, userID uuid.UUID) error {
	if s == nil {
		return nil
	}
	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}
	ids, err := models.UploadReserveIDs(ctx, db, userID)
	if err != nil {
		return err
	}
	var first error
	for _, id := range ids {
		if err := forget(ctx, db, s.Pictures, id, false, true); err != nil && first == nil {
			first = err
		}
	}
	return first
}
