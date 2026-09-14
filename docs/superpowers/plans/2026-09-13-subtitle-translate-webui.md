# Перевод субтитров в web-ui — план имплементации (план B фазы 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Плеер web-ui подкидывает платному зрителю AI-переведённую дорожку на предпочитаемом языке, когда человеческой нет и язык аудио отличается, показывает происхождение дорожек в модалке, ставит замок бесплатным и прогрессивно обновляет перевод по мере готовности. Сервис `subtitle-translate` и его роутинг это план A; здесь только web-ui.

**Architecture:** Один новый сервис `services/streamprefs` (предпочитаемый язык из настроек профиля + имена каста для глоссария) пробрасывается в job-скрипт стрима. `GetSubtitles` получает `SubtitleOpts` и реализует лестницу: правило по языку аудио, forced-дорожки, выбор дорожки по умолчанию для предпочитаемого языка, элемент провайдера `Translated` с URL `…~tr:<lang>/<name>.vtt`. Модалка рендерит пометки, замок и прогресс; плеер после 5 секунд просмотра активирует AI-дорожку, опрашивает прогресс `HEAD`-запросами и перезагружает `<track>`.

**Tech Stack:** Go 1.26 / Gin / Go-шаблоны, Preact-плеер, `node --test`, go-i18n (11 локалей с тестом паритета), Umami.

**Spec:** `docs/superpowers/specs/2026-09-13-subtitle-translate-design.md` (разделы «Что видит зритель», «web-ui», «Телеметрия», «Тестирование», решения 1–13).

## Global Constraints

- Целевой язык один: предпочитаемый язык = `stremio_settings.preferred_language` (если задан и известен `stremio.LanguageByCode`), иначе язык интерфейса `c.Lang`; для анонимов язык интерфейса. Сравнение языков по базовому тегу (`pt-BR` = `pt`).
- Правило по аудио: базовый язык аудиодорожки по умолчанию == предпочитаемый → полные субтитры не включаются, AI не запускается автоматически, по умолчанию forced-дорожка на предпочитаемом языке, если есть, иначе «Нет». Иначе (отличается или неизвестен) → лестница: загруженные пользователем → встроенные → приложенные → OpenSubtitles (`source=hash`, затем `imdb`) → AI-перевод.
- Сохранённый выбор зрителя (`ud.SubtitleID`) всегда побеждает автоматику.
- Forced-дорожки видимы в списке с пометкой «надписи», никогда не источник перевода и не полная дорожка по лестнице; признак по слову `forced` в названии встроенной или в имени приложенного файла.
- Источник перевода: не forced, не встроенный (у него нет URL), с `Src`; предпочтение: базовый язык == язык аудио, затем `en`, затем первый подходящий.
- Элемент `Translated`: `ID = "tr-<lang>"`, `Provider = "Translated"`, `SrcLang = <lang>`, `Label = "<Название языка> · AI"`, `Src = TranslateURL(source.Src, lang, names)` для платных, пустой `Src` и `Locked = true` для остальных; `Preload` никогда; `Default = true` только когда правило по аудио говорит «субтитры нужны» и человеческой дорожки на предпочитаемом языке нет.
- NSFW: для ресурса с `resource_metadata.is_adult = true` AI-дорожки нет вообще (ни пункта, ни замка/CTA): `streamprefs.IsAdultResource` → `buildSubtitleOpts(..., adult=true)` → `Translate=false`. Нет строки метаданных или ошибка БД → не adult (решение 13 спеки).
- Платный: `c.Claims.Context.Tier.Id != 0` (nil на любом уровне → не платный), либо флаг web-ui `--subtitle-translate-free` (`SUBTITLE_TRANSLATE_FREE`) для деплоев без claims-provider. Флаг `--subtitle-translate-enabled` (`SUBTITLE_TRANSLATE_ENABLED`, по умолчанию false) включает всё.
- Пометки происхождения (`Badge`): `user | embedded | sidecar | os | ai | forced`; тексты через i18n `action.stream.badge.<badge>`; все новые ключи во все 11 локалей (тест паритета).
- Прогресс: `HEAD` на `Src` дорожки раз в 3 с, заголовок `X-Subtitle-Progress: <done>/<total>`; готово, когда `total > 0 && done >= total`; `<track>` перезагружается сменой `src` на тот же URL с `&rev=<n>`.
- Umami: `subtitle-resolved` дополняется `translated`, `badge`, `audioLang`, `needed`; новые `subtitle-translate-start {lang, source, cues}`, `subtitle-translate-done {lang, seconds, cues}`, `subtitle-translate-error {lang, code}`, `subtitle-translate-lock-click {lang}`; CTA-ссылка с `data-umami-event="donate-subtitle-translate"`.
- Уровень телеметрии для `Translated`: `'5'`.
- Тесты: Go пакеты через `LD=$(grep '^PROTO_CONFLICT_LDFLAGS' Makefile | sed 's/^[^=]*:= *//') && go test -ldflags "$LD" ./pkg/`, полный прогон `make test` (docker); JS `npm test`; сборка `npm run build`. Никогда `go test ./...` в корне.
- Коммиты только явными файлами после `git status -sb`; трейлер:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01VwLULKCA6QwcXFJVLFrqJ9
  ```
- Документация: новый `docs/subtitle_translate.md` (CLAUDE.md требует документировать фичи).

---

## Карта файлов

| Файл | Изменение |
|---|---|
| `services/api/translate_url.go` (+test) | `TranslateURL(src, lang, names)` |
| `services/streamprefs/service.go` (+test) | `PreferredContentLang`, `CastNames` |
| `handlers/action/helper.go` (+test) | `ListItem.Forced/Locked/Badge`, `SubtitleOpts`, лестница, `Translated` |
| `jobs/scripts/action.go`, `jobs/scripts/translate_opts.go` (+test), `jobs/jobs.go`, `jobs/action.go`, `jobs/embed.go`, `serve.go` | флаги, проброс `streamprefs`, `StreamContent.SubtitleOpts` |
| `templates/views/action/stream_video.html` | arity `getSubtitles`, пометки, замок, прогресс, CTA |
| `locales/{en,ru,es,de,fr,pt,it,pl,tr,nl,cs}.json` | ключи `action.stream.badge.*`, `action.stream.translate.*`, `player.subtitleTranslating` |
| `assets/src/js/lib/player/subtitle-rules.js` (+test) | `pickDefaultSubtitle` |
| `assets/src/js/lib/player/subtitle-progress.js` (+test) | `parseProgress`, `withRev`, `pollProgress` |
| `assets/src/js/lib/player/subtitle-telemetry.js` (+test) | уровень 5, `badge`, `needed` |
| `assets/src/js/lib/player/Player.jsx` | автозапуск после 5 с, замок, прогресс, правило при смене аудио |
| `docs/subtitle_translate.md` | документация |

---

### Task 1: `TranslateURL` в `services/api`

**Files:**
- Create: `services/api/translate_url.go`
- Test: `services/api/translate_url_test.go`

**Interfaces:**
- Produces: `func TranslateURL(src, lang string, names []string) string` — `<scheme>://<host><escapedPath>~tr:<lang>/<basename-without-ext>.vtt?<query>[&names=<a,b>]`; пустой `src` → `""`.

- [ ] **Step 1: Падающий тест**

```go
// services/api/translate_url_test.go
package api

import "testing"

func TestTranslateURL(t *testing.T) {
	cases := []struct{ src, lang string; names []string; want string }{
		{"https://x.test/abc/Dir/movie.srt~vtt/movie.vtt?token=T&api-key=K", "pt", nil,
			"https://x.test/abc/Dir/movie.srt~vtt/movie.vtt~tr:pt/movie.vtt?token=T&api-key=K"},
		{"https://x.test/abc/movie.mkv~vi/opensubtitles/123.vtt?token=T", "ru", []string{"Hildy", "Walter"},
			"https://x.test/abc/movie.mkv~vi/opensubtitles/123.vtt~tr:ru/123.vtt?token=T&names=Hildy%2CWalter"},
		{"https://x.test/ext/aGVsbG8%3D%3D/user.srt~vtt/user.vtt?token=T", "es", nil,
			"https://x.test/ext/aGVsbG8%3D%3D/user.srt~vtt/user.vtt~tr:es/user.vtt?token=T"},
		{"", "pt", nil, ""},
	}
	for _, c := range cases {
		if got := TranslateURL(c.src, c.lang, c.names); got != c.want {
			t.Errorf("TranslateURL(%q,%q,%v)\n got %q\nwant %q", c.src, c.lang, c.names, got, c.want)
		}
	}
}
```

Run: `LD=$(grep '^PROTO_CONFLICT_LDFLAGS' Makefile | sed 's/^[^=]*:= *//') && go test -ldflags "$LD" ./services/api/ -run TestTranslateURL -v` → FAIL, undefined.

