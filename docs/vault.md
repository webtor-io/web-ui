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
  - **Freeze period: 1 day** after pledge creation
- **Claiming** — returning points to the user from an unfrozen pledge
- **Expiration** — marking a resource as expired when funding drops below required amount
  - **Deletion period: 7 days** after expiration

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
	frozen bool DEFAULT true NOT NULL,
	frozen_at timestamptz DEFAULT now() NOT NULL,
	created_at timestamptz DEFAULT now() NOT NULL,
	updated_at timestamptz DEFAULT now() NOT NULL,
	CONSTRAINT pledge_pk PRIMARY KEY (pledge_id),
	CONSTRAINT pledge_user_fk FOREIGN KEY (user_id)
		REFERENCES public."user" (user_id) ON DELETE CASCADE
);
```

**Fields:**
- `pledge_id` — pledge UUID (primary key, auto-generated)
- `resource_id` — text resource identifier (no foreign key)
- `user_id` — user UUID (foreign key to `public.user`)
- `amount` — amount of pledged Vault Points (numeric)
- `funded` — pledge active status flag (default `true`)
- `frozen` — pledge freeze flag (default `true`, means cannot be claimed)
- `frozen_at` — pledge freeze time (default `now()`)
- `created_at` — pledge creation time
- `updated_at` — last update time (automatically updated by trigger)

**Features:**
- `funded = true` means the pledge is active and counted in resource funding
- `frozen = true` means the pledge is frozen and cannot be claimed by the user
- A pledge can be claimed only if `frozen = false` and `funded = true`
- When a pledge is created, a record is created in `tx_log` with type `OpTypeFund`
- When a pledge is claimed, a record is created in `tx_log` with type `OpTypeClaim`

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
	Frozen     bool      `pg:"frozen,notnull,default:true"`
	FrozenAt   time.Time `pg:"frozen_at,notnull,default:now()"`
	CreatedAt  time.Time `pg:"created_at,notnull,default:now()"`
	UpdatedAt  time.Time `pg:"updated_at,notnull,default:now()"`
	
	User *models.User `pg:"rel:has-one,fk:user_id"`
}
```

**Methods:**
- `GetPledge(ctx, db, pledgeID)` — get pledge by ID
- `GetUserPledges(ctx, db, userID)` — get all user's pledges
- `GetResourcePledges(ctx, db, resourceID)` — get all pledges for a resource
- `GetFundedResourcePledges(ctx, db, resourceID)` — get active pledges for a resource
- `CreatePledge(ctx, db, userID, resourceID, amount)` — create pledge
- `UpdatePledgeFunded(ctx, db, pledgeID, funded)` — update funded status
- `UpdatePledgeFrozen(ctx, db, pledgeID, frozen)` — update frozen status
- `DeletePledge(ctx, db, pledgeID)` — delete pledge
- `SumFundedPledgesForResource(ctx, db, resourceID)` — sum of active pledges for a resource

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
	CreatedAt  time.Time  `pg:"created_at,notnull,default:now()"`
	UpdatedAt  time.Time  `pg:"updated_at,notnull,default:now()"`
}
```

**Methods:**
- `GetResource(ctx, db, resourceID)` — get resource by ID
- `GetFundedResources(ctx, db)` — get all funded resources
- `GetVaultedResources(ctx, db)` — get all vaulted resources
- `CreateResource(ctx, db, resourceID, requiredVP)` — create resource
- `UpdateResourceFundedVP(ctx, db, resourceID, fundedVP)` — update funded amount
- `MarkResourceFunded(ctx, db, resourceID)` — mark resource as funded
- `MarkResourceVaulted(ctx, db, resourceID)` — mark resource as vaulted
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
- `VAULT_SERVICE_HOST` / `--vault-service-host` — Vault service host (required)
- `VAULT_SERVICE_PORT` / `--vault-service-port` — Vault service port (required)
- If either host or port is empty, the service constructor returns `nil`

**Dependencies:**
- Claims service (`services/claims`) — for getting user tier and VP information
- HTTP client — for future API calls to vault service
- PostgreSQL database (`common-services/PG`) — for database operations

**Constructor:**
```go
func New(c *cli.Context, cl *claims.Claims, client *http.Client, pg *cs.PG) *Vault
```

Returns `nil` if `VAULT_SERVICE_HOST` or `VAULT_SERVICE_PORT` is not configured.

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
   - **Case 2: Record exists and points match**
     - Do nothing, return existing record
   - **Case 3: Record exists and points differ**
     - Calculate difference: `difference = newValue - oldValue` (treating NULL as 0)
     - Update `user_vp.total` to new value
     - If `newValue != 0`, create `tx_log` entry with `OpTypeChangeTier` and `balance = difference`
     - If `newValue == 0` (free tier), do not create tx_log entry
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
   - `Frozen` = sum of pledges where `frozen = true AND funded = true`
   - `Funded` = sum of all pledges where `funded = true` (guaranteed to be >= 0)
   - `Claimable` = sum of pledges where `funded = true AND frozen = false`
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

## Processes and Use Cases

### 1. Creating a Pledge (Fund)

**Preconditions:**
- User is authenticated
- User has sufficient available VP (`Available >= amount`)

**Steps:**
1. Check user balance via `GetUserStats`
2. Verify that `Available >= amount`
3. In a transaction:
   - Create record in `pledge` with `funded = true`, `frozen = true`, `frozen_at = now()`
   - Create record in `tx_log` with `op_type = OpTypeFund`, `balance = -amount`
   - Update `funded_vp` in `resource` (sum of all active pledges)
   - If `funded_vp >= required_vp`, set `funded = true`, `funded_at = now()`
4. If resource is funded — initiate vaulting process
5. Pledge remains frozen for **1 day** after creation

**Failure Handling:**
- If vaulting fails within **7 days**, VP is returned to the user (pledge is claimed back automatically)

### 2. Claiming a Pledge (Claim)

**Preconditions:**
- User is authenticated
- Pledge belongs to the user
- Pledge is not frozen (`frozen = false`)
- Pledge is active (`funded = true`)

**Steps:**
1. Check access rights and conditions
2. In a transaction:
   - Set `funded = false` in `pledge`
   - Create record in `tx_log` with `op_type = OpTypeClaim`, `balance = +amount`
   - Update `funded_vp` in `resource` (recalculate sum of active pledges)
   - If `funded_vp < required_vp` and resource was funded, set `expired = true`, `expired_at = now()`
3. Return points to user

**Consequences:**
- If after claiming the resource becomes underfunded (`funded_vp < required_vp`), it is marked as expired
- Expired resources are automatically deleted after **7 days**

### 3. Unfreezing a Pledge (Unfreeze)

**Preconditions:**
- Resource is vaulted (`vaulted = true`)
- Sufficient time has passed since vaulting

**Steps:**
1. Find all pledges for resource where `frozen = true`
2. For each pledge set `frozen = false`
3. Users can now claim their pledges

### 4. Vaulting a Resource (Vault)

**Preconditions:**
- Resource is funded (`funded = true`)
- Resource is not yet vaulted (`vaulted = false`)

**Steps:**
1. Perform operation to place resource in long-term storage
2. Set `vaulted = true`, `vaulted_at = now()` in `resource`
3. Optionally: freeze all pledges for this resource for a certain period

### 5. User Tier Change

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
   - Cannot claim a frozen pledge
   - Sum of all active pledges cannot exceed total (if not NULL)
   - Pledges are frozen for **1 day** after creation

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
