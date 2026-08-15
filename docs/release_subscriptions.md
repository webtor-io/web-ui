# Release Subscriptions — план работ

Статус: **спринт 1 сделан** (схема, сервис, API, профиль, GDPR-экспорт), спринты 2–4 впереди. Дата фиксации плана: 2026-08-15.
Предшественник: `docs/release_sub_fake_door.md` (fake-door 2026-05-20 → 05-24; free 1.3% CTR, paid 13.6% — фича валидирована как платная по классу, но здесь она делается для всех с лимитом 3 на free по решению владельца).

## Что строим

Подписка на **контент**, а не на конкретную раздачу. Пока подписка активна, пользователь получает письма о новых раздачах (инфохэшах), которых не было в предыдущих письмах по этой же подписке.

Два вида подписки:

| Вид | Ключ | Когда предлагаем | Что считается событием |
|---|---|---|---|
| `season` | `video_id` + `season` | незавершённый сезон сериала | новый инфохэш по любому эпизоду сезона (включая season-паки) |
| `movie` | `video_id` | поиск стримов не дал результатов | новый инфохэш по фильму |

Три точки входа (все три из ТЗ):
1. Discover → карточка сериала → селектор сезонов: незавершённый сезон получает кнопку подписки.
2. Страница раздачи (`/{infohash}`): торрент опознан как сериал в незавершённом сезоне → баннер.
3. Discover → модалка стримов: пустой результат — и «стримов нет вообще», и «фильтры ничего не выбрали».

## Что переиспользуем (и почему почти ничего не пишем с нуля)

| Нужно | Уже есть | Файл |
|---|---|---|
| «сериал ещё выходит» | `Enricher.IsAiringSeries` + capability `AiringChecker` (TMDB читает `status`/`in_production`) | `services/enrich/enrich.go:290`, `services/enrich/tmdb.go:619` |
| даты выхода эпизодов | `episode_metadata.air_date`, наполняется `enrichEpisodes`/`MapEpisodes` | `models/episode_metadata.go`, `services/enrich/enrich.go:1092` |
| «доминирующий сезон торрента» | `dominantSeason` — удалён при закрытии fake-door, восстанавливается из `f8e9f36` | `handlers/resource/release_subscribe_banner.go` |
| поиск раздач от имени юзера | `Builder.BuildStreamsService` — аддоны + torznab + dedup по инфохэшу + language filter | `services/stremio/builder.go:104` |
| поиск сезон-паков по запросу эпизода | `season_filter.go` (`S01-S03`, «3 сезон: 1-8 серии» и т.п.) | `services/stremio/season_filter.go` |
| отправка писем + дедуп + журнал | `notification.Service.Send` (шаблон + запись в `notification` + SMTP) | `services/notification/notification.go` |
| периодический запуск | CLI-команда + CronJob в чарте (`vault reap`, `notification send`, `enrich popular`) | `notification.go`, `chart/templates/cronjobs.yaml` |
| лимит free-тарифа | `cla.Context.Tier.Id == 0` + `checkFreeTierLimit` (3 индексера) | `handlers/torznab_indexer/handler.go:97` |
| JSON-API для Preact | `handlers/discover_watchlist` — двухуровневый handler, 402 на лимите, `/ids` для бейджей | `handlers/discover_watchlist/handler.go` |
| список-настройка в профиле | `handlers/common/list_form.go` + `lib/profile/listEditor.js` (после `b24600a`) | профиль addons/indexers |
| SSR-баннер на странице раздачи | слот уже стоит в шаблоне, шаблон+JS от fake-door остались как скаффолд | `templates/views/resource/get.html:259` |
| ссылка на раздачу из письма | GET `/magnet:?xt=urn:btih:<hash>` — торрент не обязан быть в store | `handlers/resource/post.go:35` |

Пишем с нуля: 2 таблицы, сервис подписок, поллер, 3 письма, 3 UI-поверхности.

## Данные

Миграции `66_release_subscription`, `67_release_subscription_hit`.

### `release_subscription`