- [ ] **Step 2: Реализация**

```go
// services/api/translate_url.go
package api

import (
	"net/url"
	"path/filepath"
	"strings"
)

// TranslateURL appends the ~tr:<lang> mod to a subtitle URL, mirroring
// convertToVTT: the proxy resolves the inner chain itself, so any
// subtitle URL web-ui already builds (sidecar ~vtt, OpenSubtitles ~vi,
// user upload /ext/…~vtt) can be translated by suffixing it. names are
// character names for the service's glossary (query "names", CSV).
func TranslateURL(src, lang string, names []string) string {
	if src == "" || lang == "" {
		return ""
	}
	parsed, err := url.Parse(src)
	if err != nil {
		return ""
	}
	parts := strings.Split(parsed.Path, "/")
	name := parts[len(parts)-1]
	newName := strings.TrimSuffix(name, filepath.Ext(name)) + ".vtt"
	out := parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath() + "~tr:" + lang + "/" + url.PathEscape(newName)
	q := parsed.RawQuery
	if len(names) > 0 {
		v := url.Values{}
		v.Set("names", strings.Join(names, ","))
		if q != "" {
			q += "&"
		}
		q += v.Encode()
	}
	if q != "" {
		out += "?" + q
	}
	return out
}
```

- [ ] **Step 3: Тест** → PASS.

- [ ] **Step 4: Коммит**

```bash
git add services/api/translate_url.go services/api/translate_url_test.go
git commit -m "api: TranslateURL builds the ~tr:<lang> chain on any subtitle URL"
```

---

### Task 2: `ListItem` — forced, locked, badge

**Files:**
- Modify: `handlers/action/helper.go` (`ListItem`, `embeddedSubtitleVisible`, MediaProbe/ExportTag ветки `GetSubtitles`)
- Test: `handlers/action/helper_test.go`

**Interfaces:**
- Produces: поля `ListItem.Forced bool`, `ListItem.Locked bool`, `ListItem.Badge string`; `func badgeFor(provider, source string, forced bool) string`; `embeddedSubtitleVisible` возвращает `(visible, countsForHLS, forced bool)`; `func sidecarForced(label, src string) bool` (слово `forced` в имени файла или подписи).
- Поведение: forced-дорожки теперь видимы (`visible=true, forced=true`), битмапные как раньше.

- [ ] **Step 1: Падающие тесты** (добавить в `helper_test.go`; существующие `TestGetSubtitlesHidesForcedByTitle` и строку `{"subrip", "eng forced narrative", false, true}` в `TestEmbeddedSubtitleVisible` переписать под новую семантику)

```go
func TestEmbeddedForcedIsVisibleAndFlagged(t *testing.T) {
	mp := probeWith(`[
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English (Forced)"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}}
	]`)
	got := subtitleItems(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil, SubtitleOpts{}))
	f, ok := got["mp-0"]
	if !ok || !f.Forced || f.Badge != "forced" {
		t.Fatalf("forced track must be listed and flagged: %+v", got)
	}
	if n, ok := got["mp-1"]; !ok || n.Forced || n.Badge != "embedded" {
		t.Fatalf("plain track: %+v", got)
	}
}

func TestSidecarForcedAndBadges(t *testing.T) {
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{
		{Src: "u1", SrcLang: "en", Label: "Movie.forced.srt", Kind: "subtitles"},
		{Src: "u2", SrcLang: "en", Label: "Movie.srt", Kind: "subtitles"},
	}}
	os := []api.OpenSubtitleTrack{osTrack("7", "en")}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, nil, tag, os, &models.ExternalData{}, []models.UserSubtitleTrack{{ID: "us-1", Src: "u3", Label: "mine.srt", SrcLang: "en"}}, SubtitleOpts{})
	badges := map[string]ListItem{}
	for _, it := range items {
		badges[it.ID] = it
	}
	if !badges["et-1"].Forced || badges["et-1"].Badge != "forced" {
		t.Errorf("et-1: %+v", badges["et-1"])
	}
	if badges["et-2"].Badge != "sidecar" || badges["os-7"].Badge != "os" || badges["us-1"].Badge != "user" {
		t.Errorf("badges: et-2=%s os-7=%s us-1=%s", badges["et-2"].Badge, badges["os-7"].Badge, badges["us-1"].Badge)
	}
}

func TestEmbeddedSubtitleVisible(t *testing.T) {
	cases := []struct {
		codec, title            string
		visible, counts, forced bool
	}{
		{"subrip", "", true, true, false},
		{"ass", "Signs & Songs", true, true, false},
		{"hdmv_pgs_subtitle", "", false, false, false},
		{"dvd_subtitle", "", false, true, false},
		{"subrip", "Forced", true, true, true},
		{"subrip", "eng forced narrative", true, true, true},
		{"subrip", "Unforced", true, true, false},
	}
	for _, c := range cases {
		v, n, f := embeddedSubtitleVisible(c.codec, c.title)
		if v != c.visible || n != c.counts || f != c.forced {
			t.Errorf("%s/%q: got (%v,%v,%v) want (%v,%v,%v)", c.codec, c.title, v, n, f, c.visible, c.counts, c.forced)
		}
	}
}
```

`SubtitleOpts` вводится в этой задаче: создать `models/subtitle_opts.go` (текст в Task 4, Step 2) и в `helper.go` объявить `type SubtitleOpts = models.SubtitleOpts`; добавить параметр `opts SubtitleOpts` в `GetSubtitles` (8 существующих вызовов в тестах дополнить `SubtitleOpts{}`; 2 вызова в шаблоне дополнить `.SubtitleOpts` сразу здесь, поле `StreamContent.SubtitleOpts` добавляется в Task 4 — до него шаблон получит нулевое значение через `{{ .SubtitleOpts }}` только если поле есть, поэтому в этой задаче добавить в `StreamContent` поле `SubtitleOpts models.SubtitleOpts` без вычисления). Так `make test` остаётся зелёным между задачами.

Run: `go test -ldflags "$LD" ./handlers/action/ -run 'TestEmbeddedForced|TestSidecarForced|TestEmbeddedSubtitleVisible' -v` → FAIL.

- [ ] **Step 2: Реализация**

В `ListItem` добавить:
```go
	// Forced marks "signs only" tracks (foreign-language lines and
	// on-screen text). They are never a full subtitle by the ladder and
	// never a translation source; they are the default when the audio is
	// already in the viewer's language.
	Forced bool
	// Locked is a track the viewer may not activate (AI translation for a
	// free account): rendered with a lock and a CTA, no Src.
	Locked bool
	// Badge names the origin for the picker: user, embedded, sidecar, os,
	// ai, forced (i18n key action.stream.badge.<Badge>).
	Badge string
```

Функции:
```go
func embeddedSubtitleVisible(codecName, title string) (visible bool, countsForHLS bool, forced bool) {
	if codecName == "hdmv_pgs_subtitle" {
		return false, false, false
	}
	if bitmapSubtitleCodecs[codecName] {
		return false, true, false
	}
	return true, true, forcedTitleRe.MatchString(title)
}

func sidecarForced(label, src string) bool {
	return forcedTitleRe.MatchString(label) || forcedTitleRe.MatchString(src)
}

func badgeFor(provider string, forced bool) string {
	if forced {
		return "forced"
	}
	switch provider {
	case "UserSubtitle":
		return "user"
	case "MediaProbe":
		return "embedded"
	case "ExportTag", "External":
		return "sidecar"
	case "OpenSubtitles":
		return "os"
	case "Translated":
		return "ai"
	}
	return ""
}
```

В `GetSubtitles`: MediaProbe-ветка использует три возвращаемых значения и ставит `Forced: forced, Badge: badgeFor("MediaProbe", forced)`; ExportTag-ветка `Forced: sidecarForced(t.Label, t.Src)`; OpenSubtitles/External/UserSubtitle получают `Badge: badgeFor(<provider>, false)`. Правило «forced по умолчанию нельзя выбирать автоматикой» реализуется в Task 3.

- [ ] **Step 3: Тесты** → PASS (весь пакет `./handlers/action/`).

- [ ] **Step 4: Коммит**

```bash
git add handlers/action/helper.go handlers/action/helper_test.go
git commit -m "subtitles: forced tracks listed with a badge; origin badges on list items"
```

---

### Task 3: `SubtitleOpts` и лестница в `GetSubtitles`

**Files:**
- Modify: `handlers/action/helper.go`
- Test: `handlers/action/helper_test.go`

