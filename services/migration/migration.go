package migration

import (
	"github.com/go-pg/migrations/v8"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	services "github.com/webtor-io/common-services"
)

type PGMigration struct {
	db  *services.PG
	col *migrations.Collection
}

func NewPGMigration(db *services.PG, col *migrations.Collection) *PGMigration {
	return &PGMigration{
		db:  db,
		col: col,
	}
}

func (s *PGMigration) Run(a ...string) error {
	db := s.db.Get()
	if db == nil {
		log.Infof("DB not initialized, skipping migration")
		return nil
	}
	s.col.DiscoverSQLMigrations("migrations")
	_, _, err := s.col.Run(db, "init")
	if err != nil {
		return errors.Wrap(err, "failed to init DB PGMigrations")
	}
	oldVersion, newVersion, err := s.col.Run(db, a...)
	if err != nil {
		return errors.Wrapf(err, "failed to perform PGMigration from %v to %v", oldVersion, newVersion)
	}
	if newVersion != oldVersion {
		log.Infof("DB migrated from version %d to %d", oldVersion, newVersion)
	} else {
		log.Infof("DB PGMigration version is %d", oldVersion)
	}
	return nil
}
