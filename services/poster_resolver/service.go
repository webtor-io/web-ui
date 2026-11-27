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
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/disintegration/imaging"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/thumbnail"
	"github.com/webtor-io/web-ui/services/uploads"
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
	// Private: the bytes come from a private resource's own source — its frame, a file of it, the
	// artwork of the title it was matched to. Get hands such a result only to a viewer rest-api
	// admits, and the handler never lets it be cached. The brand banner is nobody's.
	Private bool
	// Unknown: rest-api does not have the resource, and its 404 is no proof the resource is public.
	// The picture goes to everyone, as before, but the handler does not let it be cached. Not set
	// for the brand banner.
	Unknown bool
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
	api    *api.Api
	bucket string
}

// New returns a configured Service. cacheBucket is the same poster
// cache bucket used by the legacy /lib/:type/poster/:imdb_id endpoint
// — sharing it costs nothing and lets either endpoint warm the cache
// for the other when source-content matches.
func New(s3Cl *cs.S3Client, pg *cs.PG, cl *http.Client, thumb *thumbnail.Service, a *api.Api, cacheBucket string) *Service {
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
		api:    a,
		bucket: cacheBucket,
	}
}

// Get is the entry point. file is the route's :file param ("500.jpg",
// "og.jpg"). force=true bypasses both cache layers — dev escape hatch
// wired off ?force=1 in non-release builds. allowRaw=true tells the
// miss path to skip the adult blur entirely; reserved for the
// auth-gated /raw/ route, which is the opt-out for users who've
// flipped UserSettings.ShowAdult.
//
// viewer is the caller's own claims (api.GetClaimsFromContext): a private
// resource's picture is served only to the one rest-api opens the resource to.
func (s *Service) Get(ctx context.Context, viewer *api.Claims, resourceID, file string, force, allowRaw bool) (*Result, error) {
	mode, err := ParseFileMode(file)
	if err != nil {
		return nil, err
	}
	mode.Raw = allowRaw
	var res *Result
	if force {
		res, err = s.miss(ctx, resourceID, mode)
	} else {
		// Cache key already encodes Mode.Raw via CacheTag's `raw-` prefix
		// so blurred and unblurred renders of the same source live as
		// independent lazymap / S3 entries.
		key := resourceID + "/" + mode.CacheTag()
		res, err = s.lm.Get(key, func() (*Result, error) {
			return s.miss(ctx, resourceID, mode)
		})
	}
	if err != nil {
		return nil, err
	}
	// Outside the lazymap entry: one entry serves everyone who asks for the
	// picture within its TTL, the right is asked on every request.
	if res.Private {
		if err := s.admit(ctx, viewer, resourceID); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// admit asks rest-api, as the resource page does, whether the viewer may use
// the resource. A refusal and an unknown resource are ErrNotFound; any other
// failure is an error, never bytes.
//
// The cached read keeps a success for a minute per (viewer, resource): a
// library page shows dozens of pictures. A revoked right still opens the
// picture for up to a minute on a pod.
func (s *Service) admit(ctx context.Context, viewer *api.Claims, resourceID string) error {
	r, err := s.api.GetResourceCached(ctx, viewer, resourceID)
	if api.IsPrivateRefusal(err) || errors.Is(err, api.ErrForbidden) || (err == nil && r == nil) {
		return errors.Wrapf(ErrNotFound, "resource_id=%s: not the viewer's", resourceID)
	}
	if err != nil {
		return errors.Wrap(err, "failed to check the viewer")
	}
	return nil
}

// miss runs the full pipeline: resolve → S3 cache check → render →
// S3 put. Called from lazymap on cache miss; also called directly on
// force=true.
//
// A private resource's share card is the brand banner, and its other
// pictures are marked Private: Get then asks rest-api about every viewer,
// and the handler keeps it out of every cache. A resource rest-api does not
// know gets no frame of its own, and its picture is marked Unknown: served to
// everyone, cached by no one — but an upload rest-api does not know has no
// source at all. A failed lookup is an error, never the default.
func (s *Service) miss(ctx context.Context, resourceID string, mode Mode) (*Result, error) {
	r, err := s.api.LookupResource(ctx, api.InternalClaims(), resourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to look up resource")
	}
	// An upload is private whether rest-api says so or not, and whether rest-api has it now or not:
	// a 200 without the flag must not make its frame public, and after eviction the lookup is a 404
	// while the enrichment rows outlive the files — nothing of it has a source then.
	private := (r != nil && r.Private) || uploads.Owned(resourceID)
	gone := r == nil && uploads.Owned(resourceID)
	var (
		db  *pg.DB
		src *Source
	)
	// A private resource's share card never has a source: crawlers fetch it without a session and
	// get the banner. An upload that is gone has none at all.
	if !gone && (!private || !mode.OG) {
		db = s.pg.Get()
		if db == nil {
			return nil, errors.New("no db")
		}
		th := s.thumb
		if r == nil {
			// rest-api does not have the resource, and its 404 is no proof the resource is public:
			// no frame of it is looked at, only the artwork of a matched title, as before.
			th = nil
		}
		src, err = Resolve(ctx, db, th, s.cl, resourceID)
		if err != nil {
			return nil, errors.Wrap(err, "failed to resolve source")
		}
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
	res, err := s.produce(ctx, db, resourceID, src, mode)
	if err != nil {
		return nil, err
	}
	// The one place the flags are set: every way produce returns (S3 hit, render, banner fallback).
	res.Private = private && src.Kind != sourceDefault
	res.Unknown = r == nil && src.Kind != sourceDefault
	return res, nil
}

// produce turns a source into the served picture: blur decision, S3 cache
// check, fetch, render, S3 put.
//
// Adult/sport is checked here (not in Resolve) because it doesn't
// affect WHICH source is picked — only HOW the rendered output is
// post-processed. A separate look-up keeps the resolver function pure
// and lets us flip is_adult via SQL without re-running classification.
func (s *Service) produce(ctx context.Context, db *pg.DB, resourceID string, src *Source, mode Mode) (*Result, error) {
	// Force blur for adult-classified resources. Sport is tracked but
	// NOT blurred — broadcasts are legitimate content people would
	// reasonably share. Brand default never blurs — that banner is
	// generic and serving a heavily-blurred Webtor logo to Telegram
	// would just look like a server error.
	//
	// mode.Raw skips the blur entirely. The auth-gated /raw/ route
	// sets it for users who've opted into showing adult content
	// unblurred (UserSettings.ShowAdult=true); reaching this code path
	// without the opt-in is the route handler's responsibility, not
	// the resolver's.
	if !mode.Raw && src.Kind != sourceDefault {
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
	// miss marks the result by its own source, not by this banner: the banner
	// of a resource rest-api does not know goes out Unknown, uncached.
	if err != nil && mode.OG && src.Kind != sourceDefault && errors.Is(err, ErrNotFound) {
		// The share card must not fail because the artwork is gone: same
		// fallback as a resource with no source at all, in miss. Not cached
		// under the dead source's key -- the default has its own.
		src = s.defaultOGSource()
		mode.Blur = false
		ck = cacheKey(src, mode)
		if buf, cerr := s.getFromS3(ctx, ck); cerr == nil && buf != nil {
			return newResult(buf.Bytes()), nil
		}
		img, err = src.Fetch(ctx)
	}
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

// defaultOGCacheID keys the rendered banner by the banner file's bytes.
// A fixed key outlived the artwork: the S3 copy rendered from the previous
// pub/webtor.jpg kept being served after the file was redrawn, because
// nothing ever expires it.
var defaultOGCacheID = sync.OnceValue(func() string {
	return bannerCacheID(defaultOGBannerPath)
})

// bannerCacheID is "default-" plus a digest of the file, or plain "default"
// when the file cannot be read — Fetch then fails with the real error, and
// nothing is cached under either key.
func bannerCacheID(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "default"
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("default-%x", sum[:8])
}

// defaultOGSource returns the brand banner wrapped as a Source so it
// flows through the same render+cache pipeline as real artwork. CacheID
// is stable across resources so the rendered banner is cached once and
// reused for every OG fallback site-wide, and changes with the banner.
func (s *Service) defaultOGSource() *Source {
	return &Source{
		Kind:    sourceDefault,
		CacheID: defaultOGCacheID(),
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
