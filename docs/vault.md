# Vault System

## Overview

The Vault system manages users' virtual points (Vault Points, VP) and their pledges to resources (torrents) for long-term storage.

### Core Concepts

- **Vault Points (VP)** — virtual points, **1 VP = 1 GB** of torrent size. Provided by subscription tier.
- **Pledge** — user's investment of VP in a specific resource. One pledge per user per resource.
- **Funding** — resource is funded when `funded_vp >= required_vp`.
- **Vaulting** — moving a funded resource to long-term storage.
- **Freezing** — pledge is locked for `VAULT_PLEDGE_FREEZE_PERIOD` (default: 24h) after creation. Freeze runs its full duration regardless of vault status.
- **Expiration** — resource marked expired when `funded_vp < required_vp`. Abandoned resources (no pledges) deleted after 1 day; resources with unfunded pledges deleted after 7 days.
- **Transfer Timeout** — if vaulting fails within `VAULT_RESOURCE_TRANSFER_TIMEOUT_PERIOD` (default: 7 days), VP is returned.

### Resource Lifecycle

1. **Created** → `funded=false`, `vaulted=false`, `expired=false`
2. **Funded** → `funded_vp >= required_vp`, `funded=true`
3. **Vaulted** → `vaulted=true`
4. **Expired** (optional) → `funded_vp < required_vp`, deleted after 1 day (no pledges) or 7 days (with unfunded pledges)

## Database Schema

All tables in `vault` schema.

### vault.user_vp

| Column | Type | Description |
|--------|------|-------------|
| user_id | uuid PK, FK→public.user | User identifier |
| total | numeric, nullable | VP balance. `NULL` = unlimited |
| created_at, updated_at | timestamptz | Timestamps |

- `total = NULL` means unlimited balance (premium/unlimited tier)
- Balance synced with claims system on every access via `UpdateUserVP`

### vault.pledge

| Column | Type | Description |
|--------|------|-------------|
| pledge_id | uuid PK | Auto-generated |
| resource_id | text | Resource identifier |
| user_id | uuid FK→public.user | User identifier |
| amount | numeric | Pledged VP amount |
| funded | bool, default true | Active status |
| frozen_at | timestamptz, default now() | Freeze start time |
| created_at, updated_at | timestamptz | Timestamps |

- Unique constraint on `(resource_id, user_id)`
- Freeze determined dynamically: `frozen_at + freeze_period > now()`

### vault.resource

| Column | Type | Description |
|--------|------|-------------|
| resource_id | text PK | Resource identifier |
| required_vp | numeric | Required VP to vault |
| funded_vp | numeric | Current funded VP |
| funded | bool | `funded_vp >= required_vp` |
| vaulted | bool | In long-term storage |
| funded_at, vaulted_at | timestamptz | State transition times |
| expired | bool | Underfunded after being funded |
| expired_at | timestamptz | Expiration time |
| name | text | Torrent name |
| created_at, updated_at | timestamptz | Timestamps |

### vault.tx_log

| Column | Type | Description |
|--------|------|-------------|
| tx_log_id | uuid PK | Auto-generated |
| user_id | uuid FK→public.user | User identifier |
| resource_id | text, nullable | NULL for tier changes |
| balance | numeric | Change amount (non-zero) |
| op_type | smallint | Operation type |
| created_at, updated_at | timestamptz | Timestamps |

**Operation types:**

| op_type | Constant | Description | balance sign |
|---------|----------|-------------|--------------|
| 1 | OpTypeChangeTier | Tier change | +/- |
| 2 | OpTypeFund | Pledge creation | always - |
| 3 | OpTypeClaim | Pledge removal | always + |

Logging rules: no tx_log for unlimited (NULL) or free tier (0) transitions.

### public.notification

| Column | Type | Description |
|--------|------|-------------|
| notification_id | uuid PK | Unique identifier |
| key | text | Dedup key (e.g. `vaulted-{resource_id}`) |
| title | text | Email subject |
| template | text | Template name |
| body | text | Rendered HTML |
| to | text | Recipient email |
| created_at, updated_at | timestamptz | Timestamps |

At most one notification per key per recipient every 24 hours.

## Data Models (Go)

Located in `models/vault/`. All methods accept `ctx context.Context` and `db *pg.DB` as first parameters.

### UserVP — `models/vault/user_vp.go`

Methods: `GetUserVP`, `CreateUserVP`, `UpdateUserVP`

### Pledge — `models/vault/pledge.go`

Methods: `GetPledge`, `GetUserPledges`, `GetUserPledgesWithResources`, `GetResourcePledges`, `GetUserResourcePledge`, `GetFundedResourcePledges`, `GetUserPledgesOrderedByCreation`, `CreatePledge`, `UpdatePledgeFunded`, `DeletePledge`, `SumFundedPledgesForResource`

### Resource — `models/vault/resource.go`