| Колонка | Тип | Смысл |
|---|---|---|
| `release_subscription_id` | uuid pk | |
| `user_id` | uuid fk | |
| `kind` | text | `movie` / `season` |
| `video_id` | text | IMDB id (`tt…`) — единственный id, который умеют все источники |
| `season` | smallint null | для `season` |
| `title` / `poster_url` | text null | снимок на момент подписки, чтобы письмо и профиль не ходили в TMDB |
| `lang` | text | язык на момент подписки → язык писем |
| `source` | text | `discover_season` / `resource_banner` / `empty_streams` / `empty_filters` — валидируется как в watchlist |
| `enabled` | bool | тумблер в профиле |
| `state` | text | `pending_baseline` / `active` / `completed` / `disabled` |
| `last_checked_at`, `next_check_at`, `last_notified_at` | timestamptz null | планирование опроса |
| `created_at`, `updated_at` | timestamptz | + триггер `update_updated_at` |

Unique: `(user_id, kind, video_id, coalesce(season, -1))`. Индекс на `(enabled, next_check_at)` — рабочая выборка поллера.

Плюс миграция `68_user_settings_lang` — одна колонка `user_settings.lang` для языка писем, см. раздел «Откуда берётся язык».

### `release_subscription_hit`

`(release_subscription_id, infohash)` — pk. Плюс `name`, `size`, `source_name` (аддон/трекер), `season`, `episode`, `first_seen_at`, `notified_at` (null = ждёт письма), `is_baseline`.

Эта таблица и есть требование «обновления без инфохэшей, которые были в предыдущих письмах»: инфохэш попадает в письмо ровно один раз, потому что вставка идёт с `ON CONFLICT DO NOTHING`, а письмо забирает строки с `notified_at IS NULL` и проставляет его после успешной отправки.

**Baseline.** Подписка создаётся в состоянии `pending_baseline`. Первый прогон поллера записывает всё найденное как `is_baseline = true, notified_at = now()` и переводит в `active` — без письма. Иначе подписка на идущий сезон немедленно прислала бы простыню из уже существующих раздач. Для точки входа №3 (результатов нет) baseline пустой, то есть первый же новый инфохэш придёт письмом.

**GDPR.** Обе таблицы user-keyed → обязательный `fillSubscriptions` в `services/data_export/export.go` (правило из CLAUDE.md, `docs/data_export.md`).

## Сервис и API

`services/release_subscription/` — чистая логика: `Subscribe`, `Unsubscribe`, `List`, `CheckLimit`, `Eligible`. Без `gin.Context`.

`handlers/release_subscription/` — два набора маршрутов, оба под `auth.HasAuth`:

```
JSON (Discover / Preact):
  GET    /discover/subscriptions/ids                 → { items: [{kind, video_id, season}] }
  POST   /discover/subscriptions                     → 200 {added} / 402 {code:"limit_exceeded"} / 409 {code:"not_eligible"}
  DELETE /discover/subscriptions/:kind/:video_id     → 200 {removed}   (season в query)

Form (SSR: баннер раздачи + профиль):
  POST /subscription/add
  POST /subscription/delete/:id
  POST /subscription/update            ← listEditor: deleted_ids + enabled_*
  GET  /subscription/unsubscribe/:token ← подписанная ссылка из письма, без логина
```

Eligibility проверяется **на сервере** при POST (`IsAiringSeries` для `season`), даже если клиент уже её посчитал — иначе в таблицу попадёт мусор от прямых запросов.

Лимит free: 3 активных подписки, копия `checkFreeTierLimit` (`Tier.Id == 0`), 402 + тост со ссылкой на апгрейд. Paid — без лимита.

## Поллер

Новая CLI-команда `web-ui subscription poll` (по образцу `notification send`) + CronJob в приватном чарте (одна строка в `$cronJobs`).

Алгоритм одного прогона:

1. Выбрать `enabled AND next_check_at <= now()`, батч ≤ N (флаг, старт 300), сортировка по `next_check_at`.
2. Сгруппировать по пользователю: claims тянутся `claims.Get({Email, PatreonUserID})` один раз на юзера (лимит + кэш lazymap).
3. Построить **урезанный** стрим-пайплайн: аддоны + torznab + dedup + language filter. Без `EnrichStream` (ему нужны link resolver и токен — поллеру не нужна ссылка на плеер) и без `Library`. Новый метод `Builder.BuildPollStreamsService(ctx, u)` — 15 строк, вся начинка переиспользуется.
4. Какие `contentID` спрашивать:
   - `movie` → `tt…`, один запрос;
   - `season` → эпизоды сезона из `episode_metadata`, у которых `air_date <= now()`, отсортированные по убыванию даты, **не больше 3** (флаг). Свежевышедший эпизод и есть источник новых раздач; season-паки подтянутся тем же запросом через `season_filter`.
