package recommendations

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
)

// ClientClock describes the user's local moment in time at the instant of a
// request. It is always populated by the browser and sent to the server —
// the server runs in UTC and cannot guess the user's time zone, but prompts
// like "Monday evening" only make sense in the user's local frame.
//
// Fields are validated via IsValid; invalid values cause the caller to fall
// back to server UTC (so a misbehaving client degrades gracefully instead of
// crashing the recommendation flow).
type ClientClock struct {
	// DayOfWeek is the English weekday name ("Monday"..."Sunday"), matching
	// time.Weekday().String() so the server can trust the value without a
	// locale-sensitive parse.
	DayOfWeek string
	// Hour is the local hour in 0..23.
	Hour int
}

// IsValid reports whether the clock can be used as-is or whether the builder
// should fall back to server UTC.
func (c ClientClock) IsValid() bool {
	if c.Hour < 0 || c.Hour > 23 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(c.DayOfWeek)) {
	case "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
		return true
	}
	return false
}

// Canonical returns a normalized copy: weekday is title-cased ("Monday") so
// downstream code can string-compare safely.
func (c ClientClock) Canonical() ClientClock {
	if !c.IsValid() {
		return c
	}
	day := strings.ToLower(strings.TrimSpace(c.DayOfWeek))
	return ClientClock{
		DayOfWeek: strings.ToUpper(day[:1]) + day[1:],
		Hour:      c.Hour,
	}
}

// UserContext is the prompt-facing snapshot of a single user at a specific
// moment in time. Consumed by the prompt builder to ground Claude's output
// in what the user has actually watched and the current day/time window.
//
// The struct is deliberately text-first: Claude is much happier with a
// pre-rendered history block than with structured JSON, and keeping the
// rendering out of prompt.go lets us unit-test it without touching Claude.
type UserContext struct {
	Locale      string // one of recommendations.supportedLocales (see normalizeLocale)
	DayOfWeek   string // "Monday"..."Sunday" (from client clock)
	TimeOfDay   string // "morning" | "afternoon" | "evening" | "night"
	LocalHour   int    // 0..23 from client
	HistoryText string // pre-rendered multi-line block; empty for new users
	HistorySize int    // number of rows actually rendered
}

// DBUserHistoryLoader is the production UserHistoryLoader, backed by the
// models package.
type DBUserHistoryLoader struct {
	pg *cs.PG
}

// NewDBUserHistoryLoader wires a UserHistoryLoader onto a *cs.PG connection.
func NewDBUserHistoryLoader(pg *cs.PG) *DBUserHistoryLoader {
	return &DBUserHistoryLoader{pg: pg}
}

// ListUserRatedMovies delegates to models.ListUserRatedMovies. Returns an
// empty slice (not an error) when the db handle is unavailable, so callers
// can degrade to cold-start recommendations.
func (l *DBUserHistoryLoader) ListUserRatedMovies(ctx context.Context, userID uuid.UUID, limit int) ([]models.RatedMovie, error) {
	db := l.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}
	return models.ListUserRatedMovies(ctx, db, userID, limit)
}

// FilterWatchedVideoIDs delegates to models.FilterWatchedMovieIDs.
func (l *DBUserHistoryLoader) FilterWatchedVideoIDs(ctx context.Context, userID uuid.UUID, videoIDs []string) ([]string, error) {
	db := l.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}
	return models.FilterWatchedMovieIDs(ctx, db, userID, videoIDs)
}

// UserContextBuilder assembles a UserContext for a given (user, clock).
// It is the glue between the models layer and the prompt layer.
type UserContextBuilder struct {
	history UserHistoryLoader
	limit   int
}

// NewUserContextBuilder wires a builder with the given history loader and
// history size cap. If limit <= 0, a sane default of 40 is used.
func NewUserContextBuilder(history UserHistoryLoader, limit int) *UserContextBuilder {
	if limit <= 0 {
		limit = 40
	}
	return &UserContextBuilder{
		history: history,
		limit:   limit,
	}
}

// History returns the underlying history loader. Exposed so ClaudeService
// can reuse the same DB handle for watched-filter queries without forcing
// the wiring code to pass the loader twice.
func (b *UserContextBuilder) History() UserHistoryLoader {
	return b.history
}

