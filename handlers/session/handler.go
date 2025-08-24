package session

import (
	"bufio"
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
)

const (
	sessionSecretFlag = "secret"
	redisHostFlag     = "session-redis-host"
	redisPortFlag     = "session-redis-port"
	redisPassFlag     = "session-redis-pass"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   sessionSecretFlag,
			Usage:  "session secret",
			Value:  "secret123",
			EnvVar: "SESSION_SECRET",
		},
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
		store, err = redis.NewStore(10, "tcp", url, c.String(redisPassFlag), []byte(sessionSecretFlag))
		if err != nil {
			return err
		}
		log.Infof("using redis store %v", url)
	} else {
		store = cookie.NewStore([]byte(sessionSecretFlag))
	}
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
		Secret: c.String(sessionSecretFlag),
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
			ID:   getSessionId(c, "session"),
		}))
	})
	return
}
func getSessionId(c *gin.Context, name string) (id string) {
	id, _ = c.Cookie(name)
	if id != "" {
		return
	}
	id = getSessionIdFromResponseHeader(c, "Set-Cookie", name)
	return
}

func getSessionIdFromResponseHeader(c *gin.Context, headerName string, cookieName string) string {
	cookies := c.Writer.Header().Get(headerName)
	rawRequest := fmt.Sprintf("GET / HTTP/1.0\r\nCookie: %s\r\n\r\n", cookies)
	req, _ := http.ReadRequest(bufio.NewReader(strings.NewReader(rawRequest)))
	var id string
	for _, q := range req.Cookies() {
		if q.Name == cookieName {
			id = q.Value
			break
		}
	}
	return id
}

func GetFromContext(c *gin.Context) *Session {
	if r := c.Request.Context().Value(Session{}); r != nil {
		return r.(*Session)
	}
	return nil
}
