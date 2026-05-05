// Package recommendations implements an AI-backed movie recommender that powers
// the Discover page.
//
// Design notes
//
// The service is intentionally agnostic about which metadata provider sits
// behind it. All lookups go through the MetadataLookup interface, which in
// production is satisfied by *enrich.Enricher — that way TMDB, OMDB and
// Kinopoisk fall through automatically and adding a new provider requires
// zero changes here.
//
// Claude is asked to return (title, year, reason) triples as plain-text
// NDJSON (one JSON object per line) so the response can be streamed
// token-by-token — see streamClaudeItemsText. The resolver validates each
// triple against MetadataLookup and drops anything it cannot turn into a
// real IMDB-keyed VideoMetadata entry, shielding the UI from hallucinated
// titles.
package recommendations

import (
	"context"
	"time"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

// Tier is a coarse subscription classification used only for quota routing.
// Other parts of the codebase rely on the richer claims.Data struct; the
// recommendations service only needs to know "does this user get the free
// allowance or the paid allowance", which maps to a single boolean.
type Tier int

const (
	TierFree Tier = iota
	TierPaid
)

func (t Tier) String() string {
	if t == TierPaid {
		return "paid"
	}
	return "free"
}

// Chip is a suggestion chip shown above the free-form query input. Each chip
// bundles a user-facing label with an expanded Query that is sent to the
// /recommend endpoint when the chip is tapped.
type Chip struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"` // single emoji or empty
	Query string `json:"query"`
}

// Recommendation is a single resolved card to render in the UI. VideoID is
// always an IMDB identifier (tt*). Claude-only items that can't be resolved
// into an IMDB id are dropped by the resolver.
type Recommendation struct {
	VideoID string  `json:"video_id"`
	Title   string  `json:"title"`
	Year    *int16  `json:"year,omitempty"`
	Poster  string  `json:"poster,omitempty"`
	Plot    string  `json:"plot,omitempty"`
	Rating  float64 `json:"rating,omitempty"`
	Reason  string  `json:"reason"` // ← the headline feature, per-card justification
	Type    string  `json:"type"`   // "movie" (MVP) — "series" reserved for follow-up
}

// Message is a single turn in a refine conversation. Role is either "user" or
// "assistant"; the assistant turn contains the JSON payload Claude returned
// the previous round so the next refine can be grounded in context.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RecommendRequest is the input to Service.Recommend. It carries enough
// context that the same method can serve both the initial /recommend call and
// a /refine follow-up (distinguished only by whether History is empty).
type RecommendRequest struct {
	UserID  uuid.UUID
	Tier    Tier
	Query   string
	History []Message
	Locale  string
	// Clock is the user's local wall-clock at the moment of the request.
	// Populated by the browser; see ClientClock docs. An invalid clock is
	// silently replaced with a neutral default.
	Clock ClientClock
}

// ChipsRequest is the input to Service.GenerateChips. Kept symmetric with
// RecommendRequest so handlers look the same.
type ChipsRequest struct {
	UserID       uuid.UUID
	Tier         Tier
	Locale       string
	Clock        ClientClock
	ForceRefresh bool
}

// --- Streaming primitives ---
//
// StreamEvent is a single message emitted by RecommendStream onto its
// caller-supplied channel. The handler layer turns each event into one
// SSE frame on the wire. Type is a short tag the client switches on; Data
// is the typed payload (one of the *Payload structs below) and is JSON-
// serialised verbatim.
type StreamEvent struct {
	Type string
	Data any
}

// PhaseStreamPayload announces a transition between pipeline stages so the
// UI can swap its loading copy. Expected is set on the "resolving" stage
// to tell the UI how many items it should expect at most.
type PhaseStreamPayload struct {
	Phase    string `json:"phase"`              // "claude" | "resolving" | "done"
	Expected int    `json:"expected,omitempty"` // populated for "resolving"
}

// DoneStreamPayload is the terminal event sent once the pipeline finishes
// successfully (or runs out of items to send). Carries quota / tier so the
// UI can hydrate the remaining-allowance counter without a second round
// trip. DailyQuota is the user's per-day cap for the current tier — sent
// alongside RemainingQuota so the UI can render "N / M" without a second
// lookup.
type DoneStreamPayload struct {
	Total          int    `json:"total"`
	RemainingQuota int    `json:"remaining_quota"`
	DailyQuota     int    `json:"daily_quota"`
	Tier           string `json:"tier"`
}

