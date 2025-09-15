package main

import (
	"net/http"

	wa "github.com/webtor-io/web-ui/handlers/action"
	"github.com/webtor-io/web-ui/handlers/addon_url"
	wau "github.com/webtor-io/web-ui/handlers/auth"
	"github.com/webtor-io/web-ui/handlers/donate"
	we "github.com/webtor-io/web-ui/handlers/embed"
	wee "github.com/webtor-io/web-ui/handlers/embed/example"
	"github.com/webtor-io/web-ui/handlers/embed_domain"
	"github.com/webtor-io/web-ui/handlers/ext"
	"github.com/webtor-io/web-ui/handlers/geo"
	wi "github.com/webtor-io/web-ui/handlers/index"
	"github.com/webtor-io/web-ui/handlers/instructions"
	wj "github.com/webtor-io/web-ui/handlers/job"
	"github.com/webtor-io/web-ui/handlers/legal"
	"github.com/webtor-io/web-ui/handlers/library"
	wm "github.com/webtor-io/web-ui/handlers/migration"
	p "github.com/webtor-io/web-ui/handlers/profile"
	wr "github.com/webtor-io/web-ui/handlers/resource"
	sess "github.com/webtor-io/web-ui/handlers/session"
	sta "github.com/webtor-io/web-ui/handlers/static"
	"github.com/webtor-io/web-ui/handlers/stremio"
	"github.com/webtor-io/web-ui/handlers/support"
	"github.com/webtor-io/web-ui/handlers/tests"
	"github.com/webtor-io/web-ui/handlers/webdav"
	as "github.com/webtor-io/web-ui/services/abuse_store"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/geoip"
	"github.com/webtor-io/web-ui/services/umami"
	ua "github.com/webtor-io/web-ui/services/url_alias"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/embed"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/template"
	w "github.com/webtor-io/web-ui/services/web"

	stremioAddon "github.com/webtor-io/web-ui/services/stremio/addon"
)

func makeServeCMD() cli.Command {
	serveCMD := cli.Command{
		Name:    "serve",
		Aliases: []string{"s"},
		Usage:   "Serves web server",
		Action:  serve,
	}
	configureServe(&serveCMD)
	return serveCMD
}

func configureServe(c *cli.Command) {
	c.Flags = cs.RegisterPGFlags(c.Flags)
	c.Flags = cs.RegisterProbeFlags(c.Flags)
	c.Flags = cs.RegisterS3ClientFlags(c.Flags)
	c.Flags = api.RegisterFlags(c.Flags)
	c.Flags = w.RegisterFlags(c.Flags)
	c.Flags = common.RegisterFlags(c.Flags)
	c.Flags = auth.RegisterFlags(c.Flags)
	c.Flags = claims.RegisterFlags(c.Flags)
	c.Flags = claims.RegisterClientFlags(c.Flags)
	c.Flags = sess.RegisterFlags(c.Flags)
	c.Flags = sta.RegisterFlags(c.Flags)
	c.Flags = cs.RegisterRedisClientFlags(c.Flags)
	c.Flags = as.RegisterFlags(c.Flags)
	c.Flags = cs.RegisterPprofFlags(c.Flags)
	c.Flags = umami.RegisterFlags(c.Flags)
	c.Flags = geoip.RegisterFlags(c.Flags)
	c.Flags = library.RegisterFlags(c.Flags)
	c.Flags = embed.RegisterFlags(c.Flags)
	c.Flags = configureEnricher(c.Flags)
}

