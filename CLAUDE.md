# Project: web-ui (github.com/webtor-io/web-ui)

## Architectural Decisions (web-ui specific)

| Решение | Причина |
|---------|---------|
| Go/Gin + HTML templates, не SPA | SEO, быстрый first paint, работает без JS. Preact только для progressive enhancement |
| Preact, не React/Vue/Angular | Минимальный бандл (~3KB). Пользователи часто на медленном интернете |
| DaisyUI + кастомные токены (w-*) | Единая дизайн-система, быстрая разработка. Свои токены поверх DaisyUI night theme |
| `data-async` progressive enhancement | Формы работают без JS, async-атрибуты добавляют AJAX поведение поверх |
| Webpack 5, не Vite | Стабильность, проверенные плагины, нет причин мигрировать |
| Модели в models/, не в handlers | Чёткое разделение DB-логики. Handlers никогда не делают SQL-запросы напрямую |
| Two-level handler pattern | HTTP-слой отделён от бизнес-логики. Упрощает тестирование и переиспользование |
| 4K стриминг отключён | Транскодирование 4K слишком ресурсозатратно, стриминг больших файлов нестабилен |
| go-i18n + path prefix routing | SEO (каждый язык — отдельный URL), обратная совместимость (EN без префикса), HTTP middleware до Gin routing |
| `withContext` для sub-templates | Lang не протекает в domain structs. Контекст передаётся отдельно через wrapper |
| Translation keys в Go коде | Tool titles, sort labels, button names, job messages — ключи вместо строк, перевод в шаблоне/runtime |
| Adult-content blur — server-side, не client | Server рендерит блюр в poster_resolver на ответе. Защищает share-flow (Telegram/Stremio fetch'ат без auth → получают блюр гарантированно). См. `docs/adult_classification.md` |
| `resource_metadata` table — не JSONB | `is_adult`/`is_sport` — индексируемые булы для hot-path запросов (poster blur, фильтры). Full ptn snapshot в `metadata` JSONB. Switch to JSONB only когда появится много long-tail soft-флагов |
| `user_settings` — typed columns | Same rationale — `show_adult` индексируем для "сколько юзеров opted in". JSONB defer'нут до накопления флагов |
| URL/UI хелперы — НЕ методы на `web.Context` | `web.Context` остаётся raw request-state (CSRF/User/Lang). Feature-specific URL builders принимают `*Context` параметром в helpers (`web.PosterURL(rid, w, isAdult, ctx)`), не методами на shared type |
| Tool-страницы — данные, не партиал на страницу | Тело SEO-лендинга описывается списком секций в `handlers/common/tools.go`, рендерит общий `sections.html`. 19 постраничных партиалов (2537 строк) были одной и той же разметкой с разным префиксом ключей; новая страница = копия чужого файла. Шестой тип секции — сигнал, что нужен свой партиал, а не новый флаг. См. `docs/tool_pages.md` |
| PWA lite — manifest без service worker'а | Install/share/protocol-handler интеграции без offline-кэша: SW с fetch-хендлером ломает hard-cutover деплой (refresh перестаёт гарантировать свежую версию). Push-only SW допустим в будущем. См. `docs/pwa.md` |
| Seeder fast-path warmup — silent skip | Когда seeder pod уже держит head+tail pieces, `warmUp` тихо пропускается без `j.Skip` UI-следа и роутится через cap-modal ветку (как `Cache=true`). Главный win — share-flow: viewer попадает на hot pod сразу после sharer'а. См. `docs/warmup.md` |

## Internationalization (i18n)

- **Full guide**: `docs/i18n.md` — архитектура, паттерны, правила переводов
- **Languages**: EN (default, no prefix), RU (`/ru/`), ES (`/es/`), DE (`/de/`), FR (`/fr/`), PT-BR (`/pt/`), IT (`/it/`), PL (`/pl/`), TR (`/tr/`), NL (`/nl/`), CS (`/cs/`)
- **Locale files**: `locales/{en,ru,es,de,fr,pt,it,pl,tr,nl,cs}.json` — flat key-value JSON
- **PT note**: bundle `pt` содержит PT-BR. `Accept-Language: pt-BR`/`pt-PT` оба сворачиваются в `pt` через `language.Base()`. Разделять на `pt-br`/`pt-pt` — только когда потребуется европейский португальский (нужна правка middleware для переменных префиксов)
- **Template translation**: `{{ t $.Lang "key" }}`, `{{ tp $.Lang "key" "Param" value }}`
- **Links**: всегда `{{ langPath $.Lang "/path" }}`
- **Sub-templates с чужими данными**: `{{ template "name" withContext $ data }}`
- **Job messages**: `i18n.Service` пробрасывается через `Jobs` → скрипты, `s.t("key")`
- **Не переводим**: brand names (Vault, Discover, Stremio), technical terms, legal pages
- **Русский**: "Смотрите" вместо "Стримьте", "Вклад" вместо "Залог"

## Documentation

- **Before starting work, check the `docs/` directory** — it contains technical specs, DB schemas, business logic, API specs, and edge cases for major features (e.g., `docs/vault.md`).
- **After completing any task, update the relevant docs** — document new methods, services, DB tables, and changed functionality. Documentation updates are mandatory.

## Build & Toolchain

- **Go** 1.26 (module: `github.com/webtor-io/web-ui`)
- **Node** 22.x for frontend assets
- **npm** (not Yarn) — `package-lock.json` is present
- Frontend assets: webpack → `assets/dist`, served at `/assets`
- Public static files: `pub/` mounted at `/` and `/pub`

### Key Commands

- `make build` — runs `npm run build` then `go build .`
- `make run` — runs `./web-ui s` (serve)
- `npm start` — webpack-dev-server only; the Go binary is run/debugged from GoLand (air was removed 2026-07)
- `go test ./...` — run all Go tests
- `npm test` — browser-side tests via `node --test` (no test framework installed). Works because `assets/src/js/package.json` declares `{"type": "module"}`; that also puts webpack into strict-ESM mode for the tree, which is why the js rule carries `resolve: { fullySpecified: false }` and the Babel config is a root `babel.config.json` rather than a `.babelrc`. Testable logic lives in plain `.js` modules — `node --test` cannot parse JSX
- `go test ./services/parse_torrent_name -v` — parser tests (golden-file based)
- `go test ./services/parse_torrent_name -run TestParser -update` — update golden files

### Docker

```
docker build .
```
Produces minimal 3-stage Alpine image. Exposes ports 8080, 8081. Entrypoint `./server serve` with `GIN_MODE=release`.

## Architecture Rules

### Database Operations

- **ALL database operations go in `models/`** — handlers must never contain direct DB queries.
- Model files named after the entity (e.g., `models/embed_domain.go`).
- Model methods accept `*pg.DB` as first parameter.
- Provide Get/List, Create, Update, Delete, Count/Exists methods per entity.
- **New user-keyed table → must be added to the GDPR data export.** Whenever you introduce a table with a `user_id` (or any column that ties rows to a single account), you MUST also wire it into `services/data_export/export.go` so it appears in the `/profile/export` JSON. Steps and rationale are in `docs/data_export.md`. Skipping this leaves a real Art. 20 compliance gap — treat it as part of the schema change, not a follow-up.

### Handler Architecture (Two-Level Pattern)

All handlers must follow two-level separation:

- **Level 1 (HTTP layer)**: Extracts params from `gin.Context`, calls Level 2, handles HTTP responses.
- **Level 2 (Business logic)**: Pure functions, no `gin.Context` dependency, returns values and errors.
- **Auth**: Use `auth.HasAuth` middleware via `r.Group().Use(auth.HasAuth)` — don't check auth manually in handlers.
- **Reference**: `handlers/embed_domain/handler.go`, `handlers/vault/handler.go`, `handlers/streaming/backends/handler.go`

### Frontend Development

- **Server-side rendering first** — use Go templates with Gin, minimize client-side JavaScript.
- **No heavy JS frameworks** (React, Vue, Angular).
- Use HTML forms with `method="post"` for mutations.
- Use `data-async` attributes for progressive enhancement.
- Use `data-async-target` and `data-async-push-state="false"` for partial page updates.
- Handlers use `c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))` for form processing.
- Templates in `templates/partials/`, registered via TemplateManager before `tm.Init()`.
- **Good examples**: Stremio (`templates/partials/profile/stremio.html`), WebDAV, Embed domains.

#### Auto-updating Components

- Container needs unique `id` and `data-async-layout` attribute.
- Hidden form with `data-async-target` pointing to container `id` and `data-async-push-state="false"`.
- Use `requestSubmit()` instead of `submit()`.
- Call `document.querySelector("#component-id").reload()` for direct reload.
- **Reference**: `templates/partials/vault/button.html`, `templates/partials/library/button.html`

#### When JavaScript Is OK

- Clipboard functionality
- Progressive enhancement of server-side features
- Interactive features that don't break core functionality without JS
- Analytics (Umami)

## Code Style

### Go

- `go fmt` and `go vet`
- Logging: global `logrus` (`log "github.com/sirupsen/logrus"`), not injected loggers
- Structured logging with `WithField()`, `WithError()` — messages start with lowercase
- Error wrapping: `github.com/pkg/errors` with `errors.Wrap(err, "context")`
- **Log errors only at the top level** (handlers/entry points) — lower levels wrap and return
- External calls: use `context.WithTimeout` and `lazymap` caches
- Interface names: no "Interface" suffix (e.g., `StreamService` not `StreamServiceInterface`)
- Implementation structs: descriptive names (e.g., `HttpStreamService`)

### SQL Migrations

- File naming: `{number}_{description}.{up|down}.sql` (e.g., `19_create_stremio_settings.up.sql`)
- Use `public.` schema prefix, tab indentation, lowercase data types
- Constraint naming: `{table}_pk`, `{table}_{column}_unique`, `{table}_{reference}_fk`
- Include `update_updated_at` trigger for tables with `updated_at`
- Down migrations: `DROP TABLE IF EXISTS table_name;`
- Follow patterns from `18_addon_url.up.sql`

### Frontend

- Tailwind v4, webpack 5, postcss
- Stylelint available (not wired to npm scripts)
- **UIKit reference**: `docs/uikit.html` — open in browser after `npm run build` to see all design tokens and components

#### UIKit & Design System Rules

The project uses a custom design system on top of DaisyUI (night theme). All tokens and components are documented in `docs/uikit.html`. **When adding or changing UI components, consult and update `docs/uikit.html` to keep it in sync.**

**Color tokens** (`tailwind.config.js` → `w-*`): `bg`, `surface`, `card`, `pink`, `pinkL`, `purple`, `purpleL`, `cyan`, `text`, `sub`, `muted`, `line`. Use as `bg-w-{name}`, `text-w-{name}`, `border-w-{name}`.

**Button variants** — each has a designated context, do NOT mix:
| Variant | Use for | Do NOT use for |
|---------|---------|----------------|
| `btn-pink` | Homepage & tools page CTAs | Profile, vault, auth, support form |
| `btn-soft` | Profile, auth & support form actions | Homepage CTAs, vault/library |
| `btn-soft-cyan` | Secondary actions in cyan (info, tools, content) | Primary actions, profile/auth |
| `btn-accent` | Vault, library actions | Homepage, profile, auth forms |
| `btn-ghost border border-w-line` | Outlined ghost (nav, downloads, secondary) | Primary actions |
| `btn-ghost` (no border) | Tertiary actions (demo, delete, logout) | Primary actions |

**Focus color** — context-dependent:
- `focus:border-w-pink` — profile & auth page inputs
- `focus:border-w-cyan` — support form & tools page inputs

**Badge color** — matches section theme:
- Pink (`bg-w-pink/10 text-w-pinkL`) — features, profile
- Purple (`bg-w-purple/10 text-w-purpleL`) — comparison sections
- Cyan (`bg-w-cyan/10 text-w-cyan`) — info, tools, FAQ

**Custom CSS classes** (`assets/src/styles/style.css`): `btn-pink`, `btn-soft`, `btn-soft-cyan`, `toggle-soft`, `gradient-text`, `gradient-stat`, `hero-glow`, `hero-wave` (+ `wave-a/b/c`), `hero-particles` (+ `hero-particles-track`, `hero-particle-field`, depths `particles-far/mid/near`), `cta-glow`, `upload-dashed`, `navbar-redesign`, `collapse-webtor`, `progress-alert`, `promo`, `promo-close`, `promo-compact`, `promo-outline`, `loading-elipsis`, `popin`, `w-panel`, `w-card-frame`, `w-card-title`, `w-card-badge`, `w-card-badge-label`, `w-card-badge-ghost`, `w-card-stars-compact`, `w-card-stars-full`.

**Mobile patterns** — see `docs/uikit.html` section 15. Key rules:
- Hide decorative badges/subtitles on mobile (`hidden sm:inline-flex` / `hidden sm:block`)
- Use `sticky top-[72px]` for tab bars with `bg-w-bg/90 backdrop-blur-lg`
- Use `flex-col sm:flex-row` to stack filters below tabs on mobile
- Use container queries (not media queries) for card-level responsive: `w-card-badge-label`, `w-card-stars-compact/full` switch at 210px card width
- Use `line-clamp-2` instead of `truncate` for torrent names
- Touch targets: `w-card-badge-ghost` enlarges to 2.5rem on `@media (hover: none)`

**Typography** (`tailwind.config.js`):
- `font-sans` — Inter (primary body font)
- `font-logo` — Comfortaa (logo/branding)
- Font CSS embedded as base64 WOFF2 in `assets/src/styles/inter.css` and `comfortaa.css`

## Configuration (Minimum)

- `WEB_HOST` / `WEB_PORT` (default 8080)
- REST API: `REST_API_SERVICE_HOST`, `REST_API_SERVICE_PORT`, or RapidAPI via `RAPIDAPI_HOST`/`RAPIDAPI_KEY`
- Sessions: `SESSION_SECRET` (optional Redis via `REDIS_*` vars)
- Assets: `ASSETS_PATH` (default `./assets/dist`)
- DB: PostgreSQL via `common-services` flags (`PG_HOST`, etc.) — migrations auto-apply on startup
- Redis: for job queues via `common-services`

### Library Access

- **WebDAV + S3** — one filesystem tree (`services/libfs`), two wire formats. Adding an operation lands in both at once. `DISABLE_WEBDAV`; `DISABLE_S3`, `S3_SIGNING_SECRET`, `S3_DOMAIN`. См. `docs/webdav.md`, `docs/s3.md`
- **JSON API** (`/api/v1`, `docs/api.md`) — `/resource`, `/list`, `/export` это **пробросы в rest-api с его же контрактом** (возвращаются его структуры, не копии); `/library`, `/vault`, `/profile` — только здесь. Не заводить параллельную файловую абстракцию: `/fs`-вариант отвергнут, стрим-ссылки даёт `/export`. `DISABLE_API`, `API_DOMAIN` (выделенный хост → `/v1/...`; в проде это `api.webtor.io` — **передан web-ui от torrent-http-proxy 2026-08-08**, thp-клиенты живут на `api.cosmic-crab.buzz`). Swagger UI на `/api/v1/docs/index.html`; спека — `make swagger`, **обязательно** с `--instanceName libraryapi`, иначе коллизия со спекой rest-api и паника на старте процесса. Статус vault-загрузки — `GET /vault/pledges/{id}` (`libapi.NewPledgeStatus`, чистая функция). Per-key rate limit: `API_RATE_LIMIT`/`API_RATE_BURST` (в памяти реплики, 429 + `Retry-After`). На `/export` селекторы `types` (CSV, семантика rest-api) и `output` (ровно один, 404 если нет) взаимоисключающие. Дальнейшие планы — `docs/api_roadmap.md`

### Optional Integrations

- Umami analytics: `USE_UMAMI`, `UMAMI_WEBSITE_ID`, `UMAMI_HOST_URL`
- GeoIP: `USE_GEOIP_API`, `GEOIP_API_SERVICE_HOST/PORT`
- Claims (user tiers): `USE_CLAIMS`, `CLAIMS_PROVIDER_SERVICE_HOST/PORT`
- Stremio addon: `STREMIO_ADDON_USER_AGENT`, `STREMIO_ADDON_PROXY`
- Torznab indexers: `TORZNAB_TIMEOUT`, `TORZNAB_MAX_RESULTS`, `TORZNAB_USER_AGENT`, `TORZNAB_PROXY` (домашние провайдеры режут входящие с дата-центров — индексер, доступный из браузера, может быть недоступен с нод), `TORZNAB_ALLOW_PRIVATE_NETWORK` (последний — только для self-hosted: снимает запрет на приватные адреса, см. `docs/torznab.md`)
- Release subscriptions: подписка на новые раздачи фильма или сезона, письма из cron-джобы `subscription poll`. Интервалы и батчи — `SUBSCRIPTION_*` (см. `docs/release_subscriptions.md`). Поллер гоняет **тот же** пользовательский стрим-пайплайн, что и Discover (`Builder.BuildPollStreamsService`), поэтому аккаунт без аддонов и индексеров подписку не получит — предлагать её там нечего

## Workflow

- **After committing and pushing to git**, always suggest running `/deploy` to deploy the changes.

## Debugging

- pprof/probe via `common-services` flags (secondary port)
- Test API without RapidAPI: port-forward `rest-api` from K8s or set `REST_API_SERVICE_HOST/PORT`
- Asset path issues: use `--assets-path` or `WEB_ASSETS_HOST` for CDN
- Ad testing: set cookie `test-ads` or query param `test-ads`
- **Streaming-error modals (dev-only)** — append `&debug=<error>` to the resource hash to short-circuit `streamContent` and render the error template without any rest-api work. Gated by `gin.Mode() != gin.ReleaseMode` in `handlers/action/handler.go` so it's a no-op under `GIN_MODE=release`. Values:
    - `debug=slow_download` — cap-modal (`IsRateLimited=true`, fake 5/10/15 Mbps, rate=5M). Useful because under grace=ON this branch is otherwise unreachable for free users.
    - `debug=slow_download_bt` — BT-slow variant (`IsRateLimited=false`, 1/10/15 Mbps).
    - `debug=no_peers` — no_peers modal, "dead swarm" variant, with the real tier from claims.
    - `debug=no_peers_slow` / `debug=no_peers_timeout` — the other two variants of the same modal (peers exist but crawl; hard warm-up deadline), with sample counters.
    - `debug=swarm_demo` — plays the warm-up status line for ~16 s (silent-swarm countdown, then seeders/leechers/speed/bytes) and ends in the slow variant. No network involved.
    - `debug=error:<key>` — renders any user-facing error key in the job log (e.g. `debug=error:error.transcode_failed`); the keys live in `services/web/user_error.go`, see `docs/user_errors.md`.
  - **Status badge (dev-only)** — `?debug_status=idle|caching|cached|vaulting|vaulted|unknown|vault_waiting|vault_failed&seeders=4&leechers=9&peers=13&progress=42` on a resource page: `status.js` forwards these to the `/status` SSE endpoint and `handlers/resource/status.go` (`debugStatus`) emits exactly that status instead of consulting the seeder and Vault. Inert under release.
  - Example: `http://localhost:8083/<hash>?file=...#action=stream&debug=no_peers`. Implementation: hash parsed in `assets/src/js/app/resource/get.js`, posted as `debug` form-field, branched in `jobs/scripts/action.go` `streamContent` before grace setup. Cache-key includes the debug value so a real run isn't masked by a cached debug result.
- **Tier welcome letter preview (dev-only)** — `GET /notifications/preview/tier-welcome?tier=silver&lang=ru&stremio=1&vault=1&billing=1&trial=1` renders the welcome mail (subject bar + wrapped body) exactly as `SendTierWelcome` would send it, without journaling or sending. The route is not registered under `GIN_MODE=release` (`handlers/notifications/handler.go` → `notification.Service.PreviewTierWelcome`).
- **Profile Stremio block, pre-token state (dev-only)** — `/profile?preview=stremio-fresh` renders the Stremio section as an account that has not generated its addon URL yet (the state every new user meets). Gated by `gin.Mode()` in `handlers/profile/handler.go`; only the render changes, the real token is untouched.
- **Onboarding checklist preview (dev-only)** — `?onboarding=free` / `?onboarding=paid` renders the activation checklist as a FRESH account of that tier (pristine progress, no DB), bypassing the age and all-done gates — a developer's own account is past both, so a tier switch alone shows nothing. Gated by `gin.Mode() != gin.ReleaseMode` in `services/web/onboarding_middleware.go` (→ `onboarding.Service.Preview`); affects rendering only and also switches the navbar counter on any page. See `docs/onboarding.md`.
