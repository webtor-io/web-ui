package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/libapi"
	"github.com/webtor-io/web-ui/services/vault"
)

// getVault godoc
//
//	@Summary		Vault balance and pledges
//	@Description	Vault Points balance plus every pledge the account holds. 1 VP = 1 GB of torrent size; a `total` of
//	@Description	`null` means an unlimited plan.
//	@Description
//	@Description	A pledge is frozen for a period after it is created and cannot be removed while it is — `frozen` says
//	@Description	whether that applies right now, not whether the content is stored.
//	@Tags			vault
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	libapi.VaultResponse
//	@Failure		401	{object}	libapi.ErrorResponse
//	@Failure		402	{object}	libapi.ErrorResponse
//	@Failure		503	{object}	libapi.ErrorResponse	"Vault is not configured on this deployment"
//	@Router			/vault [get]
func (s *Handler) getVault(c *gin.Context) {
	if err := s.requireVault(); err != nil {
		s.abort(c, err)
		return
	}
	u := auth.GetUserFromContext(c)
	stats, enriched, err := s.vault.GetUserStats(c.Request.Context(), u)
	if err != nil {
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to load vault stats", err))
		return
	}
	c.JSON(http.StatusOK, buildVaultResponse(stats, enriched))
}

// postVaultPledge godoc
//
//	@Summary		Pledge Vault Points to a torrent
//	@Description	Creates a pledge, which is what keeps a torrent available long-term. Idempotent per resource: an
//	@Description	account holds at most one pledge for a given resource, and pledging again answers `409`.
//	@Description
//	@Description	The required amount is derived from the torrent size, so a pledge fails with `403` when the account
//	@Description	does not have that many points available.
//	@Tags			vault
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		libapi.PledgeRequest	true	"Resource to pledge to"
//	@Success		201		{object}	libapi.Pledge
//	@Failure		400		{object}	libapi.ErrorResponse
//	@Failure		401		{object}	libapi.ErrorResponse
//	@Failure		402		{object}	libapi.ErrorResponse
//	@Failure		403		{object}	libapi.ErrorResponse	"Not enough Vault Points"
//	@Failure		409		{object}	libapi.ErrorResponse	"A pledge for this resource already exists"
//	@Failure		503		{object}	libapi.ErrorResponse	"Vault is not configured on this deployment"
//	@Router			/vault/pledges [post]
func (s *Handler) postVaultPledge(c *gin.Context) {
	if err := s.requireVault(); err != nil {
		s.abort(c, err)
		return
	}
	var req libapi.PledgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest,
			"body must be a JSON object with \"resource_id\"", err))
		return
	}
	u := auth.GetUserFromContext(c)
	p, err := s.createPledge(c.Request.Context(), normalizeResourceID(req.ResourceID), u, api.GetClaimsFromContext(c))
	if err != nil {
		s.abort(c, err)
		return
	}
	c.JSON(http.StatusCreated, p)
}

