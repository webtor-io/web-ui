package vault

import (
	"context"
	"net/http"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
)

const (
	vaultPledgeFreezePeriodFlag            = "vault-pledge-freeze-period"
	VaultResourceExpirePeriodFlag          = "vault-resource-expire-period"
	vaultResourceTransferTimeoutPeriodFlag = "vault-resource-transfer-timeout-period"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.DurationFlag{
			Name:   vaultPledgeFreezePeriodFlag,
			Usage:  "vault pledge freeze period",
			Value:  24 * time.Hour,
			EnvVar: "VAULT_PLEDGE_FREEZE_PERIOD",
		},
		cli.DurationFlag{
			Name:   VaultResourceExpirePeriodFlag,
			Usage:  "period after which unfunded resource is removed from vault",
			Value:  7 * 24 * time.Hour,
			EnvVar: "VAULT_RESOURCE_EXPIRE_PERIOD",
		},
		cli.DurationFlag{
			Name:   vaultResourceTransferTimeoutPeriodFlag,
			Usage:  "period after which resource is removed and transfer attempts are stopped",
			Value:  7 * 24 * time.Hour,
			EnvVar: "VAULT_RESOURCE_TRANSFER_TIMEOUT_PERIOD",
		},
	)
}

type Vault struct {
	vaultApi              *Api
	claims                *claims.Claims
	client                *http.Client
	pg                    *cs.PG
	api                   *api.Api
	freezePeriod          time.Duration
	expirePeriod          time.Duration
	transferTimeoutPeriod time.Duration
}

func New(c *cli.Context, vaultApi *Api, cl *claims.Claims, client *http.Client, pg *cs.PG, restApi *api.Api) *Vault {
	// Return nil if vaultApi is not configured
	if vaultApi == nil {
		return nil
	}

	freezePeriod := c.Duration(vaultPledgeFreezePeriodFlag)
	expirePeriod := c.Duration(VaultResourceExpirePeriodFlag)
	transferTimeoutPeriod := c.Duration(vaultResourceTransferTimeoutPeriodFlag)

	return &Vault{
		vaultApi:              vaultApi,
		claims:                cl,
		client:                client,
		pg:                    pg,
		api:                   restApi,
		freezePeriod:          freezePeriod,
		expirePeriod:          expirePeriod,
		transferTimeoutPeriod: transferTimeoutPeriod,
	}
}

// GetFreezePeriod returns the pledge freeze period
func (s *Vault) GetFreezePeriod() time.Duration {
	return s.freezePeriod
}

// GetExpirePeriod returns the resource expire period
func (s *Vault) GetExpirePeriod() time.Duration {
	return s.expirePeriod
}

// GetTransferTimeoutPeriod returns the resource transfer timeout period
func (s *Vault) GetTransferTimeoutPeriod() time.Duration {
	return s.transferTimeoutPeriod
}

