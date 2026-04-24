package user_subtitle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash/fnv"
	"io"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/common"
)

const (
	bucketFlag = "aws-user-subtitle-bucket"

	// MaxUploadSize caps a single subtitle file. Real-world subtitles are
	// ~100 KB; 5 MB leaves plenty of headroom for verbose ASS tracks while
	// keeping abuse tiny.
	MaxUploadSize int64 = 5 * 1024 * 1024

	// MaxPerFile is the per-(user, resource_id, path) upload limit.
	MaxPerFile = 10
)

var allowedFormats = map[string]struct{}{
	"srt": {},
	"vtt": {},
	"ass": {},
}

// Sentinel errors surfaced to handlers for translation into user-friendly
// i18n keys.
var (
	ErrNotConfigured     = errors.New("user subtitle storage is not configured")
	ErrTooLarge          = errors.New("subtitle file is too large")
	ErrUnsupportedFormat = errors.New("unsupported subtitle format")
	ErrLimitReached      = errors.New("per-file subtitle limit reached")
	ErrEmptyFile         = errors.New("subtitle file is empty")
	ErrNotFound          = errors.New("subtitle not found")
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f, cli.StringFlag{
		Name:   bucketFlag,
		Usage:  "S3 bucket that stores user-uploaded subtitle blobs",
		EnvVar: "AWS_USER_SUBTITLE_BUCKET",
	})
}

type Service struct {
	s3Cl   *cs.S3Client
	pg     *cs.PG
	bucket string
	domain string
}

// New returns nil when the deployment is missing either the bucket flag, an
// S3 client, or a DB pool. Handlers and templates call Enabled() to decide
// whether to surface the feature at all.
func New(c *cli.Context, s3Cl *cs.S3Client, pg *cs.PG) *Service {
	bucket := c.String(bucketFlag)
	if bucket == "" || s3Cl == nil || pg == nil {
		return nil
	}
	return &Service{
		s3Cl:   s3Cl,
		pg:     pg,
		bucket: bucket,
		domain: strings.TrimRight(c.String(common.DomainFlag), "/"),
	}
}

// PublicURL is the externally reachable URL of the raw blob endpoint.
// torrent-http-proxy's external-proxy must be able to fetch this — that's
// why we prefix with the full public domain and not a relative path.
func (s *Service) PublicURL(hash, name string) string {
	if s == nil {
		return ""
	}
	return s.domain + "/user-subtitle/file/" + hash + "/" + name
}

// Enabled reports whether the feature has the infrastructure to run.
func (s *Service) Enabled() bool {
	return s != nil
}

// Upload stores the blob in S3 and inserts a binding row. The S3 key is the
// SHA-256 hash so uploading the same file twice (by the same user or a
// different one) is a no-op at the storage layer. A per-hash advisory lock
// serialises the transaction against Delete so a concurrent DeleteObject
// cannot race the PutObject.
func (s *Service) Upload(ctx context.Context, userID uuid.UUID, resourceID, path, filename string, data []byte) (*models.UserSubtitle, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	if len(data) == 0 {
		return nil, ErrEmptyFile
	}
	if int64(len(data)) > MaxUploadSize {
		return nil, ErrTooLarge
	}

	format := detectFormat(filename, data)
	if _, ok := allowedFormats[format]; !ok {
		return nil, ErrUnsupportedFormat
	}

	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}

	count, err := models.CountUserSubtitlesForFile(ctx, db, userID, resourceID, path)
	if err != nil {
		return nil, err
	}
	if count >= MaxPerFile {
		return nil, ErrLimitReached
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	us := &models.UserSubtitle{
		UserSubtitleID: uuid.NewV4(),
		UserID:         userID,
		ResourceID:     resourceID,
		Path:           path,
		Hash:           hash,
		OriginalName:   filename,
		Format:         format,
		Size:           int64(len(data)),
	}

	err = db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		if err := advisoryLock(ctx, tx, hash); err != nil {
			return err
		}
		if err := s.putObject(ctx, hash, data); err != nil {
			return err
		}
		if _, err := tx.ModelContext(ctx, us).Insert(); err != nil {
			// Uploading the same file twice for the same
			// (user, resource, path) is idempotent by intent: the
			// existing binding already satisfies what the user asked
			// for. Swallow the unique-key violation and return the
			// existing row so callers treat it as success.
			if isUniqueViolation(err) {
				var existing models.UserSubtitle
				if lookupErr := tx.ModelContext(ctx, &existing).
					Where("user_id = ? AND resource_id = ? AND path = ? AND hash = ?", userID, resourceID, path, hash).
					Limit(1).
					Select(); lookupErr != nil {
					return errors.Wrap(lookupErr, "failed to load existing user subtitle")
				}
				*us = existing
				return nil
			}
			return errors.Wrap(err, "failed to insert user subtitle")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return us, nil
}

// isUniqueViolation returns true when err is a Postgres unique-constraint
// violation (SQLSTATE 23505). We detect it by matching the stable prefix
// "#23505" in go-pg's formatted error message to avoid a hard dependency
// on any particular go-pg error-type API.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "#23505")
}

