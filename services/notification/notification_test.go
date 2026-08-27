package notification

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/i18n"
)

// testUserID stands in for whatever account a test's notification belongs
// to. Its value carries no meaning -- tests that care about a specific user
// build their own.
var testUserID = uuid.NewV4()

// --- Mock implementations ---

type mockStore struct {
	// lastMailed answers GetLastMailedByKeyAndUser, last answers
	// GetLastByKeyAndUser. Two fields rather than one filtered row because
	// mockStore returns exactly what a test sets, contract or no contract --
	// TestSendSurvivesStoreReturningUnmailedRow depends on being able to
	// hand back a row the real SQL could never produce.
	lastMailed *models.Notification
	last       *models.Notification
	lastErr    error
	lastAnyErr error

	createErr        error
	created          *models.Notification
	createCalls      int
	markMailedErr    error
	markMailedID     uuid.UUID
	markMailedCalled bool

	countUnread    int
	countUnreadErr error

	listByUser    []models.Notification
	listByUserErr error

	markAllReadErr    error
	markAllReadCalled bool

	pruneKeepingNewestErr    error
	pruneKeepingNewestCalled bool
	pruneKeepingNewestKeep   int
}

func (m *mockStore) GetLastMailedByKeyAndUser(_ context.Context, _ string, _ uuid.UUID) (*models.Notification, error) {
	return m.lastMailed, m.lastErr
}

func (m *mockStore) GetLastByKeyAndUser(_ context.Context, _ string, _ uuid.UUID) (*models.Notification, error) {
	return m.last, m.lastAnyErr
}

func (m *mockStore) Create(_ context.Context, n *models.Notification) error {
	m.created = n
	m.createCalls++
	return m.createErr
}

func (m *mockStore) MarkMailed(_ context.Context, id uuid.UUID, to string) error {
	m.markMailedCalled = true
	m.markMailedID = id
	if m.markMailedErr != nil {
		return m.markMailedErr
	}
	// Mirror the real column write, so a test asserting on the created
	// row's MailedAt (rather than just the call flag) is checking
	// something a regression could actually break.
	if m.created != nil {
		now := time.Now()
		m.created.MailedAt = &now
	}
	return nil
}

func (m *mockStore) CountUnread(_ context.Context, _ uuid.UUID) (int, error) {
	return m.countUnread, m.countUnreadErr
}

func (m *mockStore) ListByUser(_ context.Context, _ uuid.UUID, _ int) ([]models.Notification, error) {
	return m.listByUser, m.listByUserErr
}

func (m *mockStore) MarkAllRead(_ context.Context, _ uuid.UUID) error {
	m.markAllReadCalled = true
	return m.markAllReadErr
}

func (m *mockStore) PruneKeepingNewest(_ context.Context, keep int) error {
	m.pruneKeepingNewestCalled = true
	m.pruneKeepingNewestKeep = keep
	return m.pruneKeepingNewestErr
}

type mockMailer struct {
	sendErr error
	calls   []mailCall
}

type mailCall struct {
	to      string
	subject string
	body    string
}

func (m *mockMailer) Send(to, subject, body string) error {
	m.calls = append(m.calls, mailCall{to: to, subject: subject, body: body})
	return m.sendErr
}

// --- Test helpers ---

func setupTemplateDir(t *testing.T, templates map[string]string) string {
	t.Helper()
	tmplDir := t.TempDir()
	// The real email layout, copied rather than retyped: every mail send
	// goes through it, so a stand-in here would let these tests pass against
	// a wrapper production does not use.
	if _, ok := templates[emailLayout]; !ok {
		layout, err := os.ReadFile(filepath.Join("../../templates/notification", emailLayout))
		if err != nil {
			t.Fatalf("read the real email layout: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tmplDir, emailLayout), layout, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range templates {
		if err := os.WriteFile(filepath.Join(tmplDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return tmplDir
}

func newTestService(store notificationStore, mail mailer, templateDir string) *Service {
	return &Service{
		store:                 store,
		mail:                  mail,
		domain:                "https://webtor.io",
		templateDir:           templateDir,
		transferTimeoutPeriod: 48 * time.Hour,
	}
}

// --- Tests for render ---

func TestRender_Success(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "<p>Hello {{ .Name }}!</p>",
	})
	svc := newTestService(nil, nil, tmplDir)

	body, err := svc.render("test.html", "", map[string]any{"Name": "World"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expected := "<p>Hello World!</p>"
	if body != expected {
		t.Errorf("expected %q, got %q", expected, body)
	}
}

func TestRender_NotFound(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{})
	svc := newTestService(nil, nil, tmplDir)

	_, err := svc.render("nonexistent.html", "", nil)
	if err == nil {
		t.Fatal("expected error for missing template")
	}
	if !strings.Contains(err.Error(), "template not found") {
		t.Errorf("expected 'template not found' error, got %q", err.Error())
	}
}

func TestRender_InvalidTemplate(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"bad.html": "{{ .Missing }",
	})
	svc := newTestService(nil, nil, tmplDir)

	_, err := svc.render("bad.html", "", nil)
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}

func TestRender_ExecutionError(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"exec_err.html": "{{ .Name.Missing }}",
	})
	svc := newTestService(nil, nil, tmplDir)

	_, err := svc.render("exec_err.html", "", map[string]any{"Name": "plain string"})
	if err == nil {
		t.Fatal("expected error for template execution failure")
	}
}

