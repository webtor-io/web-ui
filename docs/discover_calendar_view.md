# Discover — Calendar View

Статус: **v1 shipped** (2026-05-20). Local-only — push/deploy ещё не выполнен.
Дата фиксации плана: 2026-05-20.
Связано: `docs/release_sub_fake_door.md` — параллельный эксперимент в том же "future-engagement" кластере (Calendar — pull, fake-door — push).

## v1 — что вошло

Поведение:
- В шапке каталога Discover видна пара кнопок **[Grid] [Calendar]** (cyan-themed join, как переключатель Catalog | Watchlist).
- Кнопки видны только если: текущий тип = `series` (для Watchlist — фильтр выставлен в `series`), есть хотя бы один item, не в search-режиме.
- Переключение мгновенное, без перезагрузки. Preference глобальная (один `viewMode` для всех series-каталогов), хранится в `localStorage` `discover-prefs.viewMode`.
- В календаре карточки эпизодов сгруппированы по датам (`-7 дней … +21 день` относительно локальной полуночи). Заголовки дней: `Today, Wednesday May 20` / `Tomorrow, Thursday May 21` / `Yesterday, …` / просто `Friday May 22`. Локаль берётся из `getLang()` через `toLocaleDateString`.
- Клик по эпизоду открывает обычный `StreamModal` напрямую на стримах конкретной S/E (минуя промежуточный список серий). `backToEpisodes` собирается из meta'шки, уже подгруженной для календаря — без второго `fetchMeta`.
- Loading skeleton + empty-state. Empty-state — текстовый CTA, без кнопки переключения каталога (упрощение v1).

Файлы:
- `assets/src/js/lib/discover/components/CalendarView.jsx` — новый компонент, владеет fetch-life-cycle + группировкой + рендером.
- `assets/src/js/lib/discover/components/discoverReducer.js` — `viewMode: 'grid'|'calendar'` в state, `SET_VIEW_MODE` action, гидрация в `INIT_SUCCESS`.
- `assets/src/js/lib/discover/prefs.js` — добавлен `getViewMode()` (читает из существующего `discover-prefs` blob, без отдельного ключа).
- `assets/src/js/lib/discover/client.js` — `fetchMeta` теперь кеширует положительные ответы в `LRUCache` (раньше пересчитывалось каждый раз; LoadMore в календаре зависел от этого).
- `assets/src/js/lib/discover/components/DiscoverApp.jsx` — toggle UI в sticky bar, `selectViewMode`, `openEpisodeFromCalendar`, conditional render.
- `locales/{en,ru,es,de,fr,pt,it,pl,tr,nl,cs}.json` — 8 новых ключей.

Telemetry (Umami, kebab-case):
- `discover-view-mode-grid` / `discover-view-mode-calendar` — клик по toggle.
- `discover-calendar-shown` — mount компонента, properties: `series_count`.
- `discover-calendar-episode-click` — клик по эпизоду, properties: `item_id`, `season`, `episode`.

## Что отложено относительно плана

- **Sticky date headers** — убраны для v1 (parent имеет свой sticky-bar 72-160px высоты; точный offset зависит от наличия CatalogSelector + AddonHealthChip). Заголовки дней не sticky, прокрутка обычная. Если adoption высокий, можно вернуться к sticky с CSS-измерением.
- **Empty-state CTA-ссылка на Trending** — заменена текстовой подсказкой "Try a Trending series catalog or your Watchlist". Полноценный router-switch требует завязки на каталог-id Cinemeta'ы, которого может не быть у юзера; v2.
- **Кеш `viewMode` per-catalog** — оставлен глобальным, как и в плане.

## Зачем

Дать юзерам **календарное** представление сериальных каталогов Discover — вместо grid'а постеров показывать timeline эпизодов с датами выхода. Главный гипотетический эффект: retention-tick "что выходит на этой неделе/выходных", который привязывает юзера к платформе как к ленте релизов.