5. Вставить найденные инфохэши в `release_subscription_hit` (`ON CONFLICT DO NOTHING`).
6. Если `state = pending_baseline` → пометить всё вставленное baseline'ом, перевести в `active`, письма не слать.
7. Иначе, если есть строки с `notified_at IS NULL` **и** `last_notified_at` старше min-интервала (флаг, старт 12 ч) → письмо-обновление; при успехе проставить `notified_at`.
8. Пересчитать `next_check_at`.

**Одно письмо на пачку.** Шаг 7 не шлёт письмо на каждый найденный инфохэш: все накопленные с прошлой отправки строки уходят одним списком. Если за один прогон нашлось четыре новых рипа — это одно письмо с четырьмя строками, а не четыре письма. Min-интервал (12 ч) — именно про это: он собирает пачку, а не глушит уведомления.

**Расписание опроса.** Cron раз в час, реальная частота — в `next_check_at`:

| Ситуация | Интервал |
|---|---|
| paid, есть эпизод с `air_date` в пределах 72 ч | 3 ч |
| paid, обычный режим | 6 ч |
| free | 12 ч |
| ничего не найдено N прогонов подряд | ×2 до потолка 24 ч |

Разная частота для free/paid — прямое следствие цифр fake-door: платящие конвертируются в 10× чаще, а исходящий трафик к чужим индексерам одинаковый. Это дешёвый рычаг, а не продуктовое ограничение (лимит 3 подписки на free остаётся как в ТЗ).

**Порядок величин.** Одна подписка на сезон = ≤3 контент-запроса × (аддоны + индексеры юзера; на free это ≤3+3). Худший случай — 18 исходящих HTTP на прогон. 1000 активных подписок на сезоны при 4 прогонах в сутки ≈ 72k исходящих запросов в сутки, размазанных по чужим хостам, из них к одному индексеру конкретного юзера — 12 в сутки на подписку. Это выдержит и Jackett, и публичный аддон. Если понадобится ужать — сначала лимит эпизодов (3 → 1), потом интервалы.

**Автозавершение — только у сезона.** `season`: когда в сезоне не осталось эпизодов с `air_date > now()` и `IsAiringSeries` вернул false — `state = completed`, письмо «подписка завершена».

`movie` **сама не завершается никогда**: пользователь мог посмотреть на первые рипы и продолжить ждать нужный (качество, дорожка, релизер), поэтому подписка живёт до ручной отписки. Единственная защита от потока — тот же min-интервал: новые рипы копятся и уходят одним письмом раз в 12 ч, а не по письму на рип. Решение владельца от 2026-08-15.

## Письма

Три шаблона в `templates/notification/`:

| Шаблон | Ключ дедупа | Повод |
|---|---|---|
| `subscription-on.html` | `sub-on-<id>` | подписка создана |
| `subscription-off.html` | `sub-off-<id>` | отписка/отключение/автозавершение |
| `subscription-update.html` | `sub-upd-<id>-<YYYYMMDDHH>` | новые раздачи |

Тело `update`: заголовок с названием (+ сезон), список новых раздач — имя, размер, источник, ссылка `{{ .Domain }}/magnet:?xt=urn:btih:<hash>&dn=<name>`, внизу «управлять подписками» и one-click «отписаться».

**Ловушка в дедупе.** `notification.Send` глушит письмо, если строка с тем же `(key, to)` создана менее 24 ч назад. Для `sub-on`/`sub-off` ключ обязан содержать id подписки, иначе вторая подписка за сутки не подтвердится письмом. Для `update` ключ содержит час запуска: интервал между письмами держит поллер (min-interval), а не дедупер — иначе проглоченное письмо унесёт с собой инфохэши, которые уже не повторятся.

**Локализация.** Сейчас шаблоны уведомлений — голый английский HTML без i18n. Добавляем `FuncMap{"t","tp"}` из `i18n.Helper` в `notification.render`, `SendOptions` получает поле `Lang`. `locales/*.json` вшиты в бинарь (`//go:embed`), в cron-контейнере доступны.

