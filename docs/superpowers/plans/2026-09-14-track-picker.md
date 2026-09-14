# Редизайн пикера дорожек плеера (аудио + субтитры) — план имплементации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Модалка `#subtitles` становится одним экраном без подвидов: аудио — ряд чипов «флаг + язык + уточнение», субтитры — ряд языковых чипов (паттерн Discover `chipClass('xs')`) и под ним дорожки выбранного языка чипами `chipClass('sm')` с фиксированным кодом происхождения (`EM/IN/OS/MY/AI`). OpenSubtitles и загрузки пользователя перестают быть отдельными экранами и становятся происхождениями внутри общего списка. Активная дорожка видна с главного экрана всегда: галочка + циановая заливка в каждой группе плюс строка «Сейчас:» в заголовке.

**Architecture:** SSR-first. Go рендерит **все** чипы обеих групп в один плоский контейнер на группу (`#audio-tracks`, `#subtitle-tracks`) в порядке `GetSubtitles`/`GetAudioTracks` и языковой ряд `#subtitle-langs`, посчитанный в Go. JS — прогрессивное улучшение в новом чистом модуле `assets/src/js/lib/player/track-picker.js`: фильтр по языку (`hidden`), точка на языковом чипе активной дорожки, пересчёт счётчиков, строка «Сейчас:». Контракт с плеером (классы `.audio`/`.subtitle` и весь набор `data-*`) сохраняется дословно; меняется только элемент (`<li>` → `<button type="button">`) и оформление активного состояния.

**Tech Stack:** Go 1.26 / Gin / Go html/template, Preact-плеер + ванильный `wireTrackHandlers`, Tailwind v4 + DaisyUI 5, `node --test`, go-i18n (11 локалей с тестом паритета).

**Дизайн-контракт:** `docs/uikit.html`, секция 19 «Track Picker (audio & subtitles)» — визуальный контракт, правила и мобильные оговорки внутри.

**Ветка:** `track-picker` (от `327bdf9`, «uikit: track picker …»).

---

## Global Constraints

- **Контракт с JS неприкосновенен.** Классы `.audio`, `.subtitle` и атрибуты `data-id`, `data-mp-id`, `data-srclang`, `data-provider`, `data-src`, `data-label`, `data-kind`, `data-badge`, `data-source`, `data-rank`, `data-source-badge`, `data-forced`, `data-locked`, `data-default`, `data-saved`, `data-autoselect` остаются с прежними именами и прежней семантикой. Новые атрибуты только добавляются: `data-lang`, `data-lang-name`, `data-lang-flag`.
- **Порядок DOM у встроенных дорожек = порядок `GetSubtitles`.** `hls-manager.remapTrackGroup` при равном числе элементов и HLS-дорожек назначает `data-mp-id` по позиции в DOM. Поэтому плоский список рендерится в исходном порядке, а группировка по языку выражается только атрибутом `hidden` — никакой пересортировки в шаблоне и в JS.
- **Tailwind видит только `./templates/**/*.html` и `./assets/src/**/*.{js,jsx}`** (`tailwind.config.js`). Ни одного Tailwind-класса в Go-коде: строки классов пишутся литералами в шаблоне, Go отдаёт только данные (`originCode` возвращает `"MY"`, а не набор классов). Классы `font-mono`, `font-normal`, `border-dashed`, `basis-full` в собранном CSS сейчас отсутствуют — они появятся ровно потому, что встречаются в шаблоне; инлайновые стили под запретом.
- **Скрытие — атрибутом `hidden`, не классом.** В собранном `style.css` есть `[hidden]:where(:not([hidden=until-found])){display:none!important}`, а утилита `.hidden` не сгенерирована; `btn` — `inline-flex`, так что только атрибут гарантированно перебивает display.
- **Клиент не переводит ничего нового.** Все строки приходят из SSR; JS пишет только числа, переключает классы и `hidden` и переносит уже отрендеренные узлы. Единственная существующая клиентская строка `player.subtitleTranslating` остаётся (уходит в `title` AI-чипа).
- **Никакого `innerHTML` в JS пикера.** Новый языковой чип (язык появился после загрузки субтитра) клонируется из `<template id="lang-chip-template">` и заполняется через `textContent` строками из `data-lang-name`/`data-lang-flag` самого чипа дорожки.
- **Флаги за `supportsFlagEmoji()`** (`assets/src/js/lib/discover/lang.js`): каждый флаг обёрнут в `<span class="chip-flag">`, при отсутствии поддержки JS ставит им `hidden` — ровно как Discover.
- **Коды происхождения не локализуются** (`EM/IN/OS/MY/AI`, тег свойства `forced`). Смысл живёт в `title` (существующие ключи `action.stream.badge.*`) и в строке-легенде, собираемой в шаблоне из тех же ключей.
- **i18n:** новые ключи во все 11 локалей (`locales/{en,ru,es,de,fr,pt,it,pl,tr,nl,cs}.json`), тест паритета `services/i18n/locales_parity_test.go`. Неразрывный пробел U+00A0 между числом и единицей. Русский стиль: «Смотрите», не «Стримьте».
- **Тесты:** Go только по пакетам — `LD=$(grep '^PROTO_CONFLICT_LDFLAGS' Makefile | sed 's/^[^=]*:= *//') && go test -ldflags "$LD" ./pkg/`; полный прогон `make test`. **Никогда `go test ./...`.** JS — `npm test`, сборка — `npm run build`.
- **Коммиты только явными файлами** после `git status -sb` (владелец коммитит в ту же копию). Трейлер:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01VwLULKCA6QwcXFJVLFrqJ9
  ```
- В публичном коде и в UI — никаких имён вендоров сверх уже присутствующего `OpenSubtitles` (это существующее происхождение, а не новое упоминание).

---

## Решения, принятые здесь (не переоткрывать при исполнении)

| № | Вопрос | Решение | Почему |
|---|---|---|---|
| Р-1 | Где живёт чип «Выкл.» | Это **сам** элемент `.subtitle[data-id="none"]`, отрендеренный первым в `#subtitle-langs`; `#subtitle-tracks` несёт `aria-owns="subtitle-off"` | Дизайн ставит «Off» в языковой ряд, а плеер требует реального `.subtitle` с `data-id="none"` (`readAllTracks`, `hasSavedDefault`, `findSubtitleItem('none')`, `pickDefaultSubtitle`). Дублировать элемент и синтезировать клик — два источника правды; `aria-owns` — ровно тот атрибут, который для этого есть. Риск: поддержка `aria-owns` неровная; деградация — чип остаётся именованной кнопкой в именованной группе |
| Р-2 | Где живут чипы `MY` | Их по-прежнему рендерит партиал `user_subtitles_view`, но контейнер `#my-subtitles` получает `class="contents"` и стоит **внутри** `#subtitle-tracks` | Это единственный вариант, при котором `data-autoselect` и async-перерисовка (`loadAsyncView` подменяет `innerHTML` цели) продолжают работать без изменений в `handlers/user_subtitle`: тот отвечает только списком загрузок и не может собрать плоский список (у него нет `MediaProbe`/`ExportTag`/OpenSubtitles). `display:contents` убирает обёртку и из раскладки flex, и из дерева доступности |
| Р-3 | Панель «Мои субтитры» | Партиал рендерит: чипы `MY`, пунктирный чип `+ Мои субтитры` (`#my-uploads-toggle`) и панель `#my-uploads-panel` (`basis-full w-full`, `hidden`) — заголовок, **список загрузок с кнопкой удаления в каждой строке** (существующая форма `POST /user-subtitle/delete/:id` с `data-async-target="#my-subtitles"`) и форма загрузки | Панель — full-width элемент flex-строки, то есть переносится на свою строку под чипами. Одна async-цель, один партиал, состояние раскрытия хранится на пережившей подмену обёртке `#my-subtitles` в `data-upload-open` |
| Р-3а | **Удаление никогда не живёт на чипе выбора** | Чип `MY` — это `role="radio"`, и только выбор дорожки. Удалить можно исключительно из строки списка в панели | Случайный тап по чипу должен максимум переключить дорожку, а не уничтожить файл зрителя. Разрушающее действие не совмещается с основным в одном элементе |
| Р-4 | Ключ `action.stream.more` («+%v») | Не заводим. Видимая подпись — `+` и `<span class="more-count">N</span>` (JS правит только число), локализуется `title`/`aria-label` новым ключом `action.stream.moreLanguages` | Серверный `tp` подставляет `{{.X}}`, клиентский `tf` — `%v`; счётчик правится обеими сторонами. Цифра не переводится, а переводить надо именно подпись «ещё языки» |
| Р-5 | Ключ `action.stream.legend` | Не заводим. Строка-легенда собирается в шаблоне из существующих `action.stream.badge.{embedded,sidecar,os,user,ai}` | Иначе смысл кода происхождения живёт в двух местах и расходится при первом же уточнении формулировки |
| Р-6 | Подпись чипа-раскрытия и ключ `action.stream.mySubtitles` | Чип называется `+ Мои субтитры` (глиф «+» и существующий ключ `action.stream.mySubtitles`), тем же ключом озаглавлена панель. Отдельный ключ `action.stream.upload` не заводим | Панель — не только загрузка: в ней ещё и удаление. «Загрузить» обещало бы меньше, чем есть, и обещало бы не то — зритель ищет способ удалить файл под словом «мои», а не под словом «загрузить». Ключ уже переведён на 11 языков |
| Р-14 | Пересборка после удаления | Ответ на удаление — тот же async-рендер партиала в `#my-subtitles`; обработчик события `async` в `Player.jsx` при отсутствии `data-autoselect` вызывает `syncUploadMarks()` и `refresh(subtitlesModal, { current: expandedLang(...) })` | Чипы `MY` живут в том же плоском ряду (Р-2), поэтому удалённый чип исчезает вместе с подменой `innerHTML`, а счётчик языка и раскрытый язык надо пересчитать: удаление последней загрузки может обнулить язык, который сейчас раскрыт. `expandedLangFor` для этого и возвращается к предпочитаемому |
| Р-7 | Неизвестный язык | Группа `data-lang="und"`, чип показывает моно-код `UND`, без флага и имени | Согласовано с секцией 19 (`UND Track 4` у аудио); код — как ISO-тег, не переводится, нового ключа не нужно |
| Р-8 | Элемент чипа | `<button type="button">` вместо `<li>`. Проверены все селекторы: `.subtitle`/`.audio`, `.subtitle[data-provider]`, `.subtitle[data-provider="MediaProbe"]`, `.audio[data-default=true]`, `e.target.closest('.subtitle')` — ни один не завязан на `li`. Комментарий в `subtitle-telemetry.js` про «`.subtitle` на `<div>` внутри `<li>`» устаревает и правится | Клавиатура и роль. `type="button"` обязателен: чипы стоят в `<dialog>`, где `<form method="dialog">` живёт рядом |
| Р-9 | Обработчик клика по аудио | `const target = e.target` → `e.target.closest('.audio')` | Сейчас у `<li>` текстовый узел, и `e.target` — сам `<li>`. У чипа внутри `<span>`/`<svg>`, и без `closest` `markTrack` получит `<span>`: чип не подсветится, `data-mp-id` прочитается как `null`, `hlsPlayer.audioTrack = NaN` |
| Р-10 | Подпись дорожки в `remapTrackGroup` | `el.getAttribute('data-label') \|\| el.textContent.trim()` | `textContent` чипа теперь включает `EM`/`forced`/`· hash`, и точное сравнение с `hlsTracks[i].name` перестанет совпадать никогда — тихо потеряется уточняющий матч при разном числе дорожек |
| Р-11 | Порядок языковых чипов | Активный язык → предпочитаемый → по убыванию числа дорожек → порядок первого появления (стабильная сортировка). Видимых чипов 4 (как в макете), остальные за «+N» | Активный всегда виден, значит никогда не прячется за «+N» |
| Р-12 | `filterSubtitlesByProvider` | Остаётся: им исключаются `UserSubtitle` из плоского списка, когда партиал рендерится (Р-2). Вызов для `OpenSubtitles` уходит | Ответ на пункт 9 задания: функция всё ещё нужна |
| Р-13 | `langDisplay` живёт в `services/stremio.Helper` | Не в `action.Helper` | `services/stremio` владеет таблицей языков и уже зарегистрирован глобально в `serve.go`; партиал загрузок рендерится билдером `user_subtitle/*`, который не тянет `action.Helper` явно |

---

## Карта файлов

| Файл | Изменение |
|---|---|
| `services/stremio/lang_display.go` (+test) | `LangDisplay`, `NewLangDisplay`, `Helper.LangDisplay` → шаблонный `langDisplay` |
| `handlers/action/picker.go` (+test) | `LangGroup`, `LangRow`, `Helper.SubtitleLangGroups`, `OriginCode`, `OriginCodeForBadge`, `OriginKey`, `PropertyTags`, `AudioSuffix` |
| `templates/views/action/stream_video.html` | переписанная модалка `#subtitles` |
| `templates/partials/action/user_subtitles.html` | чипы `MY` + чип «+ Загрузить» + панель загрузки |
| `locales/*.json` (11) | `action.stream.{off,now,upload,moreLanguages,subtitleLanguage,locked.aria}` |
| `services/template/stream_video_render_test.go` | новые funcs в трёх FuncMap + проверки чипов |
| `services/template/user_subtitles_partial_render_test.go` | `langDisplay` в FuncMap + проверки чипа |
| `assets/src/js/lib/player/track-picker.js` (+test) | новый чистый модуль |
| `assets/src/js/lib/player/Player.jsx` | проводка пикера, новый `markTrack`, удаление подвидов |
| `assets/src/js/lib/player/hls-manager.js` | `data-label` в `remapTrackGroup` |
| `assets/src/js/lib/player/subtitle-telemetry.js` | только комментарий про форму элемента |
| `assets/src/styles/style.css` | удаление правила `#my-subtitles ul > li:has(...)` |
| `docs/subtitle_translate.md`, `docs/user-subtitles.md`, `docs/uikit.html` | таблица атрибутов, раздел про пикер |

