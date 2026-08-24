package notification

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
)

// testUserID stands in for whatever account a test's notification belongs
// to. Its value carries no meaning -- tests that care about a specific user
// build their own.
var testUserID = uuid.NewV4()

// --- Mock implementations ---

type mockStore struct {
	lastNotification *models.Notification
	lastErr          error
	createErr        error
	created          *models.Notification
	markMailedErr    error
	markMailedID     uuid.UUID
	markMailedCalled bool
}

func (m *mockStore) GetLastMailedByKeyAndUser(_ context.Context, _ string, _ uuid.UUID) (*models.Notification, error) {
	return m.lastNotification, m.lastErr
}

func (m *mockStore) Create(_ context.Context, n *models.Notification) error {
	m.created = n
	return m.createErr
}

func (m *mockStore) MarkMailed(_ context.Context, id uuid.UUID) error {
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
	if mail.calls[0].body != "<p>Hello World!</p>" {
		t.Errorf("expected body '<p>Hello World!</p>', got %q", mail.calls[0].body)
	}
	if !store.markMailedCalled {
		t.Error("expected mailed_at to be stamped after a successful send")
	}
}

func TestSend_DuplicateWithin24Hours(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	mailedAt := time.Now().Add(-1 * time.Hour)
	store := &mockStore{
		lastNotification: &models.Notification{
			MailedAt: &mailedAt,
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
	// The feed entry is written regardless of the dedupe outcome -- it IS
	// the notification. Only the letter is suppressed.
	if store.created == nil {
		t.Error("expected a feed entry to be created even for a recent duplicate")
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
	store := &mockStore{
		lastNotification: &models.Notification{
			MailedAt: &mailedAt,
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
	store := &mockStore{lastNotification: nil}
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

// --- Tests for smtpMailer ---

func TestSmtpMailer_EmptyHost(t *testing.T) {
	m := &smtpMailer{host: ""}
	err := m.Send("user@example.com", "Test", "body")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured when SMTP host is empty, got %v", err)
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

// TestSendRecordsEntryWithoutMailWhenSMTPMissing pins the self-hosted case:
// no SMTP server configured, so Send must still record the feed entry, and
// must not claim a delivery that never happened.
func TestSendRecordsEntryWithoutMailWhenSMTPMissing(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	store := &mockStore{}
	// The real smtpMailer, unconfigured -- exercises the actual path that
	// makes an SMTP-less self-hosted instance return ErrNotConfigured,
	// not a stand-in that merely asserts the constant's name.
	mail := &smtpMailer{host: ""}
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
		t.Fatal("expected exactly one row to be created")
	}
	if store.created.MailedAt != nil {
		t.Errorf("expected MailedAt to be nil, got %v", store.created.MailedAt)
	}
	if store.markMailedCalled {
		t.Error("mailed_at must not be stamped when SMTP is not configured")
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

func (j *journalStore) Create(_ context.Context, n *models.Notification) error {
	j.rows = append(j.rows, n)
	return nil
}

func (j *journalStore) MarkMailed(_ context.Context, id uuid.UUID) error {
	now := time.Now()
	for _, r := range j.rows {
		if r.NotificationID == id {
			r.MailedAt = &now
		}
	}
	return nil
}

// TestSendDoesNotSuppressRetryAfterFailedSend pins the other half: a row
// that exists for this key and user but was never mailed must not count as
// a duplicate.
func TestSendDoesNotSuppressRetryAfterFailedSend(t *testing.T) {
	tmplDir := setupTemplateDir(t, map[string]string{
		"test.html": "body",
	})
	store := &journalStore{
		rows: []*models.Notification{
			{
				NotificationID: uuid.NewV4(),
				Key:            "test-key",
				UserID:         &testUserID,
				// MailedAt is nil: an earlier attempt for this key never
				// actually left -- no SMTP configured, or a failed dial.
			},
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
		t.Errorf("expected the mail to be attempted despite the unmailed row for the same key, got %d calls", len(mail.calls))
	}
}