**Interfaces:**
- Produces:
  ```go
  type SubtitleOpts struct {
      PreferredLang string   // базовый код, "" → правила по языку не применяются (поведение как сейчас)
      Translate     bool     // фича включена флагом и есть куда переводить
      Paid          bool     // зрителю можно выдавать ~tr URL
      Names         []string // глоссарий
  }
  func (s *Helper) GetSubtitles(ud, mp, tag, opensubs, ext, userSubs, opts SubtitleOpts) []ListItem
  ```
- Внутренние: `func baseLang(tag string) string` (`language.Parse` → `Base()`, `""` при ошибке); `func (s *Helper) defaultAudioLang(ud, mp) string` (базовый язык элемента `Default` из `GetAudioTracks`); `func pickTranslationSource(lis []ListItem, audioLang string) *ListItem`; `func (s *Helper) applyLadder(lis []ListItem, ud, audioLang string, opts SubtitleOpts) []ListItem`.
- Порядок в `GetSubtitles`: собрать список → `canonizeSrcLangs` → если `opts.PreferredLang == ""`: старое `selectListItem` → иначе `applyLadder` → `markPreload` (пропускает `Translated`).

- [ ] **Step 1: Падающие тесты**

```go
func humanTracks() (*ra.ExportTag, []api.OpenSubtitleTrack) {
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{{Src: "https://x/sc-en.vtt?token=T", SrcLang: "en", Label: "Movie.srt", Kind: "subtitles"}}}
	os := []api.OpenSubtitleTrack{
		{ID: "1", Source: "imdb", ExportTrack: &ra.ExportTrack{Src: "https://x/os-de.vtt?token=T", SrcLang: "de", Label: "German", Kind: "subtitles"}},
		{ID: "2", Source: "hash", ExportTrack: &ra.ExportTrack{Src: "https://x/os-en.vtt?token=T", SrcLang: "en", Label: "English", Kind: "subtitles"}},
	}
	return tag, os
}

func byID(items []ListItem) map[string]ListItem {
	m := map[string]ListItem{}
	for _, it := range items {
		m[it.ID] = it
	}
	return m
}

func defaultID(items []ListItem) string {
	for _, it := range items {
		if it.Default {
			return it.ID
		}
	}
	return ""
}

func audioProbe(lang string) *api.MediaProbe {
	return probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"` + lang + `"}},{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"eng","title":"English"}}]`)
}

func TestLadderTranslatedIsDefaultWhenNoHumanTrackInPreferredLang(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true, Names: []string{"Hildy"}})
	got := byID(items)
	tr, ok := got["tr-pt"]
	if !ok || tr.Provider != "Translated" || tr.SrcLang != "pt" || tr.Badge != "ai" || tr.Locked || tr.Preload {
		t.Fatalf("translated item: %+v", tr)
	}
	if !strings.Contains(tr.Src, "~tr:pt/") || !strings.Contains(tr.Src, "names=Hildy") {
		t.Fatalf("src=%q", tr.Src)
	}
	// source: audio is English → the English track wins (sidecar comes before OS in list order)
	if !strings.HasPrefix(tr.Src, "https://x/sc-en.vtt~tr:pt/") {
		t.Fatalf("expected the English sidecar as source, got %q", tr.Src)
	}
	if defaultID(items) != "tr-pt" {
		t.Fatalf("default=%s", defaultID(items))
	}
}

func TestLadderHumanTrackBeatsTranslation(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "de", Translate: true, Paid: true})
	if defaultID(items) != "os-1" {
		t.Fatalf("default=%s want os-1 (human German)", defaultID(items))
	}
	if _, ok := byID(items)["tr-de"]; ok {
		t.Fatal("no AI item when a human track exists in the preferred language")
	}
}

func TestLadderOrderUserEmbeddedSidecarOS(t *testing.T) {
	mp := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"eng"}},{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"rus","title":"Russian"}}]`)
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{{Src: "sc-ru", SrcLang: "ru", Label: "Movie.rus.srt", Kind: "subtitles"}}}
	os := []api.OpenSubtitleTrack{{ID: "9", Source: "hash", ExportTrack: &ra.ExportTrack{Src: "os-ru", SrcLang: "ru", Label: "Russian", Kind: "subtitles"}}}
	user := []models.UserSubtitleTrack{{ID: "us-1", Src: "u", Label: "mine.srt", SrcLang: "ru"}}
	opts := SubtitleOpts{PreferredLang: "ru", Translate: true, Paid: true}
	if d := defaultID(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, tag, os, &models.ExternalData{}, user, opts)); d != "us-1" {
		t.Errorf("user upload must win: %s", d)
	}
	if d := defaultID(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, tag, os, &models.ExternalData{}, nil, opts)); d != "mp-0" {
		t.Errorf("embedded must beat sidecar: %s", d)
	}
	if d := defaultID(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, nil, tag, os, &models.ExternalData{}, nil, opts)); d != "et-1" {
		t.Errorf("sidecar must beat OpenSubtitles: %s", d)
	}
	if d := defaultID(NewHelper().GetSubtitles(&models.VideoStreamUserData{}, nil, &ra.ExportTag{}, os, &models.ExternalData{}, nil, opts)); d != "os-9" {
		t.Errorf("OpenSubtitles last: %s", d)
	}
}

func TestLadderOSHashBeatsImdb(t *testing.T) {
	os := []api.OpenSubtitleTrack{
		{ID: "1", Source: "imdb", ExportTrack: &ra.ExportTrack{Src: "a", SrcLang: "pt", Label: "pt", Kind: "subtitles"}},
		{ID: "2", Source: "hash", ExportTrack: &ra.ExportTrack{Src: "b", SrcLang: "pt", Label: "pt", Kind: "subtitles"}},
	}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), &ra.ExportTag{}, os, &models.ExternalData{}, nil, SubtitleOpts{PreferredLang: "pt"})
	if defaultID(items) != "os-2" {
		t.Fatalf("default=%s want os-2 (hash match)", defaultID(items))
	}
}

func TestLadderAudioMatchesPreferredNoAutoSubtitles(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("por"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if defaultID(items) != "none" {
		t.Fatalf("default=%s want none", defaultID(items))
	}
	tr, ok := byID(items)["tr-pt"]
	if !ok || tr.Default {
		t.Fatalf("AI item must still be offered, not default: %+v", tr)
	}
}

func TestLadderAudioMatchesPreferredForcedIsDefault(t *testing.T) {
	mp := probeWith(`[{"codec_type":"audio","codec_name":"aac","tags":{"language":"por"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"por","title":"Portuguese (Forced)"}},
		{"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"por","title":"Portuguese"}}]`)
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, mp, &ra.ExportTag{}, nil, &models.ExternalData{}, nil, SubtitleOpts{PreferredLang: "pt"})
	if defaultID(items) != "mp-0" {
		t.Fatalf("default=%s want mp-0 (forced pt)", defaultID(items))
	}
}

func TestLadderForcedNeverFullDefaultNorSource(t *testing.T) {
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{{Src: "https://x/forced.vtt", SrcLang: "en", Label: "Movie.forced.srt", Kind: "subtitles"}}}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), tag, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if _, ok := byID(items)["tr-pt"]; ok {
		t.Fatal("a forced track is not a translation source")
	}
	if d := defaultID(items); d == "et-1" {
		t.Fatal("forced must not be the full default")
	}
}

func TestLadderLockedForFree(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: false})
	tr := byID(items)["tr-pt"]
	if !tr.Locked || tr.Src != "" || !tr.Default {
		t.Fatalf("free: %+v", tr)
	}
}

func TestLadderSavedChoiceWins(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{SubtitleID: "os-1"}, audioProbe("eng"), tag, os, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if defaultID(items) != "os-1" {
		t.Fatalf("saved choice must win: %s", defaultID(items))
	}
}

func TestLadderSourcePrefersAudioLangThenEnglish(t *testing.T) {
	tag := &ra.ExportTag{Tracks: []ra.ExportTrack{
		{Src: "https://x/sc-en.vtt", SrcLang: "en", Label: "en.srt", Kind: "subtitles"},
		{Src: "https://x/sc-fr.vtt", SrcLang: "fr", Label: "fr.srt", Kind: "subtitles"},
	}}
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("fra"), tag, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if tr := byID(items)["tr-pt"]; !strings.HasPrefix(tr.Src, "https://x/sc-fr.vtt~tr:pt/") {
		t.Fatalf("French audio → French source, got %q", tr.Src)
	}
	items = NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("deu"), tag, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if tr := byID(items)["tr-pt"]; !strings.HasPrefix(tr.Src, "https://x/sc-en.vtt~tr:pt/") {
		t.Fatalf("no source in the audio language → English, got %q", tr.Src)
	}
}

