package stremio

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/lazymap"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	sv "github.com/webtor-io/web-ui/services/common"
	lr "github.com/webtor-io/web-ui/services/link_resolver"
	"github.com/webtor-io/web-ui/services/link_resolver/common"
)

// APIService defines the interface for API operations needed by EnrichStream
type APIService interface {
	GetResource(ctx context.Context, c *api.Claims, infohash string) (*ra.ResourceResponse, error)
	StoreResource(ctx context.Context, c *api.Claims, resource []byte) (*ra.ResourceResponse, error)
	ListResourceContentCached(ctx context.Context, c *api.Claims, infohash string, args *api.ListResourceContentArgs) (*ra.ListResponse, error)
	ExportResourceContent(ctx context.Context, c *api.Claims, infohash string, itemID string, imdbID string) (*ra.ExportResponse, error)
}

// EnrichStream wraps another StreamsService to enrich streams with URLs
type EnrichStream struct {
	inner                      StreamsService
	api                        APIService
	claims                     *api.Claims
	storeResourceMap           lazymap.LazyMap[*ra.ResourceResponse]
	backgroundStoreResourceMap lazymap.LazyMap[*ra.ResourceResponse]
	linkResolver               *lr.LinkResolver
	uID                        uuid.UUID
	cla                        *claims.Data
	domain                     string
	token                      string
	secret                     string
}

// NewEnrichStream creates a new EnrichStream service
func NewEnrichStream(inner StreamsService, api APIService, lr *lr.LinkResolver, uID uuid.UUID, claims *api.Claims, cla *claims.Data, domain, token, secret string) *EnrichStream {
	return &EnrichStream{
		inner:  inner,
		api:    api,
		claims: claims,
		storeResourceMap: lazymap.New[*ra.ResourceResponse](&lazymap.Config{
			Concurrency: 20,
			ErrorExpire: time.Minute,
		}),
		backgroundStoreResourceMap: lazymap.New[*ra.ResourceResponse](&lazymap.Config{
			Concurrency: 20,
		}),
		linkResolver: lr,
		uID:          uID,
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

	for i, stream := range response.Streams {
		wg.Add(1)
		go func(index int, si *StreamItem) {
			defer wg.Done()

			// Create context with 5-second timeout
			streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			//streamCtx := ctx

			// Create a copy of the stream to avoid potential shared memory issues
			streamCopy := *si
			enriched, err := s.enrichStream(streamCtx, &streamCopy)
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

	return &StreamsResponse{Streams: finalStreams}, nil
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

	// Step 2: Check with webtor API if it has resource with infohash
	resource, err := s.api.GetResource(ctx, s.claims, stream.InfoHash)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get resource from API")
	}

	// If resource doesn't exist, store it
	if resource == nil {
		// Step 3: Make magnet URL and store it in API using lazymap
		magnetURL := s.makeMagnetURL(stream.InfoHash, stream.Sources)

		_, err := s.storeResourceMap.Get(magnetURL, func() (*ra.ResourceResponse, error) {
			return s.api.StoreResource(ctx, s.claims, []byte(magnetURL))
		})
		if err != nil {
			// Check if the error is due to context deadline
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				// Start background goroutine to retry with longer timeout
				go func() {
					log.WithField("infohash", stream.InfoHash).
						Info("started resource store in background")
					err := s.backgroundStoreResource(stream.InfoHash, magnetURL)
					if err != nil {
						log.WithError(err).
							WithField("infohash", stream.InfoHash).
							Warn("failed to store resource in background")
						return
					}
					log.WithField("infohash", stream.InfoHash).
						Info("resource stored in background")
				}()
			}
			return nil, errors.Wrap(ctx.Err(), "failed to store resource")
		}
	}

	p, err := s.getFilePathFromStream(ctx, stream)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert idx to path")
	}
	//availability, err := s.linkResolver.CheckAvailability(ctx, s.uID, s.claims, s.cla, stream.InfoHash, p, true)
	//if err != nil {
	//	return nil, errors.Wrap(err, "failed to check availability")
	//}
	//stream.Name = s.updateStreamName(stream.Name, availability)

	// Step 5: Add generated URL to Stream
	//if availability != nil {
	stream.Url = s.generateRedirectURL(stream.InfoHash, p)
	//}
	return stream, nil
}

// backgroundStoreResource attempts to store a resource in the background with extended timeout
func (s *EnrichStream) backgroundStoreResource(infohash, magnetURL string) error {
	// Create a background context with 5-minutes timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_, err := s.backgroundStoreResourceMap.Get(magnetURL, func() (*ra.ResourceResponse, error) {
		return s.api.StoreResource(ctx, s.claims, []byte(magnetURL))
	})
	return err
}

// makeMagnetURL creates a magnet URL from InfoHash and Sources
func (s *EnrichStream) makeMagnetURL(infohash string, sources []string) string {
	magnetURL := fmt.Sprintf("magnet:?xt=urn:btih:%s", infohash)

	// Add trackers from sources
	for _, source := range sources {
		if strings.TrimSpace(source) != "" {
			// URL encode the tracker
			encoded := url.QueryEscape(source)
			magnetURL += "&tr=" + encoded
		}
	}

	return magnetURL
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
	return fmt.Sprintf("%s/%s/%s/stremio/redirect/%s", s.domain, sv.AccessTokenParamName, s.token, tokenString)
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
