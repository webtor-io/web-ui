# Technical Specification: Vault System

## Overview

The Vault system is designed to manage users' virtual points (Vault Points, VP) and their pledges to resources. The system allows users to invest their points in resources (torrents) to ensure their long-term storage in the vault.

### Core Concepts

- **Vault Points (VP)** — user's virtual points that can be invested in resources
  - **1 VP = 1 GB** of torrent size
  - Points are provided as part of subscription or other activities (amount depends on subscription tier)
- **Pledge** — user's investment in a specific resource
- **Resource** — torrent or other content that can be placed in the vault
- **Funding** — the process of accumulating sufficient VP for a resource
- **Vaulting** — moving a resource to long-term storage after funding
- **Freezing** — locking a pledge so the user cannot withdraw it
  - **Freeze period:** configurable via `VAULT_PLEDGE_FREEZE_PERIOD` (default: 24 hours) after pledge creation
  - **Freeze is automatically lifted** when the resource is vaulted
- **Claiming** — returning points to the user from an unfrozen pledge
- **Expiration** — marking a resource as expired when funding drops below required amount
  - **Deletion period: 7 days** after expiration

## Notification System

The project includes a notification system to keep users informed about their resources. Notifications are sent via email and logged in the `public.notification` table to prevent duplicates (at most one notification per key per recipient every 24 hours).

### Triggers

1.  **Resource Vaulted**: Sent immediately when a resource becomes vaulted (event `resource.vaulted`). All users who made a pledge for this resource are notified.
    -   **Key**: `vaulted-%resource_id%`
    -   **Template**: `vaulted.html`
2.  **Expiring Resources**: Sent for resources that will expire in less than 7, 3, or 1 day. This is handled by a periodic CLI command.
    -   **Key**: `expiring-%days%`
    -   **Template**: `expiring.html`
3.  **Transfer Timeout**: Sent when a resource fails to transfer to the vault within the configured timeout period (default: 7 days). Triggered during the `vault reap` command. Notifies users that the transfer failed due to lack of seeds, all VP has been returned, and suggests trying another torrent or retrying later.
    -   **Key**: `transfer-timeout-%resource_id%`
    -   **Template**: `transfer-timeout.html`
4.  **Resource Expired**: Sent when a resource is removed from the vault due to expiration. Triggered during the `vault reap` command. Notifies users that the resource has been removed and they can pledge again if needed.
    -   **Key**: `expired-%resource_id%`
    -   **Template**: `expired.html`

### CLI Command

To send notifications about expiring resources, use the following command:

```bash
./web-ui notification send
```

This command should be scheduled to run daily (e.g., via cron). It aggregates all expiring resources for each user into a single email.

## Database Schema

All tables are located in a separate `vault` schema to isolate them from the main application data.

### 1. Table `vault.user_vp`

Stores information about each user's Vault Points balance.

```sql
CREATE TABLE vault.user_vp (
	user_id uuid NOT NULL,
	total numeric,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT user_vp_pk PRIMARY KEY (user_id),
	CONSTRAINT user_vp_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE
);
```

**Fields:**
- `user_id` — user UUID (primary key, foreign key to `public.user`)
- `total` — total amount of user's points (numeric, nullable; NULL means unlimited balance)
- `created_at` — record creation time
- `updated_at` — last update time (automatically updated by trigger)

**Features:**
- Value `total = NULL` means unlimited balance (e.g., for premium users or unlimited tier)
- Balance is synchronized with the claims system on every access via `UpdateUserVP` method
- When balance changes, a record is created in `tx_log` with the difference
- For unlimited accounts (NULL), no tx_log entries are created when transitioning from NULL to NULL

### 2. Table `vault.pledge`

Stores information about user pledges to resources.

```sql
CREATE TABLE vault.pledge (
	pledge_id uuid DEFAULT uuid_generate_v4() NOT NULL,
	resource_id text NOT NULL,
	user_id uuid NOT NULL,
	amount numeric NOT NULL,
	funded bool DEFAULT true NOT NULL,
	frozen_at timestamptz DEFAULT now() NOT NULL,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT pledge_pk PRIMARY KEY (pledge_id),
	CONSTRAINT pledge_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE,
	CONSTRAINT pledge_resource_user_unique UNIQUE (resource_id, user_id)
);
```

**Fields:**
- `pledge_id` — pledge UUID (primary key, auto-generated)
- `resource_id` — text resource identifier (no foreign key)
- `user_id` — user UUID (foreign key to `public.user`)
- `amount` — amount of pledged Vault Points (numeric)
- `funded` — pledge active status flag (default `true`)
- `frozen_at` — pledge freeze time (default `now()`)
- `created_at` — pledge creation time
- `updated_at` — last update time (automatically updated by trigger)

**Constraints:**
- `pledge_resource_user_unique` — unique index on `(resource_id, user_id)` to prevent duplicate pledges

**Features:**
- `funded = true` means the pledge is active and counted in resource funding
- Freeze status is determined dynamically by comparing `frozen_at + freeze_period` with current time
- A pledge can be removed only if it's not frozen (freeze period has expired)
- When a pledge is created, a record is created in `tx_log` with type `OpTypeFund`
- When a pledge is removed, a record is created in `tx_log` with type `OpTypeClaim`

### 3. Table `vault.resource`

Stores the current state of resources to track their readiness for vaulting.

```sql
CREATE TABLE vault.resource (
	resource_id text NOT NULL,
	required_vp numeric NOT NULL,
	funded_vp numeric NOT NULL,
	funded bool DEFAULT false NOT NULL,
	vaulted bool DEFAULT false NOT NULL,
	funded_at timestamptz,
	vaulted_at timestamptz,
	expired bool DEFAULT false NOT NULL,
	expired_at timestamptz,
	name text,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT resource_pk PRIMARY KEY (resource_id)
);
```

**Fields:**
- `resource_id` — text resource identifier (primary key)
- `required_vp` — required amount of points to vault the resource (numeric)
- `funded_vp` — current amount of points from all active pledges (numeric)
- `funded` — full funding flag (becomes `true` when `funded_vp >= required_vp`)
- `vaulted` — vaulted flag (becomes `true` when resource is placed in vault)
- `funded_at` — time when full funding was achieved
- `vaulted_at` — time when resource was placed in vault
- `expired` — funding expiration flag (becomes `true` when `funded_vp < required_vp` after funding)
- `expired_at` — funding expiration time
- `name` — name of the torrent (nullable, extracted from API response during resource creation)
- `created_at` — record creation time
- `updated_at` — last update time (automatically updated by trigger)

**Resource Lifecycle:**
1. **Creation** — `funded = false`, `vaulted = false`, `expired = false`
2. **Funding** — accumulating pledges, `funded_vp` grows
3. **Funded** — `funded_vp >= required_vp`, `funded = true`, `funded_at` is set
4. **Vaulted** — `vaulted = true`, `vaulted_at` is set
5. **Expired** (optional) — if `funded_vp < required_vp`, then `expired = true`, `expired_at` is set

### 4. Table `vault.tx_log`

Stores a log of all transactions with user balances for audit and change tracking.

```sql
CREATE TABLE vault.tx_log (
	tx_log_id uuid DEFAULT uuid_generate_v4() NOT NULL,
	user_id uuid NOT NULL,
	resource_id text,
	balance numeric NOT NULL,
	op_type smallint NOT NULL,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT tx_log_pk PRIMARY KEY (tx_log_id),
	CONSTRAINT tx_log_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE
);
```

**Fields:**
- `tx_log_id` — log entry UUID (primary key, auto-generated)
- `user_id` — user UUID (foreign key to `public.user`)
- `resource_id` — text resource identifier (can be `NULL` for operations not related to resources)
- `balance` — balance change (can be positive or negative, cannot be zero)
- `op_type` — operation type (smallint)
- `created_at` — record creation time
- `updated_at` — last update time (automatically updated by trigger)

**Operation Types (`op_type`):**

| Constant | Value | Description | balance Sign | resource_id |
|----------|-------|-------------|--------------|-------------|
| `OpTypeChangeTier` | 1 | Tier change | + or - | NULL |
| `OpTypeFund` | 2 | Pledge creation | - (always negative) | Required |
| `OpTypeClaim` | 3 | Pledge claim | + (always positive) | Required |

**Logging Rules:**
- When tier changes, a record is created with the difference between old and new balance
- When a pledge is created, a record is created with a negative value (debit)
- When a pledge is claimed, a record is created with a positive value (credit)
- For unlimited accounts (`total = NULL`), no log entries are created on tier change
- For free tier accounts (`total = 0`), no log entries are created on tier change

### 7. Table `public.notification`

Stores information about sent notifications.

- `notification_id` (uuid, primary key) — unique identifier for each notification
- `key` (text) — unique key for the notification type and context (e.g., `vaulted-123` or `expiring-7`)
- `title` (text) — the subject line of the email
- `template` (text) — the name of the template used to render the body
- `body` (text) — the rendered HTML content of the email
- `to` (text) — recipient's email address
- `created_at` (timestamptz) — when the notification was created/sent
- `updated_at` (timestamptz) — last update timestamp

## Data Models (Go)

### UserVP (`models/vault/user_vp.go`)

```go
type UserVP struct {
	tableName struct{}  `pg:"vault.user_vp"`
	UserID    uuid.UUID `pg:"user_id,pk,type:uuid"`
	Total     *float64  `pg:"total,type:numeric"`
	CreatedAt time.Time `pg:"created_at,notnull,default:now()"`
	UpdatedAt time.Time `pg:"updated_at,notnull,default:now()"`
	
	User *models.User `pg:"rel:has-one,fk:user_id"`
}
```

**Methods:**
- `GetUserVP(ctx, db, userID)` — get user's VP
- `CreateUserVP(ctx, db, userID, total)` — create VP record
- `UpdateUserVP(ctx, db, userID, total)` — update VP balance

### Pledge (`models/vault/pledge.go`)

```go
type Pledge struct {
	tableName  struct{}  `pg:"vault.pledge"`
	PledgeID   uuid.UUID `pg:"pledge_id,pk,type:uuid,default:uuid_generate_v4()"`
	ResourceID string    `pg:"resource_id,notnull"`
	UserID     uuid.UUID `pg:"user_id,notnull,type:uuid"`
	Amount     float64   `pg:"amount,notnull,type:numeric"`
	Funded     bool      `pg:"funded,notnull,default:true"`
	FrozenAt   time.Time `pg:"frozen_at,notnull,default:now()"`
	CreatedAt  time.Time `pg:"created_at,notnull,default:now()"`
	UpdatedAt  time.Time `pg:"updated_at,notnull,default:now()"`
	
	User     *models.User `pg:"rel:has-one,fk:user_id"`
	Resource *Resource    `pg:"rel:has-one,fk:resource_id"`
}
```