В отличие от классического "Sonarr Calendar" (который тащит данные сам из индексеров), здесь мы **переиспользуем существующий Discover data pipeline** — Stremio addon catalogs + Cinemeta meta. Никаких новых сервисов, новых таблиц, background-refresh job'ов. Просто **ещё одна view component** рядом с `ItemGrid`, питающаяся теми же `items[]`.

## Зачем

Дать юзерам **календарное** представление сериальных каталогов Discover — вместо grid'а постеров показывать timeline эпизодов с датами выхода. Главный гипотетический эффект: retention-tick "что выходит на этой неделе/выходных", который привязывает юзера к платформе как к ленте релизов.

В отличие от классического "Sonarr Calendar" (который тащит данные сам из индексеров), здесь мы **переиспользуем существующий Discover data pipeline** — Stremio addon catalogs + Cinemeta meta. Никаких новых сервисов, новых таблиц, background-refresh job'ов. Просто **ещё одна view component** рядом с `ItemGrid`, питающаяся теми же `items[]`.

## Архитектурное решение

Calendar — это **view mode** существующего каталога, не отдельная страница. Юзер:

1. Заходит в Discover, выбирает sериальный каталог (например "Cinemeta Trending Series", или watchlist).
2. В шапке каталога видит toggle: `[📋 Grid] [📅 Calendar]`.
3. Переключение между видами мгновенное (без перезагрузки), state сохраняется в prefs.

Это даёт принципиальную композицию: **любой series-каталог получает Calendar бесплатно**, в том числе пользовательские (Watchlist становится "мой персональный календарь сериалов").

## Источники данных

Всё уже есть в `assets/src/js/lib/discover/client.js`:

- `fetchCatalog(baseUrl, type, catalogId, skip)` — отдаёт **light items** (id, name, poster, type, releaseInfo). Это то что показывается в Grid сейчас.
- `fetchMeta(type, id)` — отдаёт **full meta**, включая `videos[]` с `season`, `episode`, `title`, `released` (date string). Cinemeta-first, fallback на user-addons.

Для Calendar:
- Итерируем items каталога.
- Для каждого делаем `fetchMeta` (через `Promise.allSettled` с throttle 5-way parallel).
- Из каждого meta берём `videos[]`, фильтруем по `released` в окне `[-7 дней, +21 день]` от `today`.
- Группируем по дате.

**Cache**: meta-результаты уже кешируются в `client.cache` (см. fetchCatalog/fetchMeta). На повторных visit'ах Calendar открывается мгновенно.

## UX

### Toggle

В шапке каталога, рядом с другими view-controls. Условия видимости:

- Каталог `type = series` (для movies — Calendar бессмыслен, toggle **скрыт**).
- В каталоге есть ≥1 item (на пустом — toggle скрыт; Empty state — стандартный grid-режим).

Иконки + tooltip:
- `📋` "Grid view" (i18n: `discover.view.grid`)
- `📅` "Calendar view" (i18n: `discover.view.calendar`)

### Calendar layout

Mobile-first, вертикальная timeline. Сгруппирована **по датам**, sticky-header даты при скролле.

```
─────────────────────────────────────────
Today, Wednesday May 20
─────────────────────────────────────────
[poster] Severance — S02E07 "Cold Harbor"
[poster] FROM      — S03E04 "Sins of the..."

─────────────────────────────────────────
Tomorrow, Thursday May 21
─────────────────────────────────────────
[poster] House of the Dragon — S03E02

─────────────────────────────────────────
Friday May 22
─────────────────────────────────────────
[poster] The Boys — S05E03 "Truth Hurts"
…
```

### Карточка эпизода

- Постер сериала (маленький, ~80×120)
- Название сериала (bold)
- S/E + название эпизода (если есть)
- Дата + (если применимо) "сегодня" / "вчера" / day-of-week
- Клик → существующий `StreamModal` flow (тот же что в Grid). Никакой отдельной обработки.

### Empty state

Каталог series, но из всех meta'шек ни один эпизод не попадает в окно:

