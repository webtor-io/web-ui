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

type Claims struct {
	*lazymap.LazyMap[*Data]
	cl *Client
	pg *cs.PG
}

type Data = proto.GetResponse

func New(c *cli.Context, cl *Client, pg *cs.PG) *Claims {
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

func cacheKey(r *Request) string {
	// prefer cache key by patreonID if available, otherwise by email.
	// The pointer must be dereferenced: formatting *string with %v prints
	// the address, which made the key unique per request for patreon-linked
	// users (their claims were effectively never cached).
	patreonUserID := ""
	if r.PatreonUserID != nil {
		patreonUserID = *r.PatreonUserID
	}
	return fmt.Sprintf("email:%v;patreonid:%v", r.Email, patreonUserID)
}

// Fetch queries the claims provider directly, bypassing the cache. For flows
// that poll for a tier change (e.g. right after a payment) — polling through
// Refresh would Drop hot cache entries and race the per-request middleware.
func (s *Claims) Fetch(r *Request) (*Data, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cl, err := s.cl.Get()
	if err != nil {
		return nil, err
	}
	var patreonUserID string
	if r.PatreonUserID != nil {
		patreonUserID = *r.PatreonUserID
	}
	resp, err := cl.Get(ctx, &proto.GetRequest{Email: r.Email, PatreonUserId: patreonUserID})
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get claims")
	}
	return resp, nil
}

// Refresh drops the cached claims for the request and fetches them anew, so
// subsequent Gets observe the current state immediately instead of waiting
// out the cache TTL.
func (s *Claims) Refresh(r *Request) (*Data, error) {
	s.LazyMap.Drop(cacheKey(r))
	return s.Get(r)
}

func (s *Claims) Get(r *Request) (*Data, error) {
	return s.LazyMap.Get(cacheKey(r), func() (*Data, error) {
		return s.Fetch(r)
	})
}

func (s *Claims) makeAdminClaims() *Data {
	return &Data{Context: &proto.Context{
		Tier: &proto.Tier{
			Id:   1,
			Name: "free",
		},
	},
		Claims: &proto.Claims{
			Connection: &proto.Connection{},
			Embed: &proto.Embed{
				NoAds: true,
			},
			Site: &proto.Site{
				NoAds: true,
			},
		},
	}
}

func (s *Claims) MakeUserClaimsFromContext(c *gin.Context) (*Data, error) {
	u := auth.GetUserFromContext(c)
	if auth.IsAdmin(c) {
		return s.makeAdminClaims(), nil
	}
	r, err := s.Get(&Request{
		Email:         u.Email,
		PatreonUserID: u.PatreonUserID,
	})
	if err != nil {
		// Check the error BEFORE touching r — on failure r is nil, so the
		// test-ads deref below would panic.
		return nil, err
	}
	if _, err := c.Cookie("test-ads"); err == nil {
		r.Claims.Site.NoAds = false
	} else if c.Query("test-ads") != "" {
		r.Claims.Site.NoAds = false
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
			// Feed the error to the centralized ErrorHandler (services/web)
			// without writing a status, so it can render the friendly page.
			_ = c.Error(err)
			c.Abort()
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