**Methods:**
- `GetPledge(ctx, db, pledgeID)` — get pledge by ID
- `GetUserPledges(ctx, db, userID)` — get all user's pledges (sorted by creation time DESC)
- `GetUserPledgesWithResources(ctx, db, userID)` — get all user's pledges with resource information (sorted by creation time DESC)
- `GetResourcePledges(ctx, db, resourceID)` — get all pledges for a resource
- `GetUserResourcePledge(ctx, db, userID, resourceID)` — get pledge for specific user and resource
- `GetFundedResourcePledges(ctx, db, resourceID)` — get active pledges for a resource
- `CreatePledge(ctx, db, userID, resourceID, amount)` — create pledge
- `UpdatePledgeFunded(ctx, db, pledgeID, funded)` — update funded status
- `DeletePledge(ctx, db, pledgeID)` — delete pledge
- `SumFundedPledgesForResource(ctx, db, resourceID)` — sum of active pledges for a resource

**Method Details:**

#### `GetUserPledgesWithResources`

Returns all pledges for a specific user with related resource information loaded via database relation.

**Signature:**
```go
func GetUserPledgesWithResources(ctx context.Context, db *pg.DB, userID uuid.UUID) ([]Pledge, error)
```

**Parameters:**
- `ctx` — request context
- `db` — PostgreSQL database connection
- `userID` — user identifier

**Returns:**
- Array of `Pledge` objects with `Resource` field populated
- Pledges are sorted by `created_at DESC` (newest first)
- Error if database query fails

**Algorithm:**
1. Query `vault.pledge` table with `WHERE pledge.user_id = ?`
2. Use `Relation("Resource")` to load related resource data via JOIN
3. Order results by `pledge.created_at DESC`
4. Return array of pledges with populated `Resource` field

**Usage:**
Used by the pledges list page (`GET /vault/pledge`) to display user's pledges with torrent names and resource information.

**Example:**
```go
pledges, err := vaultModels.GetUserPledgesWithResources(ctx, db, userID)
if err != nil {
    return nil, errors.Wrap(err, "failed to get user pledges")
}
for _, pledge := range pledges {
    if pledge.Resource != nil && pledge.Resource.Name != "" {
        fmt.Printf("Pledge for: %s\n", pledge.Resource.Name)
    }
}
```

### Resource (`models/vault/resource.go`)

```go
type Resource struct {
	tableName  struct{}   `pg:"vault.resource"`
	ResourceID string     `pg:"resource_id,pk"`
	RequiredVP float64    `pg:"required_vp,notnull,type:numeric"`
	FundedVP   float64    `pg:"funded_vp,notnull,type:numeric"`
	Funded     bool       `pg:"funded,notnull,default:false"`
	Vaulted    bool       `pg:"vaulted,notnull,default:false"`
	FundedAt   *time.Time `pg:"funded_at"`
	VaultedAt  *time.Time `pg:"vaulted_at"`
	Expired    bool       `pg:"expired,notnull,default:false"`
	ExpiredAt  *time.Time `pg:"expired_at"`
	Name       string     `pg:"name,notnull"`
	CreatedAt  time.Time  `pg:"created_at,notnull,default:now()"`
	UpdatedAt  time.Time  `pg:"updated_at,notnull,default:now()"`
}
```

**Methods:**
- `GetResource(ctx, db, resourceID)` — get resource by ID
- `GetFundedResources(ctx, db)` — get all funded resources
- `GetVaultedResources(ctx, db)` — get all vaulted resources
- `CreateResource(ctx, db, resourceID, requiredVP, torrentName)` — create resource (torrentName is string, empty string if name is not available)
- `UpdateResourceFundedVP(ctx, db, resourceID, fundedVP)` — update funded amount
- `MarkResourceFunded(ctx, db, resourceID)` — mark resource as funded
- `MarkResourceVaulted(ctx, db, resourceID)` — mark resource as vaulted (sets vaulted=true, vaulted_at=now())
- `UpdateResourceVaulted(ctx, db, resourceID)` — mark resource as vaulted (sets vaulted=true, vaulted_at=now(), used after Vault API sync)
- `MarkResourceExpired(ctx, db, resourceID)` — mark resource as expired
- `DeleteResource(ctx, db, resourceID)` — delete resource

### TxLog (`models/vault/tx_log.go`)

```go
type TxLog struct {
	tableName  struct{}  `pg:"vault.tx_log"`
	TxLogID    uuid.UUID `pg:"tx_log_id,pk,type:uuid,default:uuid_generate_v4()"`
	UserID     uuid.UUID `pg:"user_id,notnull,type:uuid"`
	ResourceID *string   `pg:"resource_id"`
	Balance    float64   `pg:"balance,notnull,type:numeric"`
	OpType     int16     `pg:"op_type,notnull"`
	CreatedAt  time.Time `pg:"created_at,notnull,default:now()"`
	UpdatedAt  time.Time `pg:"updated_at,notnull,default:now()"`
	
	User *models.User `pg:"rel:has-one,fk:user_id"`
}
```

**Operation Type Constants:**
```go
const (
	OpTypeChangeTier int16 = 1 // Tier change
	OpTypeFund       int16 = 2 // Pledge creation
	OpTypeClaim      int16 = 3 // Pledge claim
)
```

**Methods:**
- `GetTxLog(ctx, db, txLogID)` — get log entry by ID
- `GetUserTxLogs(ctx, db, userID)` — get all user's log entries
- `GetUserTxLogsByType(ctx, db, userID, opType)` — get log entries by operation type
- `GetResourceTxLogs(ctx, db, resourceID)` — get log entries for a resource
- `CreateTxLog(ctx, db, userID, resourceID, balance, opType)` — create log entry
- `CreateChangeTierLog(ctx, db, userID, balance)` — create tier change log entry
- `CreateFundLog(ctx, db, userID, resourceID, balance)` — create pledge creation log entry
- `CreateClaimLog(ctx, db, userID, resourceID, balance)` — create pledge claim log entry
- `GetUserBalanceSum(ctx, db, userID)` — calculate total balance from transaction log

## Business Logic

### Vault Service (`services/vault/vault.go`)

Main service for working with the Vault Points system.

**Configuration:**
- `VAULT_SERVICE_HOST` / `--vault-service-host` — Vault service host (required, configured via Vault API)
- `VAULT_SERVICE_PORT` / `--vault-service-port` — Vault service port (configured via Vault API, default: 80)
- `VAULT_SECURE` / `--vault-secure` — Use HTTPS for Vault service (configured via Vault API, default: false)
- `VAULT_PLEDGE_FREEZE_PERIOD` / `--vault-pledge-freeze-period` — pledge freeze period (default: 24 hours)
- `VAULT_RESOURCE_EXPIRE_PERIOD` / `--vault-resource-expire-period` — period after which unfunded resource is removed from vault (default: 7 days)
- `VAULT_RESOURCE_TRANSFER_TIMEOUT_PERIOD` / `--vault-resource-transfer-timeout-period` — period after which resource is removed and transfer attempts are stopped (default: 7 days)

**Dependencies:**
- Vault API client (`services/vault.Api`) — for communicating with external Vault API service
- Claims service (`services/claims`) — for getting user tier and VP information
- HTTP client — for API calls
- PostgreSQL database (`common-services/PG`) — for database operations
- REST API service (`services/api`) — for accessing torrent metadata and calculating required VP

**Constructor:**
```go
func New(c *cli.Context, vaultApi *Api, cl *claims.Claims, client *http.Client, pg *cs.PG, restApi *api.Api) *Vault
```

**Parameters:**
- `c` — CLI context with configuration flags
- `vaultApi` — Vault API client instance (created via `vault.NewApi`)
- `cl` — Claims service instance
- `client` — HTTP client
- `pg` — PostgreSQL database connection
- `restApi` — REST API service instance

**Returns:** `nil` if `vaultApi` is `nil`, otherwise returns a configured `*Vault` instance.

#### Method `UpdateUserVP`

Synchronizes user balance with the claims system (subscriptions/tiers).

**Signature:**
```go
func (s *Vault) UpdateUserVP(ctx context.Context, user *auth.User) (*vaultModels.UserVP, error)
```

**Algorithm:**
1. Get current user claims from Claims service using user's email and Patreon ID
2. Extract VP amount from claims (`Claims.Vault.Points`):
   - If field is present and not nil → convert to `*float64`
   - If field is absent or nil → `claimsPoints = nil` (unlimited)
3. Execute in database transaction with row locking (`SELECT FOR UPDATE`):
   - **Lock the user's row** to prevent concurrent modifications
   - **Case 1: No record exists**
     - Create new `user_vp` record with `total = claimsPoints`
     - If `claimsPoints != nil` AND `claimsPoints != 0`, create `tx_log` entry with `OpTypeChangeTier` and `balance = claimsPoints`
     - If `claimsPoints == nil` (unlimited) OR `claimsPoints == 0` (free tier), do not create tx_log entry
     - **Recalculate pledge funding** based on new total (see `recalculatePledgeFunding` method)
   - **Case 2: Record exists and points match**
     - Do nothing, return existing record
   - **Case 3: Record exists and points differ**
     - Calculate difference: `difference = newValue - oldValue` (treating NULL as 0)
     - Update `user_vp.total` to new value
     - If `newValue != 0`, create `tx_log` entry with `OpTypeChangeTier` and `balance = difference`
     - If `newValue == 0` (free tier), do not create tx_log entry
     - **Recalculate pledge funding** based on new total (see `recalculatePledgeFunding` method)
     - Fetch and return updated record
4. Return updated `UserVP` record or error

**Features:**
- Uses `SELECT FOR UPDATE` to prevent race conditions during concurrent updates
- NULL in `total` field means unlimited balance (no point limit)
- For unlimited accounts, no tx_log entries are created when first created with NULL
- When transitioning from unlimited (NULL) to limited, treats NULL as 0 for difference calculation
- When balance changes, only the **difference** is logged, not the absolute value
- All operations are atomic within a single database transaction

**Error Handling:**
- Returns error if database connection is unavailable
- Returns error if claims cannot be fetched
- Returns error if transaction fails at any step

#### Method `UpdateUserVPIfExists`

Synchronizes user balance with the claims system only if the user already has a record in the Vault system.

**Signature:**
```go
func (s *Vault) UpdateUserVPIfExists(ctx context.Context, user *auth.User) (*vaultModels.UserVP, error)
```

**Parameters:**
- `ctx` — request context
- `user` — authenticated user (`*auth.User`)

**Algorithm:**
1. Get database connection from PG service
2. Call `vaultModels.GetUserVP` to check if a `user_vp` record exists for the given user ID
3. If no record is found (returns `nil`), return `nil, nil` immediately without error
4. If record is found, call `UpdateUserVP(ctx, user)` to perform the synchronization
5. Return the result of `UpdateUserVP`

