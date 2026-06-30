package stremio

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	sv "github.com/webtor-io/web-ui/services/common"
	lr "github.com/webtor-io/web-ui/services/link_resolver"
	"github.com/webtor-io/web-ui/services/link_resolver/common"
	ptn "github.com/webtor-io/web-ui/services/parse_torrent_name"
)

// EnrichStream wraps another StreamsService to enrich streams with URLs.
//
// Enrichment is intentionally lightweight: per stream we issue a single
// hash-only availability check (Postgres) and emit a redirect URL whose
// JWT carries the file's filename (or fileIdx). The expensive bits — making
// sure rest-api knows the magnet, listing its contents, picking the path —
// are deferred to /stremio/resolve, which only runs when the user actually
// clicks the stream. This keeps /stream's tail latency dominated by the
// inner addon HTTP fan-out rather than per-stream rest-api round-trips.
type EnrichStream struct {
	inner        StreamsService
	linkResolver *lr.LinkResolver
	u            *auth.User
	cla          *claims.Data
	domain       string
	token        string
	secret       string
}

// NewEnrichStream creates a new EnrichStream service
func NewEnrichStream(inner StreamsService, lr *lr.LinkResolver, u *auth.User, cla *claims.Data, domain, token, secret string) *EnrichStream {
	return &EnrichStream{
		inner:        inner,
		linkResolver: lr,
		u:            u,
		cla:          cla,
		domain:       domain,
		token:        token,
		secret:       secret,
	}
}

// GetName returns the name of the inner service with "Enrich" prefix
func (s *EnrichStream) GetName() string {
	return "Enrich" + s.inner.GetName()
}

// GetStreams gets streams from the inner service and enriches them with URLs
func (s *EnrichStream) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	response, err := s.inner.GetStreams(ctx, contentType, contentID)
	if err != nil {
		return nil, err
	}

	if response == nil || len(response.Streams) == 0 {
		return response, nil
	}

	enrichedStreams := make([]*StreamItem, len(response.Streams))
	var wg sync.WaitGroup

	eCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	for i, stream := range response.Streams {
		wg.Add(1)
		go func(index int, si *StreamItem) {
			defer wg.Done()

			streamCopy := *si
			enriched, err := s.enrichStream(eCtx, &streamCopy)
			if err != nil {
				log.WithError(err).
					WithField("infohash", streamCopy.InfoHash).
					Warn("failed to enrich stream")
				return
			}
			if enriched == nil {
				return
			}
			enrichedStreams[index] = enriched
		}(i, &stream)
	}

	wg.Wait()

	var finalStreams []StreamItem
	for _, stream := range enrichedStreams {
		if stream != nil {
			finalStreams = append(finalStreams, *stream)
		}
	}

	// Within each resolution bucket put cached (⚡) entries first so users
	// see Vault hits at the top without disturbing the global resolution
	// order set by PreferredStream.
	sortVaultFirstByResolution(finalStreams)

	return &StreamsResponse{Streams: finalStreams}, nil
}

// sortVaultFirstByResolution stable-sorts each resolution group so library
// streams come first, then cached (⚡) items, preserving every other relative
// order.
//
// The library-first tier matters because PreferredStream already pinned
// library streams to the front, but it groups by resolution: when the last
// library stream shares a resolution with the following addon group they land
// in one contiguous bucket here. Sorting that bucket on Cached alone let a
// cached addon overtake a *non-cached* library stream — re-inverting the
// priority PreferredStream established. Library origin is the primary key so
// the user's own torrents stay on top regardless of cache state.
func sortVaultFirstByResolution(streams []StreamItem) {
	if len(streams) < 2 {
		return
	}
	parser := ptn.NewCompoundParser([]ptn.Parser{ptn.GetFieldParser(ptn.FieldTypeResolution)})
	resolutionOf := func(name string) string {
		ms := ptn.Matches{}
		ms, err := parser.Parse(name, ms)
		if err != nil {
			return "other"
		}
		ti := &ptn.TorrentInfo{}
		ti.Map(ms)
		switch ti.Resolution {
		case "":
			return "other"
		case "2160p":
			return "4k"
		default:
			return ti.Resolution
		}
	}
	start := 0
	currentRes := resolutionOf(streams[0].Name)
	for i := 1; i <= len(streams); i++ {
		var res string
		if i < len(streams) {
			res = resolutionOf(streams[i].Name)
		}
		if i == len(streams) || res != currentRes {
			if i-start > 1 {
				bucket := streams[start:i]
				sort.SliceStable(bucket, func(a, b int) bool {
					al, bl := isLibraryStream(&bucket[a]), isLibraryStream(&bucket[b])
					if al != bl {
						return al // library streams always above addon streams
					}
					return bucket[a].Cached && !bucket[b].Cached
				})
			}
			start = i
			currentRes = res
		}
	}
}

// enrichStream sets the redirect URL and ⚡ marker for a single stream.
func (s *EnrichStream) enrichStream(ctx context.Context, stream *StreamItem) (*StreamItem, error) {
	if stream.Url != "" {
		return stream, nil
	}
	if stream.InfoHash == "" {
		return nil, errors.New("stream has no InfoHash")
	}

	availability, err := s.linkResolver.CheckAvailability(ctx, s.u.ID, s.cla, stream.InfoHash, stream.FileIdx, true)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check availability")
	}
	stream.Name = s.updateStreamName(stream.Name, availability)
	// Mark the user's own library entries with a ⭐ so they're unmistakable
	// next to addon results. Library streams are the ones carrying the
	// webtorio| bingeGroup; addon P2P streams reach this path too (they also
	// arrive without a Url), so the marker is keyed on origin, not Url.
	if isLibraryStream(stream) {
		stream.Name = "⭐ " + stream.Name
	}
	if availability != nil && availability.Cached {
		stream.Cached = true
	}

	stream.Url = s.generateRedirectURL(stream)
	return stream, nil
}

// generateRedirectURL builds the /stremio/resolve URL. The JWT carries
// only the minimum needed to identify the file at click time: the
// torrent's infohash and the file index inside it. /resolve resolves the
// idx to a full path against rest-api lazily.
func (s *EnrichStream) generateRedirectURL(stream *StreamItem) string {
	clms := jwt.MapClaims{
		"hash": stream.InfoHash,
		"idx":  stream.FileIdx,
		"exp":  time.Now().Add(12 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, clms)
	tokenString, _ := token.SignedString([]byte(s.secret))
	return fmt.Sprintf("%s/%s/%s/stremio/resolve/%s", s.domain, sv.AccessTokenParamName, s.token, tokenString)
}

func (s *EnrichStream) updateStreamName(name string, availability *common.CheckAvailabilityResult) string {
	var prefix string
	if availability != nil {
		if availability.Cached {
			prefix = "⚡"
		}
		if pn, ok := servicesNames[availability.ServiceType]; ok {
			prefix += pn
		}
	} else {
		prefix = "P2P"
	}
	return fmt.Sprintf("[%s]\n%s", prefix, name)
}

var servicesNames = map[models.StreamingBackendType]string{
	models.StreamingBackendTypeWebtor:     "WT",
	models.StreamingBackendTypeRealDebrid: "RD",
	models.StreamingBackendTypeTorbox:     "TB",
}
