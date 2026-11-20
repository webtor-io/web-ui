package web

import (
	"testing"

	claimsproto "github.com/webtor-io/claims-provider/proto"

	"github.com/webtor-io/web-ui/services/claims"
)

// TestUploadCap: the home page's size badge quotes the account's upload
// quota by its tier, and quotes nothing when there are no claims to read the
// tier from.
func TestUploadCap(t *testing.T) {
	tier := func(id uint32) *claims.Data {
		return &claims.Data{Context: &claimsproto.Context{Tier: &claimsproto.Tier{Id: id}}}
	}
	h := &Helper{}
	for _, tc := range []struct {
		name string
		c    *claims.Data
		want string
	}{
		{"free", tier(0), "10\u00a0GB"},
		{"no claims provider, synthetic tier", tier(1), "50\u00a0GB"},
		{"paid", tier(3), "50\u00a0GB"},
		{"no claims", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := h.UploadCap(tc.c); got != tc.want {
				t.Errorf("UploadCap = %q, want %q", got, tc.want)
			}
		})
	}
}
