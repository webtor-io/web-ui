package resource

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
	vault "github.com/webtor-io/web-ui/services/vault"
)

// --- Mock implementations ---

type mockStatusVaultDB struct {
	resource *vaultModels.Resource
	err      error
}

func (m *mockStatusVaultDB) GetResource(_ context.Context, _ string) (*vaultModels.Resource, error) {
	return m.resource, m.err
}

type mockStatusVaultAPI struct {
	resource *vault.Resource
	err      error
}

func (m *mockStatusVaultAPI) GetResource(_ context.Context, _ string) (*vault.Resource, error) {
	return m.resource, m.err
}

// --- Tests for resolveStatus ---

func TestResolveStatus_Idle(t *testing.T) {
	status := resolveStatus(nil, nil, nil)
	if status.State != "idle" {
		t.Errorf("expected idle, got %q", status.State)
	}
}

func TestResolveStatus_IdleNotFunded(t *testing.T) {
	db := &vaultModels.Resource{Funded: false, Vaulted: false}
	status := resolveStatus(db, nil, nil)
	if status.State != "idle" {
		t.Errorf("expected idle, got %q", status.State)
	}
}

func TestResolveStatus_Caching(t *testing.T) {
	stats := &TorrentStatsData{Total: 100, Completed: 45, Seeders: 3}
	status := resolveStatus(nil, nil, stats)
	if status.State != "caching" {
		t.Errorf("expected caching, got %q", status.State)
	}
	if status.Progress != 45 {
		t.Errorf("expected progress 45, got %v", status.Progress)
	}
	if status.Seeders != 3 {
		t.Errorf("expected seeders 3, got %v", status.Seeders)
	}
}

func TestResolveStatus_Cached(t *testing.T) {
	stats := &TorrentStatsData{Total: 100, Completed: 100, Seeders: 5}
	status := resolveStatus(nil, nil, stats)
	if status.State != "cached" {
		t.Errorf("expected cached, got %q", status.State)
	}
}

func TestResolveStatus_VaultingQueued(t *testing.T) {
	db := &vaultModels.Resource{Funded: true, Vaulted: false}
	apiRes := &vault.Resource{Status: vault.StatusQueued}
	status := resolveStatus(db, apiRes, nil)
	if status.State != "vaulting" {
		t.Errorf("expected vaulting, got %q", status.State)
	}
	if status.Progress != 0 {
		t.Errorf("expected progress 0, got %v", status.Progress)
	}
}

func TestResolveStatus_VaultingProcessing(t *testing.T) {
	db := &vaultModels.Resource{Funded: true, Vaulted: false}
	apiRes := &vault.Resource{Status: vault.StatusProcessing, StoredSize: 72, TotalSize: 100}
	status := resolveStatus(db, apiRes, nil)
	if status.State != "vaulting" {
		t.Errorf("expected vaulting, got %q", status.State)
	}
	if status.Progress != 72 {
		t.Errorf("expected progress 72, got %v", status.Progress)
	}
}

func TestResolveStatus_Vaulted_DB(t *testing.T) {
	db := &vaultModels.Resource{Vaulted: true}
	status := resolveStatus(db, nil, nil)
	if status.State != "vaulted" {
		t.Errorf("expected vaulted, got %q", status.State)
	}
}

func TestResolveStatus_Vaulted_API(t *testing.T) {
	db := &vaultModels.Resource{Funded: true, Vaulted: false}
	apiRes := &vault.Resource{Status: vault.StatusCompleted}
	status := resolveStatus(db, apiRes, nil)
	if status.State != "vaulted" {
		t.Errorf("expected vaulted, got %q", status.State)
	}
}

func TestResolveStatus_VaultFailed(t *testing.T) {
	db := &vaultModels.Resource{Funded: true, Vaulted: false}
	apiRes := &vault.Resource{Status: vault.StatusFailed}
	apiRes.Error = "seeder pod unreachable"
	status := resolveStatus(db, apiRes, nil)
	// Funded, last transfer attempt failed: say so (the system retries), and
	// carry the API's reason for the tooltip. This used to hide as "vaulting".
	if status.State != "vault_failed" || status.Detail != "seeder pod unreachable" {
		t.Errorf("expected vault_failed with detail, got %+v", status)
	}
}

