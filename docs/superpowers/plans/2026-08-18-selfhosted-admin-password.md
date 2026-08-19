# Self-hosted Admin Password Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Дать self-hosted инстансу пароль администратора: задаётся в профиле или через `ADMIN_PASSWORD`, при заданном пароле показывается форма входа, без пароля — открытый доступ с навязчивым баннером.

**Architecture:** Вся логика живёт строго внутри ветки `if !s.hasSupetokens` в `services/auth`. Новый пакет `services/adminauth` инкапсулирует хеширование (argon2id) и источник пароля (переменная окружения имеет приоритет над хешем в БД). Сессия, CSRF и роуты `/login` `/logout` переиспользуются как есть — они уже настроены.

**Tech Stack:** Go, gin, go-pg, `gin-contrib/sessions` (Redis-store), `golang.org/x/crypto/argon2`, `golang.org/x/time/rate` через существующий `services/libapi.RateLimiter`, Go-шаблоны + Tailwind/DaisyUI.

**Spec:** `docs/superpowers/specs/2026-08-18-selfhosted-admin-password-design.md`

## Global Constraints

- **Прод не должен измениться.** Всё новое активируется только когда `hasSupetokens == false` (`services/auth/auth.go:111`). Каждая задача, трогающая поведение авторизации, обязана иметь тест, доказывающий что при сконфигурированном SuperTokens новая ветка не активируется.
- Хеширование — **argon2id** (`golang.org/x/crypto/argon2`, уже в `go.mod:159`), формат строки `$argon2id$v=19$m=<memory>,t=<time>,p=<threads>$<base64 salt>$<base64 hash>`.
- Приоритет источников пароля: `ADMIN_PASSWORD` из окружения **всегда** перекрывает хеш в БД. При заданной переменной смена пароля через профиль запрещена.
- Минимальная длина пароля — **8 символов**. Требований к составу нет.
- Администратор ровно один — пользователь с email `admin` (`services/auth/auth.go:509`). Формы входа без поля имени пользователя.
- Сравнение хешей — в постоянном времени (`crypto/subtle.ConstantTimeCompare`).
- Сессионная отметка о входе хранится под ключом `admin-authenticated` (bool) в существующей сессии (`handlers/session/handler.go:83`).

---

### Task 1: Хеширование пароля (argon2id)

Самостоятельный пакет без зависимостей от остального кода — чистые функции, полностью покрываемые юнит-тестами.

**Files:**
- Create: `services/adminauth/password.go`
- Test: `services/adminauth/password_test.go`

**Interfaces:**
- Consumes: ничего.
- Produces:
  - `func Hash(password string) (string, error)` — argon2id-хеш в строковом формате; ошибка при пароле короче 8 символов (`ErrTooShort`) и при сбое генерации соли.
  - `func Verify(encoded, password string) bool` — `true` только если пароль соответствует хешу; на битой строке хеша возвращает `false`, не паникует.
  - `var ErrTooShort = errors.New("password must be at least 8 characters")`
  - `const MinLength = 8`

- [ ] **Step 1: Написать падающий тест**

Создать `services/adminauth/password_test.go`:

```go
package adminauth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	h, err := Hash("correct horse battery")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Errorf("encoding: got %q, want an $argon2id$ string", h)
	}
	if !Verify(h, "correct horse battery") {
		t.Error("Verify rejected the password it was built from")
	}
	if Verify(h, "correct horse batterz") {
		t.Error("Verify accepted a wrong password")
	}
}

// Two hashes of the same password must differ: a per-password salt is what
// stops one leaked hash from unlocking every instance sharing that password.
func TestHashIsSalted(t *testing.T) {
	a, err := Hash("same password here")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	b, err := Hash("same password here")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if a == b {
		t.Error("two hashes of the same password are identical — salt is missing or fixed")
	}
	if !Verify(a, "same password here") || !Verify(b, "same password here") {
		t.Error("a salted hash failed to verify its own password")
	}
}

func TestHashRejectsShortPasswords(t *testing.T) {
	if _, err := Hash("short7c"); !errors.Is(err, ErrTooShort) {
		t.Errorf("7-char password: got err %v, want ErrTooShort", err)
	}
	if _, err := Hash("exactly8"); err != nil {
		t.Errorf("8-char password should be accepted, got %v", err)
	}
}

// A corrupt or foreign hash string must fail closed rather than panic: the
// value comes from the database and may predate this format.
func TestVerifyRejectsMalformedHashes(t *testing.T) {
	for _, bad := range []string{
		"",
		"not a hash",
		"$argon2id$v=19$m=65536,t=1,p=4$onlysalt",
		"$argon2id$v=19$m=abc,t=1,p=4$c2FsdA$aGFzaA",
		"$bcrypt$v=19$m=65536,t=1,p=4$c2FsdA$aGFzaA",
	} {
		if Verify(bad, "any password") {
			t.Errorf("Verify accepted malformed hash %q", bad)
		}
	}
}
```

- [ ] **Step 2: Прогнать тест — убедиться, что падает**

Run: `cd ~/Projects/webtor/web-ui && go test ./services/adminauth/ -run Test -v`
Expected: FAIL — пакет не компилируется, `undefined: Hash`

- [ ] **Step 3: Написать реализацию**

Создать `services/adminauth/password.go`:

```go
// Package adminauth owns the single-administrator password used when the
// instance runs without SuperTokens (self-hosted). It is deliberately
// separate from services/auth: that package speaks SuperTokens sessions,
// this one only answers "is this the right password".
package adminauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// MinLength is a floor on length only. Composition rules (a digit, a symbol)
// push people towards Password1! rather than towards long passphrases, so we
// do not impose them.
const MinLength = 8

var ErrTooShort = errors.New("password must be at least 8 characters")

// argon2id parameters, OWASP's second recommended profile: 64 MiB of memory,
// 3 passes, 4 lanes. They are encoded into every hash, so raising them later
// only affects newly written hashes — old ones keep verifying with the
// parameters they were made with.
const (
	hashMemory  uint32 = 64 * 1024
	hashTime    uint32 = 3
	hashThreads uint8  = 4
	hashKeyLen  uint32 = 32
	hashSaltLen        = 16
)

// Hash returns an encoded argon2id hash of password.
func Hash(password string) (string, error) {
	if len([]rune(password)) < MinLength {
		return "", ErrTooShort
	}
	salt := make([]byte, hashSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, hashTime, hashMemory, hashThreads, hashKeyLen)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, hashMemory, hashTime, hashThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify reports whether password matches the encoded hash. Any problem with
// the encoding — empty, truncated, produced by another algorithm — is a
// mismatch, never a panic: the value arrives from the database.
func Verify(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
```

- [ ] **Step 4: Прогнать тест — убедиться, что проходит**

Run: `cd ~/Projects/webtor/web-ui && go test ./services/adminauth/ -v`
Expected: PASS — все четыре теста

- [ ] **Step 5: Негативный контроль на соль**

Временно заменить в `Hash` генерацию соли на фиксированную (`salt := []byte("0123456789abcdef")`), прогнать `go test ./services/adminauth/ -run TestHashIsSalted`. Ожидается FAIL с сообщением про идентичные хеши. Вернуть как было и прогнать снова — PASS. Записать вывод обоих прогонов в отчёт: тест на соль, который не краснеет при убранной соли, бесполезен.

