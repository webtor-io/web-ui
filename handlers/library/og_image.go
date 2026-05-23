package library

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/thumbnail"
)

// OG-image canvas dimensions. 1200x630 is the cross-platform safe
// target — Telegram, Twitter (card=summary_large_image), Facebook
// and iMessage all render this aspect ratio cleanly. Posters served
// raw at 480x720 get squished or whitespace-padded by each
// platform's own logic, often badly.
const (
	ogImageWidth       = 1200
	ogImageHeight      = 630
	ogImageInnerH      = 560 // ~35 px padding around vertical sources
	ogImageInnerWPad   = 80  // padding around landscape sources
	ogImageJPEGQuality = 85
)

// ogImageBgColor — single dark fill matching the resource-page header
// card. `image/draw` ships no gradient brush and a real gradient is
// disproportionate effort for static decorative background.
var ogImageBgColor = color.NRGBA{R: 0x14, G: 0x12, B: 0x1E, A: 0xFF}

// ogImage serves the OG-preview canvas for a resource. The handler
// picks the best available source in a fixed order — IMDb-matched
// poster first (highest quality), then the per-resource thumbnail
// (image_file or ffmpeg_frame) — and 404s when neither exists. A
// single endpoint means the resource template never needs to branch
// over "which kind of preview do we have?".
//
// The URL is keyed by resource_id, but the S3 cache key uses the
// underlying source hash so two resources that resolve to the same
// IMDb poster (or the same folder.jpg) share a cached composite.
func (s *Handler) ogImage(c *gin.Context) {
	resourceID := c.Param("resource_id")
	if resourceID == "" {
		_ = c.AbortWithError(http.StatusBadRequest, errors.New("empty resource_id"))
		return
	}
	// File pattern mirrors /poster/:imdb_id/:file (`<width>.<format>`),
	// but width is fixed at 1200x630 for OG canvases — we only validate
	// the extension and accept arbitrary leading stems so callers can
	// cache-bust by varying the basename if ever needed (e.g. `og.jpg`,
	// `v2.jpg`).
	file := c.Param("file")
	fileParts := strings.Split(file, ".")
	if len(fileParts) != 2 || PosterFormat(fileParts[1]) != PosterFormatJPEG {
		_ = c.AbortWithError(http.StatusBadRequest, errors.Errorf("bad og-image file %q", file))
		return
	}

	ctx := c.Request.Context()
	db := s.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("no db"))
		return
	}

	src, err := s.resolveOGSource(ctx, db, resourceID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to resolve og source"))
		return
	}
	if src == nil {
		// Brand-default fallback so the og:image meta never resolves
		// to a 404 — Telegram in particular caches negative responses
		// for hours, which would leave shared links image-less even
		// after we add real artwork later.
		src = s.defaultOGSource()
	}

	b, err := s.getOGFromCache(ctx, src.cacheKey)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to read og cache"))
		return
	}
	if b == nil {
		b, err = src.render(ctx, s)
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to render og image"))
			return
		}
		if putErr := s.putOGToCache(ctx, src.cacheKey, b); putErr != nil {
			// Cache write failure is non-fatal: serve the freshly composed
			// image to this caller and let the next request retry the put.
			_ = putErr
		}
	}

	etag := s.generateETag(b.Bytes())
	if match := c.Request.Header.Get("If-None-Match"); match != "" && match == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("Content-Type", "image/jpeg")
	c.Header("Content-Length", strconv.Itoa(b.Len()))
	c.Header("ETag", etag)
	c.Header("Cache-Control", "public, max-age=86400")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, b)
}

// ogSource is the resolved instruction for how to produce one
// resource's OG canvas. cacheKey scopes the S3 cache; render is the
// closure that fetches+composes when the cache misses.
type ogSource struct {
	cacheKey string
	render   func(ctx context.Context, h *Handler) (*bytes.Buffer, error)
}

// resolveOGSource picks the best available source for a resource.
// Order: IMDb poster (movie → series) > per-resource thumbnail. nil
// when nothing usable exists — caller returns 404. Pure lookup; no
// fetches or composing here.
func (s *Handler) resolveOGSource(ctx context.Context, db *pg.DB, resourceID string) (*ogSource, error) {
	if movies, err := models.GetMoviesByResourceID(ctx, db, resourceID); err == nil {
		for _, m := range movies {
			md := m.GetMetadata()
			if md != nil && md.PosterURL != "" && md.VideoID != "" {
				return s.posterSource(md.VideoID, "movie", md.PosterURL), nil
			}
		}
	}
	if series, err := models.GetSeriesByResourceID(ctx, db, resourceID); err == nil {
		for _, ss := range series {
			md := ss.GetMetadata()
			if md != nil && md.PosterURL != "" && md.VideoID != "" {
				return s.posterSource(md.VideoID, "series", md.PosterURL), nil
			}
		}
	}
	if s.thumbnail != nil && s.thumbnail.Enabled() {
		t, err := s.thumbnail.Get(ctx, resourceID)
		if err != nil {
			return nil, err
		}
		if t != nil {
			return s.thumbnailSource(t), nil
		}
	}
	return nil, nil
}

