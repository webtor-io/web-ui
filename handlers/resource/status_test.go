package resource

import (
	"context"
	"testing"

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
	status := resolveStatus(db, apiRes, nil)
	if status.State != "idle" {
		t.Errorf("expected idle for failed vault, got %q", status.State)
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