// deleteVaultPledge godoc
//
//	@Summary		Claim Vault Points back
//	@Description	Removes the pledge for a resource and returns its points to the balance. A pledge that is still
//	@Description	frozen cannot be removed and answers `409`.
//	@Tags			vault
//	@Produce		json
//	@Security		BearerAuth
//	@Param			resource_id	path	string	true	"Resource (infohash) the pledge is held against"
//	@Success		204			"Removed"
//	@Failure		401			{object}	libapi.ErrorResponse
//	@Failure		402			{object}	libapi.ErrorResponse
//	@Failure		404			{object}	libapi.ErrorResponse
//	@Failure		409			{object}	libapi.ErrorResponse	"Pledge is frozen"
//	@Failure		503			{object}	libapi.ErrorResponse	"Vault is not configured on this deployment"
//	@Router			/vault/pledges/{resource_id} [delete]
func (s *Handler) deleteVaultPledge(c *gin.Context) {
	if err := s.requireVault(); err != nil {
		s.abort(c, err)
		return
	}
	u := auth.GetUserFromContext(c)
	if err := s.removePledge(c.Request.Context(), c.Param("resource_id"), u); err != nil {
		s.abort(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// createPledge is the business level: no gin, so it says what went wrong rather
// than how it is rendered. Same sequence as handlers/vault.createPledge — the
// two call the identical service methods so the API and the UI cannot drift.
func (s *Handler) createPledge(ctx context.Context, resourceID string, u *auth.User, cl *api.Claims) (*libapi.Pledge, *libapi.Error) {
	if resourceID == "" {
		return nil, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, "resource_id is required", nil)
	}
	if cl == nil {
		return nil, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to get claims", nil)
	}
	res, err := s.vault.GetOrCreateResource(ctx, cl, resourceID)
	if err != nil {
		return nil, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to resolve the resource", err)
	}
	existing, err := s.vault.GetPledge(ctx, u, res)
	if err != nil {
		return nil, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to check the existing pledge", err)
	}
	if existing != nil {
		return nil, libapi.NewError(http.StatusConflict, libapi.CodeConflict, "a pledge for this resource already exists", nil)
	}
	p, err := s.vault.CreatePledge(ctx, u, res)
	if err != nil {
		// Only the underfunded case is the caller's to act on. Everything else
		// the transaction can fail with is a wrapped DB error, and passing
		// err.Error() straight to the client would both mislabel a server fault
		// as "forbidden" and put our internals in the response body.
		if strings.Contains(err.Error(), "insufficient vault points") {
			return nil, libapi.NewError(http.StatusForbidden, libapi.CodeForbidden,
				"not enough vault points available for this resource", err)
		}
		return nil, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to create the pledge", err)
	}
	frozen, err := s.vault.IsPledgeFrozen(ctx, p)
	if err != nil {
		return nil, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to check the freeze status", err)
	}
	out := libapi.NewPledge(p, res, frozen)
	return &out, nil
}

func (s *Handler) removePledge(ctx context.Context, resourceID string, u *auth.User) *libapi.Error {
	resourceID = normalizeResourceID(resourceID)
	if resourceID == "" {
		return libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, "resource_id is required", nil)
	}
	res, err := s.vault.GetResource(ctx, resourceID)
	if err != nil {
		return libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to get the vault resource", err)
	}
	if res == nil {
		return libapi.NewError(http.StatusNotFound, libapi.CodeNotFound, "no pledge for this resource", nil)
	}
	p, err := s.vault.GetPledge(ctx, u, res)
	if err != nil {
		return libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to get the pledge", err)
	}
	if p == nil {
		return libapi.NewError(http.StatusNotFound, libapi.CodeNotFound, "no pledge for this resource", nil)
	}
	frozen, err := s.vault.IsPledgeFrozen(ctx, p)
	if err != nil {
		return libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to check the freeze status", err)
	}
	if frozen {
		return libapi.NewError(http.StatusConflict, libapi.CodeConflict, "the pledge is frozen and cannot be removed yet", nil)
	}
	if err := s.vault.RemovePledge(ctx, p); err != nil {
		return libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to remove the pledge", err)
	}
	return nil
}

// requireVault reports that this deployment runs without Vault (self-hosted
// installs do). 503 rather than 404: the route exists, the dependency does not.
func (s *Handler) requireVault() *libapi.Error {
	if s.vault == nil {
		return libapi.NewError(http.StatusServiceUnavailable, libapi.CodeUnavailable,
			"vault is not available on this deployment", nil)
	}
	return nil
}

func buildVaultResponse(stats *vault.UserStats, enriched []vault.EnrichedPledge) *libapi.VaultResponse {
	res := &libapi.VaultResponse{
		Pledges: make([]libapi.Pledge, 0, len(enriched)),
	}
	if stats != nil {
		res.Points = libapi.VaultPoints{
			Total:     stats.Total,
			Available: stats.Available,
			Funded:    stats.Funded,
			Frozen:    stats.Frozen,
			Claimable: stats.Claimable,
		}
		res.Content = libapi.VaultContent{
			Vaulted:  stats.VaultedCount,
			Loading:  stats.LoadingCount,
			Expiring: stats.ExpiringCount,
		}
	}
	for i := range enriched {
		e := &enriched[i]
		res.Pledges = append(res.Pledges, libapi.NewPledge(&e.Pledge, e.Pledge.Resource, e.IsFrozen))
	}
	return res
}