---

### Task 1: Go-хелперы — языковые группы, коды происхождения, свойства

**Files:**
- Create: `services/stremio/lang_display.go`, `services/stremio/lang_display_test.go`
- Create: `handlers/action/picker.go`, `handlers/action/picker_test.go`

**Interfaces:**
- Produces: `stremio.LangDisplay{Lang, Name, Flag, Code}`, `stremio.NewLangDisplay(tag) LangDisplay`, `(*stremio.Helper).LangDisplay(tag) LangDisplay` → шаблонный `langDisplay`.
- Produces: `action.LangGroup`, `action.LangRow{Groups, Expanded, Overflow}`, `(*action.Helper).SubtitleLangGroups(lis []ListItem, preferredLang string) LangRow` → `subtitleLangGroups`.
- Produces: `(*action.Helper).OriginCode(li ListItem) string`, `.OriginCodeForBadge(badge string) string`, `.OriginKey(li ListItem) string`, `.PropertyTags(li ListItem) []string`, `.AudioSuffix(li ListItem) string` → `originCode`, `originCodeForBadge`, `originKey`, `propertyTags`, `audioSuffix`.
- Consumes: существующие `badgeFor`, `ListItem`, `stremio.LanguageByCode`.

- [ ] **Step 1: Падающий тест на `LangDisplay`**

```go
// services/stremio/lang_display_test.go
package stremio

import "testing"

func TestNewLangDisplay(t *testing.T) {
	cases := []struct {
		in                     string
		lang, name, flag, code string
	}{
		{"ru", "ru", "Russian", "🇷🇺", ""},
		{"pt-BR", "pt", "Portuguese", "🇧🇷", ""},
		{"pt_BR", "pt", "Portuguese", "🇧🇷", ""},
		{"EN", "en", "English", "🇬🇧", ""},
		// Разбираемый, но не входящий в таблицу тег: группировать по нему
		// можно, показывать — только как код.
		{"ka", "ka", "", "", "KA"},
		{"und", "und", "", "", "UND"},
		{"", "und", "", "", "UND"},
		{"  ", "und", "", "", "UND"},
	}
	for _, c := range cases {
		got := NewLangDisplay(c.in)
		if got.Lang != c.lang || got.Name != c.name || got.Flag != c.flag || got.Code != c.code {
			t.Errorf("NewLangDisplay(%q) = %+v, want {%q %q %q %q}",
				c.in, got, c.lang, c.name, c.flag, c.code)
		}
	}
}
```

Run: `LD=$(grep '^PROTO_CONFLICT_LDFLAGS' Makefile | sed 's/^[^=]*:= *//') && go test -ldflags "$LD" ./services/stremio/ -run TestNewLangDisplay -v` → FAIL, undefined.

- [ ] **Step 2: Реализация `LangDisplay`**

```go
// services/stremio/lang_display.go
package stremio

import "strings"

// LangDisplay is everything a template needs to show one language on a
// picker chip. Known languages get a flag and a name from Languages;
// everything else gets a bare uppercase code, which is deliberately not
// localized — like an ISO tag or the EM/IN/OS origin codes, it is a code.
//
// Lang is the base tag the picker groups by ("pt" for both "por" and
// "pt-BR") and is never empty: an unknown or missing tag groups under
// "und", so every track belongs to exactly one language chip.
type LangDisplay struct {
	Lang string
	Name string
	Flag string
	Code string
}

// NewLangDisplay resolves a subtitle/audio track's srclang. The tag is
// already canonical by the time it gets here (Helper.canonizeSrcLangs in
// handlers/action runs golang.org/x/text over it), so the base language is
// the part before the first separator — the same rule baseLang() applies on
// the client (assets/src/js/lib/player/subtitle-rules.js), which is what
// keeps server-rendered groups and client-recomputed groups agreeing.
func NewLangDisplay(tag string) LangDisplay {
	base := strings.TrimSpace(tag)
	base = strings.ReplaceAll(base, "_", "-")
	base = strings.ToLower(strings.SplitN(base, "-", 2)[0])
	if base == "" || base == "und" {
		return LangDisplay{Lang: "und", Code: "UND"}
	}
	if l := LanguageByCode(base); l != nil {
		return LangDisplay{Lang: base, Name: l.Name, Flag: l.Flag}
	}
	return LangDisplay{Lang: base, Code: strings.ToUpper(base)}
}

// LangDisplay exposes NewLangDisplay to Go HTML templates. Registered
// globally via template.Manager.WithHelper (serve.go), so the uploads
// partial — rendered by the user_subtitle builder, which does not carry
// handlers/action.Helper — can call it too.
//
// Template usage: {{ $d := langDisplay .SrcLang }}{{ $d.Flag }} {{ $d.Name }}
func (s *Helper) LangDisplay(tag string) LangDisplay {
	return NewLangDisplay(tag)
}
```

Run: тот же — PASS.

- [ ] **Step 3: Падающий тест на группы и коды**

```go
// handlers/action/picker_test.go
package action

import "testing"

func li(id, lang, provider string, def bool) ListItem {
	return ListItem{ID: id, SrcLang: lang, Provider: provider, Default: def, Badge: badgeFor(provider, false)}
}

func TestSubtitleLangGroupsOrdersActiveThenPreferredThenCount(t *testing.T) {
	h := NewHelper()
	lis := []ListItem{
		{ID: "none", Label: "None"},
		li("a", "en", "MediaProbe", false),
		li("b", "en", "OpenSubtitles", false),
		li("c", "en", "ExportTag", false),
		li("d", "ru", "OpenSubtitles", true),
		li("e", "ru", "UserSubtitle", false),
		li("f", "de", "OpenSubtitles", false),
		li("g", "", "ExportTag", false),
	}
	row := h.SubtitleLangGroups(lis, "de")

	// "none" is not a language and never makes a chip.
	if got := len(row.Groups); got != 4 {
		t.Fatalf("got %d groups, want 4: %+v", got, row.Groups)
	}
	want := []string{"ru", "de", "en", "und"}
	for i, w := range want {
		if row.Groups[i].Lang != w {
			t.Errorf("group %d = %q, want %q (%+v)", i, row.Groups[i].Lang, w, row.Groups)
		}
	}
	if !row.Groups[0].Active {
		t.Error("the group holding the default track must be Active")
	}
	if row.Groups[2].Count != 3 {
		t.Errorf("en count = %d, want 3", row.Groups[2].Count)
	}
	if row.Expanded != "ru" {
		t.Errorf("Expanded = %q, want %q", row.Expanded, "ru")
	}
	if row.Overflow != 0 {
		t.Errorf("Overflow = %d, want 0", row.Overflow)
	}
}

// Subtitles off (the "None" item is the default) is not "no language
// chosen": the row still has to open on something, and the preferred
// language is the only sensible guess.
func TestSubtitleLangGroupsExpandsPreferredWhenNothingIsActive(t *testing.T) {
	h := NewHelper()
	lis := []ListItem{
		{ID: "none", Label: "None", Default: true},
		li("a", "en", "MediaProbe", false),
		li("b", "en", "OpenSubtitles", false),
		li("c", "de", "OpenSubtitles", false),
	}
	row := h.SubtitleLangGroups(lis, "de")
	if row.Expanded != "de" {
		t.Errorf("Expanded = %q, want %q", row.Expanded, "de")
	}
	if row.Groups[0].Lang != "de" {
		t.Errorf("preferred language must sort first when nothing is active, got %+v", row.Groups)
	}
	for _, g := range row.Groups {
		if g.Active {
			t.Errorf("no group may be Active when the default is None: %+v", g)
		}
	}
}

func TestSubtitleLangGroupsOverflowNeverHidesTheActiveLanguage(t *testing.T) {
	h := NewHelper()
	lis := []ListItem{{ID: "none", Label: "None"}}
	// Six single-track languages; the active one is added last, so only the
	// Active-first rule can keep it out of the overflow.
	for _, l := range []string{"en", "de", "fr", "es", "it", "pl"} {
		lis = append(lis, li("x-"+l, l, "OpenSubtitles", false))
	}
	lis = append(lis, li("y", "cs", "OpenSubtitles", true))

	row := h.SubtitleLangGroups(lis, "")
	if row.Groups[0].Lang != "cs" || row.Groups[0].Overflow {
		t.Fatalf("active language must be first and visible, got %+v", row.Groups[0])
	}
	if row.Overflow != 3 {
		t.Errorf("Overflow = %d, want 3 (7 groups, 4 visible)", row.Overflow)
	}
	for i, g := range row.Groups {
		if want := i >= 4; g.Overflow != want {
			t.Errorf("group %d (%s) Overflow = %v, want %v", i, g.Lang, g.Overflow, want)
		}
	}
}

// A forced track's Badge is "forced", which says what kind of track it is,
// not where it came from. The origin code must still be the provider's.
func TestOriginCodeAndPropertyTags(t *testing.T) {
	h := NewHelper()
	cases := []struct {
		li       ListItem
		code     string
		key      string
		tags     []string
	}{
		{ListItem{Provider: "MediaProbe", Badge: "embedded"}, "EM", "action.stream.badge.embedded", nil},
		{ListItem{Provider: "MediaProbe", Badge: "forced", Forced: true}, "EM", "action.stream.badge.embedded", []string{"forced"}},
		{ListItem{Provider: "ExportTag", Badge: "sidecar"}, "IN", "action.stream.badge.sidecar", nil},
		{ListItem{Provider: "External", Badge: "sidecar"}, "IN", "action.stream.badge.sidecar", nil},
		{ListItem{Provider: "OpenSubtitles", Badge: "os"}, "OS", "action.stream.badge.os", nil},
		{ListItem{Provider: "UserSubtitle", Badge: "user"}, "MY", "action.stream.badge.user", nil},
		{ListItem{Provider: "Translated", Badge: "ai"}, "AI", "action.stream.badge.ai", nil},
		{ListItem{ID: "none"}, "", "", nil},
	}
	for _, c := range cases {
		if got := h.OriginCode(c.li); got != c.code {
			t.Errorf("OriginCode(%+v) = %q, want %q", c.li, got, c.code)
		}
		if got := h.OriginKey(c.li); got != c.key {
			t.Errorf("OriginKey(%+v) = %q, want %q", c.li, got, c.key)
		}
		got := h.PropertyTags(c.li)
		if len(got) != len(c.tags) {
			t.Errorf("PropertyTags(%+v) = %v, want %v", c.li, got, c.tags)
			continue
		}
		for i := range got {
			if got[i] != c.tags[i] {
				t.Errorf("PropertyTags(%+v) = %v, want %v", c.li, got, c.tags)
			}
		}
	}
	if got := h.OriginCodeForBadge("sidecar"); got != "IN" {
		t.Errorf("OriginCodeForBadge(sidecar) = %q, want IN", got)
	}
	if got := h.OriginCodeForBadge("forced"); got != "" {
		t.Errorf("OriginCodeForBadge(forced) = %q, want \"\" — forced is a property, not an origin", got)
	}
}

func TestAudioSuffixDropsTheLanguageNameItWouldRepeat(t *testing.T) {
	h := NewHelper()
	if got := h.AudioSuffix(ListItem{SrcLang: "en", Label: "English"}); got != "" {
		t.Errorf("AudioSuffix = %q, want \"\" — the chip already says English", got)
	}
	if got := h.AudioSuffix(ListItem{SrcLang: "ru", Label: "Dub"}); got != "Dub" {
		t.Errorf("AudioSuffix = %q, want Dub", got)
	}
	if got := h.AudioSuffix(ListItem{SrcLang: "", Label: "Audio #1"}); got != "Audio #1" {
		t.Errorf("AudioSuffix = %q, want Audio #1", got)
	}
}
```

Run: `LD=$(grep '^PROTO_CONFLICT_LDFLAGS' Makefile | sed 's/^[^=]*:= *//') && go test -ldflags "$LD" ./handlers/action/ -run 'TestSubtitleLangGroups|TestOriginCode|TestAudioSuffix' -v` → FAIL, undefined.

- [ ] **Step 4: Реализация `picker.go`**

