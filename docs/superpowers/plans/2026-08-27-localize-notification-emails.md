# Localize Notification Emails Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every email web-ui sends renders in the recipient's language, not hardcoded English — starting with the email-verification letter (the reported bug), then the four vault letters (vaulted / expiring / expired / transfer-timeout).

**Architecture:** The i18n plumbing already exists (`SendOptions.Lang`, `Service.T`, `t`/`tp` template funcs, per-account `user_settings.lang`) and is used by the three subscription letters. This plan connects the remaining five letters to it: the verification letter takes the language from the live request (`i18n.GetLang(c)`); the vault letters resolve it from the account via a new `AccountLang` method on the notification store (mirroring `release_subscription`'s resolver). Subjects go through `Service.T`, bodies through `{{ t }}`/`{{ tp }}` keys added to the locale files.

**Tech Stack:** Go, html/template, nicksnyder/go-i18n via `services/i18n`, flat JSON locales (`locales/*.json`, 11 languages).

**Spec:** No separate spec file. The audit that motivates this plan: `services/notification/notification.go` — `SendEmailVerification` (line ~543), `SendVaulted` (~436), `SendExpiring` (~461), `SendTransferTimeout` (~477), `SendExpired` (~551) all send hardcoded English; templates `verify-email.html`, `vaulted.html`, `expiring.html`, `expired.html`, `transfer-timeout.html` contain zero `t`/`tp` calls. Subscription letters (`subscription.go`) are the reference implementation of "done right".

## Global Constraints

- Repo: `/Users/vintikzzzz/Projects/webtor/web-ui`. Branch: `feat/localize-notification-emails` (created before Task 1; all commits go there).
- Do NOT change the dedupe/feed guarantees in `Service.Send`, the mailOnly-never-writes-the-feed property, or any of the load-bearing comments around them. Read the comments before editing; keep them accurate.
- `Service.T(lang, key, args...)` with a nil i18n bundle returns the key verbatim (no interpolation). Unit tests built with `newTestService` have a nil bundle — after this change they assert *keys*, not English sentences.
- Locale fallback: a key missing from a locale falls back to the default language (en), and to the key itself only when no locale has it. So en+ru are added in Tasks 1–3; the other nine languages in Task 4.
- Locale files are flat JSON (no plural forms). For "N days" in languages with declension use an abbreviation, precedent: `"profile.payments.days": "{{.Days}} дн."` in ru.json.
- Known accepted limitation (do not fix here): `SendTransferTimeout`'s `{{.Timeout}}` value comes from `durafmt` and is English ("2 days") regardless of letter language. Leave a one-line comment at the `durafmt.Parse` call noting this.
- The escaping property (escaping_test.go) must survive: resource names keep flowing through go-i18n's text/template into a string that html/template escapes as a text node. Never mark translated output as `template.HTML`.
- Verification after each task: `go build ./... && go vet ./...` plus the named test packages. Full `go test ./...` in Task 4.
- Commit messages end with: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`

---

### Task 1: Localize the email-verification letter

**Files:**
- Modify: `services/notification/notification.go` (`mailOnly`, `SendEmailVerification`)
- Modify: `handlers/profile/handler.go` (~line 714, the `SendEmailVerification` call in `setEmail`)
- Modify: `templates/notification/verify-email.html`
- Modify: `locales/en.json`, `locales/ru.json` (3 keys each, next to the existing `email.*` block — en.json ~line 1778, ru.json ~line 1703)
- Test: `services/notification/notification_test.go`

**Interfaces:**
- Consumes: existing `Service.render(templateName, lang string, data any)`, `Service.wrapEmail(body, lang string)`, `Service.T(lang, key string, args ...any) string`, `i18n.GetLang(c *gin.Context) string`, `notification.NewWith(store, mail, i18nSvc, domain, templateDir)`.
- Produces: `func (s *Service) SendEmailVerification(to, link, lang string) error` and `func (s *Service) mailOnly(to, subject, templateName, lang string, data any) error`. Later tasks do not depend on these; handlers/profile is the only caller.

- [ ] **Step 1: Write the failing test**

Append to `services/notification/notification_test.go` (imports to add: `os` is already imported; add `"github.com/webtor-io/web-ui/services/i18n"`):

```go
// TestSendEmailVerificationLocalized pins the reported bug: an account
// browsing in Russian got the verification letter in English. Real
// templates, real locale bundle -- only the transport is a fake.
func TestSendEmailVerificationLocalized(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	i18nSvc := i18n.New(locales.FS())

	mail := &mockMailer{}
	svc := NewWith(&mockStore{}, mail, i18nSvc, "https://webtor.io", "../../templates/notification")

	if err := svc.SendEmailVerification("user@example.com", verifyLink, "ru"); err != nil {
		t.Fatalf("send verification: %v", err)
	}
	if len(mail.calls) != 1 {
		t.Fatalf("letters sent: got %d, want 1", len(mail.calls))
	}
	if mail.calls[0].subject != "Подтвердите почту для уведомлений" {
		t.Errorf("subject not localized: %q", mail.calls[0].subject)
	}
	if !strings.Contains(mail.calls[0].body, "Подтвердите этот адрес") {
		t.Errorf("body not localized:\n%s", mail.calls[0].body)
	}
	if !strings.Contains(mail.calls[0].body, verifyLink) {
		t.Errorf("the letter lost the verification link:\n%s", mail.calls[0].body)
	}

	// Negative control built in: an account with no observed language gets
	// the default (English) letter, not the key and not Russian.
	mail.calls = nil
	if err := svc.SendEmailVerification("user@example.com", verifyLink, ""); err != nil {
		t.Fatalf("send verification (default lang): %v", err)
	}
	if mail.calls[0].subject != "Confirm your notification email" {
		t.Errorf("default-language subject: %q", mail.calls[0].subject)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./services/notification/ -run TestSendEmailVerificationLocalized -v`
Expected: compile FAIL — `too many arguments in call to svc.SendEmailVerification` (signature is still 2-arg).

- [ ] **Step 3: Implement**

In `services/notification/notification.go`:

`mailOnly` gains a `lang` parameter and passes it through to both render calls (the doc comment stays, it is still accurate):

```go
func (s *Service) mailOnly(to, subject, templateName, lang string, data any) error {
	if !Deliverable(to) || !s.hasMail() {
		return nil
	}
	body, err := s.render(templateName, lang, data)
	if err != nil {
		return errors.Wrap(err, "failed to render notification template")
	}
	// Never reaches the feed, but it is still a letter, so it still needs to
	// be a document -- the layout is what makes it one.
	letter, err := s.wrapEmail(body, lang)
	if err != nil {
		return errors.Wrap(err, "failed to render email layout")
	}
	if err := s.mail.Send(to, subject, letter); err != nil {
		return errors.Wrap(err, "failed to send email")
	}
	return nil
}
```

`SendEmailVerification` gains `lang` and a translated subject (extend its doc comment with one sentence: the language is the one the request was browsing in — this letter is sent from a live handler, so no account lookup is needed):

```go
func (s *Service) SendEmailVerification(to, link, lang string) error {
	return s.mailOnly(to, s.T(lang, "email.verify.subject"), "verify-email.html", lang, map[string]any{
		"Link": link,
	})
}
```

In `handlers/profile/handler.go` `setEmail` (the package already imports `services/i18n`):

```go
		if err := s.notification.SendEmailVerification(email, link, i18n.GetLang(c)); err != nil {
```

Replace `templates/notification/verify-email.html` with:

```html
<p>{{ t "email.verify.text" }}</p>
<p><a href="{{ .Link }}">{{ .Link }}</a></p>
<p>{{ t "email.verify.expires" }}</p>
```

Add to `locales/en.json` (inside the `email.*` block, keep the file's 4-space indent and key style):

```json
    "email.verify.subject": "Confirm your notification email",
    "email.verify.text": "Confirm this email address to receive notifications from Webtor.",
    "email.verify.expires": "This link expires in 24 hours. If you did not request this, you can ignore this email.",
```

Add to `locales/ru.json`:

```json
    "email.verify.subject": "Подтвердите почту для уведомлений",
    "email.verify.text": "Подтвердите этот адрес, чтобы получать уведомления от Webtor.",
    "email.verify.expires": "Ссылка действует 24 часа. Если вы не запрашивали подтверждение, просто проигнорируйте это письмо.",
```

Fix the two existing verification tests' calls: `svc.SendEmailVerification("user@example.com", verifyLink, "")`. Their assertions do not touch the subject; leave them otherwise unchanged.

- [ ] **Step 4: Verify**

Run: `go build ./... && go vet ./... && go test ./services/notification/ ./handlers/profile/ -v -run 'TestSendEmailVerification|TestEmailSection|TestRendered'`
Expected: PASS (all three verification tests, profile email-section tests, escaping tests). If `handlers/profile` tests fail to compile because a fake calls the old 2-arg signature, update the fake's call — nothing else.

Then validate both JSON files: `python3 -c "import json; json.load(open('locales/en.json')); json.load(open('locales/ru.json'))"`

- [ ] **Step 5: Commit**

```bash
git add services/notification/notification.go services/notification/notification_test.go handlers/profile/handler.go templates/notification/verify-email.html locales/en.json locales/ru.json
git commit -m "fix(i18n): send the email-verification letter in the requester's language"
```

---

### Task 2: Resolve the account language for vault letters; localize their subjects

**Files:**
- Modify: `services/notification/store.go` (interface + `pgNotificationStore`)
- Modify: `services/notification/notification.go` (`SendVaulted`, `SendExpiring`, `SendTransferTimeout`, `SendExpired`)
- Modify: `services/notification/notification_test.go` (`mockStore` + Title assertions)
- Modify: `locales/en.json`, `locales/ru.json` (4 subject keys)

**Interfaces:**
- Consumes: `models.GetUserSettings(ctx, db, userID) (*models.UserSettings, error)` and `(*models.UserSettings).GetLang() string` (nil-safe); `Service.T`.
- Produces: `AccountLang(ctx context.Context, userID uuid.UUID) string` on the `notificationStore` interface (and on `mockStore`, backed by a plain `accountLang string` field). Task 3's tests rely on `mockStore.accountLang`.

- [ ] **Step 1: Write the failing test**

Append to `services/notification/notification_test.go`:

```go
// TestSendVaultedUsesAccountLanguage pins the resolver wiring end to end:
// the language stored on the account (user_settings.lang, here via the
// mock's accountLang) must reach both the letter's subject and the feed
// row's title. Real locale bundle, real templates.
func TestSendVaultedUsesAccountLanguage(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	i18nSvc := i18n.New(locales.FS())

	store := &mockStore{accountLang: "ru"}
	mail := &mockMailer{}
	svc := NewWith(store, mail, i18nSvc, "https://webtor.io", "../../templates/notification")

	r := &vaultModels.Resource{ResourceID: "abc123", Name: "My Torrent"}
	if err := svc.SendVaulted("user@example.com", testUserID, r); err != nil {
		t.Fatalf("send vaulted: %v", err)
	}
	if len(mail.calls) != 1 {
		t.Fatalf("letters sent: got %d, want 1", len(mail.calls))
	}
	wantSubject := "Ваш ресурс My Torrent сохранён в Vault!"
	if mail.calls[0].subject != wantSubject {
		t.Errorf("subject: got %q, want %q", mail.calls[0].subject, wantSubject)
	}
	if store.created == nil || store.created.Title != wantSubject {
		t.Errorf("feed title not localized: %+v", store.created)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./services/notification/ -run TestSendVaultedUsesAccountLanguage -v`
Expected: compile FAIL — `unknown field accountLang in struct literal` (and, once added, `mockStore` does not yet implement `AccountLang`).

- [ ] **Step 3: Implement**

`services/notification/store.go` — add to the `notificationStore` interface:

```go
	// AccountLang returns the language the account browses in
	// (user_settings.lang), or "" when it has never been observed. It is
	// what lets a letter assembled far from any HTTP request -- a NATS
	// event handler, a cron run -- speak the reader's language. "" falls
	// back to the default language, never to an error: a lookup failure
	// must not stop a send.
	AccountLang(ctx context.Context, userID uuid.UUID) string
```

and the implementation (imports: `models` is already there; add logrus as `log` if absent — check the file's existing import style first):

```go
// AccountLang mirrors release_subscription's pgStore.AccountLang (the same
// question asked by subscription letters); errors are swallowed because the
// caller always has a fallback -- the default language.
func (s *pgNotificationStore) AccountLang(ctx context.Context, userID uuid.UUID) string {
	us, err := models.GetUserSettings(ctx, s.db, userID)
	if err != nil {
		log.WithError(err).WithField("user_id", userID).Warn("failed to read account language for notification")
		return ""
	}
	return us.GetLang()
}
```

(Adjust `s.db` to the actual field name/type in `pgNotificationStore` — read the struct first; if its db handle is obtained per-call, follow the pattern its other methods use.)

`services/notification/notification_test.go` — extend `mockStore`:

```go
	// accountLang answers AccountLang -- the language "stored" on the
	// account a test pretends to have.
	accountLang string
```

```go
func (m *mockStore) AccountLang(_ context.Context, _ uuid.UUID) string {
	return m.accountLang
}
```

`services/notification/notification.go` — each of the four senders resolves the language once and uses it for both `Lang` and `Title`. Pattern (repeat for all four; `ctx := context.Background()` at the top of each):

```go
func (s *Service) SendVaulted(to string, userID uuid.UUID, r *vaultModels.Resource) error {
	lang := s.store.AccountLang(context.Background(), userID)
	opts := SendOptions{
		To:       to,
		UserID:   userID,
		Lang:     lang,
		Key:      fmt.Sprintf("vaulted-%s", r.ResourceID),
		Title:    s.T(lang, "email.vaulted.subject", "Name", r.Name),
		Template: "vaulted.html",
		Data:     s.resourceData(r),
	}
	return s.Send(opts)
}
```

The other three titles:
- `SendExpiring`: `s.T(lang, "email.expiring.subject", "Days", days)`
- `SendTransferTimeout`: `s.T(lang, "email.transfer_timeout.subject", "Name", r.Name)`
- `SendExpired`: `s.T(lang, "email.expired.subject", "Name", r.Name)`

Locale keys — `locales/en.json` (these reproduce today's `fmt.Sprintf` strings exactly, including the pre-existing "1 days" grammar for Days=1; not a regression, do not "fix" it here):

```json
    "email.vaulted.subject": "Your resource {{.Name}} has been vaulted!",
    "email.expiring.subject": "Your resources will disappear in {{.Days}} days!",
    "email.transfer_timeout.subject": "We were unable to transfer your resource {{.Name}}",
    "email.expired.subject": "Your resource {{.Name}} has expired",
```

`locales/ru.json`:

```json
    "email.vaulted.subject": "Ваш ресурс {{.Name}} сохранён в Vault!",
    "email.expiring.subject": "Ваши ресурсы исчезнут через {{.Days}} дн.!",
    "email.transfer_timeout.subject": "Не удалось перенести ваш ресурс {{.Name}}",
    "email.expired.subject": "Срок хранения ресурса {{.Name}} истёк",
```

Existing unit tests (`TestSendVaulted`, `TestSendExpiring`, `TestSendTransferTimeout`, `TestSendExpired`, and any other test asserting these Titles) run with a nil i18n bundle, where `T` returns the key verbatim. Update their Title expectations to the key strings (e.g. `store.created.Title != "email.vaulted.subject"`). Do not weaken any other assertion.

- [ ] **Step 4: Verify**

Run: `go build ./... && go vet ./... && go test ./services/notification/ ./handlers/event/ . -count=1`
Expected: PASS. (`handlers/event` and the root package compile against `notificationStore` via interfaces `vaultedNotifier`/`reaperNotification`, which did not change — this run proves it.)

Negative control for the resolver: temporarily hardcode `lang := ""` in `SendVaulted`, run `go test ./services/notification/ -run TestSendVaultedUsesAccountLanguage` — must FAIL (English subject). Revert the hardcode, re-run — PASS. State the result in the task report.

- [ ] **Step 5: Commit**

```bash
git add services/notification/store.go services/notification/notification.go services/notification/notification_test.go locales/en.json locales/ru.json
git commit -m "feat(i18n): vault letters resolve the account language and localize their subjects"
```

---

### Task 3: Localize the four vault letter bodies

**Files:**
- Modify: `templates/notification/vaulted.html`, `expiring.html`, `expired.html`, `transfer-timeout.html`
- Modify: `locales/en.json`, `locales/ru.json` (10 body keys)
- Modify: `services/notification/notification.go` (one comment at the durafmt call)
- Modify: `services/notification/escaping_test.go` (hostile-names test gets the real bundle — see Step 3)
- Test: `services/notification/render_test.go`

**Interfaces:**
- Consumes: `mockStore.accountLang` (Task 2), template funcs `t`/`tp`, `Service.resourceData`.
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Write the failing test**

Append to `services/notification/render_test.go` (add imports: `os`, `vaultModels "github.com/webtor-io/web-ui/models/vault"`, `"github.com/webtor-io/web-ui/services/i18n"`):

```go
// TestVaultTemplatesRenderLocalized drives every vault letter through the
// real templates and the real Russian locale -- the pair of things
// TestSubscriptionTemplatesRender deliberately does not cover (it pins the
// no-bundle fallback instead). One Russian word per template is enough: it
// proves the body went through the bundle, without turning the test into a
// second copy of ru.json.
func TestVaultTemplatesRenderLocalized(t *testing.T) {
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	defer locales.Close()
	s := &Service{
		templateDir: "../../templates/notification",
		domain:      "https://webtor.io",
		i18n:        i18n.New(locales.FS()),
	}
	res := &vaultModels.Resource{ResourceID: "abc123", Name: "My Torrent"}

	for _, tt := range []struct {
		template string
		data     any
		want     []string
	}{
		{"vaulted.html", s.resourceData(res), []string{"сохранён в Vault", "My Torrent", "https://webtor.io/abc123"}},
		{"expired.html", s.resourceData(res), []string{"истёк", "My Torrent"}},
		{"transfer-timeout.html", func() map[string]any {
			d := s.resourceData(res)
			d["Timeout"] = "2 days"
			return d
		}(), []string{"сидеров", "My Torrent", "2 days"}},
		{"expiring.html", map[string]any{
			"Days":      3,
			"Resources": []expiringResource{{Name: "My Torrent", URL: "https://webtor.io/abc123"}},
			"Domain":    "https://webtor.io",
		}, []string{"исчезнут", "My Torrent"}},
	} {
		t.Run(tt.template, func(t *testing.T) {
			body, err := s.render(tt.template, "ru", tt.data)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			for _, w := range tt.want {
				if !strings.Contains(body, w) {
					t.Errorf("body lacks %q:\n%s", w, body)
				}
			}
			if strings.Contains(body, "email.") {
				t.Errorf("body carries a raw message key -- a key is missing from the bundle:\n%s", body)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./services/notification/ -run TestVaultTemplatesRenderLocalized -v`
Expected: FAIL — bodies are hardcoded English, the Russian words are absent.

- [ ] **Step 3: Implement**

`templates/notification/vaulted.html`:

```html
<p>{{ tp "email.vaulted.heading" "Name" .Name }}</p>
<p>{{ t "email.vaulted.access" }} <a href="{{ .URL }}">{{ .URL }}</a></p>
```

`templates/notification/expiring.html`:

```html
<p>{{ tp "email.expiring.heading" "Days" .Days }}</p>
<ul>
    {{ range .Resources }}
    <li><a href="{{ .URL }}">{{ .Name }}</a></li>
    {{ end }}
</ul>
```

`templates/notification/expired.html`:

```html
<p>{{ tp "email.expired.heading" "Name" .Name }}</p>
<p>{{ t "email.expired.removed" }}</p>
<p>{{ t "email.resource.link" }} <a href="{{ .URL }}">{{ .URL }}</a></p>
```

`templates/notification/transfer-timeout.html`:

```html
<p>{{ tp "email.transfer_timeout.heading" "Name" .Name "Timeout" .Timeout }}</p>
<p>{{ t "email.transfer_timeout.no_seeders" }}</p>
<p>{{ t "email.transfer_timeout.points_returned" }}</p>
<p>{{ t "email.resource.link" }} <a href="{{ .URL }}">{{ .URL }}</a></p>
<p>{{ t "email.transfer_timeout.try_again" }}</p>
```

(Note: the resource-link line moves before try_again so the letter ends on the actionable suggestion; if the reviewer prefers the original order, keep the original order — the keys are the same either way. The `<strong>` around names is intentionally dropped: the name now sits inside a translated sentence, same trade subscription letters already made.)

`locales/en.json` body keys:

```json
    "email.vaulted.heading": "Your resource {{.Name}} has been vaulted!",
    "email.vaulted.access": "You can access it here:",
    "email.expiring.heading": "Your resources will disappear in {{.Days}} days:",
    "email.expired.heading": "Your resource {{.Name}} has expired.",
    "email.expired.removed": "Unfortunately, the torrent has been removed from the Vault. If you still need it, simply pledge again.",
    "email.resource.link": "Resource link:",
    "email.transfer_timeout.heading": "We were unable to transfer your resource {{.Name}} to the Vault within {{.Timeout}}.",
    "email.transfer_timeout.no_seeders": "There were no seeders available to download the data from.",
    "email.transfer_timeout.points_returned": "All your Vault points have been returned.",
    "email.transfer_timeout.try_again": "Please try to find another torrent or try again later.",
```

`locales/ru.json`:

```json
    "email.vaulted.heading": "Ваш ресурс {{.Name}} сохранён в Vault!",
    "email.vaulted.access": "Он доступен по ссылке:",
    "email.expiring.heading": "Ваши ресурсы исчезнут через {{.Days}} дн.:",
    "email.expired.heading": "Срок хранения ресурса {{.Name}} истёк.",
    "email.expired.removed": "К сожалению, торрент удалён из Vault. Если он вам ещё нужен, просто внесите пледж снова.",
    "email.resource.link": "Ссылка на ресурс:",
    "email.transfer_timeout.heading": "Мы не смогли перенести ваш ресурс {{.Name}} в Vault за {{.Timeout}}.",
    "email.transfer_timeout.no_seeders": "Не нашлось сидеров, у которых можно было бы скачать данные.",
    "email.transfer_timeout.points_returned": "Все ваши очки Vault возвращены.",
    "email.transfer_timeout.try_again": "Попробуйте найти другой торрент или повторите попытку позже.",
```

**Update the hostile-names escaping test** (`services/notification/escaping_test.go`, `TestRenderedBodyEscapesHostileNames`): it drives `SendVaulted` against the real templates with a nil i18n bundle. After this task the resource name reaches the body only through `tp`, and with a nil bundle `tp` returns the bare key — the hostile name would never enter the body and the `wantEscaped` assertions would fail for the wrong reason. Rebuild its service with the real bundle so the guard runs the same path production runs (go-i18n interpolates the name into the translated sentence, html/template escapes it on output):

```go
			locales, err := os.OpenRoot("../../locales")
			if err != nil {
				t.Fatalf("locales: %v", err)
			}
			defer locales.Close()
			store := &mockStore{}
			mail := &mockMailer{}
			// The real bundle, not nil: since the templates were localized,
			// a hostile name reaches the body through a translated sentence
			// (go-i18n's text/template interpolates it, html/template
			// escapes it on output). A nil bundle would drop the name
			// entirely -- trivially safe, and testing nothing.
			svc := NewWith(store, mail, i18n.New(locales.FS()), "https://webtor.io", "../../templates/notification")
```

(add `os` and `"github.com/webtor-io/web-ui/services/i18n"` to that file's imports; keep every assertion and the negative-control comment unchanged).

In `services/notification/notification.go`, at the `durafmt.Parse(...)` line in `SendTransferTimeout`, add:

```go
	// durafmt speaks English only; {{.Timeout}} stays "2 days" in every
	// language. Accepted for now -- the letter around it is localized.
```

- [ ] **Step 4: Verify**

Run: `go test ./services/notification/ -count=1 -v -run 'TestVaultTemplates|TestRendered|TestSubscriptionTemplates|TestSend'`
Expected: PASS — including `TestRenderedBodyEscapesHostileNames` (hostile names still come out escaped: `tp` output is a plain string that html/template escapes as a text node) and the fragments test in escaping_test.go (nil-bundle render falls back to keys, still no `<no value>`, no document tags).

Then `go build ./... && go vet ./...` and JSON validation of both locale files.

- [ ] **Step 5: Commit**

```bash
git add templates/notification/vaulted.html templates/notification/expiring.html templates/notification/expired.html templates/notification/transfer-timeout.html locales/en.json locales/ru.json services/notification/notification.go services/notification/render_test.go services/notification/escaping_test.go
git commit -m "feat(i18n): localize the vault letter bodies"
```

---

### Task 4: Translate the new keys into the remaining nine locales; full verification

**Files:**
- Modify: `locales/cs.json`, `locales/de.json`, `locales/es.json`, `locales/fr.json`, `locales/it.json`, `locales/nl.json`, `locales/pl.json`, `locales/pt.json`, `locales/tr.json`

**Interfaces:**
- Consumes: the 17 en keys added in Tasks 1–3 (`email.verify.{subject,text,expires}`, `email.{vaulted,expiring,transfer_timeout,expired}.subject`, `email.vaulted.{heading,access}`, `email.expiring.heading`, `email.expired.{heading,removed}`, `email.resource.link`, `email.transfer_timeout.{heading,no_seeders,points_returned,try_again}`).
- Produces: nothing.

- [ ] **Step 1: Write the failing check**

A shell check, not a Go test (there is no locale-parity test in this repo and this plan does not introduce one):

```bash
cd /Users/vintikzzzz/Projects/webtor/web-ui && python3 - <<'EOF'
import json, sys
keys = [
 "email.verify.subject","email.verify.text","email.verify.expires",
 "email.vaulted.subject","email.vaulted.heading","email.vaulted.access",
 "email.expiring.subject","email.expiring.heading",
 "email.expired.subject","email.expired.heading","email.expired.removed",
 "email.resource.link",
 "email.transfer_timeout.subject","email.transfer_timeout.heading",
 "email.transfer_timeout.no_seeders","email.transfer_timeout.points_returned",
 "email.transfer_timeout.try_again",
]
bad = False
for lang in ["cs","de","es","fr","it","nl","pl","pt","tr","en","ru"]:
    d = json.load(open(f"locales/{lang}.json"))
    missing = [k for k in keys if k not in d]
    if missing:
        bad = True
        print(lang, "missing:", missing)
print("OK" if not bad else "FAIL")
sys.exit(1 if bad else 0)
EOF
```

Expected now: FAIL (nine languages missing all 17 keys).

- [ ] **Step 2: Add the translations**

For each of the nine files, add all 17 keys next to that file's existing `email.subscription.*` block, translating from the en values. Rules:
- Match the register and terminology of the surrounding translations in the same file (e.g. how that locale already renders "Vault", "torrent", "seeders" — grep the file first; if the file keeps "Vault" untranslated, keep it untranslated).
- `{{.Name}}`, `{{.Days}}`, `{{.Timeout}}` placeholders must survive verbatim.
- For `email.expiring.subject`/`.heading`, use a day-abbreviation where the language declines the noun (precedent: that locale's own `profile.payments.days` — copy its unit word).
- Keep JSON valid: 4-space indent, trailing commas exactly as the file's style demands.

- [ ] **Step 3: Verify**

Re-run the Step 1 check — expected `OK`. Then `python3 -c "import json;[json.load(open(f'locales/{l}.json')) for l in ['cs','de','es','fr','it','nl','pl','pt','tr','en','ru']]"`.

Full suite: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: PASS everywhere.

- [ ] **Step 4: Commit**

```bash
git add locales/
git commit -m "feat(i18n): translate the new email keys into the remaining nine locales"
```
