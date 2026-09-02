package resource

import (
	"context"
	"os"
	"testing"

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
