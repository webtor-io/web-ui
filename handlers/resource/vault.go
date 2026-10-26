package resource

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

type VaultPledgeAddForm struct {
	Available     *float64
	Total         *float64
	Required      float64
	TorrentSizeGB float64
	Status        string
	ErrKey        string
	Funded        bool
	Vaulted       bool
	ResourceID    string
	// FreezePeriod is how long a new pledge stays frozen, said before the
	// click; FrozenFor what is left of the viewer's own pledge's freeze, 0
	// when it has none or it is over (Remove is offered only then).
	FreezePeriod time.Duration
	FrozenFor    time.Duration
}

type VaultButton struct {
	Funded bool
}

type VaultPledgeRemoveForm struct {
	FrozenFor    time.Duration // 0: not frozen
	FreezePeriod time.Duration
	Status       string
	ErrKey       string
	ResourceID   string
}

func (s *Handler) prepareVaultPledgeAddForm(c *gin.Context, args *GetArgs) (*VaultPledgeAddForm, error) {
	ctx := c.Request.Context()

	// Get user vault stats
	stats, _, err := s.vault.GetUserStats(ctx, args.User)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user vault stats")
	}

	// Get required VP using vault service (1 VP = 1 GB of the torrent's size)
	requiredVP, err := s.vault.GetRequiredVP(ctx, args.Claims, args.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get required VP")
	}

	vaultForm := &VaultPledgeAddForm{
		Available:     stats.Available,
		Total:         stats.Total,
		Required:      requiredVP,
		TorrentSizeGB: requiredVP,
		Funded:        false,
		Vaulted:       false,
		ResourceID:    args.ID,
		FreezePeriod:  s.vault.GetFreezePeriod(),
	}

	// Check if user is supporting this torrent
	resource, err := s.vault.GetResource(ctx, args.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get vault resource")
	}

	if resource != nil {
		// Check if resource is vaulted
		if resource.Vaulted {
			vaultForm.Vaulted = true
		}

		// Check if user is funding this torrent
		if resource.Funded {
			pledge, err := s.vault.GetPledge(ctx, args.User, resource)
			if err != nil {
				return nil, errors.Wrap(err, "failed to get user pledge")
			}

			if pledge != nil && pledge.Funded {
				vaultForm.Funded = true
			}
			if pledge != nil {
				vaultForm.FrozenFor = s.vault.FrozenFor(pledge)
			}
		}
	}

	// Handle redirect from vault handler
	if c.Query("from") == "/vault/add" {
		status := c.Query("status")
		if status == "error" {
			vaultForm.Status = "error"
			vaultForm.ErrKey = c.Query("err")
		} else if status == "success" {
			vaultForm.Status = "success"
		}
	}

	return vaultForm, nil
}

func (s *Handler) prepareVaultButton(ctx context.Context, args *GetArgs) (*VaultButton, error) {
	vaultButton := &VaultButton{
		Funded: false,
	}

	// Get resource from vault using service
	resource, err := s.vault.GetResource(ctx, args.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get vault resource")
	}

	// If resource doesn't exist or not funded, return default
	if resource == nil || !resource.Funded {
		return vaultButton, nil
	}

	// Get user's pledge for this resource using service
	pledge, err := s.vault.GetPledge(ctx, args.User, resource)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user pledge")
	}

	// If user has no pledge, return default
	if pledge == nil {
		return vaultButton, nil
	}

	// Set Funded to true only if both resource and pledge are funded
	if resource.Funded && pledge.Funded {
		vaultButton.Funded = true
	}

	return vaultButton, nil
}

func (s *Handler) prepareVaultPledgeRemoveForm(c *gin.Context, args *GetArgs) (*VaultPledgeRemoveForm, error) {
	ctx := c.Request.Context()

	form := &VaultPledgeRemoveForm{
		FreezePeriod: s.vault.GetFreezePeriod(),
		ResourceID:   args.ID,
	}

	// Handle redirect from vault handler
	if c.Query("from") == "/vault/remove" {
		status := c.Query("status")
		if status == "error" {
			form.Status = "error"
			form.ErrKey = c.Query("err")
		} else if status == "success" {
			form.Status = "success"
		}
		return form, nil
	}

	// Get vault resource
	resource, err := s.vault.GetResource(ctx, args.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get vault resource")
	}

	// If resource doesn't exist, return form with nothing frozen
	if resource == nil {
		return form, nil
	}

	// Get user's pledge for this resource
	pledge, err := s.vault.GetPledge(ctx, args.User, resource)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user pledge")
	}

	// If pledge doesn't exist, return form with nothing frozen
	if pledge == nil {
		return form, nil
	}

	form.FrozenFor = s.vault.FrozenFor(pledge)
	return form, nil
}