### Откуда берётся язык

У аккаунта сегодня языка нет: он живёт в URL-префиксе и в cookie `lang`, а до cron-задачи ни то, ни другое не доезжает. Поэтому — новая колонка `user_settings.lang` (typed column, как `show_adult`; таблица уже есть, миграция на одну колонку).

Пишется в одном месте: gin-middleware после auth сравнивает `i18n.GetLang(c)` со значением в сессии и, если разошлось, делает upsert в `user_settings`. То есть запись происходит на смену языка, а не на каждый запрос — переключатель языка (`?lang=X`) и заход по языковому префиксу оба через это проходят.

Цепочка при отправке письма: `user_settings.lang` → `release_subscription.lang` (снимок на момент подписки — работает, пока юзер ни разу не переключал язык залогиненным) → `en`.

Это же снимает вопрос по остальным письмам: как только у аккаунта есть язык, четыре существующих шаблона (vaulted / expiring / expired / transfer-timeout) переводятся тем же механизмом, отдельная работа не нужна.

**Отписка одним кликом.** JWT на `SESSION_SECRET` (тот же приём, что в `/stremio/resolve`), в payload — id подписки; `GET /subscription/unsubscribe/:token` выключает и показывает страницу подтверждения. Без него письма про подписки — это письма без выхода.

## UI

### 1. Discover, незавершённые сезоны

`EpisodePicker` (`StreamModal.jsx:842`) уже строит карту сезонов из `meta.videos`. Сезон считается незавершённым, если в нём есть эпизод с `released > now` — это те же данные, по которым живёт `CalendarView`. Кнопка-колокольчик рядом с селектором сезона, состояние берётся из `/discover/subscriptions/ids` (prefetch тем же приёмом, что `watchlistIds` в `DiscoverApp`).

### 2. Баннер на странице раздачи

Слот `{{ template "resource/release_subscribe_banner" $ }}` стоит на месте, шаблон и JS от fake-door живы. `prepareReleaseSubscribeBanner` возвращается к работе: series → `SeriesMetadata.VideoID` → `IsAiringSeries` → `dominantSeason` (восстановить из `f8e9f36`). Кнопки Полезно/Не нужно заменяются на Подписаться/Отписаться, всё остальное — вёрстка, ключи `release_sub.*`, dismiss в localStorage — остаётся.

### 3. Пустой результат в модалке стримов

Две ветки в `StreamContent`: `streams.length === 0` (`StreamModal.jsx:586`) и `hasActiveFilters && visibleCount === 0` (`:657`). В обе добавляется CTA.

Две честности, которые надо соблюсти:
- если у юзера **нет источников** (`hasCustomAddons === false` и нет индексеров) — подписка бессмысленна, там уже стоит «настройте аддоны», её и оставляем;
- подписка не хранит фильтры, поэтому в ветке «фильтры ничего не выбрали» текст обещает уведомление о **любых** новых раздачах, а не о раздачах под фильтр. Фильтро-зависимый матчинг — v2, отдельный столбец `match` (jsonb) и проверка в поллере.

### 4. Профиль

Секция `profile/subscriptions.html` между Stremio-настройками и WebDAV: постер, название, сезон, статус, дата последней проверки; тумблер `enabled` и удаление — целиком на `listEditor.js` + `handlers/common.SplitIDs`. Порядок строк не нужен, так что из трёх полей формы используются два.

### Аналитика

Umami, kebab-case как везде: `subscription-created` (property `source`), `subscription-removed`, `subscription-limit-hit`, `subscription-email-click`. Первые цифры к разбору — доля подписок из каждой из трёх точек входа и CTR письма-обновления.

## Спринты

| Спринт | Содержание | Оценка |
|---|---|---|
| 1 ✅ | Миграции 66/67, модели, `services/release_subscription`, оба набора маршрутов, лимит free, секция профиля, `data_export` | 3 дня |
| 2 | `BuildPollStreamsService`, CLI `subscription poll`, планировщик `next_check_at`, 3 шаблона писем, i18n в `notification.render`, миграция 68 + запись `user_settings.lang`, перевод 4 старых писем, unsubscribe-токен, CronJob в чарте | 3.5 дня |
| 3 | Три UI-поверхности, ключи в 11 локалей, Umami-события | 2.5 дня |
| 4 | Тесты (выбор эпизодов, baseline, дедуп хитов, лимиты — по образцу `services/stremio/*_test.go`), этот док в актуальное состояние, дашборд метрик | 1 день |