func TestLadderNoTranslationWithoutSource(t *testing.T) {
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{}, audioProbe("eng"), &ra.ExportTag{}, nil, &models.ExternalData{}, nil,
		SubtitleOpts{PreferredLang: "pt", Translate: true, Paid: true})
	if _, ok := byID(items)["tr-pt"]; ok {
		t.Fatal("embedded-only files have no translation source in phase 2")
	}
}

func TestLadderDisabledFallsBackToOldSelection(t *testing.T) {
	tag, os := humanTracks()
	items := NewHelper().GetSubtitles(&models.VideoStreamUserData{AcceptLangTags: []language.Tag{language.German}, FallbackLangTag: language.English}, nil, tag, os, &models.ExternalData{}, nil, SubtitleOpts{})
	if defaultID(items) != "os-1" {
		t.Fatalf("without PreferredLang the Accept-Language match applies: %s", defaultID(items))
	}
}
```

Добавить `"strings"` и `"golang.org/x/text/language"` в импорты теста (второй уже есть). Run → FAIL.

- [ ] **Step 2: Реализация**

```go
type SubtitleOpts struct {
	PreferredLang string
	Translate     bool
	Paid          bool
	Names         []string
}

func baseLang(tag string) string {
	t, err := language.Parse(tag)
	if err != nil {
		return ""
	}
	b, conf := t.Base()
	if conf == language.No {
		return ""
	}
	return b.String()
}

func (s *Helper) defaultAudioLang(ud *models.VideoStreamUserData, mp *api.MediaProbe) string {
	for _, a := range s.GetAudioTracks(ud, mp) {
		if a.Default {
			return baseLang(a.SrcLang)
		}
	}
	return ""
}

// ladderRank orders human providers for the preferred language.
func ladderRank(li ListItem) int {
	switch li.Provider {
	case "UserSubtitle":
		return 0
	case "MediaProbe":
		return 1
	case "ExportTag", "External":
		return 2
	case "OpenSubtitles":
		if li.Source == "hash" {
			return 3
		}
		return 4
	}
	return 9
}

func isHumanFull(li ListItem) bool {
	return li.ID != "none" && !li.Forced && li.Provider != "Translated"
}

func bestByLadder(lis []ListItem, lang string, forced bool) int {
	best, rank := -1, 99
	for i, li := range lis {
		if li.ID == "none" || li.Provider == "Translated" || li.Forced != forced || baseLang(li.SrcLang) != lang {
			continue
		}
		if r := ladderRank(li); r < rank {
			best, rank = i, r
		}
	}
	return best
}

// pickTranslationSource picks a non-forced, URL-backed human track: the
// audio language first (a transcription, not a translation of a
// translation), then English, then anything.
func pickTranslationSource(lis []ListItem, audioLang string) *ListItem {
	var first, en, audio *ListItem
	for i := range lis {
		li := &lis[i]
		if !isHumanFull(*li) || li.Src == "" || li.Provider == "MediaProbe" {
			continue
		}
		if first == nil {
			first = li
		}
		switch baseLang(li.SrcLang) {
		case audioLang:
			if audio == nil && audioLang != "" {
				audio = li
			}
		case "en":
			if en == nil {
				en = li
			}
		}
	}
	if audio != nil {
		return audio
	}
	if en != nil {
		return en
	}
	return first
}

func (s *Helper) applyLadder(lis []ListItem, ud *models.VideoStreamUserData, audioLang string, opts SubtitleOpts) []ListItem {
	lang := opts.PreferredLang
	humanIdx := bestByLadder(lis, lang, false)
	if humanIdx < 0 && opts.Translate {
		if src := pickTranslationSource(lis, audioLang); src != nil {
			name := lang
			if l := stremio.LanguageByCode(lang); l != nil {
				name = l.Name
			}
			tr := ListItem{ID: "tr-" + lang, Label: name + " · AI", SrcLang: lang, Kind: "subtitles", Provider: "Translated", Badge: "ai", Source: src.ID}
			if opts.Paid {
				tr.Src = api.TranslateURL(src.Src, lang, opts.Names)
			} else {
				tr.Locked = true
			}
			lis = append(lis, tr)
		}
	}
	// The viewer's saved choice always wins.
	if ud != nil && ud.SubtitleID != "" {
		for i := range lis {
			if lis[i].ID == ud.SubtitleID {
				lis[i].Default = true
				return lis
			}
		}
	}
	needed := audioLang == "" || audioLang != lang
	if !needed {
		if f := bestByLadder(lis, lang, true); f >= 0 {
			lis[f].Default = true
		} else {
			lis[0].Default = true // "none"
		}
		return lis
	}
	if humanIdx >= 0 {
		lis[humanIdx].Default = true
		return lis
	}
	for i := range lis {
		if lis[i].Provider == "Translated" {
			lis[i].Default = true
			return lis
		}
	}
	lis[0].Default = true
	return lis
}
```

В `GetSubtitles` заменить последнюю строку:
```go
	lis := s.canonizeSrcLangs(res)
	if opts.PreferredLang == "" {
		return s.markPreload(s.selectListItem(lis, ud.SubtitleID, ud), ud)
	}
	return s.markPreload(s.applyLadder(lis, ud, s.defaultAudioLang(ud, mp), opts), ud)
```
и в `markPreload` в первом `continue` добавить `|| li.Provider == "Translated"`. Импорт `"github.com/webtor-io/web-ui/services/stremio"` в helper.go: проверить, что `services/stremio` не импортирует `handlers/action` (цикл); если импортирует, взять название языка через `stremio.LanguageByCode` из `services/stremio/lang.go`, у которого зависимостей на handlers нет.

Note для реализатора: `TestLadderAudioMatchesPreferredNoAutoSubtitles` требует, чтобы элемент `Translated` добавлялся даже когда субтитры «не нужны» (для ручного выбора), но не был `Default`. Код выше это делает: добавление до проверки `needed`.

- [ ] **Step 3: Тесты** — `go test -ldflags "$LD" ./handlers/action/ -v` → PASS. Негативный контроль: убрать проверку `li.Forced != forced` в `bestByLadder` — `TestLadderForcedNeverFullDefaultNorSource` красный. Вернуть.

- [ ] **Step 4: Коммит**

```bash
git add handlers/action/helper.go handlers/action/helper_test.go
git commit -m "subtitles: ladder for the preferred language, audio-language rule, Translated item"
```

---

### Task 4: `streamprefs` и проброс в job-скрипт

**Files:**
- Create: `services/streamprefs/service.go`, `services/streamprefs/service_test.go`
- Create: `jobs/scripts/translate_opts.go`, `jobs/scripts/translate_opts_test.go`
- Modify: `jobs/jobs.go` (`New` +`prefs *streamprefs.Service`, поле, проброс), `jobs/action.go`, `jobs/embed.go` (вызовы `scripts.Action`), `jobs/scripts/action.go` (`StreamContent.SubtitleOpts`, `ActionScript.prefs`, вычисление), `serve.go` (флаги, `streamprefs.New(c, pg)`, `jobs.New(..., prefs)`)

**Interfaces:**
- Produces:
  ```go
  // services/streamprefs
  const FlagEnabled = "subtitle-translate-enabled"; const FlagFree = "subtitle-translate-free"
  func RegisterFlags(f []cli.Flag) []cli.Flag
  type Service struct{ pg *cs.PG; enabled, free bool }
  func New(c *cli.Context, pg *cs.PG) *Service
  func (s *Service) TranslateEnabled() bool; func (s *Service) FreeForAll() bool
  func ResolvePreferred(setting, uiLang string) string   // чистая: setting известен LanguageByCode → его код, иначе baseLang(uiLang)
  func (s *Service) PreferredContentLang(ctx, user *auth.User, uiLang string) string  // DB только для авторизованных, любая ошибка → uiLang
  func (s *Service) CastNames(ctx, videoID string, limit int) []string  // tmdb.GetInfoByIMDBID → metadata["credits"]["cast"][].name; best effort
  func (s *Service) IsAdultResource(ctx, resourceID string) bool  // resource_metadata.is_adult; нет строки/ошибка → false (spec, решение 13)
  // jobs/scripts
  func translateOpts(c *web.Context, prefs *streamprefs.Service, preferred string, names []string) action.SubtitleOpts  // action = handlers/action? см. ниже
  ```
- `SubtitleOpts` объявлен в `handlers/action`; `jobs/scripts` не может импортировать `handlers/action` (цикл: handler импортирует jobs). Поэтому `SubtitleOpts` объявить в `models/subtitle_opts.go` как `models.SubtitleOpts` и в helper.go использовать `models.SubtitleOpts` (Task 3 писать сразу с этим типом: `type SubtitleOpts = models.SubtitleOpts` в helper.go как алиас, чтобы тесты Task 2–3 остались без изменений).
- `isPaidForTranslate(c *web.Context) bool`: `c.Claims != nil && c.Claims.Context != nil && c.Claims.Context.Tier != nil && c.Claims.Context.Tier.Id != 0`.

- [ ] **Step 1: Падающие тесты**

```go
// services/streamprefs/service_test.go
package streamprefs

