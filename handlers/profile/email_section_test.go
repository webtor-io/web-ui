package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/notification"
)

// newEmailRouter wires setEmail with gin's Recovery middleware. The handler
// is deliberately given a nil *cs.PG throughout this file, so a request that
// gets past the gates panics inside cs.PG.Get rather than quietly storing
// something; Recovery turns that into a 500, which fails the assertions
// below instead of taking the whole test binary down with it.
func newEmailRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/profile/email", h.setEmail)
	return r
}

func postEmail(r *gin.Engine, email string) *httptest.ResponseRecorder {
	form := url.Values{"email": {email}}
	req := httptest.NewRequest(http.MethodPost, "/profile/email", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// nullJournal is the notification table, minus the table. Nothing in this
// file reads it back -- it exists only because notification.NewWith needs a
// store to build a Service whose MailConfigured() can answer true.
type nullJournal struct{}

func (nullJournal) GetLastMailedByKeyAndUser(context.Context, string, uuid.UUID) (*models.Notification, error) {
	return nil, nil
}
func (nullJournal) GetLastByKeyAndUser(context.Context, string, uuid.UUID) (*models.Notification, error) {
	return nil, nil
}
func (nullJournal) Create(context.Context, *models.Notification) error  { return nil }
func (nullJournal) MarkMailed(context.Context, uuid.UUID, string) error { return nil }
func (nullJournal) CountUnread(context.Context, uuid.UUID) (int, error) { return 0, nil }
func (nullJournal) MarkAllRead(context.Context, uuid.UUID) error        { return nil }
func (nullJournal) PruneKeepingNewest(context.Context, int) error       { return nil }
func (nullJournal) ListByUser(context.Context, uuid.UUID, int) ([]models.Notification, error) {
	return nil, nil
}

// countingMailer stands in for a configured SMTP server: its presence is
// what makes notification.Service.MailConfigured() true, and its call count
// is how these tests see whether a refused request nevertheless got as far
// as putting a letter on the wire.
type countingMailer struct{ sent []string }

func (m *countingMailer) Send(to, _, _ string) error {
	m.sent = append(m.sent, to)
	return nil
}

// notificationServiceWithMail builds a Service that reports mail as
// available, which is one of setEmail's two capability gates.
func notificationServiceWithMail(m *countingMailer) *notification.Service {
	return notification.NewWith(nullJournal{}, m, nil, "https://webtor.io", "../../templates/notification")
}

// notificationServiceWithoutMail builds a Service with no transport -- what
// notification.New produces when SMTP_HOST is empty, i.e. every default
// self-hosted instance.
func notificationServiceWithoutMail() *notification.Service {
	return notification.NewWith(nullJournal{}, nil, nil, "https://webtor.io", "../../templates/notification")
}

// TestSetEmailRefusedWithoutTheCapability is the security gate: the POST
// route must enforce the same two capabilities that decide whether the form
// is rendered at all (Handler.emailSectionAvailable), rather than assume an
// unrendered form means an unreachable endpoint.
//
// Without that gate, on production -- where the section never renders,
// because an external identity provider owns identity -- any authenticated
// user could post an arbitrary address and have this instance mail a
// verification link to it, once per request (each POST mints a fresh token,
// so the notification dedupe never applies), and could point their own
// notification_email away from the address the identity provider owns.
//
// The address posted is well-formed on purpose: it passes
// notification.Deliverable, so the only thing that can refuse the request is
// the capability check under test. The handler carries a nil *cs.PG, so a
// request that is not refused reaches cs.PG.Get and panics into a 500 --
// which is exactly what the ungated code does here.
func TestSetEmailRefusedWithoutTheCapability(t *testing.T) {
	for _, tt := range []struct {
		name             string
		identityEditable bool
		withMail         bool
		noService        bool
	}{
		{
			// Production: an external identity provider owns the identity,
			// so this account's address is not ours to change even though
			// mail works.
			name:             "identity owned by an external provider",
			identityEditable: false,
			withMail:         true,
		},
		{
			// Default self-hosted: nothing external owns identity, but with
			// no transport there is no way to verify an address, so there is
			// nothing an address could achieve.
			name:             "no mail transport",
			identityEditable: true,
			withMail:         false,
		},
		{
			name:             "no notification service at all",
			identityEditable: true,
			noService:        true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mail := &countingMailer{}
			h := &Handler{identityEditable: tt.identityEditable}
			switch {
			case tt.noService:
				h.notification = nil
			case tt.withMail:
				h.notification = notificationServiceWithMail(mail)
			default:
				h.notification = notificationServiceWithoutMail()
			}
			r := newEmailRouter(h)

			w := postEmail(r, "victim@example.com")

			if w.Code != http.StatusNotFound {
				t.Errorf("status: got %d, want 404 -- the route must refuse what the section is not rendered for (Location %q)",
					w.Code, w.Header().Get("Location"))
			}
			if len(mail.sent) != 0 {
				t.Errorf("letters sent: %v, want none -- a refused request must not mail anything", mail.sent)
			}
		})
	}
}

// TestSetEmailRejectsUndeliverableAddress is task 7's first rule: an
// undeliverable address must be rejected through the existing form-error
// mechanism, and nothing may be stored.
//
// Both capabilities are granted here, which also makes this the control on
// TestSetEmailRefusedWithoutTheCapability above: it shows the 404 there
// comes from the gate, not from a route that refuses everything.
//
// The handler still has a nil *cs.PG: setEmail must reject the address and
// return before it ever calls s.pg.Get(), which panics on a nil receiver
// (cs.PG.Get locks a mutex field before checking anything else). So this
// test does not merely check the redirect -- if the deliverability check
// were skipped, weakened, or moved after the DB call, the request would
// come back 500 from Recovery instead of redirecting, which is a strictly
// stronger signal that nothing reached storage.
func TestSetEmailRejectsUndeliverableAddress(t *testing.T) {
	mail := &countingMailer{}
	h := &Handler{identityEditable: true, notification: notificationServiceWithMail(mail)}
	r := newEmailRouter(h)

	w := postEmail(r, "not-an-email")

	if !strings.Contains(w.Header().Get("Location"), "err=profile.email.invalid") {
		t.Errorf("undeliverable address was not reported as a failure; got status %d, redirected to %q",
			w.Code, w.Header().Get("Location"))
	}
	if len(mail.sent) != 0 {
		t.Errorf("letters sent: %v, want none", mail.sent)
	}
}
