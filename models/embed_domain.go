package models

import (
	"time"

	uuid "github.com/satori/go.uuid"
)

type EmbedDomain struct {
	tableName struct{}  `pg:"embed_domain"`
	ID        uuid.UUID `pg:"embed_domain_id,pk,type:uuid,default:uuid_generate_v4()"`
	Domain    string
	Ads       bool
	CreatedAt time.Time
	UpdatedAt time.Time

	UserID uuid.UUID `pg:"user_id"`
	User   *User     `pg:"rel:has-one,fk:user_id"`
}
