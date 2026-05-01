package stremio

import (
	"context"
	"strings"

	"github.com/go-pg/pg/v10"
	"github.com/webtor-io/web-ui/services/auth"
)

// libraryBingeGroupPrefix marks streams produced by the user's own Library
// service (see services/stremio/library.go). Library streams represent
// torrents the user has already added to their Vault and must bypass the
// language filter — the user has already opted in to those titles.
const libraryBingeGroupPrefix = "webtorio|"

// LangFilterStream drops streams whose title does not advertise the user's
// preferred language. Mirrors the strict filter applied by Discover's
// stream modal (assets/src/js/lib/discover/components/StreamModal.jsx) so
// the two surfaces behave identically. When the user has no preference set
// (empty string) the filter is a no-op.
type LangFilterStream struct {
	inner StreamsService
	db    *pg.DB
	u     *auth.User
}

func NewLangFilterStream(inner StreamsService, db *pg.DB, u *auth.User) *LangFilterStream {
	return &LangFilterStream{inner: inner, db: db, u: u}
}

func (s *LangFilterStream) GetName() string {
	return "LangFilter" + s.inner.GetName()
}

func (s *LangFilterStream) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	resp, err := s.inner.GetStreams(ctx, contentType, contentID)
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Streams) == 0 {
		return resp, nil
	}
	settings, err := GetUserSettingsDataByClaims(ctx, s.db, s.u.ID)
	if err != nil {
		return nil, err
	}
	wantCode := strings.TrimSpace(settings.PreferredLanguage)
	if wantCode == "" {
		return resp, nil
	}
	want := LanguageByCode(wantCode)
	if want == nil {
		return resp, nil
	}

	filtered := make([]StreamItem, 0, len(resp.Streams))
	for _, st := range resp.Streams {
		if isLibraryStream(&st) || streamMatchesLanguage(&st, want) {
			filtered = append(filtered, st)
		}
	}
	return &StreamsResponse{Streams: filtered}, nil
}

func isLibraryStream(st *StreamItem) bool {
	return st.BehaviorHints != nil && strings.HasPrefix(st.BehaviorHints.BingeGroup, libraryBingeGroupPrefix)
}

func streamMatchesLanguage(st *StreamItem, want *Language) bool {
	// Match the JS implementation, which extracts from the user-facing
	// stream description (s.title). Fall back to Name to be safe — some
	// addons pack the language tag into either field.
	for _, l := range ExtractLanguages(st.Title) {
		if l.Name == want.Name {
			return true
		}
	}
	for _, l := range ExtractLanguages(st.Name) {
		if l.Name == want.Name {
			return true
		}
	}
	return false
}

var _ StreamsService = (*LangFilterStream)(nil)