Methods: `GetResource`, `GetFundedResources`, `GetVaultedResources`, `GetExpiredResources`, `CreateResource`, `UpdateResourceFundedVP`, `AdjustResourceFundedVP`, `MarkResourceFunded`, `MarkResourceVaulted`, `UpdateResourceVaulted`, `MarkResourceExpired`, `MarkResourceExpiredAndUnfunded`, `MarkResourceUnexpiredAndFunded`, `DeleteResource`

### TxLog — `models/vault/tx_log.go`

Methods: `GetTxLog`, `GetUserTxLogs`, `GetUserTxLogsByType`, `GetResourceTxLogs`, `CreateTxLog`, `CreateChangeTierLog`, `CreateFundLog`, `CreateClaimLog`, `GetUserBalanceSum`

## Vault Service — `services/vault/vault.go`

### Configuration

| Flag / Env | Default | Description |
|------------|---------|-------------|
| `VAULT_SERVICE_HOST` | — | Vault API host (required) |
| `VAULT_SERVICE_PORT` | 80 | Vault API port |
| `VAULT_SECURE` | false | Use HTTPS |
| `VAULT_PLEDGE_FREEZE_PERIOD` | 24h | Pledge freeze period. `0`: no freeze, and the UI says nothing about one (no confirm line, no Frozen row in the status guide, no help-page step) |
| `VAULT_RESOURCE_EXPIRE_PERIOD` | 7 days | Deletion delay for expired resources with unfunded pledges |
| `VAULT_RESOURCE_ABANDONED_EXPIRE_PERIOD` | 1 day | Deletion delay for expired resources with no pledges |
| `VAULT_RESOURCE_TRANSFER_TIMEOUT_PERIOD` | 7 days | Transfer timeout |

### Constructor

```go
func New(c *cli.Context, vaultApi *Api, cl *claims.Claims, client *http.Client, pg *cs.PG, restApi *api.Api) *Vault
```

Returns `nil` if `vaultApi` is `nil`.

### Key Methods

#### UpdateUserVP

Syncs user balance with claims system. Uses `SELECT FOR UPDATE` in transaction.

- Gets VP from claims (`Claims.Vault.Points`), `nil` = unlimited
- Creates or updates `user_vp` record, logs difference in `tx_log`
- Calls `recalculatePledgeFunding` on balance change
- Special cases: no tx_log for NULL→NULL, 0→0, *→0, 0→NULL transitions

#### UpdateUserVPIfExists

Same as `UpdateUserVP` but only if user already has a `user_vp` record. Returns `nil, nil` if not found. Used in automated event processing.

Only two things call it: the `user.updated` event and a visit to `/vault`. The per-request claims middleware syncs `user.tier` but **not** the balance, so a membership that ends without an event keeps its VP until the reaper's resync (below).

#### GetUserStats

Returns `UserStats`:

```go
type UserStats struct {
    Total         *float64 // nil = unlimited
    Frozen        float64  // VP in frozen+funded pledges
    Funded        float64  // VP in all funded pledges (>= 0)
    Available     *float64 // Total - Funded, nil if unlimited (>= 0)
    Claimable     float64  // Funded but not frozen
    VaultedCount  int      // resources with vaulted=true
    LoadingCount  int      // funded but not yet vaulted (transfer in progress)
    ExpiringCount int      // resources with expired=true
}
```

Always syncs balance first via `UpdateUserVP`. Pledges are loaded with their resources via `GetUserPledgesWithResources` so content counters can be derived in the same pass as VP totals.

#### CreatePledge

Creates pledge in transaction with `SELECT FOR UPDATE`:
1. Checks available VP (skip for unlimited)
2. Creates pledge (`funded=true`, `frozen_at=now()`)
3. Creates tx_log entry (`OpTypeFund`, negative balance)
4. Updates `resource.funded_vp`
5. If resource becomes funded → calls `putResourceToVaultAPI`, marks vaulted if already completed

#### RemovePledge

Removes pledge in transaction:
1. Deletes pledge, creates tx_log (`OpTypeClaim`, positive balance)
2. Updates `resource.funded_vp` (min 0)
3. If underfunded → marks resource expired and unfunded

#### GetOrCreateResource

Idempotent. Returns existing resource or creates new one:
- Calculates `required_vp` from torrent size via REST API (`size / 1GB`)
- Extracts torrent name from API response

#### IsPledgeFrozen, FrozenFor

Dynamic check: `FrozenFor(pledge)` is what is left of the freeze, `frozen_at + freeze_period - now()`, 0 once it is over, regardless of vault status; `IsPledgeFrozen` is `FrozenFor > 0`. One computation, so a page that says "frozen" always has a time left to say (`services/vault` `TestFrozenForIsTheFreezeLeft`). `GetUserStats` puts both on each `EnrichedPledge` (`IsFrozen`, `FrozenFor`).

#### recalculatePledgeFunding (internal)

Called when user's total changes. Processes pledges in creation order (oldest first):
- Accumulates amounts until exceeding `total`
- Funds pledges within budget, defunds those beyond it

#### putResourceToVaultAPI (internal)

