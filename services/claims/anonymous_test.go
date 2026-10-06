package claims

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"
	proto "github.com/webtor-io/claims-provider/proto"
	cs "github.com/webtor-io/common-services"
	"google.golang.org/grpc"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
)

type fakeProvider struct {
	resp *proto.GetResponse
	err  error
}

func (f *fakeProvider) Get(context.Context, *proto.GetRequest, ...grpc.CallOption) (*proto.GetResponse, error) {
	return f.resp, f.err
}

func withProvider(p proto.ClaimsProviderClient) *Claims {
	cl := &Client{}
	cl.once.Do(func() {})
	cl.cl = p
	return New(nil, cl, &cs.PG{}) // Postgres not configured: no tier write-back
}

func freeTier() *proto.GetResponse {
	return &proto.GetResponse{
		Context: &proto.Context{Tier: &proto.Tier{Id: 0, Name: "free"}},
		Claims:  &proto.Claims{Connection: &proto.Connection{}, Embed: &proto.Embed{}, Site: &proto.Site{}},
	}
}

var errDown = errors.New("rpc error: code = Internal desc = failed to get claims")

// Every visitor without an identity asks the provider the same question, and
// the provider answers it from Postgres. 2026-10-05 Postgres went away for
// five minutes and every anonymous request -- pages, /assets, the manifest --
// answered 503. The last answer is still the right one; what must not stand
// in for it is the synthetic no-provider claims (no ads, no rate): a provider
// that failed is not a provider that is absent.
func TestAnonymousClaimsOutliveAProviderOutage(t *testing.T) {
	p := &fakeProvider{err: errDown}
	s := withProvider(p)
	anon := &Request{}

	if _, err := s.Get(anon); err == nil {
		t.Fatal("no answer from the provider yet, and Get made one up")
	}

	p.resp, p.err = freeTier(), nil
	if _, err := s.Refresh(anon); err != nil {
		t.Fatal(err)
	}

	p.resp, p.err = nil, errDown
	d, err := s.Refresh(anon)
	if err != nil {
		t.Fatalf("anonymous visitor got %v while the provider was down; want its last answer", err)
	}
	if d.Context.Tier.Name != "free" || d.Claims.Site.NoAds {
		t.Errorf("got %v, want the provider's last answer", d)
	}
}

// The same outage through the middleware every page passes. An anonymous
// page goes on to its handler; a signed-in one stops at the error, which
// services/web answers 503 ("failed to get claims", user_error.go) -- it is
// not handed the free tier and the anonymous page: that would take away what
// they pay for without a word.
func TestMiddlewareDuringProviderOutage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	p := &fakeProvider{resp: freeTier()}
	s := withProvider(p)
	signedIn := &models.User{UserID: uuid.NewV4(), Email: "paid@example.com", Tier: "free"}

	var reached bool
	var errs []*gin.Error
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Next()
		errs = c.Errors
	})
	r.Use(func(c *gin.Context) {
		if c.Query("as") == "user" {
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContext{}, signedIn))
		}
	})
	s.RegisterHandler(r)
	r.GET("/", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})
	get := func(target string) int {
		reached, errs = false, nil
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		return w.Code
	}

	if code := get("/"); code != http.StatusOK || !reached {
		t.Fatalf("provider up: anonymous page answered %d", code)
	}

	p.resp, p.err = nil, errDown
	s.LazyMap.Drop(cacheKey(&Request{})) // the minute is up
	// No error page in this chain: an abort shows as an error, not a status.
	if get("/"); !reached || len(errs) != 0 {
		t.Errorf("provider down: the anonymous page did not run (errors %v)", errs)
	}
	get("/?as=user")
	if reached {
		t.Error("provider down: the signed-in page ran without its own claims")
	}
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "failed to get claims") {
		t.Errorf("provider down: signed-in errors %v, want failed to get claims", errs)
	}
}