**Features:**
- Prevents unwanted creation of Vault records for users who have not interacted with the Vault system
- Safely wraps `UpdateUserVP` for use in automated event processing (e.g., `user.updated` events)
- Returns `nil, nil` if the user is not in the Vault (not an error)

**Error Handling:**
- Returns error if database connection is unavailable
- Returns error if database query fails
- Propagates errors from `UpdateUserVP` if it is called

#### Method `GetUserStats`

Returns detailed statistics on user's Vault Points.

**Signature:**
```go
func (s *Vault) GetUserStats(ctx context.Context, user *auth.User) (*UserStats, error)
```

**Return Structure:**
```go
type UserStats struct {
	Total     *float64 // Total vault points (nil if unlimited)
	Frozen    float64  // Points in frozen and funded pledges
	Funded    float64  // Points in funded pledges
	Available *float64 // Total minus funded (nil if total is nil)
	Claimable float64  // Funded but not frozen
}
```

**Algorithm:**
1. Call `UpdateUserVP` to synchronize balance with claims system
2. Fetch all user's pledges in a single database query using `GetUserPledges`
3. Calculate statistics in application code by iterating through pledges:
   - `Total` = value from `user_vp.total` (nil if unlimited)
   - For each pledge, check if it's frozen using `IsPledgeFrozen(pledge)` method
   - `Frozen` = sum of pledges where `IsPledgeFrozen() = true AND funded = true`
   - `Funded` = sum of all pledges where `funded = true` (guaranteed to be >= 0)
   - `Claimable` = sum of pledges where `funded = true AND IsPledgeFrozen() = false`
   - `Available` = `Total - Funded` (nil if `Total` is nil, guaranteed to be >= 0 otherwise)
4. Apply safety constraints:
   - If `Funded < 0`, set `Funded = 0`
   - If `Available < 0`, set `Available = 0`
5. Return `UserStats` structure or error

**Features:**
- Always synchronizes balance with claims first via `UpdateUserVP`
- All pledges are fetched in one query to minimize database round-trips
- Statistics calculation happens in application code, not in database
- For unlimited accounts (when `Total = nil`), `Available` is also nil
- If `Total` is nil, both `Total` and `Available` fields are nil
- `Funded` and `Available` are guaranteed to never be negative (automatically set to 0 if calculation results in negative value)

**Error Handling:**
- Returns error if database connection is unavailable
- Returns error if `UpdateUserVP` fails
- Returns error if fetching pledges fails

#### Method `CreatePledge`

Creates a new pledge for a resource, deducting the required VP from user's available balance.

**Signature:**
```go
func (s *Vault) CreatePledge(ctx context.Context, user *auth.User, resource *vaultModels.Resource) (*vaultModels.Pledge, error)
```

**Parameters:**
- `ctx` — request context
- `user` — authenticated user making the pledge (`*auth.User`)
- `resource` — resource to pledge to (`*vaultModels.Resource`)

**Algorithm:**
1. Execute in database transaction with row locking:
   - **Lock user's VP row** using `SELECT FOR UPDATE` to prevent concurrent modifications
   - **Check if user has sufficient VP:**
     - If `user_vp.total = nil` (unlimited) → allow pledge creation
     - If `user_vp.total != nil` (limited):
       - Fetch all user's pledges
       - Calculate `fundedSum` = sum of all pledges where `funded = true`
       - Calculate `available = total - fundedSum`
       - If `available < resource.RequiredVP` → return error "insufficient vault points"
   - **Create pledge record:**
     - `user_id` = user's ID
     - `resource_id` = resource's ID
     - `amount` = `resource.RequiredVP`
     - `funded` = `true` (pledge is active)
     - `frozen` = `true` (pledge is frozen, cannot be claimed)
     - `frozen_at` = `now()` (freeze timestamp)
   - **Create transaction log entry:**
     - `user_id` = user's ID
     - `resource_id` = resource's ID
     - `balance` = `-resource.RequiredVP` (negative, as VP is deducted)
     - `op_type` = `OpTypeFund` (2)
   - **Update resource funding:**
     - Calculate `newFundedVP = resource.FundedVP + resource.RequiredVP`
     - Update `resource.funded_vp` to `newFundedVP` using `UpdateResourceFundedVP`
     - If `newFundedVP >= resource.RequiredVP`:
       - Mark resource as unexpired and funded using `MarkResourceUnexpiredAndFunded`
       - Set `resource.expired = false`, `resource.expired_at = NULL`, `resource.funded = true`, `resource.funded_at = now()`
       - **Check Vault API status** using `putResourceToVaultAPI` to determine if resource is already vaulted
       - If `putResourceToVaultAPI` returns `true`, call `UpdateResourceVaulted` to mark resource as vaulted
2. Return created pledge or error

**Features:**
- Uses `SELECT FOR UPDATE` to prevent race conditions during concurrent pledge creation
- Pledges are created as funded and frozen by default
- Freeze period is 1 day (enforced by business logic, not in this method)
- For unlimited accounts (NULL total), no balance check is performed
- Transaction log entry is always created with negative balance (OpTypeFund)
- All operations are atomic within a single database transaction

**Error Handling:**
- Returns error if database connection is unavailable
- Returns error if user VP record not found
- Returns error if user has insufficient available VP
- Returns error if pledge creation fails
- Returns error if transaction log creation fails
- Returns error if transaction fails at any step

#### Method `GetOrCreateResource`

Retrieves an existing resource or creates a new one if it doesn't exist.

**Signature:**
```go
func (s *Vault) GetOrCreateResource(ctx context.Context, claims *api.Claims, resourceID string) (*vaultModels.Resource, error)
```

**Parameters:**
- `ctx` — request context
- `claims` — API claims for authentication (`*api.Claims`)
- `resourceID` — resource identifier (torrent hash)

**Algorithm:**
1. Check if resource exists in database using `vaultModels.GetResource`
2. If resource exists, return it immediately
3. If resource doesn't exist:
   - Get list from REST API using `ListResourceContentCached` with `Output: api.OutputList`
   - Calculate required VP from list size: `requiredVP = list.Size / (1024 * 1024 * 1024)`
   - Get torrent name using `getTorrentName` method (extracts from API response)
   - Create new resource using `vaultModels.CreateResource` with:
     - `resource_id` = provided resourceID
     - `required_vp` = calculated VP amount
     - `name` = extracted torrent name (can be nil)
     - `funded_vp` = 0
     - `funded` = false
     - `vaulted` = false
     - `expired` = false
4. Return resource (existing or newly created)

**Features:**
- Idempotent operation: safe to call multiple times for the same resource
- Automatically calculates required VP based on torrent size
- Does not use transactions (resource creation is atomic)
- Used by handlers before creating pledges

**Error Handling:**
- Returns error if database connection is unavailable
- Returns error if resource lookup fails
- Returns error if VP calculation fails (REST API error)
- Returns error if resource creation fails
- Wraps all errors with context for debugging

**Usage:**
This method is called by the vault handler before creating a pledge. It ensures that a resource record exists in the database with the correct required VP amount calculated from the torrent size.

#### Method `getTorrentName`

Extracts torrent name from API response (private method).

**Signature:**
```go
func (s *Vault) getTorrentName(ctx context.Context, claims *api.Claims, resourceID string) (*string, error)
```

**Parameters:**
- `ctx` — request context
- `claims` — API claims for authentication (`*api.Claims`)
- `resourceID` — resource identifier (torrent hash)

**Algorithm:**
1. Call REST API using `ListResourceContentCached` with `Output: api.OutputList`
2. Check if `list.Name` is not empty
3. Return pointer to `list.Name` if present, otherwise return `nil`

**Features:**
- Private method used internally by `GetOrCreateResource`
- Uses cached REST API call for performance
- Returns `nil` if torrent name is not available (not an error)
- Extracts name from the top-level list response

**Error Handling:**
- Returns error if REST API call fails
- Returns `nil, nil` if name is not present in response
- Wraps errors with context for debugging

**Usage:**
This method is called by `GetOrCreateResource` to extract the torrent name during resource creation. It encapsulates the logic of name extraction, keeping the main method clean.

#### Method `GetRequiredVP`

Calculates the required vault points for a resource based on its total size.

**Signature:**
```go
func (s *Vault) GetRequiredVP(ctx context.Context, claims *api.Claims, resourceID string) (float64, error)
```

**Parameters:**
- `ctx` — request context
- `claims` — API claims for authentication (`*api.Claims`)
- `resourceID` — resource identifier (torrent hash)

**Algorithm:**
1. Call REST API to list all resource content using `ListResourceContentCached` with `Output: api.OutputList`
2. Get total size in bytes from the response (`list.Size`)
3. Convert bytes to VP using formula: `requiredVP = totalSize / (1024 * 1024 * 1024)`
4. Return calculated VP amount

**Features:**
- Uses cached REST API call for performance
- Conversion rate: **1 VP = 1 GB** of torrent size
- Result is returned as float64 for precision
- Does not access database, only REST API

**Error Handling:**
- Returns error if REST API call fails
- Returns error if resource content cannot be listed
- Wraps errors with context for debugging

**Usage:**
This method is used by handlers to calculate VP requirements before displaying vault forms to users. It encapsulates the logic of size calculation within the Vault service, keeping handlers clean and focused on request/response handling.

#### Method `GetResource`

Retrieves a resource by ID, returns nil if not found.

**Signature:**
```go
func (s *Vault) GetResource(ctx context.Context, resourceID string) (*vaultModels.Resource, error)
```

**Parameters:**
- `ctx` — request context
- `resourceID` — resource identifier (torrent hash)

**Algorithm:**
1. Get database connection from PG service
2. Call `vaultModels.GetResource` to fetch resource by ID
3. Return resource or nil if not found

**Features:**
- Returns `nil` if resource doesn't exist (not an error)
- Simple wrapper around model method for service layer
- Does not create resource if it doesn't exist (unlike `GetOrCreateResource`)

**Error Handling:**
- Returns error if database connection is unavailable
- Returns error if database query fails (but not for "not found" case)
- Returns `nil, nil` if resource is not found

**Usage:**
This method is used when you need to check if a resource exists without creating it. Useful for conditional logic where resource existence determines the flow.

#### Method `GetPledge`

Retrieves a pledge for a specific user and resource, returns nil if not found.

**Signature:**
```go
func (s *Vault) GetPledge(ctx context.Context, user *auth.User, resource *vaultModels.Resource) (*vaultModels.Pledge, error)
```

**Parameters:**
- `ctx` — request context
- `user` — authenticated user (`*auth.User`)
- `resource` — vault resource (`*vaultModels.Resource`)

**Algorithm:**
1. Get database connection from PG service
2. Call `vaultModels.GetUserResourcePledge` with user ID and resource ID
3. Return pledge or nil if not found

