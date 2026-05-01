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
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	sv "github.com/webtor-io/web-ui/services/common"
	lr "github.com/webtor-io/web-ui/services/link_resolver"
	"github.com/webtor-io/web-ui/services/link_resolver/common"
	ptn "github.com/webtor-io/web-ui/services/parse_torrent_name"
)

// EnrichStream wraps another StreamsService to enrich streams with URLs
type EnrichStream struct {
	inner        StreamsService
	api          *api.Api
	claims       *api.Claims
	linkResolver *lr.LinkResolver
	u            *auth.User
	cla          *claims.Data
	domain       string
	token        string
	secret       string
}

// NewEnrichStream creates a new EnrichStream service
func NewEnrichStream(inner StreamsService, api *api.Api, lr *lr.LinkResolver, u *auth.User, claims *api.Claims, cla *claims.Data, domain, token, secret string) *EnrichStream {
	return &EnrichStream{
		inner:        inner,
		api:          api,
		claims:       claims,
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
	// Get streams from inner service
	response, err := s.inner.GetStreams(ctx, contentType, contentID)
	if err != nil {
		return nil, err
	}

	if response == nil || len(response.Streams) == 0 {
		return response, nil
	}

	// Enrich streams in parallel while preserving order
	enrichedStreams := make([]*StreamItem, len(response.Streams))
	var wg sync.WaitGroup

	eCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	for i, stream := range response.Streams {
		wg.Add(1)
		go func(index int, si *StreamItem) {
			defer wg.Done()

			// Create a copy of the stream to avoid potential shared memory issues
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
			enriched = &streamCopy
			enrichedStreams[index] = enriched
		}(i, &stream)
	}

	wg.Wait()

	// Filter out empty streams (those that were dropped due to timeout)
	var finalStreams []StreamItem
	for _, stream := range enrichedStreams {
		// Check if stream was dropped (empty InfoHash indicates dropped stream)
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

// sortVaultFirstByResolution stable-sorts cached items to the front of
// each resolution group while preserving every other relative order.
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
	// Locate contiguous runs of the same resolution and sort within each.
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
					return bucket[a].Cached && !bucket[b].Cached
				})
			}
			start = i
			currentRes = res
		}
	}
}

// enrichStream enriches a single stream with URL if needed
func (s *EnrichStream) enrichStream(ctx context.Context, stream *StreamItem) (*StreamItem, error) {
	// Step 1: Check if there is URL in stream
	if stream.Url != "" {
		return stream, nil
	}

	// Check if we have InfoHash
	if stream.InfoHash == "" {
		return nil, errors.New("stream has no InfoHash")
	}

	p, err := s.getFilePathFromStream(ctx, stream)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert idx to path")
	}
	availability, err := s.linkResolver.CheckAvailability(ctx, s.u.ID, s.cla, stream.InfoHash, p, true)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check availability")
	}
	stream.Name = s.updateStreamName(stream.Name, availability)
	if availability != nil && availability.Cached {
		stream.Cached = true
	}

	stream.Url = s.generateRedirectURL(stream.InfoHash, p)
	return stream, nil
}

func (s *EnrichStream) getFilePathFromStream(ctx context.Context, stream *StreamItem) (string, error) {
	listArgs := &api.ListResourceContentArgs{
		Limit:  100,
		Offset: 0,
	}

	var targetItem *ra.ListItem
	var idx int

	// Paginate through results to find the file at the specified index
	for {
		resp, err := s.api.ListResourceContentCached(ctx, s.claims, stream.InfoHash, listArgs)
		if err != nil {
			return "", err
		}

		for _, item := range resp.Items {
			if item.Type == ra.ListTypeFile {
				if (stream.BehaviorHints != nil && stream.BehaviorHints.Filename != "" && item.Name == stream.BehaviorHints.Filename) ||
					((stream.BehaviorHints == nil || stream.BehaviorHints.Filename == "") && idx == int(stream.FileIdx)) {
					targetItem = &item
					break
				}
				idx++
			}
		}

		if targetItem != nil {
			break
		}

		// Check if we've reached the end
		if (resp.Count - int(listArgs.Offset)) == len(resp.Items) {
			break
		}

		listArgs.Offset += listArgs.Limit
	}

	if targetItem == nil {
		return "", fmt.Errorf("file at index %d not found", stream.FileIdx)
	}

	return targetItem.PathStr, nil
}

func (s *EnrichStream) generateRedirectURL(hash string, p string) string {
	clms := jwt.MapClaims{
		"hash": hash,
		"path": p,
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
