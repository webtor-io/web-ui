package main

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	_ "github.com/go-pg/pg/v10/orm"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/notification"
	vService "github.com/webtor-io/web-ui/services/vault"
)

func makeNotificationCMD() cli.Command {
	notificationCMD := cli.Command{
		Name:    "notification",
		Aliases: []string{"n"},
		Usage:   "Notification management commands",
	}
	configureNotification(&notificationCMD)
	return notificationCMD
}

func configureNotification(c *cli.Command) {
	sendCmd := cli.Command{
		Name:    "send",
		Aliases: []string{"s"},
		Usage:   "Sends notifications about expiring resources",
		Action:  sendExpiringNotifications,
	}
	configureNotificationSend(&sendCmd)
	c.Subcommands = []cli.Command{sendCmd}
}

func configureNotificationSend(c *cli.Command) {
	c.Flags = cs.RegisterPGFlags(c.Flags)
	c.Flags = common.RegisterFlags(c.Flags)
	c.Flags = api.RegisterFlags(c.Flags)
	c.Flags = claims.RegisterClientFlags(c.Flags)
	c.Flags = vService.RegisterApiFlags(c.Flags)
	c.Flags = vService.RegisterFlags(c.Flags)
}

func sendExpiringNotifications(c *cli.Context) error {
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

	// Setting Notification
	ns := notification.New(c, db)

	expirePeriod := c.Duration(vService.VaultResourceExpirePeriodFlag)

	for _, days := range []int{7, 3, 1} {
		err := sendExpiringNotificationsByDays(ctx, db, expirePeriod, ns, days)
		if err != nil {
			log.WithError(err).WithField("days", days).Error("failed to send expiring notifications")
		}
	}

	return nil
}

func sendExpiringNotificationsByDays(ctx context.Context, db *pg.DB, expirePeriod time.Duration, ns *notification.Service, days int) error {
	log.WithField("days", days).Info("processing expiring notifications")

	startTime := time.Now().Add(time.Duration(days-1) * 24 * time.Hour)
	endTime := time.Now().Add(time.Duration(days) * 24 * time.Hour)

	// 1. Get all resources expiring in the range [days-1, days] from now
	// Logic: expire_at + expirePeriod
	var resources []vault.Resource
	err := db.Model(&resources).
		Context(ctx).
		WhereGroup(func(q *pg.Query) (*pg.Query, error) {
			q = q.WhereOr("expired_at IS NOT NULL AND expired_at + ? * interval '1 microsecond' BETWEEN ? AND ?",
				expirePeriod.Microseconds(), startTime, endTime)
			return q, nil
		}).
		Select()

	if err != nil {
		return errors.Wrap(err, "failed to get resources for notification")
	}

	if len(resources) == 0 {
		return nil
	}

	// 2. Get unique users
	resourceIDs := make([]string, len(resources))
	for i, r := range resources {
		resourceIDs[i] = r.ResourceID
	}

	var pledges []vault.Pledge
	err = db.Model(&pledges).
		Context(ctx).
		Relation("User").
		Where("resource_id IN (?)", pg.In(resourceIDs)).
		Select()

	if err != nil {
		return errors.Wrap(err, "failed to get pledges for notification")
	}

	userEmails := make(map[string]string)

	for _, p := range pledges {
		if p.User == nil || p.User.Email == "" {
			continue
		}
		userID := p.UserID.String()
		userEmails[userID] = p.User.Email
	}

	for userID, email := range userEmails {
		var userPledges []vault.Pledge
		err = db.Model(&userPledges).
			Context(ctx).
			Relation("Resource").
			Where("pledge.user_id = ?", userID).
			WhereGroup(func(q *pg.Query) (*pg.Query, error) {
				q = q.WhereOr("Resource.expired_at IS NOT NULL AND Resource.expired_at + ? * interval '1 microsecond' BETWEEN ? AND ?",
					expirePeriod.Microseconds(), startTime, endTime)
				return q, nil
			}).
			Select()

		if err != nil {
			log.WithError(err).WithField("user_id", userID).Error("failed to get user pledges for notification")
			continue
		}

		if len(userPledges) == 0 {
			continue
		}

		var expiring []vault.Resource
		for _, up := range userPledges {
			if up.Resource != nil {
				expiring = append(expiring, *up.Resource)
			}
		}

		if len(expiring) > 0 {
			err = ns.SendExpiring(email, days, expiring)
			if err != nil {
				log.WithError(err).WithField("email", email).Error("failed to send expiring notification")
			}
		}
	}
	return nil
}