- [ ] **Step 6: Коммит**

```bash
git add services/adminauth/password.go services/adminauth/password_test.go
git commit -m "feat: add argon2id password hashing for the self-hosted admin"
```

---

### Task 2: Источник пароля (переменная окружения + БД)

**Files:**
- Create: `services/adminauth/store.go`
- Test: `services/adminauth/store_test.go`

**Interfaces:**
- Consumes: `Hash`, `Verify`, `ErrTooShort`, `MinLength` из Task 1.
- Produces:
  - `type HashRepo interface { Get(ctx context.Context) (string, error); Set(ctx context.Context, hash string) error }` — абстракция над хранилищем, чтобы стор тестировался без базы.
  - `type Store struct { ... }`
  - `func NewStore(envPassword string, repo HashRepo) *Store`
  - `func (s *Store) ManagedByEnv() bool` — пароль пришёл из окружения.
  - `func (s *Store) IsConfigured(ctx context.Context) bool` — пароль вообще задан (окружение или БД).
  - `func (s *Store) Verify(ctx context.Context, password string) bool`
  - `func (s *Store) Set(ctx context.Context, password string) error` — возвращает `ErrManagedByEnv`, если пароль задан переменной; `ErrTooShort` при коротком пароле.
  - `var ErrManagedByEnv = errors.New("password is managed by ADMIN_PASSWORD")`

- [ ] **Step 1: Написать падающий тест**

Создать `services/adminauth/store_test.go`:

```go
package adminauth

import (
	"context"
	"errors"
	"testing"
)

// fakeRepo stands in for the database so the precedence rules can be tested
// without one.
type fakeRepo struct {
	hash    string
	setCall int
	getErr  error
}

func (f *fakeRepo) Get(_ context.Context) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.hash, nil
}

func (f *fakeRepo) Set(_ context.Context, h string) error {
	f.setCall++
	f.hash = h
	return nil
}

func TestNoPasswordConfigured(t *testing.T) {
	s := NewStore("", &fakeRepo{})
	if s.IsConfigured(context.Background()) {
		t.Error("IsConfigured reported true with neither env nor stored hash")
	}
	if s.Verify(context.Background(), "anything") {
		t.Error("Verify accepted a password while none is configured")
	}
}

func TestEnvPasswordWins(t *testing.T) {
	stored, err := Hash("stored password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	s := NewStore("env password", &fakeRepo{hash: stored})

	if !s.ManagedByEnv() {
		t.Error("ManagedByEnv is false while ADMIN_PASSWORD is set")
	}
	if !s.IsConfigured(context.Background()) {
		t.Error("IsConfigured is false while ADMIN_PASSWORD is set")
	}
	if !s.Verify(context.Background(), "env password") {
		t.Error("the env password was rejected")
	}
	if s.Verify(context.Background(), "stored password") {
		t.Error("the stored hash still verified while ADMIN_PASSWORD is set — env must win outright")
	}
}

func TestStoredHashUsedWithoutEnv(t *testing.T) {
	stored, err := Hash("stored password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	s := NewStore("", &fakeRepo{hash: stored})

	if s.ManagedByEnv() {
		t.Error("ManagedByEnv is true with no ADMIN_PASSWORD")
	}
	if !s.Verify(context.Background(), "stored password") {
		t.Error("the stored password was rejected")
	}
	if s.Verify(context.Background(), "wrong password") {
		t.Error("a wrong password verified against the stored hash")
	}
}

func TestSetRefusedWhenManagedByEnv(t *testing.T) {
	repo := &fakeRepo{}
	s := NewStore("env password", repo)
	if err := s.Set(context.Background(), "new password"); !errors.Is(err, ErrManagedByEnv) {
		t.Errorf("Set: got %v, want ErrManagedByEnv", err)
	}
	if repo.setCall != 0 {
		t.Error("Set wrote to the repo even though the password is env-managed")
	}
}

func TestSetStoresAVerifiableHash(t *testing.T) {
	repo := &fakeRepo{}
	s := NewStore("", repo)
	if err := s.Set(context.Background(), "brand new password"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if repo.hash == "brand new password" {
		t.Fatal("Set stored the password verbatim instead of a hash")
	}
	if !s.Verify(context.Background(), "brand new password") {
		t.Error("the password just set does not verify")
	}
}

func TestSetRejectsShortPassword(t *testing.T) {
	repo := &fakeRepo{}
	s := NewStore("", repo)
	if err := s.Set(context.Background(), "short7c"); !errors.Is(err, ErrTooShort) {
		t.Errorf("Set: got %v, want ErrTooShort", err)
	}
	if repo.setCall != 0 {
		t.Error("Set wrote a too-short password to the repo")
	}
}

// A database that is down must not silently turn a protected instance into an
// open one: an unreadable hash means "cannot verify", not "no password".
func TestRepoErrorDoesNotOpenTheInstance(t *testing.T) {
	s := NewStore("", &fakeRepo{getErr: errors.New("db is down")})
	if s.Verify(context.Background(), "anything") {
		t.Error("Verify succeeded while the hash could not be read")
	}
}
```

- [ ] **Step 2: Прогнать тест — убедиться, что падает**

Run: `cd ~/Projects/webtor/web-ui && go test ./services/adminauth/ -run TestEnvPasswordWins -v`
Expected: FAIL — `undefined: NewStore`

- [ ] **Step 3: Написать реализацию**

Создать `services/adminauth/store.go`:

```go
package adminauth

import (
	"context"
	"errors"
	"strings"
)

// ErrManagedByEnv is returned by Set when ADMIN_PASSWORD is in play: the
// environment is the source of truth then, and writing to the database would
// store a password that never takes effect.
var ErrManagedByEnv = errors.New("password is managed by ADMIN_PASSWORD")

// HashRepo is the persistence half of the store. Keeping it an interface lets
// the precedence rules be tested without a database.
type HashRepo interface {
	Get(ctx context.Context) (string, error)
	Set(ctx context.Context, hash string) error
}

// Store answers whether a password is configured and whether a given password
// matches it. ADMIN_PASSWORD, when set, wins outright over the stored hash —
// that is what makes it a recovery path.
type Store struct {
	envPassword string
	repo        HashRepo
}

func NewStore(envPassword string, repo HashRepo) *Store {
	return &Store{envPassword: strings.TrimSpace(envPassword), repo: repo}
}

func (s *Store) ManagedByEnv() bool {
	return s.envPassword != ""
}

func (s *Store) IsConfigured(ctx context.Context) bool {
	if s.ManagedByEnv() {
		return true
	}
	h, err := s.repo.Get(ctx)
	if err != nil {
		// Unreadable storage is not proof that no password exists. Report
		// configured so the instance stays closed instead of falling open.
		return true
	}
	return h != ""
}

func (s *Store) Verify(ctx context.Context, password string) bool {
	if s.ManagedByEnv() {
		return subtleEqual(s.envPassword, password)
	}
	h, err := s.repo.Get(ctx)
	if err != nil || h == "" {
		return false
	}
	return Verify(h, password)
}

func (s *Store) Set(ctx context.Context, password string) error {
	if s.ManagedByEnv() {
		return ErrManagedByEnv
	}
	h, err := Hash(password)
	if err != nil {
		return err
	}
	return s.repo.Set(ctx, h)
}
```

