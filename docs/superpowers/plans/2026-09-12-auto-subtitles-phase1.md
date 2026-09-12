# Авто-субтитры, фаза 1: поиск, фильтры, телеметрия — план имплементации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Поднять долю запусков, для которых находится человеческая дорожка OpenSubtitles (сейчас по хэшу пусто в 84%), не показывать в плеере непригодные встроенные дорожки и начать измерять, на каком уровне лестницы закрывается каждый запрос.

**Architecture:** video-info получает fallback «хэш → imdb», поддержку сериалов (parent_imdb_id + сезон + серия) и ранжирование кандидатов с капом на язык; web-ui подмешивает imdb-id и сезон/серию в URL `~vi/subtitles.json` из уже сохранённого enrichment, фильтрует битмапные и forced встроенные дорожки, зеркаля правило content-transcoder, и шлёт два Umami-события: `subtitle-resolved` (уровень, на котором закрылся запрос) и `subtitle-select` (выбор любой дорожки). rest-api и torrent-http-proxy не меняются: query-параметры `~vi` уже проходят через прокси в сервис.

**Tech Stack:** Go 1.23 (video-info) / Go 1.26 (web-ui), net/http + httptest, logrus, `services/parse_torrent_name` (ptn) в web-ui, Go-шаблоны, Preact-плеер (`Player.jsx`), Umami (`window.umami.track`), `node --test` для JS.

**Spec:** `web-ui/docs/superpowers/specs/2026-09-12-auto-subtitles-design.md`, раздел «Фазы», строка 1, и разделы «video-info», «web-ui», «Телеметрия».

## Global Constraints

- Уровни лестницы: 0 загруженные пользователем, 1 встроенные, 2 приложенные, 3 OpenSubtitles по хэшу, 4 OpenSubtitles по imdb, 5 whisper (в этой фазе не существует).
- Фильтр встроенных дорожек обязан сохранять нумерацию `MPID` совместимой с content-transcoder: транскодер включает в HLS-группу все `codec_type=subtitle`, кроме `hdmv_pgs_subtitle` (`content-transcoder/services/hls.go:369`).
- OpenSubtitles: в выдачу не попадают `foreign_parts_only`, `ai_translated`, `machine_translated`; на язык не больше 3 кандидатов; кандидаты с `moviehash_match` первыми.
- Событие `subtitle-resolved` шлётся один раз на запуск, с полями `level` (`0`–`4` или `none`), `hasUiLang` (bool), `uiLang`, `count` (int).
- Событие `subtitle-select` шлётся при выборе любой дорожки с полями `provider`, `srclang`, `source` (для OpenSubtitles: `hash` или `imdb`, иначе пусто). Существующее `user-subtitle-select` остаётся.
- В публичном коде нет имён вендоров кроме OpenSubtitles (это открытый API, уже упомянут в коде).
- web-ui тестируется через `make test`, не `go test ./...` (proto-конфликт глушится ldflag'ом). JS: `npm test`.
- Коммиты в web-ui: `git add` только явными файлами, перед коммитом `git status -sb`, HEAD должен быть на `main`.
- Деплой не входит в план: после мержа владелец гонит `/deploy video-info` и `/deploy web`.

---

## Карта файлов

**video-info** (`/Users/vintikzzzz/Projects/webtor/video-info`):
- Modify `services/osdb/models.go` — поле `MoviehashMatch` в `Subtitle.Attributes`.
- Modify `services/osdb/client.go` — `SearchSubtitlesByEpisode`, нормализация imdb-id в клиенте.
- Create `services/osdb/client_test.go` — httptest на формирование запросов.
- Create `services/rank.go` + `services/rank_test.go` — чистая функция ранжирования и капа.
- Modify `services/imdb_search.go`, `services/imdb_search_pool.go` — ключ и запрос с сезоном и серией.
- Modify `services/web.go` — fallback хэш → imdb, параметры `season`/`episode`, поля `source`, `release`, `hi` в JSON.
- Create `services/web_test.go` — тест `search` с фейковым клиентом.

**web-ui** (`/Users/vintikzzzz/Projects/webtor/web-ui`):
- Modify `services/api/api.go` — `ExtSubtitle.Source`, `OpenSubtitleTrack.Source`, функция `WithSubtitleHints`.
- Create `services/api/subtitle_hints_test.go`.
- Modify `jobs/scripts/action.go:560-572` — подмешивание imdb-id и сезона/серии.
- Create `jobs/scripts/subtitle_hints.go` + `jobs/scripts/subtitle_hints_test.go` — чистая функция вычисления подсказок.
- Modify `handlers/action/helper.go:171-244` — фильтр встроенных, поле `Source`.
- Create `handlers/action/helper_test.go`.
- Modify `templates/views/action/stream_video.html` — `data-source`, `data-srclang` на OpenSubtitles-элементах.
- Modify `assets/src/js/lib/player/Player.jsx` — события.
- Create `assets/src/js/lib/player/subtitle-telemetry.js` + `subtitle-telemetry.test.js` — чистые функции для событий.

---

### Task 1: osdb-клиент — поиск по серии и поле moviehash_match

**Files:**
- Modify: `video-info/services/osdb/models.go:46-66`
- Modify: `video-info/services/osdb/client.go:159-170`
- Test: `video-info/services/osdb/client_test.go` (создать)

**Interfaces:**
- Produces: `func (s *Client) SearchSubtitlesByEpisode(ctx context.Context, parentImdbID string, season, episode int) ([]Subtitle, error)`; `func NormalizeImdbID(id string) string`; поле `Subtitle.Attributes.MoviehashMatch bool`.

- [ ] **Step 1: Написать падающий тест на URL запросов**

```go
// video-info/services/osdb/client_test.go
package osdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RequestURI())
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return &Client{apiURL: srv.URL, cl: srv.Client()}, &seen
}

func okEmpty(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"total_pages":1,"total_count":0,"page":1,"data":[]}`))
}