// --- Tests for Send ---

func TestSend_Success(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "<p>Hello {{ .Name }}!</p>",
	})
	store := &mockStore{}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	err := svc.Send(SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "test-key",
		Title:    "Test Title",
		Template: "test.html",
		Data:     map[string]any{"Name": "World"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if store.created == nil {
		t.Fatal("expected notification to be saved to store")
	}
	if store.created.Key != "test-key" {
		t.Errorf("expected key 'test-key', got %q", store.created.Key)
	}
	if store.created.Title != "Test Title" {
		t.Errorf("expected title 'Test Title', got %q", store.created.Title)
	}
	if store.created.To == nil || *store.created.To != "user@example.com" {
		t.Errorf("expected to 'user@example.com', got %v", store.created.To)
	}
	if store.created.UserID == nil || *store.created.UserID != testUserID {
		t.Errorf("expected user id %v, got %v", testUserID, store.created.UserID)
	}
	if store.created.Template != "test.html" {
		t.Errorf("expected template 'test.html', got %q", store.created.Template)
	}
	if store.created.Body != "<p>Hello World!</p>" {
		t.Errorf("expected body '<p>Hello World!</p>', got %q", store.created.Body)
	}

	if len(mail.calls) != 1 {
		t.Fatalf("expected 1 email, got %d", len(mail.calls))
	}
	if mail.calls[0].to != "user@example.com" {
		t.Errorf("expected email to 'user@example.com', got %q", mail.calls[0].to)
	}
	if mail.calls[0].subject != "Test Title" {
		t.Errorf("expected subject 'Test Title', got %q", mail.calls[0].subject)
	}
	// The letter carries the same fragment the feed row got, wrapped in the
	// email layout -- which is the whole difference between the two
	// destinations, and the reason the feed no longer shows a DOCTYPE.
	if !strings.Contains(mail.calls[0].body, "<p>Hello World!</p>") {
		t.Errorf("the letter does not carry the message:\n%s", mail.calls[0].body)
	}
	if !strings.Contains(mail.calls[0].body, "<!DOCTYPE html>") {
		t.Errorf("the letter is not a document -- a mail client needs the layout:\n%s", mail.calls[0].body)
	}
	if !store.markMailedCalled {
		t.Error("expected mailed_at to be stamped after a successful send")
	}
}

func TestSend_DuplicateWithin24Hours(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	// One row, seen by both guards: a row that was mailed an hour ago is
	// also a row that was created inside the window, so a store cannot
	// report it to one guard and hide it from the other.
	mailedAt := time.Now().Add(-1 * time.Hour)
	existing := &models.Notification{
		NotificationID: uuid.NewV4(),
		Key:            "test-key",
		UserID:         &testUserID,
		Body:           "body",
		CreatedAt:      mailedAt,
		MailedAt:       &mailedAt,
	}
	store := &mockStore{lastMailed: existing, last: existing}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	err := svc.Send(SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "test-key",
		Title:    "Test",
		Template: "test.html",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// No second feed entry: the existing one IS this notification, and a
	// repeat inside the window is the same notification arriving again.
	if store.createCalls != 0 {
		t.Errorf("feed rows created: got %d, want 0 -- the entry inside the window is this notification", store.createCalls)
	}
	if len(mail.calls) != 0 {
		t.Error("expected no email to be sent for a duplicate mailed within 24h")
	}
	if store.markMailedCalled {
		t.Error("expected mailed_at not to be stamped when the send is suppressed")
	}
}

func TestSend_DuplicateOlderThan24Hours(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	mailedAt := time.Now().Add(-25 * time.Hour)
	existing := &models.Notification{
		NotificationID: uuid.NewV4(),
		Key:            "test-key",
		UserID:         &testUserID,
		Body:           "body",
		CreatedAt:      mailedAt,
		MailedAt:       &mailedAt,
	}
	store := &mockStore{lastMailed: existing, last: existing}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	err := svc.Send(SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "test-key",
		Title:    "Test",
		Template: "test.html",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if store.created == nil {
		t.Error("expected notification to be created for old duplicate")
	}
	if len(mail.calls) != 1 {
		t.Error("expected email to be sent for old duplicate")
	}
	if !store.markMailedCalled {
		t.Error("expected mailed_at to be stamped after the send")
	}
}

func TestSend_NoPreviousNotification(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	store := &mockStore{}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	err := svc.Send(SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "test-key",
		Title:    "Test",
		Template: "test.html",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if store.created == nil {
		t.Error("expected notification to be created")
	}
	if len(mail.calls) != 1 {
		t.Error("expected email to be sent")
	}
}

func TestSend_StoreGetLastError(t *testing.T) {
	store := &mockStore{lastErr: fmt.Errorf("db connection failed")}
	svc := newTestService(store, nil, "")

	err := svc.Send(SendOptions{
		To:     "user@example.com",
		UserID: testUserID,
		Key:    "test-key",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to check for duplicate notification") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSend_RenderError(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{})
	store := &mockStore{}
	svc := newTestService(store, nil, tmplDir)

	err := svc.Send(SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "test-key",
		Title:    "Test",
		Template: "nonexistent.html",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to render notification template") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSend_StoreCreateError(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	store := &mockStore{createErr: fmt.Errorf("insert failed")}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	err := svc.Send(SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "test-key",
		Title:    "Test",
		Template: "test.html",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to save notification to db") {
		t.Errorf("unexpected error message: %v", err)
	}
	// The entry is written before anything is sent, so a failed write means
	// nothing was mailed either — there is no letter for a row that was
	// never recorded as existing (see TestSend_MailError for the reverse:
	// the entry surviving a failed send).
	if len(mail.calls) != 0 {
		t.Errorf("emails sent: got %d, want 0 — the journal write precedes the send", len(mail.calls))
	}
}

func TestSend_MailError(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	store := &mockStore{}
	mail := &mockMailer{sendErr: fmt.Errorf("SMTP timeout")}
	svc := newTestService(store, mail, tmplDir)

	err := svc.Send(SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "test-key",
		Title:    "Test",
		Template: "test.html",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to send email") {
		t.Errorf("unexpected error message: %v", err)
	}
	// The feed entry exists even though the letter failed — it is written
	// before the send is attempted, because the entry IS the notification.
	// Only mailed_at stays unset, which is what keeps the retry honest.
	if store.created == nil {
		t.Error("expected the feed entry to exist even though the send failed")
	}
	if store.markMailedCalled {
		t.Error("mailed_at must not be stamped when the send failed")
	}
}

// TestSend_RetryAfterMailFailure pins the recovery path end to end: a failed
// send still records the feed entry with mailed_at unset, so the identical
// retry is not muted by the dedupe check and actually mails.
func TestSend_RetryAfterMailFailure(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	store := &mockStore{}
	mail := &mockMailer{sendErr: fmt.Errorf("SMTP timeout")}
	svc := newTestService(store, mail, tmplDir)

	opts := SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "test-key",
		Title:    "Test",
		Template: "test.html",
	}
	if err := svc.Send(opts); err == nil {
		t.Fatal("expected the first send to fail")
	}
	if store.markMailedCalled {
		t.Fatal("first send failed; mailed_at must not be stamped")
	}

	mail.sendErr = nil
	if err := svc.Send(opts); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(mail.calls) != 2 {
		t.Errorf("send attempts: got %d, want 2 — the retry must not be muted by the dedupe", len(mail.calls))
	}
	if !store.markMailedCalled {
		t.Error("the successful retry must stamp mailed_at")
	}
}

// --- Tests for SendVaulted ---

func TestSendVaulted(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"vaulted.html": "<p>{{ .Name }} at {{ .URL }} ({{ .Domain }})</p>",
	})
	store := &mockStore{}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	r := &vaultModels.Resource{
		ResourceID: "abc123",
		Name:       "My Torrent",
	}
	err := svc.SendVaulted("user@example.com", testUserID, r)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if store.created == nil {
		t.Fatal("expected notification to be created")
	}
	if store.created.Key != "vaulted-abc123" {
		t.Errorf("expected key 'vaulted-abc123', got %q", store.created.Key)
	}
	if store.created.Title != "Your resource My Torrent has been vaulted!" {
		t.Errorf("unexpected title: %q", store.created.Title)
	}
	if store.created.To == nil || *store.created.To != "user@example.com" {
		t.Errorf("expected to 'user@example.com', got %v", store.created.To)
	}

	expectedBody := "<p>My Torrent at https://webtor.io/abc123 (https://webtor.io)</p>"
	if store.created.Body != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, store.created.Body)
	}
}

// --- Tests for SendExpiring ---

func TestSendExpiring(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"expiring.html": "{{ .Days }} days: {{ range .Resources }}{{ .Name }}={{ .URL }} {{ end }}({{ .Domain }})",
	})
	store := &mockStore{}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	resources := []vaultModels.Resource{
		{ResourceID: "res1", Name: "Torrent 1"},
		{ResourceID: "res2", Name: "Torrent 2"},
	}
	err := svc.SendExpiring("user@example.com", testUserID, 7, resources)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if store.created == nil {
		t.Fatal("expected notification to be created")
	}
	if store.created.Key != "expiring-7" {
		t.Errorf("expected key 'expiring-7', got %q", store.created.Key)
	}
	if store.created.Title != "Your resources will disappear in 7 days!" {
		t.Errorf("unexpected title: %q", store.created.Title)
	}

	expectedBody := "7 days: Torrent 1=https://webtor.io/res1 Torrent 2=https://webtor.io/res2 (https://webtor.io)"
	if store.created.Body != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, store.created.Body)
	}
}

