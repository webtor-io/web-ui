// Package user_settings is a thin caching wrapper over the
// user_settings DB row. Three consumers:
//
//   - poster_resolver: reads ShowAdult to decide whether to serve the
//     /raw/ variant
//   - resource/library templates: read ShowAdult to pick the right
//     poster URL
//   - profile handler: reads + writes via the toggle form
//
// Settings change infrequently (user opts in once), but they're read
// on the hot path (every poster fetch). The 5-min lazymap TTL bounds
// staleness without round-tripping the DB on every render. Pattern
// mirrors services/embed/domain_settings.go.
package user_settings

import (
	"context"
	"time"

	"github.com/pkg/errors"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
)

// Default is the zero-value settings returned for unauthenticated
// users or those who've never saved a preference. Centralised so a
// future field addition only needs one place updated.
func Default() *models.UserSettings {
	return &models.UserSettings{
		ShowAdult: false,
	}
}

type Service struct {
	*lazymap.LazyMap[*models.UserSettings]
	pg *cs.PG
}

func New(pg *cs.PG) *Service {
	return &Service{
		pg: pg,
		LazyMap: lazymap.New[*models.UserSettings](&lazymap.Config{
			Expire:      5 * time.Minute,
			ErrorExpire: 30 * time.Second,
		}),
	}
}

// Get returns the user's settings, never nil — falls back to Default()
// for anonymous (uuid.Nil) or missing rows. Errors are surfaced so the
// hot path can decide its own degradation (poster_resolver swallows
// + defaults to no-blur exception, profile handler propagates 500).
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (*models.UserSettings, error) {
	if userID == uuid.Nil {
		return Default(), nil
	}
	key := userID.String()
	return s.LazyMap.Get(key, func() (*models.UserSettings, error) {
		db := s.pg.Get()
		if db == nil {
			return Default(), errors.New("user_settings: no db")
		}
		us, err := models.GetUserSettings(ctx, db, userID)
		if err != nil {
			return Default(), err
		}
		if us == nil {
			us = Default()
			us.UserID = userID
		}
		return us, nil
	})
}

// Set persists the row and invalidates the cache so the next Get
// reads the fresh value. Toggle-style settings call this on every
// form-submit; the row is small enough that partial-update PATCH
// semantics aren't worth the API complexity.
func (s *Service) Set(ctx context.Context, us *models.UserSettings) error {
	if us.UserID == uuid.Nil {
		return errors.New("user_settings: empty user_id")
	}
	db := s.pg.Get()
	if db == nil {
		return errors.New("user_settings: no db")
	}
	if err := models.UpsertUserSettings(ctx, db, us); err != nil {
		return errors.Wrap(err, "failed to upsert user_settings")
	}
	// Drop from cache so the next Get reads the fresh row from DB.
	// (lazymap has no Set/overwrite — Drop + next-Get-rehydrates is
	// the idiomatic pattern.)
	s.LazyMap.Drop(us.UserID.String())
	return nil
}
