package adminauth

import (
	"context"
	"errors"
	"testing"
)

// fakeRepo stands in for the database so the precedence rules can be tested
// without one.
type fakeRepo struct {
	hash    string
	setCall int
	getErr  error
}

func (f *fakeRepo) Get(_ context.Context) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.hash, nil
}

func (f *fakeRepo) Set(_ context.Context, h string) error {
	f.setCall++
	f.hash = h
	return nil
}

func TestNoPasswordConfigured(t *testing.T) {
	s := NewStore("", &fakeRepo{})
	if s.IsConfigured(context.Background()) {
		t.Error("IsConfigured reported true with neither env nor stored hash")
	}
	if s.Verify(context.Background(), "anything") {
		t.Error("Verify accepted a password while none is configured")
	}
}

func TestEnvPasswordWins(t *testing.T) {
	stored, err := Hash("stored password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	s := NewStore("env password", &fakeRepo{hash: stored})

	if !s.ManagedByEnv() {
		t.Error("ManagedByEnv is false while ADMIN_PASSWORD is set")
	}
	if !s.IsConfigured(context.Background()) {
		t.Error("IsConfigured is false while ADMIN_PASSWORD is set")
	}
	if !s.Verify(context.Background(), "env password") {
		t.Error("the env password was rejected")
	}
	if s.Verify(context.Background(), "stored password") {
		t.Error("the stored hash still verified while ADMIN_PASSWORD is set — env must win outright")
	}
}

func TestStoredHashUsedWithoutEnv(t *testing.T) {
	stored, err := Hash("stored password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	s := NewStore("", &fakeRepo{hash: stored})

	if s.ManagedByEnv() {
		t.Error("ManagedByEnv is true with no ADMIN_PASSWORD")
	}
	if !s.Verify(context.Background(), "stored password") {
		t.Error("the stored password was rejected")
	}
	if s.Verify(context.Background(), "wrong password") {
		t.Error("a wrong password verified against the stored hash")
	}
}

func TestSetRefusedWhenManagedByEnv(t *testing.T) {
	repo := &fakeRepo{}
	s := NewStore("env password", repo)
	if err := s.Set(context.Background(), "new password"); !errors.Is(err, ErrManagedByEnv) {
		t.Errorf("Set: got %v, want ErrManagedByEnv", err)
	}
	if repo.setCall != 0 {
		t.Error("Set wrote to the repo even though the password is env-managed")
	}
}

func TestSetStoresAVerifiableHash(t *testing.T) {
	repo := &fakeRepo{}
	s := NewStore("", repo)
	if err := s.Set(context.Background(), "brand new password"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if repo.hash == "brand new password" {
		t.Fatal("Set stored the password verbatim instead of a hash")
	}
	if !s.Verify(context.Background(), "brand new password") {
		t.Error("the password just set does not verify")
	}
}

func TestSetRejectsShortPassword(t *testing.T) {
	repo := &fakeRepo{}
	s := NewStore("", repo)
	if err := s.Set(context.Background(), "short7c"); !errors.Is(err, ErrTooShort) {
		t.Errorf("Set: got %v, want ErrTooShort", err)
	}
	if repo.setCall != 0 {
		t.Error("Set wrote a too-short password to the repo")
	}
}

// A database that is down must not silently turn a protected instance into an
// open one: an unreadable hash means "cannot verify", not "no password".
func TestRepoErrorDoesNotOpenTheInstance(t *testing.T) {
	s := NewStore("", &fakeRepo{getErr: errors.New("db is down")})
	if s.Verify(context.Background(), "anything") {
		t.Error("Verify succeeded while the hash could not be read")
	}
}
