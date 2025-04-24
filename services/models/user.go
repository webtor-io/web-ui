package models

import (
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	"time"

	uuid "github.com/satori/go.uuid"
)

type User struct {
	tableName struct{}  `pg:"user"`
	UserID    uuid.UUID `pg:"user_id,pk"`
	Email     string
	Password  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func GetOrCreateUser(db *pg.DB, email string) (*User, error) {
	user := &User{}
	err := db.Model(user).Where("email = ?", email).Limit(1).Select()
	if err == nil {
		return user, nil // Found
	}
	if !errors.Is(err, pg.ErrNoRows) {
		return nil, err // DB error
	}

	// Create new user
	user.Email = email
	_, err = db.Model(user).Insert()
	if err != nil {
		return nil, err
	}
	return user, nil
}