```go
// handlers/action/picker.go
package action

import (
	"sort"
	"strings"

	"github.com/webtor-io/web-ui/services/stremio"
)

// maxVisibleLangChips is how many language chips the subtitle row shows
// before the rest collapse behind a "+N" disclosure — the number the design
// was drawn at (docs/uikit.html §19). The active language always sorts
// first, so the chip that says what is playing is never the one collapsed.
const maxVisibleLangChips = 4

// LangGroup is one chip of the subtitle language row: a language, how many
// tracks it has, and whether the track currently playing is one of them.
type LangGroup struct {
	stremio.LangDisplay
	Count int
	// Active is "the track playing right now is in this language". The chip
	// carries a dot for it, so the selection stays visible even while the
	// viewer is browsing another language's tracks.
	Active bool
	// Overflow chips are rendered but hidden behind the "+N" button.
	Overflow bool
}

// LangRow is the whole row plus the two things the template would otherwise
// have to recompute by looping: which language opens expanded, and how many
// chips sit behind "+N". One helper call instead of three.
type LangRow struct {
	Groups   []LangGroup
	Expanded string
	Overflow int
}

// SubtitleLangGroups groups the subtitle list by base language for the
// picker's language row.
//
// Order: the language of the track playing first, then the viewer's
// preferred language, then by track count, then by the order GetSubtitles
// produced (a stable sort keeps the ladder as the tie-break rather than a
// map's iteration). The client recomputes this order in
// assets/src/js/lib/player/track-picker.js (groupByLang) after an upload
// changes the counts — the two implementations must stay identical, and the
// table in TestSubtitleLangGroupsOrdersActiveThenPreferredThenCount is
// mirrored by the same fixture in track-picker.test.js.
//
// The "None" item is not a language and gets no chip: it is rendered as the
// "Off" chip at the head of the row (see the template).
func (s *Helper) SubtitleLangGroups(lis []ListItem, preferredLang string) LangRow {
	preferred := stremio.NewLangDisplay(preferredLang).Lang
	var order []string
	byLang := map[string]*LangGroup{}
	for _, li := range lis {
		if li.ID == "none" {
			continue
		}
		d := stremio.NewLangDisplay(li.SrcLang)
		g, ok := byLang[d.Lang]
		if !ok {
			g = &LangGroup{LangDisplay: d}
			byLang[d.Lang] = g
			order = append(order, d.Lang)
		}
		g.Count++
		if li.Default {
			g.Active = true
		}
	}
	out := make([]LangGroup, 0, len(order))
	for _, l := range order {
		out = append(out, *byLang[l])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		if pi, pj := out[i].Lang == preferred, out[j].Lang == preferred; pi != pj {
			return pi
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return false
	})
	row := LangRow{Groups: out}
	for i := range row.Groups {
		if i >= maxVisibleLangChips {
			row.Groups[i].Overflow = true
			row.Overflow++
		}
	}
	if len(row.Groups) > 0 {
		row.Expanded = row.Groups[0].Lang
	}
	return row
}

// originCodes maps an origin badge to the fixed two-letter code the picker
// shows. Codes never localize (docs/uikit.html §19): they are codes, and
// their meaning is carried by title= and by the legend line, both built
// from the action.stream.badge.* keys.
var originCodes = map[string]string{
	"user":     "MY",
	"embedded": "EM",
	"sidecar":  "IN",
	"os":       "OS",
	"ai":       "AI",
}

// OriginCode is the code for where a track came from.
//
// Derived from Provider through badgeFor(provider, false), not from
// li.Badge: a forced track's Badge is "forced", which says what kind of
// track it is rather than where it came from, and a chip that showed
// "forced" in the origin slot would leave the viewer with no way to tell an
// embedded signs track from one shipped in the torrent.
func (s *Helper) OriginCode(li ListItem) string {
	return originCodes[badgeFor(li.Provider, false)]
}

// OriginCodeForBadge is OriginCode for a badge string rather than an item —
// the AI track's SourceBadge, which names the origin it was translated from
// ("· from EM"). Returns "" for "forced", which is not an origin.
func (s *Helper) OriginCodeForBadge(badge string) string {
	return originCodes[badge]
}

// OriginKey is the i18n key that explains the code in the viewer's
// language: the chip's title and the legend line both use it.
func (s *Helper) OriginKey(li ListItem) string {
	b := badgeFor(li.Provider, false)
	if _, ok := originCodes[b]; !ok {
		return ""
	}
	return "action.stream.badge." + b
}

// PropertyTags are what kind of track this is, as opposed to where it came
// from: lowercase codes rendered after the file name, secondary to the
// origin badge. Only "forced" exists today; "sdh" is drawn in the uikit and
// waits for content-prober to expose ffprobe's disposition flags.
func (s *Helper) PropertyTags(li ListItem) []string {
	if li.Forced {
		return []string{"forced"}
	}
	return nil
}

// AudioSuffix is the part of an audio track's label that the language chip
// does not already say: "Dub", "Commentary", "Audio (5.1) #2". Empty when
// the label is just the language name, so "🇬🇧 English · English" never
// happens.
func (s *Helper) AudioSuffix(li ListItem) string {
	label := strings.TrimSpace(li.Label)
	d := stremio.NewLangDisplay(li.SrcLang)
	if d.Name != "" && strings.EqualFold(label, d.Name) {
		return ""
	}
	return label
}
```

Run: оба пакета — PASS. Плюс регресс существующих: `LD=$(grep '^PROTO_CONFLICT_LDFLAGS' Makefile | sed 's/^[^=]*:= *//') && go test -ldflags "$LD" ./handlers/action/ ./services/stremio/`.

- [ ] **Step 5: Негативный контроль** — временно убрать ветку `if out[i].Active != out[j].Active` из компаратора → `TestSubtitleLangGroupsOverflowNeverHidesTheActiveLanguage` должен покраснеть; вернуть. Тем же способом проверить ветку `preferred` (убрать → краснеет `TestSubtitleLangGroupsExpandsPreferredWhenNothingIsActive`) и `badgeFor(li.Provider, false)` в `OriginCode` (заменить на `originCodes[li.Badge]` → краснеет случай forced).

- [ ] **Step 6: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && git status -sb
git add services/stremio/lang_display.go services/stremio/lang_display_test.go handlers/action/picker.go handlers/action/picker_test.go
git commit -m "picker: language groups, origin codes and property tags for the track picker"
```

---

### Task 2: Шаблоны модалки и партиала загрузок + локали + рендер-тесты

**Files:**
- Modify: `templates/views/action/stream_video.html` (блок `<dialog id="subtitles">`)
- Modify: `templates/partials/action/user_subtitles.html`
- Modify: `locales/{en,ru,es,de,fr,pt,it,pl,tr,nl,cs}.json`
- Modify: `services/template/stream_video_render_test.go`, `services/template/user_subtitles_partial_render_test.go`

**Interfaces:**
- Consumes: `subtitleLangGroups`, `langDisplay`, `originCode`, `originCodeForBadge`, `originKey`, `propertyTags`, `audioSuffix`, `filterSubtitlesByProvider`, `getSubtitles`, `getAudioTracks`, `userSubtitleView`.
- Produces (контракт для Task 3/4): `#audio-tracks`, `#subtitle-langs`, `#subtitle-tracks`, `#subtitle-off`, `#subtitle-lang-more`, `#my-uploads-toggle`, `#my-uploads-panel`, `#lang-chip-template`, `#audio-now`, `#subtitle-now`; классы `chip-check`, `chip-flag`, `lang`, `lang-name`, `lang-count`, `lang-dot`, `more-count`, `now-value`, `now-origin`.

- [ ] **Step 1: Ключи локалей**

`locales/en.json` (и по одному значению в остальные десять):

| Ключ | en | ru |
|---|---|---|
| `action.stream.off` | `Off` | `Выкл.` |
| `action.stream.now` | `Now:` | `Сейчас:` |
| `action.stream.moreLanguages` | `More languages` | `Ещё языки` |
| `action.stream.subtitleLanguage` | `Subtitle language` | `Язык субтитров` |
| `action.stream.locked.aria` | `locked, supporters only` | `недоступно, только для поддержавших` |

Остальные локали (естественный перевод, не калька): es `Desactivados / Ahora: / Más idiomas / Idioma de los subtítulos / bloqueado, solo para mecenas`; de `Aus / Jetzt: / Weitere Sprachen / Untertitelsprache / gesperrt, nur für Unterstützer`; fr `Désactivés / Maintenant : / Plus de langues / Langue des sous-titres / verrouillé, réservé aux soutiens`; pt `Desativadas / Agora: / Mais idiomas / Idioma das legendas / bloqueado, apenas para apoiadores`; it `Disattivati / Ora: / Altre lingue / Lingua dei sottotitoli / bloccato, solo per i sostenitori`; pl `Wyłączone / Teraz: / Więcej języków / Język napisów / zablokowane, tylko dla wspierających`; tr `Kapalı / Şimdi: / Daha fazla dil / Altyazı dili / kilitli, yalnızca destekçiler için`; nl `Uit / Nu: / Meer talen / Ondertiteltaal / vergrendeld, alleen voor supporters`; cs `Vypnuto / Nyní: / Další jazyky / Jazyk titulků / uzamčeno, jen pro podporovatele`.

Ключ `action.stream.mySubtitles` **не удаляется и не переводится заново** — он становится подписью чипа `+ Мои субтитры` и заголовком панели (Р-6). Уходят из использования только кнопки-подвиды в `stream_video.html`.

Единиц измерения в новых строках нет, правило U+00A0 не задевается. Проверка:

```bash
LD=$(grep '^PROTO_CONFLICT_LDFLAGS' Makefile | sed 's/^[^=]*:= *//') && go test -ldflags "$LD" ./services/i18n/ -run 'TestEveryLocale|TestNoLocale|TestUnits' -v
```

- [ ] **Step 2: Модалка `#subtitles`**

Заменить в `templates/views/action/stream_video.html` весь блок от `<dialog class="modal" id="subtitles" …>` до закрывающего `</dialog>` на:

