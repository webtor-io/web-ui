// Package data_export implements the GDPR Art. 20 "right to data portability"
// payload: a JSON dump of every user-keyed row we hold for a single account.
//
// The shape is stable and documented in docs/data_export.md — clients that
// re-import the dump (or that read it programmatically) rely on field names.
// Bump SchemaVersion when changing the wire format.
package data_export

import (
	"context"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	vaultmodels "github.com/webtor-io/web-ui/models/vault"
)

// SchemaVersion identifies the wire format of an Export. Increment whenever
// the JSON shape changes in a way that breaks consumers.
const SchemaVersion = 1

// Export is the full per-user dump. Field tags drive the JSON output.
type Export struct {
	SchemaVersion     int                  `json:"schema_version"`
	GeneratedAt       time.Time            `json:"generated_at"`
	User              UserData             `json:"user"`
	Library           []LibraryItem        `json:"library"`
	WatchHistory      []WatchHistoryItem   `json:"watch_history"`
	MovieStatuses     []MovieStatusItem    `json:"movie_statuses"`
	SeriesStatuses    []SeriesStatusItem   `json:"series_statuses"`
	EpisodeStatuses   []EpisodeStatusItem  `json:"episode_statuses"`
	MovieWatchlist    []WatchlistItem      `json:"movie_watchlist"`
	SeriesWatchlist   []WatchlistItem      `json:"series_watchlist"`
	StremioAddonURLs  []StremioAddonItem   `json:"stremio_addon_urls"`
	StremioSettings   *StremioSettings     `json:"stremio_settings,omitempty"`
	EmbedDomains      []EmbedDomainItem    `json:"embed_domains"`
	StreamingBackends []StreamingBackend   `json:"streaming_backends"`
	UserSubtitles     []UserSubtitleItem   `json:"user_subtitles"`
	AccessTokens      []AccessTokenItem    `json:"access_tokens"`
	Vault             *VaultData           `json:"vault,omitempty"`
}

