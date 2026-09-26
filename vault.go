package main

import (
	"context"
	"net/http"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/notification"
	"github.com/webtor-io/web-ui/services/vault"
)

// Interfaces for testability
type reaperVault interface {
	RemovePledge(ctx context.Context, pledge *vaultModels.Pledge) error
	RemoveResource(ctx context.Context, resourceID string) error
	UpdateUserVP(ctx context.Context, user *auth.User) (*vaultModels.UserVP, error)
}

// reaperClaims answers what Vault balance a user's claims grant right now.
type reaperClaims interface {
	Points(user *auth.User) (*float64, error)
}

type claimsPoints struct {
	cl *claims.Claims
}

func (c *claimsPoints) Points(user *auth.User) (*float64, error) {
	d, err := c.cl.Get(&claims.Request{Email: user.Email, PatreonUserID: user.PatreonUserID})
	if err != nil {
		return nil, err
	}
	return vault.PointsFromClaims(d), nil
}

type reaperNotification interface {
	SendTransferTimeout(to string, userID uuid.UUID, r *vaultModels.Resource) error
	SendExpired(to string, userID uuid.UUID, r *vaultModels.Resource) error
}

type reaperStore interface {
	GetExpiredResources(ctx context.Context, expirePeriod time.Duration, abandonedExpirePeriod time.Duration, transferTimeoutPeriod time.Duration) ([]vaultModels.Resource, error)
	GetResourcePledgesWithUsers(ctx context.Context, resourceID string) ([]vaultModels.Pledge, error)
	GetGhostResources(ctx context.Context) ([]vaultModels.Resource, error)
	GetUserVPsWithFundedPledges(ctx context.Context) ([]vaultModels.UserVP, error)
}

// pgReaperStore wraps *pg.DB to implement reaperStore
type pgReaperStore struct {
	db *pg.DB
}

func (s *pgReaperStore) GetExpiredResources(ctx context.Context, expirePeriod time.Duration, abandonedExpirePeriod time.Duration, transferTimeoutPeriod time.Duration) ([]vaultModels.Resource, error) {
	return vaultModels.GetExpiredResources(ctx, s.db, expirePeriod, abandonedExpirePeriod, transferTimeoutPeriod)
}

func (s *pgReaperStore) GetResourcePledgesWithUsers(ctx context.Context, resourceID string) ([]vaultModels.Pledge, error) {
	return vaultModels.GetResourcePledgesWithUsers(ctx, s.db, resourceID)
}

func (s *pgReaperStore) GetUserVPsWithFundedPledges(ctx context.Context) ([]vaultModels.UserVP, error) {
	return vaultModels.GetUserVPsWithFundedPledges(ctx, s.db)
}

func (s *pgReaperStore) GetGhostResources(ctx context.Context) ([]vaultModels.Resource, error) {
	return vaultModels.GetGhostResources(ctx, s.db)
}