// Delete removes a binding and drops the S3 object when no other bindings
// reference the same hash. The advisory lock taken on the hash prevents a
// parallel Upload of the same content from seeing a moment where the row
// is gone but the object is still present (or vice versa).
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	if s == nil {
		return ErrNotConfigured
	}
	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}

	us, err := models.GetUserSubtitle(ctx, db, userID, id)
	if err != nil {
		return err
	}
	if us == nil {
		return ErrNotFound
	}
	hash := us.Hash

	return db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		if err := advisoryLock(ctx, tx, hash); err != nil {
			return err
		}
		deletedHash, err := models.DeleteUserSubtitleTx(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if deletedHash == "" {
			return nil
		}
		count, err := models.CountUserSubtitlesByHash(ctx, tx, deletedHash)
		if err != nil {
			return err
		}
		if count == 0 {
			if err := s.deleteObject(ctx, deletedHash); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetFile streams the raw blob from S3. The endpoint that wraps this call
// is public on purpose: torrent-http-proxy reaches it through /ext/ when
// converting SRT → VTT on the fly, so it must not require a user session.
// Hash-addressed URLs are unguessable, which is the same security posture
// as OpenSubtitles URLs the player already consumes.
func (s *Service) GetFile(ctx context.Context, hash string) (io.ReadCloser, int64, error) {
	if s == nil {
		return nil, 0, ErrNotConfigured
	}
	cl := s.s3Cl.Get()
	out, err := cl.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(hash),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == s3.ErrCodeNoSuchKey {
			return nil, 0, ErrNotFound
		}
		return nil, 0, errors.Wrap(err, "failed to get user subtitle object")
	}
	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return out.Body, size, nil
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, resourceID, path string) ([]*models.UserSubtitle, error) {
	if s == nil {
		return nil, nil
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	return models.ListUserSubtitlesForFile(ctx, db, userID, resourceID, path)
}

// Get returns the binding for a user-owned id, or nil if it does not exist.
// Useful for the delete flow, which needs (resource_id, path) before the row
// disappears so the async re-render can target the same file context.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (*models.UserSubtitle, error) {
	if s == nil {
		return nil, ErrNotConfigured
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	return models.GetUserSubtitle(ctx, db, userID, id)
}

// ListHashesForResource returns the hashes of every subtitle a user has
// uploaded for a given resource. Used by the stream-video job cache key so
// uploads/deletes invalidate the cached render.
func (s *Service) ListHashesForResource(ctx context.Context, userID uuid.UUID, resourceID string) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	return models.ListUserSubtitleHashesForResource(ctx, db, userID, resourceID)
}

func (s *Service) putObject(ctx context.Context, hash string, data []byte) error {
	cl := s.s3Cl.Get()
	_, err := cl.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(hash),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return errors.Wrap(err, "failed to put user subtitle object")
	}
	return nil
}

func (s *Service) deleteObject(ctx context.Context, hash string) error {
	cl := s.s3Cl.Get()
	_, err := cl.DeleteObjectWithContext(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(hash),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == s3.ErrCodeNoSuchKey {
			return nil
		}
		return errors.Wrap(err, "failed to delete user subtitle object")
	}
	return nil
}

// advisoryLock uses a non-cryptographic FNV hash to fit the hex digest into
// a bigint — pg_advisory_xact_lock(bigint). Collisions are harmless: the
// lock only needs to serialise operations on the same content, and a
// collision just serialises two unrelated blobs for the duration of one
// transaction.
func advisoryLock(ctx context.Context, tx *pg.Tx, hash string) error {
	h := fnv.New64a()
	_, _ = h.Write([]byte(hash))
	key := int64(h.Sum64())
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(?)", key)
	if err != nil {
		return errors.Wrap(err, "failed to take advisory lock")
	}
	return nil
}

func detectFormat(filename string, data []byte) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	if _, ok := allowedFormats[ext]; ok {
		return ext
	}
	// Sniff WEBVTT signature: browsers can drop extensions on some uploads
	// and a .txt file with a proper VTT header is still a valid track.
	trimmed := bytes.TrimLeft(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}), "\r\n \t")
	if bytes.HasPrefix(trimmed, []byte("WEBVTT")) {
		return "vtt"
	}
	return ""
}

// ContentTypeFor returns the Content-Type the file endpoint should set so
// torrent-http-proxy / srt2vtt can decide whether to transform the payload.
func ContentTypeFor(format string) string {
	switch format {
	case "vtt":
		return "text/vtt; charset=utf-8"
	case "srt":
		return "application/x-subrip; charset=utf-8"
	case "ass":
		return "text/x-ass; charset=utf-8"
	}
	return "application/octet-stream"
}
