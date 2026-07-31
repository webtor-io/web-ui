package helpers

import (
	"testing"

	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

// stub implements just enough of VideoContentWithMetadata to exercise the
// nil-handling; the real Movie/Series carry loaded relations that may be
// absent.
type stub struct {
	content  *models.VideoContent
	metadata *models.VideoMetadata
}

func (s stub) GetID() uuid.UUID                                   { return uuid.UUID{} }
func (s stub) GetContentType() models.ContentType                 { return models.ContentTypeMovie }
func (s stub) GetContent() *models.VideoContent                   { return s.content }
func (s stub) GetMetadata() *models.VideoMetadata                 { return s.metadata }
func (s stub) GetPath() *string                                   { return nil }
func (s stub) GetFileIdx() *int                                   { return nil }
func (s stub) GetFileSize() *int64                                { return nil }
func (s stub) GetEpisode(season int, episode int) *models.Episode { return nil }

func y(v int16) *int16 { return &v }

// A title enriched without a year used to dereference a nil *int16 and panic
// inside the template, taking the whole library page render with it.
func TestGetYearMetadataWithoutYear(t *testing.T) {
	h := NewVideoContentHelper()
	m := stub{metadata: &models.VideoMetadata{}, content: &models.VideoContent{Year: y(1999)}}
	if got := h.GetYear(m); got != 1999 {
		t.Errorf("expected the content year as fallback, got %d", got)
	}
	if !h.HasYear(m) {
		t.Error("hasYear must be true when the content knows the year")
	}
}

func TestGetYearPrefersMetadata(t *testing.T) {
	h := NewVideoContentHelper()
	m := stub{metadata: &models.VideoMetadata{Year: y(2020)}, content: &models.VideoContent{Year: y(1999)}}
	if got := h.GetYear(m); got != 2020 {
		t.Errorf("metadata must win, got %d", got)
	}
}

func TestGetYearNothingKnown(t *testing.T) {
	h := NewVideoContentHelper()
	for name, m := range map[string]stub{
		"no year anywhere": {metadata: &models.VideoMetadata{}, content: &models.VideoContent{}},
		"no content":       {metadata: &models.VideoMetadata{}},
		"nothing at all":   {},
	} {
		if got := h.GetYear(m); got != 0 {
			t.Errorf("%s: want 0, got %d", name, got)
		}
		if h.HasYear(m) {
			t.Errorf("%s: hasYear must be false", name)
		}
	}
}

// GetContent is nil whenever the relation was not loaded.
func TestGetTitleWithoutContent(t *testing.T) {
	h := NewVideoContentHelper()
	if got := h.GetTitle(stub{}); got != "" {
		t.Errorf("want empty title, got %q", got)
	}
	if got := h.GetTitle(stub{content: &models.VideoContent{Title: "From content"}}); got != "From content" {
		t.Errorf("want the content title, got %q", got)
	}
	if got := h.GetTitle(stub{metadata: &models.VideoMetadata{Title: "From metadata"}}); got != "From metadata" {
		t.Errorf("want the metadata title, got %q", got)
	}
}
