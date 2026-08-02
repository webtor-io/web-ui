package libapi

import (
	"time"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
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