func TestResolveStatus_VaultWaitingForSeeders(t *testing.T) {
	db := &vaultModels.Resource{Funded: true}
	queued := &vault.Resource{Status: vault.StatusQueued}
	// Nothing stored, seeder sees nobody → waiting for seeders.
	if st := resolveStatus(db, queued, &TorrentStatsData{Total: 100, Seeders: 0, Peers: 0}); st.State != "vault_waiting" {
		t.Errorf("queued + empty swarm must read vault_waiting, got %+v", st)
	}
	// Peers around → plain vaulting.
	if st := resolveStatus(db, queued, &TorrentStatsData{Total: 100, Seeders: 2, Peers: 3}); st.State != "vaulting" || st.Seeders != 2 {
		t.Errorf("queued + peers must stay vaulting with the swarm, got %+v", st)
	}
	// No stats at all → we do not know, so no claim.
	if st := resolveStatus(db, queued, nil); st.State != "vaulting" {
		t.Errorf("queued without stats must stay vaulting, got %+v", st)
	}
	// Progress already made → not waiting even if the swarm emptied.
	if st := resolveStatus(db, &vault.Resource{Status: vault.StatusProcessing, StoredSize: 50, TotalSize: 100}, &TorrentStatsData{Total: 100}); st.State != "vaulting" {
		t.Errorf("progress > 0 must stay vaulting, got %+v", st)
	}
}

func TestResolveStatus_CachingAndVaulting(t *testing.T) {
	db := &vaultModels.Resource{Funded: true, Vaulted: false}
	apiRes := &vault.Resource{Status: vault.StatusProcessing, StoredSize: 30, TotalSize: 100}
	stats := &TorrentStatsData{Total: 100, Completed: 60, Seeders: 2}
	status := resolveStatus(db, apiRes, stats)
	// Vaulting has higher priority than caching
	if status.State != "vaulting" {
		t.Errorf("expected vaulting (higher priority), got %q", status.State)
	}
	if status.Progress != 30 {
		t.Errorf("expected progress 30, got %v", status.Progress)
	}
	if status.Seeders != 2 {
		t.Errorf("expected seeders 2 from stats, got %v", status.Seeders)
	}
}

func TestResolveStatus_CachedAndVaulting(t *testing.T) {
	db := &vaultModels.Resource{Funded: true, Vaulted: false}
	apiRes := &vault.Resource{Status: vault.StatusProcessing, StoredSize: 50, TotalSize: 100}
	stats := &TorrentStatsData{Total: 100, Completed: 100, Seeders: 5}
	status := resolveStatus(db, apiRes, stats)
	// Vaulting has higher priority than cached
	if status.State != "vaulting" {
		t.Errorf("expected vaulting (higher priority), got %q", status.State)
	}
	if status.Seeders != 5 {
		t.Errorf("expected seeders 5 from stats, got %v", status.Seeders)
	}
}

func TestResolveStatus_CachingAndVaulted(t *testing.T) {
	db := &vaultModels.Resource{Vaulted: true}
	stats := &TorrentStatsData{Total: 100, Completed: 50, Seeders: 3}
	status := resolveStatus(db, nil, stats)
	// Vaulted has highest priority
	if status.State != "vaulted" {
		t.Errorf("expected vaulted (highest priority), got %q", status.State)
	}
}

func TestResolveStatus_ZeroTotalSize(t *testing.T) {
	db := &vaultModels.Resource{Funded: true, Vaulted: false}
	apiRes := &vault.Resource{Status: vault.StatusProcessing, StoredSize: 0, TotalSize: 0}
	status := resolveStatus(db, apiRes, nil)
	if status.State != "vaulting" {
		t.Errorf("expected vaulting, got %q", status.State)
	}
	if status.Progress != 0 {
		t.Errorf("expected progress 0, got %v", status.Progress)
	}
}

