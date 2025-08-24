package main

import (
	"net/http"

	"github.com/go-pg/migrations/v8"
	"github.com/urfave/cli"
	services "github.com/webtor-io/common-services"
	m "github.com/webtor-io/web-ui/migrations"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/migration"
)

func makePGMigrationCMD() cli.Command {
	migrateCmd := cli.Command{
		Name:    "migrate",
		Aliases: []string{"m"},
		Usage:   "Migrates database",
	}
	configurePGMigration(&migrateCmd)
	return migrateCmd
}

func configurePGMigration(c *cli.Command) {
	upCmd := cli.Command{
		Name:    "up",
		Usage:   "Runs all available migrations",
		Aliases: []string{"u"},
		Action: func(c *cli.Context) error {
			return pgMigrate(c, "up")
		},
	}
	downCmd := cli.Command{
		Name:    "down",
		Usage:   "Reverts last migration",
		Aliases: []string{"d"},
		Action: func(c *cli.Context) error {
			return pgMigrate(c, "down")
		},
	}
	resetCmd := cli.Command{
		Name:    "reset",
		Usage:   "Reverts all migrations",
		Aliases: []string{"r"},
		Action: func(c *cli.Context) error {
			return pgMigrate(c, "reset")
		},
	}
	versionCmd := cli.Command{
		Name:    "version",
		Usage:   "Prints current db version",
		Aliases: []string{"v"},
		Action: func(c *cli.Context) error {
			return pgMigrate(c, "version")
		},
	}
	c.Subcommands = []cli.Command{upCmd, downCmd, resetCmd, versionCmd}
	for k, _ := range c.Subcommands {
		configureSubPGMigration(&c.Subcommands[k])
	}
}
func configureSubPGMigration(c *cli.Command) {
	c.Flags = services.RegisterPGFlags(c.Flags)
	c.Flags = api.RegisterFlags(c.Flags)
}

func pgMigrate(c *cli.Context, a ...string) error {
	// Setting DB
	db := services.NewPG(c)
	defer db.Close()

	// Setting PGMigrations
	col := migrations.NewCollection()
	mgr := migration.NewPGMigration(db, col)

	// Setting HTTP Client
	cl := http.DefaultClient

	// Setting Api
	sapi := api.New(c, cl)

	// Setting custom migrations
	m.PopulateTorrentSizeBytes(col, sapi)

	// Run
	return mgr.Run(a...)
}