**Features:**
- Returns `nil` if pledge doesn't exist (not an error)
- Finds pledge by combination of user_id and resource_id
- Simple wrapper around model method for service layer

**Error Handling:**
- Returns error if database connection is unavailable
- Returns error if database query fails (but not for "not found" case)
- Returns `nil, nil` if pledge is not found

**Usage:**
This method is used to check if a user has already pledged to a specific resource. Useful for displaying pledge status in UI or preventing duplicate pledges.

#### Method `IsPledgeFrozen`

Checks if a pledge is currently in the freeze period.

**Signature:**
```go
func (s *Vault) IsPledgeFrozen(ctx context.Context, pledge *vaultModels.Pledge) (bool, error)
```

**Parameters:**
- `ctx` — request context
- `pledge` — pledge to check (`*vaultModels.Pledge`)

**Algorithm:**
1. Validate that pledge is not nil
2. Get database connection
3. Fetch the resource associated with the pledge
4. **If resource is vaulted, return `false` immediately** (freeze is lifted when resource is vaulted)
5. Calculate freeze end time: `freezeEndTime = pledge.FrozenAt + s.freezePeriod`
6. Compare current time with freeze end time
7. Return `true` if current time is before freeze end time, `false` otherwise

**Configuration:**
- `VAULT_PLEDGE_FREEZE_PERIOD` / `--vault-pledge-freeze-period` — freeze period duration (default: 24 hours)

**Features:**
- Freeze status is determined dynamically based on time and resource vaulted status, not stored in database
- Freeze period is configurable via environment variable
- Default freeze period is 24 hours
- **Freeze is automatically lifted when the resource is vaulted**, regardless of the time elapsed

**Error Handling:**
- Returns error if pledge is nil
- Returns error if database connection is unavailable
- Returns error if resource cannot be fetched

**Usage:**
This method is used to determine if a pledge can be removed. Pledges cannot be removed during the freeze period to prevent immediate withdrawal after funding. However, once the resource is successfully vaulted, the freeze is lifted and users can remove their pledges.

#### Method `RemovePledge`

Removes a pledge and updates the resource accordingly.

**Signature:**
```go
func (s *Vault) RemovePledge(ctx context.Context, pledge *vaultModels.Pledge) error
```

**Parameters:**
- `ctx` — request context
- `pledge` — pledge to remove (`*vaultModels.Pledge`)

**Algorithm:**
1. Validate that pledge is not nil
2. Execute in database transaction with row locking:
   - **Lock resource row** using `SELECT FOR UPDATE` to prevent concurrent modifications
   - **Delete pledge** using `DeletePledge` model method
   - **Create transaction log entry:**
     - `user_id` = pledge's user ID
     - `resource_id` = pledge's resource ID
     - `balance` = `+pledge.Amount` (positive, as VP is returned)
     - `op_type` = `OpTypeClaim` (3)
   - **Update resource funding:**
     - Calculate `newFundedVP = max(0, resource.FundedVP - pledge.Amount)`
     - Update `resource.funded_vp` to `newFundedVP` using `UpdateResourceFundedVP`
     - If `newFundedVP < resource.RequiredVP`:
       - Mark resource as expired using `MarkResourceExpired`
       - Set `resource.expired = true` and `resource.expired_at = now()`
       - Mark resource as unfunded using `MarkResourceUnfunded`
       - Set `resource.funded = false` and `resource.funded_at = NULL`
3. Return nil on success or error on failure

**Features:**
- Uses `SELECT FOR UPDATE` to prevent race conditions during concurrent pledge removal
- Transaction log entry is created with positive balance (OpTypeClaim)
- Resource is automatically marked as expired and unfunded if funding drops below required amount
- All operations are atomic within a single database transaction
- Protects against negative funded_vp values

**Error Handling:**
- Returns error if pledge is nil
- Returns error if database connection is unavailable
- Returns error if resource cannot be locked
- Returns error if pledge deletion fails
- Returns error if transaction log creation fails
- Returns error if resource update fails
- Returns error if transaction fails at any step

**Usage:**
This method is called by the vault handler when a user removes their pledge. It ensures that the resource state is correctly updated and the user's VP is returned.

#### Method `defundPledge` (internal)

Removes funding from a pledge and updates the resource accordingly. This is an internal method used during pledge funding recalculation.

**Signature:**
```go
func (s *Vault) defundPledge(ctx context.Context, tx *pg.Tx, pledge *vaultModels.Pledge, resource *vaultModels.Resource) error
```

**Parameters:**
- `ctx` — request context
- `tx` — database transaction
- `pledge` — pledge to defund
- `resource` — resource associated with the pledge

**Algorithm:**
1. Set `funded = false` for the pledge using `UpdatePledgeFunded`
2. Decrease `resource.funded_vp` by `pledge.Amount` using `AdjustResourceFundedVP`
3. Update local resource state: `resource.FundedVP -= pledge.Amount`
4. If `resource.FundedVP < resource.RequiredVP` AND `resource.Expired = false`:
   - Mark resource as expired and unfunded using `MarkResourceExpiredAndUnfunded`
   - Set `resource.expired = true`, `resource.expired_at = now()`, `resource.funded_at = NULL`
   - Update local state: `resource.Expired = true`, `resource.Funded = false`

**Features:**
- Operates within an existing transaction
- Updates both database and local resource state for consistency
- Automatically marks resource as expired when funding drops below required amount
- Does not create transaction log entries (handled by parent operation)

#### Method `putResourceToVaultAPI` (internal)

Checks resource status in Vault API and adds it if missing. Returns whether the resource is already vaulted (completed). This method is called automatically when a resource transitions to Funded state.

**Signature:**
```go
func (s *Vault) putResourceToVaultAPI(ctx context.Context, tx *pg.Tx, resource *vaultModels.Resource) (bool, error)
```

**Parameters:**
- `ctx` — request context
- `tx` — database transaction
- `resource` — resource to check/add in Vault API

**Returns:**
- `bool` — `true` if resource exists in Vault API with `StatusCompleted`, `false` otherwise
- `error` — error if Vault API call fails

**Algorithm:**
1. Skip if Vault API is not configured (`s.vaultApi == nil`) — return `false, nil`
2. Get resource status from Vault API using `GetResource`
3. If resource exists in Vault API and has `StatusCompleted`:
   - Return `true, nil` (resource is vaulted)
4. If resource doesn't exist in Vault API:
   - Add resource to Vault API using `PutResource`
   - Return `false, nil` (resource added but not yet completed)
5. If resource exists but not completed:
   - Return `false, nil` (resource is processing)

**Features:**
- Operates within an existing transaction
- Gracefully handles missing Vault API configuration
- Automatically adds resources to Vault API if they don't exist
- Does not update database directly — caller is responsible for updating `vaulted` status based on return value
- Does not interfere with recalculate mechanics

**Important Notes:**
- This method is called when resource transitions to Funded state (in `CreatePledge`)
- Caller must call `UpdateResourceVaulted` if this method returns `true`
- Does not affect the recalculate mechanism

#### Method `fundPledge` (internal)

Adds funding to a pledge and updates the resource accordingly. This is an internal method used during pledge funding recalculation.

**Signature:**
```go
func (s *Vault) fundPledge(ctx context.Context, tx *pg.Tx, pledge *vaultModels.Pledge, resource *vaultModels.Resource) error
```

**Parameters:**
- `ctx` — request context
- `tx` — database transaction
- `pledge` — pledge to fund
- `resource` — resource associated with the pledge

**Algorithm:**
1. Set `funded = true` for the pledge using `UpdatePledgeFunded`
2. Increase `resource.funded_vp` by `pledge.Amount` using `AdjustResourceFundedVP`
3. Update local resource state: `resource.FundedVP += pledge.Amount`
4. If `resource.FundedVP >= resource.RequiredVP` AND `resource.Expired = true`:
   - Mark resource as unexpired and funded using `MarkResourceUnexpired`
   - Set `resource.expired = false`, `resource.expired_at = NULL`, `resource.funded = true`, `resource.funded_at = now()`
   - Update local state: `resource.Expired = false`, `resource.Funded = true`

**Features:**
- Operates within an existing transaction
- Updates both database and local resource state for consistency
- Automatically marks resource as funded when funding reaches required amount
- Does not create transaction log entries (handled by parent operation)

#### Method `recalculatePledgeFunding` (internal)

Recalculates which pledges should be funded based on user's total points. This method is called automatically when user's total points change.

**Signature:**
```go
func (s *Vault) recalculatePledgeFunding(ctx context.Context, tx *pg.Tx, user *auth.User, total *float64) error
```

**Parameters:**
- `ctx` — request context
- `tx` — database transaction
- `user` — authenticated user
- `total` — user's total vault points (nil if unlimited)

**Algorithm:**
1. If `total = nil` (unlimited), return immediately (no recalculation needed)
2. Get all user pledges ordered by creation time (ascending) using `GetUserPledgesOrderedByCreation`
3. Initialize `accumulatedAmount = 0`
4. For each pledge in order:
   - Fetch and lock the resource using `SELECT FOR UPDATE`
   - If `accumulatedAmount + pledge.Amount <= total`:
     - If `pledge.Funded = false`: call `fundPledge` to activate the pledge
     - Add `pledge.Amount` to `accumulatedAmount`
   - Else (accumulated would exceed total):
     - If `pledge.Funded = true`: call `defundPledge` to deactivate the pledge
     - Do not add to `accumulatedAmount`
5. Return nil on success or error on failure

**Features:**
- Operates within an existing transaction (part of UpdateUserVP transaction)
- Processes pledges in creation order (oldest first) to ensure fairness
- Locks resources with `SELECT FOR UPDATE` to prevent concurrent modifications
- Automatically funds/defunds pledges based on available points
- Skips pledges for non-existent resources

**Example Scenario:**

User has 3 pledges (created in order):
1. Pledge A: 10 VP (created first)
2. Pledge B: 15 VP (created second)
3. Pledge C: 20 VP (created third)

**Case 1: User has 30 VP total**
- Accumulated: 0 → 10 (fund A) → 25 (fund B) → 45 > 30 (defund C)
- Result: A and B are funded, C is defunded

**Case 2: User downgrades to 20 VP total**
- Accumulated: 0 → 10 (A stays funded) → 25 > 20 (defund B) → (defund C)
- Result: Only A is funded, B and C are defunded

**Case 3: User upgrades to 50 VP total**
- Accumulated: 0 → 10 (A stays funded) → 25 (fund B) → 45 (fund C)
- Result: All pledges are funded

**Error Handling:**
- Returns error if fetching pledges fails
- Returns error if locking resource fails
- Returns error if funding/defunding operation fails
- Skips pledges for non-existent resources (logs warning)

## Processes and Use Cases

