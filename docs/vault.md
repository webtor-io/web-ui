# Vault System

## Overview

The Vault system manages users' virtual points (Vault Points, VP) and their pledges to resources (torrents) for long-term storage.

### Core Concepts

- **Vault Points (VP)** — virtual points, **1 VP = 1 GB** of torrent size. Provided by subscription tier.
- **Pledge** — user's investment of VP in a specific resource. One pledge per user per resource.
- **Funding** — resource is funded when `funded_vp >= required_vp`.
- **Vaulting** — moving a funded resource to long-term storage.
- **Freezing** — pledge is locked for `VAULT_PLEDGE_FREEZE_PERIOD` (default: 24h) after creation. Freeze is lifted when resource is vaulted.
- **Expiration** — resource marked expired when `funded_vp < required_vp`. Deleted after 7 days.
- **Transfer Timeout** — if vaulting fails within `VAULT_RESOURCE_TRANSFER_TIMEOUT_PERIOD` (default: 7 days), VP is returned.

### Resource Lifecycle

1. **Created** → `funded=false`, `vaulted=false`, `expired=false`
2. **Funded** → `funded_vp >= required_vp`, `funded=true`
3. **Vaulted** → `vaulted=true` (all pledges auto-unfrozen)
4. **Expired** (optional) → `funded_vp < required_vp`, deleted after 7 days

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
- Freeze determined dynamically: `frozen_at + freeze_period > now()` (unless resource is vaulted)

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
| `VAULT_PLEDGE_FREEZE_PERIOD` | 24h | Pledge freeze period |
| `VAULT_RESOURCE_EXPIRE_PERIOD` | 7 days | Deletion delay after expiration |
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

#### GetUserStats

Returns `UserStats`:

```go
type UserStats struct {
    Total     *float64 // nil = unlimited
    Frozen    float64  // VP in frozen+funded pledges
    Funded    float64  // VP in all funded pledges (>= 0)
    Available *float64 // Total - Funded, nil if unlimited (>= 0)
    Claimable float64  // Funded but not frozen
}
```

Always syncs balance first via `UpdateUserVP`.

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

#### IsPledgeFrozen

Dynamic check: returns `false` if resource is vaulted, otherwise checks `frozen_at + freeze_period > now()`.

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
- `expired_at < now - VAULT_RESOURCE_EXPIRE_PERIOD`, or
- `funded_at < now - VAULT_RESOURCE_TRANSFER_TIMEOUT_PERIOD AND vaulted = false`

For each: removes pledges (returns VP), sends notifications, deletes resource. Partial failures logged and skipped.

## HTTP Endpoints

Handler: `handlers/vault/handler.go` (registered only if vault service is not nil).

Files: `handler.go`, `index.go`, `add.go`, `remove.go`.

### POST /vault/pledge/add

Creates a new pledge. Auth required.

- Form: `resource_id` (required)
- Header: `X-Return-Url` for redirect
- Calls `GetOrCreateResource` → `CreatePledge`
- Redirects with `status=success` or `err=<message>`

### POST /vault/pledge/remove

Removes a pledge. Auth required (middleware).

- Form: `resource_id` (required)
- Checks freeze status, returns error if frozen
- Calls `RemovePledge`
- Redirects with `status=success` or `status=error&err=<message>`

### GET /vault/pledge

Displays pledge list. Auth required (middleware).

Data structures:

```go
type PledgeDisplay struct {
    PledgeID   string
    ResourceID string
    Resource   *vaultModels.Resource
    Amount     float64
    IsFrozen   bool   // computed via IsPledgeFrozen
    Funded     bool   // from DB
    CreatedAt  string // formatted
}

type PledgeListData struct {
    Pledges               []PledgeDisplay
    FreezePeriod          time.Duration
    ExpirePeriod          time.Duration
    TransferTimeoutPeriod time.Duration
}
```

Status badges: "Frozen" (green), "Expiring" (red), "Claimable" (yellow).

## UI Components

### Vault Button — `templates/partials/vault/button.html`

Shown on resource pages for authenticated users when vault is available.
- "Keep This Torrent Available" → opens pledge add modal
- "Remove Pledge" → opens pledge remove modal
- Uses `data-async-target` and `data-async-push-state="false"`

### Pledge Add Modal — `templates/partials/vault/pledge-add-modal.html`

States: sufficient VP → confirm form; insufficient VP → upgrade link; success (funded/not funded); vaulted; error.

### Pledge Remove Modal — `templates/partials/vault/pledge-remove-modal.html`

States: frozen warning; confirmation; success; error.

### Resource Handler Integration — `handlers/resource/get.go`

- Sets `d.Vault = s.vault != nil`
- `prepareVaultButton` → determines button text based on pledge status
- `prepareVaultPledgeAddForm` → calculates VP stats, required VP, torrent size
- `prepareVaultPledgeRemoveForm` → checks freeze status

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
3. **Resources**: funded when `funded_vp >= required_vp`; expired resources deleted after 7 days; vaulted resources can expire if underfunded
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