```gotemplate
    <dialog class="modal" id="subtitles" data-resource-id="{{ .VideoStreamUserData.ResourceID }}" data-item-id="{{ .VideoStreamUserData.ItemID }}" data-preferred-lang="{{ .SubtitleOpts.PreferredLang }}">
        {{ $subs := getSubtitles .VideoStreamUserData .MediaProbe .ExportTag .OpenSubtitles .ExternalData .UserSubtitles .SubtitleOpts }}
        {{/* Uploads are rendered by the user_subtitles_view partial, which is
             also the async-reload target (handlers/user_subtitle): it is the
             only markup the server can re-send after an upload, so the MY
             chips have to come from there. They are excluded here to avoid
             rendering each of them twice. Where the partial is not rendered
             at all (embed layouts carry DomainSettings), the uploads stay in
             the flat list so they do not disappear. */}}
        {{ $withUploads := and (not .DomainSettings) .UserSubtitlesEnabled }}
        {{ $flat := $subs }}
        {{ if $withUploads }}{{ $flat = filterSubtitlesByProvider $subs "UserSubtitle" true }}{{ end }}
        {{ $row := subtitleLangGroups $subs .SubtitleOpts.PreferredLang }}
        <div class="modal-box w-full sm:w-11/12 max-w-5xl">

            {{/* ===== AUDIO ===== */}}
            <div class="mb-6">
                <div class="flex items-baseline justify-between gap-3 mb-2">
                    <h3 class="font-bold text-lg">{{ t $.Lang "action.stream.audio" }}</h3>
                    <span id="audio-now" class="text-xs text-w-muted truncate">{{ t $.Lang "action.stream.now" }} <span class="now-value text-w-text"></span></span>
                </div>
                <div id="audio-tracks" class="flex flex-wrap gap-1.5" role="radiogroup" aria-label="{{ t $.Lang "action.stream.audio" }}">
                    {{ range getAudioTracks .VideoStreamUserData .MediaProbe }}
                    {{ $d := langDisplay .SrcLang }}{{ $sfx := audioSuffix . }}
                    <button type="button" role="radio" aria-checked="{{ if .Default }}true{{ else }}false{{ end }}"
                            data-id="{{ .ID }}" data-mp-id="{{ .MPID }}" data-srclang="{{ .SrcLang }}" data-provider="{{ .Provider }}"
                            data-lang="{{ $d.Lang }}" data-lang-name="{{ $d.Name }}" data-lang-flag="{{ $d.Flag }}"
                            {{ if .Default }}data-default="true" {{ end }}
                            class="audio btn btn-sm gap-1.5 {{ if .Default }}bg-w-cyan/15 border border-w-cyan/30 text-w-cyan{{ else }}btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan{{ end }}">
                        <svg class="chip-check w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" aria-hidden="true"{{ if not .Default }} hidden{{ end }}><polyline points="20 6 9 17 4 12"/></svg>
                        {{ if $d.Name }}<span><span class="chip-flag">{{ $d.Flag }}</span> {{ $d.Name }}</span>{{ else }}<span class="font-mono text-xs text-w-muted">{{ $d.Code }}</span>{{ end }}
                        {{ if $sfx }}<span class="text-w-muted font-normal max-w-[14rem] truncate" title="{{ $sfx }}">{{ if $d.Name }}· {{ end }}{{ $sfx }}</span>{{ end }}
                    </button>
                    {{ end }}
                </div>
            </div>

            {{/* ===== SUBTITLES ===== */}}
            <div>
                <div class="flex items-baseline justify-between gap-3 mb-2">
                    <h3 class="font-bold text-lg">{{ t $.Lang "action.stream.subtitles" }}</h3>
                    <span id="subtitle-now" class="text-xs text-w-muted truncate">{{ t $.Lang "action.stream.now" }} <span class="now-origin badge badge-xs font-mono font-semibold tracking-wider bg-base-300/80 border-w-line/50 text-w-sub align-middle" hidden></span> <span class="now-value text-w-text"></span></span>
                </div>

                {{/* Language row. The "Off" chip at its head is the real
                     .subtitle element for the "None" item (data-id="none"):
                     the player looks it up by that id (findSubtitleItem,
                     pickDefaultSubtitle, hasSavedDefault), so duplicating it
                     would give the picker two sources of truth. It is a radio
                     of the track group below, declared with aria-owns. */}}
                <div id="subtitle-langs" class="flex flex-wrap gap-1.5 mb-3" role="tablist" aria-label="{{ t $.Lang "action.stream.subtitleLanguage" }}">
                    {{ range $subs }}{{ if eq .ID "none" }}
                    <button type="button" id="subtitle-off" role="radio" aria-checked="{{ if .Default }}true{{ else }}false{{ end }}"
                            data-id="none" data-provider="{{ .Provider }}" data-srclang="" data-kind="{{ .Kind }}" data-rank="{{ .Rank }}"
                            {{ if .Default }}data-default="true" {{ end }}{{ if .Saved }}data-saved="true" {{ end }}
                            class="subtitle btn btn-xs gap-1 {{ if .Default }}bg-w-cyan/15 border border-w-cyan/30 text-w-cyan{{ else }}btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan{{ end }}">
                        <svg class="chip-check w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" aria-hidden="true"{{ if not .Default }} hidden{{ end }}><polyline points="20 6 9 17 4 12"/></svg>
                        {{ t $.Lang "action.stream.off" }}
                    </button>
                    {{ end }}{{ end }}
                    {{ range $row.Groups }}
                    <button type="button" class="lang btn btn-xs gap-1 {{ if eq .Lang $row.Expanded }}bg-w-cyan/15 border border-w-cyan/30 text-w-cyan{{ else }}btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan{{ end }}"
                            role="tab" data-lang="{{ .Lang }}" aria-selected="{{ if eq .Lang $row.Expanded }}true{{ else }}false{{ end }}"{{ if .Overflow }} hidden{{ end }}>
                        {{ if .Name }}<span class="chip-flag">{{ .Flag }}</span> <span class="lang-name">{{ .Name }}</span>{{ else }}<span class="lang-name font-mono">{{ .Code }}</span>{{ end }}
                        <span class="lang-count text-w-muted font-normal">{{ .Count }}</span>
                        <span class="lang-dot w-1.5 h-1.5 rounded-full bg-w-cyan" aria-hidden="true"{{ if not .Active }} hidden{{ end }}></span>
                    </button>
                    {{ end }}
                    <button type="button" id="subtitle-lang-more" class="btn btn-xs btn-ghost border border-w-line text-w-muted hover:border-w-cyan/30 hover:text-w-cyan"
                            aria-expanded="false" title="{{ t $.Lang "action.stream.moreLanguages" }}" aria-label="{{ t $.Lang "action.stream.moreLanguages" }}"{{ if not $row.Overflow }} hidden{{ end }}>+<span class="more-count">{{ $row.Overflow }}</span></button>
                    {{/* Cloned by track-picker.js when an upload introduces a
                         language the server did not render a chip for. Filled
                         through textContent only — no HTML is built in JS. */}}
                    <template id="lang-chip-template">
                        <button type="button" class="lang btn btn-xs gap-1 btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan" role="tab" data-lang="" aria-selected="false">
                            <span class="chip-flag"></span> <span class="lang-name"></span>
                            <span class="lang-count text-w-muted font-normal">0</span>
                            <span class="lang-dot w-1.5 h-1.5 rounded-full bg-w-cyan" aria-hidden="true" hidden></span>
                        </button>
                    </template>
                </div>

                <div id="subtitle-tracks" class="flex flex-wrap gap-1.5 mb-3" role="radiogroup" aria-label="{{ t $.Lang "action.stream.subtitles" }}" aria-owns="subtitle-off">
                    {{ range $flat }}{{ if ne .ID "none" }}
                    {{ $d := langDisplay .SrcLang }}{{ $oc := originCode . }}
                    <button type="button" role="radio" aria-checked="{{ if .Default }}true{{ else }}false{{ end }}"
                            data-id="{{ .ID }}" data-mp-id="{{ .MPID }}" data-srclang="{{ .SrcLang }}" data-provider="{{ .Provider }}" data-src="{{ .Src }}" data-label="{{ .Label }}" data-kind="{{ .Kind }}" data-badge="{{ .Badge }}" data-source="{{ .Source }}" data-rank="{{ .Rank }}"
                            data-lang="{{ $d.Lang }}" data-lang-name="{{ $d.Name }}" data-lang-flag="{{ $d.Flag }}"
                            {{ if .SourceBadge }}data-source-badge="{{ .SourceBadge }}" {{ end }}{{ if .Forced }}data-forced="true" {{ end }}{{ if .Locked }}data-locked="true" aria-disabled="true" {{ end }}{{ if .Default }}data-default="true" {{ end }}{{ if .Saved }}data-saved="true" {{ end }}
                            {{ if ne $d.Lang $row.Expanded }}hidden{{ end }}
                            class="subtitle btn btn-sm gap-2 {{ if .Default }}bg-w-cyan/15 border border-w-cyan/30 text-w-cyan{{ else if .Locked }}btn-ghost border border-w-line text-w-muted opacity-70{{ else }}btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan{{ end }}">
                        <svg class="chip-check w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" aria-hidden="true"{{ if not .Default }} hidden{{ end }}><polyline points="20 6 9 17 4 12"/></svg>
                        {{ if $oc }}<span class="badge badge-xs font-mono font-semibold tracking-wider {{ if eq $oc "MY" }}bg-w-purple/10 border-w-purple/30 text-w-purpleL{{ else if eq $oc "AI" }}bg-w-pink/10 border-w-pink/30 text-w-pinkL{{ else }}bg-base-300/80 border-w-line/50 text-w-sub{{ end }}" title="{{ t $.Lang (originKey .) }}">{{ $oc }}</span>{{ end }}
                        <span class="max-w-[14rem] truncate" title="{{ .Label }}">{{ .Label }}</span>
                        {{ range propertyTags . }}<span class="badge badge-xs border-w-line/50 text-w-muted uppercase tracking-wider" title="{{ t $.Lang (printf "action.stream.badge.%s" .) }}">{{ . }}</span>{{ end }}
                        {{ if .Source }}<span class="text-w-muted font-normal">· {{ .Source }}</span>{{ end }}
                        {{ if .SourceBadge }}<span class="text-w-muted font-normal">· {{ t $.Lang "action.stream.translate.from" }} {{ originCodeForBadge .SourceBadge }}</span>{{ end }}
                        {{ if eq .Provider "Translated" }}<span class="tr-progress text-w-muted font-normal" hidden></span><span class="tr-spinner loading loading-spinner loading-xs text-w-pinkL" aria-hidden="true" hidden></span>{{ end }}
                        {{ if .Locked }}<svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg><span class="sr-only">{{ t $.Lang "action.stream.locked.aria" }}</span>{{ end }}
                    </button>
                    {{ end }}{{ end }}
                    {{ if $withUploads }}
                    {{ $usv := userSubtitleView .VideoStreamUserData.ResourceID .Item.PathStr .EIURL .UserSubtitles $subs }}
                    {{/* display:contents — the partial's chips join this flex
                         row directly while #my-subtitles stays the element
                         loadAsyncView swaps innerHTML on. */}}
                    <div id="my-subtitles" class="contents" data-async-layout='{{`{{ template "user_subtitles_view" (withContext $ .Data) }}`}}'>
                        {{ template "user_subtitles_view" withContext $ $usv }}
                    </div>
                    {{ end }}
                </div>

                <div id="translate-cta" class="mb-3 p-3 rounded-xl bg-base-200/60 border border-w-line max-w-md" hidden>
                    <div class="text-sm">{{ t $.Lang "action.stream.translate.locked" }}</div>
                    <a href="{{ langPath $.Lang "/donate" }}" target="_blank" class="btn btn-sm btn-soft mt-2" data-umami-event="donate-subtitle-translate" data-umami-event-tier="{{ if $.User | hasAuth }}free{{ else }}anon{{ end }}">{{ t $.Lang "action.stream.translate.cta" }}</a>
                </div>

                <p class="text-[0.7rem] text-w-muted">
                    <span class="font-mono">EM</span> {{ t $.Lang "action.stream.badge.embedded" }} ·
                    <span class="font-mono">IN</span> {{ t $.Lang "action.stream.badge.sidecar" }} ·
                    <span class="font-mono">OS</span> {{ t $.Lang "action.stream.badge.os" }} ·
                    <span class="font-mono">MY</span> {{ t $.Lang "action.stream.badge.user" }} ·
                    <span class="font-mono">AI</span> {{ t $.Lang "action.stream.badge.ai" }}
                </p>
            </div>

            <div class="modal-action">
                <form method="dialog" class="w-full sm:w-auto"><button class="btn btn-ghost border border-w-line text-w-sub hover:border-w-pink hover:text-base-content w-full sm:w-auto">{{ t $.Lang "action.stream.close" }}</button></form>
            </div>
        </div>
    </dialog>
```

Удаляются: `#embedded`, `#opensubtitles`, `$openSubs`/`$otherSubs`, обе кнопки `label[for=…]`, старая позиция `#my-subtitles` и `🔒`-глиф (заменён на иконку + `sr-only`).

- [ ] **Step 3: Партиал загрузок**

`templates/partials/action/user_subtitles.html` целиком:

```gotemplate
{{ define "user_subtitles_view" }}
{{ $lang := .Ctx.Lang }}
{{ $data := .Data }}
{{/* Rendered inside #subtitle-tracks (the flat chip row) through a
     display:contents wrapper, so this template emits chips first and only
     then a full-width panel, which `basis-full` pushes onto its own flex
     line. Same partial serves the initial render and the async reload
     (handlers/user_subtitle), so both shapes have to come from here.

     Selection and deletion are deliberately separate elements: the chip is
     a radio and only switches the track, while deleting a file is only
     possible from its row in the panel below. An accidental tap on a chip
     must never destroy an upload. */}}
{{ if .Ctx.User | hasAuth }}
    {{ range $data.UserSubtitles }}
    {{ $d := langDisplay .SrcLang }}
    <button type="button" role="radio" aria-checked="{{ if .Default }}true{{ else }}false{{ end }}"
            data-id="{{ .ID }}" data-provider="UserSubtitle" data-src="{{ .Src }}" data-label="{{ .OriginalName }}" data-srclang="{{ .SrcLang }}" data-kind="subtitles" data-badge="user"
            data-lang="{{ $d.Lang }}" data-lang-name="{{ $d.Name }}" data-lang-flag="{{ $d.Flag }}"
            {{/* ladderRank("UserSubtitle") — this list is rendered from its
                 own view model, so the rank is fixed here rather than
                 carried per item. */}}
            data-rank="0"
            {{ if .Default }}data-default="true" {{ end }}{{ if .Saved }}data-saved="true" {{ end }}{{ if .Selected }}data-autoselect="true" {{ end }}
            class="subtitle btn btn-sm gap-2 {{ if .Default }}bg-w-cyan/15 border border-w-cyan/30 text-w-cyan{{ else }}btn-ghost border border-w-line text-w-sub hover:border-w-cyan/30 hover:text-w-cyan{{ end }}">
        <svg class="chip-check w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" aria-hidden="true"{{ if not .Default }} hidden{{ end }}><polyline points="20 6 9 17 4 12"/></svg>
        <span class="badge badge-xs font-mono font-semibold tracking-wider bg-w-purple/10 border-w-purple/30 text-w-purpleL" title="{{ t $lang "action.stream.badge.user" }}">MY</span>
        <span class="max-w-[14rem] truncate" title="{{ .OriginalName }}">{{ .OriginalName }}</span>
    </button>
    {{ end }}
{{ end }}
<button type="button" id="my-uploads-toggle" class="btn btn-sm btn-ghost border border-dashed border-w-line text-w-muted hover:border-w-purple/40 hover:text-w-purpleL gap-1.5"
        aria-expanded="false" aria-controls="my-uploads-panel">+ {{ t $lang "action.stream.mySubtitles" }}</button>
<div id="my-uploads-panel" class="basis-full w-full mt-2 p-4 rounded-xl bg-base-200/50 border border-w-line" hidden>
    <h3 class="font-bold text-lg mb-3">{{ t $lang "action.stream.mySubtitles" }}</h3>
    {{ if $data.ErrKey }}
        <div class="text-sm text-error mb-3">{{ t $lang $data.ErrKey }}</div>
    {{ end }}
    {{ if not (.Ctx.User | hasAuth) }}
        <div class="py-6 text-center">
            <p class="mb-4 text-w-sub">{{ t $lang "action.stream.mySubtitles.signInPrompt" }}</p>
            <a class="btn btn-soft" href="{{ langPath $lang "/login" }}?return-url={{ langPath $lang (printf "/%s?file=%s" $data.ResourceID $data.Path) }}">{{ t $lang "action.stream.mySubtitles.signIn" }}</a>
        </div>
    {{ else }}
        {{ if $data.UserSubtitles }}
            {{/* One delete control per upload — the only place a file can be
                 destroyed. Same route and same async target as before the
                 redesign, so the reload path is unchanged. */}}
            <ul class="w-full bg-base-200/50 rounded-xl divide-y divide-w-line mb-4">
                {{ range $data.UserSubtitles }}
                    <li class="p-3 flex items-center gap-4">
                        <div class="flex-1 min-w-0">
                            <div class="font-medium text-sm break-all">{{ .OriginalName }}</div>
                            <div class="text-xs text-w-muted mt-0.5">{{ .Format }} · {{ .Size | bitsForHumans }}</div>
                        </div>
                        <form method="post"
                              action="{{ langPath $lang .DeleteURL }}"
                              class="shrink-0"
                              data-async-target="#my-subtitles"
                              data-async-push-state="false">
                            <input type="hidden" name="_csrf" value="{{ $.Ctx.CSRF }}">
                            <input type="hidden" name="ei_url" value="{{ $data.EIURL }}">
                            <button type="submit" class="btn btn-ghost btn-sm text-w-pinkL hover:bg-w-pink/10" aria-label="{{ t $lang "action.stream.mySubtitles.delete" }}" data-umami-event="user-subtitle-delete">
                                <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="size-5">
                                    <path stroke-linecap="round" stroke-linejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0"/>
                                </svg>
                            </button>
                        </form>
                    </li>
                {{ end }}
            </ul>
        {{ end }}
        {{ if lt (len $data.UserSubtitles) 10 }}
            <form method="post"
                  action="{{ langPath $lang "/user-subtitle" }}"
                  enctype="multipart/form-data"
                  class="user-subtitle-form"
                  data-async-target="#my-subtitles"
                  data-async-push-state="false">
                <input type="hidden" name="_csrf" value="{{ .Ctx.CSRF }}">
                <input type="hidden" name="resource_id" value="{{ $data.ResourceID }}">
                <input type="hidden" name="path" value="{{ $data.Path }}">
                <input type="hidden" name="ei_url" value="{{ $data.EIURL }}">
                <label class="upload-dashed flex flex-col items-center justify-center p-6 cursor-pointer text-center rounded">
                    <span class="text-w-sub mb-2">{{ t $lang "action.stream.mySubtitles.dragHint" }}</span>
                    <span class="text-xs text-w-muted">{{ t $lang "action.stream.mySubtitles.formats" }}</span>
                    <input type="file" name="file" accept=".srt,.vtt,.ass,text/vtt,application/x-subrip" class="hidden user-subtitle-input">
                </label>
            </form>
        {{ else }}
            <p class="text-sm text-w-sub">{{ t $lang "action.stream.mySubtitles.limitReached" }}</p>
        {{ end }}
    {{ end }}
</div>
{{ end }}
```

