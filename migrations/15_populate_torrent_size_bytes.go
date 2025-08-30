package migrations

import (
	"github.com/go-pg/migrations/v8"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
)

func PopulateTorrentSizeBytes(col *migrations.Collection, a *api.Api) {
	col.MustRegister(func(db migrations.DB) error {
		ctx := db.Context()
		var resources []*models.TorrentResource

		err := db.Model(&resources).
			Where("torrent_size_bytes is null").
			Select()
		if err != nil {
			return err
		}
		claims := &api.Claims{}
		for _, r := range resources {
			t, err := a.GetTorrentCached(ctx, claims, r.ResourceID)
			if err != nil {
				return err
			}
			size := int64(len(t))
			r.TorrentSizeBytes = size
			log.Infof("Updating torrent size for %s: %d", r.ResourceID, size)
			res, err := db.Model(r).WherePK().Column("torrent_size_bytes").Update()
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
