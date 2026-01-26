package vault

import (
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

type UserVP struct {
	tableName struct{}  `pg:"vault.user_vp"`
	UserID    uuid.UUID `pg:"user_id,pk,type:uuid"`
	Total     *float64  `pg:"total,type:numeric"`
	CreatedAt time.Time `pg:"created_at,notnull,default:now()"`
	UpdatedAt time.Time `pg:"updated_at,notnull,default:now()"`

	User *models.User `pg:"rel:has-one,fk:user_id"`
}
