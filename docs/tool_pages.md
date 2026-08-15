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