func TestSendExpiring_EmptyResources(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"expiring.html": "{{ .Days }} days: {{ range .Resources }}{{ .Name }} {{ end }}({{ .Domain }})",
	})
	store := &mockStore{}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	err := svc.SendExpiring("user@example.com", testUserID, 3, []vaultModels.Resource{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if store.created == nil {
		t.Fatal("expected notification to be created")
	}
	if store.created.Key != "expiring-3" {
		t.Errorf("expected key 'expiring-3', got %q", store.created.Key)
	}
}

// --- Tests for SendTransferTimeout ---

func TestSendTransferTimeout(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"transfer-timeout.html": "{{ .Name }} timeout={{ .Timeout }} url={{ .URL }} ({{ .Domain }})",
	})
	store := &mockStore{}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	r := &vaultModels.Resource{
		ResourceID: "xyz789",
		Name:       "Big Torrent",
	}
	err := svc.SendTransferTimeout("user@example.com", testUserID, r)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if store.created == nil {
		t.Fatal("expected notification to be created")
	}
	if store.created.Key != "transfer-timeout-xyz789" {
		t.Errorf("expected key 'transfer-timeout-xyz789', got %q", store.created.Key)
	}
	if store.created.Title != "We were unable to transfer your resource Big Torrent" {
		t.Errorf("unexpected title: %q", store.created.Title)
	}
	if !strings.Contains(store.created.Body, "Big Torrent") {
		t.Errorf("body should contain resource name, got %q", store.created.Body)
	}
	if !strings.Contains(store.created.Body, "https://webtor.io/xyz789") {
		t.Errorf("body should contain URL, got %q", store.created.Body)
	}
	// transferTimeoutPeriod is 48h, durafmt formats it as "2 days"
	if !strings.Contains(store.created.Body, "2 days") {
		t.Errorf("body should contain formatted timeout '2 days', got %q", store.created.Body)
	}
}

