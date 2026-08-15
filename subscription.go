package main

import (
	"context"
	"net/http"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"

	ac "github.com/webtor-io/web-ui/services/anthropic_client"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/notification"
	rss "github.com/webtor-io/web-ui/services/release_subscription"
	rum "github.com/webtor-io/web-ui/services/request_url_mapper"
	stremios "github.com/webtor-io/web-ui/services/stremio"
	"github.com/webtor-io/web-ui/services/torznab"
)

func makeSubscriptionCMD() cli.Command {
	subscriptionCMD := cli.Command{
		Name:    "subscription",
		Aliases: []string{"sub"},
		Usage:   "Release subscription commands",
	}
	configureSubscription(&subscriptionCMD)
	return subscriptionCMD
}

func configureSubscription(c *cli.Command) {
	pollCmd := cli.Command{
		Name:    "poll",
		Aliases: []string{"p"},
		Usage:   "Checks due release subscriptions for new releases and mails what is new",
		Action:  pollSubscriptions,
	}
	configureSubscriptionPoll(&pollCmd)
	c.Subcommands = []cli.Command{pollCmd}
}

func configureSubscriptionPoll(c *cli.Command) {
	c.Flags = cs.RegisterPGFlags(c.Flags)
	c.Flags = common.RegisterFlags(c.Flags)
	c.Flags = api.RegisterFlags(c.Flags)
	c.Flags = claims.RegisterClientFlags(c.Flags)
	// The poll runs the user's own stream pipeline, so it needs everything
	// that pipeline needs: addon client settings, indexer settings, and the
	// URL mapper that rewrites addon hosts.
	c.Flags = stremios.RegisterClientFlags(c.Flags)
	c.Flags = torznab.RegisterFlags(c.Flags)
	c.Flags = rum.RegisterFlags(c.Flags)
	c.Flags = ac.RegisterFlags(c.Flags)
	c.Flags = configureEnricher(c.Flags)
	c.Flags = rss.RegisterPollFlags(c.Flags)
}

func pollSubscriptions(c *cli.Context) error {
	ctx := context.Background()

	pg := cs.NewPG(c)
	defer pg.Close()

	m := cs.NewPGMigration(pg)
	if err := m.Run(); err != nil {
		return errors.Wrap(err, "failed to run migrations")
	}

	db := pg.Get()
	if db == nil {
		return errors.New("db is nil")
	}

	cl := http.DefaultClient
	sapi := api.New(c, cl)

	claimsCl := claims.NewClient(c)
	defer claimsCl.Close()
	claimsSvc := claims.New(c, claimsCl, pg)

	requestURLMapper, err := rum.NewRequestURLMapper(c)
	if err != nil {
		return err
	}

	// The same builder the web process uses, minus the halves a cron job
	// cannot supply — see Builder.BuildPollStreamsService.
	torznabCl := torznab.New(c)
	torznabTitles := torznab.NewCinemetaTitles(torznabCl.HTTP(), c.String(torznab.UserAgentFlag))
	sb := stremios.NewBuilder(c, pg, stremios.NewClient(c), sapi, requestURLMapper, torznabCl, torznabTitles)

	// The enricher answers one question here: is this series still in
	// production. That is what decides whether a season subscription has a
	// future or is finished.
	anthropicCl := ac.New(c)
	en := makeEnricher(c, cl, pg, sapi, anthropicCl)

	ns := notification.New(c, db, newI18n())

	poller := rss.NewPoller(
		rss.NewStore(pg),
		rss.NewBuilderSearch(sb),
		ns,
		rss.NewClaimsTier(claimsSvc),
		en,
		rss.NewPollConfig(c),
	)

	n, err := poller.Run(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to poll release subscriptions")
	}
	log.WithField("subscriptions", n).Info("release subscription poll completed")
	return nil
}
