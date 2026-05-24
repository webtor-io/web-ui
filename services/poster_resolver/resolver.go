// Package poster_resolver picks the best available source image for a
// torrent resource and exposes it through one in-process cached entry
// point. Two consumers use this:
//
//   - Library / continue-watching cards (resized JPEG per width)
//   - Resource share previews (1200x630 OG canvas)
//
// Both share resolution (IMDb poster > per-resource thumbnail >
// brand-default), S3 cache (keyed by source-content), and lazymap dedup
// (keyed by request). Cross-resource sharing happens at the S3 layer
// — two resources matched to the same IMDb work serve the same cached
// object even though their request URLs differ.
package poster_resolver

import (
	"bytes"
	"context"
	"image"
	"io"
	"net/http"

	"github.com/disintegration/imaging"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/thumbnail"
)

// SourceKind labels where the resolved image came from. Used for
// observability (logs/metrics) and to drive cache-key prefixing.
type SourceKind string

const (
	SourceIMDbMovie  SourceKind = "imdb_movie"
	SourceIMDbSeries SourceKind = "imdb_series"
	SourceThumbnail  SourceKind = "thumbnail"
)

// Source is the resolved instruction for one resource. CacheID is the
// stable content-key used to scope the S3 cache so two resources that
// resolve to the same artwork share the cached object. Fetch decodes
// the original bytes to an in-memory image; callers chain a render
// step (resize or canvas) before serving.
type Source struct {
	Kind    SourceKind
	CacheID string
	Fetch   func(ctx context.Context) (image.Image, error)
}

// Resolve picks the best available source for a resource_id. Order:
//
//  1. movie metadata → IMDb poster URL
//  2. series metadata → IMDb poster URL
//  3. per-resource thumbnail (image_file / ffmpeg_frame / audio_art)
//
// Returns (nil, nil) when nothing usable exists — caller decides what
// to do (404 for resize, brand-default for OG canvas).
//
// The *WithMetadata loaders preload the poster URL; the plain variants
// return rows with nil metadata. See handlers/resource/get.go for the
// same pattern.
func Resolve(ctx context.Context, db *pg.DB, thumb *thumbnail.Service, cl *http.Client, resourceID string) (*Source, error) {
	if movie, err := models.GetMovieWithMetadataByResourceID(ctx, db, resourceID); err == nil && movie != nil {
		md := movie.GetMetadata()
		if md != nil && md.PosterURL != "" && md.VideoID != "" {
			return posterSource(SourceIMDbMovie, md.VideoID, md.PosterURL, cl), nil
		}
	}
	if series, err := models.GetSeriesWithMetadataByResourceID(ctx, db, resourceID); err == nil && series != nil {
		md := series.GetMetadata()
		if md != nil && md.PosterURL != "" && md.VideoID != "" {
			return posterSource(SourceIMDbSeries, md.VideoID, md.PosterURL, cl), nil
		}
	}
	if thumb != nil && thumb.Enabled() {
		t, err := thumb.Get(ctx, resourceID)
		if err != nil {
			return nil, err
		}
		if t != nil {
			return thumbnailSource(t, thumb), nil
		}
	}
	return nil, nil
}

func posterSource(kind SourceKind, videoID, posterURL string, cl *http.Client) *Source {
	return &Source{
		Kind:    kind,
		CacheID: string(kind) + "-" + videoID,
		Fetch: func(ctx context.Context) (image.Image, error) {
			req, err := http.NewRequestWithContext(ctx, "GET", posterURL, nil)
			if err != nil {
				return nil, err
			}
			resp, err := cl.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			img, err := imaging.Decode(resp.Body)
			if err != nil {
				return nil, errors.Wrap(err, "failed to decode poster")
			}
			return img, nil
		},
	}
}

func thumbnailSource(t *models.Thumbnail, svc *thumbnail.Service) *Source {
	return &Source{
		Kind:    SourceThumbnail,
		CacheID: "thumb-" + t.Hash,
		Fetch: func(ctx context.Context) (image.Image, error) {
			body, _, err := svc.GetBlob(ctx, t)
			if err != nil {
				return nil, err
			}
			if body == nil {
				return nil, errors.New("thumbnail blob missing in storage")
			}
			defer body.Close()
			raw, err := io.ReadAll(io.LimitReader(body, thumbnail.MaxImageBytes+1))
			if err != nil {
				return nil, err
			}
			img, _, err := image.Decode(bytes.NewReader(raw))
			if err != nil {
				return nil, errors.Wrap(err, "failed to decode thumbnail")
			}
			return img, nil
		},
	}
}
