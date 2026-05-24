package library

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
)

type PosterFormat string

const (
	PosterFormatJPEG PosterFormat = "jpg"
)

const (
	PosterJPEGQuality = 85
)

// errPosterNotFound is returned when no poster URL can be resolved for the
// requested video — distinct from real internal errors (db down, S3 cache
// failure, image decode failure). The HTTP layer maps it to 404 so absent
// posters don't pollute the 5xx error budget.
var errPosterNotFound = errors.New("poster not found")

type PosterArgs struct {
	t      models.ContentType
	imdbID string
	width  int
	format PosterFormat
}

func (s *Handler) bindPosterArgs(c *gin.Context) (*PosterArgs, error) {
	t := models.ContentType(c.Param("type"))
	if t != models.ContentTypeSeries && t != models.ContentTypeMovie {
		return nil, errors.Errorf("wrong video type %v", t)
	}
	file := c.Param("file")
	fileParts := strings.Split(file, ".")
	if len(fileParts) != 2 {
		return nil, errors.Errorf("wrong file format %v", file)
	}
	width, err := strconv.Atoi(fileParts[0])
	if err != nil {
		return nil, errors.Errorf("wrong width %v", width)
	}
	f := PosterFormat(fileParts[1])
	if f != PosterFormatJPEG {
		return nil, errors.Errorf("wrong format %v", f)
	}
	return &PosterArgs{
		t:      t,
		imdbID: c.Param("imdb_id"),
		width:  width,
		format: f,
	}, nil
}

func (s *Handler) poster(c *gin.Context) {

	pa, err := s.bindPosterArgs(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, errors.Wrap(err, "failed to bind poster args"))
		return
	}

	ctx := c.Request.Context()

	db := s.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("no db"))
		return
	}

	b, err := s.getResizedJPEGPosterWithCache(ctx, db, s.s3Cl, pa)

	if err != nil {
		if errors.Is(err, errPosterNotFound) {
			_ = c.AbortWithError(http.StatusNotFound, err)
			return
		}
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get resized image"))
		return
	}

	etag := s.generateETag(b.Bytes())

	// Strip Set-Cookie before flushing — see poster_resource.go for
	// rationale. Posters are public/idempotent; the session cookie
	// emitted by upstream middleware would otherwise force CF to
	// bypass the cache.
	c.Writer.Header().Del("Set-Cookie")

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

func (s *Handler) generateETag(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf(`"%x"`, sum[:])
}

func (s *Handler) getResizedPoster(ctx context.Context, db *pg.DB, args *PosterArgs) (*image.NRGBA, error) {
	md, err := s.getPosterMetadata(ctx, db, args.t, args.imdbID)
	if err != nil {
		return nil, err
	}
	if md == nil || md.PosterURL == "" {
		return nil, errors.Wrapf(errPosterNotFound, "%s %s", args.t, args.imdbID)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", md.PosterURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	srcImg, err := imaging.Decode(resp.Body)
	if err != nil {
		return nil, err
	}

	resized := imaging.Resize(srcImg, args.width, 0, imaging.Lanczos)

	return resized, nil
}

func (s *Handler) getResizedJPEGPosterWithCache(ctx context.Context, db *pg.DB, s3Cl *cs.S3Client, args *PosterArgs) (*bytes.Buffer, error) {
	if s3Cl == nil {
		return s.getResizedJPEGPoster(ctx, db, args)
	}
	cl := s3Cl.Get()
	b, err := s.getPosterFromCache(ctx, cl, args)
	if err != nil {
		return nil, err
	}
	if b != nil {
		return b, nil
	}
	b, err = s.getResizedJPEGPoster(ctx, db, args)
	if err != nil {
		return nil, err
	}
	err = s.putPosterToCache(ctx, cl, args, b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Handler) getResizedJPEGPoster(ctx context.Context, db *pg.DB, args *PosterArgs) (*bytes.Buffer, error) {
	r, err := s.getResizedPoster(ctx, db, args)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = jpeg.Encode(&buf, r, &jpeg.Options{Quality: PosterJPEGQuality})
	if err != nil {
		return nil, err
	}
	return &buf, nil
}

func (s *Handler) getPosterMetadata(ctx context.Context, db *pg.DB, t models.ContentType, videoID string) (md *models.VideoMetadata, err error) {
	// First, try the persisted enrichment record. If a torrent has been
	// enriched, the poster URL is already in series_metadata/movie_metadata
	// and we can return it directly. This keeps the served poster from the
	// same source that produced the video_id (an invariant — see migration
	// 51 and the Kinopoisk Unofficial mapper for the historical reason).
	switch t {
	case models.ContentTypeMovie:
		mm, mmErr := models.GetMovieMetadataByVideoID(ctx, db, videoID)
		if mmErr == nil && mm != nil && mm.VideoMetadata != nil && mm.PosterURL != "" {
			return mm.VideoMetadata, nil
		}
	case models.ContentTypeSeries:
		sm, smErr := models.GetSeriesMetadataByVideoID(ctx, db, videoID)
		if smErr == nil && sm != nil && sm.VideoMetadata != nil && sm.PosterURL != "" {
			return sm.VideoMetadata, nil
		}
	}

	// Fallback: AI/discover writes only into mapper-specific caches
	// (tmdb.info, kpu.info) without ever populating series_metadata /
	// movie_metadata. The mapper chain resolves those.
	if s.enricher != nil {
		md, err = s.enricher.LookupByVideoID(ctx, videoID, t)
		if err == nil && md != nil && md.PosterURL != "" {
			return md, nil
		}
	}
	return nil, nil
}

func (s *PosterArgs) Key() string {
	return fmt.Sprintf("%v/%v/%v.%v", s.t, s.imdbID, s.width, s.format)
}

func (s *Handler) getPosterFromCache(ctx context.Context, s3Cl *s3.S3, pa *PosterArgs) (*bytes.Buffer, error) {
	r, err := s3Cl.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.posterCacheS3Bucket),
		Key:    aws.String(pa.Key()),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == s3.ErrCodeNoSuchKey {
			return nil, nil
		}
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(r.Body)

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r.Body)
	if err != nil {
		return nil, err
	}
	return &buf, nil
}

func (s *Handler) makeAWSMD5(b []byte) *string {
	h := md5.Sum(b)
	m := base64.StdEncoding.EncodeToString(h[:])
	return aws.String(m)
}

func (s *Handler) putPosterToCache(ctx context.Context, s3Cl *s3.S3, pa *PosterArgs, b *bytes.Buffer) (err error) {
	data := b.Bytes()
	_, err = s3Cl.PutObjectWithContext(ctx,
		&s3.PutObjectInput{
			Bucket:     aws.String(s.posterCacheS3Bucket),
			Key:        aws.String(pa.Key()),
			Body:       bytes.NewReader(data),
			ContentMD5: s.makeAWSMD5(data),
		})
	return
}
