package claims

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	proto "github.com/webtor-io/claims-provider/proto"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
)

const (
	UseFlag = "use-claims"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.BoolFlag{
			Name:   UseFlag,
			Usage:  "use claims",
			EnvVar: "USE_CLAIMS",
		},
	)
}

type Claims struct {
	lazymap.LazyMap[*Data]
	cl *Client
	pg *cs.PG
}

type Data = proto.GetResponse

func New(c *cli.Context, cl *Client, pg *cs.PG) *Claims {
	if !c.Bool(UseFlag) || cl == nil {
		return nil
	}
	return &Claims{
		cl: cl,
		pg: pg,
		LazyMap: lazymap.New[*Data](&lazymap.Config{
			Expire:      time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
	}
}

type Request struct {
	Email         string
	PatreonUserID *string
}

func (s *Claims) Get(r *Request) (*Data, error) {
	// prefer cache key by patreonID if available, otherwise by email
	key := fmt.Sprintf("email:%v;patreonid:%v", r.Email, r.PatreonUserID)
	return s.LazyMap.Get(key, func() (resp *Data, err error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var cl proto.ClaimsProviderClient
		cl, err = s.cl.Get()
		if err != nil {
			return nil, err
		}
		var patreonUserID string
		if r.PatreonUserID != nil {
			patreonUserID = *r.PatreonUserID
		}
		resp, err = cl.Get(ctx, &proto.GetRequest{Email: r.Email, PatreonUserId: patreonUserID})
		if err != nil {
			return nil, errors.WithMessage(err, "failed to get claims")
		}
		return
	})
}

func (s *Claims) MakeUserClaimsFromContext(c *gin.Context) (*Data, error) {
	u := auth.GetUserFromContext(c)
	r, err := s.Get(&Request{
		Email:         u.Email,
		PatreonUserID: u.PatreonUserID,
	})
	if _, err := c.Cookie("test-ads"); err == nil {
		r.Claims.Site.NoAds = false
	} else if c.Query("test-ads") != "" {
		r.Claims.Site.NoAds = false
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

type Context struct{}

type TierUpdatedContext struct{}

func GetFromContext(c *gin.Context) *Data {
	if r := c.Request.Context().Value(Context{}); r != nil {
		return r.(*Data)
	}
	return nil
}

func GetTierUpdateFromContext(c *gin.Context) bool {
	if r := c.Request.Context().Value(TierUpdatedContext{}); r != nil {
		return r.(bool)
	}
	return false
}

func (s *Claims) RegisterHandler(r *gin.Engine) {
	r.Use(func(c *gin.Context) {
		ucl, err := s.MakeUserClaimsFromContext(c)
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), Context{}, ucl))
		db := s.pg.Get()
		if db == nil {
			return
		}
		uc := c.Request.Context().Value(auth.UserContext{})
		u, ok := uc.(*models.User)
		if !ok {
			return
		}
		updated := false
		if u.Tier != ucl.Context.Tier.Name {
			u.Tier = ucl.Context.Tier.Name
			err = models.UpdateUserTier(c.Request.Context(), db, u)
			if err != nil {
				_ = c.AbortWithError(http.StatusInternalServerError, err)
				return
			}
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContext{}, u))
			updated = true
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), TierUpdatedContext{}, updated))
		c.Next()
	})
	r.Use(func(c *gin.Context) {
	})
}

func IsPaid(c *gin.Context) {
	d := GetFromContext(c)
	if d == nil {
		c.Next()
		return
	}
	if d.Context.Tier.Id == 0 {
		c.AbortWithStatus(http.StatusPaymentRequired)
	} else {
		c.Next()
	}
}
