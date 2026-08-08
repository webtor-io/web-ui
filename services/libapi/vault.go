package libapi

import (
	"time"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
	vaultsvc "github.com/webtor-io/web-ui/services/vault"
)

// VaultPoints is the balance side of the Vault. Total and Available are
// pointers because `null` is a real value here: it means an unlimited plan, and
// reporting it as 0 would read as "you have nothing".
type VaultPoints struct {
	Total     *float64 `json:"total" extensions:"x-nullable" example:"100"`
	Available *float64 `json:"available" extensions:"x-nullable" example:"40"`
	Funded    float64  `json:"funded" example:"60"`
	Frozen    float64  `json:"frozen" example:"20"`
	Claimable float64  `json:"claimable" example:"40"`
}

// VaultContent counts what the pledges bought: content actually stored, content
// still being transferred, and content that lost its backing.
type VaultContent struct {
	Vaulted  int `json:"vaulted" example:"3"`
	Loading  int `json:"loading" example:"1"`
	Expiring int `json:"expiring" example:"0"`
}

// Pledge is one commitment of points to a resource.
type Pledge struct {
	PledgeID   string  `json:"pledge_id" example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	ResourceID string  `json:"resource_id" example:"08ada5a7a6183aae1e09d831df6748d566095a10"`
	Name       string  `json:"name,omitempty" example:"Sintel"`
	Amount     float64 `json:"amount" example:"2"`
	// Frozen means the points cannot be claimed back yet. It says nothing
	// about whether the content is stored — Vaulted does.
	Frozen     bool      `json:"frozen" example:"true"`
	Funded     bool      `json:"funded" example:"true"`
	Vaulted    bool      `json:"vaulted" example:"false"`
	Expired    bool      `json:"expired" example:"false"`
	RequiredVP float64   `json:"required_vp" example:"2"`
	FundedVP   float64   `json:"funded_vp" example:"2"`
	CreatedAt  time.Time `json:"created_at" example:"2026-01-02T15:04:05Z"`
}

// VaultResponse is the whole Vault state in one call: the dashboard, as JSON.
type VaultResponse struct {
	Points  VaultPoints  `json:"points"`
	Content VaultContent `json:"content"`
	Pledges []Pledge     `json:"pledges"`
}

// PledgeRequest creates a pledge.
type PledgeRequest struct {
	ResourceID string `json:"resource_id" binding:"required" example:"08ada5a7a6183aae1e09d831df6748d566095a10"`
}

// Pledge transfer statuses. The vocabulary is the storage lifecycle, not the
// UI's state machine: the UI collapses queued/storing/failed into one
// "vaulting" badge because a viewer cannot act on the difference, but an API
// client can — a stuck `queued` and a `failed` are debugged differently.
const (
	// PledgeStatusWaiting: the pledge exists but the resource is not funded
	// yet, so no transfer has been asked for.
	PledgeStatusWaiting = "waiting"
	// PledgeStatusQueued: funded and handed to storage, transfer not started.
	PledgeStatusQueued = "queued"
	// PledgeStatusStoring: the transfer is running; progress applies.
	PledgeStatusStoring = "storing"
	// PledgeStatusFailed: the last transfer attempt failed. Terminal for the
	// attempt, not for the resource — storage retries on its own schedule, so
	// the right client reaction is to keep polling, not to re-pledge.
	PledgeStatusFailed = "failed"
	// PledgeStatusVaulted: the content is stored. Terminal.
	PledgeStatusVaulted = "vaulted"
	// PledgeStatusExpired: the resource lost its funding. Terminal.
	PledgeStatusExpired = "expired"
)

// PledgeStatusResponse is a pledge plus where its transfer stands. Progress and
// the sizes are pointers because absence is a real answer: a `waiting` or
// `expired` pledge has no transfer to measure, and 0 would read as "started".
type PledgeStatusResponse struct {
	Pledge
	Status     string   `json:"status" enums:"waiting,queued,storing,failed,vaulted,expired" example:"storing"`
	Progress   *float64 `json:"progress,omitempty" extensions:"x-nullable" example:"42.5"`
	StoredSize *int64   `json:"stored_size,omitempty" extensions:"x-nullable" example:"1073741824"`
	TotalSize  *int64   `json:"total_size,omitempty" extensions:"x-nullable" example:"2147483648"`
}

// NewPledgeStatus resolves the transfer status from what the web-ui DB knows
// (flattened into the Pledge) and what the storage API reports. Pure so the
// mapping is testable without either backend.
//
// resourceFunded is the resource-level flag, deliberately not Pledge.Funded:
// a resource funded by other accounts transfers all the same, and this
// pledge's status must say so.
//
// apiResource may be nil in two honest cases — the transfer has not been
// picked up yet, or this deployment runs without the storage API — and both
// read as `queued`: funded, nothing measurable yet.
func NewPledgeStatus(p Pledge, resourceFunded bool, apiResource *vaultsvc.Resource) PledgeStatusResponse {
	out := PledgeStatusResponse{Pledge: p}
	full := 100.0
	switch {
	case p.Expired:
		out.Status = PledgeStatusExpired
	case p.Vaulted:
		out.Status = PledgeStatusVaulted
	case !resourceFunded:
		out.Status = PledgeStatusWaiting
	case apiResource == nil:
		out.Status = PledgeStatusQueued
	default:
		switch apiResource.Status {
		case vaultsvc.StatusQueued:
			out.Status = PledgeStatusQueued
		case vaultsvc.StatusCompleted:
			// Storage finished but the completion event has not reached the
			// web-ui DB yet. Report the truth, not the lag.
			out.Status = PledgeStatusVaulted
		case vaultsvc.StatusFailed:
			out.Status = PledgeStatusFailed
		default:
			// StatusProcessing, and any status newer than this code: a
			// transfer state we cannot name is still a transfer in progress.
			out.Status = PledgeStatusStoring
		}
		if apiResource.TotalSize > 0 {
			progress := apiResource.GetProgress()
			out.Progress = &progress
			stored, total := apiResource.StoredSize, apiResource.TotalSize
			out.StoredSize = &stored
			out.TotalSize = &total
		}
	}
	if out.Status == PledgeStatusVaulted {
		out.Progress = &full
	}
	return out
}

// NewPledge flattens the pledge and its resource into the wire shape. The
// resource may be missing (a pledge loaded without its relation), and the
// zero values are honest in that case — the resource-side fields all describe
// storage state, which is unknown rather than false.
func NewPledge(p *vaultModels.Pledge, r *vaultModels.Resource, frozen bool) Pledge {
	out := Pledge{
		PledgeID:   p.PledgeID.String(),
		ResourceID: p.ResourceID,
		Amount:     p.Amount,
		Frozen:     frozen,
		Funded:     p.Funded,
		CreatedAt:  p.CreatedAt,
	}
	if r != nil {
		out.Name = r.Name
		out.Vaulted = r.Vaulted
		out.Expired = r.Expired
		out.RequiredVP = r.RequiredVP
		out.FundedVP = r.FundedVP
	}
	return out
}
