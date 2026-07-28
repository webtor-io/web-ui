package models

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
	uuid "github.com/satori/go.uuid"
)

type AccessToken struct {
	tableName struct{} `pg:"access_token"`

	Token     uuid.UUID  `pg:"token,pk"`
	UserID    uuid.UUID  `pg:"user_id,notnull"`
	Name      string     `pg:"name,notnull"`
	Scope     []string   `pg:"scope,array"`
	ExpiresAt *time.Time `pg:"expires_at"`
	CreatedAt time.Time  `pg:"created_at,notnull"`

	User *User `pg:"rel:has-one,fk:user_id"`
}

func GetAccessTokenByName(ctx context.Context, db *pg.DB, userID uuid.UUID, name string) (*AccessToken, error) {
	token := new(AccessToken)
	err := db.Model(token).
		Context(ctx).
		Where("user_id = ?", userID).
		Where("name = ?", name).
		Select()

	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return token, nil
}

// ListUserAccessTokens returns every access token issued to a user, oldest first.
// Used by the GDPR data-export.
func ListUserAccessTokens(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]*AccessToken, error) {
	var list []*AccessToken
	err := db.Model(&list).
		Context(ctx).
		Where("user_id = ?", userID).
		OrderExpr("created_at ASC").
		Select()
	if err != nil {
		return nil, err
	}
	return list, nil
}

func MakeAccessToken(ctx context.Context, db *pg.DB, userID uuid.UUID, name string, scope []string) (*AccessToken, error) {
	token := &AccessToken{
		Token:     uuid.NewV4(),
		UserID:    userID,
		Name:      name,
		Scope:     scope,
		CreatedAt: time.Now(),
	}

	_, err := db.Model(token).
		Context(ctx).
		OnConflict("(user_id, name) DO UPDATE").
		Set("scope = EXCLUDED.scope").
		Returning("*").
		Insert()

	if err != nil {
		return nil, err
	}

	return token, nil
}

// RegenerateAccessToken issues a fresh token value for an existing
// (user_id, name) pair, invalidating the previous one.
//
// MakeAccessToken deliberately keeps the current token on conflict — pressing
// "Generate" twice must stay idempotent, otherwise every visit would silently
// break the addon already installed in the user's Stremio. Rotating a leaked
// URL therefore needs its own path, and it is destructive by design: the old
// URL stops resolving the moment this returns.
func RegenerateAccessToken(ctx context.Context, db *pg.DB, userID uuid.UUID, name string, scope []string) (*AccessToken, error) {
	token := &AccessToken{
		Token:     uuid.NewV4(),
		UserID:    userID,
		Name:      name,
		Scope:     scope,
		CreatedAt: time.Now(),
	}

	_, err := db.Model(token).
		Context(ctx).
		OnConflict("(user_id, name) DO UPDATE").
		Set("token = EXCLUDED.token").
		Set("scope = EXCLUDED.scope").
		Set("created_at = EXCLUDED.created_at").
		Returning("*").
		Insert()

	if err != nil {
		return nil, err
	}

	return token, nil
}

func GetUserByAccessTokenWithUser(ctx context.Context, db *pg.DB, token uuid.UUID) (*AccessToken, error) {
	accessToken := new(AccessToken)
	err := db.Model(accessToken).
		Context(ctx).
		Where("access_token.token = ?", token).
		WhereGroup(func(q *orm.Query) (*orm.Query, error) {
			return q.Where("expires_at IS NULL").WhereOr("expires_at > now()"), nil
		}).
		Relation("User").
		Select()

	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return accessToken, nil
}
