package notification

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/payments"
)

func newWinBackService(t *testing.T, store *mockStore, mail *mockMailer) *Service {
	t.Helper()
	body, err := os.ReadFile("../../templates/notification/winback.html")
	if err != nil {
		t.Fatal(err)
	}
	svc := newTestService(store, mail, setupTemplateDir(t, map[string]string{"winback.html": string(body)}))
	svc.i18n = i18n.New(os.DirFS("../../locales"))
	return svc
}

// A first-month code honoured until midnight at the start of 2027-03-22 in
// UTC+3 (21:00 UTC on the 21st).
func testCode() payments.Discount {
	return payments.Discount{
		Code:       "SAMPLE50",
		PercentOff: 50,
		PeriodDays: 30,
		ExpiresAt:  time.Date(2027, 3, 21, 21, 0, 0, 0, time.UTC),
	}
}

func assertBody(t *testing.T, body string, want []string, banned []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("body lacks %q:\n%s", w, body)
		}
	}
	for _, b := range banned {
		if strings.Contains(body, b) {
			t.Errorf("body must not contain %q:\n%s", b, body)
		}
	}
}

// The provider gave up on a declined card: the letter says so without naming
// a plan (the account lost the tier weeks earlier), shows the code on its
// own line and suggests another card.
func TestSendWinBack_PaymentFailed(t *testing.T) {
	store := &mockStore{}
	mail := &mockMailer{}
	svc := newWinBackService(t, store, mail)

	w := WinBack{Reason: WinBackPaymentFailed, CapMbps: 5, Discount: testCode(), CheckoutURL: "https://provider.example/checkout?rid=2"}
	if err := svc.SendWinBack("u@example.com", testUserID, w); err != nil {
		t.Fatal(err)
	}
	if store.created == nil || store.created.Key != "winback" {
		t.Fatalf("expected a winback entry, got %+v", store.created)
	}
	assertBody(t, store.created.Body, []string{
		"take the payment for your plan, so it is no longer active.",
		"capped at 5\u00a0Mbps",
		"promo code for 50% off your first month. Enter it at checkout",
		"<b>SAMPLE50</b>",
		"valid through 2027-03-20",
		`href="https://provider.example/checkout?rid=2"`,
		">Come back with 50% off<",
		"a different one at checkout",
		"/support?utm_source=webtor",
	}, []string{"email.", "free trial"})
	if got := store.created.Title; got != "Your payment didn't go through — 50% off your first month" {
		t.Errorf("subject: %q", got)
	}
	if len(mail.calls) != 1 || !store.markMailedCalled {
		t.Errorf("want one letter stamped mailed: mails=%d marked=%v", len(mail.calls), store.markMailedCalled)
	}
}

// A cancelled trial: the letter names the trial's plan, never mentions a
// card, and without a checkout the button leads to the storefront.
func TestSendWinBack_TrialEnded(t *testing.T) {
	store := &mockStore{accountLang: "ru"}
	svc := newWinBackService(t, store, &mockMailer{})

	if err := svc.SendWinBack("u@example.com", testUserID, WinBack{Reason: WinBackTrialEnded, Tier: "silver", CapMbps: 5, Discount: testCode()}); err != nil {
		t.Fatal(err)
	}
	assertBody(t, store.created.Body, []string{
		"Бесплатный пробный период тарифа Silver",
		"промокод на скидку 50% на первый месяц",
		"по 2027-03-20 включительно",
		`href="https://webtor.io/donate?utm_source=webtor`,
	}, []string{"email.", "карта", "Patreon не смог"})
	if got := store.created.Title; got != "Пробный период закончился — скидка 50% на первый месяц" {
		t.Errorf("subject: %q", got)
	}
}

func TestSendWinBack_YearCode(t *testing.T) {
	store := &mockStore{}
	svc := newWinBackService(t, store, &mockMailer{})
	d := testCode()
	d.PeriodDays = 365
	d.PercentOff = 30

	if err := svc.SendWinBack("u@example.com", testUserID, WinBack{Reason: WinBackTrialEnded, Discount: d}); err != nil {
		t.Fatal(err)
	}
	assertBody(t, store.created.Body, []string{"Your free trial has ended.", "30% off your first year"}, []string{"capped"})
	if got := store.created.Title; got != "Your free trial has ended — 30% off your first year" {
		t.Errorf("subject: %q", got)
	}
}

// Two events for one account pulled by two pods at once: both miss the
// earlier-entry check and both insert; the unique index rejects the second.
// The loser must neither fail nor mail — the letter belongs to the winner.
func TestSendWinBack_LosingTheInsertIsNotAnError(t *testing.T) {
	store := &mockStore{createErr: uniqueViolation{}}
	mail := &mockMailer{}
	svc := newWinBackService(t, store, mail)

	if err := svc.SendWinBack("u@example.com", testUserID, WinBack{Reason: WinBackPaymentFailed, Discount: testCode()}); err != nil {
		t.Fatalf("a unique violation must read as already handled, got %v", err)
	}
	if len(mail.calls) != 0 {
		t.Error("only the pod whose insert succeeded may mail")
	}
}