type reaper struct {
	store                 reaperStore
	vault                 reaperVault
	notification          reaperNotification
	claims                reaperClaims
	maxVPDrops            int
	expirePeriod          time.Duration
	abandonedExpirePeriod time.Duration
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

const vaultVPResyncMaxDropsFlag = "vault-vp-resync-max-drops"

func configureVaultReap(c *cli.Command) {
	c.Flags = append(c.Flags, cli.IntFlag{
		Name:   vaultVPResyncMaxDropsFlag,
		Usage:  "the most balances one reap may lower; more than that is taken for a claims failure and nothing is changed",
		Value:  200,
		EnvVar: "VAULT_VP_RESYNC_MAX_DROPS",
	})
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
		WithField("abandoned_expire_period", r.abandonedExpirePeriod).
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
	notificationService := notification.New(c, db, newI18n())

	r := &reaper{
		store:                 &pgReaperStore{db: db},
		vault:                 vaultService,
		notification:          notificationService,
		claims:                &claimsPoints{cl: claimsService},
		maxVPDrops:            c.Int(vaultVPResyncMaxDropsFlag),
		expirePeriod:          c.Duration(vault.VaultResourceExpirePeriodFlag),
		abandonedExpirePeriod: c.Duration(vault.VaultResourceAbandonedExpirePeriodFlag),
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
	// First, so the pledges it defunds start their expire period now and
	// not an hour later.
	r.resyncUserVP(ctx)

	resources, err := r.store.GetExpiredResources(ctx, r.expirePeriod, r.abandonedExpirePeriod, r.transferTimeoutPeriod)
	if err != nil {
		log.WithError(err).Warn("failed to get expired resources")
		return
	}

	log.WithField("count", len(resources)).Info("found expired resources")

	for _, resource := range resources {
		r.processResource(ctx, resource)
	}

	// Clean up ghost resources — funded_vp > 0 but no funded pledges
	// (caused by user account deletion cascading pledges but not updating resource)
	r.reapGhostResources(ctx)
}

// resyncUserVP brings the balance of everyone holding funded pledges back in
// line with their claims. A balance changes only on user.updated or a visit
// to /vault, and a membership can end without either: the 2026-07-13 matview
// fix took bronze from expired trials without an event, a billing membership
// runs out by date, a Patreon member ages out of the matview's window. Those
// accounts kept their Vault space for months (117 of them, 1.5 TB on
// 2026-09-25). UpdateUserVP defunds what no longer fits, and the pledges go
// the ordinary way: expired now, reaped after the expire period, restored if
// the tier comes back before that.
//
// A lost balance ends in content deleted from S3, so the run is checked
// before it is applied: if claims would lower more than maxVPDrops balances
// at once, that is a broken claims source (an emptied matview, a dropped
// view) and not a wave of cancellations, and nothing is changed. A user whose
// claims cannot be fetched is skipped, never read as free.
func (r *reaper) resyncUserVP(ctx context.Context) {
	if r.claims == nil {
		return
	}
	vps, err := r.store.GetUserVPsWithFundedPledges(ctx)
	if err != nil {
		log.WithError(err).Warn("failed to get user balances for resync")
		return
	}
	var stale []*auth.User
	drops := 0
	for i := range vps {
		vp := &vps[i]
		if vp.User == nil {
			continue
		}
		u := &auth.User{
			ID:            vp.User.UserID,
			Email:         vp.User.Email,
			PatreonUserID: vp.User.PatreonUserID,
		}
		points, err := r.claims.Points(u)
		if err != nil {
			log.WithError(err).WithField("user_id", u.ID).Warn("failed to get claims for balance resync, skipping user")
			continue
		}
		if pointsEqual(vp.Total, points) {
			continue
		}
		if pointsLowered(vp.Total, points) {
			drops++
		}
		stale = append(stale, u)
	}
	if drops > r.maxVPDrops {
		log.WithField("drops", drops).
			WithField("max_drops", r.maxVPDrops).
			WithField("checked", len(vps)).
			Error("balance resync would lower more balances than allowed, claims look broken; nothing changed")
		return
	}
	for _, u := range stale {
		vp, err := r.vault.UpdateUserVP(ctx, u)
		if err != nil {
			log.WithError(err).WithField("user_id", u.ID).Warn("failed to resync user balance")
			continue
		}
		l := log.WithField("user_id", u.ID)
		if vp != nil && vp.Total != nil {
			l = l.WithField("total", *vp.Total)
		}
		l.Info("resynced user balance")
	}
	log.WithField("checked", len(vps)).
		WithField("changed", len(stale)).
		WithField("lowered", drops).
		Info("balance resync done")
}

func pointsEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// pointsLowered: nil is unlimited, so any number is lower than it.
func pointsLowered(from, to *float64) bool {
	if to == nil {
		return false
	}
	return from == nil || *to < *from
}

func (r *reaper) reapGhostResources(ctx context.Context) {
	resources, err := r.store.GetGhostResources(ctx)
	if err != nil {
		log.WithError(err).Warn("failed to get ghost resources")
		return
	}

	if len(resources) == 0 {
		return
	}

	log.WithField("count", len(resources)).Info("found ghost resources (funded but no pledges)")

	for _, resource := range resources {
		log.WithField("resource_id", resource.ResourceID).
			WithField("name", resource.Name).
			WithField("funded_vp", resource.FundedVP).
			Info("removing ghost resource")

		// A ghost can still have pledges -- unfunded ones. The account-deletion
		// case this was written for has none (they cascade away with the user),
		// but a pledger who merely lost their points leaves one behind, and
		// then RemoveResource alone did the worst of both: it deleted the
		// content from the Vault, failed on pledge_resource_fk, and left the
		// row saying "vaulted" -- every hour, from 2026-08-29 to 2026-09-21
		// for the one resource it happened to. So: the same path as any other
		// reaped resource. "Expired" is the message, not "transfer timeout":
		// the content was stored, its funding went away.
		r.reapResource(ctx, resource, false)
	}
}

func (r *reaper) processResource(ctx context.Context, resource vaultModels.Resource) {
	r.reapResource(ctx, resource, resource.ExpiredAt == nil)
}

// reapResource removes the pledges (each pledger is told), then the resource.
// In that order: vault.pledge references the resource ON DELETE RESTRICT.
func (r *reaper) reapResource(ctx context.Context, resource vaultModels.Resource, isTransferTimeout bool) {

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
	if pledge.User == nil {
		return
	}
	// No notification.Deliverable guard on addr, on purpose. Both sends
	// below go through notification.Service.Send, which writes the feed
	// entry unconditionally and decides about mail on its own -- it fills
	// the To column only for a deliverable address and never opens an SMTP
	// connection without one. Returning early here would skip the feed
	// entry too, so the self-hosted admin (whose address is the sentinel
	// "admin") would lose a resource and never be told. Do not "restore"
	// the check.
	addr := notification.RecipientEmail(pledge.User.Email, pledge.User.NotificationEmail)

	r.sendNotification(addr, pledge.UserID, resource, isTransferTimeout)
}

func (r *reaper) sendNotification(email string, userID uuid.UUID, resource vaultModels.Resource, isTransferTimeout bool) {
	var err error
	var action string
	if isTransferTimeout {
		err = r.notification.SendTransferTimeout(email, userID, &resource)
		action = "transfer timeout"
	} else {
		err = r.notification.SendExpired(email, userID, &resource)
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
