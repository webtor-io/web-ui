package main

import (
	"github.com/urfave/cli"
	services "github.com/webtor-io/common-services"
)

func configure(app *cli.App) {
	serveCMD := makeServeCMD()
	migrationCMD := services.MakePGMigrationCMD()
	enrichCMD := makeEnrichCMD()
	app.Commands = []cli.Command{serveCMD, migrationCMD, enrichCMD}
}
