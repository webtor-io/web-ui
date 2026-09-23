# Tool pages (SEO landings)

Каждый URL из `handlers/common.Tools` — отдельная посадочная страница
(`/torrent-to-mp4`, `/open-torrent-file`, …). Роут, sitemap, футер и
перелинковка подхватываются из этого списка автоматически.

## Тело страницы — данные, не шаблон

До 2026-08-15 тело каждой страницы было отдельным партиалом на ~130 строк в
`templates/partials/about/`. Их было 19 (2537 строк), и они отличались только
префиксом i18n-ключей: все страницы — один и тот же набор из пяти секций.
Новая страница означала копирование чужого файла и замену префикса — самый
дешёвый способ случайно отрендерить чужой текст.

Теперь страница описывается списком секций в `handlers/common/tools.go`:

```go
{Url: "torrent-to-mp4", Title: "tool.torrentToMp4.title", …, Sections: []AboutSection{
    {Kind: AboutSteps,     Key: "steps",     Badge: "howItWorks", Accent: "pink",   CTA: "discover"},
    {Kind: AboutProse,     Key: "explained", Badge: "explained",  Accent: "purple", Alt: true, Paras: []string{"p1", "p2"}},
    {Kind: AboutChecklist, Key: "benefits",  Badge: "benefits",   Accent: "pink",   Items: 4},
    {Kind: AboutProse,     Key: "safety",    Badge: "safety",     Accent: "purple", Alt: true, Paras: []string{"text"}},
    {Kind: AboutChecklist, Key: "devices",   Badge: "devices",    Accent: "pink",   Items: 4},
}},
```

Разметку рендерит `templates/partials/about/sections.html`. Типы секций:

| Kind | Что это | Обязательные поля |
|---|---|---|
| `AboutSteps` | три пронумерованные карточки, несёт якорь `#how` | `CTA` (`discover`/`stremio`) |
| `AboutProse` | заголовок + 1–3 абзаца | `Paras` (`p1`,`p2`,`p3` или `text`) |
| `AboutChecklist` | заголовок, подзаголовок, сетка пунктов с галочками | `Items` |
| `AboutCompare` | две подписанные колонки буллетов | `Cols` (ровно две) |

Дополнительные флаги: `Alt` (тёмный фон, секции чередуются), `Footer`/`Note`
(закрывающий абзац), `Extra` (врезка под чеклистом), `Link` (кросс-ссылка на
другой лендинг), `Icon` (глиф, если он не совпадает с именем бейджа).

Шестой тип секции — сигнал, что странице нужен свой партиал, а не ещё один
флаг.

## Ключи

Префикс не пишется в литералах: `Tool.AboutKey()` выводит его из URL
(`torrent-to-mp4` → `tool.torrentToMp4.about`) и штампуется в каждую секцию
в `init()`. Секция сама собирает полные ключи (`FieldKey`, `ItemKeys`,
`ColItemKeys`), поэтому страница физически не может отрендерить копирайт
другой страницы.

Соглашение kebab-URL → camel-ключ проверяется тестом
`TestAboutKeyFollowsTheURL`.

## Как добавить страницу

1. Запись в `Tools` с `Sections`.
2. Ключи `tool.<camelUrl>.*` во **всех** локалях (`locales/*.json`):
   `title`, `benefit`, `description` плюс всё, что просят секции.
3. `go test ./handlers/common/ ./services/template/` — тесты скажут, чего не
   хватает, поимённо и по каждому языку.
4. `-update` снапшотов не требуется: новая страница добавляет свой файл сама.

## Что защищает изменения