import "testing"

func TestResolvePreferred(t *testing.T) {
	cases := []struct{ setting, ui, want string }{
		{"pt", "en", "pt"}, {"", "ru", "ru"}, {"xx", "de", "de"}, {" uk ", "en", "uk"}, {"", "pt-BR", "pt"}, {"", "", ""},
	}
	for _, c := range cases {
		if got := ResolvePreferred(c.setting, c.ui); got != c.want {
			t.Errorf("ResolvePreferred(%q,%q)=%q want %q", c.setting, c.ui, got, c.want)
		}
	}
}

func TestIsAdultResourceNilSafe(t *testing.T) {
	var s *Service
	if s.IsAdultResource(context.Background(), "abc") {
		t.Fatal("nil service must never classify as adult")
	}
	if (&Service{}).IsAdultResource(context.Background(), "abc") {
		t.Fatal("no DB → not adult")
	}
}

func TestCastNamesFromCredits(t *testing.T) {
	md := map[string]any{"credits": map[string]any{"cast": []any{
		map[string]any{"name": "Rosalind Russell"}, map[string]any{"name": "Cary Grant"}, map[string]any{"name": ""},
	}}}
	got := castNamesFromMetadata(md, 5)
	if len(got) != 2 || got[0] != "Rosalind Russell" || got[1] != "Cary Grant" {
		t.Fatalf("got %v", got)
	}
	if got := castNamesFromMetadata(map[string]any{}, 5); len(got) != 0 {
		t.Fatalf("no credits → empty, got %v", got)
	}
}
```

```go
// jobs/scripts/translate_opts_test.go
package scripts

import (
	"testing"

	claimsproto "github.com/webtor-io/claims-provider/proto"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/web"
)

func TestIsPaidForTranslate(t *testing.T) {
	if isPaidForTranslate(&web.Context{}) {
		t.Fatal("nil claims are not paid")
	}
	if isPaidForTranslate(&web.Context{Claims: &claims.Data{Context: &claimsproto.Context{Tier: &claimsproto.Tier{Id: 0, Name: "free"}}}}) {
		t.Fatal("tier 0 is free")
	}
	if !isPaidForTranslate(&web.Context{Claims: &claims.Data{Context: &claimsproto.Context{Tier: &claimsproto.Tier{Id: 2, Name: "silver"}}}}) {
		t.Fatal("tier 2 is paid")
	}
}

func TestTranslateOptsShape(t *testing.T) {
	paid := &web.Context{Claims: &claims.Data{Context: &claimsproto.Context{Tier: &claimsproto.Tier{Id: 2}}}}
	o := buildSubtitleOpts(paid, true, false, false, "pt", []string{"A"})
	if !o.Translate || !o.Paid || o.PreferredLang != "pt" || len(o.Names) != 1 {
		t.Fatalf("%+v", o)
	}
	o = buildSubtitleOpts(&web.Context{}, true, true, false, "pt", nil)
	if !o.Paid {
		t.Fatal("free-for-all flag makes everyone paid")
	}
	o = buildSubtitleOpts(paid, true, true, true, "pt", nil)
	if o.Translate {
		t.Fatal("adult resource → no translation even when enabled and free for all")
	}
	o = buildSubtitleOpts(paid, false, false, false, "pt", nil)
	if o.Translate {
		t.Fatal("feature flag off → no translation")
	}
	if o.PreferredLang != "pt" {
		t.Fatal("preferred language still drives the ladder when translation is off")
	}
}
```

Run: `go test -ldflags "$LD" ./services/streamprefs/ ./jobs/scripts/ -run 'TestResolvePreferred|TestCastNames|TestIsPaid|TestTranslateOpts' -v` → FAIL, undefined.

- [ ] **Step 2: Реализация**

```go
// models/subtitle_opts.go
package models

// SubtitleOpts drives the subtitle ladder in the player (see
// docs/subtitle_translate.md). Declared here because both the stream job
// (jobs/scripts) and the template helper (handlers/action) need it and
// jobs cannot import handlers.
type SubtitleOpts struct {
	PreferredLang string
	Translate     bool
	Paid          bool
	Names         []string
}
```

```go
// services/streamprefs/service.go
package streamprefs

import (
	"context"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	tm "github.com/webtor-io/web-ui/models/tmdb"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/stremio"
	"golang.org/x/text/language"
)

const (
	FlagEnabled = "subtitle-translate-enabled"
	FlagFree    = "subtitle-translate-free"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.BoolFlag{Name: FlagEnabled, Usage: "offer AI-translated subtitle tracks (needs the ~tr mod in torrent-http-proxy)", EnvVar: "SUBTITLE_TRANSLATE_ENABLED"},
		cli.BoolFlag{Name: FlagFree, Usage: "offer AI translation to every viewer, not only paid tiers (deployments without claims-provider)", EnvVar: "SUBTITLE_TRANSLATE_FREE"},
	)
}

type Service struct {
	pg      *cs.PG
	enabled bool
	free    bool
}

func New(c *cli.Context, pg *cs.PG) *Service {
	return &Service{pg: pg, enabled: c.Bool(FlagEnabled), free: c.Bool(FlagFree)}
}

func (s *Service) TranslateEnabled() bool { return s != nil && s.enabled }
func (s *Service) FreeForAll() bool       { return s != nil && s.free }

// ResolvePreferred: the profile setting wins when it names a known
// language; otherwise the UI language, reduced to its base.
func ResolvePreferred(setting, uiLang string) string {
	if code := strings.TrimSpace(setting); code != "" && stremio.LanguageByCode(code) != nil {
		return code
	}
	t, err := language.Parse(uiLang)
	if err != nil {
		return ""
	}
	if b, conf := t.Base(); conf != language.No {
		return b.String()
	}
	return ""
}

func (s *Service) PreferredContentLang(ctx context.Context, user *auth.User, uiLang string) string {
	setting := ""
	if s != nil && s.pg != nil && user != nil && user.HasAuth() {
		if db := s.pg.Get(); db != nil {
			if data, err := models.GetUserStremioSettingsData(ctx, db, user.ID); err == nil && data != nil {
				setting = data.PreferredLanguage
			}
		}
	}
	return ResolvePreferred(setting, uiLang)
}

// IsAdultResource answers "may this resource get an AI subtitle track":
// NSFW resources never do (spec decision 13). Unknown resources (no
// metadata row yet) and DB errors read as not adult — the flag only
// ever removes the track, it never grants anything.
func (s *Service) IsAdultResource(ctx context.Context, resourceID string) bool {
	if s == nil || s.pg == nil || resourceID == "" {
		return false
	}
	db := s.pg.Get()
	if db == nil {
		return false
	}
	rm, err := models.GetResourceMetadataByResourceID(ctx, db, resourceID)
	if err != nil {
		log.WithError(err).WithField("resource", resourceID).Warn("failed to read resource metadata for subtitle gate")
		return false
	}
	return rm != nil && rm.IsAdult
}

// CastNames returns up to limit cast names from the TMDB credits stored
// by enrichment, for the translator's glossary. Best effort: empty on
// any miss.
func (s *Service) CastNames(ctx context.Context, videoID string, limit int) []string {
	if s == nil || s.pg == nil || !strings.HasPrefix(videoID, "tt") {
		return nil
	}
	db := s.pg.Get()
	if db == nil {
		return nil
	}
	info, err := tm.GetInfoByIMDBID(ctx, db, videoID)
	if err != nil || info == nil {
		return nil
	}
	return castNamesFromMetadata(info.Metadata, limit)
}

func castNamesFromMetadata(md map[string]any, limit int) []string {
	credits, _ := md["credits"].(map[string]any)
	cast, _ := credits["cast"].([]any)
	var out []string
	for _, c := range cast {
		m, _ := c.(map[string]any)
		if name, _ := m["name"].(string); strings.TrimSpace(name) != "" {
			out = append(out, strings.TrimSpace(name))
		}
		if len(out) == limit {
			break
		}
	}
	return out
}
```

Проверить сигнатуру `tm.GetInfoByIMDBID(ctx, db, imdbID) (*Info, error)` в `models/tmdb/info.go:~100` и поле `Info.Metadata map[string]any`.

```go
// jobs/scripts/translate_opts.go
package scripts

import (
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/web"
)

func isPaidForTranslate(c *web.Context) bool {
	return c != nil && c.Claims != nil && c.Claims.Context != nil && c.Claims.Context.Tier != nil && c.Claims.Context.Tier.Id != 0
}

