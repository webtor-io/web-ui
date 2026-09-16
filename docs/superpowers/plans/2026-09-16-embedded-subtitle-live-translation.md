# Перевод встроенных субтитров потоком из транскодера — план имплементации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** AI-перевод для встроенных (MediaProbe) субтитровых дорожек: subtitle-translate читает живой HLS-плейлист субтитров текущей транскодер-сессии и переводит cue по мере их появления; web-ui даёт встроенной дорожке `Src` на этот плейлист и учитывает live-прогресс.

**Architecture:** В subtitle-translate появляется второй тип источника — медиаплейлист (`X-Source-Url` оканчивается на `.m3u8`). Для него `Runner` работает в live-режиме: опрашивает плейлист, скачивает новые VTT-сегменты, складывает cue в растущий `LiveDoc` (дедуп по времени+тексту, сдвиг на `#EXT-X-SESSION-OFFSET`), переводит пачками по накоплению или таймеру, отдаёт `X-Subtitle-Live: 1` пока плейлист живой. Финал в S3 пишется только для непрерывного прогона с начала до `#EXT-X-ENDLIST`. Ключ артефакта нормализуется — `/session/<id>` вырезается из `X-Path`. Задача сама останавливается, когда её никто не опрашивает (иначе она держала бы ffmpeg живым после ухода зрителя). В web-ui `SubtitleOpts` получает базу сессии, `GetSubtitles` ставит встроенной дорожке `Src = <base>/s<MPID>.m3u8?<query>`, `pickTranslationSource` перестаёт исключать MediaProbe, клиент читает `X-Subtitle-Live`. THP и content-transcoder не меняются (проверено на стенде 16.09).

**Tech Stack:** Go 1.26 (оба репо), `go-astisub` (уже в сервисе), `lazymap`, `common-services`; web-ui: Go html/template, Preact, `node --test`.

**Spec:** `web-ui/docs/superpowers/specs/2026-09-16-embedded-subtitle-translation-design.md` — раздел «Как транскодер отдаёт субтитры сегодня» содержит проверенные факты стенда, «Что меняется» — контракт. При расхождении плана со спекой побеждает спека; при расхождении спеки с фактами стенда — факты стенда (они в спеке помечены датой).

## Global Constraints

