package library

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/web"
)

// Cast photos (enrich.CastMember.Profile), proxied like posters and episode
// stills so the viewer's browser never talks to the TMDB CDN:
//
//	GET /lib/person/photo/<tmdb image file name>/<width>.jpg
//
// The answer is a width×width square cut from the top of the portrait, where
// the face is, for the circles in the cast strip. Cached in the poster bucket.

// personPhotoName is a TMDB image file name as profile_path carries it, minus
// the slash. Nothing else is fetched: the route can only reach TMDB images.
// Profiles are always .jpg; .png would let in logos, which come in any shape.
var personPhotoName = regexp.MustCompile(`^[A-Za-z0-9]{8,64}\.jpg$`)

// personPhotoSource is the TMDB size the square is cut from; 185px wide
// covers the 96px the strip asks for with room to spare.
const personPhotoSource = "w185"

var errPersonPhotoNotFound = errors.New("no such photo")

type PersonPhotoArgs struct {
	name  string
	width int
}

func (s *PersonPhotoArgs) Key() string {
	return fmt.Sprintf("person/%v/%v.jpg", s.name, s.width)
}

func bindPersonPhotoArgs(c *gin.Context) (*PersonPhotoArgs, error) {
	name := c.Param("name")
	if !personPhotoName.MatchString(name) {
		return nil, errors.Errorf("wrong photo name %v", name)
	}
	w, ok := strings.CutSuffix(c.Param("file"), ".jpg")
	if !ok {
		return nil, errors.Errorf("wrong file %v", c.Param("file"))
	}
	width, err := strconv.Atoi(w)
	if err != nil {
		return nil, errors.Errorf("wrong width %v", w)
	}
	// Only the width the strip asks for. poster_resolver's 32..1600 is not a
	// bound here: Fill scales a source thinner than 100px to cover the whole
	// square before cropping, so a caller-chosen image at 1600 costs hundreds
	// of MB on this unauthenticated route; at 96 it is about 1 MB.
	if width != web.PersonPhotoWidth {
		return nil, errors.Errorf("wrong width %v", width)
	}
	return &PersonPhotoArgs{name: name, width: width}, nil
}

func (s *Handler) personPhoto(c *gin.Context) {
	pa, err := bindPersonPhotoArgs(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, errors.Wrap(err, "failed to bind person photo args"))
		return
	}
	b, err := s.getPersonPhotoWithCache(c.Request.Context(), pa)
	if errors.Is(err, errPersonPhotoNotFound) {
		// The template's placeholder shows through (onerror drops the <img>).
		c.Status(http.StatusNotFound)
		return
	}
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get person photo"))
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

func (s *Handler) getPersonPhotoWithCache(ctx context.Context, args *PersonPhotoArgs) (*bytes.Buffer, error) {
	if s.s3Cl == nil || s.posterCacheS3Bucket == "" {
		return s.getPersonPhoto(ctx, args)
	}
	cl := s.s3Cl.Get()
	b, err := s.getStillFromCache(ctx, cl, args)
	if err != nil {
		return nil, err
	}
	if b != nil {
		return b, nil
	}
	b, err = s.getPersonPhoto(ctx, args)
	if err != nil {
		return nil, err
	}
	// A failed cache write still serves the photo; the next request retries.
	if err := s.putStillToCache(ctx, cl, args, b); err != nil {
		log.WithError(err).WithField("key", args.Key()).Warn("person photo cache put failed")
	}
	return b, nil
}

func (s *Handler) getPersonPhoto(ctx context.Context, args *PersonPhotoArgs) (*bytes.Buffer, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	u := fmt.Sprintf("%s/%s/%s", s.tmdbImageBaseURL, personPhotoSource, args.name)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
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
	if resp.StatusCode == http.StatusNotFound {
		return nil, errPersonPhotoNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("tmdb image status %d", resp.StatusCode)
	}
	src, err := imaging.Decode(resp.Body)
	if err != nil {
		return nil, err
	}
	square := imaging.Fill(src, args.width, args.width, imaging.Top, imaging.Lanczos)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, square, &jpeg.Options{Quality: PosterJPEGQuality}); err != nil {
		return nil, err
	}
	return &buf, nil
}
