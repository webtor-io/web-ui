// Package thumbnail generates a per-resource preview image during
// enrichment for torrents that have no IMDb-matched poster. The image
// is sourced in two tiers:
//
//  1. image_file — an existing folder/poster/cover .jpg inside the
//     torrent itself (scene releases and radarr/sonarr exports ship
//     these almost universally). Cheap: HTTP-GET + upload.
//  2. ffmpeg_frame — a single frame decoded from the longest video
//     item at a 5-minute offset. Fallback when no image file exists.
//
// Both sources end up in the same `webtor-web-ui-thumbnail` S3 bucket
// keyed by SHA-1 of the binary (dedup across resources that ship the
// same poster), with a row in public.thumbnail binding the hash to
// the (resource_id, path, offset_sec) it was generated from.
package thumbnail

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"image"
	_ "image/gif"  // register decoder
	_ "image/jpeg" // register decoder
	_ "image/png"  // register decoder
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	_ "golang.org/x/image/webp" // register webp decoder
	cs "github.com/webtor-io/common-services"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
)

const (
	bucketFlag = "aws-thumbnail-bucket"

	// MaxImageBytes caps an image-file pull so a torrent that mislabels
	// a 50 MB raw scan as poster.jpg doesn't OOM the web-ui pod. Real
	// posters are <2 MB; 10 MB leaves cushion for high-quality fanart.
	MaxImageBytes int64 = 10 * 1024 * 1024

	// ffmpegDefaultOffsetSec is the fallback when duration is unknown.
	// Five minutes skips most opening credits and studio idents in a
	// feature-length film without needing per-torrent introspection.
	ffmpegDefaultOffsetSec = 300

	// ffmpegMaxOffsetSec caps the duration-derived offset so a 3-hour
	// epic doesn't land us in the climax. 10 min is past every realistic
	// opening sequence but well clear of act-two pivots.
	ffmpegMaxOffsetSec = 600

	// FFmpegTimeout bounds the worst-case ffmpeg invocation. Warm
	// torrent + libavformat HTTP demuxer typically returns in 1-3 s;
	// 15 s fits inside the 20 s overall budget set by streamContent
	// while leaving room for image-file attempts that fail before this.
	FFmpegTimeout = 15 * time.Second

	// downloadHTTPTimeout bounds the image-file pull. THP export URL is
	// on-cluster and posters are small; 5 s is enough for a real fetch
	// and short enough to leave the bulk of the budget for ffmpeg when
	// image-file isn't viable.
	downloadHTTPTimeout = 5 * time.Second
)

// preferredImageNames is a stem→priority map; lower numbers win. The
// list mirrors the conventions of radarr/sonarr/Jellyfin and most
// scene release naming guides — they ship a single artwork file with
// one of these stems alongside the video.
var preferredImageNames = map[string]int{
	"poster":    1,
	"folder":    2,
	"cover":     3,
	"movie":     4,
	"show":      5,
	"series":    5,
	"default":   6,
	"fanart":    7,
	"art":       8,
	"banner":    9,
	"thumb":     10,
	"thumbnail": 10,
}

var supportedImageExts = map[string]struct{}{
	"jpg":  {},
	"jpeg": {},
	"png":  {},
	"webp": {},
	"gif":  {},
}

// Sentinel — distinguishes "no source available" from real failures.
// Callers (enrich) log+continue on this and bubble up real errors.
var ErrNoSource = errors.New("thumbnail: no usable source in torrent")

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f, cli.StringFlag{
		Name:   bucketFlag,
		Usage:  "S3 bucket that stores generated resource thumbnails",
		EnvVar: "AWS_THUMBNAIL_BUCKET",
	})
}

type Service struct {
	s3Cl   *cs.S3Client
	pg     *cs.PG
	api    *api.Api
	cl     *http.Client
	bucket string
}

// New returns nil when the deployment is missing any required infra so
// callers can keep their hot path branch-free — every method on a nil
// receiver is a no-op (Enabled() returns false).
func New(c *cli.Context, s3Cl *cs.S3Client, pg *cs.PG, a *api.Api, hcl *http.Client) *Service {
	bucket := c.String(bucketFlag)
	if bucket == "" || s3Cl == nil || pg == nil || a == nil {
		return nil
	}
	if hcl == nil {
		hcl = &http.Client{Timeout: downloadHTTPTimeout}
	}
	return &Service{s3Cl: s3Cl, pg: pg, api: a, cl: hcl, bucket: bucket}
}

func (s *Service) Enabled() bool { return s != nil }

