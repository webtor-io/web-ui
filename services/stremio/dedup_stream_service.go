package stremio

import (
	"context"
)

// DedupStreamService wraps another StreamService and removes duplicate streams
// based on infohash and file index while preserving order
type DedupStreamService struct {
	inner StreamService
}

// Ensure DedupStreamService implements StreamService
var _ StreamService = (*DedupStreamService)(nil)

// NewDedupStreamService creates a new deduplication stream service that wraps the given service
func NewDedupStreamService(inner StreamService) *DedupStreamService {
	return &DedupStreamService{
		inner: inner,
	}
}

// GetName returns the name of this dedup stream service for logging purposes
func (d *DedupStreamService) GetName() string {
	return "DedupStreamService"
}

// dedupKey represents the unique key used for deduplication
type dedupKey struct {
	InfoHash string
	FileIdx  uint8
}

// GetStreams fetches streams from the inner service and removes duplicates
// based on infohash and file index while maintaining original order
func (d *DedupStreamService) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	// Get streams from inner service
	response, err := d.inner.GetStreams(ctx, contentType, contentID)
	if err != nil {
		return nil, err
	}

	if response == nil || len(response.Streams) == 0 {
		return response, nil
	}

	// Track seen combinations of infohash and file index
	seen := make(map[dedupKey]bool)
	var dedupedStreams []StreamItem

	// Process streams in order, keeping only the first occurrence of each unique combination
	for _, stream := range response.Streams {
		key := dedupKey{
			InfoHash: stream.InfoHash,
			FileIdx:  stream.FileIdx,
		}

		// Only add the stream if we haven't seen this combination before
		if !seen[key] {
			seen[key] = true
			dedupedStreams = append(dedupedStreams, stream)
		}
	}

	return &StreamsResponse{Streams: dedupedStreams}, nil
}
