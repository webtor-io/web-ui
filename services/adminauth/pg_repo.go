package adminauth

import (
	"context"
	"errors"

	"github.com/go-pg/pg/v10"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models"
)

// adminEmail is the single administrator's row. It matches the email used by
// services/auth.registerAdminUser, which creates that row on first request.
const adminEmail = "admin"

var errNoDB = errors.New("database is not available")

type pgRepo struct {
	pg *cs.PG
}

// NewPGRepo stores the admin password hash in the existing user.password
// column. That column has existed since migration 2 and was never read or
// written — it predates SuperTokens.
func NewPGRepo(p *cs.PG) HashRepo {
	return &pgRepo{pg: p}
}

func (r *pgRepo) Get(ctx context.Context) (string, error) {
	db := r.pg.Get()
	if db == nil {
		return "", errNoDB
	}
	u := &models.User{}
	err := db.Model(u).
		Context(ctx).
		Where("email = ?", adminEmail).
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		// No admin row yet means nobody has visited the instance, which is
		// indistinguishable from "no password set".
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return u.Password, nil
}

func (r *pgRepo) Set(ctx context.Context, hash string) error {
	db := r.pg.Get()
	if db == nil {
		return errNoDB
	}
	u := &models.User{}
	res, err := db.Model(u).
		Context(ctx).
		Set("password = ?", hash).
		Where("email = ?", adminEmail).
		Update()
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		// Admin row does not exist; the update was a no-op.
		// Return pg.ErrNoRows so the Store above understands the write failed
		// the same way it understands a missing row in Get().
		return pg.ErrNoRows
	}
	return nil
}