- Репозитории: сервис — `/Users/vintikzzzz/Projects/webtor/subtitle-translate` (ветка `main`), web-ui — `/Users/vintikzzzz/Projects/webtor/web-ui` (ветка для этой работы: `embedded-live-translation` от `main`). В web-ui **никогда** `go test ./...` — только `make test` или `LD=… go test <pkg>` (см. `Makefile`, proto-конфликт глушится ldflag'ом). Никаких `git checkout`/`git restore`/`git stash` над незакоммиченными файлами; `git add` только явными путями.
- Формат плейлиста (стенд 16.09): `<file>~hls/session/<id>/s<N>.m3u8`, сегменты `s<N>-<k>.vtt` (URI относительные, транскодер сам дописывает query), `#EXT-X-SESSION-OFFSET:<секунды>` (целое, квант 30 с), `#EXT-X-PLAYLIST-TYPE:EVENT`, `#EXT-X-ENDLIST` появляется только по завершении ffmpeg-прогона; в сегментах **нет** `X-TIMESTAMP-MAP`, время cue — от начала прогона. Абсолютное время = cue + offset. Мёртвая сессия → 404 (`session not found`), лимит рестартов ffmpeg → 503.
- Заголовки ответа сервиса: `X-Subtitle-Progress: done/total` как сейчас; **новый** `X-Subtitle-Live: 1` пока источник живой (нет `ENDLIST` или задача ещё работает); `Access-Control-Expose-Headers: X-Subtitle-Progress, X-Subtitle-Live`. Финальный артефакт — без `Live`, с `Cache-Control: public, max-age=86400`; частичный — `no-store`.
- Ключ артефакта: `ArtifactKey(infoHash, KeyPath(X-Path), lang, model, PromptVersion)`, где `KeyPath` вырезает `/session/<32 hex>` из пути (`/a.mkv~hls/session/54269b3a…/s0.m3u8` → `/a.mkv~hls/s0.m3u8`). Для не-HLS путей `KeyPath` — тождество.
- Новые флаги сервиса (urfave/cli v1, регистрация в `configure.go`): `--live-poll-interval` (`SUBTITLE_TRANSLATE_LIVE_POLL_INTERVAL`, секунды, 4), `--live-batch-wait` (`SUBTITLE_TRANSLATE_LIVE_BATCH_WAIT`, секунды, 10), `--live-idle` (`SUBTITLE_TRANSLATE_LIVE_IDLE`, секунды, 90 — задача останавливается, если ключ не опрашивали дольше). Существующие `--max-cues`/`--max-source-bytes` применяются к накопленному документу и суммарному размеру сегментов.
- В логах и метриках нет текста реплик; в публичных репо нет имён вендоров (только «upstream»).
- Порядок строк в `Progress.Lines` = порядок cue по возрастанию `Start` **на момент добавления**: live-документ только дописывается в конец (новые cue всегда позже уже известных в пределах одного прогона; после перемотки назад cue с меньшим временем тоже дописываются в конец — `Render` сортирует по времени при выдаче, см. Task 2).
- Коммиты с трейлерами:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01VwLULKCA6QwcXFJVLFrqJ9
  ```

---

## Карта файлов

### subtitle-translate

| Файл | Ответственность |
|---|---|
| `services/hls_playlist.go` (new) | Парсер медиаплейлиста: offset, ended, список сегментов с абсолютными URL; `KeyPath` |
| `services/hls_playlist_test.go` (new) | Тесты парсера на тексте плейлиста со стенда и `KeyPath` |
| `services/live_doc.go` (new) | `LiveDoc` — растущий документ cue с дедупом и сдвигом, снимок в `*Doc` |
| `services/live_doc_test.go` (new) | Тесты дедупа, сдвига, снимка, лимитов |
| `services/live_source.go` (new) | `LiveSource` — опрос плейлиста и скачивание новых сегментов в `LiveDoc`, учёт (offset, uri), `ErrSourceGone` |
| `services/live_source_test.go` (new) | Тесты на `httptest`-сервере с «растущим» плейлистом, 404, смена offset |
| `services/store.go` | `Progress` получает `CueKeys []string` и `Live bool` |
| `services/job.go` | `Job.Live *LiveSource`, `Runner.runLive`, `Runner.Touch/lastSeen`, `Snapshot.Live`, `Runner.LiveSnapshot` |
| `services/job_test.go` | Тесты live-режима: батч по таймеру, по размеру, финал только при полном прогоне, остановка по idle и по `source_gone`, резюм по `CueKeys` |
| `services/web.go` | Детект `.m3u8`, кэш `LiveSource` по ключу, `X-Subtitle-Live`, `KeyPath` в ключе, `Touch` на каждом GET/HEAD |
| `services/handler_test.go` | Тесты handler'а для live-источника |
| `configure.go` | Три новых флага → `Runner`/`Handler` |
| `README.md` | Разделы «Called by torrent-http-proxy» (новый заголовок, статусы), «Live HLS source» (new), «Flags» |

### web-ui

| Файл | Ответственность |
|---|---|
| `models/subtitle_opts.go` | `SubtitleOpts.HLSSessionBase string` |
| `jobs/scripts/action.go` | Заполнить `sc.SubtitleOpts.HLSSessionBase` после создания сессии |
| `handlers/action/helper.go` | `Src` у видимых MediaProbe-элементов; `pickTranslationSource` без исключения MediaProbe |
| `handlers/action/helper_test.go` | Тесты Src/источника + негативный контроль |
| `assets/src/js/lib/player/subtitle-progress.js` | `parseProgress(header, live)`, `pollProgress` читает `X-Subtitle-Live`, отдаёт `p.live` |
| `assets/src/js/lib/player/subtitle-progress.test.js` | Тесты live-семантики |
| `assets/src/js/app/action/Player.jsx` (путь уточнить `find assets/src/js -name Player.jsx`) | Текст прогресса при live: `· <done>` и title `player.subtitleTranslatingLive` |
| `locales/*.json` (11) | Ключ `player.subtitleTranslatingLive` |
| `docs/subtitle_translate.md`, `CLAUDE.md` (только если меняется список флагов/классов — не ожидается) | Документация |

---

## Task 1: Парсер медиаплейлиста и `KeyPath` (subtitle-translate)

**Files:**
- Create: `services/hls_playlist.go`
- Test: `services/hls_playlist_test.go`

**Interfaces:**
- Produces:
  ```go
  type SegmentRef struct {
      URI      string        // абсолютный URL сегмента (резолв относительно URL плейлиста, query сохраняется как есть)
      Name     string        // путь без query, например "s0-3.vtt" — идентичность сегмента внутри прогона
      Duration time.Duration // EXTINF
  }
  type MediaPlaylist struct {
      Offset   time.Duration // #EXT-X-SESSION-OFFSET, 0 если тега нет
      Ended    bool          // #EXT-X-ENDLIST присутствует
      Segments []SegmentRef
  }
  func ParseMediaPlaylist(playlistURL string, data []byte) (*MediaPlaylist, error)
  func KeyPath(p string) string
  ```
  `ParseMediaPlaylist` возвращает ошибку, если тело не начинается с `#EXTM3U` или содержит `#EXT-X-STREAM-INF` (мастер вместо медиаплейлиста).

- [ ] **Step 1: Тест парсера на тексте со стенда**

```go
package services

import (
	"testing"
	"time"
)

const stagePlaylist = `#EXTM3U
#EXT-X-SESSION-OFFSET:30
#EXT-X-START:TIME-OFFSET=0
#EXT-X-VERSION:3
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:EVENT
#EXT-X-TARGETDURATION:64
#EXTINF:63.699000,
s0-0.vtt?api-key=K&token=T
#EXTINF:1.375000,
s0-1.vtt?api-key=K&token=T
`

func TestParseMediaPlaylistStageShape(t *testing.T) {
	p, err := ParseMediaPlaylist("https://edge.example/h/a.mkv~hls/session/abc/s0.m3u8?api-key=K&token=T", []byte(stagePlaylist))
	if err != nil {
		t.Fatal(err)
	}
	if p.Offset != 30*time.Second || p.Ended {
		t.Fatalf("offset=%v ended=%v", p.Offset, p.Ended)
	}
	if len(p.Segments) != 2 {
		t.Fatalf("segments: %d", len(p.Segments))
	}
	if p.Segments[0].URI != "https://edge.example/h/a.mkv~hls/session/abc/s0-0.vtt?api-key=K&token=T" {
		t.Fatalf("uri: %s", p.Segments[0].URI)
	}
	if p.Segments[0].Name != "s0-0.vtt" || p.Segments[1].Name != "s0-1.vtt" {
		t.Fatalf("names: %q %q", p.Segments[0].Name, p.Segments[1].Name)
	}
	if d := p.Segments[0].Duration; d < 63*time.Second || d > 64*time.Second {
		t.Fatalf("duration: %v", d)
	}
}

func TestParseMediaPlaylistEnded(t *testing.T) {
	p, err := ParseMediaPlaylist("https://e/x/s0.m3u8", []byte("#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\ns0-0.vtt\n#EXT-X-ENDLIST\n"))
	if err != nil || !p.Ended || p.Offset != 0 {
		t.Fatalf("p=%+v err=%v", p, err)
	}
}

func TestParseMediaPlaylistRejectsMasterAndGarbage(t *testing.T) {
	if _, err := ParseMediaPlaylist("https://e/x/index.m3u8", []byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nv0.m3u8\n")); err == nil {
		t.Fatal("master playlist must be rejected")
	}
	if _, err := ParseMediaPlaylist("https://e/x/s0.m3u8", []byte("WEBVTT\n")); err == nil {
		t.Fatal("non-m3u8 must be rejected")
	}
}

func TestKeyPathStripsSession(t *testing.T) {
	cases := map[string]string{
		"/a.mkv~hls/session/54269b3aaa44b7a7419a01301cd046dc/s0.m3u8": "/a.mkv~hls/s0.m3u8",
		"/a.mkv~hls/s0.m3u8":                 "/a.mkv~hls/s0.m3u8",
		"/movie.srt~vtt/movie.vtt":           "/movie.srt~vtt/movie.vtt",
		"/dir/session/notahex/s0.m3u8~hls/x": "/dir/session/notahex/s0.m3u8~hls/x",
	}
	for in, want := range cases {
		if got := KeyPath(in); got != want {
			t.Fatalf("KeyPath(%q)=%q want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Прогнать — падает на отсутствии символов**

Run: `cd /Users/vintikzzzz/Projects/webtor/subtitle-translate && go test ./services/ -run 'TestParseMediaPlaylist|TestKeyPath' 2>&1 | head -5`
Expected: FAIL, `undefined: ParseMediaPlaylist`.

- [ ] **Step 3: Реализация**

```go
package services

import (
	"bufio"
	"bytes"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// SegmentRef is one entry of a media playlist. Name is the identity of the
// segment inside one FFmpeg run (the transcoder numbers segments from 0 on
// every run, so Name alone is not unique across seeks — pair it with the
// playlist's Offset).
type SegmentRef struct {
	URI      string
	Name     string
	Duration time.Duration
}

// MediaPlaylist is the part of an HLS media playlist the live source needs.
type MediaPlaylist struct {
	Offset   time.Duration
	Ended    bool
	Segments []SegmentRef
}

// ParseMediaPlaylist reads the transcoder's subtitle playlist. It refuses a
// master playlist: the caller must point at the s<N>.m3u8 variant itself.
func ParseMediaPlaylist(playlistURL string, data []byte) (*MediaPlaylist, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("#EXTM3U")) {
		return nil, errors.New("not an m3u8 playlist")
	}
	base, err := url.Parse(playlistURL)
	if err != nil {
		return nil, errors.Wrap(err, "bad playlist url")
	}
	p := &MediaPlaylist{}
	var pending time.Duration
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF"):
			return nil, errors.New("master playlist given where a media playlist was expected")
		case strings.HasPrefix(line, "#EXT-X-SESSION-OFFSET:"):
			sec, perr := strconv.ParseFloat(strings.TrimPrefix(line, "#EXT-X-SESSION-OFFSET:"), 64)
			if perr != nil {
				return nil, errors.Wrap(perr, "bad session offset")
			}
			p.Offset = time.Duration(sec * float64(time.Second))
		case line == "#EXT-X-ENDLIST":
			p.Ended = true
		case strings.HasPrefix(line, "#EXTINF:"):
			v := strings.TrimPrefix(line, "#EXTINF:")
			if i := strings.IndexByte(v, ','); i >= 0 {
				v = v[:i]
			}
			sec, perr := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if perr != nil {
				return nil, errors.Wrap(perr, "bad EXTINF")
			}
			pending = time.Duration(sec * float64(time.Second))
		case strings.HasPrefix(line, "#"):
			continue
		default:
			ref, perr := url.Parse(line)
			if perr != nil {
				return nil, errors.Wrap(perr, "bad segment uri")
			}
			abs := base.ResolveReference(ref)
			p.Segments = append(p.Segments, SegmentRef{URI: abs.String(), Name: path.Base(abs.Path), Duration: pending})
			pending = 0
		}
	}
	if err := sc.Err(); err != nil {
		return nil, errors.Wrap(err, "read playlist")
	}
	return p, nil
}

var sessionPathRe = regexp.MustCompile(`~hls/session/[0-9a-f]{32}/`)

// KeyPath is X-Path with the transcoder session id removed, so every
// session of the same file and stream shares one artifact key. Paths
// without a session segment are returned unchanged.
func KeyPath(p string) string {
	return sessionPathRe.ReplaceAllString(p, "~hls/")
}
```

Проверить длину id сессии: `grep -n 'func (m \*SessionManager) Create' -A 15 /Users/vintikzzzz/Projects/webtor/content-transcoder/services/session_manager.go` — если id не 32 hex, поправить regexp и тест (`54269b3aaa44b7a7419a01301cd046dc` со стенда — 32 hex).

- [ ] **Step 4: Прогнать — зелено**

Run: `go test ./services/ -run 'TestParseMediaPlaylist|TestKeyPath' -v 2>&1 | tail -8`
Expected: PASS ×4.

- [ ] **Step 5: Негативный контроль** — временно заменить в `KeyPath` `{32}` на `{33}`, убедиться, что `TestKeyPathStripsSession` краснеет, вернуть.

- [ ] **Step 6: Commit**

```bash
cd /Users/vintikzzzz/Projects/webtor/subtitle-translate && git add services/hls_playlist.go services/hls_playlist_test.go && git commit -q -F - <<'EOF'
hls: parse the transcoder's subtitle media playlist and normalize session paths

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VwLULKCA6QwcXFJVLFrqJ9
EOF
```

---

## Task 2: `LiveDoc` — растущий документ cue (subtitle-translate)

**Files:**
- Create: `services/live_doc.go`
- Test: `services/live_doc_test.go`
- Read first: `services/vtt.go` (`Doc`, `Cue`, `ParseVTT`, `Normalize`, `Render`)

**Interfaces:**
- Consumes: `ParseVTT`, `Doc.Normalize`, `Cue`.
- Produces:
  ```go
  type LiveDoc struct { /* mu, cues []Cue, items []*astisub.Item, keys map[string]int, bytes int64 */ }
  func NewLiveDoc() *LiveDoc
  // AddSegment parses one WebVTT segment, shifts every cue by offset,
  // normalizes, appends the cues not seen before. Returns how many were added.
  func (d *LiveDoc) AddSegment(offset time.Duration, vtt []byte) (int, error)
  func (d *LiveDoc) Len() int
  func (d *LiveDoc) Bytes() int64          // сумма len(vtt) всех принятых сегментов
  func (d *LiveDoc) Keys() []string        // CueKey каждого cue по индексу
  func (d *LiveDoc) Snapshot() *Doc        // копия в виде обычного Doc (Cues + Items), cue отсортированы по Start, порядок индексов сохранён в Doc.Cues[i].Index
  func CueKey(start, end time.Duration, lines []string) string   // start|end|joined-normalized-text
  ```
  Важно: `Progress.Lines[i]` соответствует **порядку добавления** (индексу в `LiveDoc`), а `Snapshot()` возвращает `Doc`, чей `Cues[j].Index` = индекс добавления. `Render` в live-режиме должен брать `translated[c.Index]`. Поскольку текущий `Doc.Render(translated, upTo)` индексирует `translated[i]` позиционно, добавить в `vtt.go` метод `func (d *Doc) RenderByIndex(translated []string) ([]byte, error)`, который для каждого `d.Cues[i]` берёт `translated[d.Cues[i].Index]` (если `Index` в диапазоне и непустой), иначе нормализованные строки; `upTo` не нужен — live отдаёт все известные cue (непереведённые — оригиналом, чтобы зритель видел хотя бы исходный текст? **Нет**: спека требует не показывать непереведённые как перевод. Решение: непереведённые cue в live-снимке **пропускаются** — `RenderByIndex` включает только cue с непустым перевода или структурно пустые). Это отличие от `Render` (там префикс до `upTo`) — задокументировать в комментарии.

- [ ] **Step 1: Тесты**

```go
package services

import (
	"strings"
	"testing"
	"time"
)

const seg0 = "WEBVTT\n\n00:45.107 --> 01:03.699\nМакс.\n"
const seg1 = "WEBVTT\n\n01:03.699 --> 01:05.000\nПривет.\n\n01:05.000 --> 01:06.000\n[door slams]\n"

func TestLiveDocAppendsAndDedups(t *testing.T) {
	d := NewLiveDoc()
	n, err := d.AddSegment(0, []byte(seg0))
	if err != nil || n != 1 || d.Len() != 1 {
		t.Fatalf("n=%d len=%d err=%v", n, d.Len(), err)
	}
	// The same segment again (a re-read playlist) adds nothing.
	if n, _ := d.AddSegment(0, []byte(seg0)); n != 0 || d.Len() != 1 {
		t.Fatalf("dup: n=%d len=%d", n, d.Len())
	}
	// A cue that normalizes to nothing still counts (keeps its slot).
	if n, _ := d.AddSegment(0, []byte(seg1)); n != 2 || d.Len() != 3 {
		t.Fatalf("seg1: n=%d len=%d", n, d.Len())
	}
	if got := d.Bytes(); got != int64(2*len(seg0)+len(seg1)) {
		t.Fatalf("bytes=%d", got)
	}
	keys := d.Keys()
	if len(keys) != 3 || keys[0] == keys[1] {
		t.Fatalf("keys=%v", keys)
	}
}