// --- Tests for SendExpired ---

func TestSendExpired(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"expired.html": "{{ .Name }} expired url={{ .URL }} ({{ .Domain }})",
	})
	store := &mockStore{}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	r := &vaultModels.Resource{
		ResourceID: "exp456",
		Name:       "Old Torrent",
	}
	err := svc.SendExpired("user@example.com", testUserID, r)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if store.created == nil {
		t.Fatal("expected notification to be created")
	}
	if store.created.Key != "expired-exp456" {
		t.Errorf("expected key 'expired-exp456', got %q", store.created.Key)
	}
	if store.created.Title != "Your resource Old Torrent has expired" {
		t.Errorf("unexpected title: %q", store.created.Title)
	}

	expectedBody := "Old Torrent expired url=https://webtor.io/exp456 (https://webtor.io)"
	if store.created.Body != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, store.created.Body)
	}
}

// --- Tests for the two properties that make the journal trustworthy ---

// TestSendRecordsEntryWithoutMailWhenSMTPMissing pins the instance with no
// mail transport at all: Send must still record the feed entry, and must not
// claim a delivery that never happened.
func TestSendRecordsEntryWithoutMailWhenSMTPMissing(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	store := &mockStore{}
	// No mailer: this is exactly what New produces when SMTP_HOST is empty.
	// There is no unconfigured-mailer stand-in to substitute any more --
	// absence is the state under test.
	svc := newTestService(store, nil, tmplDir)

	if svc.MailConfigured() {
		t.Error("a Service with no mailer must not report mail as available")
	}

	err := svc.Send(SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "test-key",
		Title:    "Test",
		Template: "test.html",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if store.createCalls != 1 {
		t.Fatalf("expected exactly one row to be created, got %d", store.createCalls)
	}
	if store.created.MailedAt != nil {
		t.Errorf("expected MailedAt to be nil, got %v", store.created.MailedAt)
	}
	if store.markMailedCalled {
		t.Error("mailed_at must not be stamped when there is no mail transport")
	}
}

// TestTypedNilMailerIsNoMailer is the guard against the interface trap: a
// typed nil pointer stored in an interface compares != nil, so a Service
// assembled with one would report mail as available and then panic on the
// first Send. This has already cost this codebase two bugs elsewhere (a
// typed-nil *models.User in the auth request context, a nil-receiver panic
// in claims.Client.Close) -- it must not become a third here.
func TestTypedNilMailerIsNoMailer(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})

	// Sanity: the trap is real. If this ever stops holding, the guards below
	// are testing nothing.
	var typedNil mailer = (*smtpMailer)(nil)
	if typedNil == nil {
		t.Fatal("a typed nil in an interface compared equal to nil; this test's premise is gone")
	}

	// Both construction paths. They share one guard today (Service.hasMail),
	// and that is the point of testing both: a later refactor that moves the
	// check into NewWith would leave the in-package path unguarded, and this
	// second case is what would go red.
	for _, tc := range []struct {
		name  string
		build func(store notificationStore) *Service
	}{
		{
			name: "NewWith",
			build: func(store notificationStore) *Service {
				return NewWith(store, (*smtpMailer)(nil), nil, "https://webtor.io", tmplDir)
			},
		},
		{
			name: "assembled in package",
			build: func(store notificationStore) *Service {
				return newTestService(store, (*smtpMailer)(nil), tmplDir)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &mockStore{}
			svc := tc.build(store)

			if svc.MailConfigured() {
				t.Error("a typed-nil mailer must not answer that mail is available")
			}

			// Would panic dereferencing the nil *smtpMailer if Send called through.
			err := svc.Send(SendOptions{
				To:       "user@example.com",
				UserID:   testUserID,
				Key:      "test-key",
				Title:    "Test",
				Template: "test.html",
			})
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if store.createCalls != 1 {
				t.Fatalf("expected exactly one row to be created, got %d", store.createCalls)
			}
			if store.created.MailedAt != nil {
				t.Errorf("expected MailedAt to be nil, got %v", store.created.MailedAt)
			}
			if store.markMailedCalled {
				t.Error("mailed_at must not be stamped when there is no mail transport")
			}
		})
	}
}

