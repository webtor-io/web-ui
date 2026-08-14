package models

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

// TorznabIndexer is a Torznab-speaking search endpoint (Jackett, Prowlarr,
// NZBHydra2, or a single tracker's own feed) bound to a user account. It is
// the Torznab counterpart of StremioAddonUrl: both are user-configured
// upstream sources that feed the same stream pipeline.
//
// The API key lives in its own column rather than inside Url even though
// Torznab passes it as a query parameter — the feed URL is echoed back into
// the profile UI, and splitting the key out is what lets us render the URL
// without leaking the credential into the page.
type TorznabIndexer struct {
	tableName struct{}  `pg:"torznab_indexer"`
	ID        uuid.UUID `pg:"torznab_indexer_id,pk,type:uuid,default:uuid_generate_v4()"`
	Url       string    `pg:"url,notnull"`
	ApiKey    *string   `pg:"api_key"`
	Priority  int16     `pg:"priority,notnull,default:1"`
	Enabled   bool      `pg:"enabled,notnull,default:true,use_zero"`
	CreatedAt time.Time
	UpdatedAt time.Time

	// Caps snapshot — captured on add and refreshed via /refresh-caps.
	// Nullable at the schema level, but the add handler refuses to store an
	// indexer whose probe failed, so rows created through the form always
	// carry it. The nil branches downstream (buildIDQuery, CategoriesFor)
	// exist for rows written any other way.
	Name          *string      `pg:"name"`
	Caps          *TorznabCaps `pg:"caps,type:jsonb"`
	CapsFetchedAt *time.Time   `pg:"caps_fetched_at"`

	UserID uuid.UUID `pg:"user_id"`
	User   *User     `pg:"rel:has-one,fk:user_id"`
}

// TorznabCaps is the subset of a `t=caps` response we persist. The params
// lists decide which query shape we send: an indexer that advertises
// imdbid is queried by id, everything else falls back to a title query.
type TorznabCaps struct {
	Server       string   `json:"server,omitempty"`
	SearchParams []string `json:"searchParams,omitempty"`
	MovieParams  []string `json:"movieParams,omitempty"`
	TVParams     []string `json:"tvParams,omitempty"`
	// Categories are the top-level category ids the indexer exposes. Used
	// to narrow queries to video categories when the indexer has them.
	Categories []int `json:"categories,omitempty"`
	// Limit is the indexer's max results per query (0 = unknown).
	Limit int `json:"limit,omitempty"`
}

// SupportsMovieIMDB reports whether t=movie accepts an imdbid parameter.
func (c *TorznabCaps) SupportsMovieIMDB() bool {
	return c != nil && containsParam(c.MovieParams, "imdbid")
}

// SupportsTVIMDB reports whether t=tvsearch accepts an imdbid parameter.
func (c *TorznabCaps) SupportsTVIMDB() bool {
	return c != nil && containsParam(c.TVParams, "imdbid")
}

// SupportsTVSearch reports whether t=tvsearch is available at all.
func (c *TorznabCaps) SupportsTVSearch() bool {
	return c != nil && len(c.TVParams) > 0
}

// SupportsMovieSearch reports whether t=movie is available at all.
func (c *TorznabCaps) SupportsMovieSearch() bool {
	return c != nil && len(c.MovieParams) > 0
}

// SupportsSeasonEpisode reports whether t=tvsearch accepts season/ep, which
// decides between structured episode queries and an "S01E05" title query.
func (c *TorznabCaps) SupportsSeasonEpisode() bool {
	return c != nil && containsParam(c.TVParams, "season") && containsParam(c.TVParams, "ep")
}

func containsParam(params []string, want string) bool {
	for _, p := range params {
		if p == want {
			return true
		}
	}
	return false
}

// ApplyCaps copies a caps snapshot into the model and stamps CapsFetchedAt.
func (i *TorznabIndexer) ApplyCaps(name string, caps *TorznabCaps) {
	now := time.Now()
	i.Name = strPtr(name)
	i.Caps = caps
	i.CapsFetchedAt = &now
}

// GetApiKey returns the stored API key or "" when the indexer needs none.
func (i *TorznabIndexer) GetApiKey() string {
	if i.ApiKey == nil {
		return ""
	}
	return *i.ApiKey
}

// GetName returns a human-readable label for the indexer.
//
// The caps probe alone is not enough to tell two indexers apart: Jackett
// reports `<server title="Jackett">` for every feed it serves, so a user
// with three Jackett indexers gets three rows all called "Jackett". What
// distinguishes them is the tracker id in the feed path, so it is appended
// when present.
func (i *TorznabIndexer) GetName() string {
	server := ""
	if i.Name != nil {
		server = strings.TrimSpace(*i.Name)
	}
	tracker := TrackerFromFeedURL(i.Url)
	switch {
	case server != "" && tracker != "":
		return server + " · " + tracker
	case server != "":
		return server
	case tracker != "":
		return tracker
	}
	if h := hostOfURL(i.Url); h != "" {
		return h
	}
	return i.Url
}