Итого ≈ 10 рабочих дней. Спринты 1 и 2 можно вести параллельно только после того, как зафиксирована схема — поллер целиком стоит на ней.

## Что уже в коде (спринт 1)

| Слой | Файлы |
|---|---|
| Схема | `migrations/66_create_release_subscription.*`, `migrations/67_create_release_subscription_hit.*` |
| Модели | `models/release_subscription.go`, `models/release_subscription_hit.go` |
| Логика | `services/release_subscription/service.go` (+ тесты) |
| HTTP | `handlers/release_subscription/handler.go` |
| Профиль | `templates/partials/profile/subscriptions.html`, `assets/src/js/app/profile/subscriptions.js`, секция в `templates/views/profile/get.html`, `Subscriptions` в `handlers/profile` |
| GDPR | `fillReleaseSubscriptions` в `services/data_export/export.go` (+ `docs/data_export.md`) |
| Локали | `profile.subscriptions.*`, `discover.subscriptions.*`, `toast.subscription*`, `error.subscription*` во всех 11 файлах |

Детали, которые стоит знать при продолжении:

- **Уникальность контента — выражение, а не constraint.** `UNIQUE (user_id, kind, video_id, coalesce(season, -1))` индексом: у фильма сезона нет, а `NULL != NULL` пустил бы один и тот же фильм дважды.
- **Лимит считает все строки**, включая выключенные и завершённые: строка, которую можно вернуть одним кликом, — это всё ещё работа для поллера.
- **Eligibility спрашивается на записи**, а не только в UI: `IsAiringSeries` для сезона, фильм — всегда можно. Без энричера сезонная подписка отклоняется (консервативная ветка: непроверяемая строка иначе полилась бы вечно).
- **Профильный список переиспользует `listEditor.js`** как есть: строки не `draggable`, поэтому общий drag-биндинг остаётся мёртвым, а из трёх полей формы читаются два (удаления и тумблеры). Порядка у подписок нет.
- Поле `next_check_at` уже пишется (`now()` при создании) — поллер спринта 2 получает готовую очередь.

## Решения владельца (2026-08-15)

1. **movie-подписка не завершается сама** — живёт до ручной отписки; несколько новых рипов уходят одним письмом, см. «Одно письмо на пачку».
2. **Частота free 12 ч / paid 3–6 ч** — принято.
3. **Язык писем** — через новую колонку `user_settings.lang`, снимок `subscription.lang` как запасной вариант; заодно переводим 4 существующих письма.
4. **Подтверждение — на каждую подписку**, без группировки. Ключ дедупа `sub-on-<id>` содержит id именно поэтому: с ключом без id вторая подписка за сутки осталась бы без письма.

Открытых вопросов по объёму работ нет — план готов к реализации.

## Риски

- **Подписка без источников молчит навсегда.** У webtor нет дефолтных аддонов: пайплайн юзера без аддонов и индексеров возвращает только его собственную библиотеку. Поэтому CTA прячется при пустом наборе источников, а в письме-подтверждении для юзера с одним источником стоит об этом сказать.
- **Опрос чужих индексеров.** Приватные трекеры баннят за частоту. Митигация — интервалы выше, `lazymap`-кэш (1 мин) уже есть, плюс джиттер `next_check_at` при записи, чтобы прогон не бил залпом.
- **`air_date` в `episode_metadata` может отсутствовать** (эпизод ещё не вошёл в TMDB). Тогда сезон не отдаёт кандидатов на опрос — фолбэк: если кандидатов нет, опрашивать максимальный известный эпизод сезона + 1.
- **Разъезд с fake-door выводом.** Fake-door сказал: на free это не работает (1.3%). Мы всё равно даём free 3 подписки — сознательное решение владельца; метрика проверки — доля free-подписок, доживших до второго письма. Если она сойдётся к нулю, дешевле всего убрать точку входа для free, а не механику.
