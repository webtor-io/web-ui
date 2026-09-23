package template

import (
	"fmt"
	"strings"
	"testing"

	hc "github.com/webtor-io/web-ui/handlers/common"
)

// TestFreeCapLineFollowsTheCatalog: the comparison on /watch-torrents-ios
// states the free plan's speed cap with the catalog's number, and only when
// there is a cap to state and a plan that lifts it. A deployment without the
// storefront catalog renders no such line rather than a number or a plan it
// does not have.
func TestFreeCapLineFollowsTheCatalog(t *testing.T) {
	var ios *hc.Tool
	for i := range hc.Tools {
		if hc.Tools[i].Url == "watch-torrents-ios" {
			ios = &hc.Tools[i]
		}
	}
	if ios == nil {
		t.Fatal("/watch-torrents-ios is not in the tool list")
	}
	const key = "tool.watchTorrentsIos.about.utorrent.cap"
	for _, tc := range []struct {
		name  string
		plans bool
		rate  int
		want  bool
	}{
		{"production catalog", true, 5, true},
		{"no catalog", false, 0, false},
		{"plans, but the free tier is uncapped", true, 0, false},
		{"a capped free tier, but nothing to buy", false, 5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			funcs := aboutFuncs()
			funcs["hasPlans"] = func() bool { return tc.plans }
			funcs["freeRateMbps"] = func() int { return tc.rate }
			// Echo the params too, so the number is seen to come through.
			funcs["tp"] = func(lang, key string, args ...interface{}) string { return fmt.Sprint(key, args) }
			out := renderAbout(t, parseAboutTemplates(t, funcs, "../../templates/partials/about/*.html"), *ios)
			if got := strings.Contains(out, key); got != tc.want {
				t.Fatalf("cap line rendered = %v, want %v", got, tc.want)
			}
			if tc.want && !strings.Contains(out, key+"[Rate 5]") {
				t.Errorf("the cap line does not quote the catalog's rate: %s", out)
			}
		})
	}
}