> В этом каталоге нет новых серий в ближайшие 3 недели.
> Попробуйте каталог [Trending TV Shows].

(Ссылка переключает каталог через router.)

## Файлы

| Что | Где |
|---|---|
| Новый компонент | `assets/src/js/lib/discover/components/CalendarView.jsx` |
| View-toggle UI | расширить `DiscoverApp.jsx` или `Tabs.jsx` (где сейчас controls каталога) |
| State в reducer | `assets/src/js/lib/discover/components/discoverReducer.js` — действие `SET_VIEW_MODE` |
| Persist preference | `assets/src/js/lib/discover/prefs.js` — get/set `viewMode` |
| i18n keys | `locales/{en,ru,es,de,fr,pt,it,pl,tr,nl,cs}.json`: `discover.view.grid`, `discover.view.calendar`, `discover.calendar.today`, `discover.calendar.tomorrow`, `discover.calendar.empty`, `discover.calendar.tryTrending`, etc. |

## Реализация — детали

### Batch fetch с throttle

Простой паттерн: 5-параллельных запросов, остальные ждут.

```js
async function fetchVideosForCatalog(items, client, signal) {
    const CONCURRENCY = 5;
    const out = [];
    const queue = [...items.filter(i => i.type === 'series')];
    const workers = Array.from({ length: CONCURRENCY }, async () => {
        while (queue.length) {
            const item = queue.shift();
            try {
                const meta = await client.fetchMeta('series', item.id, { signal });
                if (meta?.videos?.length) {
                    out.push({ item, videos: meta.videos });
                }
            } catch (e) { /* ignore failures, drop the item */ }
        }
    });
    await Promise.all(workers);
    return out;
}
```

### Окно дат и группировка

```js
const today = new Date();
today.setHours(0, 0, 0, 0);
const from = new Date(today); from.setDate(from.getDate() - 7);
const to   = new Date(today); to.setDate(to.getDate() + 21);

const flattened = []; // [{ item, video, dateKey }]
for (const { item, videos } of catalog) {
    for (const v of videos) {
        if (!v.released) continue;
        const d = new Date(v.released);
        if (isNaN(d) || d < from || d > to) continue;
        const dateKey = d.toISOString().slice(0, 10); // YYYY-MM-DD
        flattened.push({ item, video: v, dateKey });
    }
}
// group by dateKey, sort
```

### Loading skeleton

Пока идут meta-fetches — показываем skeleton (5-7 пустых date-headers с placeholder-cards). Cinemeta обычно отвечает ~200-500ms на запрос, при 5-way concurrency полная загрузка 25 items = ~1-2 сек на холодном кэше.

### State в reducer

```js
// discoverReducer.js
case 'SET_VIEW_MODE':
    return { ...state, viewMode: action.payload };

// initial state
viewMode: prefs.getViewMode() || 'grid',
```

### Persistence

В `prefs.js`:

```js
export function getViewMode() {
    try { return localStorage.getItem('webtor.discover.viewMode') || 'grid'; }
    catch { return 'grid'; }
}
export function setViewMode(mode) {
    try { localStorage.setItem('webtor.discover.viewMode', mode); } catch {}
}
```

**Решение**: одно глобальное preference для всех series-каталогов (не per-catalog-id). Юзер думает "я хочу видеть всё календарём" один раз, потом везде так.

## Telemetry

Все события через Umami, kebab-case (как везде в webtor — см. `grace-soft-cta-shown`):

| Event | Когда |
|---|---|
| `discover-view-mode-grid` | Юзер переключился на Grid (из Calendar) |
| `discover-view-mode-calendar` | Юзер переключился на Calendar (из Grid) |
| `discover-calendar-shown` | CalendarView отрендерился (IntersectionObserver или mount) |
| `discover-calendar-episode-click` | Клик по эпизоду в Calendar (для сравнения engagement vs Grid-card-click) |

Properties: `catalog_id`, `addon_base_url`, `tier`, `is_anon`, `episodes_in_window` (количество эпизодов которые попали в timeline).

