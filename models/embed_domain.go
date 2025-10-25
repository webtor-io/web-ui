package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

type EmbedDomain struct {
	tableName struct{}  `pg:"embed_domain"`
	ID        uuid.UUID `pg:"embed_domain_id,pk,type:uuid,default:uuid_generate_v4()"`
	Domain    string    `pg:"domain,notnull"`
	Ads       *bool     `pg:"ads,notnull,default:true"`
	CreatedAt time.Time
	UpdatedAt time.Time

	UserID uuid.UUID `pg:"user_id"`
	User   *User     `pg:"rel:has-one,fk:user_id"`
}

// GetUserDomains returns all domains for a specific user
func GetUserDomains(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]EmbedDomain, error) {
	var domains []EmbedDomain
	err := db.Model(&domains).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Select()
	if err != nil {
		return nil, err
	}
	return domains, nil
}

// CountUserDomains returns the number of domains for a specific user
func CountUserDomains(ctx context.Context, db *pg.DB, userID uuid.UUID) (int, error) {
	return db.Model(&EmbedDomain{}).
		Context(ctx).
		Where("user_id = ?", userID).
		Count()
}

// DomainExists checks if a domain already exists in the system
func DomainExists(ctx context.Context, db *pg.DB, domain string) (bool, error) {
	existing := &EmbedDomain{}
	err := db.Model(existing).
		Context(ctx).
		Where("domain = ?", domain).
		Select()
	if err == nil {
		return true, nil
	} else if errors.Is(err, pg.ErrNoRows) {
		return false, nil
	}
	return false, err
}

// CreateDomain creates a new embed domain for a user
func CreateDomain(ctx context.Context, db *pg.DB, userID uuid.UUID, domain string) error {
	ads := false
	embedDomain := &EmbedDomain{
		Domain: domain,
		UserID: userID,
		Ads:    &ads, // Disable ads for registered domains
	}

	_, err := db.Model(embedDomain).
		Context(ctx).
		Insert()
	return err
}

// DeleteUserDomain deletes a domain owned by a specific user
func DeleteUserDomain(ctx context.Context, db *pg.DB, domainID uuid.UUID, userID uuid.UUID) error {
	_, err := db.Model(&EmbedDomain{}).
		Context(ctx).
		Where("embed_domain_id = ? AND user_id = ?", domainID, userID).
		Delete()
	return err
}