func TestLiveDocShiftsByOffset(t *testing.T) {
	d := NewLiveDoc()
	if _, err := d.AddSegment(30*time.Second, []byte(seg0)); err != nil {
		t.Fatal(err)
	}
	snap := d.Snapshot()
	if snap.Cues[0].Start != 75107*time.Millisecond {
		t.Fatalf("start=%v", snap.Cues[0].Start)
	}
	// The same text at the same movie time from a different run (offset 0,
	// cue at 1:15.107) is the same cue.
	same := "WEBVTT\n\n01:15.107 --> 01:33.699\nМакс.\n"
	if n, _ := d.AddSegment(0, []byte(same)); n != 0 {
		t.Fatalf("cross-run dup added: %d", n)
	}
}

func TestLiveDocSnapshotSortsButKeepsIndex(t *testing.T) {
	d := NewLiveDoc()
	_, _ = d.AddSegment(60*time.Second, []byte(seg0)) // added first, later in time
	_, _ = d.AddSegment(0, []byte(seg1))              // added second, earlier in time
	snap := d.Snapshot()
	if snap.Cues[0].Index != 1 || snap.Cues[len(snap.Cues)-1].Index != 0 {
		t.Fatalf("order: %+v", snap.Cues)
	}
	body, err := snap.RenderByIndex([]string{"PT:Макс.", "PT:Привет.", ""})
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if strings.Index(s, "PT:Привет.") > strings.Index(s, "PT:Макс.") {
		t.Fatalf("render not time-ordered:\n%s", s)
	}
}

func TestRenderByIndexSkipsUntranslated(t *testing.T) {
	d := NewLiveDoc()
	_, _ = d.AddSegment(0, []byte(seg1)) // "Привет." then a structurally empty cue
	snap := d.Snapshot()
	body, err := snap.RenderByIndex([]string{"", ""})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "Привет") {
		t.Fatalf("untranslated cue leaked into the live render:\n%s", body)
	}
	body, _ = snap.RenderByIndex([]string{"PT:Привет.", ""})
	if !strings.Contains(string(body), "PT:Привет.") {
		t.Fatalf("translated cue missing:\n%s", body)
	}
}
```

- [ ] **Step 2: Прогнать — падает** (`undefined: NewLiveDoc`).

- [ ] **Step 3: Реализация `live_doc.go` + `RenderByIndex` в `vtt.go`**

```go
package services

import (
	"bytes"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/asticode/go-astisub"
	"github.com/pkg/errors"
)

// LiveDoc accumulates cues from a stream of WebVTT segments. Segments of
// one FFmpeg run carry times from the run's start; offset is that run's
// #EXT-X-SESSION-OFFSET, so every cue is stored in movie time. The same
// cue reached twice (a re-read playlist, or a seek that replays a range)
// is stored once: identity is movie time plus normalized text.
type LiveDoc struct {
	mu    sync.Mutex
	cues  []Cue
	items []*astisub.Item
	keys  map[string]int
	bytes int64
}

func NewLiveDoc() *LiveDoc { return &LiveDoc{keys: map[string]int{}} }

// CueKey is the identity of a cue across runs and re-reads.
func CueKey(start, end time.Duration, lines []string) string {
	return strconv.FormatInt(int64(start/time.Millisecond), 10) + "|" +
		strconv.FormatInt(int64(end/time.Millisecond), 10) + "|" + strings.Join(lines, "\n")
}

func (d *LiveDoc) AddSegment(offset time.Duration, vtt []byte) (int, error) {
	doc, err := ParseVTT(bytes.NewReader(vtt))
	if err != nil {
		return 0, err
	}
	doc.Normalize()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bytes += int64(len(vtt))
	added := 0
	for i, c := range doc.Cues {
		c.Start += offset
		c.End += offset
		key := CueKey(c.Start, c.End, c.Lines)
		if _, seen := d.keys[key]; seen {
			continue
		}
		it := doc.Items.Items[i]
		item := &astisub.Item{StartAt: c.Start, EndAt: c.End, InlineStyle: it.InlineStyle, Region: it.Region, Style: it.Style, Lines: it.Lines}
		c.Index = len(d.cues)
		d.keys[key] = c.Index
		d.cues = append(d.cues, c)
		d.items = append(d.items, item)
		added++
	}
	return added, nil
}

func (d *LiveDoc) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.cues)
}

func (d *LiveDoc) Bytes() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bytes
}

func (d *LiveDoc) Keys() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.cues))
	for _, c := range d.cues {
		out[c.Index] = CueKey(c.Start, c.End, c.Lines)
	}
	return out
}

// Snapshot is the document as it stands, cues in movie-time order. Each
// Cue keeps Index = its position in the append order, which is the index
// into Progress.Lines.
func (d *LiveDoc) Snapshot() *Doc {
	d.mu.Lock()
	defer d.mu.Unlock()
	idx := make([]int, len(d.cues))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return d.cues[idx[a]].Start < d.cues[idx[b]].Start })
	out := &Doc{Items: &astisub.Subtitles{}}
	for _, i := range idx {
		out.Cues = append(out.Cues, d.cues[i])
		out.Items.Items = append(out.Items.Items, d.items[i])
	}
	return out
}
```

В `vtt.go` добавить:

```go
// RenderByIndex is Render for a live document: translated is indexed by
// Cue.Index (the append order), cues are written in the document's own
// order, and a cue without a translation yet is left out entirely rather
// than shown in the source language — the viewer asked for a translation,
// and a source-language line under an AI chip reads as a wrong one.
// Structurally empty cues (nothing after normalization) are kept as empty
// cues, as Render does.
func (d *Doc) RenderByIndex(translated []string) ([]byte, error) {
	out := &astisub.Subtitles{Metadata: d.Items.Metadata, Styles: d.Items.Styles, Regions: d.Items.Regions}
	for i, c := range d.Cues {
		src := d.Items.Items[i]
		var lines []astisub.Line
		switch {
		case c.Index < len(translated) && strings.TrimSpace(translated[c.Index]) != "":
			for _, t := range SplitLines(translated[c.Index]) {
				lines = append(lines, astisub.Line{Items: []astisub.LineItem{{Text: t}}})
			}
		case len(c.Lines) == 0:
			// keep the empty slot
		default:
			continue
		}
		out.Items = append(out.Items, &astisub.Item{StartAt: src.StartAt, EndAt: src.EndAt, InlineStyle: src.InlineStyle, Region: src.Region, Style: src.Style, Lines: lines})
	}
	return writeVTTDoc(out)
}
```

где `writeVTTDoc` — вынесенный из хвоста `Render` кусок (буфер, пустой документ → `WEBVTT\n`, `out.WriteToWebVTT`). Посмотреть хвост `Render` (`sed -n 125,150p services/vtt.go`) и сделать так, чтобы оба метода вызывали один хелпер.

- [ ] **Step 4: Прогнать** `go test ./services/ -run 'TestLiveDoc|TestRenderByIndex|TestRender' -v 2>&1 | tail -12` — PASS, старые `TestRender*` не тронуты.

- [ ] **Step 5: Негативный контроль** — убрать `default: continue` в `RenderByIndex` → `TestRenderByIndexSkipsUntranslated` краснеет; вернуть.

- [ ] **Step 6: Commit** (`services/live_doc.go services/live_doc_test.go services/vtt.go`), сообщение `live: accumulate cues from webvtt segments in movie time`.

---

## Task 3: `LiveSource` — опрос плейлиста (subtitle-translate)

**Files:**
- Create: `services/live_source.go`
- Test: `services/live_source_test.go`

**Interfaces:**
- Consumes: `ParseMediaPlaylist`, `LiveDoc`.
- Produces:
  ```go
  var ErrSourceGone = errors.New("source gone")      // 404/503 на плейлист или сегмент
  var ErrSourceTooLarge = errors.New("source too large") // байты/cue сверх лимита

  type LiveSource struct { /* url, client, doc *LiveDoc, seen map[string]bool (key offset|name), maxBytes int64, maxCues int, mu, ended bool, offsets map[time.Duration]bool, contiguous bool */ }
  func NewLiveSource(playlistURL string, client *http.Client, maxBytes int64, maxCues int) *LiveSource
  type Refresh struct { Added int; Ended bool }
  // Refresh reads the playlist once and fetches every segment not seen
  // before (in playlist order). It returns ErrSourceGone on a 404/503 for
  // the playlist, ErrSourceTooLarge past the caps; other fetch errors are
  // returned as-is (transient: the caller retries next tick).
  func (s *LiveSource) Refresh(ctx context.Context) (Refresh, error)
  func (s *LiveSource) Doc() *LiveDoc
  func (s *LiveSource) Ended() bool
  // Contiguous reports whether everything seen so far came from runs with
  // offset 0 only — the one case a final artifact may be written from.
  func (s *LiveSource) Contiguous() bool
  ```
  Каждый запрос — с таймаутом `sourceFetchTimeout` (уже есть, 30 с). Сегменты скачиваются последовательно (порядок плейлиста). `seen` — по `fmt.Sprintf("%d|%s", offset/time.Millisecond, name)`.

- [ ] **Step 1: Тесты на `httptest`**

```go
package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// livePlaylistServer serves a playlist whose body the test swaps at will,
// and segments from a map. Requests are counted per path.
type livePlaylistServer struct {
	mu       sync.Mutex
	playlist string
	status   int
	segments map[string]string
	hits     map[string]int
	srv      *httptest.Server
}