func (s *Handler) posterSource(videoID, ct, posterURL string) *ogSource {
	cacheKey := "og/poster/" + ct + "/" + videoID + ".jpg"
	return &ogSource{
		cacheKey: cacheKey,
		render: func(ctx context.Context, h *Handler) (*bytes.Buffer, error) {
			req, err := http.NewRequestWithContext(ctx, "GET", posterURL, nil)
			if err != nil {
				return nil, err
			}
			resp, err := h.cl.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			srcImg, err := imaging.Decode(resp.Body)
			if err != nil {
				return nil, errors.Wrap(err, "failed to decode poster")
			}
			return composeOGCanvas(srcImg)
		},
	}
}

// defaultOGSource composes the brand-default banner used when a
// resource has neither an IMDb poster nor a generated thumbnail.
// Source is the existing pub/webtor.jpg brand image (already 1280x720
// ≈ 16:9) wrapped into the same 1200x630 canvas as every other path,
// so platforms see one consistent aspect ratio across all share previews.
func (s *Handler) defaultOGSource() *ogSource {
	return &ogSource{
		cacheKey: "og/default.jpg",
		render: func(ctx context.Context, h *Handler) (*bytes.Buffer, error) {
			f, err := os.Open(defaultOGSourcePath)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to open %s", defaultOGSourcePath)
			}
			defer f.Close()
			srcImg, err := imaging.Decode(f)
			if err != nil {
				return nil, errors.Wrap(err, "failed to decode default brand image")
			}
			return composeOGCanvas(srcImg)
		},
	}
}

// defaultOGSourcePath — the brand image already shipped under pub/.
// Resolved relative to the working directory at request time; the web-ui
// container always runs with pub/ in place (see Dockerfile).
var defaultOGSourcePath = "./pub/webtor.jpg"

func (s *Handler) thumbnailSource(t *models.Thumbnail) *ogSource {
	cacheKey := "og/thumb/" + t.Hash + ".jpg"
	return &ogSource{
		cacheKey: cacheKey,
		render: func(ctx context.Context, h *Handler) (*bytes.Buffer, error) {
			body, _, err := h.thumbnail.GetBlob(ctx, t)
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
			srcImg, _, err := image.Decode(bytes.NewReader(raw))
			if err != nil {
				return nil, errors.Wrap(err, "failed to decode thumbnail")
			}
			return composeOGCanvas(srcImg)
		},
	}
}

// composeOGCanvas wraps a source image in the 1200x630 dark canvas.
// Vertical (poster-shaped) sources get fit-by-height; landscape
// sources get fit-by-width with horizontal padding so a wide
// ffmpeg-frame doesn't push past the canvas edges.
func composeOGCanvas(srcImg image.Image) (*bytes.Buffer, error) {
	srcW := srcImg.Bounds().Dx()
	srcH := srcImg.Bounds().Dy()
	var fit image.Image
	if srcH >= srcW {
		fit = imaging.Resize(srcImg, 0, ogImageInnerH, imaging.Lanczos)
	} else {
		fit = imaging.Resize(srcImg, ogImageWidth-ogImageInnerWPad*2, 0, imaging.Lanczos)
	}
	pw := fit.Bounds().Dx()
	ph := fit.Bounds().Dy()

	canvas := imaging.New(ogImageWidth, ogImageHeight, ogImageBgColor)
	x := (ogImageWidth - pw) / 2
	y := (ogImageHeight - ph) / 2
	canvas = imaging.Overlay(canvas, fit, image.Pt(x, y), 1.0)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: ogImageJPEGQuality}); err != nil {
		return nil, err
	}
	return &buf, nil
}

func (s *Handler) getOGFromCache(ctx context.Context, key string) (*bytes.Buffer, error) {
	if s.s3Cl == nil {
		return nil, nil
	}
	r, err := s.s3Cl.Get().GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.posterCacheS3Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == s3.ErrCodeNoSuchKey {
			return nil, nil
		}
		return nil, err
	}
	defer func(Body io.ReadCloser) { _ = Body.Close() }(r.Body)
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r.Body); err != nil {
		return nil, err
	}
	return &buf, nil
}

func (s *Handler) putOGToCache(ctx context.Context, key string, b *bytes.Buffer) error {
	if s.s3Cl == nil {
		return nil
	}
	data := b.Bytes()
	_, err := s.s3Cl.Get().PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket:     aws.String(s.posterCacheS3Bucket),
		Key:        aws.String(key),
		Body:       bytes.NewReader(data),
		ContentMD5: s.makeAWSMD5(data),
	})
	return err
}
