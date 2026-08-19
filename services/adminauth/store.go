package adminauth

import (
	"context"
	"errors"
	"strings"
)

// ErrManagedByEnv is returned by Set when ADMIN_PASSWORD is in play: the
// environment is the source of truth then, and writing to the database would
// store a password that never takes effect.
var ErrManagedByEnv = errors.New("password is managed by ADMIN_PASSWORD")

// HashRepo is the persistence half of the store. Keeping it an interface lets
// the precedence rules be tested without a database.
type HashRepo interface {
	Get(ctx context.Context) (string, error)
	Set(ctx context.Context, hash string) error
}

// Store answers whether a password is configured and whether a given password
// matches it. ADMIN_PASSWORD, when set, wins outright over the stored hash —
// that is what makes it a recovery path.
type Store struct {
	envPassword string
	repo        HashRepo
}

func NewStore(envPassword string, repo HashRepo) *Store {
	return &Store{envPassword: strings.TrimSpace(envPassword), repo: repo}
}

func (s *Store) ManagedByEnv() bool {
	return s.envPassword != ""
}

func (s *Store) IsConfigured(ctx context.Context) bool {
	if s.ManagedByEnv() {
		return true
	}
	h, err := s.repo.Get(ctx)
	if err != nil {
		// Unreadable storage is not proof that no password exists. Report
		// configured so the instance stays closed instead of falling open.
		return true
	}
	return h != ""
}

func (s *Store) Verify(ctx context.Context, password string) bool {
	if s.ManagedByEnv() {
		return subtleEqual(s.envPassword, password)
	}
	h, err := s.repo.Get(ctx)
	if err != nil || h == "" {
		return false
	}
	return Verify(h, password)
}

func (s *Store) Set(ctx context.Context, password string) error {
	if s.ManagedByEnv() {
		return ErrManagedByEnv
	}
	h, err := Hash(password)
	if err != nil {
		return err
	}
	return s.repo.Set(ctx, h)
}
