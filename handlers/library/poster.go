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
	log "github.com/sirupsen/logrus"
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
		log.WithError(err).Error("failed to bind poster args")
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	ctx := c.Request.Context()

	db := s.pg.Get()
	if db == nil {
		log.Error("no db")
		c.Status(http.StatusInternalServerError)
		return
	}

	b, err := s.getResizedJPEGPosterWithCache(ctx, db, s.s3Cl, pa)

	if err != nil {
		log.WithError(err).Error("failed to get resized image")
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
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

func (s *Handler) generateETag(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf(`"%x"`, sum[:])
}

func (s *Handler) getResizedPoster(ctx context.Context, db *pg.DB, args *PosterArgs) (*image.NRGBA, error) {
	md, err := s.getPosterMetadata(ctx, db, args.t, args.imdbID)
	if err != nil {
		return nil, err
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
	if t == models.ContentTypeSeries {
		var smd *models.SeriesMetadata
		smd, err = models.GetSeriesMetadataByVideoID(ctx, db, videoID)
		if err != nil || smd == nil {
			return
		}
		return smd.VideoMetadata, nil
	} else if t == models.ContentTypeMovie {
		var mmd *models.MovieMetadata
		mmd, err = models.GetMovieMetadataByVideoID(ctx, db, videoID)
		if err != nil || mmd == nil {
			return
		}
		return mmd.VideoMetadata, nil
	}
	return nil, errors.New("invalid video type")
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
