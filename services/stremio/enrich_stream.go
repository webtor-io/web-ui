package stremio

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	sv "github.com/webtor-io/web-ui/services/common"
	lr "github.com/webtor-io/web-ui/services/link_resolver"
	"github.com/webtor-io/web-ui/services/link_resolver/common"
)

// EnrichStream wraps another StreamsService to enrich streams with URLs
type EnrichStream struct {
	inner        StreamsService
	api          *api.Api
	claims       *api.Claims
	linkResolver *lr.LinkResolver
	uID          uuid.UUID
	cla          *claims.Data
	domain       string
	token        string
	secret       string
}

// NewEnrichStream creates a new EnrichStream service
func NewEnrichStream(inner StreamsService, api *api.Api, lr *lr.LinkResolver, uID uuid.UUID, claims *api.Claims, cla *claims.Data, domain, token, secret string) *EnrichStream {
	return &EnrichStream{
		inner:        inner,
		api:          api,
		claims:       claims,
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

	p, err := s.getFilePathFromStream(ctx, stream)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert idx to path")
	}
	availability, err := s.linkResolver.CheckAvailability(ctx, s.uID, s.cla, stream.InfoHash, p, true)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check availability")
	}
	stream.Name = s.updateStreamName(stream.Name, availability)

	stream.Url = s.generateRedirectURL(stream.InfoHash, p)
	stream.InfoHash = ""
	stream.FileIdx = 0
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
