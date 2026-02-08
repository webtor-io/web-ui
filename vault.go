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

// Interfaces for testability
type reaperVault interface {
	RemovePledge(ctx context.Context, pledge *vaultModels.Pledge) error
	RemoveResource(ctx context.Context, resourceID string) error
}

type reaperNotification interface {
	SendTransferTimeout(to string, r *vaultModels.Resource) error
	SendExpired(to string, r *vaultModels.Resource) error
}

type reaperStore interface {
	GetExpiredResources(ctx context.Context, expirePeriod time.Duration, transferTimeoutPeriod time.Duration) ([]vaultModels.Resource, error)
	GetResourcePledgesWithUsers(ctx context.Context, resourceID string) ([]vaultModels.Pledge, error)
}

// pgReaperStore wraps *pg.DB to implement reaperStore
type pgReaperStore struct {
	db *pg.DB
}

func (s *pgReaperStore) GetExpiredResources(ctx context.Context, expirePeriod time.Duration, transferTimeoutPeriod time.Duration) ([]vaultModels.Resource, error) {
	return vaultModels.GetExpiredResources(ctx, s.db, expirePeriod, transferTimeoutPeriod)
}

func (s *pgReaperStore) GetResourcePledgesWithUsers(ctx context.Context, resourceID string) ([]vaultModels.Pledge, error) {
	return vaultModels.GetResourcePledgesWithUsers(ctx, s.db, resourceID)
}

type reaper struct {
	store                 reaperStore
	vault                 reaperVault
	notification          reaperNotification
	expirePeriod          time.Duration
	transferTimeoutPeriod time.Duration
	pg                    *cs.PG
	cpCl                  *claims.Client
}

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
	r, err := initializeReaper(c)
	if err != nil {
		return err
	}
	defer r.Close()

	log.WithField("expire_period", r.expirePeriod).
		WithField("transfer_timeout_period", r.transferTimeoutPeriod).
		Info("starting vault reap process")

	r.run(ctx)

	log.Info("vault reap process completed")
	return nil
}

func initializeReaper(c *cli.Context) (*reaper, error) {
	// Setting DB
	pg := cs.NewPG(c)

	// Setting Migrations
	m := cs.NewPGMigration(pg)
	err := m.Run()
	if err != nil {
		pg.Close()
		return nil, errors.Wrap(err, "failed to run migrations")
	}

	db := pg.Get()
	if db == nil {
		pg.Close()
		return nil, errors.New("db is nil")
	}

	// Setting HTTP Client
	cl := http.DefaultClient

	// Setting Webtor API
	sapi := api.New(c, cl)

	// Setting Claims Client
	cpCl := claims.NewClient(c)

	// Setting Claims
	claimsService := claims.New(c, cpCl, pg)

	// Setting Vault API
	vaultApi := vault.NewApi(c, cl)

	// Setting Vault
	vaultService := vault.New(c, vaultApi, claimsService, cl, pg, sapi)
	if vaultService == nil {
		pg.Close()
		if cpCl != nil {
			cpCl.Close()
		}
		return nil, errors.New("vault service is not configured (missing VAULT_SERVICE_HOST)")
	}

	// Setting Notification Service
	notificationService := notification.New(c, db)

	r := &reaper{
		store:                 &pgReaperStore{db: db},
		vault:                 vaultService,
		notification:          notificationService,
		expirePeriod:          c.Duration(vault.VaultResourceExpirePeriodFlag),
		transferTimeoutPeriod: c.Duration(vault.VaultResourceTransferTimeoutPeriodFlag),
		pg:                    pg,
		cpCl:                  cpCl,
	}

	return r, nil
}

func (r *reaper) Close() {
	r.pg.Close()
	r.cpCl.Close()
}

func (r *reaper) run(ctx context.Context) {
	resources, err := r.store.GetExpiredResources(ctx, r.expirePeriod, r.transferTimeoutPeriod)
	if err != nil {
		log.WithError(err).Warn("failed to get expired resources")
		return
	}

	log.WithField("count", len(resources)).Info("found expired resources")

	for _, resource := range resources {
		r.processResource(ctx, resource)
	}
}

func (r *reaper) processResource(ctx context.Context, resource vaultModels.Resource) {
	isTransferTimeout := resource.ExpiredAt == nil

	log.WithField("resource_id", resource.ResourceID).
		WithField("is_transfer_timeout", isTransferTimeout).
		Info("processing resource")

	// Get all pledges for this resource with user information
	pledges, err := r.store.GetResourcePledgesWithUsers(ctx, resource.ResourceID)
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
		r.removePledgeAndNotify(ctx, pledge, resource, isTransferTimeout)
	}

	// Delete the resource
	err = r.vault.RemoveResource(ctx, resource.ResourceID)
	if err != nil {
		log.WithError(err).
			WithField("resource_id", resource.ResourceID).
			Warn("failed to delete resource")
		return
	}

	log.WithField("resource_id", resource.ResourceID).Info("deleted resource")
}

func (r *reaper) removePledgeAndNotify(ctx context.Context, pledge vaultModels.Pledge, resource vaultModels.Resource, isTransferTimeout bool) {
	// Remove pledge
	err := r.vault.RemovePledge(ctx, &pledge)
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

	r.sendNotification(pledge.User.Email, resource, isTransferTimeout)
}

func (r *reaper) sendNotification(email string, resource vaultModels.Resource, isTransferTimeout bool) {
	var err error
	var action string
	if isTransferTimeout {
		err = r.notification.SendTransferTimeout(email, &resource)
		action = "transfer timeout"
	} else {
		err = r.notification.SendExpired(email, &resource)
		action = "expiration"
	}

	if err != nil {
		log.WithError(err).
			WithField("resource_id", resource.ResourceID).
			WithField("user_email", email).
			Warn("failed to send " + action + " notification")
	} else {
		log.WithField("resource_id", resource.ResourceID).
			WithField("user_email", email).
			Info("sent " + action + " notification")
	}
}
