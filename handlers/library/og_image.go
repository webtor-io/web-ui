package library

import (
	"bytes"
	"context"
	"image"
	"image/color"
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

// OG-image canvas dimensions and styling. 1200x630 is the cross-platform
// safe target — Telegram, Twitter (card=summary_large_image), Facebook
// and iMessage all render this aspect ratio cleanly. Vertical posters
// served raw (480x720) get squished or whitespace-padded by each
// platform's own logic, often badly.
const (
	ogImageWidth      = 1200
	ogImageHeight     = 630
	ogImageInnerH     = 560 // leave 35px top/bottom padding around poster
	ogImageJPEGQuality = 85
)

// Background gradient anchor colors — pulled from the resource page
// header card so the OG preview matches the on-site look. Solid fill
// (single colour) used instead of a real gradient because Go's stdlib
// `image/draw` does not provide a gradient brush and adding one is
// disproportionate effort for a static decorative background.
var ogImageBgColor = color.NRGBA{R: 0x14, G: 0x12, B: 0x1E, A: 0xFF}

type OGImageArgs struct {
	t      models.ContentType
	imdbID string
}

func (s *Handler) bindOGImageArgs(c *gin.Context) (*OGImageArgs, error) {
	t := models.ContentType(c.Param("type"))
	if t != models.ContentTypeSeries && t != models.ContentTypeMovie {
		return nil, errors.Errorf("wrong video type %v", t)
	}
	file := c.Param("file")
	fileParts := strings.Split(file, ".")
	if len(fileParts) != 2 {
		return nil, errors.Errorf("wrong file format %v", file)
	}
	if PosterFormat(fileParts[1]) != PosterFormatJPEG {
		return nil, errors.Errorf("wrong format %v", fileParts[1])
	}
	return &OGImageArgs{
		t:      t,
		imdbID: fileParts[0],
	}, nil
}

func (s *OGImageArgs) Key() string {
	return string(s.t) + "/" + s.imdbID + "/og.jpg"
}

func (s *Handler) ogImage(c *gin.Context) {
	args, err := s.bindOGImageArgs(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, errors.Wrap(err, "failed to bind og-image args"))
		return
	}

	ctx := c.Request.Context()

	db := s.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("no db"))
		return
	}

	b, err := s.getOGImageWithCache(ctx, db, s.s3Cl, args)
	if err != nil {
		if errors.Is(err, errPosterNotFound) {
			_ = c.AbortWithError(http.StatusNotFound, err)
			return
		}
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get og-image"))
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
	// OG crawlers honor Cache-Control. 1 day matches the poster endpoint
	// — posters/metadata effectively don't change after enrichment.
	c.Header("Cache-Control", "public, max-age=86400")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, b)
}

func (s *Handler) getOGImageWithCache(ctx context.Context, db *pg.DB, s3Cl *cs.S3Client, args *OGImageArgs) (*bytes.Buffer, error) {
	if s3Cl == nil {
		return s.generateOGImage(ctx, db, args)
	}
	cl := s3Cl.Get()
	b, err := s.getOGImageFromCache(ctx, cl, args)
	if err != nil {
		return nil, err
	}
	if b != nil {
		return b, nil
	}
	b, err = s.generateOGImage(ctx, db, args)
	if err != nil {
		return nil, err
	}
	if err := s.putOGImageToCache(ctx, cl, args, b); err != nil {
		return nil, err
	}
	return b, nil
}

// generateOGImage produces a 1200x630 JPEG with the poster (vertical) scaled
// to fit within the inner padding box and centered horizontally over a solid
// dark background. Inputs that fail to decode are treated as
// errPosterNotFound so the caller returns 404 rather than 500 — saves the
// OG-image endpoint from polluting the 5xx budget when metadata is stale.
func (s *Handler) generateOGImage(ctx context.Context, db *pg.DB, args *OGImageArgs) (*bytes.Buffer, error) {
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
		return nil, errors.Wrap(err, "failed to decode poster")
	}

	// Resize the poster to fit the inner padding box height-first; the
	// width then follows the aspect ratio. Vertical 2:3 posters end up at
	// ~373x560 and sit centered with ~414px of background on each side.
	posterFit := imaging.Resize(srcImg, 0, ogImageInnerH, imaging.Lanczos)
	pw := posterFit.Bounds().Dx()
	ph := posterFit.Bounds().Dy()

	canvas := imaging.New(ogImageWidth, ogImageHeight, ogImageBgColor)
	x := (ogImageWidth - pw) / 2
	y := (ogImageHeight - ph) / 2
	canvas = imaging.Overlay(canvas, posterFit, image.Pt(x, y), 1.0)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: ogImageJPEGQuality}); err != nil {
		return nil, err
	}
	return &buf, nil
}

func (s *Handler) getOGImageFromCache(ctx context.Context, s3Cl *s3.S3, args *OGImageArgs) (*bytes.Buffer, error) {
	r, err := s3Cl.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.posterCacheS3Bucket),
		Key:    aws.String(args.Key()),
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
	if _, err := io.Copy(&buf, r.Body); err != nil {
		return nil, err
	}
	return &buf, nil
}

func (s *Handler) putOGImageToCache(ctx context.Context, s3Cl *s3.S3, args *OGImageArgs, b *bytes.Buffer) error {
	data := b.Bytes()
	_, err := s3Cl.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket:     aws.String(s.posterCacheS3Bucket),
		Key:        aws.String(args.Key()),
		Body:       bytes.NewReader(data),
		ContentMD5: s.makeAWSMD5(data),
	})
	return err
}
