package resource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	cp "github.com/webtor-io/claims-provider/proto"

	"github.com/webtor-io/web-ui/services/api"
	uclaims "github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/statusview"
)

// initialViewOf renders what the page's first paint carries for st, through
// a request the way the page is served (language routing, claims).
func initialViewOf(t *testing.T, h *Handler, st *TorrentStatus, path string) *statusview.View {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), api.ClaimsContext{}, &api.Claims{SessionID: "s1", Rate: "5M"})
		ctx = context.WithValue(ctx, uclaims.Context{}, &cp.GetResponse{Context: &cp.Context{Tier: &cp.Tier{Name: "free"}}})
		c.Request = c.Request.WithContext(ctx)
	})
	r.Use(i18n.GinMiddleware(i18n.New(os.DirFS("../../locales"))))
	var got *statusview.View
	r.GET("/*any", func(c *gin.Context) { got = h.initialView(c, st) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Lang", "ru")
	r.ServeHTTP(w, req)
	return got
}

// The page renders the same view the stream sends, before anything was
// asked of the seeder: an idle status there means "not known yet" -- the
// badge checks -- and nothing moves that the page knows of, so the badge
// is up, the viewer's link not drawn, nothing sold.
func TestInitialView(t *testing.T) {
	h := &Handler{offers: liveOffers()}
	v := initialViewOf(t, h, &TorrentStatus{State: "idle"}, "/"+ssHash)
	if v == nil || v.Key != statusview.KeyChecking || v.Mode != statusview.ModeBadge || v.Badge.Icon != "dots" || v.Nodes[0].Value != "" || v.Nodes[2].Show || v.Plan != nil {
		t.Fatalf("idle at render: %+v", v)
	}
	vaulted := initialViewOf(t, h, &TorrentStatus{State: "vaulted"}, "/"+ssHash)
	if vaulted.Key != statusview.KeyVaultedIdle || vaulted.Mode != statusview.ModeBadge || vaulted.Nodes[1].Kind != "vault" || vaulted.Segs[1].Show {
		t.Errorf("vaulted at render: %+v", vaulted)
	}
	if initialViewOf(t, h, nil, "/"+ssHash) != nil {
		t.Error("no status, no view")
	}
	// It serializes for the template's {{ .StatusView | json }}.
	if _, err := json.Marshal(v); err != nil {
		t.Fatal(err)
	}
}

// The cap in the viewer's own claims, in the chain's megabit; none without
// claims. (The env no longer carries a grace flag: grace segments are counted
// like any other request of the viewer's since their token carries the
// session -- docs/grace_token.md "Session".)
func TestViewEnv_ClaimCap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gc, _ := gin.CreateTestContext(httptest.NewRecorder())
	gc.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if e := (&Handler{}).viewEnv(gc, &api.Claims{Role: "free", Rate: "5M"}); e.claimCap != 5 {
		t.Errorf("5M: claimCap %v", e.claimCap)
	}
	if e := (&Handler{}).viewEnv(gc, nil); e.claimCap != 0 {
		t.Errorf("no claims: %+v", e)
	}
}
