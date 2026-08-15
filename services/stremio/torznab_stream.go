package stremio

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/models"
	ptn "github.com/webtor-io/web-ui/services/parse_torrent_name"
	tn "github.com/webtor-io/web-ui/services/torznab"
)

// hashResolveConcurrency bounds the .torrent downloads a single query can
// trigger. Results that already carry an infohash or a magnet cost nothing;
// this only limits the private-tracker case where the hash lives inside the
// torrent file.
const hashResolveConcurrency = 8

// maxHashDownloads bounds how many results in one query may need a .torrent
// download to yield their infohash. Results carrying an infohash or a magnet
// are free and are never counted here; without the bound, a feed that omits
// both turns one stream request into MaxResults downloads of up to 8 MiB
// each, from an address the feed chooses.
const maxHashDownloads = 8

// TorznabStream turns one user-configured Torznab indexer into a
// StreamsService, so indexer results enter the exact same pipeline as
// Stremio addon results: dedup by infohash, resolution preferences,
// language filter, availability marker and the /stremio/resolve URL.
//
// Everything Torznab-specific stops here. Downstream layers only ever see
// StreamItems.
type TorznabStream struct {
	cl      *tn.Client
	indexer models.TorznabIndexer
	titles  tn.TitleResolver
	cache   *lazymap.LazyMap[*StreamsResponse]
	names   trackerNameStore
}

// trackerNameStore persists the display name an indexer's own results carry.
// An interface so the stream layer keeps its single DB touch behind one
// method, and so tests need no database.
type trackerNameStore interface {
	SetTrackerName(ctx context.Context, indexerID uuid.UUID, name string) error
}

// pgTrackerNames is the production trackerNameStore.
type pgTrackerNames struct{ db *pg.DB }

func (s pgTrackerNames) SetTrackerName(ctx context.Context, indexerID uuid.UUID, name string) error {
	return models.SetTorznabIndexerTrackerName(ctx, s.db, indexerID, name)
}

var _ StreamsService = (*TorznabStream)(nil)
var _ TimeoutedService = (*TorznabStream)(nil)

func NewTorznabStream(cl *tn.Client, indexer models.TorznabIndexer, titles tn.TitleResolver, cache *lazymap.LazyMap[*StreamsResponse], names trackerNameStore) *TorznabStream {
	return &TorznabStream{
		cl:      cl,
		indexer: indexer,
		titles:  titles,
		cache:   cache,
		names:   names,
	}
}

func (s *TorznabStream) GetName() string {
	return fmt.Sprintf("TorznabStream (%v)", s.indexer.GetName())
}

// GetTimeout gives Torznab searches a longer budget than the 5s addons get.
// A Jackett endpoint pointed at "all" indexers queries every configured
// tracker before answering, and cutting that at 5s drops the whole indexer
// from the results far more often than not.
func (s *TorznabStream) GetTimeout() time.Duration {
	return s.cl.Timeout()
}

func (s *TorznabStream) GetStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	cacheKey := fmt.Sprintf("torznab_%s_%s_%s", s.indexer.ID, contentType, contentID)
	return s.cache.Get(cacheKey, func() (*StreamsResponse, error) {
		// Detached from this caller's cancellation on purpose: the result
		// is shared with everyone waiting on the same key, so a user who
		// closes the Discover modal must not fail the /stremio/stream
		// request that arrived a moment later. The client's own budget
		// still bounds the work.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cl.Timeout())
		defer cancel()
		return s.fetchStreams(fetchCtx, contentType, contentID)
	})
}

