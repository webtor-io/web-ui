package main

import (
	"context"
	"time"

	"github.com/urfave/cli"
	services "github.com/webtor-io/common-services"
	ci "github.com/webtor-io/web-ui/services/cache_index"

	log "github.com/sirupsen/logrus"
)

func makeCacheIndexCMD() cli.Command {
	cacheIndexCmd := cli.Command{
		Name:    "cache-index",
		Aliases: []string{"ci"},
		Usage:   "Cache index operations",
	}
	configureCacheIndex(&cacheIndexCmd)
	return cacheIndexCmd
}

func configureCacheIndex(c *cli.Command) {
	cleanupCmd := cli.Command{
		Name:    "cleanup",
		Usage:   "Cleans up old cache index entries",
		Aliases: []string{"c"},
		Action: func(c *cli.Context) error {
			return cacheIndexCleanup(c)
		},
	}
	c.Subcommands = []cli.Command{cleanupCmd}
	for k, _ := range c.Subcommands {
		configureSubCacheIndex(&c.Subcommands[k])
	}
}

func configureSubCacheIndex(c *cli.Command) {
	c.Flags = services.RegisterPGFlags(c.Flags)
	c.Flags = ci.RegisterFlags(c.Flags)
}

func cacheIndexCleanup(c *cli.Context) error {
	// Setting DB
	pg := services.NewPG(c)
	defer pg.Close()

	// Setting CacheIndex
	cacheIndex := ci.New(c, pg)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	// Run cleanup
	log.Info("running cache index cleanup")
	cacheIndex.RunCleanup(ctx)
	log.Info("cache index cleanup completed")

	return nil
}
