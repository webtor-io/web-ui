package main

import (
	"github.com/urfave/cli"
)

func configure(app *cli.App) {
	serveCMD := makeServeCMD()
	migrationCMD := makePGMigrationCMD()
	enrichCMD := makeEnrichCMD()
	cacheIndexCMD := makeCacheIndexCMD()
	app.Commands = []cli.Command{serveCMD, migrationCMD, enrichCMD, cacheIndexCMD}
}