func (s *TorznabStream) fetchStreams(ctx context.Context, contentType, contentID string) (*StreamsResponse, error) {
	imdbID, season, episode := parseContentID(contentID)
	if imdbID == "" {
		return &StreamsResponse{Streams: []StreamItem{}}, nil
	}

	ep := tn.Endpoint{
		URL:    s.indexer.Url,
		APIKey: s.indexer.GetApiKey(),
		Name:   s.indexer.GetName(),
	}
	cats := tn.CategoriesFor(s.indexer.Caps, contentType)

	// Queries are attempts, not a fan-out. The title query is only built
	// when the id query came back empty — building it eagerly would put a
	// metadata lookup on the hot path of every single stream request, even
	// for indexers that answer by id perfectly well.
	var results []tn.Result
	var lastErr error

	if q := s.buildIDQuery(contentType, imdbID, season, episode); q != nil {
		q.Cats = cats
		r, err := s.cl.Search(ctx, ep, *q)
		if err != nil {
			lastErr = err
		}
		results = r
	}

	if len(results) == 0 {
		if q := s.buildTitleQuery(ctx, contentType, imdbID, season, episode); q != nil {
			q.Cats = cats
			r, err := s.cl.Search(ctx, ep, *q)
			if err != nil {
				lastErr = err
			}
			results = r
		}
	}
	if len(results) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return &StreamsResponse{Streams: []StreamItem{}}, nil
	}

	s.learnTrackerName(ctx, results)

	if season != nil {
		results = filterBySeason(results, *season, s.indexer.GetName())
		if len(results) == 0 {
			return &StreamsResponse{Streams: []StreamItem{}}, nil
		}
	}

	return &StreamsResponse{Streams: s.toStreamItems(ctx, results)}, nil
}

// learnTrackerName records the name a feed gives itself, so the profile row,
// the Discover chip and the stream label all read "RuTracker.org" rather than
// the "rutracker" slug the feed URL happens to carry.
//
// It has to be learned here because there is nowhere earlier to learn it: a
// caps probe answers <server title="Jackett"/> for every feed Jackett serves,
// while every search result carries the real name in <jackettindexer>.
//
// Only when the whole page agrees on one name. A Jackett endpoint pointed at
// "all" fans out to every tracker it has, and naming the indexer after
// whichever answered first would be wrong — those keep the server-title
// label, and their rows are named per result instead.
//
// Writes at most once per indexer: the stored value is compared first.
func (s *TorznabStream) learnTrackerName(ctx context.Context, results []tn.Result) {
	if s.names == nil || len(results) == 0 {
		return
	}
	name := ""
	for _, r := range results {
		t := strings.TrimSpace(r.Tracker)
		if t == "" {
			return
		}
		if name == "" {
			name = t
			continue
		}
		if !strings.EqualFold(t, name) {
			return
		}
	}
	if name == s.indexer.GetTrackerName() {
		return
	}
	if err := s.names.SetTrackerName(ctx, s.indexer.ID, name); err != nil {
		log.WithError(err).
			WithField("indexer_id", s.indexer.ID).
			Warn("failed to store torznab tracker name")
	}
}

// filterBySeason drops results that name a season other than the one asked
// for. An indexer advertising season/ep in its caps does not necessarily
// honour them — several Jackett definitions degrade to a plain keyword
// search — and the user then gets season one of a series they opened at
// season three.
func filterBySeason(results []tn.Result, season int, indexer string) []tn.Result {
	out := make([]tn.Result, 0, len(results))
	dropped := 0
	for _, r := range results {
		if matchesRequestedSeason(r.Title, season) {
			out = append(out, r)
			continue
		}
		dropped++
	}
	if dropped > 0 {
		log.WithField("indexer", indexer).
			WithField("season", season).
			WithField("dropped", dropped).
			Debug("dropped torznab results naming another season")
	}
	return out
}

// buildIDQuery returns the query-by-imdb-id attempt, or nil when the
// indexer cannot serve one. It is preferred whenever available: it is exact
// and needs no metadata lookup. A nil caps snapshot means the probe never
// succeeded, in which case trying the id query costs one request and can
// save the fallback entirely.
func (s *TorznabStream) buildIDQuery(contentType, imdbID string, season, episode *int) *tn.Query {
	caps := s.indexer.Caps
	if isSeriesQuery(contentType, season, episode) {
		if caps == nil || (caps.SupportsTVIMDB() && caps.SupportsSeasonEpisode()) {
			return &tn.Query{
				Type:    tn.SearchTypeTV,
				IMDBID:  imdbID,
				Season:  season,
				Episode: episode,
			}
		}
		return nil
	}
	if caps == nil || caps.SupportsMovieIMDB() {
		return &tn.Query{Type: tn.SearchTypeMovie, IMDBID: imdbID}
	}
	return nil
}