Called when resource transitions to funded. Checks Vault API status:
- If `StatusCompleted` → returns `true` (caller marks vaulted)
- If not found → calls `PutResource` to queue, returns `false`
- If exists but not completed → returns `false`

## Notification System

### Triggers

| Trigger | Key | Template | When |
|---------|-----|----------|------|
| Resource Vaulted | `vaulted-{resource_id}` | `vaulted.html` | Event `resource.vaulted` |
| Expiring Resources | `expiring-{days}` | `expiring.html` | Periodic CLI command, <7/3/1 days |
| Transfer Timeout | `transfer-timeout-{resource_id}` | `transfer-timeout.html` | `vault reap` command |
| Resource Expired | `expired-{resource_id}` | `expired.html` | `vault reap` command |

### CLI Commands

**Send expiring notifications** (schedule daily via cron):
```bash
./web-ui notification send
```

**Reap expired resources** (schedule daily via cron):
```bash
./web-ui vault reap   # alias: ./web-ui v r
```

Selects resources where:
- `expired_at < now - VAULT_RESOURCE_EXPIRE_PERIOD` AND has pledges (unfunded), or
- `expired_at < now - VAULT_RESOURCE_ABANDONED_EXPIRE_PERIOD` AND has no pledges (abandoned), or
- `funded_at < now - VAULT_RESOURCE_TRANSFER_TIMEOUT_PERIOD AND vaulted = false`

For each: removes pledges (returns VP), sends notifications, deletes resource. Partial failures logged and skipped.

Before that, each run resyncs balances — see "Balance resync (reaper)".

## HTTP Endpoints

Handler: `handlers/vault/handler.go` (registered only if vault service is not nil).

Files: `handler.go`, `index.go`, `add.go`, `remove.go`.

Auth model: the route is registered without group middleware so the GET handler can do its own auth check; mutating `POST /vault/add` and `POST /vault/remove` are gated by per-route `auth.HasAuth`. Anonymous GET requests redirect to `/login?from=vault&return-url=/vault` (lang-prefixed via `i18n.LangPath` so `/ru/vault` round-trips correctly). The login page renders a contextual info card built from `vault.signInCard.intro` + `vault.signInCard.feature1..4` keys — the same keys are also rendered on the dedicated `/instructions/vault` page, single source of truth. The `signInCard` namespace is uniform across vault/library/discover; the card descriptor (which keys to render for a given `from`) lives in `handlers/auth/handler.go::loginCardFor` so the template stays declarative.

### POST /vault/add

Creates a new pledge. Auth required.

- Form: `resource_id` (required)
- Header: `X-Return-Url` for redirect
- Calls `GetOrCreateResource` → `CreatePledge`
- Redirects with `status=success` or `err=<message>`

### POST /vault/remove

Removes pledges. Auth required (middleware).