| Тест | Что ловит |
|---|---|
| `handlers/common.TestEveryToolDeclaresItsBody` | зарегистрированный URL без тела — пустая страница между hero и футером |
| `services/template.TestAboutSnapshots` | правка `sections.html` тихо меняет разметку всех 18 страниц. Снапшоты в `services/template/testdata/about/`; `go test ./services/template/ -run TestAbout -update` перезаписывает их — **читать диф, а не обновлять вслепую** |
| `services/template.TestAboutCopyExistsInEveryLocale` | ключ, который страница реально рендерит, отсутствует в каком-то языке (на SEO-странице это выводит сырой ключ). Список ключей берётся из отрендеренной разметки, а не из догадки о том, что просит шаблон |
| `services/template.TestAboutPartialsUseTheirOwnKeys` | страница рендерит чужой префикс |

Снапшоты, которыми обложен рефакторинг 2026-08-15, сняты **до** него: все 18
страниц после перехода на секции рендерятся байт-в-байт (с точностью до
пробелов) так же, как рендерились постраничные партиалы.

## t против tHTML

Прозаические поля (`stepN.text`, `subtitle`, `explained.p*`, `safety.text`,
`compare.footer`, `video.subtitle`) рендерятся через `tHTML`: примерно треть
их значений содержит `<strong>` хотя бы в одном языке. Короткие подписи
(`title`, `label`, `itemN`) — через `t`; разметки в них нет ни в одной локали,
и это проверено по всем 11 файлам.

## Намерение tool-страницы после сабмита

С 2026-09-23 страница, на которой человек отправил торрент, не теряется по
дороге до страницы ресурса.

**Прогресс.** Все три формы tool-страницы (поиск, дропзона, демо) шлют скрытое
`instruction` — путь страницы. `resource.newPostData` ищет его через
`common.ToolByURL` и кладёт `Tool` в `PostData`, поэтому ответ на сабмит
рендерит hero, H1 и `<title>` той же tool-страницы. Раньше `Tool` не
заполнялся, и под адресом `/magnet-to-torrent` (POST отвечает 202, `async.js`
адрес не меняет) показывались H1 и `<title>` главной. Неизвестное значение
`instruction` сбрасывается в пустое — это главная; раньше такая страница не
рендерила ни тело главной, ни тело tool-страницы.

**Слаг до страницы ресурса — `?tool=<slug>`.** Хост лога
(`partials/load/progress.html`) при наличии `Tool` несёт
`<input type="hidden" name="tool">`, а `progressLog.js` на редиректе div-хоста
превращает скрытые инпуты хоста в query-параметры (тот же путь, которым
Discover передаёт `file-idx`). Итог: `/<hash>?tool=magnet-to-torrent`.

- **Не в `jobs/load.go`.** Load-джоба ключуется `sha1(hash + "/" + lang)`, её
  лог хранится и реплеится каждому, кто грузит тот же торрент на том же языке.
  Слаг, зашитый в сохранённый редирект, уезжал бы от первого посетителя ко
  всем остальным.
- **Query, а не сессия.** Без состояния; виден в URL pageview Umami
  (`exclude_search` не включён), так что сегмент «пришёл с tool-страницы»
  меряется без нового события; не нужно писать в сессию на каждый сабмит и
  решать, когда значение гасить; перезагрузки async-layout
  (`window.location.href`) сохраняют параметр сами.
- **`tool`, а не `from`.** `from` уже занят: `RedirectWithError/Success`
  пишут туда путь запроса, страница ресурса читает `from=/vault/add` и
  `from=/vault/remove`.
- **Whitelist — одно место:** `common.ToolByURL`. Неизвестный слаг ничего не
  выбирает и в страницу не попадает.

**Что параметр не трогает.** Страница ресурса — `X-Robots-Tag: noindex,
follow` и без canonical (проверено на живом проде 2026-09-23); hreflang и
языковые ссылки в навигации строятся из `URL.Path`; sitemap — из
`common.Tools`; ссылки внутри страницы ресурса собирает сервер из ID, `tool`
в них нет. «Поделиться» (`lib/share/share.js`) вырезает `tool` из ссылки: это
намерение того, кто делится, не получателя. Edge (проверено 2026-09-23)
HTML страниц ресурса не кэширует, а его правила для этих страниц смотрят на
путь, не на query; referer запросов `/status` с такой страницы по-прежнему
содержит домен.