// buildTitleQuery returns the query-by-name attempt. This is the only mode
// most bare tracker feeds support, and the fallback when an id query finds
// nothing. Returns nil when the title cannot be resolved — an indexer is
// better left empty than searched for a raw IMDb id.
func (s *TorznabStream) buildTitleQuery(ctx context.Context, contentType, imdbID string, season, episode *int) *tn.Query {
	caps := s.indexer.Caps
	isSeries := isSeriesQuery(contentType, season, episode)

	title, err := s.titles.Resolve(ctx, contentType, imdbID)
	if err != nil || title == nil {
		log.WithError(err).
			WithField("content_id", imdbID).
			Debug("failed to resolve title for torznab query")
		return nil
	}

	if isSeries {
		searchType := tn.SearchTypeTV
		if caps != nil && !caps.SupportsTVSearch() {
			searchType = tn.SearchTypeSearch
		}
		// Season and episode go as parameters whenever the indexer takes
		// them, with the title alone in q. Baking "S05E14" into the query
		// text instead assumes releases are named the Anglo per-episode
		// way, and on a tracker that names them "Сезон 5" it matches
		// nothing: measured against a live Jackett, the structured form
		// returned 8 results for an episode where the S01E05 form
		// returned zero.
		if searchType == tn.SearchTypeTV && (caps == nil || caps.SupportsSeasonEpisode()) {
			return &tn.Query{
				Type:    searchType,
				Q:       title.Name,
				Season:  season,
				Episode: episode,
			}
		}
		// No structured support: the episode marker has to ride in the
		// text, which is all such an indexer understands.
		return &tn.Query{
			Type: searchType,
			Q:    fmt.Sprintf("%s S%02dE%02d", title.Name, *season, *episode),
		}
	}

	q := title.Name
	if title.Year != "" {
		q = fmt.Sprintf("%s %s", title.Name, title.Year)
	}
	searchType := tn.SearchTypeMovie
	if caps != nil && !caps.SupportsMovieSearch() {
		searchType = tn.SearchTypeSearch
	}
	return &tn.Query{Type: searchType, Q: q}
}

func isSeriesQuery(contentType string, season, episode *int) bool {
	return contentType == "series" && season != nil && episode != nil
}

// toStreamItems resolves infohashes in parallel and drops results we cannot
// address. A result without a hash is not playable by any Webtor backend, so
// dropping it here is better than surfacing a dead entry.
func (s *TorznabStream) toStreamItems(ctx context.Context, results []tn.Result) []StreamItem {
	items := make([]*StreamItem, len(results))
	sem := make(chan struct{}, hashResolveConcurrency)
	downloads := make(chan struct{}, maxHashDownloads)
	var wg sync.WaitGroup

	for i, r := range results {
		wg.Add(1)
		go func(index int, res tn.Result) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if tn.NeedsDownload(&res) {
				select {
				case downloads <- struct{}{}:
				default:
					// Budget spent on earlier results. Highest-seeded
					// come first, so what is dropped here is the tail.
					return
				}
			}
			hash, err := s.cl.ResolveInfoHash(ctx, &res)
			if err != nil {
				log.WithError(err).
					WithField("indexer", s.indexer.GetName()).
					WithField("title", res.Title).
					Debug("failed to resolve infohash for torznab result")
				return
			}
			items[index] = &StreamItem{
				Name:     s.makeStreamName(res),
				Title:    s.makeStreamTitle(res),
				InfoHash: hash,
				// A Torznab result names a torrent, not a file in it.
				FileIdxUnknown: true,
			}
		}(i, r)
	}
	wg.Wait()

	out := make([]StreamItem, 0, len(items))
	for _, it := range items {
		if it != nil {
			out = append(out, *it)
		}
	}
	return out
}