Строка-подсказка про `.ssa` в `accept` намеренно не трогается — это отдельный известный хвост (`project_user_subtitles_session_offset_bug`).

- [ ] **Step 4: Рендер-тесты**

В `services/template/stream_video_render_test.go` во **все три** FuncMap добавить:

```go
		"subtitleLangGroups":  helper.SubtitleLangGroups,
		"originCode":          helper.OriginCode,
		"originCodeForBadge":  helper.OriginCodeForBadge,
		"originKey":           helper.OriginKey,
		"propertyTags":        helper.PropertyTags,
		"audioSuffix":         helper.AudioSuffix,
		"langDisplay":         stremio.NewHelper().LangDisplay,
```

(импорт `"github.com/webtor-io/web-ui/services/stremio"`).

В `TestStreamVideoRendersTranslateBadgesAndCTA` список `want` заменить на:

```go
	for _, want := range []string{
		`data-rank="5"`,                 // Translated item's ladder rank
		`data-source-badge="sidecar"`,   // AI item translated from the ExportTag sidecar
		`data-badge="forced"`,           // the embedded "Forced (English)" track
		`data-forced="true"`,
		`data-locked="true"`,
		`aria-disabled="true"`,          // locked chip is not activatable
		`id="translate-cta"`,
		`action.stream.translate.locked`,
		`action.stream.translate.cta`,
		`action.stream.locked.aria`,
		// The redesign's own contract: one flat container per group, a
		// language row, and the codes the chips are read by.
		`id="subtitle-tracks"`,
		`id="subtitle-langs"`,
		`id="subtitle-off"`,
		`id="lang-chip-template"`,
		`aria-owns="subtitle-off"`,
		// title= is what tells a chip badge from the legend line, which
		// renders the same codes with no title at all.
		`title="action.stream.badge.embedded">EM<`,
		`title="action.stream.badge.sidecar">IN<`,
		`title="action.stream.badge.ai">AI<`,
		`class="tr-progress`,
		`class="tr-spinner`,
		`data-lang="en"`,
		`class="lang btn btn-xs`,
		`class="subtitle btn btn-sm`,
	} {
```

и добавить проверки, что подвидов больше нет и что порядок встроенных дорожек не переставлен:

```go
	for _, gone := range []string{`id="embedded"`, `id="opensubtitles"`, `label for="opensubtitles"`, `label for="my-subtitles"`} {
		if strings.Contains(html, gone) {
			t.Errorf("rendered stream_video.html still contains the removed sub-view markup %q", gone)
		}
	}
	// hls-manager.remapTrackGroup assigns data-mp-id by DOM order when the
	// counts match, so the embedded chips must appear in GetSubtitles order.
	if a, b := strings.Index(html, `data-mp-id="0"`), strings.Index(html, `data-mp-id="1"`); a < 0 || b < 0 || a > b {
		t.Errorf("embedded subtitle chips are out of GetSubtitles order (mp-0 at %d, mp-1 at %d)", a, b)
	}
```

В `services/template/user_subtitles_partial_render_test.go` добавить импорт `"github.com/webtor-io/web-ui/services/stremio"`, в FuncMap `TestUserSubtitlesPartialMarksSelected` — `"langDisplay": stremio.NewHelper().LangDisplay`, и проверки:

```go
	if !strings.Contains(out, `id="my-uploads-toggle"`) {
		t.Error("partial must render the My-uploads chip")
	}
	if !strings.Contains(out, `id="my-uploads-panel"`) {
		t.Error("partial must render the My-uploads panel")
	}
	if n := strings.Count(out, `class="subtitle btn btn-sm`); n != 2 {
		t.Errorf("expected two MY chips, got %d:\n%s", n, out)
	}
	if !strings.Contains(out, `data-lang="en"`) {
		t.Error("MY chips must carry the language they group under")
	}
```

И отдельный тест на удаление — уложенное требование владельца: файл зрителя остаётся удаляемым, и удаление живёт в строке панели, а не на чипе выбора.

```go
// TestUserSubtitlesPartialKeepsADeleteControlPerRow pins the half of the
// redesign that is easy to lose: the picker's chips are radios, so the
// delete form had to move into the "My uploads" panel — and a panel that
// silently stopped rendering it would leave viewers unable to remove a
// file they uploaded, with nothing failing anywhere.
//
// It also pins the separation: no chip may carry a delete form. An
// accidental tap on a selection chip must at worst switch the track.
func TestUserSubtitlesPartialKeepsADeleteControlPerRow(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	helper := i18n.NewHelper(i18n.New(locales.FS()))

	funcs := template.FuncMap{
		"t":             helper.T,
		"langPath":      func(lang, p string) string { return p },
		"hasAuth":       func(any) bool { return true },
		"bitsForHumans": func(int64) string { return "1 KB" },
		"langDisplay":   stremio.NewHelper().LangDisplay,
	}
	tpl, err := template.New("user_subtitles.html").Funcs(funcs).
		ParseFiles("../../templates/partials/action/user_subtitles.html")
	if err != nil {
		t.Fatalf("failed to parse partial: %v", err)
	}

	data := &models.UserSubtitleView{
		ResourceID: "res",
		Path:       "/movie.mkv",
		EIURL:      "http://ei",
		UserSubtitles: []models.UserSubtitleTrack{
			{ID: "us-old", OriginalName: "old.srt", Format: "srt", Size: 10, Src: "http://a", DeleteURL: "/d/1", SrcLang: "und"},
			{ID: "us-new", OriginalName: "new.en.srt", Format: "srt", Size: 20, Src: "http://b", DeleteURL: "/d/2", SrcLang: "en"},
		},
	}

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "user_subtitles_view", map[string]any{
		"Ctx":  map[string]any{"Lang": "en", "User": struct{}{}, "CSRF": "csrf"},
		"Data": data,
	}); err != nil {
		t.Fatalf("failed to render: %v", err)
	}
	out := buf.String()

	if n := strings.Count(out, `action="/d/`); n != 2 {
		t.Errorf("expected one delete form per upload row, got %d:\n%s", n, out)
	}
	if n := strings.Count(out, `data-umami-event="user-subtitle-delete"`); n != 2 {
		t.Errorf("expected one delete button per upload row, got %d", n)
	}
	if n := strings.Count(out, `data-async-target="#my-subtitles"`); n != 3 {
		t.Errorf("expected two delete forms + the upload form to reload the picker, got %d", n)
	}
	// The delete forms must live in the panel, after it opens — never
	// inside a selection chip.
	panelAt := strings.Index(out, `id="my-uploads-panel"`)
	if panelAt < 0 {
		t.Fatal("no My-uploads panel")
	}
	if first := strings.Index(out, `action="/d/`); first < panelAt {
		t.Errorf("a delete form is rendered before the panel (at %d, panel at %d) — it must not sit on a chip", first, panelAt)
	}
}
```

Run:
```bash
LD=$(grep '^PROTO_CONFLICT_LDFLAGS' Makefile | sed 's/^[^=]*:= *//') && go test -ldflags "$LD" ./services/template/ ./services/i18n/ -v
```

- [ ] **Step 5: Негативный контроль** — временно убрать `aria-owns="subtitle-off"` из шаблона → рендер-тест краснеет; вернуть. Убрать `{{ if ne $d.Lang $row.Expanded }}hidden{{ end }}` → проверить руками, что без JS видны все дорожки (это и есть SSR-деградация, см. «Поведение без JS»), вернуть.

- [ ] **Step 6: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && git status -sb
git add templates/views/action/stream_video.html templates/partials/action/user_subtitles.html services/template/stream_video_render_test.go services/template/user_subtitles_partial_render_test.go locales/en.json locales/ru.json locales/es.json locales/de.json locales/fr.json locales/pt.json locales/it.json locales/pl.json locales/tr.json locales/nl.json locales/cs.json
git commit -m "player modal: one-screen track picker — language chips, origin codes, inline uploads"
```

---

### Task 3: Чистый модуль `track-picker.js`

**Files:**
- Create: `assets/src/js/lib/player/track-picker.js`, `assets/src/js/lib/player/track-picker.test.js`

**Interfaces:**
- Produces (чистые): `groupByLang(chips)`, `activeLang(chips)`, `expandedLangFor(chips, {preferred, current})`, `langRowOps(chips, rowChips, expanded)`.
- Produces (DOM): `readChips(container)`, `applyLangFilter(container, lang)`, `syncLangRow(container)`, `syncNow(container)`, `setChipActive(el, on)`, `applyFlagSupport(container)`, `refreshMarks(container)`, `refresh(container)`.
- Consumes: `supportsFlagEmoji` из `../discover/lang.js`.

- [ ] **Step 1: Падающий тест**

