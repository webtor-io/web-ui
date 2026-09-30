package scripts

import (
	"html/template"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/models"
)

// The player restarts a stream whose transcoder session is gone with
// purge=true (assets/src/js/lib/player/stream-restart.js). When that start
// ends on the slow-download modal, its "watch as is" must ask past the job
// cache too: within the same 10-min bucket the force-slow job it would get
// is the viewer's earlier one, whose session is the dead one (Chrome,
// 2026-09-30: master 404, the card, a player that never became ready).

func slowModal(t *testing.T, d *SlowDownloadData) string {
	t.Helper()
	tpl, err := template.New("slow_download.html").Funcs(offerFuncsWith(t, prodCatalog(), nil)).
		ParseFiles("../../templates/views/action/errors/slow_download.html", "../../templates/partials/icons.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	type slowCtx struct {
		Lang string
		Data *SlowDownloadData
	}
	return renderModal(t, tpl, "en", &slowCtx{Lang: "en", Data: d})
}

func TestSlowDownloadWatchAsIsCarriesThePurge(t *testing.T) {
	d := &SlowDownloadData{TierName: "free", MeasuredSpeedMbps: 5, RequiredSpeedMbps: 9, IsRateLimited: true, RateLimitMbps: 5,
		Endpoint: "/stream-video", ResourceID: "abc", ItemID: "i1", LogTargetID: "i1"}
	out := slowModal(t, d)
	if !strings.Contains(out, `name="force-slow" value="true"`) {
		t.Fatalf("no force-slow in the form:\n%s", out)
	}
	if strings.Contains(out, `name="purge"`) {
		t.Errorf("an ordinary start's modal asks past the job cache")
	}
	d.Purge = true
	out = slowModal(t, d)
	if !strings.Contains(out, `<input type="hidden" name="purge" value="true" />`) {
		t.Errorf("a restart's modal does not carry the purge:\n%s", out)
	}
}

func TestActionCarriesPurgeToTheModalNotToTheID(t *testing.T) {
	pinClock(t)
	start := func(purge bool) (*ErrorWrapperScript, string) {
		r, id := Action(nil, nil, nil, nil, nil, nil, nil, nil, jobKeyContext(), "08ada5a7a6183aae1e09d831df6748d566095a10", "item-1", "stream-video", &models.StreamSettings{}, nil, jobKeyVSUD(), WarmupSettings{}, GraceSettings{}, true, purge, "", "", nil)
		return r.(*ErrorWrapperScript), id
	}
	plain, plainID := start(false)
	purged, purgedID := start(true)
	if plainID != purgedID {
		t.Errorf("purge changed the job id: %s vs %s", plainID, purgedID)
	}
	var d SlowDownloadData
	plain.resubmitContext(&d)
	if d.Purge {
		t.Errorf("an ordinary start's modal carries a purge")
	}
	purged.resubmitContext(&d)
	if !d.Purge {
		t.Errorf("a purged start's modal does not carry it")
	}
	if d.Endpoint != "/stream-video" || d.ItemID != "item-1" || d.LogTargetID != "item-1" {
		t.Errorf("resubmit context: %+v", d)
	}
}