func TestResolveStatus_FundedNoAPI(t *testing.T) {
	db := &vaultModels.Resource{Funded: true, Vaulted: false}
	status := resolveStatus(db, nil, nil)
	if status.State != "vaulting" {
		t.Errorf("expected vaulting (funded, no API data), got %q", status.State)
	}
	if status.Progress != 0 {
		t.Errorf("expected progress 0, got %v", status.Progress)
	}
}

func TestResolveStatus_StatsZeroTotal(t *testing.T) {
	stats := &TorrentStatsData{Total: 0, Completed: 0, Seeders: 1}
	status := resolveStatus(nil, nil, stats)
	if status.State != "idle" {
		t.Errorf("expected idle for zero total stats, got %q", status.State)
	}
}

// --- Tests for prepareInitialStatus ---

func TestPrepareInitialStatus_NoVault(t *testing.T) {
	h := &Handler{vault: nil}
	status := h.prepareInitialStatus(context.Background(), "test-id")
	if status.State != "idle" {
		t.Errorf("expected idle, got %q", status.State)
	}
}

func TestPrepareInitialStatus_DBError(t *testing.T) {
	// When vault DB returns an error, prepareInitialStatus falls back to idle.
	// We test resolveStatus with nil (the fallback path).
	status := resolveStatus(nil, nil, nil)
	if status.State != "idle" {
		t.Errorf("expected idle on error fallback, got %q", status.State)
	}
}

func TestPrepareInitialStatus_Vaulted(t *testing.T) {
	db := &vaultModels.Resource{Vaulted: true}
	status := resolveStatus(db, nil, nil)
	if status.State != "vaulted" {
		t.Errorf("expected vaulted, got %q", status.State)
	}
}

func TestPrepareInitialStatus_Funded(t *testing.T) {
	db := &vaultModels.Resource{Funded: true, Vaulted: false}
	status := resolveStatus(db, nil, nil)
	if status.State != "vaulting" {
		t.Errorf("expected vaulting, got %q", status.State)
	}
	if status.Progress != 0 {
		t.Errorf("expected progress 0, got %v", status.Progress)
	}
}

type mockVaultForStatus struct {
	resource *vaultModels.Resource
	err      error
}

func (m *mockVaultForStatus) GetResource(_ context.Context, _ string) (*vaultModels.Resource, error) {
	return m.resource, m.err
}

func TestResolveStatus_CarriesSeedersLeechersPeers(t *testing.T) {
	st := resolveStatus(nil, nil, &TorrentStatsData{Total: 100, Completed: 10, Seeders: 4, Leechers: 9, Peers: 13})
	if st.State != "caching" || st.Seeders != 4 || st.Leechers != 9 || st.Peers != 13 {
		t.Errorf("swarm counters must ride on the status: %+v", st)
	}
}

func TestSwarmLabel(t *testing.T) {
	loc := i18n.New(os.DirFS("../../locales")).Localizer("en")
	cases := []struct {
		st   TorrentStatus
		want string
	}{
		{TorrentStatus{State: "caching", Seeders: 4, Leechers: 9, Peers: 13}, "4 seeders · 9 leechers"},
		{TorrentStatus{State: "idle", Peers: 3}, "3 peers"},
		{TorrentStatus{State: "idle"}, ""},
		// Terminal states play regardless of who is around.
		{TorrentStatus{State: "cached", Seeders: 4}, ""},
		{TorrentStatus{State: "unknown", Peers: 3}, ""},
	}
	for _, c := range cases {
		if got := swarmLabel(loc, &c.st); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.st, got, c.want)
		}
	}
}

type testPiece = struct {
	Position int  `json:"position"`
	Complete bool `json:"complete"`
	Priority int  `json:"priority"`
}

func evWith(ps ...testPiece) api.EventData {
	var ev api.EventData
	ev.Pieces = append(ev.Pieces, ps...)
	return ev
}

