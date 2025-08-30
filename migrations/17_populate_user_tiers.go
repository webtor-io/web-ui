package migrations

import (
	"github.com/go-pg/migrations/v8"
	log "github.com/sirupsen/logrus"
	proto "github.com/webtor-io/claims-provider/proto"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/claims"
)

func PopulateUserTiers(col *migrations.Collection, cl *claims.Client) {
	col.MustRegister(func(db migrations.DB) error {
		if cl == nil {
			return nil
		}
		c, err := cl.Get()
		if err != nil {
			return err
		}
		ctx := db.Context()
		var users []*models.User

		err = db.Model(&users).
			Where("tier is null").
			Select()
		if err != nil {
			return err
		}
		for _, u := range users {
			var patreonUserID string
			if u.PatreonUserID != nil {
				patreonUserID = *u.PatreonUserID
			}
			r, err := c.Get(ctx, &proto.GetRequest{Email: u.Email, PatreonUserId: patreonUserID})
			if err != nil {
				return err
			}
			u.Tier = r.Context.Tier.Name
			log.Infof("Updating tier for user %s: %s", u.UserID, u.Tier)
			res, err := db.Model(u).WherePK().Column("tier").Update()
			if err != nil {
				return err
			}
			log.Infof("Updated %d rows", res.RowsAffected())
		}
		return nil
	}, func(db migrations.DB) error {
		return nil
	})
}