func serve(c *cli.Context) error {
	// Setting HTTP Client
	cl := http.DefaultClient

	// Setting DB
	pg := cs.NewPG(c)
	defer pg.Close()

	// Setting Migrations
	err := pgMigrate(c)
	if err != nil {
		return err
	}

	// Setting template renderer
	re := multitemplate.NewRenderer()

	// Setting TemplateManager
	tm := template.NewManager[*w.Context](re).
		WithHelper(w.NewHelper(c)).
		WithHelper(umami.NewHelper(c)).
		WithHelper(geoip.NewHelper())

	var servers []cs.Servable
	// Setting Probe
	probe := cs.NewProbe(c)
	if probe != nil {
		servers = append(servers, probe)
		defer probe.Close()
	}

	// Setting Pprof
	pprof := cs.NewPprof(c)
	if pprof != nil {
		servers = append(servers, pprof)
		defer pprof.Close()
	}
	// Setting Gin
	r := gin.Default()
	r.RedirectTrailingSlash = false
	r.HTMLRender = re

	// Setting Web
	web, err := w.New(c, r)
	if err != nil {
		return err
	}
	servers = append(servers, web)
	defer web.Close()

	// Setting URL Alias
	ual := ua.New(pg, r)
	ual.RegisterHandler(r)

	err = sess.RegisterHandler(c, r, []string{
		"/auth/dashboard",
		"/s/",
		"/token/",
		"/webdav/",
	})
	if err != nil {
		return err
	}

	// Setting Auth
	a := auth.New(c, cl, pg)

	if a != nil {
		err := a.Init()
		if err != nil {
			return err
		}
		a.RegisterHandler(r)
	}

	// Setting Access Token
	ats := at.New(pg)
	ats.RegisterHandler(r)

	// Setting Claims Client
	cpCl := claims.NewClient(c)
	if cpCl != nil {
		defer cpCl.Close()
	}

	// Setting UserClaims
	uc := claims.New(c, cpCl, pg)
	if uc != nil {
		// Setting UserClaimsHandler
		uc.RegisterHandler(r)
	}

	// Setting S3 Client
	s3Cl := cs.NewS3Client(c, cl)

	// Setting GeoIP
	gapi := geoip.New(c, cl)

	if gapi != nil {
		err = geo.RegisterHandler(gapi, r)
		if err != nil {
			return err
		}
	}

	// Setting Api
	sapi := api.New(c, cl)

	// Setting ApiClaimsHandler
	sapi.RegisterHandler(r)

	// Setting Static
	err = sta.RegisterHandler(c, r)
	if err != nil {
		return err
	}

	// Setting Migration from v1 to v2
	wm.RegisterHandler(r)

	// Setting Redis
	redis := cs.NewRedisClient(c)
	defer redis.Close()

	// Setting AuthHandlers
	if a != nil {
		wau.RegisterHandler(r, tm)
	}

	// Setting Enricher
	en := makeEnricher(c, cl, pg, sapi)

	// Setting JobQueues
	queues := job.NewQueues(job.NewStorage(redis, gin.Mode()))

	// Setting JobHandler
	jobs := wj.New(queues, tm, sapi, en)

	jobs.RegisterHandler(r)

	// Setting AbuseStore
	asc := as.New(c)

	if asc != nil {
		defer asc.Close()
		// Setting Support
		support.RegisterHandler(r, tm, asc)

		// Setting Legal
		legal.RegisterHandler(r, tm)
	}

	// Setting DomainSettings
	ds, err := embed.NewDomainSettings(c, pg, uc)
	if err != nil {
		return err
	}

	// Setting ResourceHandler
	wr.RegisterHandler(c, r, tm, sapi, jobs, pg)

	// Setting IndexHandler
	wi.RegisterHandler(r, tm)

	// Setting ActionHandler
	wa.RegisterHandler(r, tm, jobs)

	// Setting ProfileHandler
	p.RegisterHandler(r, tm, ats, ual, pg, uc)

	// Setting EmbedDomainHandler
	err = embed_domain.RegisterHandler(c, r, pg)
	if err != nil {
		return err
	}

	av := stremioAddon.NewValidator(cl)

	// Setting AddonUrlHandler
	err = addon_url.RegisterHandler(c, av, r, pg)
	if err != nil {
		return err
	}

	// Setting EmbedExamplesHandler
	wee.RegisterHandler(r, tm)

	// Setting EmbedHandler
	we.RegisterHandler(cl, r, tm, jobs, ds, sapi)

	// Setting ExtHandler
	ext.RegisterHandler(r, tm)

	// Setting Donate
	donate.RegisterHandler(r)

	// Setting Library
	library.RegisterHandler(c, r, tm, sapi, pg, jobs, cl, s3Cl)

	// Setting Stremio
	stremio.RegisterHandler(c, r, pg, ats, sapi)

	// Setting WebDAV
	webdav.RegisterHandler(r, pg, ats, sapi, jobs)

	// Setting Tests
	tests.RegisterHandler(r, tm)

	// Setting Instructions
	instructions.RegisterHandler(r, tm)

	// Render templates
	err = tm.Init()
	if err != nil {
		return err
	}

	// Setting Serve
	serve := cs.NewServe(servers...)

	// And SERVE!
	err = serve.Serve()
	if err != nil {
		log.WithError(err).Error("got server error")
	}
	return err
}
