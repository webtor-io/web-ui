# Project: web-ui (github.com/webtor-io/web-ui)

## Documentation

- **Before starting work, check the `docs/` directory** — it contains technical specs, DB schemas, business logic, API specs, and edge cases for major features (e.g., `docs/vault.md`).
- **After completing any task, update the relevant docs** — document new methods, services, DB tables, and changed functionality. Documentation updates are mandatory.

## Build & Toolchain

- **Go** 1.24.x (module: `github.com/webtor-io/web-ui`)
- **Node** 22.x for frontend assets
- **npm** (not Yarn) — `package-lock.json` is present
- Frontend assets: webpack → `assets/dist`, served at `/assets`
- Public static files: `pub/` mounted at `/` and `/pub`

### Key Commands

- `make build` — runs `npm run build` then `go build .`
- `make run` — runs `./web-ui s` (serve)
- `npm start` — hot reload via `air` (Go) + webpack-dev-server (requires `air`: `go install github.com/cosmtrek/air@latest`)
- `go test ./...` — run all Go tests
- `go test ./services/parse_torrent_name -v` — parser tests (golden-file based)
- `go test ./services/parse_torrent_name -run TestParser -update` — update golden files

### Docker

```
docker build .
```
Produces minimal Alpine image. Exposes ports 8080, 8081. Runs `./server serve` with `GIN_MODE=release`.

## Architecture Rules

### Database Operations

- **ALL database operations go in `models/`** — handlers must never contain direct DB queries.
- Model files named after the entity (e.g., `models/embed_domain.go`).
- Model methods accept `*pg.DB` as first parameter.
- Provide Get/List, Create, Update, Delete, Count/Exists methods per entity.

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
| `btn-accent` | Vault, library actions | Homepage, profile, auth forms |
| `btn-ghost` | Secondary actions (demo, delete, logout) | Primary actions |

**Focus color** — context-dependent:
- `focus:border-w-pink` — profile & auth page inputs
- `focus:border-w-cyan` — support form & tools page inputs

**Badge color** — matches section theme:
- Pink (`bg-w-pink/10 text-w-pinkL`) — features, profile
- Purple (`bg-w-purple/10 text-w-purpleL`) — comparison sections
- Cyan (`bg-w-cyan/10 text-w-cyan`) — info, tools, FAQ

**Custom CSS classes** (`assets/src/styles/style.css`): `btn-pink`, `btn-soft`, `toggle-soft`, `gradient-text`, `gradient-stat`, `hero-glow`, `cta-glow`, `upload-dashed`, `navbar-redesign`, `collapse-webtor`, `progress-alert`.

## Configuration (Minimum)

- `WEB_HOST` / `WEB_PORT` (default 8080)
- REST API: `REST_API_SERVICE_HOST`, `REST_API_SERVICE_PORT`, or RapidAPI via `RAPIDAPI_HOST`/`RAPIDAPI_KEY`
- Sessions: `SESSION_SECRET` (optional Redis via `REDIS_*` vars)
- Assets: `ASSETS_PATH` (default `./assets/dist`)
- DB: PostgreSQL via `common-services` flags (`PG_HOST`, etc.) — migrations auto-apply on startup
- Redis: for job queues via `common-services`

### Optional Integrations

- Umami analytics: `USE_UMAMI`, `UMAMI_WEBSITE_ID`, `UMAMI_HOST_URL`
- GeoIP: `USE_GEOIP_API`, `GEOIP_API_SERVICE_HOST/PORT`
- Claims (user tiers): `USE_CLAIMS`, `CLAIMS_PROVIDER_SERVICE_HOST/PORT`
- Stremio addon: `STREMIO_ADDON_USER_AGENT`, `STREMIO_ADDON_PROXY`

## Debugging

- pprof/probe via `common-services` flags (secondary port)
- Test API without RapidAPI: port-forward `rest-api` from K8s or set `REST_API_SERVICE_HOST/PORT`
- Asset path issues: use `--assets-path` or `WEB_ASSETS_HOST` for CDN
- Ad testing: set cookie `test-ads` or query param `test-ads`