func TestNormalizeImdbID(t *testing.T) {
	cases := map[string]string{"tt0109424": "109424", "TT0000123": "123", "109424": "109424", "": ""}
	for in, want := range cases {
		if got := NormalizeImdbID(in); got != want {
			t.Errorf("NormalizeImdbID(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSearchSubtitlesByIMDBNormalizes(t *testing.T) {
	c, seen := newTestClient(t, okEmpty)
	if _, err := c.SearchSubtitlesByIMDB(context.Background(), "tt0109424"); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[0]; got != "/subtitles?imdb_id=109424" {
		t.Errorf("got %q", got)
	}
}

func TestSearchSubtitlesByEpisode(t *testing.T) {
	c, seen := newTestClient(t, okEmpty)
	if _, err := c.SearchSubtitlesByEpisode(context.Background(), "tt0903747", 1, 3); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[0]; got != "/subtitles?episode_number=3&parent_imdb_id=903747&season_number=1" {
		t.Errorf("got %q", got)
	}
}

func TestMoviehashMatchDecoded(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"1","attributes":{"language":"en","moviehash_match":true}}]}`))
	})
	subs, err := c.SearchSubtitlesByIMDB(context.Background(), "tt1")
	if err != nil || len(subs) != 1 || !subs[0].Attributes.MoviehashMatch {
		t.Fatalf("subs=%+v err=%v", subs, err)
	}
}
```

Поля `Client.apiURL` и `Client.cl` существуют с этими именами (`client.go:70-79`).

- [ ] **Step 2: Запустить, убедиться, что падает**

Run: `cd /Users/vintikzzzz/Projects/webtor/video-info && go test ./services/osdb/ -run 'TestNormalizeImdbID|TestSearchSubtitlesByEpisode|TestMoviehashMatchDecoded|TestSearchSubtitlesByIMDBNormalizes' -v`
Expected: FAIL, `undefined: NormalizeImdbID`, `SearchSubtitlesByEpisode`, `MoviehashMatch`.

- [ ] **Step 3: Реализовать**

В `models.go`, в `Subtitle.Attributes` после `MachineTranslated`:

```go
		MoviehashMatch    bool      `json:"moviehash_match"`
```

В `client.go` заменить `SearchSubtitlesByIMDB` и добавить:

```go
// NormalizeImdbID strips the "tt" prefix and leading zeros: the
// OpenSubtitles API wants the bare number.
func NormalizeImdbID(id string) string {
	id = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id)), "tt")
	return strings.TrimLeft(id, "0")
}

func (s *Client) SearchSubtitlesByIMDB(ctx context.Context, id string) (subs []Subtitle, err error) {
	u := fmt.Sprintf("%v/subtitles?imdb_id=%v", s.apiURL, NormalizeImdbID(id))
	return s.SearchSubtitles(ctx, u)
}

// SearchSubtitlesByEpisode looks up one episode of a series by the
// series' IMDb id. Query keys are alphabetical so tests can match the
// exact URL.
func (s *Client) SearchSubtitlesByEpisode(ctx context.Context, parentImdbID string, season, episode int) (subs []Subtitle, err error) {
	q := url.Values{}
	q.Set("episode_number", strconv.Itoa(episode))
	q.Set("parent_imdb_id", NormalizeImdbID(parentImdbID))
	q.Set("season_number", strconv.Itoa(season))
	return s.SearchSubtitles(ctx, s.apiURL+"/subtitles?"+q.Encode())
}
```

Добавить импорты `net/url`, `strconv`, `strings`.

- [ ] **Step 4: Запустить тесты**

Run: `cd /Users/vintikzzzz/Projects/webtor/video-info && go test ./services/osdb/ -v && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/video-info && git status -sb && git add services/osdb/models.go services/osdb/client.go services/osdb/client_test.go && git commit -m "osdb: episode search by parent imdb id, moviehash_match attribute"
```

---

### Task 2: Ранжирование и кап кандидатов

**Files:**
- Create: `video-info/services/rank.go`
- Test: `video-info/services/rank_test.go`

**Interfaces:**
- Produces: `func RankSubtitles(subs []osdb.Subtitle, perLang int) []osdb.Subtitle` — фильтрует `ForeignPartsOnly`, `AiTranslated`, `MachineTranslated`; сортирует внутри языка: `MoviehashMatch` → `FromTrusted` → `DownloadCount` по убыванию; оставляет не больше `perLang` на язык; порядок языков — по первому появлению во входе.

- [ ] **Step 1: Падающий тест**

```go
// video-info/services/rank_test.go
package services

import (
	"testing"

	"github.com/webtor-io/video-info/services/osdb"
)

func sub(id, lang string, dl int, hashMatch, trusted, foreign, ai bool) osdb.Subtitle {
	var s osdb.Subtitle
	s.Id = id
	s.Attributes.Language = lang
	s.Attributes.DownloadCount = dl
	s.Attributes.MoviehashMatch = hashMatch
	s.Attributes.FromTrusted = trusted
	s.Attributes.ForeignPartsOnly = foreign
	s.Attributes.AiTranslated = ai
	return s
}

func ids(subs []osdb.Subtitle) []string {
	var r []string
	for _, s := range subs {
		r = append(r, s.Id)
	}
	return r
}

func TestRankSubtitlesOrderAndCap(t *testing.T) {
	in := []osdb.Subtitle{
		sub("en-low", "en", 10, false, false, false, false),
		sub("en-trusted", "en", 5, false, true, false, false),
		sub("en-hash", "en", 1, true, false, false, false),
		sub("en-high", "en", 900, false, false, false, false),
		sub("en-foreign", "en", 9999, false, false, true, false),
		sub("pt-ai", "pt", 9999, false, false, false, true),
		sub("pt-one", "pt", 3, false, false, false, false),
	}
	got := ids(RankSubtitles(in, 3))
	want := []string{"en-hash", "en-trusted", "en-high", "pt-one"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestRankSubtitlesEmpty(t *testing.T) {
	if got := RankSubtitles(nil, 3); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
```

- [ ] **Step 2: Убедиться, что падает**

Run: `cd /Users/vintikzzzz/Projects/webtor/video-info && go test ./services/ -run TestRankSubtitles -v`
Expected: FAIL, `undefined: RankSubtitles`.

- [ ] **Step 3: Реализовать**

```go
// video-info/services/rank.go
package services

import (
	"sort"

	"github.com/webtor-io/video-info/services/osdb"
)

// RankSubtitles drops tracks that are useless as a full-dialogue
// source (forced-only, machine/AI translated), orders the rest so a
// hash-matched (already in sync) track wins, then trusted uploaders,
// then popularity, and keeps at most perLang per language. Languages
// keep their first-seen order so the caller's listing stays stable.
func RankSubtitles(subs []osdb.Subtitle, perLang int) []osdb.Subtitle {
	byLang := map[string][]osdb.Subtitle{}
	var order []string
	for _, s := range subs {
		a := s.Attributes
		if a.ForeignPartsOnly || a.AiTranslated || a.MachineTranslated {
			continue
		}
		if _, ok := byLang[a.Language]; !ok {
			order = append(order, a.Language)
		}
		byLang[a.Language] = append(byLang[a.Language], s)
	}
	var res []osdb.Subtitle
	for _, lang := range order {
		group := byLang[lang]
		sort.SliceStable(group, func(i, j int) bool {
			ai, aj := group[i].Attributes, group[j].Attributes
			if ai.MoviehashMatch != aj.MoviehashMatch {
				return ai.MoviehashMatch
			}
			if ai.FromTrusted != aj.FromTrusted {
				return ai.FromTrusted
			}
			return ai.DownloadCount > aj.DownloadCount
		})
		if perLang > 0 && len(group) > perLang {
			group = group[:perLang]
		}
		res = append(res, group...)
	}
	return res
}
```

- [ ] **Step 4: Тесты зелёные**

Run: `cd /Users/vintikzzzz/Projects/webtor/video-info && go test ./services/ -run TestRankSubtitles -v`
Expected: PASS.

- [ ] **Step 5: Негативный контроль.** Временно убрать строку `if ai.MoviehashMatch != aj.MoviehashMatch {...}` — тест обязан покраснеть (`en-hash` уедет вниз). Вернуть.

- [ ] **Step 6: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/video-info && git add services/rank.go services/rank_test.go && git commit -m "rank OpenSubtitles candidates: hash match, trusted, downloads; cap per language"
```

---

### Task 3: video-info — fallback хэш → imdb, сезон/серия, источник в ответе

**Files:**
- Modify: `video-info/services/imdb_search.go`, `video-info/services/imdb_search_pool.go`
- Modify: `video-info/services/web.go:98-116` (`getCacheKey`, `search`), `:184-228` (`/subtitles.json`)
- Test: `video-info/services/web_test.go` (создать)

**Interfaces:**
- Consumes: `RankSubtitles` (Task 2), `SearchSubtitlesByEpisode` (Task 1).
- Produces: тип `SearchQuery{ImdbID string; Season, Episode int}`; `func (s *Web) search(ctx, sourceURL string, q SearchQuery, purge bool, cache *redis.Cache, logger *log.Entry) ([]osdb.Subtitle, string, error)` — второй результат `source` = `"hash"` | `"imdb"` | `""`. JSON `/subtitles.json` получает поля `source`, `release`, `hi`, `downloads`.
- Интерфейс для теста: `type subtitleSearcher interface { ByHash(ctx, sourceURL string, cache *redis.Cache, purge bool) ([]osdb.Subtitle, error); ByIMDB(ctx, q SearchQuery, cache *redis.Cache, purge bool) ([]osdb.Subtitle, error) }` — `Web` держит поле `searcher subtitleSearcher`, в проде это адаптер над `SearchPool` и `IMDBSearchPool`.

- [ ] **Step 1: Падающий тест на fallback**

```go
// video-info/services/web_test.go
package services

import (
	"context"
	"errors"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/video-info/services/osdb"
	"github.com/webtor-io/video-info/services/redis"
)

type fakeSearcher struct {
	hash    []osdb.Subtitle
	hashErr error
	imdb    []osdb.Subtitle
	imdbErr error
	gotQ    SearchQuery
	calls   []string
}

func (f *fakeSearcher) ByHash(_ context.Context, _ string, _ *redis.Cache, _ bool) ([]osdb.Subtitle, error) {
	f.calls = append(f.calls, "hash")
	return f.hash, f.hashErr
}
func (f *fakeSearcher) ByIMDB(_ context.Context, q SearchQuery, _ *redis.Cache, _ bool) ([]osdb.Subtitle, error) {
	f.calls = append(f.calls, "imdb")
	f.gotQ = q
	return f.imdb, f.imdbErr
}

func one(id string) osdb.Subtitle { var s osdb.Subtitle; s.Id = id; s.Attributes.Language = "en"; return s }

func TestSearchHashWins(t *testing.T) {
	f := &fakeSearcher{hash: []osdb.Subtitle{one("h")}, imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	subs, src, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, log.NewEntry(log.New()))
	if err != nil || src != "hash" || len(subs) != 1 || subs[0].Id != "h" {
		t.Fatalf("subs=%v src=%q err=%v", subs, src, err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("imdb must not be queried when hash hits: %v", f.calls)
	}
}

func TestSearchFallsBackToIMDBOnEmptyHash(t *testing.T) {
	f := &fakeSearcher{imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	subs, src, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1", Season: 2, Episode: 5}, false, nil, log.NewEntry(log.New()))
	if err != nil || src != "imdb" || len(subs) != 1 || subs[0].Id != "i" {
		t.Fatalf("subs=%v src=%q err=%v", subs, src, err)
	}
	if f.gotQ.Season != 2 || f.gotQ.Episode != 5 {
		t.Fatalf("season/episode not forwarded: %+v", f.gotQ)
	}
}

func TestSearchFallsBackToIMDBOnHashError(t *testing.T) {
	f := &fakeSearcher{hashErr: errors.New("boom"), imdb: []osdb.Subtitle{one("i")}}
	w := &Web{searcher: f}
	_, src, err := w.search(context.Background(), "http://src", SearchQuery{ImdbID: "tt1"}, false, nil, log.NewEntry(log.New()))
	if err != nil || src != "imdb" {
		t.Fatalf("src=%q err=%v", src, err)
	}
}

func TestSearchNoIMDBNoFallback(t *testing.T) {
	f := &fakeSearcher{}
	w := &Web{searcher: f}
	subs, src, err := w.search(context.Background(), "http://src", SearchQuery{}, false, nil, log.NewEntry(log.New()))
	if err != nil || src != "" || len(subs) != 0 || len(f.calls) != 1 {
		t.Fatalf("subs=%v src=%q err=%v calls=%v", subs, src, err, f.calls)
	}
}

func TestCacheKeyIncludesEpisode(t *testing.T) {
	a := cacheKey("hash", "/p", SearchQuery{ImdbID: "tt1", Season: 1, Episode: 1})
	b := cacheKey("hash", "/p", SearchQuery{ImdbID: "tt1", Season: 1, Episode: 2})
	if a == b {
		t.Fatal("cache key must differ per episode")
	}
}
```

- [ ] **Step 2: Убедиться, что падает**

Run: `cd /Users/vintikzzzz/Projects/webtor/video-info && go test ./services/ -run 'TestSearch|TestCacheKey' -v`
Expected: FAIL, `undefined: SearchQuery`, `searcher`, `cacheKey`.

- [ ] **Step 3: Реализовать**

`imdb_search.go`: заменить поле `imdbID string` на `q SearchQuery` и запрос:

```go
type SearchQuery struct {
	ImdbID  string
	Season  int
	Episode int
}

func (q SearchQuery) IsEpisode() bool { return q.Season > 0 && q.Episode > 0 }

func (q SearchQuery) Key() string {
	return fmt.Sprintf("%s:%d:%d", osdb.NormalizeImdbID(q.ImdbID), q.Season, q.Episode)
}
```

и в `get`:

```go
	var subtitles []osdb.Subtitle
	var err error
	if s.q.IsEpisode() {
		subtitles, err = s.cl.SearchSubtitlesByEpisode(ctx, s.q.ImdbID, s.q.Season, s.q.Episode)
	} else {
		subtitles, err = s.cl.SearchSubtitlesByIMDB(ctx, s.q.ImdbID)
	}
```

(Заодно заменить `context.Background()` на `ctx` — сейчас запрос не отменяется вместе с клиентом.) `NewIMDBSearch(q SearchQuery, cl, c)`.

`imdb_search_pool.go`:

```go
func (s *IMDBSearchPool) Get(ctx context.Context, q SearchQuery, c *redis.Cache, purge bool) ([]osdb.Subtitle, error) {
	v, loaded := s.sm.LoadOrStore(q.Key(), NewIMDBSearch(q, s.cl, c))
	if !loaded {
		defer s.sm.Delete(q.Key())
	}
	return v.(*IMDBSearch).Get(ctx, purge)
}
```

`web.go`: добавить интерфейс и адаптер, поле в `Web`, инициализацию в `NewWeb`:

```go
type subtitleSearcher interface {
	ByHash(ctx context.Context, sourceURL string, cache *redis.Cache, purge bool) ([]osdb.Subtitle, error)
	ByIMDB(ctx context.Context, q SearchQuery, cache *redis.Cache, purge bool) ([]osdb.Subtitle, error)
}

type poolSearcher struct {
	hash *SearchPool
	imdb *IMDBSearchPool
}

func (p poolSearcher) ByHash(ctx context.Context, u string, c *redis.Cache, purge bool) ([]osdb.Subtitle, error) {
	return p.hash.Get(ctx, u, c, purge)
}
func (p poolSearcher) ByIMDB(ctx context.Context, q SearchQuery, c *redis.Cache, purge bool) ([]osdb.Subtitle, error) {
	return p.imdb.Get(ctx, q, c, purge)
}
```

В `NewWeb`: `searcher: poolSearcher{hash: sp, imdb: isp}` (старые поля `searchPool`/`imdbSearchPool` убрать, если больше не используются).

Разбор запроса и ключ кэша:

```go
func parseSearchQuery(r *http.Request) SearchQuery {
	q := r.URL.Query()
	season, _ := strconv.Atoi(q.Get("season"))
	episode, _ := strconv.Atoi(q.Get("episode"))
	return SearchQuery{ImdbID: q.Get("imdb-id"), Season: season, Episode: episode}
}

func cacheKey(infoHash, path string, q SearchQuery) string {
	return infoHash + path + q.Key()
}

func getCacheKey(r *http.Request) string {
	return cacheKey(r.Header.Get("X-Info-Hash"), r.Header.Get("X-Path"), parseSearchQuery(r))
}
```

Ключ кэша по-прежнему один на (файл, запрос): hash-результат и imdb-результат не смешиваются, потому что `search` кладёт в один `cache` только то, что вернул. Внимание: `SearchPool`/`IMDBSearch` пишут `cache.SetSubtitles` сами, и после fallback в кэше окажется imdb-выдача под тем же ключом — это ожидаемо (следующий запрос сразу получит её из кэша через `ByHash`... нет: `ByHash` читает кэш первым и вернёт imdb-выдачу как «hash»). Чтобы `source` не врал, `search` определяет источник по данным, а не по ветке:

```go
func sourceOf(subs []osdb.Subtitle) string {
	for _, s := range subs {
		if s.Attributes.MoviehashMatch {
			return "hash"
		}
	}
	return "imdb"
}

func (s *Web) search(ctx context.Context, sourceURL string, q SearchQuery, purge bool, cache *redis.Cache, logger *log.Entry) ([]osdb.Subtitle, string, error) {
	if sourceURL == "" && q.ImdbID == "" {
		return nil, "", errors.Errorf("no data provided to find subtitles")
	}
	var subs []osdb.Subtitle
	var err error
	if sourceURL != "" {
		logger.Info("fetching subtitles by hash and file size")
		subs, err = s.searcher.ByHash(ctx, sourceURL, cache, purge)
		if err != nil {
			logger.WithError(err).Warn("hash search failed")
		}
		if len(subs) > 0 {
			return RankSubtitles(subs, 3), sourceOf(subs), nil
		}
	}
	if q.ImdbID == "" {
		return nil, "", err
	}
	logger.WithField("episode", q.IsEpisode()).Info("fetching subtitles by IMDB id")
	subs, err = s.searcher.ByIMDB(ctx, q, cache, purge)
	if err != nil {
		return nil, "", err
	}
	return RankSubtitles(subs, 3), "imdb", nil
}
```

Примечание к `sourceOf`: OpenSubtitles проставляет `moviehash_match` только когда в запросе был `moviehash`, поэтому у hash-выдачи он `true` у всех, у imdb-выдачи `false`. Если hash-выдача пришла из кэша, флаг сохранён в JSON — источник восстанавливается верно.

Оба хендлера (`/opensubtitles/`, `/subtitles.json`) переводятся на `parseSearchQuery(r)` и новую сигнатуру. В `/subtitles.json` расширить `Subtitle` (`web.go:40-46`):

```go
type Subtitle struct {
	SrcLang   string  `json:"srclang"`
	Label     string  `json:"label"`
	Src       string  `json:"src"`
	Format    string  `json:"format"`
	ID        string  `json:"id"`
	Source    string  `json:"source"`
	Release   string  `json:"release,omitempty"`
	Fps       float64 `json:"fps,omitempty"`
	HI        bool    `json:"hi,omitempty"`
	Downloads int     `json:"downloads,omitempty"`
}
```

и заполнять `Source: source, Release: s.Attributes.Release, Fps: s.Attributes.Fps, HI: s.Attributes.HearingImpaired, Downloads: s.Attributes.DownloadCount`. В `/opensubtitles/<id>` при поиске дорожки по id использовать нерэнкованный список? Нет: `search` уже вернул ранжированный список, а id из него же выдан клиенту, значит совпадёт. Единственный риск — кап на язык мог отрезать дорожку, id которой клиент получил из старого списка до деплоя; кэш 24 ч, принять.

- [ ] **Step 4: Тесты и сборка**

Run: `cd /Users/vintikzzzz/Projects/webtor/video-info && go build ./... && go vet ./... && go test ./... -v 2>&1 | tail -20`
Expected: PASS, сборка чистая.

- [ ] **Step 5: Ручная проверка бинарём против прода OpenSubtitles (без деплоя)**

Взять `OSDB_API_KEY` и UA из `infra/helmfile/values/video-info.yaml.gotmpl`, запустить локально:

```bash
cd /Users/vintikzzzz/Projects/webtor/video-info && OSDB_API_KEY=… OSDB_API_USER_AGENT=… go run . serve --web-port 18080 &
curl -s 'http://localhost:18080/subtitles.json?imdb-id=tt0903747&season=1&episode=3' | python3 -m json.tool | head -40
curl -s 'http://localhost:18080/subtitles.json?imdb-id=tt0109424' | python3 -c 'import sys,json; d=json.load(sys.stdin); print(len(d), sorted({x["srclang"] for x in d})[:20], d[0])'
```

Expected: сериал отдаёт дорожки именно S01E03 (в `release` виден `S01E03`), фильм отдаёт ≤ 3 на язык, `source: "imdb"`. Если Redis не настроен локально и сервис падает на старте — поднять `docker run -p 6379:6379 redis:7` и указать `REDIS_HOST=localhost`. Точные имена флагов — в `configure.go` и `common-services`.

- [ ] **Step 6: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/video-info && git add services/imdb_search.go services/imdb_search_pool.go services/web.go services/web_test.go && git commit -m "subtitles: hash first, imdb fallback with season/episode, ranked candidates, source in response"
```

---

### Task 4: web-ui — подсказки для `~vi` в URL и поле `Source`

**Files:**
- Modify: `web-ui/services/api/api.go:136-143` (`ExtSubtitle`), `:609-612` (`OpenSubtitleTrack`), `:614-650` (`GetOpenSubtitles`)
- Create: `web-ui/services/api/subtitle_hints.go`
- Test: `web-ui/services/api/subtitle_hints_test.go`

**Interfaces:**
- Produces: `func WithSubtitleHints(u string, h SubtitleHints) string`; `type SubtitleHints struct { ImdbID string; Season, Episode int }`; `OpenSubtitleTrack.Source string`; `ExtSubtitle.Source string`.

- [ ] **Step 1: Падающий тест**

```go
// web-ui/services/api/subtitle_hints_test.go
package api

import "testing"

func TestWithSubtitleHints(t *testing.T) {
	base := "https://x.test/abc/Movie.mkv~vi/subtitles.json?token=T&api-key=K"
	cases := []struct {
		name string
		h    SubtitleHints
		want string
	}{
		{"empty", SubtitleHints{}, base},
		{"imdb only", SubtitleHints{ImdbID: "tt0109424"}, "https://x.test/abc/Movie.mkv~vi/subtitles.json?api-key=K&imdb-id=tt0109424&token=T"},
		{"episode", SubtitleHints{ImdbID: "tt0903747", Season: 1, Episode: 3}, "https://x.test/abc/Movie.mkv~vi/subtitles.json?api-key=K&episode=3&imdb-id=tt0903747&season=1&token=T"},
		{"season without episode ignored", SubtitleHints{ImdbID: "tt1", Season: 2}, "https://x.test/abc/Movie.mkv~vi/subtitles.json?api-key=K&imdb-id=tt1&token=T"},
	}
	for _, c := range cases {
		if got := WithSubtitleHints(base, c.h); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestWithSubtitleHintsKeepsExistingImdb(t *testing.T) {
	base := "https://x.test/a/b~vi/subtitles.json?imdb-id=tt9&token=T"
	got := WithSubtitleHints(base, SubtitleHints{ImdbID: "tt1"})
	if got != "https://x.test/a/b~vi/subtitles.json?imdb-id=tt9&token=T" {
		t.Errorf("rest-api supplied imdb-id must win: %q", got)
	}
}
```

- [ ] **Step 2: Убедиться, что падает**

Run: `cd /Users/vintikzzzz/Projects/webtor/web-ui && go test ./services/api/ -run TestWithSubtitleHints -v`
Expected: FAIL, `undefined: WithSubtitleHints`.

- [ ] **Step 3: Реализовать**

```go
// web-ui/services/api/subtitle_hints.go
package api

import (
	"net/url"
	"strconv"
)

// SubtitleHints is what web-ui knows about the file beyond its bytes:
// the IMDb id from enrichment and, for a series file, which episode.
// video-info uses them when the OpenSubtitles hash lookup is empty.
type SubtitleHints struct {
	ImdbID  string
	Season  int
	Episode int
}

// WithSubtitleHints appends the hints as query parameters of a
// ~vi/subtitles.json URL. An imdb-id already present (rest-api forwards
// the one an API caller passed explicitly) is kept. Season is only
// meaningful together with episode. Query keys are re-encoded in sorted
// order, which is how url.Values encodes anyway.
func WithSubtitleHints(u string, h SubtitleHints) string {
	if h.ImdbID == "" {
		return u
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	q := parsed.Query()
	if q.Get("imdb-id") == "" {
		q.Set("imdb-id", h.ImdbID)
	}
	if h.Season > 0 && h.Episode > 0 {
		q.Set("season", strconv.Itoa(h.Season))
		q.Set("episode", strconv.Itoa(h.Episode))
	}
	parsed.RawQuery = q.Encode()
	return parsed.String()
}
```

В `api.go`: `ExtSubtitle` получает `Source string \`json:"source"\``; `OpenSubtitleTrack` получает `Source string`; в `GetOpenSubtitles` (`api.go:645`, где стоит `ID: esub.Id,`) добавить строку `Source: esub.Source,`.

- [ ] **Step 4: Тесты**

Run: `cd /Users/vintikzzzz/Projects/webtor/web-ui && go test ./services/api/ -v -run 'TestWithSubtitleHints|Subtitle'`
Expected: PASS (включая существующий `subtitle_url_test.go`).

- [ ] **Step 5: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && git status -sb && git add services/api/subtitle_hints.go services/api/subtitle_hints_test.go services/api/api.go && git commit -m "api: subtitle hints (imdb, season, episode) for ~vi, source field on OpenSubtitles tracks"
```

---

### Task 5: web-ui — вычисление подсказок в job-скрипте стрима

**Files:**
- Create: `web-ui/jobs/scripts/subtitle_hints.go`
- Test: `web-ui/jobs/scripts/subtitle_hints_test.go`
- Modify: `web-ui/jobs/scripts/action.go:560-572`

**Interfaces:**
- Consumes: `api.SubtitleHints`, `api.WithSubtitleHints` (Task 4); `enrich.MakeTorrentInfo(item *ra.ListItem) (*enrich.TorrentInfo, error)` (`services/enrich/enrich.go:455`); `models.VideoMetadata.VideoID` (imdb `tt…` или `tmdb123`).
- Produces: `func subtitleHints(settingsImdbID string, md *models.VideoMetadata, item *ra.ListItem) api.SubtitleHints`.

- [ ] **Step 1: Падающий тест**

```go
// web-ui/jobs/scripts/subtitle_hints_test.go
package scripts

import (
	"testing"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
)

func TestSubtitleHintsPrefersEmbedSetting(t *testing.T) {
	h := subtitleHints("tt0000001", &models.VideoMetadata{VideoID: "tt0000002"}, &ra.ListItem{PathStr: "/Movie.2020.1080p.mkv"})
	if h.ImdbID != "tt0000001" {
		t.Fatalf("got %+v", h)
	}
}

func TestSubtitleHintsFromEnrichment(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tt0109424"}, &ra.ListItem{PathStr: "/Movie.2020.1080p.mkv"})
	if h.ImdbID != "tt0109424" || h.Season != 0 || h.Episode != 0 {
		t.Fatalf("got %+v", h)
	}
}

func TestSubtitleHintsIgnoresTmdbOnlyID(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tmdb12345"}, &ra.ListItem{PathStr: "/Movie.mkv"})
	if h.ImdbID != "" {
		t.Fatalf("tmdb id must not be sent as imdb-id: %+v", h)
	}
}

func TestSubtitleHintsEpisodeFromPath(t *testing.T) {
	h := subtitleHints("", &models.VideoMetadata{VideoID: "tt0903747"}, &ra.ListItem{PathStr: "/Breaking.Bad.S01/Breaking.Bad.S01E03.1080p.mkv"})
	if h.ImdbID != "tt0903747" || h.Season != 1 || h.Episode != 3 {
		t.Fatalf("got %+v", h)
	}
}

func TestSubtitleHintsNilSafe(t *testing.T) {
	h := subtitleHints("", nil, nil)
	if h.ImdbID != "" || h.Season != 0 || h.Episode != 0 {
		t.Fatalf("got %+v", h)
	}
}
```

- [ ] **Step 2: Убедиться, что падает**

Run: `cd /Users/vintikzzzz/Projects/webtor/web-ui && go test -ldflags "$(grep PROTO_CONFLICT_LDFLAGS Makefile | head -1 | sed 's/.*= *//')" ./jobs/scripts/ -run TestSubtitleHints -v`

Если извлечение ldflags из Makefile неудобно — `make test` целиком (медленнее, но надёжно).
Expected: FAIL, `undefined: subtitleHints`.

- [ ] **Step 3: Реализовать**

```go
// web-ui/jobs/scripts/subtitle_hints.go
package scripts

import (
	"strings"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/enrich"
)

// subtitleHints decides what to tell video-info about the file. An
// explicit imdb id from embed settings wins (the embedding site knows
// its content); otherwise the persisted enrichment is used when it
// resolved to IMDb (TMDB-only ids are useless to OpenSubtitles).
// Season and episode come from the file path via the same parser
// enrichment uses, so series files hit the episode endpoint.
func subtitleHints(settingsImdbID string, md *models.VideoMetadata, item *ra.ListItem) api.SubtitleHints {
	h := api.SubtitleHints{ImdbID: settingsImdbID}
	if h.ImdbID == "" && md != nil && strings.HasPrefix(md.VideoID, "tt") {
		h.ImdbID = md.VideoID
	}
	if h.ImdbID == "" || item == nil {
		return h
	}
	if ti, err := enrich.MakeTorrentInfo(item); err == nil && ti != nil && ti.TorrentInfo != nil {
		h.Season = ti.Season
		h.Episode = ti.Episode
	}
	return h
}
```

В `action.go:560-572` заменить вызов:

```go
		if subtitles, ok := exportResponse.ExportItems["subtitles"]; ok {
			if osEnabled, ok := settings.Features["opensubtitles"]; (ok && osEnabled) || !ok {
				j.InProgress(s.t("job.loadingSubtitles"))
				osCtx, osCancel := context.WithTimeout(ctx, 30*time.Second)
				defer osCancel()
				subsURL := api.WithSubtitleHints(subtitles.URL, subtitleHints(settings.ImdbID, enrichedMD, sc.Item))
				subs, err := s.api.GetOpenSubtitles(osCtx, subsURL)
```

`enrichedMD` уже объявлен выше в той же функции (`action.go:404`), `sc.Item` — `action.go:397`. Проверить, что пакет `api` уже импортирован в `action.go` (используется `*api.Api`, значит да).

- [ ] **Step 4: Тесты**

Run: `cd /Users/vintikzzzz/Projects/webtor/web-ui && make test 2>&1 | tail -30`
Expected: все пакеты `ok`.

- [ ] **Step 5: Негативный контроль.** Убрать проверку `strings.HasPrefix(md.VideoID, "tt")` — `TestSubtitleHintsIgnoresTmdbOnlyID` обязан покраснеть. Вернуть.

- [ ] **Step 6: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && git status -sb && git add jobs/scripts/subtitle_hints.go jobs/scripts/subtitle_hints_test.go jobs/scripts/action.go && git commit -m "stream: pass imdb id and episode from enrichment to OpenSubtitles lookup"
```

---

### Task 6: web-ui — фильтр встроенных дорожек и `Source` в списке

**Files:**
- Modify: `web-ui/handlers/action/helper.go:14-23` (`ListItem`), `:171-244` (`GetSubtitles`)
- Test: `web-ui/handlers/action/helper_test.go` (создать)
- Modify: `web-ui/templates/views/action/stream_video.html:69` (OpenSubtitles `<li>`)

**Interfaces:**
- Consumes: `api.OpenSubtitleTrack.Source` (Task 4).
- Produces: `ListItem.Source string`; функция `embeddedSubtitleVisible(codecName, title string) (visible bool, countsForHLS bool)`.

- [ ] **Step 1: Падающий тест**

```go
// web-ui/handlers/action/helper_test.go
package action

import (
	"encoding/json"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
)

func probeWith(streams string) *api.MediaProbe {
	var mp api.MediaProbe
	if err := json.Unmarshal([]byte(`{"streams":`+streams+`}`), &mp); err != nil {
		panic(err)
	}
	return &mp
}

func subtitleItems(items []ListItem) map[string]ListItem {
	m := map[string]ListItem{}
	for _, it := range items {
		if it.Provider == "MediaProbe" {
			m[it.ID] = it
		}
	}
	return m
}

func TestGetSubtitlesSkipsPGSWithoutIndexShift(t *testing.T) {
	// Transcoder drops hdmv_pgs from the HLS group, so the text track
	// that follows it is HLS subtitle #0, not #1.
	mp := probeWith(`[
		{"codec_type":"video","codec_name":"h264"},
		{"codec_type":"subtitle","codec_name":"hdmv_pgs_subtitle","tags":{"language":"eng"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"rus","title":"Russian"}}
	]`)
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil)
	got := subtitleItems(items)
	if len(got) != 1 {
		t.Fatalf("want exactly one embedded track, got %+v", got)
	}
	it, ok := got["mp-0"]
	if !ok || it.MPID != "0" || it.SrcLang != "ru" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetSubtitlesHidesDVDSubButKeepsIndex(t *testing.T) {
	// dvd_subtitle IS in the transcoder's group (only PGS is excluded),
	// so it occupies HLS index 0 and the text track after it is #1.
	mp := probeWith(`[
		{"codec_type":"subtitle","codec_name":"dvd_subtitle","tags":{"language":"eng"}},
		{"codec_type":"subtitle","codec_name":"ass","tags":{"language":"eng"}}
	]`)
	got := subtitleItems(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil))
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if it, ok := got["mp-1"]; !ok || it.MPID != "1" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetSubtitlesHidesForcedByTitle(t *testing.T) {
	mp := probeWith(`[
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English (Forced)"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}}
	]`)
	got := subtitleItems(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil))
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if it, ok := got["mp-1"]; !ok || it.Label != "English" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetSubtitlesCarriesOpenSubtitlesSource(t *testing.T) {
	os := []api.OpenSubtitleTrack{{ID: "7", Source: "imdb", ExportTrack: &ra.ExportTrack{Src: "u", SrcLang: "en", Label: "English", Kind: "subtitles"}}}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, nil, &ra.ExportTag{}, os, &models.ExternalData{}, nil)
	for _, it := range items {
		if it.ID == "os-7" {
			if it.Source != "imdb" {
				t.Fatalf("got %+v", it)
			}
			return
		}
	}
	t.Fatal("os-7 not found")
}

func TestEmbeddedSubtitleVisible(t *testing.T) {
	cases := []struct {
		codec, title string
		visible, counts bool
	}{
		{"subrip", "", true, true},
		{"ass", "Signs & Songs", true, true},
		{"hdmv_pgs_subtitle", "", false, false},
		{"dvd_subtitle", "", false, true},
		{"dvb_subtitle", "", false, true},
		{"subrip", "Forced", false, true},
		{"subrip", "eng forced narrative", false, true},
	}
	for _, c := range cases {
		v, n := embeddedSubtitleVisible(c.codec, c.title)
		if v != c.visible || n != c.counts {
			t.Errorf("%s/%q: got (%v,%v) want (%v,%v)", c.codec, c.title, v, n, c.visible, c.counts)
		}
	}
}
```

Проверить, что `models.VideoStreamUserData` создаётся нулевым значением без паники в `selectListItem`/`matchLang` (там используется `ud.AcceptLangTags`, `ud.FallbackLangTag`, `ud.SubtitleID` — нулевые значения допустимы: `matcher.Match()` без тегов вернёт `language.No`). Если паникует — заменить на `models.NewVideoStreamUserData("r", "i", &models.StreamSettings{})` (`handlers/action/handler.go:195`).

- [ ] **Step 2: Убедиться, что падает**

Run: `cd /Users/vintikzzzz/Projects/webtor/web-ui && make test 2>&1 | grep -A5 'handlers/action'`
Expected: FAIL, `undefined: embeddedSubtitleVisible`, `it.Source`.

- [ ] **Step 3: Реализовать**

В `ListItem` добавить `Source string` после `Kind`. В `helper.go` добавить:

```go
// Bitmap subtitle codecs cannot be rendered by the browser (they need
// OCR) and cannot be translated. hdmv_pgs is also dropped by
// content-transcoder from the HLS subtitle group, so it must not
// consume an MPID; the other bitmap codecs stay in the group and keep
// their slot even though we hide them.
var bitmapSubtitleCodecs = map[string]bool{
	"hdmv_pgs_subtitle": true,
	"dvd_subtitle":      true,
	"dvb_subtitle":      true,
	"xsub":              true,
}

// embeddedSubtitleVisible reports whether an embedded subtitle stream
// is offered in the picker and whether it occupies an index in the
// transcoder's HLS subtitle group (see content-transcoder
// services/hls.go: everything but hdmv_pgs is included). "Forced"
// tracks carry only foreign-language lines and are hidden by title
// until content-prober exposes ffprobe's disposition flags.
func embeddedSubtitleVisible(codecName, title string) (visible bool, countsForHLS bool) {
	if codecName == "hdmv_pgs_subtitle" {
		return false, false
	}
	if bitmapSubtitleCodecs[codecName] {
		return false, true
	}
	if strings.Contains(strings.ToLower(title), "forced") {
		return false, true
	}
	return true, true
}
```

Цикл MediaProbe в `GetSubtitles` переписать:

```go
	if mp != nil {
		i := 0
		for _, stream := range mp.Streams {
			if stream.CodecType != "subtitle" {
				continue
			}
			visible, counts := embeddedSubtitleVisible(stream.CodecName, stream.Tags.Title)
			if !counts {
				continue
			}
			if visible {
				label := fmt.Sprintf("Subtitle #%v", i+1)
				if stream.Tags.Title != "" {
					label = stream.Tags.Title
				}
				srcLang := "eng"
				if stream.Tags.Language != "" {
					srcLang = stream.Tags.Language
				}
				res = append(res, ListItem{
					ID:       "mp-" + strconv.Itoa(i),
					MPID:     strconv.Itoa(i),
					Label:    label,
					SrcLang:  srcLang,
					Kind:     "subtitles",
					Provider: "MediaProbe",
				})
			}
			i++
		}
	}
```

В цикле OpenSubtitles добавить `Source: t.Source`. Добавить `"strings"` в импорты.

В `stream_video.html:69` OpenSubtitles-элемент:

```html
<li data-id="{{ .ID }}" data-provider="{{ .Provider }}" data-srclang="{{ .SrcLang }}" data-source="{{ .Source }}" {{ if .Default }}data-default="true" {{ end }} class="subtitle cursor-pointer pr-3{{ if .Default }} text-primary underline{{ end }}">{{ .Label }}</li>
```

- [ ] **Step 4: Тесты**

Run: `cd /Users/vintikzzzz/Projects/webtor/web-ui && make test 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 5: Негативный контроль.** В `embeddedSubtitleVisible` вернуть для `hdmv_pgs_subtitle` `(false, true)` — `TestGetSubtitlesSkipsPGSWithoutIndexShift` обязан покраснеть (дорожка станет `mp-1`). Вернуть.

- [ ] **Step 6: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && git status -sb && git add handlers/action/helper.go handlers/action/helper_test.go templates/views/action/stream_video.html && git commit -m "subtitles: hide bitmap and forced embedded tracks, keep HLS index in sync with transcoder; carry OpenSubtitles source"
```

---

### Task 7: web-ui — телеметрия `subtitle-resolved` и `subtitle-select`

**Files:**
- Create: `web-ui/assets/src/js/lib/player/subtitle-telemetry.js`
- Test: `web-ui/assets/src/js/lib/player/subtitle-telemetry.test.js`
- Modify: `web-ui/assets/src/js/lib/player/Player.jsx:189-198` (рядом со `stream-start`), `:743-760` (клик по дорожке)

**Interfaces:**
- Produces: `export function resolveSubtitleLevel(tracks, uiLang)` → `{level: '0'|'1'|'2'|'3'|'4'|'none', hasUiLang: boolean, count: number}`; `export function selectEventData(el)` → `{provider, srclang, source}`; `export function readTracks(modal)` → массив `{provider, srclang, source}` из `li.subtitle` (кроме `data-id="none"`).
- Соответствие провайдер → уровень: `UserSubtitle`→`0`, `MediaProbe`→`1`, `ExportTag`→`2`, `OpenSubtitles` c `source=hash`→`3`, `OpenSubtitles` иначе→`4`, `External`→`2`.

- [ ] **Step 1: Падающий тест**

```js
// web-ui/assets/src/js/lib/player/subtitle-telemetry.test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolveSubtitleLevel, selectEventData } from './subtitle-telemetry.js';

test('none when no tracks', () => {
    assert.deepEqual(resolveSubtitleLevel([], 'en'), { level: 'none', hasUiLang: false, count: 0 });
});

test('best level wins and ui language is detected', () => {
    const tracks = [
        { provider: 'OpenSubtitles', srclang: 'en', source: 'imdb' },
        { provider: 'ExportTag', srclang: 'ru', source: '' },
        { provider: 'OpenSubtitles', srclang: 'pt-BR', source: 'imdb' },
    ];
    assert.deepEqual(resolveSubtitleLevel(tracks, 'pt'), { level: '2', hasUiLang: true, count: 3 });
});

test('hash-matched OpenSubtitles is level 3, imdb is 4', () => {
    assert.equal(resolveSubtitleLevel([{ provider: 'OpenSubtitles', srclang: 'en', source: 'hash' }], 'de').level, '3');
    assert.equal(resolveSubtitleLevel([{ provider: 'OpenSubtitles', srclang: 'en', source: 'imdb' }], 'de').level, '4');
});

test('user subtitles are level 0', () => {
    assert.equal(resolveSubtitleLevel([{ provider: 'UserSubtitle', srclang: '', source: '' }], 'en').level, '0');
});

test('selectEventData reads data attributes', () => {
    const el = {
        getAttribute: (n) => ({ 'data-provider': 'OpenSubtitles', 'data-srclang': 'en', 'data-source': 'hash' })[n] || null,
    };
    assert.deepEqual(selectEventData(el), { provider: 'OpenSubtitles', srclang: 'en', source: 'hash' });
});
```

- [ ] **Step 2: Убедиться, что падает**

Run: `cd /Users/vintikzzzz/Projects/webtor/web-ui && npm test 2>&1 | tail -15`
Expected: FAIL, модуль не найден.

- [ ] **Step 3: Реализовать**

```js
// web-ui/assets/src/js/lib/player/subtitle-telemetry.js
// Pure helpers behind the two Umami events that measure the subtitle
// ladder (spec: docs/superpowers/specs/2026-09-12-auto-subtitles-design.md).
// Levels: 0 user upload, 1 embedded, 2 sidecar in torrent / embed
// external, 3 OpenSubtitles matched by file hash, 4 OpenSubtitles by
// IMDb id. Lower is better; whisper (5) does not exist yet.

const LEVELS = ['0', '1', '2', '3', '4'];

function levelOf(track) {
    switch (track.provider) {
        case 'UserSubtitle': return '0';
        case 'MediaProbe': return '1';
        case 'ExportTag':
        case 'External': return '2';
        case 'OpenSubtitles': return track.source === 'hash' ? '3' : '4';
        default: return null;
    }
}

function baseLang(tag) {
    return String(tag || '').toLowerCase().split(/[-_]/)[0];
}

export function readTracks(modal) {
    if (!modal) return [];
    return Array.from(modal.querySelectorAll('li.subtitle'))
        .filter((el) => el.getAttribute('data-id') !== 'none')
        .map(selectEventData);
}

export function selectEventData(el) {
    return {
        provider: el.getAttribute('data-provider') || '',
        srclang: el.getAttribute('data-srclang') || '',
        source: el.getAttribute('data-source') || '',
    };
}

export function resolveSubtitleLevel(tracks, uiLang) {
    let best = null;
    let hasUiLang = false;
    const ui = baseLang(uiLang);
    for (const t of tracks) {
        const l = levelOf(t);
        if (l !== null && (best === null || LEVELS.indexOf(l) < LEVELS.indexOf(best))) best = l;
        if (ui && baseLang(t.srclang) === ui) hasUiLang = true;
    }
    return { level: best === null ? 'none' : best, hasUiLang, count: tracks.length };
}
```

В `Player.jsx`: импорт `import { readTracks, resolveSubtitleLevel, selectEventData } from './subtitle-telemetry.js';`. В `useEffect` со `stream-start` (`:189-198`) после `window.umami.track('stream-start', …)` добавить:

```js
        const modal = document.getElementById('subtitles');
        const uiLang = document.documentElement.lang || '';
        if (window.umami && modal) {
            window.umami.track('subtitle-resolved', { ...resolveSubtitleLevel(readTracks(modal), uiLang), uiLang });
        }
```

`document.documentElement.lang` заполнен: `templates/layouts/main.html:2` ставит `<html lang="{{ $.Lang }}">`.

В обработчике клика (`:743-760`) заменить тело на:

```js
            const target = e.target.closest('.subtitle');
            if (!target || !subtitlesModal.contains(target)) return;
            const id = target.getAttribute('data-id');
            if (id && id !== 'none' && window.umami) {
                window.umami.track('subtitle-select', selectEventData(target));
                if (target.getAttribute('data-provider') === 'UserSubtitle') window.umami.track('user-subtitle-select');
            }
            activateSubtitle(container, target);
```

`data-srclang` уже есть у элементов `#embedded` (`stream_video.html:57`) и `#my-subtitles` (`partials/action/user_subtitles.html:23`); OpenSubtitles-элементы получают его в Task 6.

- [ ] **Step 4: Тесты и сборка фронта**

Run: `cd /Users/vintikzzzz/Projects/webtor/web-ui && npm test 2>&1 | tail -10 && npm run build 2>&1 | tail -5`
Expected: тесты PASS, сборка без ошибок.

- [ ] **Step 5: Ручная проверка в браузере**

Собрать и запустить web-ui локально по README проекта (или на стейдже `web-stage` после деплоя владельцем), открыть любой стрим с субтитрами, в консоли: `umami` отсутствует локально — заменить проверкой `window.umami = { track: (...a) => console.log('umami', ...a) }` до старта плеера. Ожидание: через 5 с воспроизведения одно `subtitle-resolved` с `level`, `hasUiLang`, `count`; клик по дорожке даёт `subtitle-select` с `provider`/`srclang`/`source`.

- [ ] **Step 6: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && git status -sb && git add assets/src/js/lib/player/subtitle-telemetry.js assets/src/js/lib/player/subtitle-telemetry.test.js assets/src/js/lib/player/Player.jsx && git commit -m "player: subtitle-resolved and subtitle-select telemetry for the subtitle ladder"
```

Собранные файлы в `assets/dist/` коммитятся так же, как это делается в проекте сейчас: посмотреть `git log --stat -3 -- assets/dist | head` и повторить практику.

---

### Task 8: Верификация сквозняком и заметка для деплоя

**Files:**
- Modify: `web-ui/docs/superpowers/specs/2026-09-12-auto-subtitles-design.md` — раздел «Открытые вопросы», пункт 3 закрыть.

- [ ] **Step 1: Проверить прохождение query-параметров через THP до деплоя web-ui**

video-info уже читает `imdb-id` из query проксированного запроса, значит `season`/`episode` пройдут тем же путём. Убедиться на проде после деплоя video-info (Task 3) одним запросом: взять любой рабочий `~vi/subtitles.json?...token=...` URL из логов web-ui или собрать через rest-api, добавить `&imdb-id=tt0903747&season=1&episode=3`:

```bash
curl -s '<url>&imdb-id=tt0903747&season=1&episode=3' | python3 -c 'import sys,json; d=json.load(sys.stdin); print(len(d), d[0] if d else None)'
```

Expected: JSON с `"source": "imdb"` и `release` вида `S01E03`. Если параметры не доходят (пустой ответ при живом imdb) — это блокер, тогда rest-api должен пробрасывать `season`/`episode` в `BuildSubtitlesURL` (`rest-api/services/url_builder.go:607-617`) по образцу `imdb-id`, и web-ui передаёт их через `ExportResourceContent`. Это запасной путь, не делать заранее.

- [ ] **Step 2: Замер после деплоя обоих сервисов (владелец гонит `/deploy video-info`, `/deploy web`)**

Через сутки, Loki:

```
sum(count_over_time({namespace="webtor",app="video-info"} |= "subtitles.json" |= "completed handling" |= "size=3 " [24h]))
sum(count_over_time({namespace="webtor",app="video-info"} |= "subtitles.json" |= "completed handling" [24h]))
```

Expected: доля `size=3` (пустой ответ) падает с 84% заметно (цель по спеке — остаток для whisper виден); и Umami:

```sql
select d.string_value level, count(*) from website_event e join event_data d on d.website_event_id=e.event_id
where e.event_name='subtitle-resolved' and d.data_key='level' and e.created_at >= now() - interval '1 day' group by 1 order by 1;
```

- [ ] **Step 3: Обновить спеку**

В «Открытые вопросы» пункт 3 заменить на: «Закрыто фазой 1: imdb-id приходил только из embed-настроек и API; теперь web-ui подмешивает его из enrichment вместе с сезоном и серией».

- [ ] **Step 4: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && git add docs/superpowers/specs/2026-09-12-auto-subtitles-design.md && git commit -m "spec: auto-subtitles open question 3 closed by phase 1"
```

---

## Порядок и зависимости

1 → 2 → 3 (video-info, деплоится первым, обратно совместим: без новых параметров поведение прежнее плюс fallback и ранжирование).
4 → 5 → 6 → 7 (web-ui). Task 6 и 7 не зависят от video-info, но `source` в событиях будет пустым до его деплоя.
8 после деплоя обоих.

## Что сознательно не делается в фазе 1

- Проброс `season`/`episode` через rest-api (API-потребители и Stremio получают imdb-поиск без серии) — только если Step 1 Task 8 покажет, что query не проходит.
- Forced по `disposition` из ffprobe — требует поля в proto content-prober; пока эвристика по title.
- Переупорядочивание списка в модалке по уровням — UI не меняется, только фильтры и данные.
