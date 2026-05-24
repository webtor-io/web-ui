package main

import (
	"context"
	"net/http"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	ac "github.com/webtor-io/web-ui/services/anthropic_client"
	"github.com/webtor-io/web-ui/services/api"
)

func makeEnrichCMD() cli.Command {
	enrichCMD := cli.Command{
		Name:    "enrich",
		Aliases: []string{"e"},
		Usage:   "Enriches content with metadata",
	}
	configureEnrich(&enrichCMD)
	return enrichCMD
}

func configureEnrich(c *cli.Command) {
	runCmd := cli.Command{
		Name:   "run",
		Usage:  "Enriches specific torrent resources with metadata",
		Action: enrich,
	}
	runCmd.Flags = append(runCmd.Flags,
		cli.BoolFlag{
			Name:  "force",
			Usage: "force enrichment",
		},
		cli.BoolFlag{
			Name:  "force-error",
			Usage: "force error enrichment",
		},
		cli.BoolFlag{
			Name:  "metadata-only",
			Usage: "only populate resource_metadata (classification + parsed-name) for resources missing it; skip mapper/AI work",
		},
		cli.StringFlag{
			Name:  "id",
			Usage: "id for enrichment",
		},
	)
	runCmd.Flags = cs.RegisterPGFlags(runCmd.Flags)
	runCmd.Flags = api.RegisterFlags(runCmd.Flags)
	runCmd.Flags = ac.RegisterFlags(runCmd.Flags)
	runCmd.Flags = configureEnricher(runCmd.Flags)

	popularCmd := cli.Command{
		Name:   "popular",
		Usage:  "Fetches popular recent films from metadata providers into the DB cache",
		Action: enrichPopular,
	}
	popularCmd.Flags = cs.RegisterPGFlags(popularCmd.Flags)
	popularCmd.Flags = ac.RegisterFlags(popularCmd.Flags)
	popularCmd.Flags = configureEnricher(popularCmd.Flags)
	popularCmd.Flags = append(popularCmd.Flags,
		cli.StringFlag{
			Name:   "release-date-gte",
			Usage:  "minimum release date (YYYY-MM-DD)",
			Value:  "2025-01-01",
			EnvVar: "ENRICH_POPULAR_RELEASE_DATE_GTE",
		},
		cli.IntFlag{
			Name:   "limit",
			Usage:  "max number of films to fetch",
			Value:  300,
			EnvVar: "ENRICH_POPULAR_LIMIT",
		},
		cli.BoolFlag{
			Name:  "force",
			Usage: "re-fetch and update all films even if already cached (useful after adding new metadata fields like credits)",
		},
	)

	c.Subcommands = []cli.Command{runCmd, popularCmd}
}

func enrichPopular(c *cli.Context) error {
	releaseDateGte := c.String("release-date-gte")
	limit := c.Int("limit")
	force := c.Bool("force")

	pg := cs.NewPG(c)
	defer pg.Close()

	m := cs.NewPGMigration(pg)
	if err := m.Run(); err != nil {
		return err
	}

	cl := http.DefaultClient
	// api.Api is not needed for popular — only the enricher's metadata
	// mappers, which are wired through makeEnricher. We pass a nil api
	// since popular flow never hits the REST API. The AI resolver also
	// stays disabled here: popular cron deals with already-known TMDB
	// ids by definition.
	en := makeEnricher(c, cl, pg, nil, ac.New(c))

	log.WithFields(log.Fields{
		"release_date_gte": releaseDateGte,
		"limit":            limit,
		"force":            force,
	}).Info("starting enrich popular")

	ctx := context.Background()
	if err := en.RefreshPopular(ctx, releaseDateGte, limit, force); err != nil {
		return errors.Wrap(err, "enrich popular failed")
	}

	log.Info("enrich popular completed")
	return nil
}

func enrich(c *cli.Context) error {
	force := c.Bool("force")
	forceError := c.Bool("force-error")
	metadataOnly := c.Bool("metadata-only")
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

	// Setting Enricher (with optional AI fallback wired from --ai-enrich-* flags)
	en := makeEnricher(c, cl, pg, sapi, ac.New(c))

	ctx := context.Background()

	// Metadata-only backfill — populates resource_metadata without
	// rerunning the full mapper/AI pipeline. Selection logic:
	//   --id          → just that one hash (re-classifies regardless)
	//   --force       → every media_info row (re-classifies all, used
	//                   after a parse_torrent_name change)
	//   default       → only rows missing a resource_metadata entry
	//                   (cheap incremental backfill)
	if metadataOnly {
		var hashes []string
		switch {
		case id != "":
			hashes = []string{id}
		case force:
			hashes, err = models.GetAllResourceIDsFromMediaInfo(ctx, db)
		default:
			hashes, err = models.GetResourceIDsWithoutResourceMetadata(ctx, db)
		}
		if err != nil {
			return err
		}
		log.WithField("count", len(hashes)).WithField("force", force).
			Info("metadata-only backfill: classifying resources")
		for _, h := range hashes {
			if mdErr := en.EnsureResourceMetadata(ctx, h, &api.Claims{}); mdErr != nil {
				// Per-resource failures are non-fatal so one bad hash
				// (e.g. torrent-store dropped the item) doesn't abort
				// the whole sweep.
				log.WithError(mdErr).WithField("hash", h).
					Warn("metadata-only backfill: classify failed")
			}
		}
		return nil
	}

	var resources []*models.TorrentResource
	if id != "" {
		r, err := models.GetResourceByID(ctx, db, id)
		if err != nil {
			return err
		}
		// torrent_resource is only populated when a resource lands in
		// Library; resources that were only streamed have no row.
		// Enrichment itself only needs the hash, so synthesize a
		// minimal TorrentResource and continue.
		if r == nil {
			log.WithField("id", id).Info("no torrent_resource row, enriching by hash directly")
			r = &models.TorrentResource{ResourceID: id}
		}
		resources = append(resources, r)
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
		err = en.Enrich(ctx, resource.ResourceID, &api.Claims{}, force, "")
		if err != nil {
			return err
		}
	}
	return nil
}