// ErrorStreamPayload is the terminal event sent when the pipeline fails.
// The Code is one of the same machine-readable tokens used by the JSON
// error envelope (quota_exceeded, empty_query, query_too_long, etc.).
//
// DailyQuota, ResetAt and UpgradeQuota are populated for the
// quota_exceeded code so the UI can render the full upgrade hint without
// a second lookup:
//   - DailyQuota   — current tier's daily cap, for the "0 / N" headline
//   - ResetAt      — unix-seconds the cap resets, for "resets in 10h 30m"
//   - UpgradeQuota — paid tier's daily cap, only set when the current
//     tier is free (zero means "no upgrade hint", which is what we
//     want for paid users hitting their own anti-abuse cap)
type ErrorStreamPayload struct {
	Code         string `json:"code"`
	Tier         string `json:"tier,omitempty"`
	DailyQuota   int    `json:"daily_quota,omitempty"`
	UpgradeQuota int    `json:"upgrade_quota,omitempty"`
	ResetAt      int64  `json:"reset_at,omitempty"`
}

// ChipsResponse wraps a generated chip list. GeneratedAt is unix seconds,
// used by the client to decide when to refresh chips locally without a
// network round trip.
type ChipsResponse struct {
	Chips       []Chip `json:"chips"`
	GeneratedAt int64  `json:"generated_at"`
	Tier        string `json:"tier"`
}

// Service is the only surface handlers and tests need. Implementations are
// wired in claude.go (production) and via fakes in tests.
type Service interface {
	// GenerateChips returns a list of recommendation chips tailored to the
	// given user's history and the current moment in time. Results are
	// cached in a distributed (Redis) cache — see Config.ChipsTTLSeconds.
	//
	// This call does NOT consume quota on a cache hit, and does not consume
	// quota on a cache miss either — chip generation is the first thing a
	// user sees, and we don't want to burn a free-tier user's only daily
	// request just by opening Discover. Explicit /chips/refresh does consume
	// a unit via ConsumeQuota below.
	GenerateChips(ctx context.Context, req ChipsRequest) (*ChipsResponse, error)

	// RecommendStream turns a natural-language request into a stream of
	// resolved movie cards. Claude is given the user's watch history as
	// context, and each fully-formed recommendation is pushed onto
	// `events` as soon as the resolver finishes its TMDB lookup — phase
	// transitions, individual items, and a terminal "done" or "error"
	// event. The implementation closes `events` before returning, so the
	// SSE handler can simply range over it. Quota is consumed exactly
	// once, atomically, before the first phase event is emitted.
	RecommendStream(ctx context.Context, req RecommendRequest, events chan<- StreamEvent)

	// Remaining returns how many quota units the user has left today
	// without mutating state. Used by read-only endpoints (GET /chips) so
	// the UI can display "N left today".
	Remaining(ctx context.Context, userID uuid.UUID, tier Tier) (int, error)

	// DailyQuota returns the per-day request cap for the given tier.
	// Pure config lookup, no I/O. The UI uses it to render "N / M left"
	// instead of just "N left".
	DailyQuota(tier Tier) int

	// QuotaResetAt returns the unix timestamp (seconds) at which the
	// user's daily quota next rolls over. The UI uses this on the
	// quota-exceeded screen to display "resets in 10h 30m".
	QuotaResetAt() int64

	// ConsumeQuota atomically charges a unit. Used by /chips/refresh, which
	// is an explicit paid action. Returns ErrQuotaExceeded if the user is
	// already at their daily limit.
	ConsumeQuota(ctx context.Context, userID uuid.UUID, tier Tier) (int, error)
}

// MetadataLookup is the minimum metadata-provider interface the resolver needs.
// In production it is satisfied by *enrich.Enricher via LookupByTitleYear —
// keeping the resolver agnostic about TMDB/OMDB/Kinopoisk ordering.
type MetadataLookup interface {
	LookupByTitleYear(ctx context.Context, title string, year *int16, ct models.ContentType) (*models.VideoMetadata, error)
}