// UpdateUserVP updates user vault points based on claims
func (s *Vault) UpdateUserVP(ctx context.Context, user *auth.User) (*vaultModels.UserVP, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database connection is not available")
	}

	// Get current claims
	claimsData, err := s.claims.Get(&claims.Request{
		Email:         user.Email,
		PatreonUserID: user.PatreonUserID,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get claims")
	}

	// Extract vault points from claims (optional field)
	var claimsPoints *float64
	if claimsData.Claims != nil && claimsData.Claims.Vault != nil && claimsData.Claims.Vault.Points != nil {
		points := float64(*claimsData.Claims.Vault.Points)
		claimsPoints = &points
	}

	p := float64(0)
	claimsPoints = &p

	// Execute in transaction with SELECT FOR UPDATE
	var result *vaultModels.UserVP
	err = db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		// Lock the row for update
		existingVP := &vaultModels.UserVP{}
		err := tx.Model(existingVP).
			Where("user_id = ?", user.ID).
			For("UPDATE").
			Select()

		if err != nil && !errors.Is(err, pg.ErrNoRows) {
			return errors.Wrap(err, "failed to lock user vault points")
		}

		recordExists := !errors.Is(err, pg.ErrNoRows)

		if !recordExists {
			// Case 1 & 2: No record exists - create new one
			vp := &vaultModels.UserVP{
				UserID: user.ID,
				Total:  claimsPoints,
			}

			_, err := tx.Model(vp).
				Context(ctx).
				Insert()
			if err != nil {
				return errors.Wrap(err, "failed to create user vault points")
			}

			// Create tx_log entry only if points are defined (not unlimited) and not zero (free tier)
			if claimsPoints != nil && *claimsPoints != 0 {
				txLog := &vaultModels.TxLog{
					UserID:     user.ID,
					ResourceID: nil,
					Balance:    *claimsPoints,
					OpType:     vaultModels.OpTypeChangeTier,
				}
				_, err = tx.Model(txLog).Context(ctx).Insert()
				if err != nil {
					return errors.Wrap(err, "failed to create change tier log")
				}
			}

			result = vp
			return nil
		}

		// Record exists - check if update is needed
		// Case 3: Points match - do nothing
		if pointsEqual(existingVP.Total, claimsPoints) {
			result = existingVP
			return nil
		}

		// Case 4: Points differ - update and log the change
		// Calculate the difference (treat NULL as 0 for logging)
		oldValue := float64(0)
		if existingVP.Total != nil {
			oldValue = *existingVP.Total
		}

		newValue := float64(0)
		if claimsPoints != nil {
			newValue = *claimsPoints
		}

		difference := newValue - oldValue

		// Update the total
		_, err = tx.Model(&vaultModels.UserVP{}).
			Context(ctx).
			Set("total = ?", claimsPoints).
			Where("user_id = ?", user.ID).
			Update()
		if err != nil {
			return errors.Wrap(err, "failed to update user vault points")
		}

		// Create tx_log entry for the change only if new value is not zero (free tier)
		if newValue != 0 {
			txLog := &vaultModels.TxLog{
				UserID:     user.ID,
				ResourceID: nil,
				Balance:    difference,
				OpType:     vaultModels.OpTypeChangeTier,
			}
			_, err = tx.Model(txLog).Context(ctx).Insert()
			if err != nil {
				return errors.Wrap(err, "failed to create change tier log")
			}
		}

		// Recalculate pledge funding based on new total
		err = s.recalculatePledgeFunding(ctx, tx, user, claimsPoints)
		if err != nil {
			return errors.Wrap(err, "failed to recalculate pledge funding")
		}

		// Fetch updated record
		updatedVP := &vaultModels.UserVP{}
		err = tx.Model(updatedVP).
			Where("user_id = ?", user.ID).
			Select()
		if err != nil {
			return errors.Wrap(err, "failed to fetch updated user vault points")
		}

		result = updatedVP
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// UpdateUserVPIfExists updates user vault points only if user already has a record in Vault
func (s *Vault) UpdateUserVPIfExists(ctx context.Context, user *auth.User) (*vaultModels.UserVP, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database connection is not available")
	}
	vp, err := vaultModels.GetUserVP(ctx, db, user.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check user vault points")
	}
	if vp == nil {
		return nil, nil
	}
	return s.UpdateUserVP(ctx, user)
}

// UserStats represents user vault points statistics
type UserStats struct {
	Total     *float64 // Total vault points (nil if unlimited)
	Frozen    float64  // Points in frozen and funded pledges
	Funded    float64  // Points in funded pledges
	Available *float64 // Total minus funded (nil if total is nil)
	Claimable float64  // Funded but not frozen
}

