package recommendations

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	tm "github.com/webtor-io/web-ui/models/tmdb"
)

// FreshReleasesLoader returns a pre-formatted prompt block listing recent
// popular films that may postdate Claude's training data. The block is
// injected as a second system TextBlockParam with its own cache_control
// breakpoint, so it's cached independently of the base system prompt.
type FreshReleasesLoader interface {
	// LoadFreshReleases returns a formatted text block ready to embed in
	// the system prompt, or "" if no releases are available (cron hasn't
	// run yet, or the query returned nothing).
	LoadFreshReleases(ctx context.Context) string
}

// DBFreshReleasesLoader queries tmdb.info for recent popular films and
// formats them into a compact prompt block. An in-memory cache avoids
// hitting the database on every request — the block is the same for
// all users and changes only when the `enrich popular` cron writes new
// entries (every ~6h).
type DBFreshReleasesLoader struct {
	pg       *cs.PG
	minYear  int16
	limit    int
	cacheTTL time.Duration

	mu       sync.Mutex
	cached   string
	cachedAt time.Time
}

// NewDBFreshReleasesLoader wires a loader. cacheTTL controls how long the
// in-memory formatted block is reused before re-querying the database.
func NewDBFreshReleasesLoader(pg *cs.PG, minYear int16, limit int, cacheTTLSeconds int) *DBFreshReleasesLoader {
	ttl := time.Duration(cacheTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &DBFreshReleasesLoader{
		pg:       pg,
		minYear:  minYear,
		limit:    limit,
		cacheTTL: ttl,
	}
}

func (l *DBFreshReleasesLoader) LoadFreshReleases(ctx context.Context) string {
	l.mu.Lock()
	if l.cached != "" && time.Since(l.cachedAt) < l.cacheTTL {
		block := l.cached
		l.mu.Unlock()
		return block
	}
	l.mu.Unlock()

	block := l.buildBlock(ctx)

	l.mu.Lock()
	l.cached = block
	l.cachedAt = time.Now()
	l.mu.Unlock()

	return block
}

func (l *DBFreshReleasesLoader) buildBlock(ctx context.Context) string {
	db := l.pg.Get()
	if db == nil {
		return ""
	}
	infos, err := tm.ListRecentPopular(ctx, db, l.minYear, l.limit)
	if err != nil {
		log.WithError(err).WithField("feature", "ai_rec").Warn("failed to load fresh releases")
		return ""
	}
	if len(infos) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# RECENT RELEASES (since %d)\n", l.minYear))
	sb.WriteString("# These films are verified real releases from TMDB that may postdate\n")
	sb.WriteString("# your training data. Use them when relevant to the user's request.\n\n")

	for _, info := range infos {
		year := ""
		if info.Year != nil {
			year = fmt.Sprintf(" (%d)", *info.Year)
		}

		genres := extractGenreNames(info.Metadata)
		genreStr := ""
		if len(genres) > 0 {
			genreStr = " [" + strings.Join(genres, ", ") + "]"
		}

		rating := ""
		if va, ok := info.Metadata["vote_average"].(float64); ok && va > 0 {
			rating = fmt.Sprintf(" %.1f", va)
		}

		sb.WriteString(info.Title)
		sb.WriteString(year)
		sb.WriteString(genreStr)
		sb.WriteString(rating)
		sb.WriteByte('\n')
	}

	log.WithFields(log.Fields{
		"feature": "ai_rec",
		"count":   len(infos),
	}).Debug("fresh releases block built")

	return sb.String()
}

// extractGenreNames pulls genre name strings from the TMDB metadata
// "genres" field, which is an array of {"id": N, "name": "..."} objects.
func extractGenreNames(metadata map[string]any) []string {
	raw, ok := metadata["genres"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(arr))
	for _, g := range arr {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := gm["name"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	return names
}
