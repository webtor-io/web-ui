// Package ai_enrich holds DB models for the AI enrichment fallback.
// Currently only a query cache that memoizes Claude's title-normalization
// outcomes — see services/enrich/ai_resolver.go for the consumer.
package ai_enrich

import (
	"context"
	"strings"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
)

// missingYearSentinel mirrors the migration default for ai_enrich.query —
// "-1" stands in for a nullable parsed year so the composite primary
// key behaves like a regular UNIQUE without NULL-distinct semantics
// breaking ON CONFLICT.
const missingYearSentinel int16 = -1

// Candidate is one of Claude's normalized name suggestions.
// Mirrors enrich.TitleCandidate but lives here so the model layer
// doesn't import services/enrich. Year is optional.
type Candidate struct {
	Title    string `json:"title"`
	Year     *int16 `json:"year,omitempty"`
	Language string `json:"language,omitempty"`
}

// Query caches Claude's normalization outcome for a parsed (title, year,
// content_type) tuple. Keyed by parsed_title — the input the enrichment
// pipeline tried first. Candidates is the list Claude returned; an
// empty slice is a negative cache entry ("Claude had nothing useful to
// add for this parsed title"). The pipeline never asks Claude twice for
// the same key — see services/enrich/ai_resolver.go.
type Query struct {
	tableName struct{} `pg:"ai_enrich.query"`

	ParsedTitle string      `pg:"parsed_title,pk"`
	ParsedYear  int16       `pg:"parsed_year,pk,use_zero"`
	ContentType int16       `pg:"content_type,pk"`
	Candidates  []Candidate `pg:"candidates,type:jsonb"`
	Model       *string     `pg:"model"`
	// ResourceID and HintPath are diagnostic-only: the first torrent
	// hash + full pathHint that triggered this normalization call.
	// Preserved across re-fires of the same (parsed_title, year, ct)
	// from different resources so we don't lose the original sample.
	ResourceID *string   `pg:"resource_id"`
	HintPath   *string   `pg:"hint_path"`
	CreatedAt  time.Time `pg:"created_at,default:now()"`
	UpdatedAt  time.Time `pg:"updated_at,default:now()"`
}

// YearOrNil returns the cached parsed year as *int16, or nil when the
// stored row used the missing-year sentinel.
func (q *Query) YearOrNil() *int16 {
	if q.ParsedYear == missingYearSentinel {
		return nil
	}
	y := q.ParsedYear
	return &y
}

// normTitle is the canonical form used for the cache key. Must stay in
// sync with whatever the upsert side writes — lower + trim covers the
// common "User entered 'Vot eto drama  ' vs ' vot eto drama'" drift.
func normTitle(t string) string {
	return strings.ToLower(strings.TrimSpace(t))
}

// GetQuery returns the cached normalization for the given parsed
// (title, year, content_type), or (nil, nil) when there is no cached
// entry. The caller distinguishes "negative cache hit" from "no entry"
// by inspecting `Candidates` — an empty slice on a returned row is the
// negative cache. nil return means "fresh, ask Claude".
func GetQuery(ctx context.Context, db *pg.DB, title string, year *int16, contentType int16) (*Query, error) {
	q := &Query{}
	err := db.Model(q).
		Context(ctx).
		Where("parsed_title = ?", normTitle(title)).
		Where("parsed_year = ?", yearOrSentinel(year)).
		Where("content_type = ?", contentType).
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return q, nil
}

func yearOrSentinel(year *int16) int16 {
	if year == nil {
		return missingYearSentinel
	}
	return *year
}

// UpsertQuery stores Claude's normalization outcome. Pass an empty
// (or nil) `candidates` slice to record a negative cache entry. The
// title is stored in its normalized form so subsequent GetQuery calls
// can match without a normalize step on the read path.
//
// resourceID and hintPath are diagnostic-only — the first observed
// torrent hash and full pathHint that triggered this normalization.
// On conflict we COALESCE so the original sample is preserved when
// a later resource hits the same cache key (avoids churn from the
// 2nd, 3rd, Nth torrent overwriting useful debug context).
func UpsertQuery(ctx context.Context, db *pg.DB, title string, year *int16, contentType int16, candidates []Candidate, model, resourceID, hintPath string) error {
	if candidates == nil {
		candidates = []Candidate{}
	}
	q := &Query{
		ParsedTitle: normTitle(title),
		ParsedYear:  yearOrSentinel(year),
		ContentType: contentType,
		Candidates:  candidates,
	}
	if model != "" {
		q.Model = &model
	}
	if resourceID != "" {
		q.ResourceID = &resourceID
	}
	if hintPath != "" {
		q.HintPath = &hintPath
	}
	_, err := db.Model(q).
		Context(ctx).
		OnConflict("(parsed_title, parsed_year, content_type) DO UPDATE").
		Set("candidates = EXCLUDED.candidates").
		Set("model = EXCLUDED.model").
		// Qualify with target table name on the right of ON CONFLICT
		// SET — PostgreSQL reports `resource_id is ambiguous` for the
		// unqualified form because the EXCLUDED pseudo-table is also in
		// scope. COALESCE keeps the first observed sample and only takes
		// EXCLUDED when nothing was stored before.
		Set(`resource_id = COALESCE("query".resource_id, EXCLUDED.resource_id)`).
		Set(`hint_path = COALESCE("query".hint_path, EXCLUDED.hint_path)`).
		Set("updated_at = now()").
		Insert()
	return err
}
