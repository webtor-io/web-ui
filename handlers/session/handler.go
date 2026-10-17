package session

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-contrib/sessions/redis"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	csrf "github.com/utrack/gin-csrf"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/tracksession"
)

const (
	redisHostFlag = "session-redis-host"
	redisPortFlag = "session-redis-port"
	redisPassFlag = "session-redis-pass"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   redisHostFlag,
			Usage:  "session redis host",
			EnvVar: "REDIS_MASTER_SERVICE_HOST, REDIS_SERVICE_HOST",
		},
		cli.StringFlag{
			Name:   redisPassFlag,
			Usage:  "session redis pass",
			EnvVar: "REDIS_PASS",
		},
		cli.IntFlag{
			Name:   redisPortFlag,
			Usage:  "session redis port",
			EnvVar: "REDIS_MASTER_SERVICE_PORT, REDIS_SERVICE_PORT",
		},
	)
}

type Session struct {
	ID   string
	CSRF string
}

func RegisterHandler(c *cli.Context, r *gin.Engine, csrfIgnorePrefixes []string) (err error) {
	var store sessions.Store
	if c.String(redisHostFlag) != "" && c.Int(redisPortFlag) != 0 {
		url := fmt.Sprintf("%v:%v", c.String(redisHostFlag), c.Int(redisPortFlag))
		store, err = redis.NewStore(10, "tcp", url, c.String(redisPassFlag), []byte(c.String(common.SessionSecretFlag)))
		if err != nil {
			return err
		}
		if err = tracksession.Install(store); err != nil {
			return err
		}
		log.Infof("using redis store %v", url)
	} else {
		store = cookie.NewStore([]byte(c.String(common.SessionSecretFlag)))
	}
	// A cross-site iframe (the embed flow) only keeps the session if the
	// cookie is SameSite=None, which browsers accept only with Secure -- and
	// a Secure cookie is stored only on https and localhost. So the mode
	// follows the scheme of the configured public origin: on an http
	// instance opened by LAN address a Secure cookie is dropped, every
	// request starts a new session and every CSRF-checked form answers 400
	// (self-hosted#116). Configuration, not the request: behind a TLS
	// terminator the request reaches us as plain http.
	sameSite, secure := http.SameSiteLaxMode, false
	if domain := c.String(common.DomainFlag); servedOverHTTPS(domain) {
		sameSite, secure = http.SameSiteNoneMode, true
	} else {
		log.Infof("DOMAIN %q is not https: the session cookie is SameSite=Lax and not Secure, so a player embedded on another site cannot keep a session; if this instance is served over https, set DOMAIN=https://...", domain)
	}
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 30,
		Secure:   secure,
		HttpOnly: true,
		SameSite: sameSite,
	})
	r.Use(sessions.Sessions("session", store))
	r.Use(func(ctx *gin.Context) {
		id := ctx.GetHeader("X-Session-ID")
		if id == "" {
			id, _ = ctx.GetPostForm("_sessionID")
		}
		if id != "" {
			ctx.Request.AddCookie(&http.Cookie{
				Name:  "session",
				Value: id,
			})
		}

	})
	r.Use(csrf.Middleware(csrf.Options{
		Secret: c.String(common.SessionSecretFlag),
		ErrorFunc: func(c *gin.Context) {
			for _, r := range csrfIgnorePrefixes {
				if strings.HasPrefix(c.Request.URL.Path, r) {
					c.Next()
					return
				}
			}
			c.String(400, "CSRF token mismatch")
			c.Abort()
		},
	}))
	r.Use(func(c *gin.Context) {
		if c.GetHeader("X-Token") != "" {
			csrf.GetToken(c)
		}
	})
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), Session{}, &Session{
			CSRF: csrf.GetToken(c),
			ID:   sessions.Default(c).ID(),
		}))
	})
	return
}

// servedOverHTTPS answers whether the instance is configured to be served
// over https -- from DOMAIN, never from a probe or the request scheme.
func servedOverHTTPS(domain string) bool {
	return strings.HasPrefix(strings.ToLower(domain), "https://")
}

func GetFromContext(c *gin.Context) *Session {
	if r := c.Request.Context().Value(Session{}); r != nil {
		return r.(*Session)
	}
	return nil
}