// Generate produces (or returns the existing) thumbnail for a resource.
// Idempotent: a row already in `public.thumbnail` short-circuits the
// expensive item-listing + S3 round-trip. Callers don't need to dedup
// upstream.
//
// durationSec is the probed video duration (0 if unknown). When non-zero
// the ffmpeg fallback aims for ~25 % in, capped at 10 min — past every
// realistic opening sequence but well clear of act-two pivots. Image-file
// extraction ignores it.
//
// Returns ErrNoSource when the torrent has neither a usable image file
// nor a video item — caller logs this at info and proceeds with the
// favicon fallback. Other errors are propagated so they show up in the
// daily error budget.
func (s *Service) Generate(ctx context.Context, claims *api.Claims, resourceID string, durationSec int) (*models.Thumbnail, error) {
	if s == nil {
		return nil, nil
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	if existing, err := models.GetThumbnailByResourceID(ctx, db, resourceID); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	items, err := s.listAllItems(ctx, claims, resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list resource items")
	}

	if img := pickBestImage(items); img != nil {
		t, gErr := s.generateFromImageFile(ctx, db, claims, resourceID, img)
		if gErr == nil {
			return t, nil
		}
		log.WithError(gErr).WithField("resource_id", resourceID).
			WithField("path", img.PathStr).Warn("image-file thumbnail failed, falling back to ffmpeg")
	}

	if vid := pickFirstVideo(items); vid != nil {
		offset := pickFFmpegOffset(durationSec)
		t, gErr := s.generateFromFFmpegFrame(ctx, db, claims, resourceID, vid, offset)
		if gErr == nil {
			return t, nil
		}
		return nil, errors.Wrap(gErr, "ffmpeg-frame thumbnail failed")
	}

	// Audio-only torrent: try to pull the embedded album art (id3
	// APIC frame for mp3, METADATA_BLOCK_PICTURE for flac, cover atom
	// for m4a/aac). Most tagged music carries one, and it's the right
	// thing to show in a share preview.
	if aud := pickFirstAudio(items); aud != nil {
		t, gErr := s.generateFromAudioEmbeddedArt(ctx, db, claims, resourceID, aud)
		if gErr == nil {
			return t, nil
		}
		// Missing-art failures aren't surprising — many encodes ship
		// audio with no APIC. Log + degrade rather than bubbling.
		log.WithError(gErr).WithField("resource_id", resourceID).
			WithField("path", aud.PathStr).Info("audio embedded-art extraction failed")
	}

	return nil, ErrNoSource
}

// pickFFmpegOffset returns the seek-in-seconds for `ffmpeg -ss`. With a
// known duration: a quarter of the way through, capped at 10 min. With
// no duration: a 5-minute default. Floor of 30 s so very short clips
// still skip leader frames / black intros.
func pickFFmpegOffset(durationSec int) int {
	if durationSec <= 0 {
		return ffmpegDefaultOffsetSec
	}
	off := durationSec / 4
	if off > ffmpegMaxOffsetSec {
		off = ffmpegMaxOffsetSec
	}
	if off < 30 {
		off = 30
	}
	if off > durationSec-1 && durationSec > 1 {
		off = durationSec - 1
	}
	return off
}

// Get returns the thumbnail row for a resource (or nil if none) — used
// by the OG-image handler to discover whether a thumbnail exists.
func (s *Service) Get(ctx context.Context, resourceID string) (*models.Thumbnail, error) {
	if s == nil {
		return nil, nil
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	return models.GetThumbnailByResourceID(ctx, db, resourceID)
}

// GetBlob streams the raw binary out of S3 keyed by the thumbnail's hash.
// Returns (nil, 0, nil) when the row's S3 object has disappeared so the
// caller can fall back gracefully instead of returning a 500.
func (s *Service) GetBlob(ctx context.Context, t *models.Thumbnail) (io.ReadCloser, int64, error) {
	if s == nil {
		return nil, 0, errors.New("thumbnail service not configured")
	}
	cl := s.s3Cl.Get()
	out, err := cl.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(t.Hash, t.Format)),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == s3.ErrCodeNoSuchKey {
			return nil, 0, nil
		}
		return nil, 0, errors.Wrap(err, "failed to get thumbnail object")
	}
	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return out.Body, size, nil
}

func (s *Service) objectKey(hash, format string) string {
	if format == "" {
		return hash
	}
	return hash + "." + format
}

