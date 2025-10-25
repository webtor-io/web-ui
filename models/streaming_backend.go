package models

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// StreamingBackendType represents the type of streaming backend
type StreamingBackendType string

const (
	StreamingBackendTypeWebtor     StreamingBackendType = "webtor"
	StreamingBackendTypeRealDebrid StreamingBackendType = "real_debrid"
	StreamingBackendTypeTorbox     StreamingBackendType = "torbox"
)

// StreamingBackendStatus represents the last status of a streaming backend
type StreamingBackendStatus string

const (
	StreamingBackendStatusOK                 StreamingBackendStatus = "ok"
	StreamingBackendStatusInvalidCredentials StreamingBackendStatus = "invalid_credentials"
	StreamingBackendStatusRateLimited        StreamingBackendStatus = "rate_limited"
	StreamingBackendStatusError              StreamingBackendStatus = "error"
)

// StreamingBackendConfig represents the JSONB config field
type StreamingBackendConfig map[string]interface{}

type StreamingBackend struct {
	tableName     struct{}                `pg:"streaming_backend"`
	ID            uuid.UUID               `pg:"streaming_backend_id,pk,type:uuid,default:uuid_generate_v4()"`
	UserID        uuid.UUID               `pg:"user_id,notnull"`
	Type          StreamingBackendType    `pg:"type,notnull"`
	AccessToken   string                  `pg:"access_token"`
	Config        StreamingBackendConfig  `pg:"config,type:jsonb,notnull,default:'{}'"`
	Priority      int16                   `pg:"priority,notnull"`
	Enabled       bool                    `pg:"enabled,notnull,default:true,use_zero"`
	LastStatus    *StreamingBackendStatus `pg:"last_status"`
	LastCheckedAt *time.Time              `pg:"last_checked_at"`
	CreatedAt     time.Time               `pg:"created_at,default:now()"`
	UpdatedAt     time.Time               `pg:"updated_at,default:now()"`

	User *User `pg:"rel:has-one,fk:user_id"`
}

// GetUserStreamingBackends returns all streaming backends for a user ordered by priority (highest first)
func GetUserStreamingBackends(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]*StreamingBackend, error) {
	var backends []*StreamingBackend
	err := db.Model(&backends).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("priority DESC").
		Select()
	if err != nil {
		return nil, err
	}
	return backends, nil
}

// GetStreamingBackendByID returns a streaming backend by ID
func GetStreamingBackendByID(ctx context.Context, db *pg.DB, id uuid.UUID) (*StreamingBackend, error) {
	backend := &StreamingBackend{}
	err := db.Model(backend).
		Context(ctx).
		Where("streaming_backend_id = ?", id).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return backend, nil
}

// CreateStreamingBackend creates a new streaming backend
func CreateStreamingBackend(ctx context.Context, db *pg.DB, backend *StreamingBackend) error {
	_, err := db.Model(backend).Context(ctx).Insert()
	return err
}

// UpdateStreamingBackend updates an existing streaming backend
func UpdateStreamingBackend(ctx context.Context, db *pg.DB, backend *StreamingBackend) error {
	_, err := db.Model(backend).
		Context(ctx).
		Where("streaming_backend_id = ?", backend.ID).
		Update()
	return err
}

// DeleteStreamingBackend deletes a streaming backend by ID
func DeleteStreamingBackend(ctx context.Context, db *pg.DB, id uuid.UUID) error {
	_, err := db.Model(&StreamingBackend{}).
		Context(ctx).
		Where("streaming_backend_id = ?", id).
		Delete()
	return err
}

// StreamingBackendExists checks if a streaming backend of given type exists for a user
func StreamingBackendExists(ctx context.Context, db *pg.DB, userID uuid.UUID, backendType StreamingBackendType) (bool, error) {
	count, err := db.Model(&StreamingBackend{}).
		Context(ctx).
		Where("user_id = ? AND type = ?", userID, backendType).
		Count()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