type UserData struct {
	UserID        uuid.UUID `json:"user_id"`
	Email         string    `json:"email"`
	PatreonUserID *string   `json:"patreon_user_id,omitempty"`
	Tier          string    `json:"tier"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type LibraryItem struct {
	ResourceID  string    `json:"resource_id"`
	Name        string    `json:"name,omitempty"`
	TorrentName string    `json:"torrent_name,omitempty"`
	SizeBytes   int64     `json:"size_bytes,omitempty"`
	FileCount   int       `json:"file_count,omitempty"`
	AddedAt     time.Time `json:"added_at"`
}

type WatchHistoryItem struct {
	ResourceID string    `json:"resource_id"`
	Path       string    `json:"path"`
	Position   float32   `json:"position_seconds"`
	Duration   float32   `json:"duration_seconds"`
	Watched    bool      `json:"watched"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type MovieStatusItem struct {
	VideoID   string     `json:"video_id"`
	Watched   bool       `json:"watched"`
	Rating    *int16     `json:"rating,omitempty"`
	Source    string     `json:"source"`
	WatchedAt *time.Time `json:"watched_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type SeriesStatusItem struct {
	VideoID   string     `json:"video_id"`
	Watched   bool       `json:"watched"`
	Rating    *int16     `json:"rating,omitempty"`
	Source    string     `json:"source"`
	WatchedAt *time.Time `json:"watched_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type EpisodeStatusItem struct {
	VideoID   string     `json:"video_id"`
	Season    int16      `json:"season"`
	Episode   int16      `json:"episode"`
	Watched   bool       `json:"watched"`
	Rating    *int16     `json:"rating,omitempty"`
	Source    string     `json:"source"`
	WatchedAt *time.Time `json:"watched_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type WatchlistItem struct {
	VideoID   string `json:"video_id"`
	Type      string `json:"type"`
	Title     string `json:"title,omitempty"`
	Year      *int16 `json:"year,omitempty"`
	Rating    *float64 `json:"rating,omitempty"`
	Source    string `json:"source,omitempty"`
	CreatedAt int64  `json:"created_at_unix"`
}

type StremioAddonItem struct {
	URL             string     `json:"url"`
	Priority        int16      `json:"priority"`
	Enabled         bool       `json:"enabled"`
	ManifestID      *string    `json:"manifest_id,omitempty"`
	ManifestName    *string    `json:"manifest_name,omitempty"`
	ManifestVersion *string    `json:"manifest_version,omitempty"`
	ManifestFetched *time.Time `json:"manifest_fetched_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type StremioSettings struct {
	PreferredResolutions []models.ResolutionSetting `json:"preferred_resolutions"`
	DiscoverOnly         bool                       `json:"discover_only"`
	PreferredLanguage    string                     `json:"preferred_language,omitempty"`
	UpdatedAt            time.Time                  `json:"updated_at"`
}

type EmbedDomainItem struct {
	Domain    string    `json:"domain"`
	Ads       bool      `json:"ads"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// StreamingBackend mirrors the user-provided RealDebrid/Torbox config.
// AccessToken is included because the user supplied it and can see it on
// the profile page; the export is delivered over an authenticated session.
type StreamingBackend struct {
	Type        string                              `json:"type"`
	AccessToken string                              `json:"access_token,omitempty"`
	Config      models.StreamingBackendConfig       `json:"config,omitempty"`
	Priority    int16                               `json:"priority"`
	Enabled     bool                                `json:"enabled"`
	LastStatus  *models.StreamingBackendStatus      `json:"last_status,omitempty"`
	LastChecked *time.Time                          `json:"last_checked_at,omitempty"`
	CreatedAt   time.Time                           `json:"created_at"`
	UpdatedAt   time.Time                           `json:"updated_at"`
}

type UserSubtitleItem struct {
	ResourceID   string    `json:"resource_id"`
	Path         string    `json:"path"`
	Hash         string    `json:"hash"`
	OriginalName string    `json:"original_name"`
	Format       string    `json:"format"`
	Size         int64     `json:"size"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AccessTokenItem describes one Webtor-issued access token (Stremio, WebDAV).
// The raw token value is included so users can re-create their addon URLs;
// the export is delivered over an authenticated session.
type AccessTokenItem struct {
	Name      string     `json:"name"`
	Token     uuid.UUID  `json:"token"`
	Scope     []string   `json:"scope,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type VaultData struct {
	Balance     *float64       `json:"balance,omitempty"`
	UpdatedAt   *time.Time     `json:"updated_at,omitempty"`
	Pledges     []VaultPledge  `json:"pledges"`
	Transactions []VaultTxLog  `json:"transactions"`
}

type VaultPledge struct {
	ResourceID string    `json:"resource_id"`
	Amount     float64   `json:"amount"`
	Funded     bool      `json:"funded"`
	FrozenAt   time.Time `json:"frozen_at"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type VaultTxLog struct {
	ResourceID *string   `json:"resource_id,omitempty"`
	Balance    float64   `json:"balance"`
	OpType     string    `json:"op_type"`
	CreatedAt  time.Time `json:"created_at"`
}

// Build assembles a fresh Export for the given user. Pure function — takes
// the *pg.DB the handler already has and returns the populated struct ready
// for json.Marshal. Errors are wrapped per CLAUDE.md guidance: only the
// top-level handler should log.
func Build(ctx context.Context, db *pg.DB, u *models.User) (*Export, error) {
	if u == nil {
		return nil, errors.New("nil user")
	}

	exp := &Export{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		User: UserData{
			UserID:        u.UserID,
			Email:         u.Email,
			PatreonUserID: u.PatreonUserID,
			Tier:          u.Tier,
			CreatedAt:     u.CreatedAt,
			UpdatedAt:     u.UpdatedAt,
		},
	}

	if err := exp.fillLibrary(ctx, db, u.UserID); err != nil {
		return nil, err
	}
	if err := exp.fillWatchHistory(ctx, db, u.UserID); err != nil {
		return nil, err
	}
	if err := exp.fillStatuses(ctx, db, u.UserID); err != nil {
		return nil, err
	}
	if err := exp.fillWatchlists(ctx, db, u.UserID); err != nil {
		return nil, err
	}
	if err := exp.fillStremio(ctx, db, u.UserID); err != nil {
		return nil, err
	}
	if err := exp.fillEmbedAndBackends(ctx, db, u.UserID); err != nil {
		return nil, err
	}
	if err := exp.fillSubtitlesAndTokens(ctx, db, u.UserID); err != nil {
		return nil, err
	}
	if err := exp.fillVault(ctx, db, u.UserID); err != nil {
		return nil, err
	}
	return exp, nil
}

func (e *Export) fillLibrary(ctx context.Context, db *pg.DB, uID uuid.UUID) error {
	libs, err := models.GetLibraryTorrentsList(ctx, db, uID, models.SortTypeRecentlyAdded)
	if err != nil {
		return errors.Wrap(err, "failed to load library")
	}
	e.Library = make([]LibraryItem, 0, len(libs))
	for _, l := range libs {
		item := LibraryItem{
			ResourceID: l.ResourceID,
			Name:       l.Name,
			AddedAt:    l.CreatedAt,
		}
		if l.Torrent != nil {
			item.TorrentName = l.Torrent.Name
			item.SizeBytes = l.Torrent.SizeBytes
			item.FileCount = l.Torrent.FileCount
		}
		e.Library = append(e.Library, item)
	}
	return nil
}

func (e *Export) fillWatchHistory(ctx context.Context, db *pg.DB, uID uuid.UUID) error {
	rows, err := models.ListAllWatchHistory(ctx, db, uID)
	if err != nil {
		return err
	}
	e.WatchHistory = make([]WatchHistoryItem, 0, len(rows))
	for _, w := range rows {
		e.WatchHistory = append(e.WatchHistory, WatchHistoryItem{
			ResourceID: w.ResourceID,
			Path:       w.Path,
			Position:   w.Position,
			Duration:   w.Duration,
			Watched:    w.Watched,
			CreatedAt:  w.CreatedAt,
			UpdatedAt:  w.UpdatedAt,
		})
	}
	return nil
}

func (e *Export) fillStatuses(ctx context.Context, db *pg.DB, uID uuid.UUID) error {
	movies, err := models.ListAllMovieStatuses(ctx, db, uID)
	if err != nil {
		return err
	}
	e.MovieStatuses = make([]MovieStatusItem, 0, len(movies))
	for _, m := range movies {
		e.MovieStatuses = append(e.MovieStatuses, MovieStatusItem{
			VideoID:   m.VideoID,
			Watched:   m.Watched,
			Rating:    m.Rating,
			Source:    m.Source.String(),
			WatchedAt: m.WatchedAt,
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
		})
	}

	series, err := models.ListAllSeriesStatuses(ctx, db, uID)
	if err != nil {
		return err
	}
	e.SeriesStatuses = make([]SeriesStatusItem, 0, len(series))
	for _, s := range series {
		e.SeriesStatuses = append(e.SeriesStatuses, SeriesStatusItem{
			VideoID:   s.VideoID,
			Watched:   s.Watched,
			Rating:    s.Rating,
			Source:    s.Source.String(),
			WatchedAt: s.WatchedAt,
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
		})
	}

	episodes, err := models.ListAllEpisodeStatuses(ctx, db, uID)
	if err != nil {
		return err
	}
	e.EpisodeStatuses = make([]EpisodeStatusItem, 0, len(episodes))
	for _, ep := range episodes {
		e.EpisodeStatuses = append(e.EpisodeStatuses, EpisodeStatusItem{
			VideoID:   ep.VideoID,
			Season:    ep.Season,
			Episode:   ep.Episode,
			Watched:   ep.Watched,
			Rating:    ep.Rating,
			Source:    ep.Source.String(),
			WatchedAt: ep.WatchedAt,
			CreatedAt: ep.CreatedAt,
			UpdatedAt: ep.UpdatedAt,
		})
	}
	return nil
}

func (e *Export) fillWatchlists(ctx context.Context, db *pg.DB, uID uuid.UUID) error {
	movies, err := models.ListMovieWatchlistItems(ctx, db, uID)
	if err != nil {
		return errors.Wrap(err, "failed to load movie watchlist")
	}
	e.MovieWatchlist = make([]WatchlistItem, 0, len(movies))
	for _, m := range movies {
		e.MovieWatchlist = append(e.MovieWatchlist, WatchlistItem{
			VideoID:   m.VideoID,
			Type:      m.Type,
			Title:     m.Title,
			Year:      m.Year,
			Rating:    m.Rating,
			Source:    m.Source,
			CreatedAt: m.CreatedAt,
		})
	}

	seriesList, err := models.ListSeriesWatchlistItems(ctx, db, uID)
	if err != nil {
		return errors.Wrap(err, "failed to load series watchlist")
	}
	e.SeriesWatchlist = make([]WatchlistItem, 0, len(seriesList))
	for _, s := range seriesList {
		e.SeriesWatchlist = append(e.SeriesWatchlist, WatchlistItem{
			VideoID:   s.VideoID,
			Type:      s.Type,
			Title:     s.Title,
			Year:      s.Year,
			Rating:    s.Rating,
			Source:    s.Source,
			CreatedAt: s.CreatedAt,
		})
	}
	return nil
}

func (e *Export) fillStremio(ctx context.Context, db *pg.DB, uID uuid.UUID) error {
	addons, err := models.GetAllUserStremioAddonUrls(ctx, db, uID)
	if err != nil {
		return errors.Wrap(err, "failed to load stremio addon urls")
	}
	e.StremioAddonURLs = make([]StremioAddonItem, 0, len(addons))
	for _, a := range addons {
		e.StremioAddonURLs = append(e.StremioAddonURLs, StremioAddonItem{
			URL:             a.Url,
			Priority:        a.Priority,
			Enabled:         a.Enabled,
			ManifestID:      a.ManifestID,
			ManifestName:    a.Name,
			ManifestVersion: a.ManifestVersion,
			ManifestFetched: a.ManifestFetchedAt,
			CreatedAt:       a.CreatedAt,
			UpdatedAt:       a.UpdatedAt,
		})
	}

	s, err := models.GetUserStremioSettings(ctx, db, uID)
	if err != nil {
		return errors.Wrap(err, "failed to load stremio settings")
	}
	if s != nil && s.Settings != nil {
		e.StremioSettings = &StremioSettings{
			PreferredResolutions: s.Settings.PreferredResolutions,
			DiscoverOnly:         s.Settings.DiscoverOnly,
			PreferredLanguage:    s.Settings.PreferredLanguage,
			UpdatedAt:            s.UpdatedAt,
		}
	}
	return nil
}

func (e *Export) fillEmbedAndBackends(ctx context.Context, db *pg.DB, uID uuid.UUID) error {
	domains, err := models.GetUserDomains(ctx, db, uID)
	if err != nil {
		return errors.Wrap(err, "failed to load embed domains")
	}
	e.EmbedDomains = make([]EmbedDomainItem, 0, len(domains))
	for _, d := range domains {
		ads := false
		if d.Ads != nil {
			ads = *d.Ads
		}
		e.EmbedDomains = append(e.EmbedDomains, EmbedDomainItem{
			Domain:    d.Domain,
			Ads:       ads,
			CreatedAt: d.CreatedAt,
			UpdatedAt: d.UpdatedAt,
		})
	}

	backends, err := models.GetUserStreamingBackends(ctx, db, uID)
	if err != nil {
		return errors.Wrap(err, "failed to load streaming backends")
	}
	e.StreamingBackends = make([]StreamingBackend, 0, len(backends))
	for _, b := range backends {
		e.StreamingBackends = append(e.StreamingBackends, StreamingBackend{
			Type:        string(b.Type),
			AccessToken: b.AccessToken,
			Config:      b.Config,
			Priority:    b.Priority,
			Enabled:     b.Enabled,
			LastStatus:  b.LastStatus,
			LastChecked: b.LastCheckedAt,
			CreatedAt:   b.CreatedAt,
			UpdatedAt:   b.UpdatedAt,
		})
	}
	return nil
}

func (e *Export) fillSubtitlesAndTokens(ctx context.Context, db *pg.DB, uID uuid.UUID) error {
	subs, err := models.ListAllUserSubtitles(ctx, db, uID)
	if err != nil {
		return err
	}
	e.UserSubtitles = make([]UserSubtitleItem, 0, len(subs))
	for _, s := range subs {
		e.UserSubtitles = append(e.UserSubtitles, UserSubtitleItem{
			ResourceID:   s.ResourceID,
			Path:         s.Path,
			Hash:         s.Hash,
			OriginalName: s.OriginalName,
			Format:       s.Format,
			Size:         s.Size,
			CreatedAt:    s.CreatedAt,
			UpdatedAt:    s.UpdatedAt,
		})
	}

	tokens, err := models.ListUserAccessTokens(ctx, db, uID)
	if err != nil {
		return errors.Wrap(err, "failed to load access tokens")
	}
	e.AccessTokens = make([]AccessTokenItem, 0, len(tokens))
	for _, t := range tokens {
		e.AccessTokens = append(e.AccessTokens, AccessTokenItem{
			Name:      t.Name,
			Token:     t.Token,
			Scope:     t.Scope,
			ExpiresAt: t.ExpiresAt,
			CreatedAt: t.CreatedAt,
		})
	}
	return nil
}

func (e *Export) fillVault(ctx context.Context, db *pg.DB, uID uuid.UUID) error {
	vp, err := vaultmodels.GetUserVP(ctx, db, uID)
	if err != nil {
		return errors.Wrap(err, "failed to load vault balance")
	}
	pledges, err := vaultmodels.GetUserPledges(ctx, db, uID)
	if err != nil {
		return errors.Wrap(err, "failed to load vault pledges")
	}
	logs, err := vaultmodels.ListUserTxLogs(ctx, db, uID)
	if err != nil {
		return err
	}
	if vp == nil && len(pledges) == 0 && len(logs) == 0 {
		return nil
	}

	v := &VaultData{
		Pledges:      make([]VaultPledge, 0, len(pledges)),
		Transactions: make([]VaultTxLog, 0, len(logs)),
	}
	if vp != nil {
		v.Balance = vp.Total
		t := vp.UpdatedAt
		v.UpdatedAt = &t
	}
	for _, p := range pledges {
		v.Pledges = append(v.Pledges, VaultPledge{
			ResourceID: p.ResourceID,
			Amount:     p.Amount,
			Funded:     p.Funded,
			FrozenAt:   p.FrozenAt,
			CreatedAt:  p.CreatedAt,
			UpdatedAt:  p.UpdatedAt,
		})
	}
	for _, l := range logs {
		v.Transactions = append(v.Transactions, VaultTxLog{
			ResourceID: l.ResourceID,
			Balance:    l.Balance,
			OpType:     opTypeName(l.OpType),
			CreatedAt:  l.CreatedAt,
		})
	}
	e.Vault = v
	return nil
}

func opTypeName(t int16) string {
	switch t {
	case vaultmodels.OpTypeChangeTier:
		return "change_tier"
	case vaultmodels.OpTypeFund:
		return "fund"
	case vaultmodels.OpTypeClaim:
		return "claim"
	case vaultmodels.OpTypeAbuseRefund:
		return "abuse_refund"
	default:
		return "unknown"
	}
}