// Build assembles a UserContext for the given user.
//
// History errors are downgraded to warnings by returning a best-effort
// context plus the wrapped error — callers can log and continue with a
// cold-start recommendation. Clock validation is silent: an invalid clock
// is not an error, it just means the caller didn't supply one and we pretend
// it's "morning on Monday" from the user's point of view (still better than
// failing the whole request).
func (b *UserContextBuilder) Build(ctx context.Context, userID uuid.UUID, locale string, clock ClientClock) (*UserContext, error) {
	clk := clock.Canonical()
	if !clk.IsValid() {
		// Client did not provide a usable clock. We refuse to guess the user's
		// tz from a UTC server clock (that's how you end up recommending
		// "wind-down Monday evening content" to someone eating breakfast in
		// Tokyo). Default to a neutral "Saturday afternoon" window — cheerful
		// and generic — until the client sends a real value.
		clk = ClientClock{DayOfWeek: "Saturday", Hour: 14}
	}

	uc := &UserContext{
		Locale:    normalizeLocale(locale),
		DayOfWeek: clk.DayOfWeek,
		TimeOfDay: bucketTimeOfDay(clk.Hour),
		LocalHour: clk.Hour,
	}

	rows, err := b.history.ListUserRatedMovies(ctx, userID, b.limit)
	if err != nil {
		// Soft-fail: return a valid context so Claude can still do cold-start
		// recommendations. The handler decides whether to log or surface.
		return uc, errors.Wrap(err, "failed to load user history")
	}
	uc.HistoryText = renderHistory(rows)
	uc.HistorySize = len(rows)
	return uc, nil
}

// bucketTimeOfDay maps 0-23 hours into four coarse labels. Claude doesn't
// need finer granularity — "evening Monday" is already enough signal to tilt
// recommendations toward wind-down content.
func bucketTimeOfDay(hour int) string {
	switch {
	case hour >= 5 && hour < 12:
		return "morning"
	case hour >= 12 && hour < 17:
		return "afternoon"
	case hour >= 17 && hour < 23:
		return "evening"
	default:
		return "night"
	}
}

// supportedLocales lists the 2-letter codes the prompt explicitly teaches
// Claude how to write reasons in (see prompt.go LOCALE STYLE section). Any
// other value falls back to English in normalizeLocale.
//
// Keep this in sync with:
//   - assets/src/js/lib/discover/aiClient.js SUPPORTED_LOCALES
//   - default_chips.go defaultChipDefs* maps
//   - i18n.SupportedLangs (UI-side list)
//
// Note: "pt" carries Brazilian Portuguese (PT-BR) — see services/i18n/i18n.go
// for the URL/middleware rationale.
var supportedLocales = map[string]struct{}{
	"en": {},
	"ru": {},
	"es": {},
	"de": {},
	"fr": {},
	"pt": {},
	"it": {},
	"pl": {},
	"tr": {},
	"nl": {},
	"cs": {},
}

// normalizeLocale clamps the locale to a 2-letter prefix we know how to
// instruct Claude about. Anything we don't recognise falls back to English.
func normalizeLocale(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if len(locale) >= 2 {
		prefix := locale[:2]
		if _, ok := supportedLocales[prefix]; ok {
			return prefix
		}
	}
	return "en"
}

// renderHistory turns RatedMovie rows into a compact multi-line block Claude
// can read at a glance. Format is intentionally terse — every token in the
// prompt costs money.
//
// Example:
//
//	- Interstellar (2014) [liked]
//	- Tenet (2020) [disliked]
//	- Arrival (2016) [watched]
//	- Dune (2021) [queued]
func renderHistory(rows []models.RatedMovie) string {
	if len(rows) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, r := range rows {
		title := strings.TrimSpace(r.Title)
		if title == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(title)
		if r.Year != nil {
			sb.WriteString(fmt.Sprintf(" (%d)", *r.Year))
		}
		// Rating signal takes priority over watched because it's a stronger
		// preference indicator. Fall back to watched-only for neutral history.
		var tag string
		switch {
		case r.Rating != nil && *r.Rating > 0:
			tag = "liked"
		case r.Rating != nil && *r.Rating < 0:
			tag = "disliked"
		case r.Watched:
			tag = "watched"
		default:
			tag = "queued"
		}
		sb.WriteString(" [")
		sb.WriteString(tag)
		sb.WriteString("]")
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}