### 1. Creating a Pledge (Fund)

**Preconditions:**
- User is authenticated
- User has sufficient available VP (`Available >= amount`)

**Steps:**
1. Check user balance via `GetUserStats`
2. Verify that `Available >= amount`
3. In a transaction:
   - Create record in `pledge` with `funded = true`, `frozen_at = now()`
   - Create record in `tx_log` with `op_type = OpTypeFund`, `balance = -amount`
   - Update `funded_vp` in `resource` (sum of all active pledges)
   - If `funded_vp >= required_vp`, set `funded = true`, `funded_at = now()`
4. If resource is funded — initiate vaulting process
5. Pledge remains frozen after creation for a period configurable via `VAULT_PLEDGE_FREEZE_PERIOD` (default: 24 hours)
   - Freeze is determined by `frozen_at + freeze_period`
   - **Freeze is automatically lifted when resource is vaulted**, regardless of time elapsed

**Failure Handling:**
- If vaulting fails within **7 days**, VP is returned to the user (pledge is removed automatically)

### 2. Removing a Pledge (Remove)

**Preconditions:**
- User is authenticated
- Pledge belongs to the user
- Pledge is not frozen (`IsPledgeFrozen(ctx, pledge) = false`)
  - Pledge is unfrozen if: resource is vaulted OR freeze period has expired
- Pledge is active (`funded = true`)

**Steps:**
1. Check access rights and freeze status via `IsPledgeFrozen`
   - Method checks if resource is vaulted first (if yes, pledge is not frozen)
   - If resource is not vaulted, checks if freeze period has expired
2. If pledge is frozen, return error "pledge is frozen and cannot be removed"
3. In a transaction:
   - Delete pledge record from database
   - Create record in `tx_log` with `op_type = OpTypeClaim`, `balance = +amount`
   - Update `funded_vp` in `resource` (subtract pledge amount, ensure >= 0)
   - If `funded_vp < required_vp`:
     - Set `expired = true`, `expired_at = now()`
     - Set `funded = false`, `funded_at = NULL`
4. Return points to user

**Consequences:**
- If after removal the resource becomes underfunded (`funded_vp < required_vp`), it is marked as expired and unfunded
- Expired resources are automatically deleted after **7 days**
- Pledge is permanently deleted from database (cannot be restored)

### 3. Vaulting a Resource (Vault)

**Preconditions:**
- Resource is funded (`funded = true`)
- Resource is not yet vaulted (`vaulted = false`)

**Steps:**
1. Perform operation to place resource in long-term storage
2. Set `vaulted = true`, `vaulted_at = now()` in `resource`

**Effects:**
- **All pledges for this resource are automatically unfrozen** when the resource becomes vaulted
- Users can now remove their pledges and reclaim their VP, even if the freeze period has not expired
- Freeze status is determined dynamically via `IsPledgeFrozen` method, which checks the resource's vaulted status first

### 4. User Tier Change

**Preconditions:**
- User changed subscription/tier
- Claims system updated VP information

**Steps:**
1. On next access, `UpdateUserVP` is called (typically via middleware or handler)
2. Method automatically:
   - Gets new balance from claims (`Claims.Vault.Points`)
   - Compares with current balance in database
   - If balance differs:
     - Calculates difference (treating NULL as 0 for calculation)
     - Updates balance in DB
     - If new value is not 0 (not free tier), logs change in tx_log with the difference
     - If new value is 0 (free tier), no tx_log entry is created
   - If balance matches or both are NULL:
     - No changes made, returns existing record

**Special Cases:**
- **Unlimited → Unlimited (NULL → NULL)**: No tx_log entry created
- **Free → Free (0 → 0)**: No tx_log entry created
- **Unlimited → Free (NULL → 0)**: No tx_log entry created (free tier)
- **Free → Unlimited (0 → NULL)**: No tx_log entry created (unlimited tier)
- **Unlimited → Limited (NULL → value > 0)**: Treats NULL as 0, logs positive difference
- **Limited → Unlimited (value → NULL)**: Treats NULL as 0, logs negative difference
- **Free → Limited (0 → value > 0)**: Logs positive difference
- **Limited → Free (value → 0)**: No tx_log entry created (free tier)
- **Limited → Limited (value → value, both > 0)**: Logs the difference (can be positive or negative)

### 6. Subscription Expiration

**What happens when user's subscription expires:**

**Immediate Effects:**
- User's VP balance is updated to reflect the new (lower or zero) tier via `UpdateUserVP`
- Points that were invested in resources lose their power (become inactive)
- All resources funded by this user are checked against new balance

**Resource Expiration:**
1. When subscription expires and VP balance drops below funded amount:
   - All resources where user's pledges contribute to funding are re-evaluated
   - If `funded_vp < required_vp` for any resource, it is marked as `expired = true`, `expired_at = now()`
   - Expired resources are automatically deleted after **7 days**

**Subscription Renewal:**
- If subscription is renewed before the 7-day deletion period:
  - VP balance is restored via `UpdateUserVP`
  - Points become active again
  - Resources funded by these points are no longer expired (`expired = false`)
  - Resources remain in vault and continue to be available

**Important Notes:**
- The 7-day grace period allows users to renew their subscription without losing their vaulted content
- If subscription is not renewed within 7 days, all expired resources are permanently deleted

## Constraints and Rules

### Business Rules

1. **User Balance:**
   - Cannot be negative (checked at application level)
   - NULL means unlimited balance
   - Synchronized with claims system on every access

2. **Pledges:**
   - Cannot create a pledge larger than available balance
   - Cannot remove a frozen pledge (freeze period has not expired and resource is not vaulted)
   - Sum of all active pledges cannot exceed total (if not NULL)
   - Pledges are frozen after creation for a period configurable via `VAULT_PLEDGE_FREEZE_PERIOD` (default: 24 hours)
   - Freeze status is determined dynamically by checking:
     1. If resource is vaulted → pledge is NOT frozen (freeze is lifted)
     2. Otherwise, compare `frozen_at + freeze_period` with current time
   - Each user can have only one pledge per resource (enforced by unique index)

3. **Resources:**
   - Resource becomes funded when `funded_vp >= required_vp`
   - Resource can expire if sum of active pledges drops below required_vp
   - Expired resources are deleted after **7 days**
   - Vaulted resource cannot be deleted (unless expired)
   - **When a resource is deleted, all pledges associated with it are also deleted**
   - **Important limitation**: If a torrent is blocked by copyright holders or removed from public sources, it may disappear from the vault. The Vault system does not guarantee preservation in such cases, as it depends on the availability of the original torrent data.

4. **Transactions:**
   - All balance operations must be logged in tx_log
   - Balance in tx_log cannot be zero
   - OpTypeFund always has negative balance
   - OpTypeClaim always has positive balance
   - OpTypeChangeTier can have any sign

5. **Vaulting Process:**
   - If vaulting fails within **7 days** of funding, VP is automatically returned to users
   - Successful vaulting sets `vaulted = true` and `vaulted_at = now()`

### Technical Constraints

1. **Concurrency:**
   - Uses `SELECT FOR UPDATE` to prevent race conditions
   - All balance-changing operations are executed in transactions

2. **Performance:**
   - Indexes on foreign keys (user_id, resource_id)
   - Statistics calculated in application, not in DB
   - Application-level caching is used (if configured)

3. **Data Integrity:**
   - Cascade delete when user is deleted
   - Triggers for automatic updated_at updates
   - Foreign keys to ensure referential integrity

## Migrations

The system uses 6 migrations to create and configure the schema:

1. **24_create_vault_schema** — creates `vault` schema
2. **25_create_user_vp** — creates `user_vp` table with `total` as NOT NULL
3. **26_create_pledge** — creates `pledge` table
4. **27_create_resource** — creates `resource` table
5. **28_create_tx_log** — creates `tx_log` table
6. **29_alter_user_vp_total_nullable** — alters `user_vp.total` to allow NULL values

All table creation migrations include:
- Table creation with correct types and constraints
- Primary key creation
- Foreign key creation (where applicable)
- `update_updated_at` trigger creation for automatic updated_at field updates

Down migrations simply drop the corresponding tables or revert schema changes:
```sql
DROP TABLE IF EXISTS vault.table_name;
-- or
ALTER TABLE vault.user_vp ALTER COLUMN total SET NOT NULL;
```

## Integration with Other Systems

### Claims Service

The Vault system integrates with Claims Service to get user subscription information:

- On every call to `UpdateUserVP` or `GetUserStats`, synchronization with claims occurs
- Claims returns the VP amount for the user's current tier
- NULL in claims means unlimited access
- Changes in claims are automatically reflected in vault on next access

### Storage Service (future integration)

Integration with long-term storage service is planned:

- When `funded = true` is reached, resource is queued for vaulting
- After successful vaulting, `vaulted = true` is set
- Storage notifies vault about the need to unfreeze pledges

## HTTP API Endpoints

The Vault system provides HTTP endpoints for user interactions with pledges.

### Vault Handler (`handlers/vault/handler.go`)

Handler for managing vault pledges through HTTP requests.

**Registration:**
```go
func RegisterHandler(r *gin.Engine, v *vault.Vault, tm *template.Manager[*web.Context], pg *cs.PG)
```

The handler is registered only if the vault service is not nil. This is checked in `serve.go`:
```go
if v != nil {
    vh.RegisterHandler(r, v, tm, pg)
}
```

**Dependencies:**
- Vault service (`services/vault`) — for business logic operations
- Template Manager (`services/template`) — for rendering HTML views
- PostgreSQL (`common-services/PG`) — for database access
- Auth service (`services/auth`) — for user authentication
- API service (`services/api`) — for getting API claims

**File Structure:**
The handler is organized into multiple files following the library handler pattern:
- `handler.go` — contains `Handler` struct, `PledgeDisplay` struct, `PledgeListData` struct, and `RegisterHandler` function
- `index.go` — contains `index` and `getPledgesList` methods for displaying pledges list
- `add.go` — contains `addPledge` and `createPledge` methods for creating pledges
- `remove.go` — contains `removePledge` and `deletePledge` methods for removing pledges

**Data Structures:**
```go
type PledgeDisplay struct {
	PledgeID   string                // Pledge UUID as string
	ResourceID string                // Resource identifier (torrent hash)
	Resource   *vaultModels.Resource // Related resource with name and metadata
	Amount     float64               // Pledged vault points amount
	IsFrozen   bool                  // Computed frozen status using IsPledgeFrozen
	Funded     bool                  // Pledge funded status from database
	CreatedAt  string                // Formatted creation timestamp
}

type PledgeListData struct {
	Pledges               []PledgeDisplay // Array of pledges for display
	FreezePeriod          time.Duration   // Pledge freeze period from vault service
	ExpirePeriod          time.Duration   // Resource expire period from vault service
	TransferTimeoutPeriod time.Duration   // Resource transfer timeout period from vault service
}
```