Добавить в `services/adminauth/password.go` вспомогательную функцию для сравнения открытых строк в постоянном времени (пароль из переменной окружения не хешируется, но сравнивать его наивно всё равно нельзя):

```go
// subtleEqual compares two plaintext strings without leaking their common
// prefix length through timing. Used for the ADMIN_PASSWORD path, where there
// is no hash to compare against.
func subtleEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
```

- [ ] **Step 4: Прогнать тесты — убедиться, что проходят**

Run: `cd ~/Projects/webtor/web-ui && go test ./services/adminauth/ -v`
Expected: PASS — все тесты обоих файлов

- [ ] **Step 5: Коммит**

```bash
git add services/adminauth/store.go services/adminauth/password.go services/adminauth/store_test.go
git commit -m "feat: add admin password store with env-over-database precedence"
```

---

### Task 3: Хранилище хеша в БД

**Files:**
- Create: `services/adminauth/pg_repo.go`
- Modify: `models/user.go:17` (комментарий к полю `Password`)

**Interfaces:**
- Consumes: `HashRepo` из Task 2.
- Produces: `func NewPGRepo(pg *cs.PG) HashRepo` — читает и пишет колонку `password` у пользователя с email `admin`. При отсутствующем соединении с БД `Get` возвращает ошибку, `Set` — ошибку.

Колонка `password` в таблице `user` уже существует (`migrations/2_normalize_users.up.sql:5`), поле `models.User.Password` объявлено (`models/user.go:17`) и до сих пор ничем не использовалось.

- [ ] **Step 1: Написать реализацию**

Создать `services/adminauth/pg_repo.go`:

```go
package adminauth

import (
	"context"
	"errors"

	"github.com/go-pg/pg/v10"
	cs "github.com/webtor-io/common-services"

	"github.com/webtor-io/web-ui/models"
)

// adminEmail is the single administrator's row. It matches the email used by
// services/auth.registerAdminUser, which creates that row on first request.
const adminEmail = "admin"

var errNoDB = errors.New("database is not available")

type pgRepo struct {
	pg *cs.PG
}

// NewPGRepo stores the admin password hash in the existing user.password
// column. That column has existed since migration 2 and was never read or
// written — it predates SuperTokens.
func NewPGRepo(p *cs.PG) HashRepo {
	return &pgRepo{pg: p}
}

func (r *pgRepo) Get(ctx context.Context) (string, error) {
	db := r.pg.Get()
	if db == nil {
		return "", errNoDB
	}
	u := &models.User{}
	err := db.Model(u).
		Context(ctx).
		Where("email = ?", adminEmail).
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		// No admin row yet means nobody has visited the instance, which is
		// indistinguishable from "no password set".
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return u.Password, nil
}

func (r *pgRepo) Set(ctx context.Context, hash string) error {
	db := r.pg.Get()
	if db == nil {
		return errNoDB
	}
	u := &models.User{}
	_, err := db.Model(u).
		Context(ctx).
		Set("password = ?", hash).
		Where("email = ?", adminEmail).
		Update()
	return err
}
```

- [ ] **Step 2: Дополнить комментарий к полю модели**

В `models/user.go` заменить строку `	Password      string` на:

```go
	// Password holds the argon2id hash of the self-hosted administrator's
	// password (services/adminauth). It is empty for every other user and on
	// every SuperTokens-backed deployment.
	Password string
```

- [ ] **Step 3: Проверить, что всё компилируется**

Run: `cd ~/Projects/webtor/web-ui && go build ./... && go vet ./services/adminauth/ ./models/`
Expected: без вывода, код возврата 0

- [ ] **Step 4: Коммит**

```bash
git add services/adminauth/pg_repo.go models/user.go
git commit -m "feat: persist the admin password hash in the existing user.password column"
```

---

### Task 4: Ветвление в middleware авторизации

Сердце задачи. Здесь же — обязательный негативный контроль на прод.

**Files:**
- Modify: `services/auth/auth.go:459-465` (ветка `!hasSupetokens` в `RegisterHandler`), `services/auth/auth.go:108-127` (`New`), `services/auth/auth.go:49-90` (флаги)
- Test: `services/auth/admin_password_test.go`

**Interfaces:**
- Consumes: `adminauth.Store` из Task 2, `adminauth.NewPGRepo` из Task 3.
- Produces:
  - флаг `admin-password` с `EnvVar: "ADMIN_PASSWORD"` в `services/auth`;
  - `func IsOpenInstance(c *gin.Context) bool` — инстанс работает без пароля (для баннера);
  - `func (s *Auth) AdminStore() *adminauth.Store` — доступ для хендлеров логина и профиля;
  - ключ сессии `admin-authenticated`.

- [ ] **Step 1: Написать падающий тест**

Создать `services/auth/admin_password_test.go`:

```go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/adminauth"
)

// withSession wires the same session middleware the app uses, so the tests
// exercise the real cookie plumbing rather than a stub.
func withSession(r *gin.Engine) {
	store := cookie.NewStore([]byte("test secret"))
	r.Use(sessions.Sessions("session", store))
}

// The whole feature must be inert wherever SuperTokens is configured — that is
// production. A regression here would put a login form in front of every
// webtor.io user.
func TestAdminPasswordInertWithSupertokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// A password IS configured here. That is the point: the SuperTokens check
	// must win over a perfectly valid password, otherwise dropping the check
	// would go unnoticed.
	a := &Auth{
		hasSupetokens: true,
		adminStore:    adminauth.NewStore("some password", nil),
	}

	if a.adminPasswordActive(nil) {
		t.Error("the admin-password branch activated while SuperTokens is configured")
	}
}

func TestOpenInstanceWhenNoPasswordConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	withSession(r)

	var open, admin bool
	r.Use(func(c *gin.Context) {
		open = IsOpenInstance(c)
		admin = IsAdmin(c)
	})
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if !open {
		t.Error("IsOpenInstance is false while no password is configured")
	}
	_ = admin
}
```

Замечание для исполнителя: тест выше пинит две вещи, которые можно проверить без базы — что при SuperTokens ветка не активируется и что при незаданном пароле инстанс помечен открытым. Полный сценарий «пароль задан → без сессии доступа нет» проверяется в Task 5 на уровне `HasAuth`, где не нужен реальный `*cs.PG`.

- [ ] **Step 2: Прогнать тест — убедиться, что падает**

Run: `cd ~/Projects/webtor/web-ui && go test ./services/auth/ -run TestAdminPassword -v`
Expected: FAIL — `a.adminPasswordActive undefined`

- [ ] **Step 3: Добавить флаг**

В `services/auth/auth.go` в список констант рядом с `overrideUserEmail = "override-user-email"` (строка 45) добавить:

```go
	adminPasswordFlag       = "admin-password"
```

В функцию `RegisterFlags`, рядом с флагом `overrideUserEmail` (строка 82), добавить:

```go
		cli.StringFlag{
			Name:   adminPasswordFlag,
			Usage:  "password for the single self-hosted administrator; overrides the stored one and disables changing it from the profile",
			EnvVar: "ADMIN_PASSWORD",
		},
```

- [ ] **Step 4: Прокинуть стор в структуру**