```js
// assets/src/js/lib/player/track-picker.test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { groupByLang, activeLang, expandedLangFor, langRowOps, syncLangRow } from './track-picker.js';

// Same fixture as handlers/action.TestSubtitleLangGroupsOrdersActiveThenPreferredThenCount:
// the row is rendered by Go and recomputed here after an upload, and the
// two orders must be the same one.
const C = (id, lang, extra = {}) => ({ id, lang, isDefault: false, ...extra });

test('groupByLang: active first, then preferred, then count, then order', () => {
    const chips = [
        C('a', 'en'), C('b', 'en'), C('c', 'en'),
        C('d', 'ru', { isDefault: true }), C('e', 'ru'),
        C('f', 'de'),
        C('g', 'und'),
    ];
    assert.deepEqual(groupByLang(chips, 'de'), [
        { lang: 'ru', count: 2, active: true },
        { lang: 'de', count: 1, active: false },
        { lang: 'en', count: 3, active: false },
        { lang: 'und', count: 1, active: false },
    ]);
});

test('groupByLang: preferred first when nothing is active', () => {
    const chips = [C('a', 'en'), C('b', 'en'), C('c', 'de')];
    assert.deepEqual(groupByLang(chips, 'de').map((g) => g.lang), ['de', 'en']);
});

test('activeLang is empty when subtitles are off', () => {
    assert.equal(activeLang([C('a', 'en'), C('b', 'ru')]), '');
    assert.equal(activeLang([C('a', 'en'), C('b', 'ru', { isDefault: true })]), 'ru');
});

test('expandedLangFor keeps the language the viewer opened when nothing is active', () => {
    const chips = [C('a', 'en'), C('b', 'ru')];
    assert.equal(expandedLangFor(chips, { preferred: 'de', current: 'en' }), 'en');
    // The current language lost its last track (deleted upload) — fall back.
    assert.equal(expandedLangFor(chips, { preferred: 'ru', current: 'pl' }), 'ru');
    assert.equal(expandedLangFor(chips, { preferred: 'pl', current: 'pl' }), 'en');
    // An active track always wins over both.
    assert.equal(expandedLangFor([C('a', 'en'), C('b', 'ru', { isDefault: true })], { preferred: 'en', current: 'en' }), 'ru');
});

test('langRowOps: counts, dot, and the chip a new upload needs', () => {
    const chips = [C('a', 'en'), C('b', 'ru', { isDefault: true }), C('c', 'pl', { langName: 'Polish', langFlag: '🇵🇱' })];
    const row = [{ lang: 'en' }, { lang: 'ru' }];
    const ops = langRowOps(chips, row, 'ru');
    assert.deepEqual(ops.updates, [
        { lang: 'en', count: 1, active: false, selected: false, hidden: false },
        { lang: 'ru', count: 1, active: true, selected: true, hidden: false },
    ]);
    assert.deepEqual(ops.missing, [{ lang: 'pl', count: 1, name: 'Polish', flag: '🇵🇱' }]);
});

// Deleting the last upload of a language is the one event that can empty a
// chip out from under the viewer: the row still has the chip the server
// rendered, and the tracks container no longer has anything in it.
test('post-delete: the emptied language chip drops to 0 and hides', () => {
    const before = [C('a', 'en'), C('us-1', 'pl', { langName: 'Polish', langFlag: '🇵🇱' })];
    const row = [{ lang: 'en' }, { lang: 'pl' }];
    assert.deepEqual(langRowOps(before, row, 'pl').updates, [
        { lang: 'en', count: 1, active: false, selected: false, hidden: false },
        { lang: 'pl', count: 1, active: false, selected: true, hidden: false },
    ]);
    const after = [C('a', 'en')];
    assert.deepEqual(langRowOps(after, row, 'pl').updates, [
        { lang: 'en', count: 1, active: false, selected: false, hidden: false },
        { lang: 'pl', count: 0, active: false, selected: true, hidden: true },
    ]);
    // …and the row must not stay expanded on a language with no tracks.
    assert.equal(expandedLangFor(after, { preferred: 'de', current: 'pl' }), 'en');
});

// The same event through the DOM half, on a fake container: after the
// partial is swapped in without the deleted chip, a refresh must leave the
// row honest and the surviving chips visible.
test('post-delete: syncLangRow rewrites counts and hides the emptied chip', () => {
    const chip = (lang, attrs = {}) => {
        const a = { 'data-id': 'x-' + lang, 'data-lang': lang, 'data-provider': 'OpenSubtitles', ...attrs };
        return { hidden: false, getAttribute: (n) => (n in a ? a[n] : null), setAttribute: (n, v) => { a[n] = v; }, querySelector: () => null };
    };
    const span = () => ({ textContent: '', hidden: false });
    const langChip = (lang) => {
        const a = { 'data-lang': lang, 'aria-selected': lang === 'pl' ? 'true' : 'false' };
        const parts = { '.lang-count': span(), '.lang-dot': span() };
        return {
            hidden: false,
            classList: { toggle() {} },
            getAttribute: (n) => (n in a ? a[n] : null),
            setAttribute: (n, v) => { a[n] = v; },
            querySelector: (s) => parts[s] || null,
            parts,
        };
    };
    const en = langChip('en'), pl = langChip('pl');
    const tracks = { querySelectorAll: () => [chip('en')] };          // the pl upload is gone
    const langs = {
        querySelectorAll: () => [en, pl],
        querySelector: (s) => (s === '#subtitle-lang-more' || s === '#lang-chip-template' ? null : null),
        insertBefore() {},
    };
    const container = {
        getAttribute: (n) => (n === 'data-preferred-lang' ? 'de' : null),
        querySelector: (s) => (s === '#subtitle-tracks' ? tracks : s === '#subtitle-langs' ? langs : null),
    };
    syncLangRow(container);
    assert.equal(en.parts['.lang-count'].textContent, '1');
    assert.equal(pl.parts['.lang-count'].textContent, '0');
    assert.equal(en.hidden, false);
    assert.equal(pl.hidden, true);
});

test('langRowOps hides everything past the fourth chip but never the active one', () => {
    const chips = ['en', 'de', 'fr', 'es', 'it'].map((l, i) => C('x' + i, l));
    chips.push(C('y', 'cs', { isDefault: true }));
    const row = ['en', 'de', 'fr', 'es', 'it', 'cs'].map((lang) => ({ lang }));
    const ops = langRowOps(chips, row, 'cs');
    const hidden = ops.updates.filter((u) => u.hidden).map((u) => u.lang);
    assert.deepEqual(hidden, ['es', 'it']);
    assert.equal(ops.overflow, 2);
});
```

Run: `npm test` → FAIL, модуль не существует.

- [ ] **Step 2: Реализация**

```js
// assets/src/js/lib/player/track-picker.js
//
// The track picker (docs/uikit.html §19) is server-rendered: Go emits every
// audio and subtitle chip into one flat container per group plus the
// subtitle language row. This module is the progressive-enhancement half —
// it filters the flat list by language, keeps the row's counts and the dot
// on the active language honest, and writes the "Now:" summary. Without it
// the dialog still shows every track and every chip is still clickable.
//
// The pure half (groupByLang / activeLang / expandedLangFor / langRowOps)
// takes plain objects so node --test can exercise it, and mirrors
// handlers/action.Helper.SubtitleLangGroups exactly: Go renders the first
// row, this recomputes it after an upload adds or removes a track, and a
// disagreement between the two would make the row jump on the first click.

import { supportsFlagEmoji } from '../discover/lang.js';

// Mirrors maxVisibleLangChips in handlers/action/picker.go.
const MAX_VISIBLE_LANGS = 4;

const ACTIVE_CLASSES = ['bg-w-cyan/15', 'border-w-cyan/30', 'text-w-cyan'];
const INACTIVE_CLASSES = ['btn-ghost', 'border-w-line', 'text-w-sub'];

// groupByLang collapses chips into language groups, ordered the way the
// row is drawn: the language playing, then the viewer's preferred one,
// then by how many tracks it has, then by the order the server rendered
// (Array.prototype.sort is stable, so the input order is the tie-break).
export function groupByLang(chips, preferred = '') {
    const order = [];
    const byLang = new Map();
    for (const c of chips) {
        if (!c || c.id === 'none') continue;
        const lang = c.lang || 'und';
        let g = byLang.get(lang);
        if (!g) {
            g = { lang, count: 0, active: false };
            byLang.set(lang, g);
            order.push(g);
        }
        g.count++;
        if (c.isDefault) g.active = true;
    }
    return order.slice().sort((a, b) => {
        if (a.active !== b.active) return a.active ? -1 : 1;
        const pa = a.lang === preferred, pb = b.lang === preferred;
        if (pa !== pb) return pa ? -1 : 1;
        return b.count - a.count;
    });
}

// activeLang is the language of the track playing, or '' when subtitles
// are off. "Off" is a choice, not a language, so it has no group.
export function activeLang(chips) {
    for (const c of chips) {
        if (c && c.isDefault && c.id !== 'none') return c.lang || 'und';
    }
    return '';
}

// expandedLangFor answers which language's tracks the row shows. The
// active one wins; otherwise the viewer's own last choice, as long as it
// still has tracks (deleting the last upload of a language must not leave
// an empty list); otherwise the preferred language; otherwise the first
// group.
export function expandedLangFor(chips, { preferred = '', current = '' } = {}) {
    const groups = groupByLang(chips, preferred);
    if (!groups.length) return '';
    const active = activeLang(chips);
    if (active) return active;
    const has = (l) => groups.some((g) => g.lang === l);
    if (current && has(current)) return current;
    if (preferred && has(preferred)) return preferred;
    return groups[0].lang;
}

// langRowOps is what the row has to become: one update per chip already in
// it, the chips a language has but the row does not (an upload in a new
// language), and how many chips end up behind "+N".
export function langRowOps(chips, rowChips, expanded, preferred = '') {
    const groups = groupByLang(chips, preferred);
    const pos = new Map(groups.map((g, i) => [g.lang, i]));
    const updates = [];
    for (const rc of rowChips) {
        const g = groups.find((x) => x.lang === rc.lang);
        const i = pos.has(rc.lang) ? pos.get(rc.lang) : Number.MAX_SAFE_INTEGER;
        updates.push({
            lang: rc.lang,
            count: g ? g.count : 0,
            active: !!(g && g.active),
            selected: rc.lang === expanded,
            hidden: !g || i >= MAX_VISIBLE_LANGS,
        });
    }
    const known = new Set(rowChips.map((rc) => rc.lang));
    const missing = [];
    for (const g of groups) {
        if (known.has(g.lang)) continue;
        const src = chips.find((c) => (c.lang || 'und') === g.lang) || {};
        missing.push({ lang: g.lang, count: g.count, name: src.langName || '', flag: src.langFlag || '' });
    }
    return { updates, missing, overflow: updates.filter((u) => u.hidden).length + missing.length };
}

// ---- DOM half -------------------------------------------------------

function chipData(el) {
    return {
        id: el.getAttribute('data-id') || '',
        lang: el.getAttribute('data-lang') || 'und',
        langName: el.getAttribute('data-lang-name') || '',
        langFlag: el.getAttribute('data-lang-flag') || '',
        isDefault: el.getAttribute('data-default') === 'true',
        el,
    };
}

// readChips reads the flat subtitle list. Scoped to #subtitle-tracks on
// purpose: the "Off" chip lives in the language row (it is the .subtitle
// element for data-id="none"), and it is a choice rather than a language.
export function readChips(container) {
    const box = container && container.querySelector('#subtitle-tracks');
    if (!box) return [];
    return Array.from(box.querySelectorAll('.subtitle[data-provider]')).map(chipData);
}

function langChips(container) {
    const row = container && container.querySelector('#subtitle-langs');
    if (!row) return [];
    return Array.from(row.querySelectorAll('.lang[data-lang]'));
}

// setChipActive is the one place the active look is written: cyan fill plus
// the check icon that was already in the markup. Classes and hidden only —
// never innerHTML, so a chip keeps its origin badge, its property tag and
// its translation-progress span across every selection.
export function setChipActive(el, on) {
    if (!el || !el.classList) return;
    // A locked chip (the AI track on a free account) is drawn dimmed
    // (text-w-muted opacity-70) and can never become active. Writing
    // text-w-sub onto it would leave two colour utilities on one element
    // and let stylesheet order decide which one the viewer sees.
    if (!on && el.getAttribute && el.getAttribute('data-locked') === 'true') {
        el.setAttribute('aria-checked', 'false');
        return;
    }
    for (const c of ACTIVE_CLASSES) el.classList.toggle(c, on);
    for (const c of INACTIVE_CLASSES) el.classList.toggle(c, !on);
    el.setAttribute('aria-checked', on ? 'true' : 'false');
    const check = el.querySelector && el.querySelector('.chip-check');
    if (check) check.hidden = !on;
}

// applyLangFilter shows the tracks of one language and hides the rest. The
// "Off" chip is outside this container and is never hidden.
export function applyLangFilter(container, lang) {
    for (const c of readChips(container)) {
        c.el.hidden = c.lang !== lang;
    }
    for (const el of langChips(container)) {
        const on = el.getAttribute('data-lang') === lang;
        el.setAttribute('aria-selected', on ? 'true' : 'false');
        for (const c of ACTIVE_CLASSES) el.classList.toggle(c, on);
        for (const c of INACTIVE_CLASSES) el.classList.toggle(c, !on);
    }
}

export function expandedLang(container) {
    for (const el of langChips(container)) {
        if (el.getAttribute('aria-selected') === 'true') return el.getAttribute('data-lang');
    }
    return '';
}

// syncLangRow rewrites the counts, the dot, the overflow and — when an
// upload brought a language the server did not render a chip for — clones
// one out of <template id="lang-chip-template">. Filled through
// textContent: no HTML is built here.
export function syncLangRow(container) {
    const row = container && container.querySelector('#subtitle-langs');
    if (!row) return;
    const preferred = (container.getAttribute && container.getAttribute('data-preferred-lang')) || '';
    const chips = readChips(container);
    const expanded = expandedLang(container);
    const els = langChips(container);
    const ops = langRowOps(chips, els.map((el) => ({ lang: el.getAttribute('data-lang') })), expanded, preferred);

    const more = row.querySelector('#subtitle-lang-more');
    const expandedRow = more ? more.getAttribute('aria-expanded') === 'true' : false;
    els.forEach((el, i) => {
        const u = ops.updates[i];
        const count = el.querySelector('.lang-count');
        if (count) count.textContent = String(u.count);
        const dot = el.querySelector('.lang-dot');
        if (dot) dot.hidden = !u.active;
        el.hidden = u.hidden && !expandedRow;
    });

    const tpl = row.querySelector('#lang-chip-template');
    if (tpl && tpl.content) {
        for (const m of ops.missing) {
            const el = tpl.content.firstElementChild.cloneNode(true);
            el.setAttribute('data-lang', m.lang);
            const flag = el.querySelector('.chip-flag');
            if (flag) {
                flag.textContent = m.flag;
                flag.hidden = !m.flag || !supportsFlagEmoji();
            }
            const name = el.querySelector('.lang-name');
            if (name) name.textContent = m.name || m.lang.toUpperCase();
            const count = el.querySelector('.lang-count');
            if (count) count.textContent = String(m.count);
            row.insertBefore(el, more || null);
        }
    }
    if (more) {
        more.hidden = ops.overflow === 0 || expandedRow;
        const n = more.querySelector('.more-count');
        if (n) n.textContent = String(ops.overflow);
    }
}

// syncNow writes the "Now:" summary of both groups off the DOM, reusing
// the strings the server already rendered on the active chip. Nothing is
// translated here — the client has no copy of these names.
export function syncNow(container) {
    const write = (boxID, selector) => {
        const box = container.querySelector(boxID);
        if (!box) return;
        const value = box.querySelector('.now-value');
        const origin = box.querySelector('.now-origin');
        const el = container.querySelector(selector);
        if (origin) {
            const code = el && el.querySelector('.badge.font-mono');
            origin.textContent = code ? code.textContent.trim() : '';
            origin.hidden = !code;
        }
        if (!value) return;
        if (!el) { value.textContent = ''; return; }
        const flag = el.getAttribute('data-lang-flag') || '';
        const name = el.getAttribute('data-lang-name') || '';
        const label = el.getAttribute('data-label') || el.textContent.trim();
        const head = name ? ((flag && supportsFlagEmoji() ? flag + ' ' : '') + name) : label;
        value.textContent = head;
    };
    write('#audio-now', '.audio[data-default="true"]');
    write('#subtitle-now', '.subtitle[data-default="true"]');
}

// applyFlagSupport hides every flag when the platform renders regional
// indicators as bare letter pairs (Windows outside Firefox) — the same
// guard Discover applies to its own chips.
export function applyFlagSupport(container) {
    if (supportsFlagEmoji()) return;
    for (const el of container.querySelectorAll('.chip-flag')) el.hidden = true;
}

// refreshMarks is what a selection needs: the row's dot and the summaries.
// It deliberately does NOT re-run the language filter — the viewer's
// expanded language is their choice and must not jump under them.
export function refreshMarks(container) {
    syncLangRow(container);
    syncNow(container);
}

// refresh is the full pass: used when the dialog opens and after the
// uploads partial is swapped in, where the set of chips itself changed.
export function refresh(container, { current = '' } = {}) {
    const preferred = (container.getAttribute && container.getAttribute('data-preferred-lang')) || '';
    const lang = expandedLangFor(readChips(container), {
        preferred,
        current: current || expandedLang(container),
    });
    applyLangFilter(container, lang);
    applyFlagSupport(container);
    refreshMarks(container);
}
```