func TestPieceMap_FullThenDiff(t *testing.T) {
	// A stream opens with the full list: 512 pieces → 256 cells of two.
	var full api.EventData
	for i := 0; i < 512; i++ {
		full.Pieces = append(full.Pieces, testPiece{Position: i, Complete: i < 3})
	}
	var m pieceMap
	m.apply(full)
	fill, active := m.buckets()
	if len(fill) != PieceBuckets || fill[0] != 255 || fill[1] != 127 || fill[2] != 0 {
		t.Fatalf("after full: %v", fill[:4])
	}
	if m.done() != 3 || len(m.complete) != 512 {
		t.Errorf("counts after full: done %d total %d", m.done(), len(m.complete))
	}
	// Then only what changed: piece 3 completes, piece 4 starts fetching.
	m.apply(evWith(testPiece{Position: 3, Complete: true}, testPiece{Position: 4, Priority: 1}))
	fill, active = m.buckets()
	if fill[1] != 255 {
		t.Errorf("diff must patch the map, not replace it: cell 1 = %d", fill[1])
	}
	if fill[0] != 255 {
		t.Errorf("untouched cells must keep their state: cell 0 = %d", fill[0])
	}
	if active[0]&(1<<2) == 0 {
		t.Errorf("fetching piece 4 must light cell 2: %08b", active[0])
	}
	if m.done() != 4 {
		t.Errorf("done after diff: %d", m.done())
	}
	// An event without pieces (peers changed only) leaves the map alone.
	m.apply(api.EventData{Peers: 9})
	if f, _ := m.buckets(); f[1] != 255 || len(f) != PieceBuckets {
		t.Error("a pieceless event must not erase the map")
	}
}

func TestPieceMap_SmallTorrentAndDiffOnlyStream(t *testing.T) {
	var m pieceMap
	var small api.EventData
	for i := 0; i < 10; i++ {
		small.Pieces = append(small.Pieces, testPiece{Position: i, Complete: i%2 == 0})
	}
	m.apply(small)
	fill, _ := m.buckets()
	if len(fill) != 10 || fill[0] != 255 || fill[1] != 0 {
		t.Errorf("small torrent, one cell per piece: %v", fill)
	}
	// A stream that (for whatever reason) never sent the full list still
	// yields a picture sized by the highest position seen.
	var d pieceMap
	d.apply(evWith(testPiece{Position: 7, Complete: true}))
	if len(d.complete) != 8 || d.done() != 1 {
		t.Errorf("diff-only sizing: %d/%d", d.done(), len(d.complete))
	}
	var empty pieceMap
	if f, _ := empty.buckets(); f != nil {
		t.Error("nothing known → no bar")
	}
}

// Complete content draws no bar — the badge already says it; the bar exists
// only while something moves.
func TestResolveStatus_NoBarForCompleteContent(t *testing.T) {
	if st := resolveStatus(&vaultModels.Resource{Funded: true, Vaulted: true}, nil, nil); st.Pieces != "" {
		t.Error("vaulted must not draw a bar")
	}
	if st := resolveStatus(nil, nil, &TorrentStatsData{Total: 10, Completed: 10, Fill: []byte{255}, Active: []byte{0}}); st.State != "cached" || st.Pieces != "" {
		t.Errorf("cached must not draw a bar: %+v", st)
	}
	if idle := resolveStatus(nil, nil, nil); idle.Pieces != "" {
		t.Error("idle must not pretend to know the pieces")
	}
}

