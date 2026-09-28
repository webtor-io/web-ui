package scripts

import (
	"testing"
	"time"

	"github.com/webtor-io/web-ui/models"
)

// Keys recorded at acf12dea (before the HEVC passthrough declaration
// existed) for the fixtures in job_key_fixture_test.go, with the clock
// pinned to jobKeyAt. A start that declares nothing must keep the job id it
// had: a changed id splits the render cache and the dedup of a visitor's
// repeated starts for nothing.
const (
	goldenActionID = "a95e9c41040c89d71060c5bf48691c676bc86080"
	goldenEmbedKey = "7febc81e2317163fdc90871f25ab39db96056fb1"
)

func pinClock(t *testing.T) {
	t.Helper()
	actionClock = func() time.Time { return jobKeyAt }
	t.Cleanup(func() { actionClock = time.Now })
}

func actionID(vsud *models.VideoStreamUserData) string {
	_, id := Action(nil, nil, nil, nil, nil, nil, nil, nil, jobKeyContext(), "08ada5a7a6183aae1e09d831df6748d566095a10", "item-1", "stream-video", &models.StreamSettings{}, nil, vsud, WarmupSettings{}, GraceSettings{}, false, "", "", nil)
	return id
}

func embedKey(t *testing.T, decl models.DecodeRequest) string {
	t.Helper()
	_, hash, err := Embed(nil, nil, jobKeyContext(), nil, nil, nil, jobKeyEmbedSettings(), "", nil, WarmupSettings{}, decl)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestActionIDWithoutDeclarationIsUnchanged(t *testing.T) {
	pinClock(t)
	if got := actionID(jobKeyVSUD()); got != goldenActionID {
		t.Errorf("job id %s, want %s (acf12dea)", got, goldenActionID)
	}
}

func TestEmbedKeyWithoutDeclarationIsUnchanged(t *testing.T) {
	pinClock(t)
	if got := embedKey(t, models.DecodeRequest{}); got != goldenEmbedKey {
		t.Errorf("embed key %s, want %s (acf12dea)", got, goldenEmbedKey)
	}
}

// Two browsers of one visitor that declare differently never share a
// render, and a restart after a failed passthrough is a start of its own.
func TestJobKeysFollowTheDeclaration(t *testing.T) {
	pinClock(t)
	reqs := []models.DecodeRequest{
		{},
		{Decode: "hevc8,hevc10"},
		{Decode: "hevc8,hevc10,hevc8-2160,hevc10-2160,hdr-pq"},
		{Decode: "unknown"},
		{FallbackReason: "decode_error", FallbackClass: "hevc10"},
		{FallbackReason: "user", FallbackClass: "unknown"},
	}
	actions, embeds := map[string]int{}, map[string]int{}
	for i, r := range reqs {
		v := jobKeyVSUD()
		v.DecodeRequest = r
		id := actionID(v)
		if j, ok := actions[id]; ok {
			t.Errorf("action: requests %d and %d share the id %s", j, i, id)
		}
		actions[id] = i
		k := embedKey(t, r)
		if j, ok := embeds[k]; ok {
			t.Errorf("embed: requests %d and %d share the key %s", j, i, k)
		}
		embeds[k] = i
	}
}
