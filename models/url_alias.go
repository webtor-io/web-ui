package models

import (
	"errors"
	"fmt"
	"github.com/go-pg/pg/v10"
	"math/rand"
	"time"
)

type URLAlias struct {
	tableName struct{} `pg:"url_alias"`

	Code      string    `pg:"code,pk"`
	URL       string    `pg:"url,notnull"`
	CreatedAt time.Time `pg:"created_at,notnull"`
}

var alphaNum = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

func randomAlphaNum(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = alphaNum[rand.Intn(len(alphaNum))]
	}
	return string(b)
}

func GetURLAliasByCode(db *pg.DB, code string) (*URLAlias, error) {
	alias := new(URLAlias)

	err := db.Model(alias).
		Where("code = ?", code).
		Select()

	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil // код не найден
		}
		return nil, err // ошибка БД
	}

	return alias, nil
}

func CreateOrGetURLAlias(db *pg.DB, url string) (*URLAlias, error) {
	// поиск по URL
	alias := new(URLAlias)
	err := db.Model(alias).
		Where("url = ?", url).
		Select()
	if err == nil {
		return alias, nil
	}
	if !errors.Is(err, pg.ErrNoRows) {
		return nil, err
	}

	// генерация уникального кода
	var code string
	for i := 0; i < 10; i++ {
		code = randomAlphaNum(6)
		exists, err := db.Model((*URLAlias)(nil)).
			Where("code = ?", code).
			Exists()
		if err != nil {
			return nil, err
		}
		if !exists {
			break
		}
		if i == 9 {
			return nil, fmt.Errorf("failed to generate unique code")
		}
	}

	alias = &URLAlias{
		Code:      code,
		URL:       url,
		CreatedAt: time.Now(),
	}

	_, err = db.Model(alias).Insert()
	if err != nil {
		return nil, err
	}

	return alias, nil
}