// GetUserStats returns vault points statistics for a user
func (s *Vault) GetUserStats(ctx context.Context, user *auth.User) (*UserStats, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database connection is not available")
	}

	// Update user VP first
	userVP, err := s.UpdateUserVP(ctx, user)
	if err != nil {
		return nil, errors.Wrap(err, "failed to update user vault points")
	}

	// Fetch all pledges for user in one query
	pledges, err := vaultModels.GetUserPledges(ctx, db, user.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user pledges")
	}

	// Calculate statistics in application
	stats := &UserStats{
		Total:     userVP.Total,
		Frozen:    0,
		Funded:    0,
		Available: nil,
		Claimable: 0,
	}

	// Process pledges
	for _, pledge := range pledges {
		// Check if pledge is frozen using IsPledgeFrozen method
		isFrozen, err := s.IsPledgeFrozen(ctx, &pledge)
		if err != nil {
			// If error checking frozen status, skip this pledge
			continue
		}

		// Frozen: sum of pledges that are frozen AND funded
		if isFrozen && pledge.Funded {
			stats.Frozen += pledge.Amount
		}

		// Funded: sum of all pledges with funded=true
		if pledge.Funded {
			stats.Funded += pledge.Amount
		}

		// Claimable: funded but not frozen
		if pledge.Funded && !isFrozen {
			stats.Claimable += pledge.Amount
		}
	}

	// Ensure Funded is never negative
	if stats.Funded < 0 {
		stats.Funded = 0
	}

	// Available: total minus funded (nil if total is nil)
	if userVP.Total != nil {
		available := *userVP.Total - stats.Funded
		// Ensure Available is never negative
		if available < 0 {
			available = 0
		}
		stats.Available = &available
	}

	return stats, nil
}

// CreatePledge creates a new pledge for a resource
func (s *Vault) CreatePledge(ctx context.Context, user *auth.User, resource *vaultModels.Resource) (*vaultModels.Pledge, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database connection is not available")
	}

	// Execute in transaction
	var result *vaultModels.Pledge
	err := db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		// Lock user VP for update
		userVP := &vaultModels.UserVP{}
		err := tx.Model(userVP).
			Where("user_id = ?", user.ID).
			For("UPDATE").
			Select()

		if err != nil {
			if errors.Is(err, pg.ErrNoRows) {
				return errors.New("user vault points not found")
			}
			return errors.Wrap(err, "failed to lock user vault points")
		}

		// Check if user has unlimited VP (Total = nil)
		if userVP.Total == nil {
			// Unlimited VP - allow pledge creation
		} else {
			// Calculate available VP
			// Get all funded pledges for user
			var pledges []vaultModels.Pledge
			err := tx.Model(&pledges).
				Context(ctx).
				Where("user_id = ?", user.ID).
				Select()
			if err != nil {
				return errors.Wrap(err, "failed to get user pledges")
			}

			// Sum funded pledges
			fundedSum := float64(0)
			for _, pledge := range pledges {
				if pledge.Funded {
					fundedSum += pledge.Amount
				}
			}

			available := *userVP.Total - fundedSum

			// Check if user has enough available VP
			if available < resource.RequiredVP {
				return errors.New("insufficient vault points")
			}
		}

		// Create pledge with Funded=true, FrozenAt=now()
		now := time.Now()
		pledge := &vaultModels.Pledge{
			UserID:     user.ID,
			ResourceID: resource.ResourceID,
			Amount:     resource.RequiredVP,
			Funded:     true,
			FrozenAt:   now,
		}

		_, err = tx.Model(pledge).
			Context(ctx).
			Insert()
		if err != nil {
			return errors.Wrap(err, "failed to create pledge")
		}

		// Create tx_log entry with OpTypeFund (negative balance)
		// OpTypeFund is always negative according to documentation
		txLog := &vaultModels.TxLog{
			UserID:     user.ID,
			ResourceID: &resource.ResourceID,
			Balance:    -resource.RequiredVP,
			OpType:     vaultModels.OpTypeFund,
		}
		_, err = tx.Model(txLog).Context(ctx).Insert()
		if err != nil {
			return errors.Wrap(err, "failed to create fund log")
		}

		// Update resource: increase funded_vp by pledge amount
		newFundedVP := resource.FundedVP + resource.RequiredVP
		err = vaultModels.UpdateResourceFundedVP(ctx, tx, resource.ResourceID, newFundedVP)
		if err != nil {
			return errors.Wrap(err, "failed to update resource funded VP")
		}

		// If funded_vp >= required_vp, mark resource as funded and unexpired
		if newFundedVP >= resource.RequiredVP {
			err = vaultModels.MarkResourceUnexpiredAndFunded(ctx, tx, resource.ResourceID)
			if err != nil {
				return errors.Wrap(err, "failed to mark resource as unexpired and funded")
			}

			// Put to Vault API when resource transitions to Funded state
			vaulted, err := s.putResourceToVaultAPI(ctx, tx, resource)
			if err != nil {
				return errors.Wrap(err, "failed to sync resource with vault api")
			}
			if vaulted {
				err := vaultModels.UpdateResourceVaulted(ctx, tx, resource.ResourceID)
				if err != nil {
					return errors.Wrap(err, "failed to set resource vaulted")
				}
			}
		}

		result = pledge
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetOrCreateResource retrieves an existing resource or creates a new one if it doesn't exist
func (s *Vault) GetOrCreateResource(ctx context.Context, claims *api.Claims, resourceID string) (*vaultModels.Resource, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database connection is not available")
	}

	// Check if resource exists
	resource, err := vaultModels.GetResource(ctx, db, resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get resource")
	}

	// If resource exists, return it
	if resource != nil {
		return resource, nil
	}

	requiredVP, err := s.GetRequiredVP(ctx, claims, resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get required vault points")
	}

	torrentName, err := s.getTorrentName(ctx, claims, resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get torrent name")
	}

	// Create new resource
	resource, err = vaultModels.CreateResource(ctx, db, resourceID, requiredVP, torrentName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create resource")
	}

	return resource, nil
}