// adult switches the AI track off entirely for NSFW resources (spec decision 13):
// no item, no lock, no CTA — the ladder simply never sees Translate=true.
func buildSubtitleOpts(c *web.Context, enabled, freeForAll, adult bool, preferred string, names []string) models.SubtitleOpts {
	return models.SubtitleOpts{
		PreferredLang: preferred,
		Translate:     enabled && !adult,
		Paid:          freeForAll || isPaidForTranslate(c),
		Names:         names,
	}
}
```

В `jobs/scripts/action.go`: поле `SubtitleOpts` уже добавлено в Task 2; поле `prefs *streamprefs.Service` в `ActionScript` и параметр в `Action(...)`; после вычисления `enrichedMD` (строка ~418):
```go
	preferred := s.prefs.PreferredContentLang(ctx, c.User, c.Lang)
	var castNames []string
	if enrichedMD != nil && s.prefs.TranslateEnabled() {
		nCtx, nCancel := context.WithTimeout(ctx, 3*time.Second)
		castNames = s.prefs.CastNames(nCtx, enrichedMD.VideoID, 15)
		nCancel()
	}
	adult := s.prefs.IsAdultResource(ctx, resourceID)
	sc.SubtitleOpts = buildSubtitleOpts(c, s.prefs.TranslateEnabled(), s.prefs.FreeForAll(), adult, preferred, castNames)
