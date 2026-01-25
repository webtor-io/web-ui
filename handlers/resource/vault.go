package resource

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/api"
)

type VaultPledgeAddForm struct {
	Available     *float64
	Total         *float64
	Required      float64
	TorrentSizeGB float64
	Status        string
	Err           error
	Funded        bool
	Vaulted       bool
}

type VaultButton struct {
	Funded bool
}

type VaultPledgeRemoveForm struct {
	Frozen bool
	Status string
	Err    error
}

func (s *Handler) prepareVaultPledgeAddForm(c *gin.Context, args *GetArgs) (*VaultPledgeAddForm, error) {
	ctx := c.Request.Context()

	// Get user vault stats
	stats, err := s.vault.GetUserStats(ctx, args.User)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user vault stats")
	}

	// Get required VP using vault service
	requiredVP, err := s.vault.GetRequiredVP(ctx, args.Claims, args.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get required VP")
	}

	// Get torrent size separately via REST API
	list, err := s.api.ListResourceContentCached(ctx, args.Claims, args.ID, &api.ListResourceContentArgs{
		Output: api.OutputList,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list resource content for size calculation")
	}

	// Convert bytes to GB
	torrentSizeGB := float64(list.Size) / (1024 * 1024 * 1024)

	vaultForm := &VaultPledgeAddForm{
		Available:     stats.Available,
		Total:         stats.Total,
		Required:      requiredVP,
		TorrentSizeGB: torrentSizeGB,
		Funded:        false,
		Vaulted:       false,
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
		}
	}

	// Handle redirect from vault handler
	if c.Query("from") == "/vault/pledge/add" {
		status := c.Query("status")
		if status == "error" {
			vaultForm.Status = "error"
			errMsg := c.Query("err")
			if errMsg != "" {
				vaultForm.Err = errors.New(errMsg)
			}
		} else if status == "success" {
			vaultForm.Status = "success"
			vaultForm.Err = nil
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
		Frozen: false,
	}

	// Handle redirect from vault handler
	if c.Query("from") == "/vault/pledge/remove" {
		status := c.Query("status")
		if status == "error" {
			form.Status = "error"
			errMsg := c.Query("err")
			if errMsg != "" {
				form.Err = errors.New(errMsg)
			}
		} else if status == "success" {
			form.Status = "success"
			form.Err = nil
		}
		return form, nil
	}

	// Get vault resource
	resource, err := s.vault.GetResource(ctx, args.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get vault resource")
	}

	// If resource doesn't exist, return form with frozen=false
	if resource == nil {
		return form, nil
	}

	// Get user's pledge for this resource
	pledge, err := s.vault.GetPledge(ctx, args.User, resource)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user pledge")
	}

	// If pledge doesn't exist, return form with frozen=false
	if pledge == nil {
		return form, nil
	}

	// Check if pledge is frozen
	isFrozen, err := s.vault.IsPledgeFrozen(pledge)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check pledge frozen status")
	}

	form.Frozen = isFrozen
	return form, nil
}