The `PledgeDisplay` struct is used to present pledges in the UI with computed `IsFrozen` status and `Funded` status from database. The `IsFrozen` field is calculated using `vault.IsPledgeFrozen()` method which checks both the freeze period and whether the resource is vaulted. The `Funded` field indicates whether the pledge is funded (true) or expiring (false).

**Routes:**
- `GET /vault/pledge` — display user's pledges list
- `POST /vault/pledge/add` — create a new pledge
- `POST /vault/pledge/remove` — remove an existing pledge

### POST /vault/pledge/add

Creates a new pledge for a resource.

**Authentication:** Required (checks `auth.GetUserFromContext` and `user.HasAuth()`)

**Form Parameters:**
- `resource_id` (required) — resource identifier (torrent hash)

**Request Headers:**
- `X-Return-Url` — URL to redirect after operation (required for redirect)

**Algorithm:**
1. Get current user from context using `auth.GetUserFromContext`
2. Verify user is authenticated (`user.HasAuth()`)
3. Get `resource_id` from form data
4. Get API claims from context using `api.GetClaimsFromContext`
5. Call `vault.GetOrCreateResource` to ensure resource exists
6. Call `vault.CreatePledge` to create the pledge
7. On success: redirect to `X-Return-Url` with query parameters `status=success` and `from=/vault/pledge/add`
8. On error: redirect to `X-Return-Url` with query parameters `err=<error_message>` and `from=/vault/pledge/add`

**Success Response:**
- HTTP 302 redirect to `X-Return-Url?status=success&from=/vault/pledge/add`

**Error Responses:**
- HTTP 302 redirect to `X-Return-Url?err=unauthorized&from=/vault/pledge/add` — user not authenticated
- HTTP 302 redirect to `X-Return-Url?err=resource_id+is+required&from=/vault/pledge/add` — missing resource_id
- HTTP 302 redirect to `X-Return-Url?err=failed+to+get+claims&from=/vault/pledge/add` — claims unavailable
- HTTP 302 redirect to `X-Return-Url?err=<error>&from=/vault/pledge/add` — other errors (insufficient VP, database errors, etc.)

**Usage Example:**
```html
<form method="post" action="/vault/pledge/add">
    <input type="hidden" name="resource_id" value="abc123..." />
    <button type="submit">Create Pledge</button>
</form>
```

**Notes:**
- Uses `web.RedirectWithSuccess(c)` helper for success redirects
- Uses `web.RedirectWithError(c, err)` helper for error redirects
- Both helpers automatically add `from` parameter with current URL path
- Follows server-side rendering pattern with form submissions

### POST /vault/pledge/remove

Removes a user's pledge for a resource and returns VP to their account.

**Authentication:** Required (uses `auth.HasAuth` middleware)

**Form Parameters:**
- `resource_id` (required) — resource identifier (torrent hash)

**Request Headers:**
- `X-Return-Url` — URL to redirect after operation (required for redirect)

**Algorithm:**
1. Get current user from context using `auth.GetUserFromContext`
2. Get `resource_id` from form data
3. Validate `resource_id` is not empty
4. Get vault resource using `vault.GetResource`
5. Get user's pledge for this resource using `vault.GetPledge`
6. Check if pledge is frozen using `vault.IsPledgeFrozen`
7. If pledge is frozen, return error "pledge is frozen and cannot be removed"
8. Call `vault.RemovePledge` to remove the pledge (in transaction):
   - Delete pledge from database
   - Create tx_log entry with OpTypeClaim (positive balance)
   - Update resource funded_vp
   - Mark resource as expired/unfunded if needed
9. On success: redirect to `X-Return-Url` with query parameters `status=success` and `from=/vault/pledge/remove`
10. On error: redirect to `X-Return-Url` with query parameters `status=error`, `err=<error_message>` and `from=/vault/pledge/remove`

**Success Response:**
- HTTP 302 redirect to `X-Return-Url?status=success&from=/vault/pledge/remove`

**Error Responses:**
- HTTP 302 redirect to `X-Return-Url?status=error&err=resource_id+is+required&from=/vault/pledge/remove` — missing resource_id
- HTTP 302 redirect to `X-Return-Url?status=error&err=resource+not+found&from=/vault/pledge/remove` — resource doesn't exist
- HTTP 302 redirect to `X-Return-Url?status=error&err=pledge+not+found&from=/vault/pledge/remove` — pledge doesn't exist
- HTTP 302 redirect to `X-Return-Url?status=error&err=pledge+is+frozen+and+cannot+be+removed&from=/vault/pledge/remove` — pledge is still frozen
- HTTP 302 redirect to `X-Return-Url?status=error&err=<error>&from=/vault/pledge/remove` — other errors (database errors, etc.)

**Usage Example:**
```html
<form method="post" action="/vault/pledge/remove">
    <input type="hidden" name="resource_id" value="abc123..." />
    <button type="submit">Remove Pledge</button>
</form>
```

**Notes:**
- Uses `web.RedirectWithSuccess(c)` helper for success redirects
- Uses `web.RedirectWithError(c, err)` helper for error redirects
- Both helpers automatically add `from` parameter with current URL path
- Follows server-side rendering pattern with form submissions
- Pledge removal is permanent and cannot be undone
- VP is immediately returned to user's available balance

### GET /vault/pledge

Displays a list of user's pledges with resource information.

**Authentication:** Required (uses `auth.HasAuth` middleware)

**Query Parameters:** None

**Algorithm:**
1. Get current user from context using `auth.GetUserFromContext`
2. Call business logic method `getPledgesList` with user ID
3. In `getPledgesList`:
   - Get database connection from PG service
   - Call `vaultModels.GetUserPledgesWithResources` to fetch pledges with resource information
   - For each pledge, compute `IsFrozen` status using `vault.IsPledgeFrozen()` method
   - Convert pledges to `PledgeDisplay` format with computed frozen status
   - Return array of `PledgeDisplay` sorted by creation time (newest first)
4. Prepare `PledgeListData` structure with display pledges array
5. Render `vault/pledge/index` template with data

**Success Response:**
- HTTP 200 with rendered HTML page showing pledges list

**Error Responses:**
- HTTP 500 — database connection error or query failure

**Template Data Structure:**
```go
type PledgeListData struct {
    Pledges               []PledgeDisplay
    FreezePeriod          time.Duration
    ExpirePeriod          time.Duration
    TransferTimeoutPeriod time.Duration
}
```

The `PledgeListData` includes:
- `Pledges` — array of pledges for display
- `FreezePeriod` — pledge freeze period from vault service (used in status annotation)
- `ExpirePeriod` — resource expire period from vault service
- `TransferTimeoutPeriod` — resource transfer timeout period from vault service

Each `PledgeDisplay` includes:
- `PledgeID` — unique pledge identifier (UUID as string)
- `ResourceID` — resource identifier (torrent hash)
- `Amount` — pledged vault points amount
- `IsFrozen` — computed frozen status using `vault.IsPledgeFrozen()`
- `Funded` — pledge funded status from database (true = funded, false = expiring)
- `CreatedAt` — formatted pledge creation timestamp
- `Resource` — related resource information (loaded via relation):
  - `Name` — torrent name (string, empty if not available)
  - `ResourceID` — resource identifier

**Template:** `templates/views/vault/pledge/index.html`

**UI Features:**
- Page title: "Vault Pledges"
- Table with columns:
  - **Torrent** — torrent name (or resource ID if name is empty) as link to resource page with `data-async-target="main"`
  - **Points** — pledged amount formatted as `%.2f`
  - **Status** — badge showing pledge status:
    - "Frozen" (green badge, `badge-success`) if `IsFrozen = true`
    - "Expiring" (red badge, `badge-error`) if `Funded = false`
    - "Claimable" (yellow badge, `badge-warning`) if `IsFrozen = false` and `Funded = true`
- Pledges sorted by creation time (newest first)
- Status annotation box (below table, using Tailwind Description List `<dl>`):
  - **Frozen** (bold term) — points become frozen immediately after pledge and unfreeze after the torrent is transferred to Vault and the freeze period expires (displays actual freeze period value)
  - **Expiring** (bold term) — pledge is no longer backed by points (happens when subscription expires or tier is downgraded). Content with such pledges may be removed from Vault
  - **Claimable** (bold term) — points that can be claimed back. After claiming, the content they were pledged to may be removed
- "Back To Profile" button (accent, outline, centered) linking to `/profile` with `data-async-target="main"` (below annotation box)
- "What is Vault?" link (below "Back To Profile" button) — links to `/instructions/vault` with Umami tracking (`data-umami-event="instruction-vault"`)
- Empty state: "No pledges yet" message if user has no pledges
- Styling consistent with Vault profile section (table in `bg-base-100 rounded-lg shadow p-6`)

**Usage Example:**
```html
<a href="/vault/pledge" class="btn btn-accent btn-outline" data-async-target="main">Pledges</a>
```

**Notes:**
- Follows two-level handler architecture: `index` (HTTP layer) and `getPledgesList` (business logic)
- Uses server-side rendering with template builder
- Integrates with async navigation system via `data-async-target="main"`
- Accessible from Vault section in user profile via "Pledges" button

## UI Components

The Vault system includes UI components for user interaction with the vault functionality on resource pages.

### Vault Button (`templates/partials/vault/button.html`)

A button component that allows authenticated users to initiate the vault pledge process.

**Location:** Displayed on resource pages (torrents) for authenticated users when vault service is available.

**Behavior:**
- Renders only if user is authenticated (`hasAuth`) and vault service is available (`.Data.Vault = true`)
- Displays different button text based on pledge status:
  - "Keep This Torrent Available" — if user has no pledge for this resource
  - "Remove Pledge" — if user has an active pledge for this resource
- Submits a GET form asynchronously to the current URL with parameter `pledge-add-form=true` or `pledge-remove-form=true`
- Uses `data-async-target` and `data-async-push-state="false"` for progressive enhancement
- Includes Umami analytics tracking with `data-umami-event="vault-clicked"` or `data-umami-event="vault-remove-clicked"`

**Template Structure:**
```html
{{ define "vault/button" }}
	{{ if and (.User | hasAuth) .Data.Vault }}
		{{ if .Data.VaultButton.Funded }}
			<form method="get" data-async-target="#pledge-remove-form" data-async-push-state="false">
				<input type="hidden" name="pledge-remove-form" value="true" />
				<button type="submit" class="btn btn-accent btn-outline btn-soft" data-umami-event="vault-remove-clicked">
					Remove Pledge
				</button>
			</form>
		{{ else }}
			<form method="get" data-async-target="#pledge-add-form" data-async-push-state="false">
				<input type="hidden" name="pledge-add-form" value="true" />
				<button type="submit" class="btn btn-accent btn-outline" data-umami-event="vault-clicked">
					Keep This Torrent Available
				</button>
			</form>
		{{ end }}
	{{ end }}
{{ end }}
```

