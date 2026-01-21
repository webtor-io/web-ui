package vault

import (
	"context"
	"net/http"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
)

const (
	vaultServiceHostFlag = "vault-service-host"
	vaultServicePortFlag = "vault-service-port"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   vaultServiceHostFlag,
			Usage:  "vault service host",
			Value:  "",
			EnvVar: "VAULT_SERVICE_HOST",
		},
		cli.IntFlag{
			Name:   vaultServicePortFlag,
			Usage:  "vault service port",
			Value:  80,
			EnvVar: "VAULT_SERVICE_PORT",
		},
	)
}

type Vault struct {
	host   string
	port   int
	claims *claims.Claims
	client *http.Client
	pg     *cs.PG
}

func New(c *cli.Context, cl *claims.Claims, client *http.Client, pg *cs.PG) *Vault {
	host := c.String(vaultServiceHostFlag)
	port := c.Int(vaultServicePortFlag)

	// Return nil if host or port is not configured
	if host == "" {
		return nil
	}

	return &Vault{
		host:   host,
		port:   port,
		claims: cl,
		client: client,
		pg:     pg,
	}
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
		// Frozen: sum of pledges with frozen=true AND funded=true
		if pledge.Frozen && pledge.Funded {
			stats.Frozen += pledge.Amount
		}

		// Funded: sum of all pledges with funded=true
		if pledge.Funded {
			stats.Funded += pledge.Amount
		}

		// Claimable: funded but not frozen
		if pledge.Funded && !pledge.Frozen {
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