**Что видно на странице ресурса.**

| Откуда | Что | Где |
|---|---|---|
| `/magnet-to-torrent` | строка `resource.toolIntent.magnetToTorrent` («Or download the files directly — no torrent client needed») и кнопка `resource.download` под парой `.torrent`/magnet | `partials/resource/tool_intent.html` |
| `/torrent-to-magnet` | кнопка magnet показывает подпись `resource.copyMagnet` и зелёная (цвет, который она иначе получает на hover) | `resource/torrent_magnet_split` в `views/resource/get.html` |
| любая tool-страница | `tool` как prop события `download-torrent` и `copy-magnet` | там же, `lib/share/share.js` |

Кнопка строки — ссылка на `#content`: без JS браузер прокручивает к карточке
файла и списку. С JS `lib/toolIntent.js` нажимает кнопку самой страницы:
архив каталога (TAR, `#list form.download-dir`), если есть список, иначе
«Скачать» файла (`#file form.download`) — с Turnstile, busy-состоянием,
модалкой архива и событием этой кнопки. Своей копии этих форм у строки нет,
кнопка ищется в момент нажатия (выбор файла или каталога подменяет
`#content`/`#list`). О скорости строка молчит: время скачивания называет
карточка самой джобы (`downloadPitch`).

Почему именно magnet-to-torrent (Umami, SEO-разбор 2026-09): 38.7% её
сессий качают только `.torrent`, 23.5% — файлы; у «только `.torrent`»
платных переходов 0, у качающих файлы — 8.1 на 1000.

**Umami.** `tool-intent-direct` (+ `tool`) — нажатия строки. Следом стреляет
событие нажатой кнопки — `download-dir` (`format: tar`) или `download`, так
что эти счётчики включают нажатия через строку. Pageview —
`/<hash>?tool=<slug>`. Эффект: доля сессий с pageview `?tool=magnet-to-torrent`,
которые качают файлы, против тех, что жмут только `download-torrent`.

**Известные дыры.**

- Ретрай мёртвого магнета («ждать 10 минут», `load/errors/magnet`) — форма
  карточки, отрисованной джобой; `instruction` в ней нет и быть не может (лог
  общий), поэтому после удачного ретрая редирект уходит без `?tool=`. Hero
  страницы остаётся tool-страницы: ретрай меняет только `#log-load`.
- Поиск и дропзона в навигации шлют без `instruction` — они глобальные.
- `/torrent-to-zip`, `/torrent-to-ddl`, `/magnet-to-ddl` пока ничего не
  подсвечивают: сначала замер по magnet-to-torrent и по pageview остальных.

| Тест | Что ловит |
|---|---|
| `handlers/common.TestToolByURL` | whitelist пропускает что-то кроме зарегистрированных URL |
| `handlers/resource.TestNewPostDataResolvesTheTool`, `TestProgressPageKeepsTheToolPage` | прогресс после сабмита с tool-страницы рендерит главную; хост лога не несёт `tool` |
| `jobs.TestLoadProgressPartialCarriesTheTool` | `tool` не внутри хоста (progressLog.js читает только его инпуты) |
| `handlers/resource.TestBindGetArgsReadsTheTool` | `?tool=` не читается, читается неизвестный, параметр попадает в путь контекста |
| `handlers/resource.TestToolIntentLine`, `TestTorrentMagnetSplitCarriesTheTool` | строка не там, где нужно, или без перевода; пара `.torrent`/magnet меняется для обычного визита |
| `handlers/resource.TestToolIntentFixturesAreCurrent` + `assets/src/js/lib/toolIntent.test.js` | строка нажимает не ту кнопку; фикстура — реальная разметка из шаблонов, перегенерация — команда в тексте падения |
| `assets/src/js/lib/share/share.test.js` | «Поделиться» уносит `?tool=` получателю; `copy-magnet` теряет prop `tool` |