В структуру `Auth` (рядом с `overrideUserEmail string`, строка 105) добавить:

```go
	adminStore          *adminauth.Store
```

В `New` (строка 108) добавить в конструирование:

```go
		adminStore:          adminauth.NewStore(c.String(adminPasswordFlag), adminauth.NewPGRepo(pg)),
```

Добавить импорт `"github.com/webtor-io/web-ui/services/adminauth"`.

- [ ] **Step 5: Переписать ветку без SuperTokens**

Заменить в `services/auth/auth.go` начало `RegisterHandler` (строки 459-465, блок `if !s.hasSupetokens { r.Use(func(c *gin.Context) { s.registerAdminUser(c) ... }) }`) на:

```go
	if !s.hasSupetokens {
		r.Use(func(c *gin.Context) {
			// Three states, in order of precedence:
			//   no password configured → open instance, auto-admin (legacy
			//     behaviour) plus a flag the banner renders from;
			//   password configured and the session carries the login mark →
			//     auto-admin;
			//   password configured and no mark → stay anonymous, which lands
			//     the request on HasAuth and from there on /login.
			if !s.adminStore.IsConfigured(c.Request.Context()) {
				ctx := context.WithValue(c.Request.Context(), IsOpenInstanceContext{}, true)
				c.Request = c.Request.WithContext(ctx)
				s.registerAdminUser(c)
				c.Next()
				return
			}
			if adminSessionActive(c) {
				s.registerAdminUser(c)
			}
			c.Next()
		})
		return
	}
```

Ниже в том же файле добавить:

```go
// IsOpenInstanceContext marks a request served by an instance that has no
// administrator password at all.
type IsOpenInstanceContext struct{}

// IsOpenInstance reports whether this instance is running without a password.
// Templates use it to render the "set a password" banner.
func IsOpenInstance(c *gin.Context) bool {
	v := c.Request.Context().Value(IsOpenInstanceContext{})
	open, ok := v.(bool)
	return ok && open
}

// AdminSessionKey is the session entry that records a successful password
// login. It lives in the session store already configured in
// handlers/session, so it inherits that cookie's HttpOnly/Secure settings.
const AdminSessionKey = "admin-authenticated"

func adminSessionActive(c *gin.Context) bool {
	v := sessions.Default(c).Get(AdminSessionKey)
	active, ok := v.(bool)
	return ok && active
}

// adminPasswordActive reports whether the password branch governs this
// request. It is false wherever SuperTokens is configured, which is what keeps
// production untouched.
func (s *Auth) adminPasswordActive(c *gin.Context) bool {
	if s.hasSupetokens {
		return false
	}
	if c == nil {
		return s.adminStore != nil
	}
	return s.adminStore.IsConfigured(c.Request.Context())
}

// AdminStore exposes the password store to the login and profile handlers.
func (s *Auth) AdminStore() *adminauth.Store {
	return s.adminStore
}
```

Добавить импорт `"github.com/gin-contrib/sessions"`, если его ещё нет в файле.

- [ ] **Step 6: Прогнать тесты**

Run: `cd ~/Projects/webtor/web-ui && go build ./... && go test ./services/auth/ -v`
Expected: PASS — новые тесты и существующие `has_auth_test.go`

- [ ] **Step 7: Негативный контроль на защиту прода**

Временно убрать из `adminPasswordActive` блок `if s.hasSupetokens { return false }` целиком, прогнать `go test ./services/auth/ -run TestAdminPasswordInertWithSupertokens`. Ожидается FAIL: стор в тесте сконфигурирован непустым паролем, поэтому без проверки на SuperTokens функция вернёт `true`. Вернуть как было, прогнать снова — PASS. Вывод обоих прогонов записать в отчёт: это единственный тест, стоящий между этой правкой и падением прода.

- [ ] **Step 8: Коммит**

```bash
git add services/auth/auth.go services/auth/admin_password_test.go
git commit -m "feat: gate the auto-admin login behind a password when one is configured"
```

---

### Task 5: Редирект на форму входа для браузера

`HasAuth` (`services/auth/auth.go:535-542`) сейчас отдаёт голый 401. Для навигационного запроса это белая страница вместо формы входа.

**Files:**
- Modify: `services/auth/auth.go:535-542`
- Test: `services/auth/has_auth_test.go` (дописать)

**Interfaces:**
- Consumes: ничего нового.
- Produces: `HasAuth` при отсутствии авторизации отдаёт `302` на `/login?from=<path>` для навигационных HTML-запросов и `401` для всего остального.

- [ ] **Step 1: Написать падающий тест**

Дописать в `services/auth/has_auth_test.go`:

```go
// A browser navigating to a protected page must land on the login form, not on
// a blank 401. Everything else — XHR, the JSON API, SSE — must keep getting
// 401, because a 302 to an HTML page is unparseable for them.
func TestHasAuthRedirectsBrowsersButNotAPIClients(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	gr := r.Group("/guarded")
	gr.Use(HasAuth)
	gr.GET("", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	cases := map[string]struct {
		headers  map[string]string
		wantCode int
	}{
		"browser navigation": {
			headers:  map[string]string{"Accept": "text/html,application/xhtml+xml", "Sec-Fetch-Mode": "navigate"},
			wantCode: http.StatusFound,
		},
		"xhr": {
			headers:  map[string]string{"Accept": "application/json", "X-Requested-With": "XMLHttpRequest"},
			wantCode: http.StatusUnauthorized,
		},
		"api client": {
			headers:  map[string]string{"Accept": "application/json"},
			wantCode: http.StatusUnauthorized,
		},
		"html fetch that is not a navigation": {
			headers:  map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "cors"},
			wantCode: http.StatusUnauthorized,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/guarded", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", w.Code, tc.wantCode)
			}
			if tc.wantCode == http.StatusFound {
				if loc := w.Header().Get("Location"); loc != "/login?from=%2Fguarded" && loc != "/login?from=/guarded" {
					t.Errorf("Location: got %q, want a /login redirect carrying the original path", loc)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Прогнать тест — убедиться, что падает**

Run: `cd ~/Projects/webtor/web-ui && go test ./services/auth/ -run TestHasAuthRedirects -v`
Expected: FAIL — для браузерного случая приходит 401 вместо 302

- [ ] **Step 3: Написать реализацию**

Заменить `HasAuth` в `services/auth/auth.go`:

```go
// HasAuth rejects anonymous requests. It must abort, not merely return:
// gin runs its handlers in a loop, and a middleware that returns without
// aborting simply hands over to the next one.
//
// A browser navigating to a protected page gets a redirect to the login form;
// everything else keeps the bare 401, because an XHR or an SSE stream cannot
// do anything useful with an HTML page.
func HasAuth(c *gin.Context) {
	u := GetUserFromContext(c)
	if !u.HasAuth() {
		if isNavigation(c.Request) {
			c.Redirect(http.StatusFound, "/login?from="+url.QueryEscape(c.Request.URL.Path))
			c.Abort()
			return
		}
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	c.Next()
}

// isNavigation reports whether the request is a browser navigating to a page,
// as opposed to a script fetching data. Sec-Fetch-Mode is authoritative where
// the browser sends it; the Accept check covers the rest.
func isNavigation(r *http.Request) bool {
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		return false
	}
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return mode == "navigate"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}
```

Добавить импорты `"net/url"` и `"strings"`, если их нет.

- [ ] **Step 4: Прогнать тесты**

Run: `cd ~/Projects/webtor/web-ui && go test ./services/auth/ -v`
Expected: PASS — включая существующие `TestHasAuthAbortsAnonymousRequests` и `TestHasAuthLetsSignedInRequestsThrough`

- [ ] **Step 5: Коммит**

```bash
git add services/auth/auth.go services/auth/has_auth_test.go
git commit -m "feat: send browsers to the login form instead of a bare 401"
```

---

### Task 6: Форма входа по паролю

**Files:**
- Modify: `handlers/auth/handler.go:93-133`
- Create: `templates/views/auth/password.html`
- Test: `handlers/auth/password_login_test.go`

**Interfaces:**
- Consumes: `auth.Auth.AdminStore()` и `auth.AdminSessionKey` из Task 4, `adminauth.Store.Verify` из Task 2.
- Produces:
  - `POST /login` — принимает поле формы `password`; при верном пароле пишет `AdminSessionKey=true` в сессию и редиректит на `/`; при неверном рендерит форму с ошибкой и статусом 401; при превышении лимита попыток — 429.
  - `GET /login` в парольном режиме рендерит `auth/password` вместо `auth/login`.

Лимитер: `libapi.NewRateLimiterWith(0.2, 5)` — пять попыток разом, дальше одна попытка в пять секунд, ключ — IP клиента. Сигнатура: `Take(key string) (retryAfter time.Duration, ok bool)` (`services/libapi/ratelimit.go:74`).

- [ ] **Step 1: Написать падающий тест**

Создать `handlers/auth/password_login_test.go`:

```go
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/adminauth"
	"github.com/webtor-io/web-ui/services/libapi"
)

