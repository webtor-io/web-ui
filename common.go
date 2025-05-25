package main

import (
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/api"
	enr "github.com/webtor-io/web-ui/services/enrich"
	ku "github.com/webtor-io/web-ui/services/kinopoisk_unofficial"
	"github.com/webtor-io/web-ui/services/omdb"
	"net/http"
)

func configureEnricher(f []cli.Flag) []cli.Flag {
	f = omdb.RegisterFlags(f)
	f = ku.RegisterFlags(f)
	return f
}

func makeEnricher(c *cli.Context, cl *http.Client, pg *cs.PG, sapi *api.Api) *enr.Enricher {
	var mdMappers []enr.MetadataMapper

	// Setting OMDB API
	omdbApi := omdb.New(c, cl)

	// Setting OMDB Mapper
	om := enr.NewOMDB(pg, omdbApi)
	if om != nil {
		mdMappers = append(mdMappers, om)
	}

	// Setting Kinopoisk Unofficial API
	kpuApi := ku.New(c, cl)

	// Setting Kinopoisk Unofficial Mapper
	kpu := enr.NewKinopoiskUnofficial(pg, kpuApi)
	if kpu != nil {
		mdMappers = append(mdMappers, kpu)
	}

	// Setting Enricher
	return enr.NewEnricher(pg, sapi, mdMappers)
}
