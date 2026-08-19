package main

import (
	"github.com/urfave/cli"
)

func configure(app *cli.App) {
	serveCMD := makeServeCMD()
	migrationCMD := makePGMigrationCMD()
	enrichCMD := makeEnrichCMD()
	cacheIndexCMD := makeCacheIndexCMD()
	vaultCMD := makeVaultCMD()
	notificationCMD := makeNotificationCMD()
	subscriptionCMD := makeSubscriptionCMD()
	adminCMD := makeAdminCMD()
	app.Commands = []cli.Command{serveCMD, migrationCMD, enrichCMD, cacheIndexCMD, vaultCMD, notificationCMD, subscriptionCMD, adminCMD}
}