// listAllItems paginates through the resource's flat file listing. Cached
// upstream — the page-fetch overhead is minimal but rest-api caches the
// full response per (resource, claims) too.
func (s *Service) listAllItems(ctx context.Context, claims *api.Claims, resourceID string) ([]ra.ListItem, error) {
	limit := uint(100)
	offset := uint(0)
	var items []ra.ListItem
	for {
		resp, err := s.api.ListResourceContentCached(ctx, claims, resourceID, &api.ListResourceContentArgs{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		items = append(items, resp.Items...)
		if (resp.Count - int(offset)) == len(resp.Items) {
			break
		}
		offset += limit
	}
	return items, nil
}

// pickBestImage scans items for the highest-priority artwork file.
// Lower preferredImageNames priority wins; ties broken by larger size
// (more detail). Items outside supportedImageExts are skipped — webp
// and gif are kept because Telegram/iMessage handle both fine.
func pickBestImage(items []ra.ListItem) *ra.ListItem {
	type cand struct {
		item *ra.ListItem
		prio int
		size int64
	}
	var cands []cand
	for i := range items {
		it := &items[i]
		if it.MediaFormat != ra.Image {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(it.Name), "."))
		if _, ok := supportedImageExts[ext]; !ok {
			continue
		}
		stem := strings.ToLower(strings.TrimSuffix(it.Name, filepath.Ext(it.Name)))
		prio, hit := preferredImageNames[stem]
		if !hit {
			// Fallback bucket for unrecognised stems — still better than
			// no preview at all. Priority 99 places them strictly below
			// every named convention.
			prio = 99
		}
		cands = append(cands, cand{item: it, prio: prio, size: it.Size})
	}
	if len(cands) == 0 {
		return nil
	}
	sort.SliceStable(cands, func(a, b int) bool {
		if cands[a].prio != cands[b].prio {
			return cands[a].prio < cands[b].prio
		}
		return cands[a].size > cands[b].size
	})
	return cands[0].item
}

// pickFirstVideo returns the largest video item — file size is a good
// proxy for "the main feature" in mixed packs (extras, samples and
// trailers come in much smaller than the actual movie).
func pickFirstVideo(items []ra.ListItem) *ra.ListItem {
	var best *ra.ListItem
	for i := range items {
		it := &items[i]
		if it.MediaFormat != ra.Video {
			continue
		}
		if best == nil || it.Size > best.Size {
			best = it
		}
	}
	return best
}

// pickFirstAudio returns the largest audio item. Used only when the
// torrent has no video at all — embedded album art on the longest
// track is the best guess for the release-level cover image.
func pickFirstAudio(items []ra.ListItem) *ra.ListItem {
	var best *ra.ListItem
	for i := range items {
		it := &items[i]
		if it.MediaFormat != ra.Audio {
			continue
		}
		if best == nil || it.Size > best.Size {
			best = it
		}
	}
	return best
}

func (s *Service) generateFromImageFile(ctx context.Context, db *pg.DB, claims *api.Claims, resourceID string, item *ra.ListItem) (*models.Thumbnail, error) {
	dlURL, err := s.downloadURLFor(ctx, claims, resourceID, item.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve image download URL")
	}
	data, err := s.fetchCapped(ctx, dlURL, MaxImageBytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch image bytes")
	}
	format := strings.ToLower(strings.TrimPrefix(filepath.Ext(item.Name), "."))
	if format == "jpeg" {
		format = "jpg"
	}
	if _, ok := supportedImageExts[format]; !ok {
		return nil, errors.Errorf("unsupported image extension %q", format)
	}
	w, h := decodeDimensions(data)
	return s.store(ctx, db, resourceID, item.PathStr, 0,
		models.ThumbnailSourceImageFile, data, format, w, h)
}

func (s *Service) generateFromFFmpegFrame(ctx context.Context, db *pg.DB, claims *api.Claims, resourceID string, item *ra.ListItem, offsetSec int) (*models.Thumbnail, error) {
	dlURL, err := s.downloadURLFor(ctx, claims, resourceID, item.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve video download URL")
	}

	cctx, cancel := context.WithTimeout(ctx, FFmpegTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx,
		"ffmpeg",
		"-loglevel", "error",
		"-ss", fmt.Sprintf("%d", offsetSec),
		"-i", dlURL,
		"-frames:v", "1",
		"-c:v", "mjpeg",
		"-q:v", "3",
		"-f", "image2pipe",
		"-",
	)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, errors.Wrapf(err, "ffmpeg failed: %s", strings.TrimSpace(stderr.String()))
	}
	if out.Len() == 0 {
		return nil, errors.New("ffmpeg produced empty output")
	}
	data := out.Bytes()
	w, h := decodeDimensions(data)
	return s.store(ctx, db, resourceID, item.PathStr, offsetSec,
		models.ThumbnailSourceFFmpegFrame, data, "jpg", w, h)
}