Run: `npm test` → все PASS (106 существующих + 6 новых).

- [ ] **Step 3: Негативный контроль** — временно убрать `if (a.active !== b.active)` из компаратора `groupByLang` → краснеет первый тест; убрать `if (!g || i >= MAX_VISIBLE_LANGS)` → краснеет тест переполнения; вернуть.

- [ ] **Step 4: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && git status -sb
git add assets/src/js/lib/player/track-picker.js assets/src/js/lib/player/track-picker.test.js
git commit -m "player: track-picker module — language grouping, chip marking, Now summary"
```

---

### Task 4: Проводка в `Player.jsx`, новый `markTrack`, удаление мёртвого кода

**Files:**
- Modify: `assets/src/js/lib/player/Player.jsx`
- Modify: `assets/src/js/lib/player/hls-manager.js`
- Modify: `assets/src/js/lib/player/subtitle-telemetry.js` (комментарий)
- Modify: `assets/src/styles/style.css`

**Interfaces:**
- Consumes: `track-picker.js`.

- [ ] **Step 1: `markTrack` и импорт**

В шапке `Player.jsx` добавить:

```js
import { refresh, refreshMarks, applyLangFilter, setChipActive, expandedLang } from './track-picker.js';
```

Заменить `markTrack` целиком:

```js
// markTrack moves the active marker of one group (audio or subtitle) onto
// `el`. The look is a cyan fill plus the check icon that is already in
// every chip's markup — toggled, never rebuilt: a chip carries its origin
// badge, its property tag and, on the AI item, the .tr-progress span of a
// running translation, and innerHTML would throw all three away mid-poll.
function markTrack(container, el, type, persist = true) {
    if (el.getAttribute('data-default') === 'true') return;
    setChipActive(el, true);
    el.setAttribute('data-default', 'true');

    const s = container.querySelector('#subtitles');
    if (!s) return;
    const es = s.querySelectorAll(`.${type}`);
    for (const ee of es) {
        if (ee === el) continue;
        setChipActive(ee, false);
        ee.removeAttribute('data-default');
    }
    // The dot on the language chip and the "Now:" line, not the language
    // filter: the viewer's expanded language is their own choice.
    refreshMarks(s);

    if (!persist) return;
    fetch(`/stream-video/${type}`, {
        method: 'PUT',
        headers: {
            'Content-Type': 'application/json',
            'X-CSRF-TOKEN': window._CSRF,
        },
        body: JSON.stringify({
            id: el.getAttribute('data-id'),
            resourceID: s.getAttribute('data-resource-id'),
            itemID: s.getAttribute('data-item-id'),
        }),
    });
}
```

- [ ] **Step 2: `wireTrackHandlers` — клики по языкам, «+N», «+ Загрузить», аудио-фикс, удаление подвидов**

В `wireTrackHandlers`, внутри `if (subtitlesModal) { … }`, после существующего делегата на `.subtitle` добавить делегат на языковой ряд и раскрытие:

```js
        // Language row: filter only. A language chip never changes what is
        // playing — the "Off" chip does, and it is a .subtitle, handled by
        // the delegate above.
        subtitlesModal.addEventListener('click', (e) => {
            const lang = e.target.closest('.lang[data-lang]');
            if (lang && subtitlesModal.contains(lang)) {
                applyLangFilter(subtitlesModal, lang.getAttribute('data-lang'));
                return;
            }
            const more = e.target.closest('#subtitle-lang-more');
            if (more && subtitlesModal.contains(more)) {
                more.setAttribute('aria-expanded', 'true');
                for (const el of subtitlesModal.querySelectorAll('#subtitle-langs .lang[data-lang]')) el.hidden = false;
                more.hidden = true;
                return;
            }
            const upload = e.target.closest('#my-uploads-toggle');
            if (upload && subtitlesModal.contains(upload)) {
                const panel = subtitlesModal.querySelector('#my-uploads-panel');
                if (!panel) return;
                const open = panel.hidden;
                panel.hidden = !open;
                upload.setAttribute('aria-expanded', open ? 'true' : 'false');
                // The toggle and the panel are both replaced on every async
                // swap; the wrapper is not, so the open state lives there.
                const wrap = subtitlesModal.querySelector('#my-subtitles');
                if (wrap) wrap.setAttribute('data-upload-open', open ? 'true' : 'false');
            }
        });
```

Аудио-обработчик (Р-9):

```js
    // Audio click handlers (no async swap — direct binding is enough).
    // e.target.closest, not e.target: a chip's click lands on the flag
    // <span> or the check <svg> as often as on the button itself, and
    // markTrack would then mark a <span> and read data-mp-id as null.
    for (const audio of container.querySelectorAll('.audio')) {
        audio.addEventListener('click', (e) => {
            const target = e.target.closest('.audio');
            if (!target) return;
            markTrack(container, target, 'audio');
            if (window.hlsPlayer && target.getAttribute('data-provider') === 'MediaProbe') {
                window.hlsPlayer.audioTrack = parseInt(target.getAttribute('data-mp-id'));
            }
            if (hooks.onAudioSelect) hooks.onAudioSelect(target);
        });
    }
```

`syncMySubtitleMark` переименовать в `syncUploadMarks` и перевести на чипы:

```js
    // The <track default> in <video> drives playback correctly on reload,
    // but the uploads' chips are re-rendered by the partial and never go
    // through the click path, so they lose the active marker. Re-derive it
    // from the live textTracks and re-run on every async swap.
    const syncUploadMarks = () => {
        const video = container.querySelector('video.player');
        const mySubs = container.querySelector('#my-subtitles');
        if (!video || !mySubs) return;
        let activeID = null;
        for (const t of video.textTracks) {
            if (t.mode === 'showing' && t.id) { activeID = t.id; break; }
        }
        if (!activeID) {
            const dt = video.querySelector('track[default]');
            if (dt && dt.id) activeID = dt.id;
        }
        if (!activeID) return;
        for (const item of mySubs.querySelectorAll('.subtitle')) {
            const isActive = item.getAttribute('data-id') === activeID;
            setChipActive(item, isActive);
            if (isActive) item.setAttribute('data-default', 'true');
            else item.removeAttribute('data-default');
        }
    };
    syncUploadMarks();
```

Обработчик `async` — восстановить панель и пересобрать пикер:

```js
    const mySubsContainer = container.querySelector('#my-subtitles');
    if (mySubsContainer) {
        window.addEventListener('async', (e) => {
            if (!e.detail || e.detail.target !== mySubsContainer) return;
            // The panel and its toggle are part of the swapped markup; the
            // wrapper is not, so it is what remembers whether the viewer
            // had the upload form open.
            // After a delete the viewer is still looking at the panel: it
            // must come back open, or removing two files in a row means
            // re-opening it between them.
            if (mySubsContainer.getAttribute('data-upload-open') === 'true') {
                const panel = mySubsContainer.querySelector('#my-uploads-panel');
                const toggle = mySubsContainer.querySelector('#my-uploads-toggle');
                if (panel) panel.hidden = false;
                if (toggle) toggle.setAttribute('aria-expanded', 'true');
            }
            const fresh = mySubsContainer.querySelector('.subtitle[data-autoselect="true"]');
            if (fresh) {
                activateSubtitle(container, fresh);
                // A subtitle just uploaded in a language the row had no chip
                // for must not land behind "+N": expand its language.
                applyLangFilter(subtitlesModal, fresh.getAttribute('data-lang') || 'und');
                refreshMarks(subtitlesModal);
                return;
            }
            syncUploadMarks();
            // A delete: the chip went away with the innerHTML swap, but the
            // language row still carries its count, and the expanded
            // language may have just lost its last track.
            refresh(subtitlesModal, { current: expandedLang(subtitlesModal) });
        });
    }
```

Удалить целиком блок подвидов (`const embedded = …` … закрывающая скобка цикла `for (const v of views)`).

В конце `wireTrackHandlers` добавить первичный проход:

```js
    // First pass: hide the tracks of every collapsed language, drop flags
    // where the platform cannot draw them, fill both "Now:" lines.
    if (subtitlesModal) refresh(subtitlesModal);
```

- [ ] **Step 3: Прогресс перевода в чипе**

В `startTranslationProgress` добавить рядом со `span` спиннер и заменить записи в него.

Взятие узлов (рядом с существующим `const span = el.querySelector('.tr-progress');`):

```js
        const spinner = el.querySelector('.tr-spinner');
```

Старт:

```js
        if (span) {
            span.hidden = false;
            span.textContent = '· 0%';
            span.title = tf('player.subtitleTranslating', 0);
        }
        if (spinner) spinner.hidden = false;
```

Спиннер гасится везде, где гаснет `span`: в `fail` (`if (spinner) spinner.hidden = true;`), в `onDone` и в `stopTranslationProgress`. Для последнего добавить рядом с `progressSpanRef` второй ref:

```js
    // The chip's spinner, paired with progressSpanRef: a spinner left
    // running after the poll stops says a translation is still going.
    const progressSpinnerRef = useRef(null);
```

и в `stopTranslationProgress`:

```js
        if (progressSpinnerRef.current) {
            progressSpinnerRef.current.hidden = true;
            progressSpinnerRef.current = null;
        }
