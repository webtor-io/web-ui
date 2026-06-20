package main

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	gopg "github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"golang.org/x/sync/errgroup"

	"github.com/webtor-io/web-ui/models"
	ac "github.com/webtor-io/web-ui/services/anthropic_client"
	"github.com/webtor-io/web-ui/services/api"
	enr "github.com/webtor-io/web-ui/services/enrich"
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
		cli.IntFlag{
			Name:  "workers",
			Usage: "concurrent workers for --metadata-only backfill (default 4 — safe under in-cluster 2Gi pod limit; bump to 20+ locally where memory is unconstrained)",
			Value: 4,
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

	cleanupCmd := cli.Command{
		Name:   "cleanup-matches",
		Usage:  "Detach pre-guard fuzzy-false-positive metadata matches (e.g. \"01\" → \"0187 UFO\"). Dry-run by default; pass --apply to write.",
		Action: enrichCleanupMatches,
	}
	cleanupCmd.Flags = cs.RegisterPGFlags(cleanupCmd.Flags)
	cleanupCmd.Flags = append(cleanupCmd.Flags,
		cli.BoolFlag{
			Name:  "apply",
			Usage: "actually detach the false-positive matches; without this flag the command only reports (dry-run)",
		},
		cli.IntFlag{
			Name:  "sample",
			Usage: "how many example rejections to print",
			Value: 25,
		},
	)

	c.Subcommands = []cli.Command{runCmd, popularCmd, cleanupCmd}
}

// enrichCleanupMatches scans every movie/series row that carries a
// metadata link, re-evaluates it against the current enrichment guards
// (enr.IsRejectableMatch — the same weak-title + fuzzy-match rules the
// live pipeline uses), and detaches the false positives. The shared
// metadata rows are left intact; only the per-resource FK is nulled, so
// a later re-enrich can resolve the resource correctly.
//
// Dry-run by default — it reports the count and a sample and writes
// nothing unless --apply is passed.
func enrichCleanupMatches(c *cli.Context) error {
	apply := c.Bool("apply")
	sampleN := c.Int("sample")

	pg := cs.NewPG(c)
	defer pg.Close()
	db := pg.Get()
	if db == nil {
		return errors.New("db is nil")
	}
	ctx := context.Background()

	kinds := []struct {
		name   string
		list   func(context.Context, *gopg.DB) ([]models.MetadataMatchRow, error)
		detach func(context.Context, *gopg.DB, []uuid.UUID) (int, error)
	}{
		{"movie", models.ListMovieMetadataMatches, models.DetachMovieMetadata},
		{"series", models.ListSeriesMetadataMatches, models.DetachSeriesMetadata},
	}

	var grandTotal, grandReject int
	for _, k := range kinds {
		rows, err := k.list(ctx, db)
		if err != nil {
			return errors.Wrapf(err, "listing %s matches", k.name)
		}
		var rejectIDs []uuid.UUID
		var shown int
		for _, r := range rows {
			if !enr.IsRejectableMatch(r.QueryTitle, r.ResultTitle) {
				continue
			}
			rejectIDs = append(rejectIDs, r.ID)
			if shown < sampleN {
				log.WithFields(log.Fields{
					"kind":     k.name,
					"resource": r.ResourceID,
					"query":    r.QueryTitle,
					"year":     yearStr(r.QueryYear),
					"matched":  r.ResultTitle,
					"video_id": r.VideoID,
				}).Info("fuzzy false positive")
				shown++
			}
		}
		grandTotal += len(rows)
		grandReject += len(rejectIDs)
		log.WithFields(log.Fields{
			"kind":     k.name,
			"scanned":  len(rows),
			"rejected": len(rejectIDs),
		}).Info("cleanup-matches: scan complete")

		if apply && len(rejectIDs) > 0 {
			// Detach in batches to keep the IN (...) list and the
			// transaction footprint bounded.
			const batch = 1000
			var detached int
			for i := 0; i < len(rejectIDs); i += batch {
				end := i + batch
				if end > len(rejectIDs) {
					end = len(rejectIDs)
				}
				n, err := k.detach(ctx, db, rejectIDs[i:end])
				if err != nil {
					return errors.Wrapf(err, "detaching %s matches", k.name)
				}
				detached += n
			}
			log.WithFields(log.Fields{"kind": k.name, "detached": detached}).
				Info("cleanup-matches: detached")
		}
	}

	mode := "DRY-RUN (no changes written — pass --apply to detach)"
	if apply {
		mode = "APPLIED"
	}
	log.WithFields(log.Fields{
		"scanned":  grandTotal,
		"rejected": grandReject,
		"mode":     mode,
	}).Info("cleanup-matches: done")
	return nil
}

func yearStr(y *int16) string {
	if y == nil {
		return ""
	}
	return strconv.Itoa(int(*y))
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

		// Default 4 workers — observed sweet spot under the 2Gi pod
		// memory limit when this runs alongside the regular HTTP
		// server (`./server` is a single binary in two modes). 10
		// workers OOMKilled the pod within ~30 minutes because each
		// in-flight item list + ptn parse holds heap faster than Go's
		// GC reclaims it. Override via --workers when running locally
		// (memory unconstrained) to push throughput higher.
		workers := c.Int("workers")
		if workers < 1 {
			workers = 1
		}
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(workers)

		var done, failed atomic.Int64
		progressTicker := time.NewTicker(60 * time.Second)
		progressDone := make(chan struct{})
		go func() {
			defer close(progressDone)
			for {
				select {
				case <-progressTicker.C:
					log.WithFields(log.Fields{
						"done":   done.Load(),
						"total":  len(hashes),
						"failed": failed.Load(),
					}).Info("metadata-only backfill: progress")
				case <-gctx.Done():
					return
				}
			}
		}()

		for _, h := range hashes {
			h := h // capture
			g.Go(func() error {
				if mdErr := en.EnsureResourceMetadata(gctx, h, &api.Claims{}); mdErr != nil {
					failed.Add(1)
					log.WithError(mdErr).WithField("hash", h).
						Warn("metadata-only backfill: classify failed")
				}
				done.Add(1)
				return nil
			})
		}
		_ = g.Wait()
		progressTicker.Stop()
		<-progressDone
		log.WithFields(log.Fields{
			"done":   done.Load(),
			"total":  len(hashes),
			"failed": failed.Load(),
		}).Info("metadata-only backfill: done")
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