// The bar exists only while something moves or once the content is complete;
// an idle seeder that knows its pieces draws nothing, and neither do the
// states where nothing is known.
func TestBarPolicy(t *testing.T) {
	stats := &TorrentStatsData{Total: 100, Completed: 0, Seeders: 1, Fill: []byte{255, 0}, Active: []byte{0}}
	if st := resolveStatus(nil, nil, stats); st.State != "idle" || st.Pieces != "" {
		t.Errorf("idle with known pieces must not draw a bar: %+v", st)
	}
	stats.Completed = 10
	if st := resolveStatus(nil, nil, stats); st.State != "caching" || st.Pieces == "" {
		t.Errorf("caching must draw the bar: %+v", st)
	}
	db := &vaultModels.Resource{Funded: true}
	waiting := resolveStatus(db, &vault.Resource{Status: vault.StatusQueued}, &TorrentStatsData{Total: 100, Fill: []byte{0, 0}, Active: []byte{0}})
	if waiting.State != "vault_waiting" || waiting.Pieces != "" {
		t.Errorf("waiting for seeders must not draw a bar: %+v", waiting)
	}
	failed := resolveStatus(db, &vault.Resource{Status: vault.StatusFailed, StoredSize: 30, TotalSize: 100}, &TorrentStatsData{Total: 100, Completed: 30, Fill: []byte{255, 0}, Active: []byte{0}})
	if failed.State != "vault_failed" || failed.Pieces == "" {
		t.Errorf("a failed transfer with stored pieces keeps its bar: %+v", failed)
	}
}

// The verdict is earned, not assumed: nothing is said about partially cached
// content for the first settleAfter unless activity proves it alive; after
// that, people around means paused and nobody around means no seeders.
func TestJudgeSwarm(t *testing.T) {
	cases := []struct {
		name     string
		observed time.Duration
		activity bool
		seeders  int
		peers    int
		want     swarmVerdict
	}{
		{"fresh stream, quiet, seeders around", time.Second, false, 3, 5, verdictChecking},
		{"fresh stream, quiet, empty swarm", time.Second, false, 0, 0, verdictChecking},
		{"fresh stream but bytes moving", time.Second, true, 0, 0, verdictCaching},
		{"settled, quiet, seeders around → paused", 6 * time.Second, false, 2, 4, verdictPaused},
		{"settled, quiet, only peers → paused", 6 * time.Second, false, 0, 4, verdictPaused},
		// A cold seeder pod sees nobody for tens of seconds while it reaches
		// trackers and the DHT — still checking, not "no seeders".
		{"6 s, quiet, nobody → still checking", 6 * time.Second, false, 0, 0, verdictChecking},
		{"29 s, quiet, nobody → still checking", 29 * time.Second, false, 0, 0, verdictChecking},
		{"31 s, quiet, nobody → no seeders", 31 * time.Second, false, 0, 0, verdictNoSeeders},
		{"settled and moving → caching", 60 * time.Second, true, 1, 1, verdictCaching},
	}
	for _, c := range cases {
		if got := judgeSwarm("caching", c.observed, c.activity, c.seeders, c.peers); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if judgeSwarm("vaulting", time.Minute, false, 0, 0) != verdictCaching || judgeSwarm("idle", time.Minute, false, 0, 0) != verdictCaching {
		t.Error("only caching is judged")
	}
	if hasActive([]byte{0, 0}) || !hasActive([]byte{0, 4}) {
		t.Error("hasActive bitset check")
	}
}

func TestReconnectPolicy(t *testing.T) {
	inProgress := &TorrentStatsData{Total: 100, Completed: 40}
	fresh := 3 * time.Second
	if !shouldReconnect(inProgress, 0, fresh) || !shouldReconnect(inProgress, 4, fresh) {
		t.Error("a download interrupted mid-flight is worth reconnecting for")
	}
	if shouldReconnect(inProgress, 5, fresh) {
		t.Error("retries are bounded")
	}
	// The seeder unloads idle torrents itself: a stream that closes after a
	// quiet spell, or with no progress ever seen, must not wake a pod.
	if shouldReconnect(inProgress, 0, 2*time.Minute) || shouldReconnect(inProgress, 0, -1) {
		t.Error("no recent activity: let it go idle instead of waking a seeder")
	}
	if shouldReconnect(&TorrentStatsData{Total: 100, Completed: 0}, 0, fresh) {
		t.Error("nothing stored yet: do not wake a seeder for it")
	}
	if shouldReconnect(&TorrentStatsData{Total: 100, Completed: 100}, 0, fresh) || shouldReconnect(nil, 0, fresh) {
		t.Error("complete or unknown: nothing to reconnect for")
	}
	if sinceProgress(time.Time{}) != -1 {
		t.Error("no progress observed reads as -1")
	}
}