// ContentLocalizer is an optional capability for localizing metadata (title,
// plot) into the user's language. In production it is satisfied by
// *enrich.Enricher. If nil, the resolver skips localization.
type ContentLocalizer interface {
	Localize(ctx context.Context, md *models.VideoMetadata, lang string)
}

// WatchlistTitle is the prompt-shaped projection of a watchlist row — just
// enough fields for Claude to ground its picks. Loaded by ListUserWatchlist
// alongside the rated-history block so the model sees both what the user has
// watched and what they have explicitly bookmarked.
type WatchlistTitle struct {
	VideoID string
	Title   string
	Year    *int16
	// Type is "movie" or "series" — Claude treats a saved series differently
	// (cue similar shows, not similar films).
	Type string
}

// UserHistoryLoader loads the "what has this user watched and rated" picture
// used to ground Claude's output. Separated from the models package so tests
// can inject fixtures without touching a real Postgres.
type UserHistoryLoader interface {
	// ListUserRatedMovies returns the user's recent watch/rate history for
	// prompt grounding.
	ListUserRatedMovies(ctx context.Context, userID uuid.UUID, limit int) ([]models.RatedMovie, error)
	// ListUserWatchlist returns the user's recent watchlist entries (movies
	// + series merged, newest first), capped at limit. Surfaced to Claude as
	// a strong taste signal: titles the user explicitly bookmarked.
	ListUserWatchlist(ctx context.Context, userID uuid.UUID, limit int) ([]WatchlistTitle, error)
	// FilterWatchedVideoIDs returns the subset of the given video_ids that
	// the user has already marked as watched. Used to hide already-seen
	// titles from AI recommendations — Claude is asked to do this in the
	// prompt, but we don't trust the model to respect it perfectly.
	FilterWatchedVideoIDs(ctx context.Context, userID uuid.UUID, videoIDs []string) ([]string, error)
	// FilterWatchlistVideoIDs returns the subset of the given video_ids that
	// are already in the user's watchlist (movies + series). Mirror of the
	// watched filter — the user already knows about a bookmarked title, so
	// recommending it is wasted real estate.
	FilterWatchlistVideoIDs(ctx context.Context, userID uuid.UUID, videoIDs []string) ([]string, error)
}

// Quota guards spend against the Anthropic API. Implementations live in
// quota.go (Redis) and tests inject fakes.
type Quota interface {
	// Consume increments the user's daily counter and returns the remaining
	// allowance. If the user is already at or above their limit, it returns
	// ErrQuotaExceeded and does not modify state.
	Consume(ctx context.Context, userID uuid.UUID, tier Tier) (remaining int, err error)

	// Remaining reports how many requests the user has left today without
	// mutating state. Used by read-only endpoints like /chips that must not
	// spend quota themselves.
	Remaining(ctx context.Context, userID uuid.UUID, tier Tier) (int, error)

	// ResetAt returns the wall-clock instant the user's quota will next
	// roll over. Pure config / clock lookup, no I/O. Exposed via
	// Service.QuotaResetAt so the UI can render "resets in 10h 30m".
	ResetAt() time.Time
}

// Sentinel errors returned by Service implementations so handlers can map
// them to HTTP status codes without reaching for reflection.
//
// Note: there is no ErrFeatureDisabled. The disabled state is signalled by
// rec.New returning a nil Service before any handler is registered, so
// handlers never see this case at runtime.
var (
	// ErrQuotaExceeded means the user has used their daily allowance.
	// Handlers return 402 (Payment Required) with a body describing the
	// tier and reset time so the UI can show an upgrade prompt.
	ErrQuotaExceeded = errors.New("ai recommendations quota exceeded")

	// ErrQueryTooLong means the user-provided query exceeds MaxQueryLength.
	// Handlers return 400.
	ErrQueryTooLong = errors.New("query is too long")

	// ErrEmptyQuery means the recommend handler was called without any
	// query text. Handlers return 400.
	ErrEmptyQuery = errors.New("query is empty")

	// ErrNoChips means Claude returned an empty / unusable chip list. The
	// chips path is the only place this is raised; on the recommend path
	// "0 items" is not an error — RecommendStream just emits a "done"
	// with Total=0 and the UI shows its empty state.
	ErrNoChips = errors.New("no chips available")
)
