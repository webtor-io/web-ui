package models

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/go-pg/pg/v10"
)

type URLAlias struct {
	tableName struct{}  `pg:"url_alias"`
	Code      string    `pg:"code,pk"`
	URL       string    `pg:"url,notnull"`
	Proxy     bool      `pg:"proxy,notnull,default:false"`
	CreatedAt time.Time `pg:"created_at,notnull"`
}

var alphaNum = []byte("abcdefghijklmnopqrstuvwxyz0123456789")

// aliasCodeLen is 16 characters over a 36-symbol alphabet — about 82 bits.
//
// It was 6, which is ~31 bits, and that is not enough for what these codes
// actually are. A /s/<code> is a capability URL: it fronts /token/<token>/…,
// so resolving one either serves the owner's WebDAV tree in place or
// 301-redirects with their raw access token in the Location header. The
// resolving endpoint is anonymous, has no rate limit, and answers 404 or 301
// — a clean oracle — so the work to hit *somebody's* alias is 2^31 divided by
// the number of live aliases, not 2^31.
//
// Existing 6-character codes keep working: lookup is by code, and
// CreateOrGetURLAlias matches on the URL, so a row is only re-minted when the
// underlying token is regenerated. Rotating the old short ones is a separate
// data fix.
const aliasCodeLen = 16

// randomAlphaNum draws from crypto/rand.
//
// math/rand's global source is randomly seeded on modern Go, so the old
// implementation was not *predictable* — the defect was the keyspace above.
// It is crypto/rand here anyway: a value that acts as a bearer credential
// should not depend on which generator happens to be wired up.
//
// Rejection sampling because 256 is not a multiple of 36; taking the modulo
// directly would make the first four letters ~11% likelier than the rest.
func randomAlphaNum(n int) (string, error) {
	const maxUnbiased = 252 // 7 * 36
	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("failed to read random bytes: %w", err)
		}
		for _, b := range buf {
			if b >= maxUnbiased {
				continue
			}
			out = append(out, alphaNum[int(b)%len(alphaNum)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}

func GetURLAliasByCode(ctx context.Context, db *pg.DB, code string) (*URLAlias, error) {
	alias := new(URLAlias)

	err := db.Model(alias).
		Context(ctx).
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

func CreateOrGetURLAlias(ctx context.Context, db *pg.DB, url string, proxy bool) (*URLAlias, error) {
	// поиск по URL
	alias := new(URLAlias)
	err := db.Model(alias).
		Context(ctx).
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
		code, err = randomAlphaNum(aliasCodeLen)
		if err != nil {
			return nil, err
		}
		exists, err := db.Model((*URLAlias)(nil)).
			Context(ctx).
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
		Proxy:     proxy,
		CreatedAt: time.Now(),
	}

	_, err = db.Model(alias).
		Context(ctx).
		Insert()
	if err != nil {
		return nil, err
	}

	return alias, nil
}
