package poster_resolver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/disintegration/imaging"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/thumbnail"
)

// ErrNotFound is returned when a resource has no resolvable source
// and no default fallback applies (resize mode). Distinct from real
// internal errors so the HTTP layer can map cleanly to 404.
var ErrNotFound = errors.New("poster: no source for resource")

// defaultOGBannerPath — brand banner shipped under pub/. Used as the
// last-resort source for OG canvas requests so Telegram & friends
// never cache a 404. Resize requests get ErrNotFound instead because a
// "Webtor banner" cropped into a card slot looks like a UI defect.
const defaultOGBannerPath = "./pub/webtor.jpg"

// Result is what the handler ultimately serves. ETag is precomputed
// once at miss-time so subsequent If-None-Match checks don't rehash
// the body on every request.
type Result struct {
	Body []byte
	ETag string
	Mime string
}

// Service caches rendered posters per (resource_id, file) for in-process
// dedup, and per (source-content, mode) on S3 for cross-pod + persistent
// sharing. The two layers compose:
//
//	request → lazymap (resource_id+file) → S3 (source-content+mode)
//	  → original fetch (IMDb URL / S3 thumbnail blob)
type Service struct {
	lm     *lazymap.LazyMap[*Result]
	s3Cl   *cs.S3Client
	pg     *cs.PG
	cl     *http.Client
	thumb  *thumbnail.Service
	bucket string
}

// New returns a configured Service. cacheBucket is the same poster
// cache bucket used by the legacy /lib/:type/poster/:imdb_id endpoint
// — sharing it costs nothing and lets either endpoint warm the cache
// for the other when source-content matches.
func New(s3Cl *cs.S3Client, pg *cs.PG, cl *http.Client, thumb *thumbnail.Service, cacheBucket string) *Service {
	return &Service{
		// 5-min TTL bounds memory growth under bursty library/continue-watching
		// reloads (multiple tabs of the same user, returning visitor in the same
		// hour). The S3 cache is the persistent layer; lazymap is just the
		// concurrent-request collapser + hot-path skip.
		lm: lazymap.New[*Result](&lazymap.Config{
			Expire:      5 * time.Minute,
			ErrorExpire: 30 * time.Second,
		}),
		s3Cl:   s3Cl,
		pg:     pg,
		cl:     cl,
		thumb:  thumb,
		bucket: cacheBucket,
	}
}

// Get is the entry point. file is the route's :file param ("500.jpg",
// "og.jpg"). force=true bypasses both cache layers — dev escape hatch
// wired off ?force=1 in non-release builds.
func (s *Service) Get(ctx context.Context, resourceID, file string, force bool) (*Result, error) {
	mode, err := ParseFileMode(file)
	if err != nil {
		return nil, err
	}
	if force {
		return s.miss(ctx, resourceID, mode)
	}
	key := resourceID + "/" + file
	return s.lm.Get(key, func() (*Result, error) {
		return s.miss(ctx, resourceID, mode)
	})
}

// miss runs the full pipeline: resolve → S3 cache check → render →
// S3 put. Called from lazymap on cache miss; also called directly on
// force=true.
//
// Adult/sport is checked here (not in Resolve) because it doesn't
// affect WHICH source is picked — only HOW the rendered output is
// post-processed. A separate look-up keeps the resolver function pure
// and lets us flip is_adult via SQL without re-running classification.
func (s *Service) miss(ctx context.Context, resourceID string, mode Mode) (*Result, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	src, err := Resolve(ctx, db, s.thumb, s.cl, resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve source")
	}
	if src == nil {
		// Resize mode → 404. Caller-side CSS placeholder is a better UX
		// than a brand banner cropped into a card slot.
		if !mode.OG {
			return nil, errors.Wrapf(ErrNotFound, "resource_id=%s", resourceID)
		}
		// OG canvas mode → fall back to brand banner so platforms cache
		// something durable.
		src = s.defaultOGSource()
	}

	// Force blur for adult-classified resources. Sport is tracked but
	// NOT blurred — broadcasts are legitimate content people would
	// reasonably share. Brand default never blurs — that banner is
	// generic and serving a heavily-blurred Webtor logo to Telegram
	// would just look like a server error.
	if src.Kind != sourceDefault {
		if rm, err := models.GetResourceMetadataByResourceID(ctx, db, resourceID); err == nil && rm != nil && rm.IsAdult {
			mode.Blur = true
		}
		// rm lookup error is non-fatal — default to no blur, log
		// silently. Classification will retry on the next miss.
	}

	ck := cacheKey(src, mode)
	if buf, err := s.getFromS3(ctx, ck); err != nil {
		return nil, err
	} else if buf != nil {
		return newResult(buf.Bytes()), nil
	}

	img, err := src.Fetch(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch source image")
	}
	buf, err := render(img, mode)
	if err != nil {
		return nil, err
	}
	if putErr := s.putToS3(ctx, ck, buf); putErr != nil {
		// Cache write failure is non-fatal — serve the freshly composed
		// image to this caller and let the next request retry. Logged
		// so persistent S3 issues show up in the error budget.
		log.WithError(putErr).WithField("key", ck).Warn("poster cache put failed")
	}
	return newResult(buf.Bytes()), nil
}

func newResult(body []byte) *Result {
	sum := sha256.Sum256(body)
	return &Result{
		Body: body,
		ETag: fmt.Sprintf(`"%x"`, sum[:]),
		Mime: "image/jpeg",
	}
}

// defaultOGSource returns the brand banner wrapped as a Source so it
// flows through the same render+cache pipeline as real artwork. CacheID
// is stable across resources so the rendered banner is cached once and
// reused for every OG fallback site-wide.
func (s *Service) defaultOGSource() *Source {
	return &Source{
		Kind:    sourceDefault,
		CacheID: "default",
		Fetch: func(ctx context.Context) (image.Image, error) {
			f, err := os.Open(defaultOGBannerPath)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to open %s", defaultOGBannerPath)
			}
			defer f.Close()
			img, err := imaging.Decode(f)
			if err != nil {
				return nil, errors.Wrap(err, "failed to decode default banner")
			}
			return img, nil
		},
	}
}

func (s *Service) getFromS3(ctx context.Context, key string) (*bytes.Buffer, error) {
	if s.s3Cl == nil || s.bucket == "" {
		return nil, nil
	}
	out, err := s.s3Cl.Get().GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == s3.ErrCodeNoSuchKey {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read poster cache")
	}
	defer out.Body.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, out.Body); err != nil {
		return nil, err
	}
	return &buf, nil
}

func (s *Service) putToS3(ctx context.Context, key string, b *bytes.Buffer) error {
	if s.s3Cl == nil || s.bucket == "" {
		return nil
	}
	data := b.Bytes()
	_, err := s.s3Cl.Get().PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	return err
}