// getTorrentName extracts torrent name from API response
func (s *Vault) getTorrentName(ctx context.Context, claims *api.Claims, resourceID string) (string, error) {
	r, err := s.api.GetResourceCached(ctx, claims, resourceID)
	if err != nil {
		return "", errors.Wrap(err, "failed to get resource")
	}

	return r.Name, nil
}

// GetRequiredVP calculates the required vault points for a resource based on its total size
func (s *Vault) GetRequiredVP(ctx context.Context, claims *api.Claims, resourceID string) (float64, error) {
	// Get list to calculate total size
	list, err := s.api.ListResourceContentCached(ctx, claims, resourceID, &api.ListResourceContentArgs{
		Output: api.OutputList,
	})
	if err != nil {
		return 0, errors.Wrap(err, "failed to list resource content")
	}

	// Convert bytes to VP (1 VP = 1 GB)
	requiredVP := float64(list.Size) / (1024 * 1024 * 1024)

	return requiredVP, nil
}

// GetResource retrieves a resource by ID, returns nil if not found
func (s *Vault) GetResource(ctx context.Context, resourceID string) (*vaultModels.Resource, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database connection is not available")
	}

	resource, err := vaultModels.GetResource(ctx, db, resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get resource")
	}

	return resource, nil
}

// GetPledge retrieves a pledge for a specific user and resource, returns nil if not found
func (s *Vault) GetPledge(ctx context.Context, user *auth.User, resource *vaultModels.Resource) (*vaultModels.Pledge, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database connection is not available")
	}

	pledge, err := vaultModels.GetUserResourcePledge(ctx, db, user.ID, resource.ResourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user resource pledge")
	}

	return pledge, nil
}

// IsPledgeFrozen checks if a pledge is currently in the freeze period
// A pledge is not frozen if the resource is vaulted, regardless of the freeze period
func (s *Vault) IsPledgeFrozen(ctx context.Context, pledge *vaultModels.Pledge) (bool, error) {
	if pledge == nil {
		return false, errors.New("pledge is nil")
	}

	db := s.pg.Get()
	if db == nil {
		return false, errors.New("database connection is not available")
	}

	// Get the resource to check if it's vaulted
	resource, err := vaultModels.GetResource(ctx, db, pledge.ResourceID)
	if err != nil {
		return false, errors.Wrap(err, "failed to get resource")
	}

	// If resource is vaulted, pledge is not frozen
	if resource != nil && resource.Vaulted {
		return false, nil
	}

	// Calculate the time when freeze period ends
	freezeEndTime := pledge.FrozenAt.Add(s.freezePeriod)

	// Check if current time is still within freeze period
	now := time.Now()
	isFrozen := now.Before(freezeEndTime)

	return isFrozen, nil
}