```
(`s.prefs` nil-safe: методы проверяют `s != nil`.) `jobs.New` получает `prefs *streamprefs.Service`, хранит и передаёт в `scripts.Action` во всех вызовах (`jobs/action.go`, `jobs/embed.go`); `serve.go`: `app.Flags = streamprefs.RegisterFlags(app.Flags)` рядом с другими регистрациями и `jobs.New(..., streamprefs.New(c, pg))`.

- [ ] **Step 3: Тесты и полный прогон**

Run: `go vet ./services/streamprefs/ ./jobs/scripts/ ./jobs/ ./handlers/action/ && make test 2>&1 | tail -15` → все `ok` (шаблонный тест рендера `stream_video` пока может падать на arity `getSubtitles` — если так, выполнить Task 5 до коммита Task 4 и закоммитить вместе; иначе коммитить раздельно).

- [ ] **Step 4: Коммит**

```bash
git add models/subtitle_opts.go services/streamprefs/service.go services/streamprefs/service_test.go jobs/scripts/translate_opts.go jobs/scripts/translate_opts_test.go jobs/scripts/action.go jobs/jobs.go jobs/action.go jobs/embed.go serve.go handlers/action/helper.go
git commit -m "stream: preferred content language, cast glossary and translate options for the subtitle ladder"
```

---

### Task 5: Шаблон модалки и локали

**Files:**
- Modify: `templates/views/action/stream_video.html`
- Modify: `locales/en.json`, `locales/ru.json`, `locales/es.json`, `locales/de.json`, `locales/fr.json`, `locales/pt.json`, `locales/it.json`, `locales/pl.json`, `locales/tr.json`, `locales/nl.json`, `locales/cs.json`
- Test: `services/i18n/locales_parity_test.go` (существующий), `services/template/*_test.go` если есть рендер `stream_video`

- [ ] **Step 1: Шаблон**

Оба вызова `getSubtitles … .UserSubtitles` → `getSubtitles … .UserSubtitles .SubtitleOpts` (если не сделано в Task 2). На `<dialog id="subtitles">` добавить `data-preferred-lang="{{ .SubtitleOpts.PreferredLang }}"`. Аудио `<li>` без изменений (уже несёт `data-srclang`). Список субтитров в `#embedded`:

```gotemplate
{{ range $otherSubs }}
<li data-id="{{ .ID }}" data-mp-id="{{ .MPID }}" data-srclang="{{ .SrcLang }}" data-provider="{{ .Provider }}" data-src="{{ .Src }}" data-label="{{ .Label }}" data-kind="{{ .Kind }}" data-badge="{{ .Badge }}" {{ if .Forced }}data-forced="true" {{ end }}{{ if .Locked }}data-locked="true" {{ end }}{{ if .Default }}data-default="true" {{ end }} class="subtitle cursor-pointer pr-3{{ if .Default }} text-primary underline{{ end }}{{ if .Locked }} opacity-70{{ end }}">
  {{ .Label }}{{ if .Badge }} <span class="badge badge-xs bg-w-cyan/10 text-w-cyan align-middle">{{ t $.Lang (printf "action.stream.badge.%s" .Badge) }}</span>{{ end }}{{ if .Locked }} <span aria-hidden="true">🔒</span>{{ end }}{{ if eq .Provider "Translated" }} <span class="tr-progress text-xs text-w-muted" hidden></span>{{ end }}
</li>
{{ end }}
```

OpenSubtitles-элементы в `#opensubtitles` получают тот же `data-badge="{{ .Badge }}"` и span с пометкой. После `</ul>` секции субтитров добавить карточку CTA (скрыта, показывается плеером по клику на замок):

```gotemplate
<div id="translate-cta" class="mt-3 p-3 rounded-xl bg-base-200/60 border border-w-line" hidden>
  <div class="text-sm">{{ t $.Lang "action.stream.translate.locked" }}</div>
  <a href="{{ langPath $.Lang "/donate" }}" target="_blank" class="btn btn-sm btn-pink mt-2" data-umami-event="donate-subtitle-translate" data-umami-event-tier="{{ if $.User | hasAuth }}free{{ else }}anon{{ end }}">{{ t $.Lang "action.stream.translate.cta" }}</a>
</div>
```

Классы: `btn-pink` допустим для CTA на апгрейд (аналог `action.upgrade.*`); при сомнении сверить с `docs/uikit.html`.

- [ ] **Step 2: Локали** — в каждый из 11 файлов добавить (значения переводить на язык файла; en):

```json
"action.stream.badge.user": "mine",
"action.stream.badge.embedded": "embedded",
"action.stream.badge.sidecar": "in torrent",
"action.stream.badge.os": "OS",
"action.stream.badge.forced": "signs only",
"action.stream.badge.ai": "AI",
"action.stream.translate.locked": "AI translation of subtitles into your language is available to supporters.",
"action.stream.translate.cta": "Support and translate",
"player.subtitleTranslating": "Translating… {{.Percent}}%"
```

`ru`: «мои», «встроенные», «в раздаче», «OS», «надписи», «AI», «AI-перевод субтитров на ваш язык доступен подписчикам.», «Поддержать и перевести», «Перевод… {{.Percent}}%». Неразрывный пробел перед `%` не нужен (правило про единицы: `%` пишется слитно).

- [ ] **Step 3: Тесты**

Run: `go test -ldflags "$LD" ./services/i18n/ ./services/template/ && make test 2>&1 | tail -8` → PASS. Проверить рендер standalone, если есть тест шаблона `stream_video`; иначе `go run` не нужен — покрытие через Task 7.

- [ ] **Step 4: Коммит**

```bash
git add templates/views/action/stream_video.html locales/en.json locales/ru.json locales/es.json locales/de.json locales/fr.json locales/pt.json locales/it.json locales/pl.json locales/tr.json locales/nl.json locales/cs.json
git commit -m "player modal: origin badges, AI track with lock and CTA, progress slot"
```

---

### Task 6: JS — правило по аудио, прогресс, телеметрия, проводка в плеере

**Files:**
- Create: `assets/src/js/lib/player/subtitle-rules.js`, `assets/src/js/lib/player/subtitle-rules.test.js`
- Create: `assets/src/js/lib/player/subtitle-progress.js`, `assets/src/js/lib/player/subtitle-progress.test.js`
- Modify: `assets/src/js/lib/player/subtitle-telemetry.js`, `subtitle-telemetry.test.js`
- Modify: `assets/src/js/lib/player/Player.jsx`

**Interfaces:**
- `subtitle-rules.js`: `export function baseLang(tag)`; `export function pickDefaultSubtitle(tracks, audioLang, preferredLang)` — `tracks: [{id, provider, srclang, forced, locked, source}]`, возвращает `id` или `'none'`; та же лестница, что в Go (user → embedded → sidecar → os hash → os imdb → translated, forced при совпадении аудио).
- `subtitle-progress.js`: `export function parseProgress(header)` → `{done, total, final}` (`'12/48'`; `'100/100'` → final); `export function withRev(src, n)`; `export function pollProgress(src, {fetchImpl, intervalMs, onProgress, onDone, onError})` → возвращает `stop()`; HEAD раз в `intervalMs`, `onProgress({done,total})` при изменении, `onDone()` при `final`, `onError(status)` при не-200 (и стоп).
- `subtitle-telemetry.js`: `levelOf` → `Translated: '5'`; `LEVELS` дополнить `'5'`; `selectEventData` добавляет `badge`; `resolveSubtitleLevel(tracks, uiLang, {audioLang})` возвращает также `badge` (пометка дорожки по умолчанию) и `needed` (`!audioLang || baseLang(audioLang) !== baseLang(uiLang)`); `readTracks` читает `data-badge`, `data-default`, `data-forced`, `data-locked`.

- [ ] **Step 1: Падающие тесты**

```js
// assets/src/js/lib/player/subtitle-rules.test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { baseLang, pickDefaultSubtitle } from './subtitle-rules.js';

const T = (id, provider, srclang, extra = {}) => ({ id, provider, srclang, forced: false, locked: false, source: '', ...extra });

test('baseLang reduces regional tags', () => {
    assert.equal(baseLang('pt-BR'), 'pt');
    assert.equal(baseLang('eng'), 'en');
    assert.equal(baseLang(''), '');
});

test('audio in the preferred language: forced wins, else none', () => {
    const tracks = [T('mp-0', 'MediaProbe', 'pt', { forced: true }), T('mp-1', 'MediaProbe', 'pt'), T('tr-pt', 'Translated', 'pt')];
    assert.equal(pickDefaultSubtitle(tracks, 'por', 'pt'), 'mp-0');
    assert.equal(pickDefaultSubtitle([T('mp-1', 'MediaProbe', 'pt')], 'por', 'pt'), 'none');
});

test('audio differs: ladder user > embedded > sidecar > os hash > os imdb > translated', () => {
    const all = [
        T('tr-pt', 'Translated', 'pt'),
        T('os-1', 'OpenSubtitles', 'pt', { source: 'imdb' }),
        T('os-2', 'OpenSubtitles', 'pt', { source: 'hash' }),
        T('et-1', 'ExportTag', 'pt'),
        T('mp-0', 'MediaProbe', 'pt'),
        T('us-1', 'UserSubtitle', 'pt'),
    ];
    assert.equal(pickDefaultSubtitle(all, 'eng', 'pt'), 'us-1');
    assert.equal(pickDefaultSubtitle(all.slice(0, 5), 'eng', 'pt'), 'mp-0');
    assert.equal(pickDefaultSubtitle(all.slice(0, 4), 'eng', 'pt'), 'et-1');
    assert.equal(pickDefaultSubtitle(all.slice(0, 3), 'eng', 'pt'), 'os-2');
    assert.equal(pickDefaultSubtitle(all.slice(0, 2), 'eng', 'pt'), 'os-1');
    assert.equal(pickDefaultSubtitle(all.slice(0, 1), 'eng', 'pt'), 'tr-pt');
});

test('forced never counts as a full track; locked translated is still the pick (caller shows the lock)', () => {
    assert.equal(pickDefaultSubtitle([T('et-1', 'ExportTag', 'pt', { forced: true }), T('tr-pt', 'Translated', 'pt', { locked: true })], 'eng', 'pt'), 'tr-pt');
    assert.equal(pickDefaultSubtitle([T('et-1', 'ExportTag', 'pt', { forced: true })], 'eng', 'pt'), 'none');
});

test('unknown audio language means subtitles are needed', () => {
    assert.equal(pickDefaultSubtitle([T('os-1', 'OpenSubtitles', 'pt', { source: 'hash' })], '', 'pt'), 'os-1');
});
```

```js
// assets/src/js/lib/player/subtitle-progress.test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseProgress, withRev, pollProgress } from './subtitle-progress.js';

test('parseProgress', () => {
    assert.deepEqual(parseProgress('12/48'), { done: 12, total: 48, final: false });
    assert.deepEqual(parseProgress('100/100'), { done: 100, total: 100, final: true });
    assert.deepEqual(parseProgress('0/0'), { done: 0, total: 0, final: false });
    assert.deepEqual(parseProgress(null), { done: 0, total: 0, final: false });
});

test('withRev appends or replaces rev', () => {
    assert.equal(withRev('https://x/a.vtt?token=T', 3), 'https://x/a.vtt?token=T&rev=3');
    assert.equal(withRev('https://x/a.vtt?token=T&rev=3', 4), 'https://x/a.vtt?token=T&rev=4');
    assert.equal(withRev('https://x/a.vtt', 1), 'https://x/a.vtt?rev=1');
});

test('pollProgress reports changes and stops when final', async () => {
    const answers = ['3/10', '3/10', '10/10'];
    let i = 0;
    const fetchImpl = async () => ({ status: 200, headers: { get: () => answers[Math.min(i++, answers.length - 1)] } });
    const seen = [];
    let done = false;
    const stop = pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, onProgress: (p) => seen.push(p.done), onDone: () => { done = true; } });
    await new Promise((r) => setTimeout(r, 30));
    stop();
    assert.deepEqual(seen, [3, 10]);
    assert.equal(done, true);
});

test('pollProgress stops on a non-200', async () => {
    let calls = 0;
    const fetchImpl = async () => { calls++; return { status: 502, headers: { get: () => null } }; };
    let err = null;
    pollProgress('https://x/a.vtt', { fetchImpl, intervalMs: 1, onError: (s) => { err = s; } });
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(err, 502);
    assert.equal(calls, 1);
});
```

В `subtitle-telemetry.test.js` существующие `deepEqual`-ожидания `resolveSubtitleLevel` дополнить новыми ключами (`badge: ''`, `needed: true`, `translated: false` при отсутствии `audioLang`) и добавить:
```js
test('translated is level 5 and badge/needed are reported', () => {
    const tracks = [{ provider: 'Translated', srclang: 'pt', source: '', badge: 'ai', isDefault: true }];
    assert.deepEqual(resolveSubtitleLevel(tracks, 'pt', { audioLang: 'eng' }), { level: '5', hasUiLang: true, count: 1, badge: 'ai', needed: true, translated: true });
    assert.equal(resolveSubtitleLevel([], 'pt', { audioLang: 'por' }).needed, false);
});
```

Run: `npm test` → FAIL (модули не найдены / ключи отсутствуют).

- [ ] **Step 2: Реализация**

```js
// assets/src/js/lib/player/subtitle-rules.js
// Mirror of handlers/action/helper.go applyLadder for the client: the
// server picks the default at render time, the client re-runs the same
// rule when the viewer switches the audio track.
const ISO3 = { eng: 'en', por: 'pt', rus: 'ru', spa: 'es', deu: 'de', ger: 'de', fra: 'fr', fre: 'fr', ita: 'it', pol: 'pl', tur: 'tr', nld: 'nl', dut: 'nl', ces: 'cs', cze: 'cs', ukr: 'uk', jpn: 'ja', kor: 'ko', zho: 'zh', chi: 'zh', ara: 'ar', hin: 'hi', ind: 'id', vie: 'vi', tha: 'th', swe: 'sv', nor: 'no', dan: 'da', fin: 'fi', ell: 'el', gre: 'el', heb: 'he', hun: 'hu', ron: 'ro', rum: 'ro', bul: 'bg', srp: 'sr', hrv: 'hr' };

export function baseLang(tag) {
    const s = String(tag || '').toLowerCase().split(/[-_]/)[0];
    if (!s) return '';
    return ISO3[s] || s;
}

function rank(t) {
    switch (t.provider) {
        case 'UserSubtitle': return 0;
        case 'MediaProbe': return 1;
        case 'ExportTag':
        case 'External': return 2;
        case 'OpenSubtitles': return t.source === 'hash' ? 3 : 4;
        case 'Translated': return 5;
        default: return 9;
    }
}

function best(tracks, lang, forced) {
    let pick = null;
    for (const t of tracks) {
        if (t.id === 'none' || !!t.forced !== forced || baseLang(t.srclang) !== lang) continue;
        if (!pick || rank(t) < rank(pick)) pick = t;
    }
    return pick;
}

export function pickDefaultSubtitle(tracks, audioLang, preferredLang) {
    const pref = baseLang(preferredLang);
    const audio = baseLang(audioLang);
    if (!pref) return 'none';
    if (audio && audio === pref) {
        const f = best(tracks, pref, true);
        return f ? f.id : 'none';
    }
    const full = best(tracks, pref, false);
    return full ? full.id : 'none';
}
```

```js
// assets/src/js/lib/player/subtitle-progress.js
export function parseProgress(header) {
    const m = /^(\d+)\/(\d+)$/.exec(String(header || '').trim());
    if (!m) return { done: 0, total: 0, final: false };
    const done = parseInt(m[1], 10);
    const total = parseInt(m[2], 10);
    return { done, total, final: total > 0 && done >= total };
}

export function withRev(src, n) {
    const u = new URL(src, 'https://placeholder.invalid');
    u.searchParams.set('rev', String(n));
    if (src.startsWith('http')) return u.toString();
    return u.pathname + u.search;
}

// pollProgress HEADs src every intervalMs (the service answers HEAD with
// X-Subtitle-Progress and never starts work on it), reports changes and
// stops on completion or on a non-200. Returns a stop() function.
export function pollProgress(src, { fetchImpl = fetch, intervalMs = 3000, onProgress, onDone, onError } = {}) {
    let stopped = false;
    let last = -1;
    let timer = null;
    const tick = async () => {
        if (stopped) return;
        let res;
        try {
            res = await fetchImpl(src, { method: 'HEAD', cache: 'no-store' });
        } catch (e) {
            if (onError) onError(0);
            return;
        }
        if (res.status !== 200) {
            if (onError) onError(res.status);
            return;
        }
        const p = parseProgress(res.headers.get('X-Subtitle-Progress'));
        if (p.done !== last) {
            last = p.done;
            if (onProgress) onProgress(p);
        }
        if (p.final) {
            if (onDone) onDone(p);
            return;
        }
        timer = setTimeout(tick, intervalMs);
    };
    timer = setTimeout(tick, 0);
    return () => { stopped = true; if (timer) clearTimeout(timer); };
}
```

`subtitle-telemetry.js`: `LEVELS = ['0','1','2','3','4','5']`, `case 'Translated': return '5';`; `readTracks` возвращает также `badge: el.getAttribute('data-badge') || ''`, `isDefault: el.getAttribute('data-default') === 'true'`, `forced`, `locked`; `selectEventData` добавляет `badge`; `resolveSubtitleLevel(tracks, uiLang, { audioLang } = {})` возвращает `{ level, hasUiLang, count, badge, needed, translated }`, где `badge` у дорожки с `isDefault`, `needed = !audioLang || baseLang(audioLang) !== baseLang(uiLang)` (импорт `baseLang` из `subtitle-rules.js`), `translated = badge === 'ai'`.

`Player.jsx`:
1. В эффекте `stream-start`: собрать `tracks = readTracks(modal)`, `audioLang = modal.querySelector('.audio[data-default="true"]')?.getAttribute('data-srclang') || ''`, отправить `subtitle-resolved` с новыми полями; затем, если дорожка по умолчанию это `.subtitle[data-provider="Translated"][data-default="true"]` без `data-locked`, вызвать `activateSubtitle(container, el)` и `startTranslationProgress(container, el)`.
2. `startTranslationProgress(container, el)`: `src = el.getAttribute('data-src')`; `umami.track('subtitle-translate-start', {lang, source: el.getAttribute('data-source')||'', cues: 0})`; `const span = el.querySelector('.tr-progress'); span.hidden = false;` `pollProgress(src, { onProgress: (p) => { span.textContent = tf('player.subtitleTranslating', Math.round(100*p.done/Math.max(1,p.total))); const tr = video.querySelector('track#'+CSS.escape(id)); if (tr) tr.src = withRev(src, p.done); }, onDone: (p) => { span.hidden = true; umami.track('subtitle-translate-done', {lang, seconds, cues: p.total}); }, onError: (code) => { span.textContent = '!'; umami.track('subtitle-translate-error', {lang, code}); } })`. Хранить `stop` в ref и вызывать при размонтировании и при выборе другой дорожки.
3. В обработчике клика по `.subtitle`: если `data-locked="true"` → показать `#translate-cta` (`hidden=false`), `umami.track('subtitle-translate-lock-click', {lang})`, `return` без активации. Если `data-provider="Translated"` и не locked → после `activateSubtitle` вызвать `startTranslationProgress`. Любой ручной клик ставит `manualSubtitleRef.current = true`.
4. Клик по `.audio`: после переключения, если `!manualSubtitleRef.current`: `id = pickDefaultSubtitle(readTracks(modal).map(t => ({...t, srclang: t.srclang})), target.getAttribute('data-srclang'), getLang())` — предпочитаемый язык для клиента приходит в `data-preferred-lang` на `#subtitles` (добавить в шаблон Task 5: `data-preferred-lang="{{ .SubtitleOpts.PreferredLang }}"`), использовать его вместо `getLang()`; затем `activateSubtitle(container, modal.querySelector('.subtitle[data-id="'+id+'"]'))` (для `'none'` — элемент `none`).
5. `i18n`: `tf('player.subtitleTranslating', pct)` — проверить сигнатуру `tf` в `assets/src/js/lib/player/i18n.js` (подстановка `{{.Percent}}`); если `tf` принимает объект, передать `{Percent: pct}`.

- [ ] **Step 3: Тесты и сборка** — `npm test` → все PASS (73 + новые); `npm run build` → только известное предупреждение о размере.

- [ ] **Step 4: Ручная проверка** — локально или на стейдже (`web-stage`, alias в sync.sh): стрим с приложенным английским srt при русском интерфейсе на платном аккаунте: через 5 с в модалке пункт «Русский · AI» подчёркнут, рядом растущий процент, субтитры появляются по мере готовности, `<track>` с `rev=` в Network; бесплатный аккаунт: замок и карточка CTA по клику; переключение аудио на русскую дорожку выключает субтитры (или включает forced).

- [ ] **Step 5: Коммит**

```bash
git add assets/src/js/lib/player/subtitle-rules.js assets/src/js/lib/player/subtitle-rules.test.js assets/src/js/lib/player/subtitle-progress.js assets/src/js/lib/player/subtitle-progress.test.js assets/src/js/lib/player/subtitle-telemetry.js assets/src/js/lib/player/subtitle-telemetry.test.js assets/src/js/lib/player/Player.jsx templates/views/action/stream_video.html
git commit -m "player: AI track auto-start after 5s, progress polling, lock CTA, audio-language rule"
```

---

### Task 7: Документация, флаги в values, сквозная проверка

**Files:**
- Create: `docs/subtitle_translate.md`
- Modify: `CLAUDE.md` (раздел Optional Integrations: одна строка про `SUBTITLE_TRANSLATE_ENABLED`/`SUBTITLE_TRANSLATE_FREE`)
- infra: `charts/web-ui/values.yaml` (`subtitleTranslate: {enable: false, free: false}`), `charts/web-ui/templates/_helpers.tpl` (env `SUBTITLE_TRANSLATE_ENABLED`, `SUBTITLE_TRANSLATE_FREE` по образцу `ANTHROPIC_API_KEY`), `values/web-ui.yaml.gotmpl` (`subtitleTranslate: {enable: true}`)

- [ ] **Step 1: `docs/subtitle_translate.md`** — назначение, лестница (таблица уровней и пометок), правило по языку аудио и forced, предпочитаемый язык (`streamprefs`), гейт и флаги, NSFW-исключение (`is_adult` → без AI-дорожки), формат URL `~tr:<lang>`, протокол прогресса (HEAD/`X-Subtitle-Progress`/`rev`), события Umami с полями, что делать при ошибке перевода (сервис план A), ссылки на обе спеки.

- [ ] **Step 2: values и деплой стейджа**

```bash
cd /Users/vintikzzzz/Projects/webtor/infra/helmfile && git diff charts/web-ui values/web-ui.yaml.gotmpl | head -40
./sync.sh --wait web-stage     # после push web-ui и сборки образа; план A (сервис + ~tr в THP) должен быть в проде
```
Проверка на `webtor.cc`/стейдж-хосте по сценарию Task 6 Step 4 с реальным переводом; посмотреть логи `subtitle-translate` (`translation finished`, токены) и `subtitle-resolved` в Umami (`badge=ai`, `needed=true`).

- [ ] **Step 3: Прод** — `./sync.sh --wait web`, коммит `images.yaml`; через сутки: доля `subtitle-resolved` с `badge=ai` среди `needed=true`, `subtitle-translate-error` < 2% стартов, стоимость по метрикам токенов.

- [ ] **Step 4: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && git add docs/subtitle_translate.md CLAUDE.md && git commit -m "docs: subtitle translation (phase 2)"
cd /Users/vintikzzzz/Projects/webtor/infra/helmfile && git add charts/web-ui/values.yaml charts/web-ui/templates/_helpers.tpl values/web-ui.yaml.gotmpl && git commit -m "web-ui: subtitle translation flags"
```

---

## Порядок и зависимости

1 → 2 → 3 → 4 → 5 → 6 → 7. Task 2 меняет arity `GetSubtitles`; до Task 5 шаблонный рендер-тест (если есть) красный, поэтому Tasks 2–5 лучше исполнять подряд и гонять `make test` в Task 5. План A должен быть задеплоен до Task 7 Step 2.

## Что сознательно не делается

- Перевод встроенных HLS-дорожек (нет URL у источника).
- `disposition.forced` из ffprobe (content-prober).
- Embed-виджет.
- Единый дневной AI-лимит (точка врезки: `buildSubtitleOpts` → `Paid=false` при исчерпании).