**Associated JavaScript:** `assets/src/js/app/vault/button.js` — handles form submission and modal interaction.

### Vault Pledge Add Modal (`templates/partials/vault/pledge-add-modal.html`)

A modal dialog that displays vault points information and allows users to confirm or cancel the pledge.

**Location:** Rendered on resource pages, opens when user clicks the vault button.

**Behavior:**
- Renders only if user is authenticated and vault service is available
- Opens automatically when `VaultPledgeAddForm` data is present (when `pledge-add-form=true` parameter is processed)
- Uses DaisyUI modal component with `open` attribute for automatic display
- Wrapped in `<div id="pledge-add-form">` with `data-async-layout` for dynamic updates
- Displays different states: default form, success message, error message, funded status, vaulted status

**Display Logic:**

1. **Sufficient Points (Available >= RequiredVP or Unlimited):**
   - Message: "You have enough Vault Points to store this torrent in the Vault."
   - Shows "Store in Vault" button
   - Shows "Cancel" button to close modal

2. **Insufficient Points (Available < RequiredVP):**
   - Message: "You don't have enough Vault Points to store this torrent."
   - Suggests upgrading subscription with link to `/donate` (opens in new tab)
   - Shows only "Cancel" button (no "Store in Vault" button)

3. **Success Status (Status = "success"):**
   - **Fully Funded (`Funded` = true):**
     - Message: "Your pledge has been successfully registered and your torrent will be saved in the Vault soon."
     - Umami event: `vault-pledge-confirmed` on "Got it!" button
   - **Not Funded (`Funded` = false):**
     - Message: "Unfortunately, you don't have enough Vault Points to continue supporting this resource, and it may disappear. Try renewing your subscription:"
     - Shows link to `/donate` with text "Upgrade subscription" (Umami event: `vault-upgrade-clicked`)
     - Umami event: `vault-pledge-confirmed` on "Got it!" button

4. **Vaulted Status (`Vaulted` = true):**
   - Message: "Your torrent is already in the Vault and will be kept available."
   - Umami event: `vault-pledge-confirmed` on "Got it!" button

5. **Error Status (Status = "error"):**
   - Message: "Unfortunately, your pledge could not be saved due to an error:" followed by error details
   - Shows only "Ok" button to close modal
   - Umami event: `vault-pledge-error-confirmed`

**Information Table:**
- Link to `/instructions/vault` with text "What is Vault?" (opens in new tab)
- Umami analytics tracking:
  - `data-umami-event="vault-store-confirmed"` on "Store in Vault" button
  - `data-umami-event="vault-cancelled"` on "Cancel" button
  - `data-umami-event="vault-upgrade-clicked"` on "Upgrade subscription" link
  - `data-umami-event="instruction-vault"` on "What is Vault?" link

### Vault Pledge Remove Modal (`templates/partials/vault/pledge-remove-modal.html`)

A modal dialog that allows users to remove their pledge and return VP to their account.

**Location:** Rendered on resource pages, opens when user clicks the "Remove Pledge" button.

**Behavior:**
- Renders only if user is authenticated and vault service is available
- Opens automatically when `VaultPledgeRemoveForm` data is present (when `pledge-remove-form=true` parameter is processed)
- Uses DaisyUI modal component with `open` attribute for automatic display
- Wrapped in `<div id="pledge-remove-form">` with `data-async-layout` for dynamic updates
- Displays different states: frozen warning, confirmation form, success message, error message

**Display Logic:**

1. **Frozen Status (Frozen = true):**
   - Message: "Your Vault Points are currently frozen and cannot be removed yet. Please come back later when the freeze period expires."
   - Shows only "Got it!" button to close modal
   - Umami event: `vault-pledge-frozen-acknowledged`

2. **Success Status (Status = "success"):**
   - Message: "Your pledge has been successfully removed and your Vault Points have been returned to your account. The torrent may be removed from the Vault if it's no longer funded. You can always pledge your points again if needed."
   - Shows only "Got it!" button to close modal
   - Umami event: `vault-pledge-remove-success-confirmed`

3. **Error Status (Status = "error"):**
   - Message: "Unfortunately, your pledge could not be removed due to an error:" followed by error details
   - Shows only "Ok" button to close modal
   - Umami event: `vault-pledge-remove-error-confirmed`

4. **Default Confirmation (no status):**
   - Message: "Are you sure you want to remove your pledge for this torrent? This action cannot be undone."
   - Shows "Remove Pledge" button (submits POST to `/vault/pledge/remove`)
   - Shows "Cancel" button with `btn-soft` class to close modal
   - Umami events: `vault-pledge-remove-confirmed`, `vault-pledge-remove-cancelled`

**Additional Elements:**
- Form uses `data-async-target="#pledge-remove-form"` and `data-async-push-state="false"` for progressive enhancement
- Resource ID is passed as hidden input field from `.Resource.ID`

### Handler Integration (`handlers/resource/get.go`)

The resource GET handler integrates vault functionality to display modals with calculated VP requirements and pledge status.

**VaultPledgeAddForm Structure:**
```go
type VaultPledgeAddForm struct {
	Available     *float64 // User's available VP (nil if unlimited)
	Total         *float64 // User's total VP (nil if unlimited)
	Required      float64  // Required VP for this resource
	TorrentSizeGB float64  // Torrent size in GB
}
```

**VaultPledgeRemoveForm Structure:**
```go
type VaultPledgeRemoveForm struct {
	Frozen bool   // True if pledge is frozen
	Status string // "success", "error", or empty
	Err    error  // Error message if Status = "error"
}
```

**VaultButton Structure:**
```go
type VaultButton struct {
	Funded bool // True if user has active pledge for this resource
}
```

**GetData Structure:**
```go
type GetData struct {
	Args              *GetArgs
	Resource          *ExtendedResource
	List              *ra.ListResponse
	Item              *ra.ListItem
	Instruction       string
	VaultForm         *VaultPledgeAddForm       // Present when pledge-add-form=true
	VaultButton       *VaultButton              // Present when user is authenticated
	VaultPledgeRemove *VaultPledgeRemoveForm    // Present when pledge-remove-form=true
	Vault             bool                      // True if vault service is available
}
```

**Handler Logic:**

1. **Vault Availability Check:**
   - Sets `d.Vault = s.vault != nil` to indicate if vault service is configured
   - This flag controls visibility of vault button and modals in templates

2. **Vault Button State (when user is authenticated):**
   - Calls `prepareVaultButton` to determine button state:
     - Gets vault resource via `s.vault.GetResource(ctx, args.ID)`
     - Gets user's pledge via `s.vault.GetPledge(ctx, args.User, resource)`
     - Sets `Funded = true` if both resource and pledge are funded
   - Sets `d.VaultButton` which controls button text and target modal

3. **Pledge Add Form Processing (when `pledge-add-form=true` or `from=/vault/pledge/add`):**
   - Checks if vault service is available (`s.vault != nil`)
   - Checks if user is authenticated (`args.User.HasAuth()`)
   - Calls `prepareVaultPledgeAddForm` to calculate VP requirements:
     - Gets user vault stats via `s.vault.GetUserStats(ctx, args.User)`
     - Gets required VP via `s.vault.GetRequiredVP(ctx, args.Claims, args.ID)` (encapsulated in Vault service)
     - Gets torrent size separately via REST API `api.ListResourceContentCached` with `Output: api.OutputList`
     - Converts total size to GB: `torrentSizeGB = totalSize / (1024 * 1024 * 1024)`
     - Checks resource and pledge status (Funded, Vaulted)
     - Handles redirect status from `/vault/pledge/add` (success/error)
     - Returns `VaultPledgeAddForm` with Available, Total, Required, TorrentSizeGB, Status, Err, Funded, Vaulted
   - Sets `d.VaultForm` which triggers modal display in template

4. **Pledge Remove Form Processing (when `pledge-remove-form=true` or `from=/vault/pledge/remove`):**
   - Checks if vault service is available (`s.vault != nil`)
   - Checks if user is authenticated (`args.User.HasAuth()`)
   - Calls `prepareVaultPledgeRemoveForm` to check pledge status:
     - Handles redirect status from `/vault/pledge/remove` (success/error)
     - If not redirect, gets vault resource and user's pledge
     - Checks if pledge is frozen via `s.vault.IsPledgeFrozen(pledge)`
     - Returns `VaultPledgeRemoveForm` with Frozen, Status, Err
   - Sets `d.VaultPledgeRemove` which triggers modal display in template

**VP and Size Calculation:**
- **Required VP**: Calculated by Vault service method `GetRequiredVP`, which internally fetches resource content and converts size to VP (1 VP = 1 GB)
- **Torrent Size GB**: Calculated separately in handler by fetching resource content via REST API and converting bytes to GB
- Both calculations use the same source data (REST API), but are performed independently for clarity and separation of concerns

**Error Handling:**
- Returns 500 error if vault stats cannot be fetched
- Returns 500 error if resource content cannot be listed
- Wraps errors with context for debugging

### Template Integration (`templates/views/resource/get.html`)

The resource view template includes both vault components:

```html
{{ template "vault/button" $ }}
...
{{ template "vault/modal" . }}
```

**Rendering Order:**
1. Vault button is displayed in the main content area (before file list)
2. Vault modals are rendered at the end of the template (after all content)
3. Pledge add modal opens automatically when `VaultPledgeAddForm` is present in data
4. Pledge remove modal opens automatically when `VaultPledgeRemoveForm` is present in data

**Progressive Enhancement:**
- Button works without JavaScript (submits GET form)
- With JavaScript, form submission is asynchronous and updates only the modal area
- Modal can be closed by clicking backdrop or Cancel button

## CLI Commands

### Vault Reap Command

The `vault reap` command (alias: `v r`) is a maintenance command that removes expired vault resources and their associated pledges.

**Usage:**
```bash
./web-ui vault reap [options]
# or using alias
./web-ui v r [options]
```

**What it does:**

1. Selects all vault resources that meet one of the following conditions:
   - `expired_at < now - VAULT_RESOURCE_EXPIRE_PERIOD` (default: 7 days) - resources marked as expired
   - `funded_at < now - VAULT_RESOURCE_TRANSFER_TIMEOUT_PERIOD AND vaulted = false` (default: 7 days) - resources that were funded but failed to transfer to vault within the timeout period

2. For each expired resource:
   - Retrieves all associated pledges with user information
   - Removes each pledge through the vault service (which handles VP return and tx_log entries)
   - Sends email notifications to users about the resource removal:
     - **Transfer Timeout:** If the resource failed to transfer within the timeout period, sends a notification explaining that the transfer failed due to lack of seeds, all VP has been returned, and suggests trying another torrent or retrying later
     - **Expiration:** If the resource expired normally, sends a notification explaining that the resource has been removed from the vault and the user can pledge again if needed
   - Deletes the resource from the database

