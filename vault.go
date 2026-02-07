package main

import (
	"context"
	"net/http"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/notification"
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
	c.Flags = common.RegisterFlags(c.Flags)
}

func reap(c *cli.Context) error {
	ctx := context.Background()

	// Initialize services
	vaultService, notificationService, db, err := initializeServices(c)
	if err != nil {
		return err
	}

	// Get configuration
	expirePeriod := c.Duration(vault.VaultResourceExpirePeriodFlag)
	transferTimeoutPeriod := c.Duration(vault.VaultResourceTransferTimeoutPeriodFlag)

	log.WithField("expire_period", expirePeriod).
		WithField("transfer_timeout_period", transferTimeoutPeriod).
		Info("starting vault reap process")

	// Process transfer timeouts
	err = processTransferTimeouts(ctx, db, vaultService, notificationService, transferTimeoutPeriod)
	if err != nil {
		log.WithError(err).Warn("failed to process transfer timeouts")
	}

	// Process expirations
	err = processExpirations(ctx, db, vaultService, notificationService, expirePeriod)
	if err != nil {
		log.WithError(err).Warn("failed to process expirations")
	}

	log.Info("vault reap process completed")
	return nil
}

func initializeServices(c *cli.Context) (*vault.Vault, *notification.Service, *pg.DB, error) {
	// Setting DB
	pg := cs.NewPG(c)

	// Setting Migrations
	m := cs.NewPGMigration(pg)
	err := m.Run()
	if err != nil {
		pg.Close()
		return nil, nil, nil, errors.Wrap(err, "failed to run migrations")
	}

	db := pg.Get()
	if db == nil {
		pg.Close()
		return nil, nil, nil, errors.New("db is nil")
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
		pg.Close()
		return nil, nil, nil, errors.New("vault service is not configured (missing VAULT_SERVICE_HOST)")
	}

	// Setting Notification Service
	notificationService := notification.New(c, db)

	return vaultService, notificationService, db, nil
}

func processTransferTimeouts(ctx context.Context, db *pg.DB, vaultService *vault.Vault, notificationService *notification.Service, transferTimeoutPeriod time.Duration) error {
	// Get resources with transfer timeout
	var resources []vaultModels.Resource
	now := time.Now()
	transferThreshold := now.Add(-transferTimeoutPeriod)

	err := db.Model(&resources).
		Context(ctx).
		Where("funded_at IS NOT NULL AND funded_at < ? AND vaulted = false AND expired_at IS NULL", transferThreshold).
		Select()
	if err != nil {
		return errors.Wrap(err, "failed to get transfer timeout resources")
	}

	log.WithField("count", len(resources)).Info("found transfer timeout resources")

	// Process each resource
	for _, resource := range resources {
		processResource(ctx, db, vaultService, notificationService, resource, true)
	}

	return nil
}

func processExpirations(ctx context.Context, db *pg.DB, vaultService *vault.Vault, notificationService *notification.Service, expirePeriod time.Duration) error {
	// Get expired resources
	var resources []vaultModels.Resource
	now := time.Now()
	expireThreshold := now.Add(-expirePeriod)

	err := db.Model(&resources).
		Context(ctx).
		Where("expired_at IS NOT NULL AND expired_at < ?", expireThreshold).
		Select()
	if err != nil {
		return errors.Wrap(err, "failed to get expired resources")
	}

	log.WithField("count", len(resources)).Info("found expired resources")

	// Process each resource
	for _, resource := range resources {
		processResource(ctx, db, vaultService, notificationService, resource, false)
	}

	return nil
}

func processResource(ctx context.Context, db *pg.DB, vaultService *vault.Vault, notificationService *notification.Service, resource vaultModels.Resource, isTransferTimeout bool) {
	log.WithField("resource_id", resource.ResourceID).Info("processing resource")

	// Get all pledges for this resource with user information
	pledges, err := vaultModels.GetResourcePledgesWithUsers(ctx, db, resource.ResourceID)
	if err != nil {
		log.WithError(err).
			WithField("resource_id", resource.ResourceID).
			Warn("failed to get resource pledges, skipping resource")
		return
	}

	log.WithField("resource_id", resource.ResourceID).
		WithField("pledge_count", len(pledges)).
		Info("found pledges for resource")

	// Remove all pledges and send notifications
	for _, pledge := range pledges {
		removePledgeAndNotify(ctx, vaultService, notificationService, pledge, resource, isTransferTimeout)
	}

	// Delete the resource
	err = vaultService.RemoveResource(ctx, resource.ResourceID)
	if err != nil {
		log.WithError(err).
			WithField("resource_id", resource.ResourceID).
			Warn("failed to delete resource")
		return
	}

	log.WithField("resource_id", resource.ResourceID).Info("deleted resource")
}

func removePledgeAndNotify(ctx context.Context, vaultService *vault.Vault, notificationService *notification.Service, pledge vaultModels.Pledge, resource vaultModels.Resource, isTransferTimeout bool) {
	// Remove pledge
	err := vaultService.RemovePledge(ctx, &pledge)
	if err != nil {
		log.WithError(err).
			WithField("resource_id", resource.ResourceID).
			WithField("pledge_id", pledge.PledgeID).
			Warn("failed to remove pledge, skipping")
		return
	}

	log.WithField("resource_id", resource.ResourceID).
		WithField("pledge_id", pledge.PledgeID).
		WithField("user_id", pledge.UserID).
		WithField("amount", pledge.Amount).
		Info("removed pledge")

	// Send notification to user if user data is available
	if pledge.User == nil || pledge.User.Email == "" {
		return
	}

	if isTransferTimeout {
		sendTransferTimeoutNotification(notificationService, pledge.User.Email, resource)
	} else {
		sendExpiredNotification(notificationService, pledge.User.Email, resource)
	}
}

func sendTransferTimeoutNotification(notificationService *notification.Service, email string, resource vaultModels.Resource) {
	err := notificationService.SendTransferTimeout(email, &resource)
	if err != nil {
		log.WithError(err).
			WithField("resource_id", resource.ResourceID).
			WithField("user_email", email).
			Warn("failed to send transfer timeout notification")
	} else {
		log.WithField("resource_id", resource.ResourceID).
			WithField("user_email", email).
			Info("sent transfer timeout notification")
	}
}

func sendExpiredNotification(notificationService *notification.Service, email string, resource vaultModels.Resource) {
	err := notificationService.SendExpired(email, &resource)
	if err != nil {
		log.WithError(err).
			WithField("resource_id", resource.ResourceID).
			WithField("user_email", email).
			Warn("failed to send expiration notification")
	} else {
		log.WithField("resource_id", resource.ResourceID).
			WithField("user_email", email).
			Info("sent expiration notification")
	}
}