// makeStreamName follows the addon convention: first line names the source,
// the rest are labels. PreferredStream parses the resolution out of this
// field, and Discover renders the extra lines as chips.
//
// The result's own tracker name is the label, not the indexer's: it is the
// name the user recognises ("RuTracker.org"), it is what learnTrackerName
// stores for the profile row so both agree, and on an aggregate feed it names
// the tracker that actually has this torrent. Appending it to the indexer
// label instead produced "Jackett · rutracker · RuTracker.org" — the same
// tracker spelled three ways.
func (s *TorznabStream) makeStreamName(r tn.Result) string {
	name := s.indexer.GetName()
	if t := strings.TrimSpace(r.Tracker); t != "" {
		name = t
	}
	if res := parseResolution(r.Title); res != "" {
		return name + "\n" + res
	}
	return name
}

// resolutionParser is shared: GetFieldParser already hands out one global
// *FieldParser per field to every request in flight, so the parsers are
// read-only by construction.
var resolutionParser = ptn.NewCompoundParser([]ptn.Parser{ptn.GetFieldParser(ptn.FieldTypeResolution)})

// parseResolution extracts the resolution token from a release name, or ""
// when the name carries none. Kept separate from the resolution bucketing in
// enrich_stream.go, which maps the unknown case to "other".
func parseResolution(name string) string {
	ms := ptn.Matches{}
	ms, err := resolutionParser.Parse(name, ms)
	if err != nil {
		return ""
	}
	ti := &ptn.TorrentInfo{}
	ti.Map(ms)
	return ti.Resolution
}

// makeStreamTitle carries the release name and swarm stats. LangFilterStream
// reads languages out of this field, which is why the untouched release name
// has to be its first line.
func (s *TorznabStream) makeStreamTitle(r tn.Result) string {
	parts := make([]string, 0, 3)
	if r.Seeders > 0 {
		parts = append(parts, fmt.Sprintf("👤 %d", r.Seeders))
	}
	if r.Size > 0 && r.Size <= math.MaxInt64 {
		// Same formatter the library rows use: indexer and library streams
		// sit in one list, so "700 MB" must not become "700.00 MB" halfway
		// down it.
		parts = append(parts, fmt.Sprintf("💾 %s", humanizeBytes(int64(r.Size))))
	}
	if r.Tracker != "" {
		parts = append(parts, fmt.Sprintf("⚙️ %s", r.Tracker))
	}
	if len(parts) == 0 {
		return r.Title
	}
	return r.Title + "\n" + strings.Join(parts, " ")
}

// parseContentID splits Stremio's content id into its parts: "tt0898266" for
// a movie, "tt0898266:5:14" for an episode.
func parseContentID(contentID string) (string, *int, *int) {
	parts := strings.Split(strings.TrimSpace(contentID), ":")
	id := strings.TrimSpace(parts[0])
	if id == "" || !strings.HasPrefix(id, "tt") {
		// Library items use internal ids ("wt-<uuid>") which no external
		// indexer can resolve.
		return "", nil, nil
	}
	if len(parts) < 3 {
		return id, nil, nil
	}
	season, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return id, nil, nil
	}
	episode, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil {
		return id, nil, nil
	}
	return id, &season, &episode
}

// NewTorznabCompositeStreamsByUserID builds a CompositeStream over every
// enabled indexer of a user, mirroring NewAddonCompositeStreamsByUserID.
func NewTorznabCompositeStreamsByUserID(ctx context.Context, db *pg.DB, cl *tn.Client, userID uuid.UUID, titles tn.TitleResolver, cache *lazymap.LazyMap[*StreamsResponse]) (*CompositeStream, error) {
	indexers, err := models.GetUserTorznabIndexers(ctx, db, userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user torznab indexers")
	}
	names := pgTrackerNames{db: db}
	services := make([]StreamsService, 0, len(indexers))
	for _, indexer := range indexers {
		services = append(services, NewTorznabStream(cl, indexer, titles, cache, names))
	}
	return NewCompositeStream(services), nil
}