func newLivePlaylistServer(t *testing.T) *livePlaylistServer {
	t.Helper()
	s := &livePlaylistServer{status: 200, segments: map[string]string{}, hits: map[string]int{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.hits[r.URL.Path]++
		if r.URL.Path == "/h/a.mkv~hls/session/0123456789abcdef0123456789abcdef/s0.m3u8" {
			if s.status != 200 {
				w.WriteHeader(s.status)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte(s.playlist))
			return
		}
		if body, ok := s.segments[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "text/vtt")
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *livePlaylistServer) set(playlist string, segs map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.playlist = playlist
	for k, v := range segs {
		s.segments["/h/a.mkv~hls/session/0123456789abcdef0123456789abcdef/"+k] = v
	}
}

func (s *livePlaylistServer) url() string {
	return s.srv.URL + "/h/a.mkv~hls/session/0123456789abcdef0123456789abcdef/s0.m3u8?token=T"
}

const pl1 = "#EXTM3U\n#EXT-X-SESSION-OFFSET:0\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:EVENT\n#EXT-X-TARGETDURATION:64\n#EXTINF:63.7,\ns0-0.vtt?token=T\n"
const pl2 = pl1 + "#EXTINF:1.3,\ns0-1.vtt?token=T\n"
const pl2end = pl2 + "#EXT-X-ENDLIST\n"

func TestLiveSourceFetchesOnlyNewSegments(t *testing.T) {
	srv := newLivePlaylistServer(t)
	srv.set(pl1, map[string]string{"s0-0.vtt": seg0, "s0-1.vtt": seg1})
	ls := NewLiveSource(srv.url(), srv.srv.Client(), 1<<20, 5000)
	r, err := ls.Refresh(context.Background())
	if err != nil || r.Added != 1 || r.Ended {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	srv.set(pl2, nil)
	r, err = ls.Refresh(context.Background())
	if err != nil || r.Added != 2 || r.Ended || ls.Doc().Len() != 3 {
		t.Fatalf("r=%+v len=%d err=%v", r, ls.Doc().Len(), err)
	}
	srv.set(pl2end, nil)
	r, err = ls.Refresh(context.Background())
	if err != nil || r.Added != 0 || !r.Ended || !ls.Ended() {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.hits["/h/a.mkv~hls/session/0123456789abcdef0123456789abcdef/s0-0.vtt"] != 1 {
		t.Fatalf("segment 0 fetched %d times", srv.hits["/h/a.mkv~hls/session/0123456789abcdef0123456789abcdef/s0-0.vtt"])
	}
	if !ls.Contiguous() {
		t.Fatal("offset-0-only run must be contiguous")
	}
}

func TestLiveSourceSeekIsANewRun(t *testing.T) {
	srv := newLivePlaylistServer(t)
	srv.set(pl1, map[string]string{"s0-0.vtt": seg0})
	ls := NewLiveSource(srv.url(), srv.srv.Client(), 1<<20, 5000)
	if _, err := ls.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// After a seek the transcoder restarts numbering at s0-0 with a new
	// offset and new content: the same Name must be fetched again.
	seek := "#EXTM3U\n#EXT-X-SESSION-OFFSET:600\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:2.0,\ns0-0.vtt?token=T\n"
	srv.set(seek, map[string]string{"s0-0.vtt": "WEBVTT\n\n00:01.000 --> 00:02.000\nПозже.\n"})
	r, err := ls.Refresh(context.Background())
	if err != nil || r.Added != 1 {
		t.Fatalf("r=%+v err=%v", r, err)
	}
	snap := ls.Doc().Snapshot()
	if snap.Cues[len(snap.Cues)-1].Start != 601*time.Second {
		t.Fatalf("seeked cue at %v", snap.Cues[len(snap.Cues)-1].Start)
	}
	if ls.Contiguous() {
		t.Fatal("a run with offset 600 breaks contiguity")
	}
}

func TestLiveSourceGoneAndTooLarge(t *testing.T) {
	srv := newLivePlaylistServer(t)
	srv.set(pl1, map[string]string{"s0-0.vtt": seg0})
	ls := NewLiveSource(srv.url(), srv.srv.Client(), 10, 5000) // 10 bytes cap
	if _, err := ls.Refresh(context.Background()); !errors.Is(err, ErrSourceTooLarge) {
		t.Fatalf("want too large, got %v", err)
	}
	srv.mu.Lock()
	srv.status = 404
	srv.mu.Unlock()
	ls2 := NewLiveSource(srv.url(), srv.srv.Client(), 1<<20, 5000)
	if _, err := ls2.Refresh(context.Background()); !errors.Is(err, ErrSourceGone) {
		t.Fatalf("want gone, got %v", err)
	}
}
```

- [ ] **Step 2: Прогнать — падает** (`undefined: NewLiveSource`).

- [ ] **Step 3: Реализация**

```go
package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/pkg/errors"
)

var (
	ErrSourceGone     = errors.New("source gone")
	ErrSourceTooLarge = errors.New("source too large")
)

type Refresh struct {
	Added int
	Ended bool
}

// LiveSource follows one subtitle media playlist of a transcoder session.
type LiveSource struct {
	url      string
	client   *http.Client
	doc      *LiveDoc
	maxBytes int64
	maxCues  int

	mu         sync.Mutex
	seen       map[string]bool
	ended      bool
	contiguous bool
}

func NewLiveSource(playlistURL string, client *http.Client, maxBytes int64, maxCues int) *LiveSource {
	return &LiveSource{url: playlistURL, client: client, doc: NewLiveDoc(), maxBytes: maxBytes, maxCues: maxCues, seen: map[string]bool{}, contiguous: true}
}

func (s *LiveSource) Doc() *LiveDoc { return s.doc }

func (s *LiveSource) Ended() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended
}

func (s *LiveSource) Contiguous() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.contiguous
}

func (s *LiveSource) get(ctx context.Context, u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, sourceFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusServiceUnavailable:
		// 404: the session is gone (viewer left). 503: the transcoder hit
		// its restart budget for this session. Neither comes back.
		return nil, errors.Wrapf(ErrSourceGone, "status %d", res.StatusCode)
	default:
		return nil, errors.Errorf("source returned %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, s.maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > s.maxBytes {
		return nil, ErrSourceTooLarge
	}
	return data, nil
}

func (s *LiveSource) Refresh(ctx context.Context) (Refresh, error) {
	data, err := s.get(ctx, s.url)
	if err != nil {
		return Refresh{}, err
	}
	pl, err := ParseMediaPlaylist(s.url, data)
	if err != nil {
		return Refresh{}, err
	}
	var out Refresh
	for _, seg := range pl.Segments {
		key := fmt.Sprintf("%d|%s", pl.Offset/time.Millisecond, seg.Name)
		s.mu.Lock()
		dup := s.seen[key]
		s.mu.Unlock()
		if dup {
			continue
		}
		body, err := s.get(ctx, seg.URI)
		if err != nil {
			return out, err
		}
		if s.doc.Bytes()+int64(len(body)) > s.maxBytes {
			return out, ErrSourceTooLarge
		}
		n, err := s.doc.AddSegment(pl.Offset, body)
		if err != nil {
			return out, err
		}
		if s.doc.Len() > s.maxCues {
			return out, ErrSourceTooLarge
		}
		s.mu.Lock()
		s.seen[key] = true
		s.mu.Unlock()
		out.Added += n
	}
	s.mu.Lock()
	if pl.Offset != 0 {
		s.contiguous = false
	}
	if pl.Ended {
		s.ended = true
	}
	out.Ended = s.ended
	s.mu.Unlock()
	return out, nil
}
```

- [ ] **Step 4: Прогнать** `go test ./services/ -run TestLiveSource -v 2>&1 | tail -8` — PASS ×3.

- [ ] **Step 5: Негативный контроль** — в `Refresh` убрать проверку `dup` → `TestLiveSourceFetchesOnlyNewSegments` краснеет по счётчику hits; вернуть.

- [ ] **Step 6: Commit** (`services/live_source.go services/live_source_test.go`), `live: follow a subtitle media playlist and fetch new segments`.

---

## Task 4: Live-режим `Runner` (subtitle-translate)

**Files:**
- Modify: `services/store.go` (`Progress`), `services/job.go`
- Test: `services/job_test.go`

**Interfaces:**
- Consumes: `LiveSource`, `LiveDoc`, `RenderByIndex`, `Progress`, `Store`.
- Produces:
  ```go
  type Progress struct {
      Total   int
      Lines   []string
      CueKeys []string // live only: CueKey per index, so a restarted job reuses translations
      Live    bool     // live only: written while the playlist is live; cleared on the last write
  }
  type Job struct { Lang, SourceLang string; Glossary []string; Doc *Doc; Live *LiveSource }
  type LiveConfig struct { PollInterval, BatchWait, Idle time.Duration }
  func NewRunner(store Store, tr Translator, batchSize, maxJobs int, lockTTL time.Duration) *Runner   // как есть
  func (r *Runner) SetLive(cfg LiveConfig)          // вызывается из configure.go; нулевые поля → дефолты 4s/10s/90s
  func (r *Runner) Touch(key string)                // handler зовёт на каждом GET/HEAD; live-цикл читает
  type Snapshot struct { Body []byte; Done, Total int; Final bool; Live bool }
  func (r *Runner) LiveSnapshot(ctx context.Context, key string, src *LiveSource) (*Snapshot, error)
  ```
  Метрики: `JobErrors` с новыми label-значениями `source_gone`, `viewer_gone` (используются как причины завершения, не ошибки в смысле алерта — задокументировать в README «Metrics»).

**Алгоритм `runLive(ctx, key, job)`** (после того же захвата лока, что в `run`):
1. `p := GetProgress`; если `p == nil` → `p = &Progress{Live: true}`. Построить `known map[string]string` из `p.CueKeys[i] → p.Lines[i]` (непустые).
2. Цикл с `ticker(PollInterval)`; на каждом тике:
   - `ref, err := job.Live.Refresh(ctx)`; `ErrSourceGone` → лог `source gone`, `JobErrors{source_gone}++`, `p.Live=false`, `PutProgress`, return. `ErrSourceTooLarge` → `JobErrors{too_large}`, `p.Live=false`, `PutProgress`, return. Иная ошибка → лог Warn, continue (следующий тик).
   - Синхронизировать `p` с документом: `keys := doc.Keys()`; для `i` от `len(p.Lines)` до `len(keys)-1` — `p.Lines = append(p.Lines, known[keys[i]])` (пусто, если неизвестно), `p.CueKeys = append(p.CueKeys, keys[i])`; `p.Total = len(keys)`.
   - `pending` = индексы с `len(cue.Lines)>0 && p.Lines[i]==""` (по снимку `doc.Snapshot()` — использовать `Cues[j].Index`, брать первые по времени). Запомнить `firstPendingAt` при появлении первого pending.
   - Переводить, если `len(pending) >= batchSize` **или** (`len(pending)>0` и прошло `≥ BatchWait` с `firstPendingAt`) **или** (`ref.Ended` и `len(pending)>0`): взять до `batchSize` первых pending, `texts` = `JoinLines`, вызвать существующий `r.runBatch(ctx, key, token, logger, job, targetName, p, idx, texts)` — он пишет в `p.Lines[idx]`, `PutProgress`, `RefreshLock`; `false` → return. Сбросить `firstPendingAt`. **Контекст перевода**: `translateChunk` берёт `lastTranslated(p.Lines, idx[0], 5)` — для live индексы по времени ≠ по добавлению, но это только подсказка модели; допустимо.
   - Если `len(pending)==0` и не было батча на этом тике: `PutProgress(p)` только если `Total` изменился (чтобы HEAD видел новые cue).
   - Завершение: `ref.Ended && len(pending)==0` → если `job.Live.Contiguous()` → `body := doc.Snapshot().RenderByIndex(p.Lines)`; `PutFinal`; `DropProgress`; return. Иначе `p.Live=false; PutProgress; return` (частичный остаётся 24 ч, финала нет).
   - Idle: если `time.Since(r.lastSeen(key)) > Idle` → лог Info `viewer gone, stopping`, `JobErrors{viewer_gone}++`, `PutProgress` (с `Live=true` — источник не завершён), return. `Touch` при `Ensure` ставит начальную отметку.
   - `RefreshLock` на каждом тике без батча тоже нужен (лок TTL 300 с, тики каждые 4 с — продлевать раз в `lockTTL/3`).
3. `Ensure`: если `job.Live != nil` — `run` вызывает `runLive` после захвата лока (вынести общую часть захвата/освобождения лока в `run`, ветвление по `job.Live`).

**`LiveSnapshot`**: `GetFinal` → как в `Snapshot`. Иначе `p := GetProgress`; `doc := src.Doc().Snapshot()`; `lines := p.Lines` (или пусто); `body := doc.RenderByIndex(lines)`; `done` = число `i` с `lines[i] != ""` или структурно пустых cue среди `len(doc.Cues)`; `total = len(doc.Cues)`; `Live = !src.Ended() || (p != nil && p.Live)`; `Final = false`. (Когда `Ended` и задача дописала `Live=false` без финала — `Live=false`, `done==total` → клиент считает завершённым; тело — всё, что переведено.)

- [ ] **Step 1: Тесты** (в `job_test.go`; используют `newLivePlaylistServer` из Task 3)

```go
func newLiveRunner(t *testing.T, tr Translator, batch int, cfg LiveConfig) (*Runner, *MemoryStore) {
	t.Helper()
	st := NewMemoryStore()
	r := NewRunner(st, tr, batch, 4, time.Minute)
	r.SetLive(cfg)
	t.Cleanup(r.Close)
	return r, st
}

func TestLiveRunnerTranslatesByTimerThenFinal(t *testing.T) {
	srv := newLivePlaylistServer(t)
	srv.set(pl1, map[string]string{"s0-0.vtt": seg0, "s0-1.vtt": seg1})
	tr := &fakeTranslator{}
	r, st := newLiveRunner(t, tr, 50, LiveConfig{PollInterval: 10 * time.Millisecond, BatchWait: 30 * time.Millisecond, Idle: time.Minute})
	ls := NewLiveSource(srv.url(), srv.srv.Client(), 1<<20, 5000)
	r.Touch("k")
	r.Ensure(context.Background(), "k", &Job{Lang: "pt", Live: ls})
	time.Sleep(80 * time.Millisecond) // one cue pending for > BatchWait → translated alone
	p, _ := st.GetProgress(context.Background(), "k")
	if p == nil || len(p.Lines) != 1 || p.Lines[0] != "PT:Макс." || !p.Live || p.CueKeys[0] == "" {
		t.Fatalf("progress after timer batch: %+v", p)
	}
	srv.set(pl2end, nil)
	r.Wait("k")
	body, ok, _ := st.GetFinal(context.Background(), "k")
	if !ok || !strings.Contains(string(body), "PT:Привет.") {
		t.Fatalf("final missing: ok=%v body=%s", ok, body)
	}
	if p, _ := st.GetProgress(context.Background(), "k"); p != nil {
		t.Fatal("progress must be dropped after the final")
	}
}

func TestLiveRunnerBatchBySize(t *testing.T) {
	srv := newLivePlaylistServer(t)
	srv.set(pl1, map[string]string{"s0-0.vtt": vttWith(4)})
	tr := &fakeTranslator{}
	r, st := newLiveRunner(t, tr, 2, LiveConfig{PollInterval: 10 * time.Millisecond, BatchWait: time.Hour, Idle: time.Minute})
	ls := NewLiveSource(srv.url(), srv.srv.Client(), 1<<20, 5000)
	r.Touch("k")
	r.Ensure(context.Background(), "k", &Job{Lang: "pt", Live: ls})
	time.Sleep(80 * time.Millisecond)
	p, _ := st.GetProgress(context.Background(), "k")
	if p == nil || countFilled(p.Lines) != 4 || atomic.LoadInt32(&tr.calls) != 2 {
		t.Fatalf("size batches: filled=%d calls=%d", countFilled(p.Lines), atomic.LoadInt32(&tr.calls))
	}
}

func countFilled(lines []string) int {
	n := 0
	for _, l := range lines {
		if l != "" {
			n++
		}
	}
	return n
}

func TestLiveRunnerNoFinalAfterSeek(t *testing.T) {
	srv := newLivePlaylistServer(t)
	seek := "#EXTM3U\n#EXT-X-SESSION-OFFSET:600\n#EXTINF:2.0,\ns0-0.vtt?token=T\n#EXT-X-ENDLIST\n"
	srv.set(seek, map[string]string{"s0-0.vtt": seg0})
	r, st := newLiveRunner(t, &fakeTranslator{}, 50, LiveConfig{PollInterval: 10 * time.Millisecond, BatchWait: time.Hour, Idle: time.Minute})
	ls := NewLiveSource(srv.url(), srv.srv.Client(), 1<<20, 5000)
	r.Touch("k")
	r.Ensure(context.Background(), "k", &Job{Lang: "pt", Live: ls})
	r.Wait("k")
	if _, ok, _ := st.GetFinal(context.Background(), "k"); ok {
		t.Fatal("a run that did not start at 0 must not produce a final artifact")
	}
	p, _ := st.GetProgress(context.Background(), "k")
	if p == nil || p.Live || p.Lines[0] != "PT:Макс." {
		t.Fatalf("partial progress kept without Live: %+v", p)
	}
}

func TestLiveRunnerStopsWhenViewerGone(t *testing.T) {
	srv := newLivePlaylistServer(t)
	srv.set(pl1, map[string]string{"s0-0.vtt": seg0})
	r, st := newLiveRunner(t, &fakeTranslator{}, 50, LiveConfig{PollInterval: 10 * time.Millisecond, BatchWait: time.Hour, Idle: 50 * time.Millisecond})
	ls := NewLiveSource(srv.url(), srv.srv.Client(), 1<<20, 5000)
	r.Touch("k")
	r.Ensure(context.Background(), "k", &Job{Lang: "pt", Live: ls})
	done := make(chan struct{})
	go func() { r.Wait("k"); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not stop after the idle window")
	}
	p, _ := st.GetProgress(context.Background(), "k")
	if p == nil || !p.Live {
		t.Fatalf("progress must stay Live (source not ended): %+v", p)
	}
	srv.mu.Lock()
	hits := srv.hits["/h/a.mkv~hls/session/0123456789abcdef0123456789abcdef/s0.m3u8"]
	srv.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.hits["/h/a.mkv~hls/session/0123456789abcdef0123456789abcdef/s0.m3u8"] != hits {
		t.Fatal("playlist still polled after the job stopped")
	}
}

func TestLiveRunnerStopsOnSourceGone(t *testing.T) {
	srv := newLivePlaylistServer(t)
	srv.set(pl1, map[string]string{"s0-0.vtt": seg0})
	r, st := newLiveRunner(t, &fakeTranslator{}, 50, LiveConfig{PollInterval: 10 * time.Millisecond, BatchWait: time.Hour, Idle: time.Minute})
	ls := NewLiveSource(srv.url(), srv.srv.Client(), 1<<20, 5000)
	r.Touch("k")
	r.Ensure(context.Background(), "k", &Job{Lang: "pt", Live: ls})
	time.Sleep(30 * time.Millisecond)
	srv.mu.Lock()
	srv.status = 404
	srv.mu.Unlock()
	r.Wait("k")
	p, _ := st.GetProgress(context.Background(), "k")
	if p == nil || p.Live {
		t.Fatalf("gone source must clear Live and keep progress: %+v", p)
	}
}

func TestLiveRunnerReusesTranslationsByCueKey(t *testing.T) {
	srv := newLivePlaylistServer(t)
	srv.set(pl1, map[string]string{"s0-0.vtt": seg0})
	tr := &fakeTranslator{}
	r, st := newLiveRunner(t, tr, 50, LiveConfig{PollInterval: 10 * time.Millisecond, BatchWait: 20 * time.Millisecond, Idle: time.Minute})
	ls := NewLiveSource(srv.url(), srv.srv.Client(), 1<<20, 5000)
	r.Touch("k")
	r.Ensure(context.Background(), "k", &Job{Lang: "pt", Live: ls})
	time.Sleep(80 * time.Millisecond)
	srv.mu.Lock()
	srv.status = 404
	srv.mu.Unlock()
	r.Wait("k")
	calls := atomic.LoadInt32(&tr.calls)
	// A new session: fresh LiveSource, same key, same cue → no new upstream call.
	srv.mu.Lock()
	srv.status = 200
	srv.mu.Unlock()
	srv.set(pl1+"#EXT-X-ENDLIST\n", nil)
	ls2 := NewLiveSource(srv.url(), srv.srv.Client(), 1<<20, 5000)
	r.Touch("k")
	r.Ensure(context.Background(), "k", &Job{Lang: "pt", Live: ls2})
	r.Wait("k")
	if atomic.LoadInt32(&tr.calls) != calls {
		t.Fatalf("translated again: %d → %d", calls, atomic.LoadInt32(&tr.calls))
	}
	if _, ok, _ := st.GetFinal(context.Background(), "k"); !ok {
		t.Fatal("second contiguous run reaching ENDLIST must write the final")
	}
}

func TestLiveSnapshotReportsLive(t *testing.T) {
	srv := newLivePlaylistServer(t)
	srv.set(pl1, map[string]string{"s0-0.vtt": seg0})
	r, _ := newLiveRunner(t, &fakeTranslator{}, 50, LiveConfig{PollInterval: time.Hour, BatchWait: time.Hour, Idle: time.Minute})
	ls := NewLiveSource(srv.url(), srv.srv.Client(), 1<<20, 5000)
	if _, err := ls.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := r.LiveSnapshot(context.Background(), "k", ls)
	if err != nil || !s.Live || s.Final || s.Total != 1 || s.Done != 0 || strings.Contains(string(s.Body), "Макс") {
		t.Fatalf("snap=%+v body=%s err=%v", s, s.Body, err)
	}
}
```

- [ ] **Step 2: Прогнать — падает на компиляции** (`Job` без `Live`, нет `SetLive`).

- [ ] **Step 3: Реализация** по алгоритму выше. Ключевые фрагменты:

```go
// store.go
type Progress struct {
	Total   int
	Lines   []string
	CueKeys []string
	Live    bool
}

// job.go — поля Runner
	live     LiveConfig
	seenMu   sync.Mutex
	lastSeen map[string]time.Time

type LiveConfig struct{ PollInterval, BatchWait, Idle time.Duration }

func (r *Runner) SetLive(cfg LiveConfig) {
	if cfg.PollInterval <= 0 { cfg.PollInterval = 4 * time.Second }
	if cfg.BatchWait <= 0 { cfg.BatchWait = 10 * time.Second }
	if cfg.Idle <= 0 { cfg.Idle = 90 * time.Second }
	r.live = cfg
}

// Touch records that someone asked for key just now. The live loop stops
// when nobody has for r.live.Idle: polling the playlist keeps the
// transcoder session (and its FFmpeg) alive, and a translation nobody is
// watching would otherwise transcode the whole file for no one.
func (r *Runner) Touch(key string) {
	r.seenMu.Lock()
	if r.lastSeen == nil { r.lastSeen = map[string]time.Time{} }
	r.lastSeen[key] = time.Now()
	r.seenMu.Unlock()
}

func (r *Runner) sinceSeen(key string) time.Duration {
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	t, ok := r.lastSeen[key]
	if !ok { return 0 }
	return time.Since(t)
}
```

`run`: после `PutProgress`-регистрации в текущем коде стоит `if p == nil || len(p.Lines) != len(job.Doc.Cues)` — обернуть: `if job.Live != nil { r.runLive(ctx, key, token, logger, job, start); return }` **сразу после захвата лока и defer Unlock**, до чтения `job.Doc` (он nil). Удалить запись из `lastSeen` при выходе задачи (в `Ensure`'s defer вместе с `delete(r.running, key)`) — иначе карта растёт. `NewRunner` вызывает `SetLive(LiveConfig{})` для дефолтов.

`LiveSnapshot` — как описано; `done` считать функцией:

```go
func countDoneByIndex(lines []string, doc *Doc) int {
	n := 0
	for _, c := range doc.Cues {
		if len(c.Lines) == 0 || (c.Index < len(lines) && lines[c.Index] != "") { n++ }
	}
	return n
}
```

- [ ] **Step 4: Прогнать** `go test ./services/ 2>&1 | tail -5` — весь пакет зелёный (старые Runner-тесты не тронуты). При флаках по таймингам увеличить `Sleep` до 150 мс, не ослаблять проверки.

- [ ] **Step 5: Негативный контроль** — закомментировать проверку `Contiguous()` перед `PutFinal` → `TestLiveRunnerNoFinalAfterSeek` краснеет; вернуть. Закомментировать idle-выход → `TestLiveRunnerStopsWhenViewerGone` краснеет по таймауту; вернуть.

- [ ] **Step 6: Commit** (`services/store.go services/job.go services/job_test.go`), `live: translate a growing playlist document by size or timer, final only for a contiguous run`.

---

## Task 5: Handler, флаги, README (subtitle-translate)

**Files:**
- Modify: `services/web.go`, `configure.go`, `README.md`
- Test: `services/handler_test.go`

**Interfaces:**
- Consumes: `KeyPath`, `NewLiveSource`, `Runner.Touch`, `Runner.LiveSnapshot`, `Runner.SetLive`.
- Produces: заголовок `X-Subtitle-Live: 1`; `Handler.lives *lazymap.LazyMap[*LiveSource]`; функция `isPlaylistSource(u *url.URL) bool` (`strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")`).

**Поведение `ServeHTTP` для playlist-источника** (ветка после проверки `GetFinal` и до `HEAD`-ветки):
- `key := ArtifactKey(X-Info-Hash, KeyPath(X-Path), lang, model, PromptVersion)` — для **всех** запросов (KeyPath тождествен для не-HLS).
- `h.Runner.Touch(key)` на каждом GET и HEAD (и для обычных источников — безвредно).
- Если `isPlaylistSource`: `src := h.liveFor(key, sourceURL)` (lazymap с `Expire: 30*time.Minute`, `Capacity: sourceCacheCapacity`; конструктор — `NewLiveSource(sourceURL, h.Client, h.MaxSourceBytes, h.MaxCues)`, без сетевых вызовов). Для `HEAD`: `snap, err := h.Runner.LiveSnapshot(ctx, key, src)`; `writeVTTLive(w, r, nil, snap)`. Для `GET`: если `src.Doc().Len()==0` — один синхронный `src.Refresh(ctx)` (чтобы первый ответ уже нёс cue и 404/413 источника вернулись клиенту как сейчас: `ErrSourceGone` → 404 `source unavailable`, `ErrSourceTooLarge` → 413, иное → 404); затем `h.Runner.Ensure(ctx, key, &Job{Lang, SourceLang, Glossary, Live: src})`; `snap := LiveSnapshot`; `writeVTTLive`.
- `writeVTT` получает параметр `live bool`: `X-Subtitle-Live: 1` при `live`; `Access-Control-Expose-Headers: X-Subtitle-Progress, X-Subtitle-Live` всегда (один список для всех ответов).
- **Внимание к сессии другого источника**: если `sourceURL` для того же `key` сменился (новая сессия — другой `/session/<id>/`), а в кэше лежит `LiveSource` со старым URL — старый URL даёт 404 → задача завершится `source_gone`, а зритель будет получать 404. Поэтому `liveFor` сравнивает `src.url` с новым `sourceURL`; при несовпадении — `h.lives.Drop(key)` (проверить имя метода удаления в `lazymap`: `grep -n 'func (m \*LazyMap' $(go env GOMODCACHE)/github.com/webtor-io/lazymap@*/lazymap.go`) и создать новый. Тест на это обязателен.

- [ ] **Step 1: Тесты**

```go
func newLiveHandlerForTest(t *testing.T, tr Translator) (*Handler, *livePlaylistServer) {
	t.Helper()
	srv := newLivePlaylistServer(t)
	r := NewRunner(NewMemoryStore(), tr, 3, 4, time.Minute)
	r.SetLive(LiveConfig{PollInterval: 10 * time.Millisecond, BatchWait: 20 * time.Millisecond, Idle: time.Minute})
	t.Cleanup(r.Close)
	h := &Handler{Runner: r, Model: "m", Client: srv.srv.Client(), MaxSourceBytes: 1 << 20, MaxCues: 5000}
	return h, srv
}

func doLive(h http.Handler, method, sourceURL string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/s0.vtt", nil)
	req.Header.Set("X-Mod-Extra", "pt")
	req.Header.Set("X-Info-Hash", "abc")
	req.Header.Set("X-Path", "/a.mkv~hls/session/0123456789abcdef0123456789abcdef/s0.m3u8")
	req.Header.Set("X-Source-Url", sourceURL)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandlerLivePlaylistSource(t *testing.T) {
	h, srv := newLiveHandlerForTest(t, &fakeTranslator{})
	srv.set(pl1, map[string]string{"s0-0.vtt": seg0, "s0-1.vtt": seg1})
	rec := doLive(h, "GET", srv.url())
	if rec.Code != 200 || rec.Header().Get("X-Subtitle-Live") != "1" || rec.Header().Get("X-Subtitle-Progress") != "0/1" {
		t.Fatalf("code=%d live=%q progress=%q", rec.Code, rec.Header().Get("X-Subtitle-Live"), rec.Header().Get("X-Subtitle-Progress"))
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Expose-Headers"), "X-Subtitle-Live") {
		t.Fatalf("expose: %q", rec.Header().Get("Access-Control-Expose-Headers"))
	}
	if strings.Contains(rec.Body.String(), "Макс") {
		t.Fatal("untranslated cue must not be in the live body")
	}
	time.Sleep(80 * time.Millisecond)
	rec = doLive(h, "HEAD", srv.url())
	if rec.Header().Get("X-Subtitle-Progress") != "1/1" || rec.Header().Get("X-Subtitle-Live") != "1" {
		t.Fatalf("after timer batch: %q live=%q", rec.Header().Get("X-Subtitle-Progress"), rec.Header().Get("X-Subtitle-Live"))
	}
	srv.set(pl2end, nil)
	h.Runner.Wait(ArtifactKey("abc", "/a.mkv~hls/s0.m3u8", "pt", "m", PromptVersion))
	rec = doLive(h, "GET", srv.url())
	if rec.Header().Get("X-Subtitle-Live") != "" || rec.Header().Get("Cache-Control") != "public, max-age=86400" || !strings.Contains(rec.Body.String(), "PT:Привет.") {
		t.Fatalf("final: live=%q cc=%q body=%s", rec.Header().Get("X-Subtitle-Live"), rec.Header().Get("Cache-Control"), rec.Body.String())
	}
}

func TestHandlerLiveKeyIgnoresSessionID(t *testing.T) {
	h, srv := newLiveHandlerForTest(t, &fakeTranslator{})
	srv.set(pl1+"#EXT-X-ENDLIST\n", map[string]string{"s0-0.vtt": seg0})
	doLive(h, "GET", srv.url())
	h.Runner.Wait(ArtifactKey("abc", "/a.mkv~hls/s0.m3u8", "pt", "m", PromptVersion))
	req := httptest.NewRequest("GET", "/s0.vtt", nil)
	req.Header.Set("X-Mod-Extra", "pt")
	req.Header.Set("X-Info-Hash", "abc")
	req.Header.Set("X-Path", "/a.mkv~hls/session/ffffffffffffffffffffffffffffffff/s0.m3u8")
	req.Header.Set("X-Source-Url", "http://127.0.0.1:1/dead~hls/session/ffffffffffffffffffffffffffffffff/s0.m3u8")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=86400" {
		t.Fatalf("second session must hit the final of the first: code=%d cc=%q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestHandlerLiveNewSessionReplacesStaleSource(t *testing.T) {
	h, srv := newLiveHandlerForTest(t, &fakeTranslator{})
	srv.set(pl1, map[string]string{"s0-0.vtt": seg0})
	doLive(h, "GET", srv.url())
	// Same key, another source URL (a new transcoder session): the cached
	// LiveSource for the old URL must be replaced, not reused.
	other := strings.Replace(srv.url(), "token=T", "token=T2", 1)
	rec := doLive(h, "GET", other)
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	src, _ := h.lives.Get(ArtifactKey("abc", "/a.mkv~hls/s0.m3u8", "pt", "m", PromptVersion), func() (*LiveSource, error) { t.Fatal("must already be cached"); return nil, nil })
	if src.url != other {
		t.Fatalf("stale source kept: %s", src.url)
	}
}

func TestHandlerLiveGoneSourceIs404(t *testing.T) {
	h, srv := newLiveHandlerForTest(t, &fakeTranslator{})
	srv.mu.Lock()
	srv.status = 404
	srv.mu.Unlock()
	rec := doLive(h, "GET", srv.url())
	if rec.Code != 404 || rec.Body.String() != msgSourceUnavail+"\n" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Прогнать — падает.**

- [ ] **Step 3: Реализация** в `web.go` (по описанию выше), флаги в `configure.go`:

```go
const (
	flagLivePollInterval = "live-poll-interval"
	flagLiveBatchWait    = "live-batch-wait"
	flagLiveIdle         = "live-idle"
)
// в app.Flags:
cli.IntFlag{Name: flagLivePollInterval, Usage: "how often a live HLS subtitle playlist is re-read, seconds", Value: 4, EnvVar: "SUBTITLE_TRANSLATE_LIVE_POLL_INTERVAL"},
cli.IntFlag{Name: flagLiveBatchWait, Usage: "longest a pending live cue waits before a batch smaller than --batch-size is sent, seconds", Value: 10, EnvVar: "SUBTITLE_TRANSLATE_LIVE_BATCH_WAIT"},
cli.IntFlag{Name: flagLiveIdle, Usage: "a live job stops when nobody polled its key for this long, seconds", Value: 90, EnvVar: "SUBTITLE_TRANSLATE_LIVE_IDLE"},
// после NewRunner:
runner.SetLive(services.LiveConfig{PollInterval: time.Duration(c.Int(flagLivePollInterval)) * time.Second, BatchWait: time.Duration(c.Int(flagLiveBatchWait)) * time.Second, Idle: time.Duration(c.Int(flagLiveIdle)) * time.Second})
```

Найти место создания `Runner` в `configure.go` (`grep -n NewRunner configure.go`).

- [ ] **Step 4: Прогнать** `go test ./... 2>&1 | tail -5` и `go vet ./...` — зелено.

- [ ] **Step 5: README** — в «Called by torrent-http-proxy»: `X-Path` → «part of the cache key after `/session/<id>/` is removed»; новый подраздел «Live HLS source» после него:

```markdown
## Live HLS source

When `X-Source-Url` points at a media playlist (`….m3u8`) — the transcoder's
subtitle variant `<file>~hls/session/<id>/s<N>.m3u8` — the job follows the
playlist instead of reading one file: every `--live-poll-interval` it re-reads
the playlist, fetches the segments it has not seen, shifts their cues by
`#EXT-X-SESSION-OFFSET` into movie time and translates what has accumulated —
a batch of `--batch-size` cues, or fewer once the oldest pending cue has waited
`--live-batch-wait`.

- `X-Subtitle-Live: 1` is set while the playlist is live (no `#EXT-X-ENDLIST`)
  or the job is still writing. Do not read `done == total` as complete while it
  is present. The body carries the translated cues only: a cue not translated
  yet is omitted, never shown in the source language.
- The final artifact is written only for a contiguous run — offset 0 from the
  first read to `#EXT-X-ENDLIST`. A viewer who seeks gets a partial translation
  for the session (kept in Redis for 24 h under the same key, reused by cue
  identity on the next session); the next contiguous viewing completes it.
- The job stops on its own when nobody polled the key for `--live-idle`:
  reading the playlist keeps the transcoder session alive, so an unwatched
  translation would otherwise transcode the whole file for nobody. It also stops
  when the session is gone (404/503 from the transcoder), keeping progress.
- Size caps apply to the accumulated document: `--max-source-bytes` to the sum
  of segment bytes, `--max-cues` to the cue count.
```

В «Flags» — три новых флага. В «Metrics» — `subtitle_translate_job_errors_total{reason="source_gone"|"viewer_gone"}` (проверить точное имя метрики в `services/metrics.go`) с пометкой «terminations, not failures».

- [ ] **Step 6: Commit** (`services/web.go services/handler_test.go configure.go README.md`), `http: translate a live HLS subtitle playlist and report X-Subtitle-Live`.

---

## Task 6: web-ui — `Src` у встроенных дорожек и источник перевода

**Files:**
- Modify: `models/subtitle_opts.go`, `jobs/scripts/action.go:555-566`, `handlers/action/helper.go:473-500` (`pickTranslationSource`), `handlers/action/helper.go:760-790` (`GetSubtitles`, MediaProbe-ветка)
- Test: `handlers/action/helper_test.go`
- Branch: `git -C /Users/vintikzzzz/Projects/webtor/web-ui status -sb` — убедиться, что HEAD на `main` и рабочее дерево чистое от чужого WIP; затем `git switch -c embedded-live-translation`.

**Interfaces:**
- Produces: `models.SubtitleOpts.HLSSessionBase string` — URL вида `https://<edge>/<hash>/<file>~hls/session/<id>` **с query** stream-URL (`?api-key=…&token=…`), пусто вне транскодер-сессии. `GetSubtitles` для видимого MediaProbe-элемента при непустой базе: `Src = <base без query>/s<MPID>.m3u8<query>`.

- [ ] **Step 1: Тесты**

```go
func TestGetSubtitlesEmbeddedGetsPlaylistSrcInSession(t *testing.T) {
	h := NewHelper()
	mp := &api.MediaProbe{Streams: []api.Stream{
		{CodecType: "video", CodecName: "h264"},
		{CodecType: "subtitle", CodecName: "hdmv_pgs_subtitle"},       // skipped entirely, not counted
		{CodecType: "subtitle", CodecName: "dvd_subtitle"},            // bitmap: counted, hidden
		{CodecType: "subtitle", CodecName: "subrip", Tags: api.Tags{Language: "rus", Title: "Full"}},
	}}
	opts := SubtitleOpts{PreferredLang: "pt", HLSSessionBase: "https://edge.example/h/a.mkv~hls/session/0123456789abcdef0123456789abcdef?api-key=K&token=T"}
	lis := h.GetSubtitles(nil, mp, &ra.ExportTag{}, nil, nil, nil, opts)
	var emb *ListItem
	for i := range lis {
		if lis[i].Provider == "MediaProbe" {
			emb = &lis[i]
		}
	}
	if emb == nil || emb.MPID != "1" {
		t.Fatalf("embedded item: %+v", emb)
	}
	if want := "https://edge.example/h/a.mkv~hls/session/0123456789abcdef0123456789abcdef/s1.m3u8?api-key=K&token=T"; emb.Src != want {
		t.Fatalf("src=%q want %q", emb.Src, want)
	}
	// Negative control: no session → no Src (native MP4 path).
	lis = h.GetSubtitles(nil, mp, &ra.ExportTag{}, nil, nil, nil, SubtitleOpts{PreferredLang: "pt"})
	for _, li := range lis {
		if li.Provider == "MediaProbe" && li.Src != "" {
			t.Fatalf("Src without a session: %q", li.Src)
		}
	}
}

func TestLadderTranslationFromEmbeddedTrack(t *testing.T) {
	lis := []ListItem{
		{ID: "none", Kind: "subtitles"},
		{ID: "mp-0", MPID: "0", Provider: "MediaProbe", Kind: "subtitles", SrcLang: "rus", Badge: "embedded",
			Src: "https://edge.example/h/a.mkv~hls/session/0123456789abcdef0123456789abcdef/s0.m3u8?token=T"},
	}
	out := applyLadder(lis, "rus", SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	tr := findByID(out, "tr-pt")
	if tr == nil || tr.SourceBadge != "embedded" || tr.SourceID != "mp-0" {
		t.Fatalf("translated item: %+v", tr)
	}
	if want := "https://edge.example/h/a.mkv~hls/session/0123456789abcdef0123456789abcdef/s0.m3u8~tr:pt/s0.vtt?token=T"; tr.Src != want {
		t.Fatalf("src=%q want %q", tr.Src, want)
	}
	// Negative control: an embedded track without Src is still no source.
	lis[1].Src = ""
	if findByID(applyLadder(lis, "rus", SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true}), "tr-pt") != nil {
		t.Fatal("Src-less embedded track became a source")
	}
}
```

Сигнатуру `applyLadder` и хелпер `findByID` взять из существующих тестов (`grep -n 'applyLadder(\|func findByID' handlers/action/helper_test.go | head`); если хелпера нет — добавить в тест-файл. Типы `api.Stream`/`api.Tags` сверить с `services/api/media_probe.go` (`grep -n 'type Stream\|type Tags\|Language\|Title' services/api/*.go`).

- [ ] **Step 2: Прогнать — падает**

Run: `cd /Users/vintikzzzz/Projects/webtor/web-ui && LD=$(grep -m1 'ldflags' Makefile | sed 's/.*-ldflags[= ]*//') ; go test -ldflags "$LD" ./handlers/action/ -run 'TestGetSubtitlesEmbeddedGetsPlaylistSrc|TestLadderTranslationFromEmbeddedTrack' 2>&1 | tail -5`
(точную форму ldflags взять из `Makefile`, цель `test`). Expected: FAIL (`unknown field HLSSessionBase`).

- [ ] **Step 3: Реализация**

`models/subtitle_opts.go`:
```go
	// HLSSessionBase is the transcoder session's URL prefix
	// (…~hls/session/<id>, query included) while the stream plays through
	// the transcoder; empty otherwise. Embedded subtitle tracks get their
	// playlist Src from it, so they can feed the translation chain.
	HLSSessionBase string
```

`jobs/scripts/action.go`, в блоке `Step 4` после `sc.SessionSeekURL = result.SeekURL`:
```go
		if base, err := sessionBaseURL(exportResponse.ExportItems["stream"].URL); err == nil {
			if u, perr := url.Parse(base); perr == nil {
				u.Path += "/session/" + result.Session.ID
				sc.SubtitleOpts.HLSSessionBase = u.String()
			}
		}
```
(`sessionBaseURL` сохраняет query исходного stream-URL — проверить по `action.go:272-283`: `u.Path` меняется, `RawQuery` остаётся.)

`helper.go`, `GetSubtitles`, в ветке `if visible {` перед `res = append`:
```go
				src := ""
				if opts.HLSSessionBase != "" {
					src = embeddedPlaylistURL(opts.HLSSessionBase, i)
				}
```
и `Src: src,` в литерале. Хелпер:
```go
// embeddedPlaylistURL is the transcoder's subtitle variant for embedded
// stream i: the same s<N>.m3u8 hls.js plays, so the translation service
// follows exactly the track the viewer sees. N is the index among the
// non-PGS subtitle streams — the transcoder's numbering (NewHLS in
// content-transcoder counts the same set).
func embeddedPlaylistURL(base string, i int) string {
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.Path += "/s" + strconv.Itoa(i) + ".m3u8"
	return u.String()
}
```
`pickTranslationSource`: убрать `|| li.Provider == "MediaProbe"` и обновить комментарий над функцией («Embedded tracks are a source when they carry a playlist Src (transcoder session); a native MP4 without a session has none»). Проверить, что `isHumanFull` не исключает MediaProbe сам (`grep -n 'func isHumanFull' -A 8 handlers/action/helper.go`).

- [ ] **Step 4: Прогнать** целевые тесты и затем `make test` — зелено.

- [ ] **Step 5: Негативный контроль** — вернуть `|| li.Provider == "MediaProbe"` → `TestLadderTranslationFromEmbeddedTrack` краснеет; убрать снова.

- [ ] **Step 6: Commit** (`models/subtitle_opts.go jobs/scripts/action.go handlers/action/helper.go handlers/action/helper_test.go`), `subtitles: embedded tracks carry their session playlist and can source a translation`.

---

## Task 7: web-ui клиент — `X-Subtitle-Live`

**Files:**
- Modify: `assets/src/js/lib/player/subtitle-progress.js`, `Player.jsx` (`find assets/src/js -name Player.jsx`), `locales/{en,ru,es,de,fr,pt,it,pl,tr,nl,cs}.json`
- Test: `assets/src/js/lib/player/subtitle-progress.test.js`, `services/i18n` parity-тест (существующий, гоняется `make test`)

**Interfaces:**
- Produces: `parseProgress(header, live = false) → { done, total, final, live }`, где `final = !live && total > 0 && done >= total`, `live = Boolean(live)`. `pollProgress` передаёт `res.headers.get('X-Subtitle-Live') === '1'`. `onProgress(p)` получает `p.live`.
- Player: при `p.live` чип показывает `· ${p.done}` (число переведённых cue, без процента — знаменатель растёт), `span.title = tf('player.subtitleTranslatingLive')`; при `!p.live` — как сейчас. Ключ `player.subtitleTranslatingLive`: en «Translating along with playback…», ru «Перевод идёт вместе с просмотром…», остальные 9 локалей — перевод той же фразы (без плейсхолдера).

- [ ] **Step 1: Тесты**

```js
test('parseProgress with live never reports final', () => {
    assert.deepEqual(parseProgress('7/7', true), { done: 7, total: 7, final: false, live: true });
    assert.deepEqual(parseProgress('7/7', false), { done: 7, total: 7, final: true, live: false });
    assert.deepEqual(parseProgress('7/7'), { done: 7, total: 7, final: true, live: false });
});

test('pollProgress keeps polling while X-Subtitle-Live is set', async () => {
    const answers = [['1/1', '1'], ['1/1', '1'], ['2/2', null]];
    let i = 0;
    const fetchImpl = async () => {
        const [p, live] = answers[Math.min(i++, answers.length - 1)];
        return { status: 200, headers: { get: (h) => (h === 'X-Subtitle-Live' ? live : p) } };
    };
    const seen = [];
    let done = false;
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, onProgress: (p) => seen.push([p.done, p.live]), onDone: () => { done = true; } });
    await new Promise((r) => setTimeout(r, 30));
    stop();
    assert.deepEqual(seen, [[1, true], [2, false]]);
    assert.equal(done, true);
});
```

Существующий тест `parseProgress` дополнить полем `live: false` в ожидаемых объектах (deepEqual).

- [ ] **Step 2: Прогнать** `cd /Users/vintikzzzz/Projects/webtor/web-ui && node --test assets/src/js/lib/player/subtitle-progress.test.js 2>&1 | tail -8` — падает.

- [ ] **Step 3: Реализация**

```js
export function parseProgress(header, live = false) {
    const isLive = Boolean(live);
    const m = /^(\d+)\/(\d+)$/.exec(String(header || '').trim());
    if (!m) return { done: 0, total: 0, final: false, live: isLive };
    const done = parseInt(m[1], 10);
    const total = parseInt(m[2], 10);
    // `0/0` is "the job has not counted the cues yet", not "done". A live
    // source (X-Subtitle-Live) is never done either: its total grows with
    // the playlist, so done == total only means "caught up for now".
    return { done, total, final: !isLive && total > 0 && done >= total, live: isLive };
}
```
В `pollProgress`: `const p = parseProgress(res.headers.get('X-Subtitle-Progress'), res.headers.get('X-Subtitle-Live') === '1');` и условие `if (p.done !== last || p.live !== lastLive)` с отслеживанием `lastLive`, чтобы переход live→не-live дошёл до `onProgress`.

`Player.jsx`, `onProgress`:
```js
            onProgress: (p) => {
                cues = p.total;
                if (span) {
                    if (p.live) {
                        // A live source has no denominator worth a percent:
                        // the playlist grows with the transcode. Show the
                        // count and say so in the title.
                        span.textContent = `· ${p.done}`;
                        span.title = tf('player.subtitleTranslatingLive');
                    } else {
                        const pct = p.total > 0 ? Math.round((100 * p.done) / p.total) : 0;
                        span.textContent = `· ${pct}%`;
                        span.title = tf('player.subtitleTranslating', pct);
                    }
                }
                if (p.total > 0) reload(p.done, false);
            },
```
Проверить, что `tf` с одним аргументом допустим (`grep -n 'const tf\|function tf' assets/src/js -r | head -3`); если `tf` требует параметр — использовать `t(...)`.

Локали: добавить ключ во все 11 файлов рядом с `player.subtitleTranslating`.

- [ ] **Step 4: Прогнать** `npm test`, `npm run build`, `make test` — зелено (parity-тест локалей).

- [ ] **Step 5: Негативный контроль** — вернуть `final: total > 0 && done >= total` без `!isLive` → live-тест краснеет; вернуть.

- [ ] **Step 6: Commit** (`assets/src/js/lib/player/subtitle-progress.js assets/src/js/lib/player/subtitle-progress.test.js <Player.jsx path> locales/*.json` — перечислить все 11 явно), `player: a live translation is never final; count instead of percent`.

---

## Task 8: web-ui документация

**Files:**
- Modify: `docs/subtitle_translate.md` (разделы «The Translated item», «Client behaviour», «Template attributes» не меняются; «Known limitations» — удалить два пункта про embedded/HLS, добавить новый), `docs/superpowers/specs/2026-09-16-embedded-subtitle-translation-design.md` («Порядок работ» — отметить пункты 2–3)

- [ ] **Step 1:** В «The Translated item», абзац «Translation source selection»: заменить «Embedded (`MediaProbe`) tracks are never a source — they have no standalone URL for the proxy chain to fetch.» на:

```markdown
Embedded (`MediaProbe`) tracks are a source when the stream plays through the transcoder:
`GetSubtitles` gives each visible embedded track `Src = <HLSSessionBase>/s<MPID>.m3u8` (the same
variant hls.js plays; `SubtitleOpts.HLSSessionBase` is set in `streamContent` once the session
exists), and `TranslateURL` appends `~tr:<lang>/s<MPID>.vtt` to it. The service then follows the
live playlist (service README, «Live HLS source»): the translation arrives along with the
transcode, `X-Subtitle-Live: 1` marks it as still growing, and a final artifact is cached only
for a contiguous run from the start. A native MP4 played without a session has no base and no
embedded source, as before.
```

- [ ] **Step 2:** «Client behaviour»: добавить пункт про `X-Subtitle-Live` (`parseProgress(header, live)`, чип `· <done>` и title `player.subtitleTranslatingLive`, `final` только без `Live`).

- [ ] **Step 3:** «Known limitations»: удалить пункты «Embedded-only source files get no AI item» и «Translations of embedded HLS subtitle tracks are not supported»; добавить:

```markdown
- **Embedded-track translations follow the viewer's transcode.** The source is the live
  subtitle playlist of the current transcoder session, so the translation runs at transcode
  speed and stops when the viewer leaves (`--live-idle` on the service) or seeks far (a new run
  with a non-zero offset). A seeked session never produces a cached final artifact; the partial
  progress is kept 24 h and reused by cue identity. Native MP4 without a transcoder session
  still has no embedded source.
```

- [ ] **Step 4:** `npm run build` не нужен; `make test` — зелено (нет кода). Commit (`docs/subtitle_translate.md docs/superpowers/specs/2026-09-16-embedded-subtitle-translation-design.md docs/superpowers/plans/2026-09-16-embedded-subtitle-live-translation.md`), `docs: embedded subtitle tracks as a live translation source`.

---

## Task 9: Стейдж-проверка (ручная, после деплоя сервиса и web-ui-alt)

Деплой сервиса и стейджа — за владельцем (`sync.sh subtitle-translate`, `sync.sh --wait web-stage`). Проверка, MKV с встроенными текстовыми субтитрами (например `70a09ff6c67865f8c6a3afaf9912a88a88ac73c7`), предпочитаемый язык зрителя — которого нет в файле:

1. Пикер показывает «Translate to <Lang>» с origin `EM` (`data-source-badge="embedded"`).
2. Выбор → чип `· N` растёт, title «идёт вместе с просмотром», субтитры появляются через ≤ 20–30 с после начала речи.
3. `curl -I '<Src перевода>'` через THP: `X-Subtitle-Live: 1`, `X-Subtitle-Progress: n/m`, `Access-Control-Expose-Headers` содержит оба.
4. Перемотка на +10 мин: перевод продолжает появляться с новой позиции; после `ENDLIST` финала в S3 нет (Loki сервиса: `translation finished` отсутствует, прогресс с `Live=false`).
5. Закрыть вкладку: через ~90 с в Loki сервиса `viewer gone, stopping`; транскодер-сессия не продлевается (Loki транскодера — нет `Touch` от сервиса после этого).
6. Полный просмотр без перемотки с начала (короткий файл): после `ENDLIST` — `translation finished`, следующий вход отдаёт финал `Cache-Control: public` без `Live`.
7. Sintel.mp4 (без сессии): встроенных дорожек нет, поведение как раньше (регресс-контроль).