// journalStore is a store fake with real dedupe filtering, mirroring the
// production SQL predicate (key, user_id, mailed_at IS NOT NULL) instead of
// a hand-set return value -- so a regression in that filter is something a
// test built on it can actually catch, the way TestSendDoesNotSuppress...
// below does.
type journalStore struct {
	rows []*models.Notification
}

func (j *journalStore) GetLastMailedByKeyAndUser(_ context.Context, key string, userID uuid.UUID) (*models.Notification, error) {
	for i := len(j.rows) - 1; i >= 0; i-- {
		r := j.rows[i]
		if r.Key == key && r.UserID != nil && *r.UserID == userID && r.MailedAt != nil {
			return r, nil
		}
	}
	return nil, nil
}

// GetLastByKeyAndUser is the feed guard's read: newest row for this key and
// user, mailed or not. The missing MailedAt condition is the whole
// difference from the method above.
func (j *journalStore) GetLastByKeyAndUser(_ context.Context, key string, userID uuid.UUID) (*models.Notification, error) {
	for i := len(j.rows) - 1; i >= 0; i-- {
		r := j.rows[i]
		if r.Key == key && r.UserID != nil && *r.UserID == userID {
			return r, nil
		}
	}
	return nil, nil
}

// Create fills in what the real table's column defaults fill in. Both
// matter: without notification_id MarkMailed cannot find the row it is
// meant to stamp, and without created_at every row looks infinitely old to
// the feed guard, which would let a second entry through in exactly the
// tests written to catch one.
func (j *journalStore) Create(_ context.Context, n *models.Notification) error {
	if n.NotificationID == (uuid.UUID{}) {
		n.NotificationID = uuid.NewV4()
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	j.rows = append(j.rows, n)
	return nil
}

func (j *journalStore) MarkMailed(_ context.Context, id uuid.UUID, to string) error {
	now := time.Now()
	for _, r := range j.rows {
		if r.NotificationID == id {
			r.MailedAt = &now
			// Mirrors the real store: the address is recorded with the
			// stamp, not only at Create. A reused row carries the earlier
			// attempt's `to`, which is NULL when that attempt had nowhere
			// to send.
			addr := to
			r.To = &addr
		}
	}
	return nil
}

// CountUnread, ListByUser, MarkAllRead and PruneKeepingNewest are not
// exercised by any journalStore-based test today -- those methods back
// the feed-reading path, while journalStore exists for Send's dedupe
// filter -- but the type still has to satisfy notificationStore.
func (j *journalStore) CountUnread(_ context.Context, userID uuid.UUID) (int, error) {
	count := 0
	for _, r := range j.rows {
		if r.UserID != nil && *r.UserID == userID && r.ReadAt == nil {
			count++
		}
	}
	return count, nil
}

func (j *journalStore) ListByUser(_ context.Context, userID uuid.UUID, limit int) ([]models.Notification, error) {
	var out []models.Notification
	for i := len(j.rows) - 1; i >= 0 && len(out) < limit; i-- {
		if r := j.rows[i]; r.UserID != nil && *r.UserID == userID {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (j *journalStore) MarkAllRead(_ context.Context, userID uuid.UUID) error {
	now := time.Now()
	for _, r := range j.rows {
		if r.UserID != nil && *r.UserID == userID && r.ReadAt == nil {
			r.ReadAt = &now
		}
	}
	return nil
}

func (j *journalStore) PruneKeepingNewest(_ context.Context, _ int) error {
	return nil
}

// TestSendCreatesOneFeedEntryPerKeyAndUserInsideTheWindow is property one:
// the same (key, user) sent twice inside the window leaves exactly one feed
// row.
//
// This is the JetStream redelivery case reduced to its essentials.
// handlers/event.resourceVaulted notifies users in a loop and returns on the
// first failing lookup, after the earlier users have already been notified;
// handler.subscribe Naks the message, JetStream redelivers it, and every one
// of those earlier users used to collect another identical entry -- once per
// redelivery, forever. The dedupe used to be the journal row itself, and was
// lost when the row started being written unconditionally.
func TestSendCreatesOneFeedEntryPerKeyAndUserInsideTheWindow(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	store := &journalStore{}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	opts := SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "vaulted-abc123",
		Title:    "Test",
		Template: "test.html",
	}
	if err := svc.Send(opts); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if err := svc.Send(opts); err != nil {
		t.Fatalf("redelivery: %v", err)
	}

	if len(store.rows) != 1 {
		t.Errorf("feed rows: got %d, want 1 -- a redelivered event must not add a second entry", len(store.rows))
	}
	// The letter is deduped too, by mailed_at, which is the guard that
	// already worked. Asserted here so a fix that suppressed the row by
	// suppressing the whole send would still have to explain itself.
	if len(mail.calls) != 1 {
		t.Errorf("letters: got %d, want 1", len(mail.calls))
	}
}

// TestSendCreatesASecondFeedEntryOutsideTheWindow is the negative control on
// the guard above: repeats are bounded by a window, not blocked forever.
//
// It is the difference between this design and the unique index on
// (key, user_id) an earlier draft proposed. expiring-7 is a digest key: the
// same user legitimately earns it again next month, and an index would have
// allowed one seven-day warning per account ever.
func TestSendCreatesASecondFeedEntryOutsideTheWindow(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	stale := time.Now().Add(-dedupeWindow - time.Hour)
	store := &journalStore{
		rows: []*models.Notification{
			{
				NotificationID: uuid.NewV4(),
				Key:            "expiring-7",
				UserID:         &testUserID,
				Body:           "body",
				CreatedAt:      stale,
				MailedAt:       &stale,
			},
		},
	}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	err := svc.Send(SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "expiring-7",
		Title:    "Test",
		Template: "test.html",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(store.rows) != 2 {
		t.Errorf("feed rows: got %d, want 2 -- a repeat past the window is a new notification, not a duplicate", len(store.rows))
	}
	if len(mail.calls) != 1 {
		t.Errorf("letters: got %d, want 1", len(mail.calls))
	}
}

// TestSendDoesNotSuppressRetryAfterFailedSend is property two, and the
// interaction that makes the fix above more than a one-liner: a row inside
// the window whose letter never left suppresses the duplicate entry and
// still owes the letter. The retry has to mail that existing row and stamp
// its mailed_at -- deduping the feed by refusing to work on the row it
// found would fix duplicates by breaking retries.
func TestSendDoesNotSuppressRetryAfterFailedSend(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	existing := &models.Notification{
		NotificationID: uuid.NewV4(),
		Key:            "test-key",
		UserID:         &testUserID,
		Body:           "body",
		CreatedAt:      time.Now().Add(-time.Hour),
		// MailedAt is nil: an earlier attempt for this key never
		// actually left -- no SMTP configured, or a failed dial.
	}
	store := &journalStore{rows: []*models.Notification{existing}}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	err := svc.Send(SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "test-key",
		Title:    "Test",
		Template: "test.html",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(mail.calls) != 1 {
		t.Errorf("expected the mail to be attempted despite the unmailed row for the same key, got %d calls", len(mail.calls))
	}
	// Both halves, together: the letter went out AND no second entry was
	// added. Either assertion alone is satisfiable by a broken fix -- the
	// mail one by dropping the feed guard, the row one by returning early.
	if len(store.rows) != 1 {
		t.Errorf("feed rows: got %d, want 1 -- the retry mails the existing entry, it does not add one", len(store.rows))
	}
	if existing.MailedAt == nil {
		t.Error("mailed_at was not stamped on the existing row -- the letter it owed is still owed, so the next send will mail again")
	}
}

// TestSendRecordsTheRecipientWhenStampingAReusedRow closes the gap that
// stamping alone leaves. A row reused by a redelivered event carries the
// earlier attempt's `to` -- NULL when that attempt had nowhere to send, which
// is the ordinary case on an instance with no deliverable address. Stamping
// mailed_at without also writing the address would leave a row asserting that
// a letter went out, with no record of where: the same shape of quiet untruth
// this table was changed to stop telling.
func TestSendRecordsTheRecipientWhenStampingAReusedRow(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	existing := &models.Notification{
		NotificationID: uuid.NewV4(),
		Key:            "test-key",
		UserID:         &testUserID,
		Body:           "body",
		CreatedAt:      time.Now().Add(-time.Hour),
		// To is nil and MailedAt is nil: the earlier attempt had no
		// deliverable address, so it wrote a feed entry and stopped.
	}
	store := &journalStore{rows: []*models.Notification{existing}}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	// The account has since confirmed an address, so this attempt can mail.
	err := svc.Send(SendOptions{
		To:       "confirmed@example.com",
		UserID:   testUserID,
		Key:      "test-key",
		Title:    "Test",
		Template: "test.html",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if existing.MailedAt == nil {
		t.Fatal("mailed_at was not stamped, so this test cannot say anything about the address")
	}
	if existing.To == nil {
		t.Fatal("row is stamped as mailed but records no recipient -- the journal says a letter went out to nobody")
	}
	if *existing.To != "confirmed@example.com" {
		t.Errorf("recipient recorded as %q, want the address this send actually used", *existing.To)
	}
}

// TestSendSurvivesStoreReturningUnmailedRow is the Go-side counterpart to
// TestSendDoesNotSuppressRetryAfterFailedSend: that test proves the SQL
// predicate matters, this one proves Send does not crash if a store
// implementation -- one this package does not control, reached only
// through the notificationStore interface -- ever violates the contract
// and hands back a row with MailedAt nil. Send runs inside a bare `go f()`
// in release_subscription with no recover(), so a nil-pointer panic here
// would take down the whole process, not just this send.
func TestSendSurvivesStoreReturningUnmailedRow(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	// mockStore returns exactly what is set here, unlike journalStore --
	// this is deliberately a store that violates its own contract.
	store := &mockStore{
		lastMailed: &models.Notification{
			Key:      "test-key",
			UserID:   &testUserID,
			MailedAt: nil,
		},
	}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	err := svc.Send(SendOptions{
		To:       "user@example.com",
		UserID:   testUserID,
		Key:      "test-key",
		Title:    "Test",
		Template: "test.html",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(mail.calls) != 1 {
		t.Errorf("expected the mail to be attempted -- a nil MailedAt means never mailed, not a recent duplicate, got %d calls", len(mail.calls))
	}
}

// --- Tests for SendEmailVerification ---

// verifyTemplate is the real one, reduced to the part that matters: the
// token-bearing link, which templates/notification/verify-email.html renders
// as <a href="{{ .Link }}">{{ .Link }}</a>.
const verifyTemplate = `<p><a href="{{ .Link }}">{{ .Link }}</a></p>`

const verifyLink = "https://webtor.io/profile/email/verify/deadbeefdeadbeef"

// TestSendEmailVerificationDoesNotEnterTheFeed is the whole point of
// verification. The feed is readable by the account that submitted the
// address (templates/views/notifications/get.html renders .Body verbatim),
// so a journal row carrying the verification link would let the submitter
// read the token out of their own feed and confirm a mailbox they have no
// access to -- which is the one thing verifying an address is for.
//
// The letter still goes out: this is transactional mail, not feed content,
// and suppressing the row must not suppress the message.
func TestSendEmailVerificationDoesNotEnterTheFeed(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"verify-email.html": verifyTemplate,
	})
	store := &mockStore{}
	mail := &mockMailer{}
	svc := newTestService(store, mail, tmplDir)

	if err := svc.SendEmailVerification("user@example.com", verifyLink, ""); err != nil {
		t.Fatalf("send verification: %v", err)
	}

	if store.createCalls != 0 {
		t.Errorf("feed rows created: got %d, want 0 -- the verification link must never be published to the submitter's feed", store.createCalls)
	}
	if store.created != nil && strings.Contains(store.created.Body, verifyLink) {
		t.Errorf("the feed row carries the verification link, which voids verification:\n%s", store.created.Body)
	}

	if len(mail.calls) != 1 {
		t.Fatalf("letters sent: got %d, want 1 -- keeping it out of the feed must not stop it being mailed", len(mail.calls))
	}
	if mail.calls[0].to != "user@example.com" {
		t.Errorf("recipient: got %q, want the pending address", mail.calls[0].to)
	}
	if !strings.Contains(mail.calls[0].body, verifyLink) {
		t.Errorf("the letter does not carry the verification link:\n%s", mail.calls[0].body)
	}
}

// TestSendEmailVerificationWithoutMailTransportIsNoOp pins the other half of
// the mechanism: with no transport the call is a success that does nothing.
// Not an error, because the address field is only offered where mail works
// (handlers/profile.emailSectionAvailable), so there is nothing to report --
// and still no feed row, since the row is what would leak the token. Both
// assertions are needed: the no-error half alone passed before this fix too.
func TestSendEmailVerificationWithoutMailTransportIsNoOp(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"verify-email.html": verifyTemplate,
	})
	store := &mockStore{}
	// No mailer: exactly what New produces when SMTP_HOST is empty.
	svc := newTestService(store, nil, tmplDir)

	if err := svc.SendEmailVerification("user@example.com", verifyLink, ""); err != nil {
		t.Fatalf("send verification without a transport must be a no-op, got error: %v", err)
	}
	if store.createCalls != 0 {
		t.Errorf("feed rows created: got %d, want 0 -- an unsendable verification link must not be published to the feed either", store.createCalls)
	}
}

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