// RemovePledge removes a pledge and updates the resource accordingly
func (s *Vault) RemovePledge(ctx context.Context, pledge *vaultModels.Pledge) error {
	if pledge == nil {
		return errors.New("pledge is nil")
	}

	db := s.pg.Get()
	if db == nil {
		return errors.New("database connection is not available")
	}

	// Execute in transaction
	err := db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		// Lock the resource for update
		resource := &vaultModels.Resource{}
		err := tx.Model(resource).
			Where("resource_id = ?", pledge.ResourceID).
			For("UPDATE").
			Select()
		if err != nil {
			if errors.Is(err, pg.ErrNoRows) {
				return errors.New("resource not found")
			}
			return errors.Wrap(err, "failed to lock resource")
		}

		// Delete the pledge
		err = vaultModels.DeletePledge(ctx, tx, pledge.PledgeID)
		if err != nil {
			return errors.Wrap(err, "failed to delete pledge")
		}

		// Create tx_log entry with OpTypeClaim (positive balance - returning points)
		txLog := &vaultModels.TxLog{
			UserID:     pledge.UserID,
			ResourceID: &pledge.ResourceID,
			Balance:    pledge.Amount,
			OpType:     vaultModels.OpTypeClaim,
		}
		_, err = tx.Model(txLog).Context(ctx).Insert()
		if err != nil {
			return errors.Wrap(err, "failed to create claim log")
		}

		// Update resource: decrease funded_vp by pledge amount
		newFundedVP := resource.FundedVP - pledge.Amount
		if newFundedVP < 0 {
			newFundedVP = 0
		}
		err = vaultModels.UpdateResourceFundedVP(ctx, tx, resource.ResourceID, newFundedVP)
		if err != nil {
			return errors.Wrap(err, "failed to update resource funded VP")
		}

		// If funded_vp < required_vp, mark resource as expired and unfunded
		if newFundedVP < resource.RequiredVP {
			// Mark as expired
			err = vaultModels.MarkResourceExpiredAndUnfunded(ctx, tx, resource.ResourceID)
			if err != nil {
				return errors.Wrap(err, "failed to mark resource as expired and unfunded")
			}
		}

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

// defundPledge removes funding from a pledge and updates the resource accordingly
func (s *Vault) defundPledge(ctx context.Context, tx *pg.Tx, pledge *vaultModels.Pledge, resource *vaultModels.Resource) error {
	// 1. Set funded = false for the pledge
	err := vaultModels.UpdatePledgeFunded(ctx, tx, pledge.PledgeID, false)
	if err != nil {
		return errors.Wrap(err, "failed to update pledge funded status")
	}

	// 2. Decrease resource funded_vp by pledge amount
	newFundedVP := resource.FundedVP - pledge.Amount
	err = vaultModels.UpdateResourceFundedVP(ctx, tx, resource.ResourceID, newFundedVP)
	if err != nil {
		return errors.Wrap(err, "failed to adjust resource funded VP")
	}

	// Update local resource state
	resource.FundedVP -= pledge.Amount

	// 3. If funded_vp < required_vp and expired = false, mark as expired
	if resource.FundedVP < resource.RequiredVP && !resource.Expired {
		err = vaultModels.MarkResourceExpiredAndUnfunded(ctx, tx, resource.ResourceID)
		if err != nil {
			return errors.Wrap(err, "failed to mark resource as expired and unfunded")
		}
		resource.Expired = true
		resource.Funded = false
	}

	return nil
}

// putResourceToVaultAPI checks and syncs resource status with Vault API
func (s *Vault) putResourceToVaultAPI(ctx context.Context, tx *pg.Tx, resource *vaultModels.Resource) (bool, error) {
	// Skip if Vault API is not configured
	if s.vaultApi == nil {
		return false, nil
	}

	// Get resource status from Vault API
	vaultResource, err := s.vaultApi.GetResource(ctx, resource.ResourceID)
	if err != nil {
		return false, errors.Wrap(err, "failed to get resource from vault api")
	}

	// If resource exists in Vault and has StatusCompleted, mark as vaulted
	if vaultResource != nil && vaultResource.Status == StatusCompleted {
		return true, nil
	}

	// If resource doesn't exist in Vault, add it via PUT
	if vaultResource == nil {
		_, err = s.vaultApi.PutResource(ctx, resource.ResourceID)
		if err != nil {
			return false, errors.Wrap(err, "failed to put resource to vault api")
		}
	}

	return false, nil
}

// fundPledge adds funding to a pledge and updates the resource accordingly
func (s *Vault) fundPledge(ctx context.Context, tx *pg.Tx, pledge *vaultModels.Pledge, resource *vaultModels.Resource) error {
	// 1. Set funded = true for the pledge
	err := vaultModels.UpdatePledgeFunded(ctx, tx, pledge.PledgeID, true)
	if err != nil {
		return errors.Wrap(err, "failed to update pledge funded status")
	}

	// 2. Increase resource funded_vp by pledge amount
	newFundedVP := resource.FundedVP + pledge.Amount
	err = vaultModels.UpdateResourceFundedVP(ctx, tx, resource.ResourceID, newFundedVP)
	if err != nil {
		return errors.Wrap(err, "failed to adjust resource funded VP")
	}

	// Update local resource state
	resource.FundedVP += pledge.Amount

	// 3. If funded_vp >= required_vp and expired = true, mark as unexpired and funded
	if resource.FundedVP >= resource.RequiredVP && resource.Expired {
		err = vaultModels.MarkResourceUnexpiredAndFunded(ctx, tx, resource.ResourceID)
		if err != nil {
			return errors.Wrap(err, "failed to mark resource as unexpired")
		}
		resource.Expired = false
		resource.Funded = true
	}

	return nil
}

// recalculatePledgeFunding recalculates which pledges should be funded based on user's total points
func (s *Vault) recalculatePledgeFunding(ctx context.Context, tx *pg.Tx, user *auth.User, total *float64) error {
	// Get all user pledges ordered by creation time (ascending)
	pledges, err := vaultModels.GetUserPledgesOrderedByCreation(ctx, tx, user.ID)
	if err != nil {
		return errors.Wrap(err, "failed to get user pledges")
	}

	// Track accumulated amount
	accumulatedAmount := float64(0)

	// Process each pledge in order
	for i := range pledges {
		pledge := &pledges[i]

		// Fetch resource
		resource := &vaultModels.Resource{}
		err := tx.Model(resource).
			Where("resource_id = ?", pledge.ResourceID).
			For("UPDATE").
			Select()
		if err != nil {
			if errors.Is(err, pg.ErrNoRows) {
				// Resource doesn't exist, skip this pledge
				continue
			}
			return errors.Wrap(err, "failed to lock resource")
		}

		// Check if this pledge should be funded
		if total == nil || accumulatedAmount+pledge.Amount <= *total {
			// This pledge should be funded
			if !pledge.Funded {
				// Fund the pledge
				err = s.fundPledge(ctx, tx, pledge, resource)
				if err != nil {
					return errors.Wrap(err, "failed to fund pledge")
				}
			}
			// Add to accumulated amount (whether it was already funded or just funded)
			accumulatedAmount += pledge.Amount
		} else {
			// This pledge should NOT be funded (accumulated would exceed total)
			if pledge.Funded {
				// Defund the pledge
				err = s.defundPledge(ctx, tx, pledge, resource)
				if err != nil {
					return errors.Wrap(err, "failed to defund pledge")
				}
			}
			// Don't add to accumulated amount
		}
	}

	return nil
}

// RemoveResource removes a resource from the database and vault API
func (s *Vault) RemoveResource(ctx context.Context, resourceID string) error {
	db := s.pg.Get()
	if db == nil {
		return errors.New("database connection is not available")
	}

	// Delete resource from vault API
	_, err := s.vaultApi.DeleteResource(ctx, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to delete resource from vault api")
	}

	// Delete resource from database
	err = vaultModels.DeleteResource(ctx, db, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to delete resource from database")
	}

	return nil
}

// pointsEqual compares two *float64 values, treating nil as distinct from any number
func pointsEqual(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
