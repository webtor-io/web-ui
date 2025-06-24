package models

import (
	"errors"
	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
	uuid "github.com/satori/go.uuid"
	"time"
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

func GetAccessTokenByName(db *pg.DB, userID uuid.UUID, name string) (*AccessToken, error) {
	token := new(AccessToken)
	err := db.Model(token).
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

func MakeAccessToken(db *pg.DB, userID uuid.UUID, name string, scope []string) (*AccessToken, error) {
	token := &AccessToken{
		Token:     uuid.NewV4(),
		UserID:    userID,
		Name:      name,
		Scope:     scope,
		CreatedAt: time.Now(),
	}

	_, err := db.Model(token).
		OnConflict("(user_id, name) DO UPDATE").
		Set("scope = EXCLUDED.scope").
		Returning("*").
		Insert()

	if err != nil {
		return nil, err
	}

	return token, nil
}

func RevokeAccessToken(db *pg.DB, userID uuid.UUID, name string) error {
	_, err := db.Model((*AccessToken)(nil)).
		Where("user_id = ?", userID).
		Where("name = ?", name).
		Delete()
	return err
}

func GetUserByAccessTokenWithUser(db *pg.DB, token uuid.UUID) (*AccessToken, error) {
	accessToken := new(AccessToken)
	err := db.Model(accessToken).
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