## Тонкие места

1. **Episodes без `released`**. Cinemeta для далёкого будущего иногда отдаёт `videos[]` без даты. **Фильтруем эти эпизоды**, не показываем.

2. **Pre-air vs aired-but-not-on-webtor**. На v1 не различаем доступность через webtor. Клик ведёт в обычный `StreamModal`, который сам решит "стримим / нет торрентов". Если штатное окно "stream not available" будет часто всплывать — это уже сигнал для v2 (фильтр availability).

3. **Не-series в series-каталоге**. Иногда catalog'и подмешивают movies или specials. В Calendar pipeline **фильтруем `type !== 'series'`**, они просто не попадают в timeline.

4. **Меняющийся каталог**. Если юзер в Calendar и сменил каталог через CatalogSelector — данные перерасчитываются с нуля. CalendarView должна корректно реагировать на смену `items` props (useEffect, abort предыдущего batch fetch через AbortController).

5. **Watchlist-каталог**. Watchlist по сути уже series-каталог. На нём Calendar становится **личным календарём** юзера — это самый ценный кейс. Стоит протестировать отдельно: фильтровать только airing-сериалы? Или показывать все? **Решение для v1**: показывать все, окно дат само отфильтрует неактуальные.

6. **Timezone**. `released` даты Cinemeta — UTC midnight. Парсим в локальную TZ, групповой ключ берём по локальному дню. Это правильно для UX "что выходит сегодня".

## Что НЕ делаем в v1

- iCal export (paid feature, отложено)
- Web push / email уведомления о выходящих эпизодах (зависит от fake-door результата)
- "Доступно на webtor" badge (требует запросов в media_info, дополнительная сложность)
- Background refresh для устаревших meta'шек (Cinemeta достаточно свежая)
- Trakt sync
- Curated empty-state recommendations (просто текстовый CTA)
- Анонимная аудитория (Discover уже требует регистрации для caталог-customization, Calendar следует тому же паттерну)

## После запуска — что мерим

Через **2-4 недели** после деплоя:

1. **Adoption**: какая доля юзеров, открывших series-каталог, переключилась на Calendar хотя бы раз. Сегментировано по тарифу.
2. **Stickiness**: сколько юзеров **остались** на Calendar (preference saved → они в нём при следующем визите)? vs кто попробовал и вернулся в Grid.
3. **Engagement**: `discover-calendar-episode-click / discover-calendar-shown` — CTR на эпизоды в timeline. Сравнение с grid-card-click rate (если такое событие уже есть, иначе добавить).
4. **Bonus signal**: вырос ли общий retention у юзеров которые включили Calendar (returning sessions per week)? Хотя attribution сложная, watch как proxy.

Если **adoption <5%** через месяц — Calendar не нашёл аудиторию, view-toggle можно убрать, оставив только тех кто включил его в prefs. Если **adoption >20%** — расширяем (iCal export, push, "Доступно на webtor" badge).

## Связь с release-sub fake-door

Эти две фичи **взаимно подкрепляют** "future-engagement" гипотезу:

- Если **fake-door fails** (release-subscribe-banner CTR <5%) и **Calendar adoption fails** (<5%) → весь "future-engagement" кластер не работает на webtor, поведенческая модель транзакционная. Сильный сигнал на пивот.
- Если **fake-door fails** но **Calendar adopts** (>10%) → push-канал отвергнут, pull-канал работает. Развиваем Calendar (iCal, Watchlist-calendar как самостоятельная страница).
- Если **fake-door passes** и **Calendar adopts** → весь кластер живой, делаем subscription + Calendar + push intersected (например, в Calendar — bell-icon "подписаться на эту раздачу").
- Если **fake-door passes** но **Calendar fails** → юзер хочет push, но не pull discovery. Уходим в email digest + Telegram bot.

Поэтому **запускать параллельно** — правильно: они проверяют **смежные**, но не идентичные гипотезы, и любая комбинация исходов даёт ясный next step.
