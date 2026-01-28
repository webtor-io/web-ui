package main

import (
	"context"
	"net/http"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/vault"
)

func makeVaultCMD() cli.Command {
	vaultCMD := cli.Command{
		Name:    "vault",
		Aliases: []string{"v"},
		Usage:   "Vault management commands",
	}
	configureVault(&vaultCMD)
	return vaultCMD
}

func configureVault(c *cli.Command) {
	reapCmd := cli.Command{
		Name:    "reap",
		Aliases: []string{"r"},
		Usage:   "Removes expired vault resources and their pledges",
		Action:  reap,
	}
	configureVaultReap(&reapCmd)
	c.Subcommands = []cli.Command{reapCmd}
}

func configureVaultReap(c *cli.Command) {
	c.Flags = cs.RegisterPGFlags(c.Flags)
	c.Flags = api.RegisterFlags(c.Flags)
	c.Flags = claims.RegisterClientFlags(c.Flags)
	c.Flags = vault.RegisterApiFlags(c.Flags)
	c.Flags = vault.RegisterFlags(c.Flags)
}

func reap(c *cli.Context) error {
	ctx := context.Background()

	// Setting DB
	pg := cs.NewPG(c)
	defer pg.Close()

	// Setting Migrations
	m := cs.NewPGMigration(pg)
	err := m.Run()
	if err != nil {
		return errors.Wrap(err, "failed to run migrations")
	}

	db := pg.Get()
	if db == nil {
		return errors.New("db is nil")
	}

	// Setting HTTP Client
	cl := http.DefaultClient

	// Setting Webtor API
	sapi := api.New(c, cl)

	// Setting Claims Client
	cpCl := claims.NewClient(c)
	if cpCl != nil {
		defer cpCl.Close()
	}

	// Setting Claims
	claimsService := claims.New(c, cpCl, pg)

	// Setting Vault API
	vaultApi := vault.NewApi(c, cl)

	// Setting Vault
	vaultService := vault.New(c, vaultApi, claimsService, cl, pg, sapi)
	if vaultService == nil {
		return errors.New("vault service is not configured (missing VAULT_SERVICE_HOST)")
	}

	// Get expiration periods from vault service
	expirePeriod := vaultService.GetExpirePeriod()
	transferTimeoutPeriod := vaultService.GetTransferTimeoutPeriod()

	log.WithField("expire_period", expirePeriod).
		WithField("transfer_timeout_period", transferTimeoutPeriod).
		Info("starting vault reap process")

	// Get expired resources
	resources, err := vaultModels.GetExpiredResources(ctx, db, expirePeriod, transferTimeoutPeriod)
	if err != nil {
		return errors.Wrap(err, "failed to get expired resources")
	}

	log.WithField("count", len(resources)).Info("found expired resources")

	// Process each resource
	for _, resource := range resources {
		log.WithField("resource_id", resource.ResourceID).Info("processing resource")

		// Get all pledges for this resource
		pledges, err := vaultModels.GetResourcePledges(ctx, db, resource.ResourceID)
		if err != nil {
			log.WithError(err).
				WithField("resource_id", resource.ResourceID).
				Warn("failed to get resource pledges, skipping resource")
			continue
		}

		log.WithField("resource_id", resource.ResourceID).
			WithField("pledge_count", len(pledges)).
			Info("found pledges for resource")

		// Remove all pledges through vault service
		for _, pledge := range pledges {
			err = vaultService.RemovePledge(ctx, &pledge)
			if err != nil {
				log.WithError(err).
					WithField("resource_id", resource.ResourceID).
					WithField("pledge_id", pledge.PledgeID).
					Warn("failed to remove pledge, skipping")
				continue
			}

			log.WithField("resource_id", resource.ResourceID).
				WithField("pledge_id", pledge.PledgeID).
				WithField("user_id", pledge.UserID).
				WithField("amount", pledge.Amount).
				Info("removed pledge")
		}

		// Delete the resource
		err = vaultService.RemoveResource(ctx, resource.ResourceID)
		if err != nil {
			log.WithError(err).
				WithField("resource_id", resource.ResourceID).
				Warn("failed to delete resource")
			continue
		}

		log.WithField("resource_id", resource.ResourceID).Info("deleted resource")
	}

	log.Info("vault reap process completed")
	return nil
}