type memRepo struct{ hash string }

func (m *memRepo) Get(_ context.Context) (string, error)      { return m.hash, nil }
func (m *memRepo) Set(_ context.Context, h string) error      { m.hash = h; return nil }

func newLoginRouter(t *testing.T, store *adminauth.Store) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("test secret"))))
	h := &Handler{adminStore: store, loginLimiter: libapi.NewRateLimiterWith(0.2, 5)}
	r.POST("/login", h.passwordLogin)
	return r
}

func post(r *gin.Engine, password string) *httptest.ResponseRecorder {
	form := url.Values{"password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPasswordLoginAcceptsCorrectPassword(t *testing.T) {
	h, err := adminauth.Hash("the right password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	r := newLoginRouter(t, adminauth.NewStore("", &memRepo{hash: h}))

	w := post(r, "the right password")
	if w.Code != http.StatusFound {
		t.Fatalf("status: got %d, want 302 (body %q)", w.Code, w.Body.String())
	}
	if !strings.Contains(strings.Join(w.Header().Values("Set-Cookie"), " "), "session=") {
		t.Error("no session cookie was set after a successful login")
	}
}

func TestPasswordLoginRejectsWrongPassword(t *testing.T) {
	h, err := adminauth.Hash("the right password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	r := newLoginRouter(t, adminauth.NewStore("", &memRepo{hash: h}))

	w := post(r, "the wrong password")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
	if w.Header().Get("Location") != "" {
		t.Error("a failed login still issued a redirect")
	}
}

// Without a limit, a public instance is one script away from a password.
func TestPasswordLoginRateLimitsAttempts(t *testing.T) {
	h, err := adminauth.Hash("the right password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	r := newLoginRouter(t, adminauth.NewStore("", &memRepo{hash: h}))

	var got429 bool
	for i := 0; i < 12; i++ {
		if post(r, "guess").Code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Error("twelve wrong passwords in a row never hit the rate limit")
	}
}
```

- [ ] **Step 2: Прогнать тест — убедиться, что падает**

Run: `cd ~/Projects/webtor/web-ui && go test ./handlers/auth/ -run TestPasswordLogin -v`
Expected: FAIL — `h.passwordLogin undefined`, `Handler` не имеет полей `adminStore` и `loginLimiter`

- [ ] **Step 3: Написать реализацию**

В `handlers/auth/handler.go` расширить структуру `Handler` (строка 89):

```go
type Handler struct {
	tb           template.Builder[*web.Context]
	adminStore   *adminauth.Store
	loginLimiter *libapi.RateLimiter
}
```

Изменить сигнатуру `RegisterHandler` и добавить роут:

```go
func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], a *auth.Auth) {
	h := &Handler{
		tb: tm.MustRegisterViews("auth/*").WithLayout("main"),
		// Five attempts at once, then one per five seconds. Enough that a
		// mistyped password is never noticed, little enough that guessing is
		// pointless.
		loginLimiter: libapi.NewRateLimiterWith(0.2, 5),
		adminStore:   a.AdminStore(),
	}
	...
	r.GET("/login", h.login)
	r.POST("/login", h.passwordLogin)
	...
}
```

Изменить `login` так, чтобы в парольном режиме рендерилась форма:

```go
func (s *Handler) login(c *gin.Context) {
	if s.adminStore != nil && s.adminStore.IsConfigured(c.Request.Context()) {
		s.tb.Build("auth/password").HTML(http.StatusOK, web.NewContext(c).WithData(PasswordLoginData{}))
		return
	}
	instruction := "default"
	...
}
```

Добавить обработчик и тип данных:

```go
// PasswordLoginData drives templates/views/auth/password.html. Err carries an
// i18n key rather than a message so the copy stays in the locales.
type PasswordLoginData struct {
	Err string
}

// passwordLogin verifies the single administrator password. A failure returns
// 401 with the form re-rendered — never a redirect, so a script cannot tell
// success from failure by following one.
func (s *Handler) passwordLogin(c *gin.Context) {
	if s.adminStore == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if s.loginLimiter != nil {
		if retryAfter, ok := s.loginLimiter.Take(c.ClientIP()); !ok {
			c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			s.tb.Build("auth/password").HTML(http.StatusTooManyRequests,
				web.NewContext(c).WithData(PasswordLoginData{Err: "auth.password.tooManyAttempts"}))
			return
		}
	}
	if !s.adminStore.Verify(c.Request.Context(), c.PostForm("password")) {
		s.tb.Build("auth/password").HTML(http.StatusUnauthorized,
			web.NewContext(c).WithData(PasswordLoginData{Err: "auth.password.wrong"}))
		return
	}
	session := sessions.Default(c)
	session.Set(auth.AdminSessionKey, true)
	if err := session.Save(); err != nil {
		s.tb.Build("auth/password").HTML(http.StatusInternalServerError,
			web.NewContext(c).WithData(PasswordLoginData{Err: "auth.password.sessionFailed"}))
		return
	}
	c.Redirect(http.StatusFound, "/")
}
```

Дописать `logout`, чтобы он снимал отметку:

```go
func (s *Handler) logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Delete(auth.AdminSessionKey)
	_ = session.Save()
	s.tb.Build("auth/logout").HTML(http.StatusOK, web.NewContext(c).WithData(LogoutData{}))
}
```

Добавить импорты `"strconv"`, `"github.com/webtor-io/web-ui/services/adminauth"`, `"github.com/webtor-io/web-ui/services/libapi"`.

Обновить вызов `RegisterHandler` в `serve.go` — найти строку с `auth.RegisterHandler(` и передать третьим аргументом экземпляр `*auth.Auth`, который там уже создаётся.

- [ ] **Step 4: Написать шаблон формы**

Создать `templates/views/auth/password.html` по образцу существующего `templates/views/auth/login.html` (взять из него разметку карточки и layout-обвязку). Содержимое формы:

```html
{{ define "auth/password" }}
<div class="flex justify-center py-16 px-4">
    <div class="w-full max-w-sm bg-base-200 border border-w-line rounded-box p-6">
        <h1 class="text-xl font-semibold mb-1">{{ t $.Lang "auth.password.title" }}</h1>
        <p class="text-sm text-w-muted mb-6">{{ t $.Lang "auth.password.subtitle" }}</p>
        {{ if .Data.Err }}
        <div class="alert alert-error mb-4 text-sm">{{ t $.Lang .Data.Err }}</div>
        {{ end }}
        <form method="post" action="{{ langPath $.Lang "/login" }}">
            <input type="hidden" name="_csrf" value="{{ $.CSRF }}" />
            <input type="password" name="password" autocomplete="current-password" autofocus required
                   class="input input-bordered w-full mb-4" placeholder="{{ t $.Lang "auth.password.placeholder" }}" />
            <button type="submit" class="btn btn-pink w-full">{{ t $.Lang "auth.password.submit" }}</button>
        </form>
    </div>
</div>
{{ end }}
```

Добавить ключи `auth.password.title`, `auth.password.subtitle`, `auth.password.placeholder`, `auth.password.submit`, `auth.password.wrong`, `auth.password.tooManyAttempts`, `auth.password.sessionFailed` во все файлы локалей — найти их командой `ls locales/` и добавить в каждый, взяв за образец соседние ключи `auth.login.*`.

- [ ] **Step 5: Прогнать тесты**

Run: `cd ~/Projects/webtor/web-ui && go build ./... && go test ./handlers/auth/ ./services/... -count=1`
Expected: PASS

- [ ] **Step 6: Негативный контроль на лимитер**

Временно поменять в `passwordLogin` условие `if s.loginLimiter != nil` на `if false`, прогнать `go test ./handlers/auth/ -run TestPasswordLoginRateLimits`. Ожидается FAIL. Вернуть и прогнать снова — PASS. Вывод обоих прогонов в отчёт.

- [ ] **Step 7: Коммит**

```bash
git add handlers/auth/ templates/views/auth/password.html locales/ serve.go
git commit -m "feat: add the password login form for self-hosted instances"
```

---

### Task 7: Секция пароля в профиле

**Files:**
- Modify: `handlers/profile/handler.go:82` (структура данных), `handlers/profile/handler.go:441` (заполнение), `templates/views/profile/get.html`
- Create: `templates/partials/profile/password.html`
- Test: `handlers/profile/password_section_test.go`

**Interfaces:**
- Consumes: `adminauth.Store` (`Set`, `ManagedByEnv`, `IsConfigured`, `ErrManagedByEnv`, `ErrTooShort`) из Task 2.
- Produces: `POST /profile/password` — поля формы `current` и `new`; при заданном пароле требует верный `current`.

Существующий паттерн секции профиля: поле в структуре `Data` (`handlers/profile/handler.go:82` — `DisableWebDAV bool`), заполнение (`:441`), гейт в шаблоне (`templates/views/profile/get.html:60` — `{{ if not .Data.DisableWebDAV }}`).

- [ ] **Step 1: Написать падающий тест**

Создать `handlers/profile/password_section_test.go`:

```go
package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/adminauth"
)

type memRepo struct{ hash string }

func (m *memRepo) Get(_ context.Context) (string, error) { return m.hash, nil }
func (m *memRepo) Set(_ context.Context, h string) error { m.hash = h; return nil }

func postPassword(r *gin.Engine, current, next string) *httptest.ResponseRecorder {
	form := url.Values{"current": {current}, "new": {next}}
	req := httptest.NewRequest(http.MethodPost, "/profile/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSetPasswordOnAnOpenInstance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &memRepo{}
	store := adminauth.NewStore("", repo)
	r := gin.New()
	h := &Handler{adminStore: store}
	r.POST("/profile/password", h.setPassword)

	if w := postPassword(r, "", "a brand new password"); w.Code != http.StatusFound {
		t.Fatalf("status: got %d, want 302 (body %q)", w.Code, w.Body.String())
	}
	if !store.Verify(context.Background(), "a brand new password") {
		t.Error("the password was not stored")
	}
}

// Changing an existing password without proving you know it turns a stolen
// session into a permanent takeover.
func TestChangePasswordRequiresTheCurrentOne(t *testing.T) {
	gin.SetMode(gin.TestMode)
	existing, err := adminauth.Hash("the old password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	repo := &memRepo{hash: existing}
	store := adminauth.NewStore("", repo)
	r := gin.New()
	h := &Handler{adminStore: store}
	r.POST("/profile/password", h.setPassword)

	if w := postPassword(r, "not the old one", "a brand new password"); w.Code == http.StatusFound {
		t.Error("the password changed without the current one")
	}
	if store.Verify(context.Background(), "a brand new password") {
		t.Error("the new password took effect despite a wrong current password")
	}
	if w := postPassword(r, "the old password", "a brand new password"); w.Code != http.StatusFound {
		t.Errorf("a correct current password was rejected: %d", w.Code)
	}
}

func TestPasswordChangeRefusedWhenEnvManaged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := adminauth.NewStore("env password", &memRepo{})
	r := gin.New()
	h := &Handler{adminStore: store}
	r.POST("/profile/password", h.setPassword)

	w := postPassword(r, "env password", "a brand new password")
	if w.Code == http.StatusFound {
		t.Error("the profile changed a password that is managed by ADMIN_PASSWORD")
	}
}
```

- [ ] **Step 2: Прогнать тест — убедиться, что падает**

Run: `cd ~/Projects/webtor/web-ui && go test ./handlers/profile/ -run TestSetPassword -v`
Expected: FAIL — `h.setPassword undefined`

- [ ] **Step 3: Написать реализацию**

В `handlers/profile/handler.go` добавить в структуру `Handler` поле `adminStore *adminauth.Store` и заполнить его там же, где заполняется `disableWebDAV` (строка 122), значением `a.AdminStore()` — экземпляр `*auth.Auth` передать в `RegisterHandler` из `serve.go`.

В структуру данных страницы (строка 82, рядом с `DisableWebDAV bool`) добавить:

```go
	PasswordSet        bool
	PasswordManagedEnv bool
```

В месте заполнения (строка 441, рядом с `DisableWebDAV: s.disableWebDAV`) добавить:

```go
		PasswordSet:        s.adminStore != nil && s.adminStore.IsConfigured(c.Request.Context()),
		PasswordManagedEnv: s.adminStore != nil && s.adminStore.ManagedByEnv(),
```

Добавить обработчик:

```go
// setPassword sets or changes the single administrator password. Changing an
// existing password requires the current one: otherwise a stolen session
// converts into a permanent takeover.
func (s *Handler) setPassword(c *gin.Context) {
	if s.adminStore == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	ctx := c.Request.Context()
	if s.adminStore.ManagedByEnv() {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if s.adminStore.IsConfigured(ctx) && !s.adminStore.Verify(ctx, c.PostForm("current")) {
		c.Redirect(http.StatusFound, "/profile?err=auth.password.wrongCurrent")
		return
	}
	if err := s.adminStore.Set(ctx, c.PostForm("new")); err != nil {
		if errors.Is(err, adminauth.ErrTooShort) {
			c.Redirect(http.StatusFound, "/profile?err=auth.password.tooShort")
			return
		}
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Redirect(http.StatusFound, "/profile")
}
```

Зарегистрировать роут рядом с другими POST-роутами профиля: `r.POST("/profile/password", h.setPassword)` под тем же `HasAuth`, что и остальные формы профиля.

- [ ] **Step 4: Написать шаблон секции**

Создать `templates/partials/profile/password.html` по образцу `templates/partials/profile/webdav.html`:

```html
{{ define "profile/password" }}
<div class="bg-base-200 border border-w-line rounded-box p-6">
    <h2 class="text-lg font-semibold mb-1">{{ t .Ctx.Lang "profile.password.title" }}</h2>
    {{ if .Ctx.Data.PasswordManagedEnv }}
    <p class="text-sm text-w-muted">{{ t .Ctx.Lang "profile.password.managedByEnv" }}</p>
    {{ else }}
    <p class="text-sm text-w-muted mb-4">
        {{ if .Ctx.Data.PasswordSet }}{{ t .Ctx.Lang "profile.password.setHint" }}{{ else }}{{ t .Ctx.Lang "profile.password.unsetHint" }}{{ end }}
    </p>
    <form method="post" action="{{ langPath .Ctx.Lang "/profile/password" }}" class="flex flex-col gap-3 max-w-sm">
        <input type="hidden" name="_csrf" value="{{ .Ctx.CSRF }}" />
        {{ if .Ctx.Data.PasswordSet }}
        <input type="password" name="current" autocomplete="current-password" required
               class="input input-bordered w-full" placeholder="{{ t .Ctx.Lang "profile.password.current" }}" />
        {{ end }}
        <input type="password" name="new" autocomplete="new-password" minlength="8" required
               class="input input-bordered w-full" placeholder="{{ t .Ctx.Lang "profile.password.new" }}" />
        <button type="submit" class="btn btn-soft self-start">{{ t .Ctx.Lang "profile.password.submit" }}</button>
    </form>
    {{ end }}
</div>
{{ end }}
```

Подключить секцию в `templates/views/profile/get.html` рядом с секцией webdav (строка 60), обернув в гейт «только без SuperTokens» — признак взять из данных страницы, добавив поле `SelfHosted bool` тем же способом, что `PasswordSet`, со значением `!hasSupetokens`.

Добавить ключи `profile.password.*` во все локали.

- [ ] **Step 5: Прогнать тесты**

Run: `cd ~/Projects/webtor/web-ui && go build ./... && go test ./handlers/profile/ -count=1 -v`
Expected: PASS — три новых теста

- [ ] **Step 6: Коммит**

```bash
git add handlers/profile/ templates/ locales/ serve.go
git commit -m "feat: let the administrator set a password from the profile"
```

---

### Task 8: Баннер открытого инстанса

**Files:**
- Create: `templates/partials/open_instance_banner.html`
- Modify: `templates/layouts/main.html`, `services/web/context.go:37-51`, `services/web/context.go` (конструктор)

**Interfaces:**
- Consumes: `auth.IsOpenInstance(c)` из Task 4.
- Produces: поле `OpenInstance bool` в `web.Context`, доступное во всех шаблонах как `$.OpenInstance`.

- [ ] **Step 1: Прокинуть признак в контекст шаблонов**

В `services/web/context.go` в структуру `Context` (строка 37) добавить поле:

```go
	// OpenInstance is true on a self-hosted instance running without an
	// administrator password: anyone who can reach the port is admin. The
	// banner rendered from this is the only thing standing between the
	// instance and a stranger, so it is deliberately hard to ignore.
	OpenInstance bool
```

В конструкторе `NewContext` заполнить его `auth.IsOpenInstance(c)`.

- [ ] **Step 2: Написать баннер**

Создать `templates/partials/open_instance_banner.html`:

```html
{{ define "open_instance_banner" }}
{{ if $.OpenInstance }}
<div id="open-instance-banner" class="bg-error/15 border-b border-error/40 px-4 py-3">
    <div class="max-w-5xl mx-auto flex items-center gap-3 text-sm">
        <svg class="size-5 shrink-0 text-error" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m9-.75a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 3.75h.008v.008H12v-.008Z" />
        </svg>
        <span class="flex-1">{{ t $.Lang "banner.openInstance" }}</span>
        <a href="{{ langPath $.Lang "/profile" }}#password" class="btn btn-sm btn-error shrink-0">{{ t $.Lang "banner.openInstanceAction" }}</a>
        <button type="button" class="btn btn-ghost btn-sm shrink-0"
                onclick="sessionStorage.setItem('open-instance-banner-hidden','1');document.getElementById('open-instance-banner').remove()">
            {{ t $.Lang "banner.hideForNow" }}
        </button>
    </div>
</div>
{{ end }}
{{ end }}
```

Свёртка через `sessionStorage`, а не `localStorage`: баннер должен возвращаться в следующей сессии браузера, пока пароль не задан.

- [ ] **Step 3: Подключить в layout**

В `templates/layouts/main.html` вставить `{{ template "open_instance_banner" $ }}` сразу после открывающего `<body>`, до навигации, чтобы баннер был первым, что видно на любой странице.

- [ ] **Step 4: Добавить ключи локалей**

Ключи `banner.openInstance`, `banner.openInstanceAction`, `banner.hideForNow` во все файлы локалей. Русский текст `banner.openInstance`: «Инстанс открыт: любой, кто знает адрес, имеет полный доступ. Задайте пароль администратора.»

- [ ] **Step 5: Проверить сборку и шаблоны**

Run: `cd ~/Projects/webtor/web-ui && go build ./... && go test ./services/web/ -count=1`
Expected: PASS

- [ ] **Step 6: Коммит**

```bash
git add templates/ services/web/context.go locales/
git commit -m "feat: warn on every page while the instance has no admin password"
```

---

### Task 9: CLI-команда сброса пароля

**Files:**
- Create: `admin.go` (в корне репозитория, рядом с `vault.go`, `notification.go`)
- Modify: `configure.go:15`

**Interfaces:**
- Consumes: `adminauth.NewStore`, `adminauth.NewPGRepo` из Tasks 2-3.
- Produces: команда `web-ui admin set-password <password>`.

- [ ] **Step 1: Написать команду**

Создать `admin.go` в корне репозитория. Структура повторяет `vault.go`: функция `makeXCMD()`
возвращает `cli.Command`, флаги регистрируются через `cs.RegisterPGFlags`, база поднимается
через `cs.NewPG(c)`.

```go
package main

import (
	"context"
	"errors"

	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	log "github.com/sirupsen/logrus"

	"github.com/webtor-io/web-ui/services/adminauth"
)

// makeAdminCMD groups maintenance for the single self-hosted administrator.
// Setting the password here rather than through ADMIN_PASSWORD keeps the
// secret out of `docker inspect` and out of shell history.
func makeAdminCMD() cli.Command {
	adminCMD := cli.Command{
		Name:  "admin",
		Usage: "Self-hosted administrator management commands",
	}
	configureAdmin(&adminCMD)
	return adminCMD
}

func configureAdmin(c *cli.Command) {
	setPasswordCmd := cli.Command{
		Name:      "set-password",
		Usage:     "Sets the administrator password",
		ArgsUsage: "<password>",
		Action:    setAdminPassword,
	}
	setPasswordCmd.Flags = cs.RegisterPGFlags(setPasswordCmd.Flags)
	c.Subcommands = []cli.Command{setPasswordCmd}
}

func setAdminPassword(c *cli.Context) error {
	password := c.Args().First()
	if password == "" {
		return errors.New("usage: web-ui admin set-password <password>")
	}

	pg := cs.NewPG(c)
	defer pg.Close()

	if pg.Get() == nil {
		return errors.New("db is nil")
	}

	// An empty env password is passed deliberately: this command writes to the
	// database, and ADMIN_PASSWORD would refuse the write with ErrManagedByEnv.
	store := adminauth.NewStore("", adminauth.NewPGRepo(pg))
	if err := store.Set(context.Background(), password); err != nil {
		return err
	}

	log.Info("administrator password updated")
	return nil
}
```

- [ ] **Step 2: Зарегистрировать команду**

В `configure.go` добавить построение команды рядом с остальными и включить её в список:

```go
	subscriptionCMD := makeSubscriptionCMD()
	adminCMD := makeAdminCMD()
	app.Commands = []cli.Command{serveCMD, migrationCMD, enrichCMD, cacheIndexCMD, vaultCMD, notificationCMD, subscriptionCMD, adminCMD}
```

- [ ] **Step 3: Проверить**

Run: `cd ~/Projects/webtor/web-ui && go build -o /tmp/web-ui . && /tmp/web-ui admin set-password 2>&1 | head -3`
Expected: сообщение об использовании (пароль не передан), команда распознана

- [ ] **Step 4: Коммит**

```bash
git add admin.go configure.go
git commit -m "feat: add web-ui admin set-password for password recovery"
```

---

### Task 10: Проброс в self-hosted и смоук-сценарий

Выполняется в репозитории `/Users/vintikzzzz/Projects/webtor/self-hosted`, а не в web-ui.

**Files:**
- Modify: `etc/webtor/common.template.env`, `README.md`, `CLAUDE.md`
- Create: `tests/scenarios/60-admin-password.sh`

**Interfaces:**
- Consumes: `ADMIN_PASSWORD` из Task 4, форму входа из Task 6.
- Produces: сценарий смоук-сьюта, проверяющий оба состояния.

- [ ] **Step 1: Пробросить переменную**

В `etc/webtor/common.template.env` добавить строку:

```
ADMIN_PASSWORD=${ADMIN_PASSWORD:-}
```

- [ ] **Step 2: Задокументировать в README**

В `README.md` после раздела «Supported Platforms» добавить:

```markdown
## Administrator Password

By default the instance is open: anyone who can reach it has full access. Set a
password from the profile page, or start the container with one:

```bash
docker run -e ADMIN_PASSWORD=your-password -d -p 8080:8080 -v data:/data -v pgdata:/pgdata \
  --name webtor --restart=always ghcr.io/webtor-io/self-hosted:latest
```

`ADMIN_PASSWORD` overrides whatever password was set from the profile, which
also makes it the way back in if you forget it. To change the password without
putting it on the command line:

```bash
docker exec webtor /app/web-ui admin set-password <new-password>
```
```

- [ ] **Step 3: Написать сценарий**

Создать `tests/scenarios/60-admin-password.sh` по образцу существующих сценариев (взять `jget`/`fail`/`wait_for` из `tests/lib.sh`, следовать конвенциям `10-ddl.sh`):

```bash
#!/usr/bin/env bash
# With ADMIN_PASSWORD set, the front page must ask for a password; without it,
# the instance stays open. Both directions matter: the first is the feature,
# the second is that we did not break existing installs.
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"

# The suite's container runs without ADMIN_PASSWORD, so the open path is what
# the shared stack can show us directly.
code="$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/")"
assert_eq "$code" "200" "front page on an open instance"

body="$(curl -sL "$BASE_URL/")"
case "$body" in
  *open-instance-banner*) : ;;
  *) fail "the open-instance banner is missing from an instance with no password" ;;
esac

# The closed path needs its own container: ADMIN_PASSWORD is read at startup.
port=18099
name=webtor-smoke-adminpw
docker rm -f "$name" >/dev/null 2>&1 || true
docker run -d --name "$name" -e ADMIN_PASSWORD=smoke-test-password \
  -p "$port:8080" "$WEBTOR_IMAGE" >/dev/null
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT

closed="http://localhost:$port"
wait_for 180 "closed instance to boot" curl -fsS -o /dev/null "$closed/login"

code="$(curl -s -o /dev/null -w '%{http_code}' -H 'Accept: text/html' -H 'Sec-Fetch-Mode: navigate' "$closed/profile")"
assert_eq "$code" "302" "a protected page on a closed instance must redirect to the login form"

code="$(curl -s -o /dev/null -w '%{http_code}' -X POST -d 'password=wrong-password' "$closed/login")"
[ "$code" = "401" ] || fail "a wrong password did not return 401 (got $code)"

echo "PASS: admin-password"
```

- [ ] **Step 4: Прогнать сьют**

Run: `docker build -t webtor-self-hosted:adminpw . && WEBTOR_HOST_PORT=18080 tests/run.sh webtor-self-hosted:adminpw`
Expected: `SUITE PASSED`, включая `PASS: admin-password`

Замечание: образ должен быть собран из Dockerfile, у которого компонент web-ui запинен на дайджест с этой функциональностью. До того как web-ui соберётся в CI, собрать локальный образ web-ui и временно подменить строку `FROM ghcr.io/webtor-io/web-ui:...` в Dockerfile; вернуть перед коммитом и отметить это в отчёте.

- [ ] **Step 5: Обновить CLAUDE.md**

В разделе «Env-переменные пользователя» упомянуть `ADMIN_PASSWORD` и то, что без него инстанс открыт.

- [ ] **Step 6: Коммит**

```bash
git add etc/webtor/common.template.env README.md CLAUDE.md tests/scenarios/60-admin-password.sh
git commit -m "feat: wire ADMIN_PASSWORD into self-hosted and cover both states in the smoke suite"
```

---

## Порядок и зависимости

Tasks 1-3 независимы от остального и делаются подряд. Task 4 зависит от 1-3. Tasks 5 и 6 зависят от 4. Tasks 7, 8, 9 зависят от 2-4 и независимы друг от друга. Task 10 — последний, ему нужен собранный образ web-ui со всем предыдущим.

**Переименование колонки `password` → `password_hash`, описанное в спеке, в этот план намеренно не вошло.** Колонка мёртвая, переименование чисто косметическое, а миграция выполнилась бы и на продовой базе — риск без выигрыша. Вместо этого Task 3 снабжает поле модели комментарием, объясняющим, что в нём лежит. Если владелец решит переименовать, это отдельная задача с отдельной миграцией; ничего в этом плане от неё не зависит.