// TrackerFromFeedURL pulls the indexer id out of a feed URL:
//
//	Jackett  …/api/v2.0/indexers/rutracker-ru/results/torznab → rutracker-ru
//	Prowlarr …/api/v1/indexer/3/newznab                       → #3
//
// Jackett's "all" aggregate names no single tracker, so it yields "". A
// bare tracker feed has no id in its path either — the host is the label
// there.
func TrackerFromFeedURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range parts {
		if p != "indexers" && p != "indexer" {
			continue
		}
		if i+1 >= len(parts) {
			return ""
		}
		id := strings.TrimSpace(parts[i+1])
		if id == "" || strings.EqualFold(id, "all") {
			return ""
		}
		if isAllDigits(id) {
			// Prowlarr numbers its indexers; the number is meaningless on
			// its own but still tells two rows apart.
			return "#" + id
		}
		return id
	}
	return ""
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func hostOfURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// GetUserTorznabIndexers returns enabled indexers for a user, highest
// priority first — same ordering contract as GetUserStremioAddonUrls.
func GetUserTorznabIndexers(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]TorznabIndexer, error) {
	var indexers []TorznabIndexer
	err := db.Model(&indexers).
		Context(ctx).
		Where("user_id = ? AND enabled = ?", userID, true).
		Order("priority DESC").
		Select()
	if err != nil {
		return nil, err
	}
	return indexers, nil
}

// GetAllUserTorznabIndexers returns every indexer for a user, enabled or not.
func GetAllUserTorznabIndexers(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]TorznabIndexer, error) {
	var indexers []TorznabIndexer
	err := db.Model(&indexers).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("priority DESC").
		Select()
	if err != nil {
		return nil, err
	}
	return indexers, nil
}

// CountUserTorznabIndexers returns the number of indexers a user has.
func CountUserTorznabIndexers(ctx context.Context, db *pg.DB, userID uuid.UUID) (int, error) {
	return db.Model(&TorznabIndexer{}).
		Context(ctx).
		Where("user_id = ?", userID).
		Count()
}

// TorznabIndexerExists checks whether a user already bound this URL.
func TorznabIndexerExists(ctx context.Context, db *pg.DB, userID uuid.UUID, url string) (bool, error) {
	existing := &TorznabIndexer{}
	err := db.Model(existing).
		Context(ctx).
		Where("url = ? AND user_id = ?", url, userID).
		Select()
	if err == nil {
		return true, nil
	} else if errors.Is(err, pg.ErrNoRows) {
		return false, nil
	}
	return false, err
}

// GetTorznabIndexerByID retrieves an indexer by ID, or (nil, nil) when it
// does not exist.
func GetTorznabIndexerByID(ctx context.Context, db *pg.DB, indexerID uuid.UUID) (*TorznabIndexer, error) {
	indexer := &TorznabIndexer{}
	err := db.Model(indexer).
		Context(ctx).
		Where("torznab_indexer_id = ?", indexerID).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return indexer, nil
}

// CreateTorznabIndexer binds a new indexer to a user. New rows land at the
// bottom of the priority order, matching CreateStremioAddonUrl.
func CreateTorznabIndexer(ctx context.Context, db *pg.DB, userID uuid.UUID, url, apiKey, name string, caps *TorznabCaps) error {
	count, err := CountUserTorznabIndexers(ctx, db, userID)
	if err != nil {
		return err
	}

	indexer := &TorznabIndexer{
		Url:      url,
		ApiKey:   strPtr(apiKey),
		UserID:   userID,
		Priority: int16(count + 1),
		Enabled:  true,
	}
	if caps != nil || name != "" {
		indexer.ApplyCaps(name, caps)
	}

	_, err = db.Model(indexer).
		Context(ctx).
		Insert()
	return err
}

// UpdateTorznabIndexer persists changes to an existing indexer.
func UpdateTorznabIndexer(ctx context.Context, db *pg.DB, indexer *TorznabIndexer) error {
	_, err := db.Model(indexer).
		Context(ctx).
		WherePK().
		Update()
	return err
}

// UpdateTorznabIndexerCaps refreshes the caps snapshot for a single indexer
// owned by the given user. Returns pg.ErrNoRows when the indexer does not
// exist or belongs to someone else.
func UpdateTorznabIndexerCaps(ctx context.Context, db *pg.DB, indexerID, userID uuid.UUID, name string, caps *TorznabCaps) error {
	if caps == nil {
		return errors.New("caps are required")
	}
	now := time.Now()
	res, err := db.Model(&TorznabIndexer{}).
		Context(ctx).
		Set("name = ?", strPtr(name)).
		Set("caps = ?", jsonbValue(caps)).
		Set("caps_fetched_at = ?", now).
		Where("torznab_indexer_id = ? AND user_id = ?", indexerID, userID).
		Update()
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return pg.ErrNoRows
	}
	return nil
}

// DeleteUserTorznabIndexer deletes an indexer owned by a specific user.
func DeleteUserTorznabIndexer(ctx context.Context, db *pg.DB, indexerID, userID uuid.UUID) error {
	_, err := db.Model(&TorznabIndexer{}).
		Context(ctx).
		Where("torznab_indexer_id = ? AND user_id = ?", indexerID, userID).
		Delete()
	return err
}

// jsonbValue marshals a value as a JSONB literal for go-pg's Set builder.
// Same reasoning as jsonbStringSlice: passing the struct straight through
// leaves go-pg guessing at the column type.
func jsonbValue(v interface{}) interface{} {
	if v == nil {
		return json.RawMessage(nil)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(nil)
	}
	return json.RawMessage(b)
}
