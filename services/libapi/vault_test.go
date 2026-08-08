package libapi

import (
	"testing"

	vaultsvc "github.com/webtor-io/web-ui/services/vault"
)

// The status mapping is the contract clients poll against; every branch is
// pinned here so a refactor that drops one fails loudly.
func TestNewPledgeStatus(t *testing.T) {
	storing := &vaultsvc.Resource{Status: vaultsvc.StatusProcessing, StoredSize: 50, TotalSize: 200}
	for _, tc := range []struct {
		name           string
		pledge         Pledge
		resourceFunded bool
		apiResource    *vaultsvc.Resource
		status         string
		progress       *float64
		storedSize     *int64
		totalSize      *int64
	}{
		{
			name:   "expired wins over everything",
			pledge: Pledge{Expired: true, Vaulted: true}, resourceFunded: true, apiResource: storing,
			status: PledgeStatusExpired,
		},
		{
			name:   "vaulted in the db is terminal",
			pledge: Pledge{Vaulted: true}, resourceFunded: true,
			status: PledgeStatusVaulted, progress: f64(100),
		},
		{
			name:   "unfunded resource waits even when the pledge itself is funded",
			pledge: Pledge{Funded: true}, resourceFunded: false,
			status: PledgeStatusWaiting,
		},
		{
			name:           "funded with no storage answer is queued, not started",
			resourceFunded: true,
			status:         PledgeStatusQueued,
		},
		{
			name:           "storage queued",
			resourceFunded: true, apiResource: &vaultsvc.Resource{Status: vaultsvc.StatusQueued, TotalSize: 200},
			status: PledgeStatusQueued, progress: f64(0), storedSize: i64(0), totalSize: i64(200),
		},
		{
			name:           "storing reports byte progress",
			resourceFunded: true, apiResource: storing,
			status: PledgeStatusStoring, progress: f64(25), storedSize: i64(50), totalSize: i64(200),
		},
		{
			name:           "storage finished before the completion event landed",
			resourceFunded: true, apiResource: &vaultsvc.Resource{Status: vaultsvc.StatusCompleted, StoredSize: 200, TotalSize: 200},
			status: PledgeStatusVaulted, progress: f64(100), storedSize: i64(200), totalSize: i64(200),
		},
		{
			name:           "failed keeps the partial progress",
			resourceFunded: true, apiResource: &vaultsvc.Resource{Status: vaultsvc.StatusFailed, StoredSize: 150, TotalSize: 200},
			status: PledgeStatusFailed, progress: f64(75), storedSize: i64(150), totalSize: i64(200),
		},
		{
			name:           "a status this code cannot name is still a transfer in progress",
			resourceFunded: true, apiResource: &vaultsvc.Resource{Status: 42, StoredSize: 10, TotalSize: 200},
			status: PledgeStatusStoring, progress: f64(5), storedSize: i64(10), totalSize: i64(200),
		},
		{
			name:           "zero total size measures nothing",
			resourceFunded: true, apiResource: &vaultsvc.Resource{Status: vaultsvc.StatusProcessing},
			status: PledgeStatusStoring,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := NewPledgeStatus(tc.pledge, tc.resourceFunded, tc.apiResource)
			if got.Status != tc.status {
				t.Fatalf("status = %q, want %q", got.Status, tc.status)
			}
			checkF64(t, "progress", got.Progress, tc.progress)
			checkI64(t, "stored_size", got.StoredSize, tc.storedSize)
			checkI64(t, "total_size", got.TotalSize, tc.totalSize)
		})
	}
}

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }

func checkF64(t *testing.T, name string, got, want *float64) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Fatalf("%s = %v, want %v", name, fmtF64(got), fmtF64(want))
	}
	if got != nil && *got != *want {
		t.Fatalf("%s = %v, want %v", name, *got, *want)
	}
}

func checkI64(t *testing.T, name string, got, want *int64) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	if got != nil && *got != *want {
		t.Fatalf("%s = %v, want %v", name, *got, *want)
	}
}

func fmtF64(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
