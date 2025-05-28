package main

import (
	"context"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"net/http"
)

func makeEnrichCMD() cli.Command {
	enrichCMD := cli.Command{
		Name:    "enrich",
		Aliases: []string{"e"},
		Usage:   "Enriches torrents with metadata",
		Action:  enrich,
	}
	configureEnrich(&enrichCMD)
	return enrichCMD
}

func configureEnrich(c *cli.Command) {
	c.Flags = append(c.Flags,
		cli.BoolFlag{
			Name:  "force",
			Usage: "force enrichment",
		},
		cli.BoolFlag{
			Name:  "force-error",
			Usage: "force error enrichment",
		},
	)
	c.Flags = append(c.Flags,
		cli.StringFlag{
			Name:  "id",
			Usage: "id for enrichment",
		},
	)
	c.Flags = cs.RegisterPGFlags(c.Flags)
	c.Flags = api.RegisterFlags(c.Flags)
	c.Flags = configureEnricher(c.Flags)
}

func enrich(c *cli.Context) error {
	force := c.Bool("force")
	forceError := c.Bool("force-error")
	if forceError {
		force = true
	}
	id := c.String("id")
	// Setting DB
	pg := cs.NewPG(c)
	defer pg.Close()

	// Setting Migrations
	m := cs.NewPGMigration(pg)
	err := m.Run()
	if err != nil {
		return err
	}
	db := pg.Get()
	if db == nil {
		return errors.New("db is nil")
	}

	// Setting HTTP Client
	cl := http.DefaultClient

	// Setting Webtor API
	sapi := api.New(c, cl)

	// Setting Enricher
	en := makeEnricher(c, cl, pg, sapi)

	var resources []*models.TorrentResource
	ctx := context.Background()
	if id != "" {
		r, err := models.GetResourceByID(ctx, db, id)
		if err != nil {
			return err
		} else {
			resources = append(resources, r)
		}
	} else {
		if forceError {
			resources, err = models.GetErrorResources(ctx, db)
		} else if force {
			resources, err = models.GetAllResources(ctx, db)
		} else {
			resources, err = models.GetResourcesWithoutMediaInfo(ctx, db)
		}
	}
	if err != nil {
		return err
	}
	for _, resource := range resources {
		err = en.Enrich(ctx, resource.ResourceID, &api.Claims{}, force)
		if err != nil {
			return err
		}
	}
	return nil
}