**Configuration:**

The command uses the following environment variables/flags:

- `VAULT_RESOURCE_EXPIRE_PERIOD` / `--vault-resource-expire-period`: Period after which unfunded resource is removed from vault (default: 168h = 7 days)
- `VAULT_RESOURCE_TRANSFER_TIMEOUT_PERIOD` / `--vault-resource-transfer-timeout-period`: Period after which resource is removed and transfer attempts are stopped (default: 168h = 7 days)
- `VAULT_SERVICE_HOST` / `--vault-service-host`: Vault service host (required)
- `VAULT_SERVICE_PORT` / `--vault-service-port`: Vault service port (default: 80)

Plus all standard database and API configuration flags.

**Implementation Details:**

- **File:** `reap.go`
- **Model Method:** `models/vault/resource.go::GetExpiredResources()` - queries resources based on expiration conditions
- **Service Methods:**
  - `services/vault/vault.go::RemovePledge()` - handles pledge removal with proper VP return and logging
  - `services/vault/vault.go::RemoveResource()` - handles resource deletion from both Vault API and database
- **Transaction Safety:** Each pledge removal is wrapped in a database transaction to ensure consistency

**Logging:**

The command logs:
- Start of reap process with configured periods
- Number of expired resources found
- Processing of each resource with pledge count
- Each pledge removal with user ID and amount
- Notification sending (success or failure) with user email and resource ID
- Resource deletion
- Completion of reap process

**Error Handling:**

- If a pledge cannot be removed, it logs a warning and continues with the next pledge
- If a notification cannot be sent, it logs a warning and continues (pledge removal still succeeds)
- If a resource cannot be deleted, it logs a warning and continues with the next resource
- This ensures partial failures don't stop the entire reap process

**Recommended Usage:**

Run this command periodically (e.g., via cron) to clean up expired resources:

```bash
# Example cron entry (daily at 3 AM)
0 3 * * * /path/to/web-ui vault reap
```

## Vault API SDK

The Vault API SDK (`services/vault/api.go`) provides a Go client for interacting with the external Vault API service. This SDK handles HTTP communication, resource management, and caching.

### Configuration

**CLI Flags / Environment Variables:**

```go
func RegisterApiFlags(f []cli.Flag) []cli.Flag
```

Available flags:
- `--vault-service-host` / `VAULT_SERVICE_HOST`: Vault service host (required)
- `--vault-service-port` / `VAULT_SERVICE_PORT`: Vault service port (default: 80)
- `--vault-secure` / `VAULT_SECURE`: Use HTTPS for Vault service (default: false)

**Note:** If `VAULT_SERVICE_HOST` is empty, `NewApi` returns `nil` instead of creating an instance.

### Data Structures

#### ErrorResponse

Represents an error response from the Vault API:

```go
type ErrorResponse struct {
	Error string `json:"error"`
}
```

#### Resource

Represents a resource in the Vault API:

```go
type Resource struct {
	ResourceID string    `json:"resource_id"`
	Status     int       `json:"status"`
	StoredSize int64     `json:"stored_size"`
	TotalSize  int64     `json:"total_size"`
	Error      string    `json:"error"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
```

**Status Constants:**
- `StatusQueued = 0`: Resource is queued for processing
- `StatusProcessing = 1`: Resource is being processed
- `StatusCompleted = 2`: Resource is fully stored
- `StatusFailed = 3`: Resource storage failed

**Helper Methods:**
- `IsStored() bool`: Returns true if resource is fully stored (status = completed)
- `IsFailed() bool`: Returns true if resource storage failed
- `IsProcessing() bool`: Returns true if resource is being processed or queued
- `GetProgress() float64`: Returns storage progress as percentage (0-100)

### API Client

#### Constructor

```go
func NewApi(c *cli.Context, cl *http.Client) *Api
```

Creates a new Vault API client with:
- URL constructed from host, port, and secure flags
- HTTP client for making requests
- LazyMap cache for resources (1 minute TTL)

**Returns:** `nil` if `VAULT_SERVICE_HOST` is empty, otherwise returns a configured `*Api` instance.

#### Methods

##### GetResource

```go
func (s *Api) GetResource(ctx context.Context, resourceID string) (*Resource, error)
```

Retrieves a resource by ID from the Vault API.

**Parameters:**
- `ctx`: Request context
- `resourceID`: Resource identifier (infohash)

**Returns:**
- `*Resource`: Resource information
- `error`: Error if request fails or resource not found

**HTTP Details:**
- Method: GET
- Endpoint: `/resource/{id}`
- Status Codes:
  - 200: Success, returns Resource
  - 404: Resource not found
  - 500: Internal server error

##### GetResourceCached

```go
func (s *Api) GetResourceCached(ctx context.Context, resourceID string) (*Resource, error)
```

Retrieves a resource with caching (1 minute TTL). Same parameters and return values as `GetResource`.

**Use Case:** Use this method for frequent resource status checks to reduce API load.

##### PutResource

```go
func (s *Api) PutResource(ctx context.Context, resourceID string) (*Resource, error)
```

Queues a resource for storage in the Vault.

**Parameters:**
- `ctx`: Request context
- `resourceID`: Resource identifier (infohash)

**Returns:**
- `*Resource`: Resource information with queued status
- `error`: Error if request fails

**HTTP Details:**
- Method: PUT
- Endpoint: `/resource/{id}`
- Status Codes:
  - 202: Accepted, resource queued for processing
  - 500: Internal server error

**Behavior:**
- Creates the resource if it doesn't exist
- Marks existing resource as queued for processing
- Returns resource with updated status

##### DeleteResource

```go
func (s *Api) DeleteResource(ctx context.Context, resourceID string) (*Resource, error)
```

Queues a resource for deletion from the Vault.

**Parameters:**
- `ctx`: Request context
- `resourceID`: Resource identifier (infohash)

**Returns:**
- `*Resource`: Resource information with deletion status
- `error`: Error if request fails

**HTTP Details:**
- Method: DELETE
- Endpoint: `/resource/{id}`
- Status Codes:
  - 202: Accepted, resource queued for deletion
  - 500: Internal server error

### Usage Examples

#### Initialize the API Client

```go
import (
	"net/http"
	"github.com/urfave/cli"
	vaultapi "github.com/webtor-io/web-ui/services/vault"
)

func main() {
	app := cli.NewApp()
	app.Flags = vaultapi.RegisterApiFlags([]cli.Flag{})
	
	app.Action = func(c *cli.Context) error {
		client := &http.Client{Timeout: 30 * time.Second}
		api := vaultapi.NewApi(c, client)
		
		// Use the API client
		return nil
	}
	
	app.Run(os.Args)
}
```

#### Check Resource Status

```go
// Get resource status with caching
resource, err := api.GetResourceCached(ctx, "abc123def456")
if err != nil {
	log.WithError(err).Error("failed to get resource")
	return err
}

if resource.IsStored() {
	log.Info("resource is fully stored in vault")
} else if resource.IsProcessing() {
	log.Infof("resource is being processed: %.2f%% complete", resource.GetProgress())
} else if resource.IsFailed() {
	log.Errorf("resource storage failed: %s", resource.Error)
}
```

#### Queue Resource for Storage

```go
// Queue a resource for vault storage
resource, err := api.PutResource(ctx, "abc123def456")
if err != nil {
	log.WithError(err).Error("failed to queue resource")
	return err
}

log.WithField("resource_id", resource.ResourceID).
	WithField("status", resource.Status).
	Info("resource queued for storage")
```

#### Delete Resource from Vault

```go
// Queue a resource for deletion
resource, err := api.DeleteResource(ctx, "abc123def456")
if err != nil {
	log.WithError(err).Error("failed to queue resource for deletion")
	return err
}

log.WithField("resource_id", resource.ResourceID).
	Info("resource queued for deletion")
```

### Error Handling

The SDK wraps all errors with context for better debugging:

```go
resource, err := api.GetResource(ctx, resourceID)
if err != nil {
	// Error messages include context about what failed
	// Examples:
	// - "failed to get resource: failed to create request: ..."
	// - "failed to get resource: resource not found"
	// - "failed to get resource: vault api error: internal error"
	log.WithError(err).
		WithField("resource_id", resourceID).
		Error("failed to retrieve resource from vault")
	return err
}
```

**Error Types:**
- Network errors: Connection failures, timeouts
- HTTP errors: 404 (not found), 500 (server error)
- Parsing errors: Invalid JSON responses
- API errors: Error messages from the Vault API service

### Integration with Vault Service

The Vault API SDK is designed to work alongside the main Vault service (`services/vault/vault.go`):

- **Vault Service** (`vault.go`): Manages database operations, user VP, pledges, and business logic
- **Vault API SDK** (`api.go`): Communicates with external Vault API service for resource storage operations

**Typical Flow:**

1. User creates a pledge via Vault Service → database updated
2. When resource is funded, Vault Service calls Vault API SDK to queue storage
3. Vault API SDK sends PUT request to external Vault API
4. External Vault API processes and stores the resource
5. Vault Service can check storage status via Vault API SDK

**Example Integration:**

```go
// In vault service method
func (s *Vault) QueueResourceForStorage(ctx context.Context, resourceID string) error {
	// Check if resource is funded in database
	resource, err := s.GetResource(ctx, resourceID)
	if err != nil {
		return err
	}
	
	if !resource.Funded {
		return errors.New("resource is not funded")
	}
	
	// Queue resource for storage via API
	apiResource, err := s.vaultApi.PutResource(ctx, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to queue resource for storage")
	}
	
	log.WithField("resource_id", resourceID).
		WithField("status", apiResource.Status).
		Info("resource queued for vault storage")
	
	return nil
}
```

## Future Improvements

1. **Automatic Unfreezing:**
   - Background process for automatic pledge unfreezing N days after vaulting
   - Configurable freeze period for different resource types

2. **Notifications:**
   - Notify user when resource is funded
   - Notify when pledge is unfrozen and available for claiming
   - Notify on resource funding expiration

3. **Analytics:**
   - Dashboard with resource statistics
   - User balance change history
   - Top resources by pledge count

4. **Optimization:**
   - Materialized views for statistics
   - tx_log partitioning by date
   - Old record archiving

5. **Additional Features:**
   - Ability to transfer pledges between resources
   - Group pledges (multiple users together)
   - Automatic reinvestment on expiration

6. **Vault API SDK Enhancements:**
   - Retry logic with exponential backoff for failed requests
   - Circuit breaker pattern for API availability
   - Metrics and monitoring integration
   - Batch operations for multiple resources
   - Webhook support for storage completion notifications