// generateFromAudioEmbeddedArt extracts the cover image embedded in
// the audio file's tags via `ffmpeg -map 0:v?`. Works for id3v2 APIC
// (mp3), METADATA_BLOCK_PICTURE (flac), and the cover atom in m4a/aac.
// No -ss seek — embedded pictures are not in the audio timeline.
func (s *Service) generateFromAudioEmbeddedArt(ctx context.Context, db *pg.DB, claims *api.Claims, resourceID string, item *ra.ListItem) (*models.Thumbnail, error) {
	dlURL, err := s.downloadURLFor(ctx, claims, resourceID, item.ID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve audio download URL")
	}

	cctx, cancel := context.WithTimeout(ctx, FFmpegTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx,
		"ffmpeg",
		"-loglevel", "error",
		"-i", dlURL,
		"-map", "0:v?", // optional video stream = embedded picture
		"-frames:v", "1",
		"-c:v", "mjpeg",
		"-q:v", "3",
		"-f", "image2pipe",
		"-",
	)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, errors.Wrapf(err, "ffmpeg failed: %s", strings.TrimSpace(stderr.String()))
	}
	if out.Len() == 0 {
		return nil, errors.New("no embedded album art")
	}
	data := out.Bytes()
	w, h := decodeDimensions(data)
	return s.store(ctx, db, resourceID, item.PathStr, 0,
		models.ThumbnailSourceAudioArt, data, "jpg", w, h)
}

// downloadURLFor pulls the `download` ExportItem for a torrent item.
// We use the raw download URL (not the stream URL) because libavformat
// + a static HTTP source is the most reliable feed for `ffmpeg -ss`.
func (s *Service) downloadURLFor(ctx context.Context, claims *api.Claims, resourceID, itemID string) (string, error) {
	resp, err := s.api.ExportResourceContent(ctx, claims, resourceID, itemID, "")
	if err != nil {
		return "", err
	}
	dl, ok := resp.ExportItems["download"]
	if !ok || dl.URL == "" {
		return "", errors.New("no download export item")
	}
	return dl.URL, nil
}

func (s *Service) fetchCapped(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("unexpected status %d", resp.StatusCode)
	}
	// io.LimitReader silently truncates; we want to fail loudly so a
	// torrent shipping a 50 MB "poster" never ends up half-saved. +1
	// lets us detect overrun on the read.
	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxBytes {
		return nil, errors.Errorf("image exceeds %d bytes", maxBytes)
	}
	return buf, nil
}

// store wraps the S3 PutObject + DB upsert in an advisory-locked
// transaction so two parallel enrich runs for the same resource don't
// race on either side.
func (s *Service) store(ctx context.Context, db *pg.DB, resourceID, path string, offsetSec int,
	kind models.ThumbnailSourceKind, data []byte, format string, width, height int) (*models.Thumbnail, error) {

	sum := sha1.Sum(data)
	hash := hex.EncodeToString(sum[:])

	t := &models.Thumbnail{
		ResourceID: resourceID,
		Path:       path,
		OffsetSec:  offsetSec,
		SourceKind: kind,
		Hash:       hash,
		Format:     format,
		Size:       int64(len(data)),
		Width:      width,
		Height:     height,
	}

	err := db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		if err := advisoryLock(ctx, tx, hash); err != nil {
			return err
		}
		if err := s.putObject(ctx, hash, format, data); err != nil {
			return err
		}
		// UpsertThumbnail handles (resource_id, path, offset_sec)
		// uniqueness; using tx here would need a separate model variant.
		// The advisory lock + idempotent S3 put already cover the race,
		// so a same-tx insert is not load-bearing.
		return models.UpsertThumbnail(ctx, db, t)
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) putObject(ctx context.Context, hash, format string, data []byte) error {
	cl := s.s3Cl.Get()
	_, err := cl.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(s.objectKey(hash, format)),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentTypeFor(format)),
	})
	if err != nil {
		return errors.Wrap(err, "failed to put thumbnail object")
	}
	return nil
}

// advisoryLock — FNV-fold the hex hash into a bigint and take a
// per-transaction lock. Mirrors the user_subtitle pattern; collisions
// are harmless (they only serialise unrelated content).
func advisoryLock(ctx context.Context, tx *pg.Tx, hash string) error {
	h := fnv.New64a()
	_, _ = h.Write([]byte(hash))
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(?)", int64(h.Sum64()))
	if err != nil {
		return errors.Wrap(err, "failed to take advisory lock")
	}
	return nil
}

func decodeDimensions(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// ContentTypeFor returns the HTTP Content-Type for serving a thumbnail
// stored in `format`. Exposed so the OG-image handler can pass the
// correct header when proxying raw thumbnails.
func ContentTypeFor(format string) string { return contentTypeFor(format) }

func contentTypeFor(format string) string {
	switch strings.ToLower(format) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	}
	return "application/octet-stream"
}