```

(присваивание `progressSpinnerRef.current = spinner;` — там же, где `progressSpanRef.current = span;`; сброс в `fail`/`onDone` — там же, где `progressSpanRef.current = null`).

```js
            onProgress: (p) => {
                cues = p.total;
                const pct = p.total > 0 ? Math.round((100 * p.done) / p.total) : 0;
                if (span) {
                    // Design (docs/uikit.html §19): the chip says "· 37%".
                    // The sentence "Translating… 37%" is still the one
                    // localized string, and it lives in the title.
                    span.textContent = `· ${pct}%`;
                    span.title = tf('player.subtitleTranslating', pct);
                }
```

Остальное (`fail`, `onDone`, `stopTranslationProgress`) уже ставит `span.hidden = true` — не трогать.

- [ ] **Step 4: `hls-manager.js` (Р-10)**

```js
    const used = new Set();
    for (const el of elements) {
        const lang = (el.getAttribute('data-srclang') || '').toLowerCase();
        // data-label, not textContent: a picker chip's text now includes the
        // origin code ("EM"), the property tag ("forced") and the source
        // suffix ("· hash"), so an exact compare against the manifest's
        // track name would never match again.
        const label = (el.getAttribute('data-label') || el.textContent || '').trim();
        if (!lang || !label) continue;
```

- [ ] **Step 5: Комментарии и мёртвый CSS**

В `subtitle-telemetry.js` заменить абзац про форму элемента:

```js
// readAllTracks is every item the picker renders, the "None" entry
// included. `[data-provider]` is the trait every chip shares: tracks live
// in #subtitle-tracks and the "None" entry is the "Off" chip at the head
// of the language row (templates/views/action/stream_video.html), with the
// uploads rendered into the same flat row by the user_subtitles_view
// partial.
```

В `assets/src/styles/style.css` удалить правило `#my-subtitles ul > li:has(.subtitle[data-default="true"]) { … }` вместе с его комментарием — активное состояние теперь на чипе, а списка `<li>` в `#my-subtitles` больше нет.

- [ ] **Step 6: Тесты и сборка**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && npm test && npm run build
grep -o 'font-mono\|border-dashed\|basis-full' assets/dist/style.css | sort -u
```
Ожидание: `npm test` — все PASS; `npm run build` — только известное предупреждение о размере бандла; grep находит все три утилиты (доказательство, что Tailwind подхватил классы из шаблона, а не что они «должны были» появиться).

- [ ] **Step 7: Негативный контроль** — временно вернуть `const target = e.target` в аудио-обработчике и кликнуть по флагу внутри чипа на локальном стриме: чип не подсвечивается, в консоли `audioTrack = NaN`. Вернуть `closest`.

- [ ] **Step 8: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && git status -sb
git add assets/src/js/lib/player/Player.jsx assets/src/js/lib/player/hls-manager.js assets/src/js/lib/player/subtitle-telemetry.js assets/src/styles/style.css
git commit -m "player: wire the track picker, chip-based active marker, drop the sub-view toggles"
```

---

### Task 5: Документация, полный прогон, стейдж

**Files:**
- Modify: `docs/subtitle_translate.md`, `docs/user-subtitles.md`, `docs/uikit.html`

- [ ] **Step 1: `docs/subtitle_translate.md`** — заменить раздел «Template attributes (`data-*` per list-item kind)»:

```markdown
## Template attributes (`data-*` per chip)

Since the picker redesign there is one flat container per group and no sub-views.
Every chip is a `<button type="button">`; `.audio`/`.subtitle` and the whole `data-*`
set are unchanged from the `<li>` era, so every JS reader (`readAllTracks`,
`findSubtitleItem`, `remapTrackGroup`, `initDefaultTracks`) kept working across the
change.

| Kind | Where | Attributes |
|---|---|---|
| `.audio` | `#audio-tracks` | `data-id`, `data-mp-id`, `data-srclang`, `data-provider`, `data-lang`, `data-lang-name`, `data-lang-flag`, `data-default` |
| `.subtitle` | `#subtitle-tracks` (everything except uploads: embedded, sidecar, OpenSubtitles, embed externals, AI) | `data-id`, `data-mp-id`, `data-srclang`, `data-provider`, `data-src`, `data-label`, `data-kind`, `data-badge`, `data-source`, `data-rank`, `data-lang`, `data-lang-name`, `data-lang-flag`, `data-source-badge` (Translated only), `data-forced`, `data-locked`, `data-default`, `data-saved` |
| `.subtitle` | `#my-subtitles` (`display:contents`) inside `#subtitle-tracks`, from `templates/partials/action/user_subtitles.html` | `data-id`, `data-provider="UserSubtitle"`, `data-src`, `data-label`, `data-srclang`, `data-kind`, `data-badge="user"`, `data-rank="0"` (fixed — this view model has no ladder), `data-lang*`, `data-default`, `data-saved`, `data-autoselect="true"` when just uploaded |
| `.subtitle#subtitle-off` | head of `#subtitle-langs` — the "Off" chip **is** the `None` item | `data-id="none"`, `data-provider=""`, `data-kind`, `data-rank`, `data-default`, `data-saved` |
| `.lang` | `#subtitle-langs` | `data-lang`, `aria-selected` |

The AI chip additionally carries `.tr-progress` (the "· 37%" span) and `.tr-spinner`; both
are `hidden` unless a translation is polling. `setChipActive` never rebuilds a chip's
innerHTML, so they survive every selection.

Deletion is not on a chip: a chip is a `role="radio"` and only switches the track. The
delete form per upload lives in `#my-uploads-panel`, which the `+ My Subtitles` chip
(`#my-uploads-toggle`) opens on its own flex line. The panel posts to the unchanged
`POST /user-subtitle/delete/:id` with `data-async-target="#my-subtitles"`; the `async`
handler in `Player.jsx` then re-runs the picker so the chip row, the language counts and
the expanded language all follow.

`data-lang` is the base language tag the chip groups under, `und` when unknown;
`data-lang-name`/`data-lang-flag` carry the display strings so the client can clone a
new language chip out of `<template id="lang-chip-template">` without a language table
of its own. All three come from `stremio.NewLangDisplay`.
```

Дальше — новый раздел:

```markdown
## Track picker

One screen, no sub-views (`docs/uikit.html` §19). Audio is a chip row; subtitles are a
language row plus the tracks of the expanded language. Origin codes `EM` embedded,
`IN` in torrent, `OS` OpenSubtitles, `MY` my uploads, `AI` translation are the same
string in every locale — the meaning is in `title=` and in the legend line, both built
from `action.stream.badge.*`. `forced` is a property tag, not an origin: a forced
embedded track shows `EM` + `forced`.

Server side: `handlers/action/picker.go` (`SubtitleLangGroups` → the row, `OriginCode`,
`PropertyTags`, `AudioSuffix`) and `services/stremio/lang_display.go` (`langDisplay`).
Client side: `assets/src/js/lib/player/track-picker.js` — the same grouping rule again,
because an upload changes the counts after the server has rendered. The two orderings
(active language → preferred → count → render order) must stay identical; the Go and
the JS test carry the same fixture.

Without JS every chip is rendered and clickable and the expanded language's tracks are
visible; what JS adds is the language filter, the counts, the dot and the "Now:" line.
```

- [ ] **Step 2: `docs/user-subtitles.md`** — найти и поправить упоминания вкладки:

```bash
grep -n "My Subtitles\|my-subtitles\|tab\|вкладк" docs/user-subtitles.md
```
Заменить описание «отдельная вкладка модалки» на «чипы `MY` в общем списке + панель загрузки за чипом “+ Загрузить”, async-цель `#my-subtitles` с `display:contents`».

- [ ] **Step 3: `docs/uikit.html` §19** — дописать в `uikit-rule` две вещи, которых в макете нет, а в реализации есть: «Off» — это и есть элемент дорожки `none` (`aria-owns` из группы дорожек), и неизвестный язык группируется под моно-кодом `UND`. Больше ничего в секции не менять: макет остаётся контрактом.

- [ ] **Step 4: Полный прогон**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && make test && npm test && npm run build
```

- [ ] **Step 5: Ручной чек-лист (локально, затем `web-stage`)**

Деплой стейджа: `cd /Users/vintikzzzz/Projects/webtor/infra/helmfile && ./sync.sh --wait web-stage` (алиас; `web-ui-alt` аргументом падает). После — проверить живое состояние, а не код возврата.

1. Файл с встроенными + приложенными + OpenSubtitles-дорожками: открыть пикер — одна ширма, аудио-чипы сверху, языковой ряд, дорожки активного языка. «Сейчас:» в обеих строках заполнено.
2. Клик по другому языку — меняется только видимый набор дорожек; точка остаётся на языке играющей дорожки.
3. Клик по дорожке — галочка и циановая заливка переезжают, `PUT /stream-video/subtitle` уходит (Network), точка переезжает, «Сейчас:» обновляется.
4. «Выкл.» — субтитры выключаются, галочка на чипе «Выкл.», `data-default` только на нём.
5. Смена аудио на дорожку другого языка без ручного выбора субтитров — правило лестницы отрабатывает, чип подсвечивается сам.
6. «+ Мои субтитры» раскрывает панель; загрузка `.srt` — чип `MY` появляется в ряду, выбирается сам, панель остаётся открытой, счётчик языка увеличился.
6а. **Удаление.** В панели у каждой загрузки своя корзина. Удалить одну: строка и чип `MY` исчезают, счётчик языка уменьшается, панель остаётся открытой (можно удалить вторую подряд). Удалить последнюю загрузку языка, который сейчас раскрыт: чип языка исчезает из ряда, раскрывается предпочитаемый язык, список дорожек не пустой. На чипе выбора кнопки удаления нет ни в одном состоянии.
7. Бесплатный аккаунт, AI-дорожка: чип с замком, `aria-disabled`, клик раскрывает `#translate-cta`, выбор не меняется. Платный: `· 0%` → `· 37%` → дорожка появляется.
8. Больше пяти языков — видны 4 + «+N»; клик по «+N» раскрывает остальные.
9. Мобильный вьюпорт 360px: чипы переносятся, дорожки остаются `btn-sm`, длинные имена обрезаны и полностью читаются в `title`.
10. Fullscreen: модалка открывается поверх (в `top layer`) — поведение не менялось, но подтвердить.
11. Windows/Chrome (или подменить `supportsFlagEmoji` в консоли на `() => false`): флагов нет, имена языков на месте.
12. **Проверка вкладкой на переднем плане.** Скрытая вкладка молча стопорит hls.js — все проверки плеера гонять в активной вкладке (`document.visibilityState === 'visible'`).

- [ ] **Step 6: Коммит**

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && git status -sb
git add docs/subtitle_translate.md docs/user-subtitles.md docs/uikit.html
git commit -m "docs: track picker — chip attributes, grouping rules, upload panel"
```

---

## Порядок и зависимости

1 → 2 → 3 → 4 → 5. После Task 1 зелено всё; после Task 2 шаблон уже работает без JS (Task 3/4 ещё не написаны — старый `markTrack` подсвечивает `text-primary underline`, что визуально неверно, но ничего не ломает); Tasks 3 и 4 можно делать параллельно только при условии, что Task 4 не мержится до Task 3. `make test` обязателен в Task 5.

## Поведение без JS

Модалка открывается только из JS (`showModal()`), так что «без JS» здесь означает «JS пикера не загрузился, плеер загрузился»: видны все чипы обеих групп, кроме дорожек свёрнутых языков (их прячет серверный `hidden`), активная дорожка помечена галочкой и заливкой из SSR, «Сейчас:» пустая (её пишет JS), счётчики верны, языковой ряд не фильтрует. Переключение дорожек при полностью выключенном JS невозможно и раньше — обработчики всегда были клиентские.

## Behaviour changes for viewers

1. **Подвидов больше нет.** Кнопки «OpenSubtitles» и «My Subtitles» убраны; дорожки этих происхождений лежат в общем списке под кодами `OS` и `MY`. Путь «открыть модалку → нажать OpenSubtitles → выбрать» сокращается до «открыть → выбрать язык → выбрать».
2. **Субтитры теперь отфильтрованы по языку.** По умолчанию раскрыт язык играющей дорожки (или предпочитаемый, если субтитры выключены). Дорожки других языков не видны, пока не нажат их чип. Для файла с 40+ дорожками OpenSubtitles это главный выигрыш; для файла с двумя дорожками на разных языках — один лишний клик.
3. **Активный выбор помечается галочкой и заливкой, а не подчёркиванием.** Подчёркивание пропадало на touch-hover и не читалось при цветовой слепоте.
4. **Происхождение дорожки видно всегда** — код `EM/IN/OS/MY/AI` на каждом чипе, а не только пометка на части из них.
5. **Строка «Сейчас:»** в заголовке обеих групп: что играет, видно без прокрутки рядов (особенно на телефоне).
6. **Загрузка и удаление — инлайн.** Пунктирный чип «+ Мои субтитры» раскрывает панель под рядом чипов: форма загрузки и список своих файлов с корзиной в каждой строке. Загруженный файл появляется чипом `MY` в том же ряду и выбирается сам (как и раньше). Удаление осталось на прежнем маршруте и в прежней async-схеме, но живёт в строке списка, а не на чипе выбора: чип — радиокнопка, и случайный тап по нему не должен уничтожать файл. После удаления ряд чипов и языковой ряд пересобираются.
7. **Заблокированная AI-дорожка** получает иконку замка вместо эмодзи и `sr-only`-пояснение; прогресс перевода показывается внутри чипа как `· 37%`.
8. **Флаги языков** появляются там, где платформа их рисует, и исчезают целиком там, где нет (Windows вне Firefox) — имена языков остаются.
9. **Клавиатура.** Чипы стали кнопками: обходятся табом и активируются Enter/Space. Раньше `<li>` не фокусировались вообще.

## Что сознательно не делается

- **`sdh` как свойство дорожки.** Нарисовано в макете, но `content-prober` не отдаёт `disposition` из ffprobe; `PropertyTags` расширяется одной строкой, когда отдаст.
- **Стрелочная навигация внутри radiogroup/tablist** (Left/Right между чипами с roving tabindex). Таб + Enter работают; полноценный паттерн WAI-ARIA — отдельная задача, и он нужен одинаково в Discover, где тот же `chipClass` уже живёт без него.
- **Переезд на строки вместо чипов при >8 дорожках в одном языке.** Правило записано в uikit как условие на будущее; сейчас языковой ряд уже режет список до единиц.
- **Клиентская локализация чего-либо в пикере.** Все строки серверные; в бандл плеера новых ключей не добавляется.
- **Embed-виджет** (`DomainSettings`) получает тот же пикер, но без панели загрузки — это существующее поведение, отдельно не расширяется.
- **Поиск/фильтр по имени файла внутри языка.** Не требуется на текущих объёмах.
- **Перенос `#translate-cta` внутрь чипа** или его превращение в поповер: карточка остаётся отдельным блоком под рядом, как в макете.
- **Кэш-ключ стрим-джобы и предпочитаемый язык** — известное ограничение из `docs/subtitle_translate.md`, редизайном не задевается.
- **Остановка воспроизведения удалённой дорожки.** Удаление загрузки, которая сейчас играет, убирает чип и строку, но созданный ранее `<track>` остаётся в `<video>` и продолжает показывать субтитры до перезагрузки страницы. Поведение существующее (`syncMySubtitleMark` так же выходил ни с чем, не найдя элемента) и редизайном не меняется; чинить — отдельной задачей в `activateSubtitle`/`ensureTrackElement`, где живёт владение `<track>`-элементами.
- **Подтверждение удаления.** Корзина удаляет сразу, как и сегодня. Разделение выбора и удаления (Р-3а) снимает главный риск случайного нажатия; диалог подтверждения не добавляем, чтобы не плодить ещё один слой в модалке поверх модалки.