// A mailed entry ends it for good: nothing is written, claimed or sent.
func TestSendWinBack_OncePerAccount(t *testing.T) {
	mailed := time.Now().Add(-40 * 24 * time.Hour)
	store := &mockStore{last: &models.Notification{Key: "winback", MailedAt: &mailed, UpdatedAt: mailed}}
	mail := &mockMailer{}
	svc := newWinBackService(t, store, mail)

	if err := svc.SendWinBack("u@example.com", testUserID, WinBack{Reason: WinBackPaymentFailed, Discount: testCode()}); err != nil {
		t.Fatal(err)
	}
	if store.createCalls != 0 || store.claimCalls != 0 || len(mail.calls) != 0 {
		t.Errorf("a second letter: creates=%d claims=%d mails=%d", store.createCalls, store.claimCalls, len(mail.calls))
	}
}

// An entry whose letter never left ends it for events too: the events
// behind this letter come once per membership, and the owed letter is the
// daily cron's job (SendOwedWinBacks), not a second event's.
func TestSendWinBack_UnmailedEntryIsLeftToTheCron(t *testing.T) {
	store := &mockStore{last: &models.Notification{Key: "winback", UpdatedAt: time.Now().Add(-time.Hour)}, claimOwed: true}
	mail := &mockMailer{}
	svc := newWinBackService(t, store, mail)

	if err := svc.SendWinBack("u@example.com", testUserID, WinBack{Reason: WinBackPaymentFailed, Discount: testCode()}); err != nil {
		t.Fatal(err)
	}
	if store.createCalls != 0 || store.claimCalls != 0 || len(mail.calls) != 0 {
		t.Errorf("event path touched an owed entry: creates=%d claims=%d mails=%d", store.createCalls, store.claimCalls, len(mail.calls))
	}
}

// The daily cron mails owed entries as written, to the address they were
// written for — only rows its claim wins, only from the window where the
// code in the body is still honoured and the writer is done.
func TestSendOwedWinBacks(t *testing.T) {
	to := "u@example.com"
	uid := testUserID
	row := func(title string) models.Notification {
		return models.Notification{Key: "winback", Title: title, Body: "<p>" + title + " body</p>", To: &to, UserID: &uid, UpdatedAt: time.Now().Add(-time.Hour)}
	}
	noAddr := row("no address")
	noAddr.To = nil
	store := &mockStore{owed: []models.Notification{row("owed"), noAddr}, claimOwed: true}
	mail := &mockMailer{}
	svc := newWinBackService(t, store, mail)

	sent, err := svc.SendOwedWinBacks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sent != 1 || len(mail.calls) != 1 || mail.calls[0].to != to || mail.calls[0].subject != "owed" || !strings.Contains(mail.calls[0].body, "owed body") {
		t.Fatalf("want the one addressed entry mailed as written: sent=%d calls=%+v", sent, mail.calls)
	}
	if !store.markMailedCalled {
		t.Error("the entry must be stamped mailed")
	}
	if age := time.Since(store.owedUpdatedBefore); age < winbackRetryAfter-time.Second || age > winbackRetryAfter+time.Minute {
		t.Errorf("writer-done window: rows untouched for %v, want %v", age, winbackRetryAfter)
	}
	if age := time.Since(store.owedCreatedAfter); age < winbackRetryWithin-time.Second || age > winbackRetryWithin+time.Minute {
		t.Errorf("code-still-live window: rows younger than %v, want %v", age, winbackRetryWithin)
	}

	lost := &mockStore{owed: []models.Notification{row("owed")}, claimOwed: false}
	mail = &mockMailer{}
	if sent, err := newWinBackService(t, lost, mail).SendOwedWinBacks(context.Background()); err != nil || sent != 0 || len(mail.calls) != 0 {
		t.Errorf("a lost claim must not mail: sent=%d err=%v calls=%d", sent, err, len(mail.calls))
	}
}

// uniqueViolation is what go-pg returns when the once-per-account index
// rejects a second entry.
type uniqueViolation struct{}

func (uniqueViolation) Error() string {
	return "ERROR #23505 duplicate key value violates unique constraint"
}
func (uniqueViolation) Field(f byte) string {
	if f == 'C' {
		return "23505"
	}
	return ""
}
func (uniqueViolation) IntegrityViolation() bool { return true }

// The day the letter calls the last one, inclusive, must be whole in every
// time zone down to UTC-12 — never the day the code dies partway through.
func TestLastDay(t *testing.T) {
	cases := map[time.Time]string{
		time.Date(2027, 3, 21, 21, 0, 0, 0, time.UTC):  "2027-03-20", // midnight in UTC+3
		time.Date(2027, 3, 22, 0, 0, 0, 0, time.UTC):   "2027-03-20",
		time.Date(2027, 3, 22, 12, 0, 0, 0, time.UTC):  "2027-03-21", // exactly the end of the 21st in UTC-12
		time.Date(2027, 3, 22, 11, 59, 0, 0, time.UTC): "2027-03-20",
		{}: "",
	}
	for in, want := range cases {
		if got := lastDay(in); got != want {
			t.Errorf("lastDay(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestPreviewWinBackRendersWithoutSending(t *testing.T) {
	store := &mockStore{}
	mail := &mockMailer{}
	svc := newWinBackService(t, store, mail)

	subject, html, err := svc.PreviewWinBack("en", WinBack{Reason: WinBackTrialEnded, Tier: "silver", CapMbps: 5, Discount: testCode()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(subject, "free trial") || !strings.Contains(html, "<html>") || !strings.Contains(html, "SAMPLE50") {
		t.Errorf("preview: subject %q\n%s", subject, html)
	}
	if store.createCalls != 0 || len(mail.calls) != 0 {
		t.Error("preview must not journal or send")
	}
}