- Form: `resource_id` (the resource page's remove modal) and/or `resource_ids`, comma-joined (the dashboard's "remove selected"). One field rather than a box per field: `lib/async.js` posts multipart, Go's multipart reader stops at 1000 parts and gin swallows that error, so a part per box arrived cut to nothing or to a tail. `web.FormBulkIDs` reads both, drops empty and repeated values (a repeat would fail its second removal as "pledge not found") and refuses more than `web.MaxBulkIDs` (1000; the page's "Select all" stops there)
- Per id (`deletePledge`): checks freeze status, error if frozen; calls `RemovePledge` — one transaction per pledge, as before
- One failure does not stop the rest (`deletePledges`): the ones before it are already committed, and the user gets as many as can go rather than a selection cut at an arbitrary row. The first failure is the one reported, wrapped as `failed to remove N of M pledges: …` — `ClassifyError` matches by substring, so `frozen` still reads as `error.pledge_frozen`. The dashboard says it: `GET /vault` reads `status=error&err=` into `ErrKey` and draws it above the table (the resource page's modal reads its own, `from=/vault/remove`)
- Redirects with `status=success` or `status=error&err=<message>`

### GET /vault

"My Vault" dashboard. Anonymous visitors are redirected to `/login?from=vault&return-url=/vault` (handler-level check in `handlers/vault/index.go`); authed users see `templates/views/vault/index.html`.

Reachable from the top nav via the dedicated Vault button (`templates/partials/nav.html`, layered-diamond icon matching the canonical Vault icon used in profile and the Save-to-Vault button, visible to everyone — `aria-label="Vault"`). The library button next to it shortened from "My Library" → "Library" (key `nav.library`) to make room.

Dashboard shows three blocks (the third with its "remove selected"):
1. **Content stats** (primary): Saved (vaulted), Loading (funded, transfer in progress — hidden when 0), Expiring (lost backing). Numbers tinted purple/cyan/pink to mirror the badge palette.
2. **Vault Points stats** (secondary, compact): Total / Available / Funded / Frozen.
3. **Active torrents table** with per-pledge status badges. When the table is empty, what gets rendered depends on tier:
   - **Free tier** (`Claims.Context.Tier.Id == 0`, mirrors `services/claims.IsPaid`): inline upsell card — vault icon + `vault.dashboard.upsellTitle`/`upsellSub` on the left, `btn-soft` CTA "Get a plan" linking to `/donate` (`data-umami-event="donate-vault"`, `data-umami-event-tier="free"`). Replaces the empty-state hint because a free user has zero VP and can't act on it.
   - **Paid tier**: empty-state illustration with hint pointing back to the "Save to Vault" CTA on resource pages.
   - **Remove selected** (`templates/partials/bulk_select.html`, `assets/src/js/app/bulk_select.js` — the same component as the library's, `docs/uikit.html` §23). "Select" beside the table's title puts the table in select mode: each row's first cell trades its icon for a checkbox, the title row sticks under the nav with "Select all / Remove (N) / Cancel", and Remove asks `vault.bulkRemoveConfirm` (points come back; a torrent left underfunded may be removed) before posting the checked ids, comma-joined as `resource_ids`, to `POST /vault/remove`. A **frozen** pledge's box is rendered disabled with `vault.pledges.frozenDesc` as its title — "Select all" skips it, and the server would refuse it anyway. Offered only while at least one pledge is out of its freeze (`$removable` in the view): otherwise select mode would have nothing to select. JS-only: without it the toggle stays hidden and single removal stays in the resource page's modal. The selection survives the table's own reload when its status tokens are renewed (the ids are put back on the rows still there).

Data structures:

```go
type PledgeDisplay struct {
    PledgeID     string
    ResourceID   string
    Resource     *vaultModels.Resource
    Amount       float64
    IsFrozen     bool          // computed via IsPledgeFrozen
    FrozenFor    time.Duration // what is left of the freeze, said under the amount
    Funded       bool          // from DB
    CreatedAt    string        // formatted
    ExpiresIn    time.Duration // for unfunded pledges with expired resources
    ShowProgress bool          // funded but not yet vaulted — wires up live SSE row
}

type PledgeListData struct {
    Pledges               []PledgeDisplay
    Stats                 *vault.UserStats
    FreezePeriod          time.Duration
    ExpirePeriod          time.Duration
    TransferTimeoutPeriod time.Duration
    IsFree                bool // tier-id 0 → render upsell instead of empty-state hint
}
```

Per-pledge states in the table: `Frozen` (the VP cell, cyan snowflake — not a pill — with the time left under the amount), `Expiring` (pink pill, clock, the time to removal under it), `Saved` (Vault's purple pill with its layers). These describe **VP claimability**, not the resource's content state — the dashboard subtitle calls this out for users.

**Frozen row** (2026-10-26). The amount keeps its snowflake (`role="img"` added to its `aria-label` `vault.pledges.frozen`: without the role some screen readers skip the label; not checked with one), and under it, as visible text — a `title` is all a phone never shows — `vault.pledges.frozenLeft` ("13 h 20 min left", `text-[11px] text-w-sub`, its own line like Expiring's time under its pill). The VP column is a fixed `w-24`, its cell `px-1`: daisyUI's td padding left 64 of the 96px to the text, which wrapped "13 h 20 min / left" even at 1280px and could not hold de's or pl's duration whole (77 and 82px at 11px Inter). Measured in a 360px iframe viewport (a real one: `sm:` off) with the built CSS, every locale's line fits without overflowing and its duration never splits: one line in en, nl, ru and tr, two in cs, de, es, fr, it, pl and pt ("noch / 13 Std. 20 Min."); the columns keep their widths (32/14/96/144px: the name column's 14px is the squeeze described under Width, not this line), no horizontal page scroll; at 1280px en is one line. The status guide's Frozen row (`vault.pledges.frozenDesc`, also the disabled box's title in select mode) states the configured period: "A new pledge is frozen for 24 h and can't be removed until then. The time left is shown under its points." — absent when no freeze is configured. `handlers/vault` `TestVaultFrozenRowSaysTimeLeft`.

**Durations** — the template func `duration $.Lang d` (`web.Helper.Duration`), here and on the help page: the two largest units in the page's language, from the download ETA's keys (`offer.eta.*`: abbreviated, so no plural forms and short enough for this column), one unbreakable token: every space in it is a no-break one, the one between its parts too — the strings keep a plain space there for the download ETA, and a phone broke "13 h / 20 min", the number a frozen pledge's line is there to say, in every modal. Rounded up to the minute (to the hour from 48 h), rounding before splitting ("1 h", never "60 min"): it counts a wait, and a rounded-down "13 h" of 13 h 20 min sends the user back into a refusal. Under a minute is "1 min". Days only from 48 h, so the 24 h freeze reads "24 h" and the 7-day expiry "6 d 23 h". It replaced `durafmt`, which is English only: the Russian help page said "замораживаются на 1 day", the dashboard's Expiring time "5 hours" in every language (`services/web` `TestDurationRoundsUpInTheViewersLanguage`). The Expiring time rounds up too — up to a minute (an hour beyond 48 h) more than is left before removal. A duration can end in a dot (de "Std.", pl "godz." and "d.", ru "дн."), so no sentence ends right on its placeholder: four ru and pl help-page sentences read "7 дн.." and "7 d.." (`handlers/instructions` `TestVaultHelpNamesTheRealButton` fails on a double stop in any locale; `TestVaultFreezeLinesInEveryLocale` on the modal's lines).

**Every status pill on the page is the transfer status's badge** (2026-09-25): the one element `templates/partials/status/badge.html` renders (`status/badge`) — the resource page's badge while nothing moves — with its classes, tones, icons and size, and the one renderer `assets/src/js/lib/statusBadge.js`. The pledge states are that element made in the template (`statusBadge "vault" "vault" (t … "vault.pledges.saved")`, `statusBadge "pink" "clock" …` — `handlers/resource` `Helper.StatusBadge`, no id: a page carries many), the status guide's two pills too (each the `<dt>` of its row in the guide's `<dl>`, the VP snowflake the first); the badge's icons are on the page once (`status/badge_sprite`, outside the async-reloaded table). `pink` is the Vault page's own tone (style.css `.tx-badge[data-tone="pink"]`), which statusview never sends. Before, `vault/progress.js` kept its own `BADGE_CONFIG` copied from the old badge, which had drifted: no `missing_idle` / `vault_missing`, the old pause glyph, green "Saved", no cyan "checking". Tests: `handlers/vault` `TestVaultRowsDrawTheStatusBadge`; `lib/statusBadge.test.js` (the resource page's badge and every pill here have one shape); `lib/vaultProgress.test.js` against a generated page with two live rows (each stream draws into its own row).

### Live progress rows

For pledges with `ShowProgress = true` (funded, not vaulted, not expired) the row becomes a live progress display:

- **Lightning icon (leftmost cell)** — single `text-w-pinkL` SVG with the `vault-pulse` class (gentle opacity pulse `1 → 0.45 → 1` on a 1.8s ease-in-out infinite loop, defined in `assets/src/styles/style.css`). Pure heartbeat — no fill / glow / motion paths. Still under `prefers-reduced-motion: reduce` (2026-10-03). Vaulted rows render the same SVG with `text-w-pinkL` but no `vault-pulse` class. Frozen / Expiring / unfunded rows render no icon.
- **Row-wide background fill (`<tr>`)** — JS sets `style.backgroundImage = "linear-gradient(to right, <tint> <pct>%, transparent <pct>%)"` on each SSE message. Tints (low alpha, the bar is a wash not a hard fill): `caching` `rgba(0,206,201,0.10)`, `cached`/`idle` `rgba(0,206,201,0.06)`, `vaulting` `rgba(108,92,231,0.12)`, `vaulted` `rgba(34,197,94,0.08)`. SSR ships the row with no `style=` attribute — the row is plain until the first SSE message lands, so there's no SSR/SSE flash.
- **Status badge** — the status badge element (above), rendered by the server as statusview's `checking` (cyan dots, "Checking activity…" — what statusview says of a torrent nothing has been asked about yet, `Torrent.Pending`) until the first message, which can take seconds (the stream waits for the seeder's first answer). A real state at the size of every other badge (24px tall), not an empty pill of dots that grew and re-centred when the stream filled it (`handlers/vault` `TestVaultRowsDrawTheStatusBadge` holds it to `statusview.Build`'s). Every message of the row's stream carries `badge`: `statusview.View.Badge` of the same status, built by the same `present` with **no viewer** (the dashboard draws none, and its stream never follows one) and the stream's own swarm hold, so a transfer from peers without the whole file does not blink "waiting for missing pieces" between its pieces (`handlers/resource` `TestPresent_DashboardBadgeHoldsThroughAGap`). Every state is the resource page's: "Vaulting 64% (9 seeders)", `vault_missing` "Waiting for missing pieces · 58%" once the seeder knows the peers' pieces, `vault_waiting`, `vault_failed`, and — should the Vault row be unreadable — the caching ones with `missing_idle`. `lib/statusBadge.js` writes tone, icon, pulse and words into the same nodes (`lib/vaultProgress.test.js`: node identity across every state, each ending where a fresh server render of its badge would). On `state == "vaulted"` the row takes the page's word, "Saved" (`data-vault-saved-label`), for the server's "Vaulted": it ends exactly as a finished pledge's pill is drawn, and the SSE source closes. A message without `badge` — a server from before it, during a rolling deploy or after a rollback — leaves the badge as it is (the fill and percent still move; no badge is made up from `state`, which would be a combination statusview never sends), and `vaulted` still ends as "Saved": the stream closes on it, and a row left on its first badge would stay so until a reload (`lib/vaultProgress.test.js`).
- **Width** — in this table the badge's words wrap (2026-10-03; `#vault-pledges .tx-badge .badge-text`: `white-space: normal`, hyphenated by the page's `lang`, a word wider than the column broken anywhere). They used to end in an ellipsis with the whole text in a `title` (`partials/status/badge.html`, `lib/statusBadge.js` — still there, and still how the resource page's badge works), which a touch screen never shows: on a phone the column leaves the words 68px, and 12 of 14 states were cut ("Ждём сидо…", "Сохраняет…", "Vaulting 64% (9 seeders)" 41% of it, "Übertragung fehlgeschlagen…" 20%). Measured 2026-10-03 in headless Chromium with the built CSS at 360 and 390px: no state cut or out of its cell; most take two to five lines, de `vault_failed` seven (`Übertragung fehlgeschlagen, neuer Versuch 12% (3 Seeder)`); one line from `sm` for the common labels. The column is `w-36 sm:w-60 md:w-72`. On a phone the name column is squeezed to nothing by the VP and status columns (29px at 375, 68px at 414) — true before this change too (`w-24` + `w-36` of ~300px); giving the badge more of the row (a narrower VP column on phones, the swarm `.tx-bx` hidden there) is the owner's call.
- **Stream contract** — `GET /:resource_id/status` (the resource page's route: `/<hash>/status`, `/ru/<hash>/status` — prefixed but for English, `langPath`; there is no `/resources/…` path) without `session=1`: `state`, `progress`, `badge` (`{tone, icon, pulse?, label, extra?}`, localized), no `view`; ends after `vaulted`. A message carries only what the page and the dashboard read — `state`, `progress`, `pieces`, `active`, `missing`, `pieces_label`, `view`, `badge`, `final` (`TorrentStatus`; the rest is what the view is built from). The old fields (`label`, `swarm`, `seeders`, `leechers`, `peers`, `rate`, `rate_label`, `paused`/`paused_hint`, `no_seeders`/`no_seeders_hint`, `checking`, `pieces_done`/`pieces_total`) were dropped on 2026-10-03: nothing read them since bf27b9a8 (2026-09-26), and the smoothed `rate` changed every second, so the dedup let through messages that drew nothing new — on a download moving at a piece a second, 35 messages in 26 s instead of 16, 102 KB instead of 44 KB on the page's stream and 27 KB instead of 9.5 KB on a dashboard row's (`TestStatusStream_EveryMessageSaysSomethingNew`). The JSON the loop deduplicates on is the one written, not encoded again. The piece bar's data (`pieces`, `active`, `missing`, `pieces_label`) goes out only where the view draws a bar (`view.bar.mode == "pieces"`, statusview's `bar`: one policy, no second list of states in the handler), so never on a dashboard row's stream, which draws no bar — it was three quarters of a row's bytes (the same 26 s: 9.5 KB → 2.2 KB). The page's stream (`session=1`) has the badge in `view.badge` only, never `badge` as well. `handlers/resource` `TestStatusStream_DashboardCarriesTheBadge`, `TestStatusStream_DashboardBadgeIsThePagesBadge` (the dashboard's badge equals the page stream's `view.badge` for the same status with no viewer, and the page's stream has no `badge`). Cost of building the badge on this path, measured (Apple M3 Pro, `present` + `json.Marshal`, vaulting 64%): 8.5 µs, 9.6 KB, 125 allocs a message, against 2.3 µs / 2.6 KB / 37 before; about two a second per live row (the 1 s tick and the seeder's 1 s stats), no I/O (the offers are an atomic catalog). 100 live rows fleet-wide ≈ 1.2 ms CPU/s. Identical input for 60 ticks sends nothing new in any of the 14 states.
- **Wiring** — `assets/src/js/app/vault/progress.js`. One `EventSource` per active row pointing at the existing `/:resource_id/status` endpoint (`langPath`, `handlers/resource/status.go`); no new backend endpoint. Inactive rows stay static and don't open SSE. On `state == "vaulted"` the JS removes the `vault-pulse` class so the icon settles to fully opaque.
- **Dev preview** — the page's `debug_status=…` and the rest of the transfer status's debug params (`lib/statusDebug.js`, the same list the resource page forwards) ride along to every live row's stream, so any state can be looked at on `/vault`: `/vault?debug_status=vaulting&progress=58&peers=12&seeders=0&availability=0.73&debug_missing=holes&wanted_missing=5` (`vault_missing`), `…debug_status=vault_failed&progress=37&seeders=3`, `…debug_status=vaulted` (the row turns into "Saved"). Inert under release: the server's `gin.Mode()` check in `debugStatus` is the only gate (`lib/statusDebug.js` forwards from any URL), held by `handlers/resource` `TestStatusStream_DebugPreviewIsInertInRelease`.

### GET /vault/pledge → 301

Backwards-compat redirect to `/vault`. The page was renamed from "Pledges" to "My Vault" to ditch the financial connotation of "pledge/вклад" and align with the brand-driven CTA "Save to Vault".

## Future work

- **Clickable stat-cards as filters**: tapping "Expiring: 2" on the dashboard could filter the pledges table to that subset. The stat blocks already carry IDs that would make this cheap.
- **Auto-refresh for the Loading metric**: while transfers are in flight, polling the dashboard via `data-async-layout` would let users watch the count drop without a manual refresh. The per-row progress already covers this for individual rows; the aggregate `Loading` stat still requires a manual refresh.

## UI Components

### Vault Button — `templates/partials/vault/button.html`

Shown in the resource page's header for authenticated users when vault is available.
- "Save to Vault" (`vault.keepAvailable`) → opens the pledge add modal at its confirm step
- "Saved to Vault" (`vault.supporting`, the viewer's own funded pledge) → opens the same modal at its saved view, where Remove Pledge lives
- Uses `data-async-target` and `data-async-push-state="false"`

The help page (`templates/views/instructions/vault.html`) names the button by `vault.keepAvailable`, put into `instructions.vault.howStep1` as `{{.Action}}` at render time — the step had its own copy of the label, which stayed "Keep This Torrent Available" long after the button was renamed (`handlers/instructions` `TestVaultHelpNamesTheRealButton`).

### Pledge Add Modal — `templates/partials/vault/pledge-modal/add.html`, `add/success.html`

States: sufficient VP → confirm form; insufficient VP → upgrade link; success (just pledged) and saved (`Funded`: the viewer's own pledge is funded) — both `add/success.html`, vaulted or not; error.

**The freeze, before the click** (2026-10-26). A paying user pledged 103.50 VP of 250 to a 111 GB pack 2.7 s after opening this modal, then met the frozen dialog seven times and asked support how long the freeze is. Wherever the Store button is, the last line before it (below "What is Vault?") says what the click commits to: `vault.freezeNotice` "Saving freezes 103.50 VP for 24 h. After that you can remove the pledge at any time, and the points return to your balance." — the amount is `Required` as the table prints it, the period `VaultPledgeAddForm.FreezePeriod` (the configured one, never a hardcoded 24 h). Unlimited points get `vault.freezeNoticeShort` (no amount, no balance to return to: the pledge is still frozen). Not enough points: no button, no line. No freeze configured: no line. Not an alert and not in the button's label (a German label with the amount would not fit beside Cancel): it is a condition of the pledge.

**A frozen pledge offers no Remove.** The saved view — right after pledging and whenever the header button reads "Saved to Vault" — showed Remove Pledge, certain to be refused for the first 24 h. While the viewer's own pledge is frozen (`VaultPledgeAddForm.FrozenFor > 0`, from the pledge `prepareVaultPledgeAddForm` already loads for a funded resource) the view says when it will work instead, `vault.frozenFor`: "This pledge is frozen for another 13 h 20 min. Every pledge stays frozen for 24 h after it is made. After that you can remove it here." — and "Got it!" is its only action. Not frozen: Remove Pledge as before. No live countdown: reopening recomputes, and a freeze only ever ends, so a Remove that is shown never becomes wrong. No amount in this line: an unlimited or a no-longer-funded pledge has none to return. Just after the click the time left rounds up to the whole period (the first minute at 24 h, the first hour from 48 h), and the line read "frozen for another 24 h. Every pledge stays frozen for 24 h" just after the confirm step had said 24 h: while the two read the same, the view says the period once, `vault.freezeNoticeShort` — "Once saved, the pledge is frozen for 24 h. After that you can remove it at any time." (`TestVaultJustFrozenPledgeSaysThePeriodOnce`).

The lines are one partial, `vault/pledge-modal/freeze-note` (`freeze-note.html`, called with the sentence): the dashboard's snowflake, `aria-hidden` (the words say frozen), `text-sm text-w-sub`. Which sentence a frozen pledge gets is `vault/pledge-modal/frozen-for` beside it, called with the modal's context by the saved view and the remove dialog alike.

### Pledge Remove Modal — `templates/partials/vault/pledge-modal/remove.html`

States: frozen; confirmation; success; error. Frozen — still reachable by its URL (`?pledge-remove-form=true`) and from a page an older deploy rendered: the same `vault.frozenFor` line with the time left (`VaultPledgeRemoveForm.FrozenFor`, `FreezePeriod`) in place of "come back later when the freeze period expires", which gave no time (`vault.frozen`, deleted).

`handlers/resource` `TestVaultConfirmSaysTheFreezeBeforeTheClick`, `TestVaultFrozenPledgeSaysWhenRemoveWorks`, `TestVaultJustFrozenPledgeSaysThePeriodOnce`, `TestVaultFreezeLinesInEveryLocale` (all 11 locales fill both lines' placeholders with their own durations, with no double stop). Measured from these templates with the built CSS in a 360px iframe viewport, all 11 locales: a line takes three to six lines (pl's frozen line six), nothing overflows, no duration splits across lines; screenshots at 360 and 1280px for en, de, ru, pl.

### Resource Handler Integration — `handlers/resource/get.go`

- Sets `d.Vault = s.vault != nil`
- `prepareVaultButton` → determines button text based on pledge status
- `prepareVaultPledgeAddForm` → calculates VP stats, required VP, torrent size; the freeze period, and `FrozenFor` of the viewer's own pledge
- `prepareVaultPledgeRemoveForm` → `FrozenFor` of the viewer's pledge and the freeze period

## Vault API SDK — `services/vault/api.go`

HTTP client for external Vault API service.

### Resource Status Constants

| Status | Value | Description |
|--------|-------|-------------|
| StatusQueued | 0 | Queued for processing |
| StatusProcessing | 1 | Being processed |
| StatusCompleted | 2 | Fully stored |
| StatusFailed | 3 | Storage failed |

Helper methods: `IsStored()`, `IsFailed()`, `IsProcessing()`, `GetProgress()`.

### API Methods

| Method | HTTP | Endpoint | Returns |
|--------|------|----------|---------|
| `GetResource` | GET | `/resource/{id}` | Resource or 404 |
| `GetResourceCached` | GET | `/resource/{id}` | Cached (1min TTL) |
| `PutResource` | PUT | `/resource/{id}` | 202 Accepted |
| `DeleteResource` | DELETE | `/resource/{id}` | 202 Accepted |

Constructor `NewApi` returns `nil` if `VAULT_SERVICE_HOST` is empty.

## Business Rules

1. **Balance**: non-negative, `NULL` = unlimited, synced with claims on every access
2. **Pledges**: cannot exceed available VP; cannot remove frozen pledge; one per user per resource; freeze determined dynamically
3. **Resources**: funded when `funded_vp >= required_vp`; abandoned expired resources deleted after 1 day, expired with unfunded pledges after 7 days; vaulted resources can expire if underfunded
4. **Transactions**: all balance ops logged in tx_log; balance never zero; OpTypeFund always negative, OpTypeClaim always positive
5. **Concurrency**: `SELECT FOR UPDATE` for all balance-changing operations
6. **Copyright**: if torrent is blocked/removed by copyright holders, it may disappear from vault

## Migrations

| # | Migration | Description |
|---|-----------|-------------|
| 24 | create_vault_schema | Creates `vault` schema |
| 25 | create_user_vp | Creates `user_vp` table |
| 26 | create_pledge | Creates `pledge` table |
| 27 | create_resource | Creates `resource` table |
| 28 | create_tx_log | Creates `tx_log` table |
| 29 | alter_user_vp_total_nullable | Makes `user_vp.total` nullable |

All include `update_updated_at` triggers. Down migrations drop tables or revert alterations.


## Transfer status as the user sees it

Moved to [transfer_status.md](transfer_status.md) (2026-10-03): the resource
page's chain, badge, piece bar, hint, plan box and details, with Vault's
states on it (`vault_waiting`, `vault_missing`, `vault_failed`). The
dashboard's own rows are "Live progress rows" above.

## Ghost resources (reaper)

A ghost is a resource with `funded_vp > 0` and no *funded* pledge
(`GetGhostResources`). It was written for account deletion, where the pledges
cascade away with the user — but a pledger who merely lost their points leaves
an **unfunded pledge row behind**, and `vault.pledge` references the resource
`ON DELETE RESTRICT`.

The sweep therefore goes through the same path as any reaped resource
(`reaper.reapResource`): pledges removed and pledgers told first, the resource
last. Until 2026-09-21 it called `RemoveResource` alone, which deleted the
content from the Vault, failed on the foreign key, and left the row saying
`vaulted = true` — every hour, for the one resource it happened to
(2026-08-29 → 09-21). The message is "expired", not "transfer timeout": the
content was stored, its funding went away.

## Balance resync (reaper)

`user_vp.total` follows claims only through `UpdateUserVP`, which runs on `user.updated` or on `/vault`. A membership can end with neither: the 2026-07-13 `patreon.member` matview fix took bronze from expired trials without publishing anything, a `billing.member` runs out by date, a Patreon member ages out of the matview's `pledge_cadence + 5 days` window. On 2026-09-25 that was 117 accounts without a membership holding 1562 VP (~1.5 TB of the Vault's 25 TB), most of them untouched since spring.

So every `vault reap` run first calls `reaper.resyncUserVP`: for each user with a funded pledge (`GetUserVPsWithFundedPledges`) it compares the balance with the claims and calls `UpdateUserVP` where they differ. The defunded pledges then go the ordinary way — resource marked expired now, reaped with the "expired" letter after `VAULT_RESOURCE_EXPIRE_PERIOD`, un-expired by `fundPledge` if the tier comes back first.

Two guards, because a lowered balance ends in content deleted from S3:

- **A claims error skips the user** — never read as "free". A failed lookup is not an absent membership.
- **`VAULT_VP_RESYNC_MAX_DROPS`** (default 200): if one run would lower more balances than that, it changes nothing and logs `balance resync would lower more balances than allowed` at error level. That many at once is a broken claims source (an emptied matview, a dropped view), not a wave of cancellations. Raises don't count. After a deliberate mass change, raise the limit for one run.

Every run logs `balance resync done` with `checked` / `changed` / `lowered`.

