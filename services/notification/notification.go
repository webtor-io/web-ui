package notification

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/hako/durafmt"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/vault"
)

type Service struct {
	store                 notificationStore
	mail                  mailer
	i18n                  *i18n.Service
	domain                string
	templateDir           string
	transferTimeoutPeriod time.Duration
}

// New builds the Service from CLI flags. The i18n service is what lets a template say
// {{ t "email.something" }} instead of carrying English text: notifications
// are rendered in a cron job, where the URL prefix and the lang cookie that
// pick a language everywhere else do not exist. It may be nil, in which case
// templates fall back to their message keys — callers that have a bundle
// should always pass it.
// Store is the notification journal — what makes the 24-hour duplicate
// check possible. Mailer is the transport, and is optional: an instance with
// no SMTP server simply has none. Both are exported so a caller can assemble
// a Service from parts it already has (see NewWith), which is how the
// subscription end-to-end test drives real templates and real translations
// without SMTP or a database.
type Store = notificationStore

type Mailer = mailer

// NewWith assembles a Service from explicit parts.
//
// Contract for mail: pass a working transport, or nil when this instance has
// none -- absence of a mailer is how "this instance cannot send mail" is
// spelled, and it is what MailConfigured answers.
//
// Say "no mail" with a plain nil. A typed nil pointer ((*smtpMailer)(nil), or
// a nil fake) is not the same thing: an interface holding one compares
// non-nil, so a naive check would read it as "mail is available" and then
// panic on the first call -- the same mistake that has already cost this
// codebase two bugs (a typed-nil *models.User in the auth request context, a
// nil-receiver panic in claims.Client.Close). The Service does not trust the
// bare comparison (see hasMail), so a typed nil is survivable rather than
// fatal, but it is still not the way to say it.
func NewWith(store Store, mail Mailer, i18nSvc *i18n.Service, domain, templateDir string) *Service {
	return &Service{
		store:       store,
		mail:        mail,
		i18n:        i18nSvc,
		domain:      domain,
		templateDir: templateDir,
	}
}

func New(c *cli.Context, db *pg.DB, i18nSvc *i18n.Service) *Service {
	s := &Service{
		i18n:                  i18nSvc,
		store:                 &pgNotificationStore{db: db},
		domain:                c.String(common.DomainFlag),
		templateDir:           "templates/notification",
		transferTimeoutPeriod: c.Duration(vault.VaultResourceTransferTimeoutPeriodFlag),
	}
	// The mailer is built only where there is an SMTP server to build it
	// around. With no host there is no transport, and the Service says that
	// by not having one -- rather than by carrying a mailer that answers
	// every call with a refusal.
	if host := c.String(common.SMTPHostFlag); host != "" {
		s.mail = &smtpMailer{
			host:   host,
			port:   c.Int(common.SMTPPortFlag),
			user:   c.String(common.SMTPUserFlag),
			pass:   c.String(common.SMTPPassFlag),
			from:   c.String(common.SMTPFromFlag),
			secure: c.Bool(common.SMTPSecureFlag),
		}
	} else {
		// Said once, at startup, because an absent capability gets explained
		// rather than silently hidden: notifications still reach the user
		// through the in-app feed, and nothing is queued waiting for a mail
		// server that is never going to appear. This replaces a warning that
		// used to fire on every send from a mailer that existed only to
		// refuse -- once per process is the right volume for a fact about
		// configuration.
		log.Info("SMTP_HOST is not set: no mail transport, notifications are delivered to the in-app feed only")
	}
	return s
}

// hasMail reports whether this Service can put a letter on the wire. It is
// the single presence check -- Send and MailConfigured both go through it, so
// there is one place to get right rather than two comparisons to keep in step.
//
// It is deliberately not a bare `s.mail != nil`: an interface holding a typed
// nil pointer compares non-nil and would pass that check, then panic when
// called. Normalising in NewWith instead was considered and dropped: it would
// leave this predicate looking safe while only one of the two construction
// paths was, and neither guard could then be shown to matter on its own.
func (s *Service) hasMail() bool {
	if s.mail == nil {
		return false
	}
	switch v := reflect.ValueOf(s.mail); v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return !v.IsNil()
	default:
		return true
	}
}

type SendOptions struct {
	To       string
	UserID   uuid.UUID
	Key      string
	Title    string
	Template string
	Data     any
	// Lang renders the template through the i18n bundle. Empty means the
	// default language, which is what the older English-only templates use.
	Lang string
}

func (s *Service) Send(opts SendOptions) error {
	ctx := context.Background()

	// Dedupe on what was actually mailed. A row with mailed_at NULL is a
	// feed entry whose letter never left -- either there is no SMTP server
	// or the send failed -- and must not suppress a later attempt.
	last, err := s.store.GetLastMailedByKeyAndUser(ctx, opts.Key, opts.UserID)
	if err != nil {
		return errors.Wrap(err, "failed to check for duplicate notification")
	}
	// last.MailedAt != nil looks redundant -- the query above is documented
	// to only return rows with mailed_at IS NOT NULL -- but that predicate
	// lives in a different file behind the notificationStore interface,
	// where nothing in this package enforces it. Send runs inside a bare
	// `go f()` in release_subscription (no recover()), so a store
	// implementation, a cache layer, or a hand-run fix that ever hands back
	// a row with MailedAt nil turns a bad dedupe hit into a nil-pointer
	// panic that takes the whole process down, not one failed send. Do not
	// delete this check.
	mailedRecently := last != nil && last.MailedAt != nil && time.Since(*last.MailedAt) < 24*time.Hour

	body, err := s.render(opts.Template, opts.Lang, opts.Data)
	if err != nil {
		return errors.Wrap(err, "failed to render notification template")
	}

	// The entry is written before anything is sent, because the entry IS the
	// notification -- the letter is one way of carrying it, and a user with
	// no deliverable address must still be told. This inverts the previous
	// ordering, which existed so that a journal row implied a letter had
	// left. That property is preserved by mailed_at instead: it is stamped
	// only after an SMTP server accepts the message, so a failed send still
	// leaves nothing that looks like a delivery.
	n := &models.Notification{
		Key:      opts.Key,
		Title:    opts.Title,
		Template: opts.Template,
		Body:     body,
		UserID:   &opts.UserID,
	}
	if Deliverable(opts.To) {
		to := opts.To
		n.To = &to
	}
	if err := s.store.Create(ctx, n); err != nil {
		return errors.Wrap(err, "failed to save notification to db")
	}

	if n.To == nil || mailedRecently {
		return nil
	}

	// No transport, nothing to attempt. On an instance that was never given
	// an SMTP server the feed entry written above IS the delivery, so this is
	// a success, not a failure to report. mailed_at stays NULL, which is what
	// keeps the letter owed once a server appears.
	if !s.hasMail() {
		return nil
	}

	// The feed got the fragment; the wire gets it wrapped in a document.
	// Wrapping happens here rather than in render so the stored body stays
	// the thing the feed can show.
	letter, err := s.wrapEmail(body, opts.Lang)
	if err != nil {
		return errors.Wrap(err, "failed to render email layout")
	}

	if err := s.mail.Send(*n.To, opts.Title, letter); err != nil {
		return errors.Wrap(err, "failed to send email")
	}
	return s.store.MarkMailed(ctx, n.NotificationID)
}

// MailConfigured reports whether this Service can actually send mail. The
// answer is not a flag anyone read: the Service either was given a mail
// transport or was not, and having one is the whole of the capability.
//
// It is the check a caller outside this package needs before offering a
// feature that only makes sense with a working SMTP server -- e.g. an address
// that can only be confirmed by emailing it a verification link. Without a
// transport there is nothing such a feature could do, so gating on it is what
// makes "verify before use" possible at all.
func (s *Service) MailConfigured() bool {
	return s.hasMail()
}

// CountUnread returns how many of a user's notifications have not been
// read yet. Thin pass-through to the store so callers outside this package
// (the per-request middleware in serve.go) never reach past Service into
// the unexported notificationStore.
func (s *Service) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.store.CountUnread(ctx, userID)
}

// ListByUser returns a user's most recent notifications, newest first. Thin
// pass-through to the store, same reasoning as CountUnread: it keeps the
// notifications page (and anything else outside this package) off the
// unexported notificationStore.
func (s *Service) ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]models.Notification, error) {
	return s.store.ListByUser(ctx, userID, limit)
}

// MarkAllRead marks every one of a user's notifications as read. Thin
// pass-through, same reasoning as ListByUser.
func (s *Service) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	return s.store.MarkAllRead(ctx, userID)
}

// PruneKeepingNewest caps the notification feed by deleting everything
// past the newest `keep` entries for every user. Called from the
// "notification send" cron subcommand, after the sending work for that run
// -- pruning first could delete rows the run still needed to reference
// (e.g. for the mailed-recently dedupe check).
func (s *Service) PruneKeepingNewest(ctx context.Context, keep int) error {
	return s.store.PruneKeepingNewest(ctx, keep)
}

// emailLayout is the one document wrapper every letter goes out in. It lives
// next to the message templates because it is written and edited by whoever
// writes them, but it is not one of them: render never resolves a caller's
// template name to it, only wrapEmail names it.
const emailLayout = "layout.html"

// render executes a message template and returns the fragment it produced.
//
// A fragment is the message and nothing else -- no DOCTYPE, no <html>, no
// sign-off. Every one of these templates used to be a whole document, which
// is what put "<!DOCTYPE html> <html> <body>" on screen in the in-app feed:
// the feed shows the stored body, and a document has no place inside a list
// row. The wrapper is now wrapEmail's job, so there is exactly one body and
// two presentations of it, rather than two bodies that drift apart.
//
// The return value is the output of html/template, which is what makes it
// safe for the feed to show as markup: every {{ . }} in the template was
// escaped as it was written, so a torrent name carrying "<script>" is in
// there as "&lt;script&gt;". Anything that changes this function to skip
// that escaping (text/template, or interpolating data into the template
// source) breaks the feed's safety, not just this package's -- see
// TestRenderedBodyEscapesHostileNames.
func (s *Service) render(templateName string, lang string, data any) (string, error) {
	path := filepath.Join(s.templateDir, templateName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("template not found: %s", path)
	}

	t, err := template.New(filepath.Base(path)).Funcs(s.funcs(lang)).ParseFiles(path)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse template")
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", errors.Wrap(err, "failed to execute template")
	}

	return buf.String(), nil
}

// wrapEmail puts a rendered fragment inside the shared email document, which
// is what a mail client needs and a feed row does not: DOCTYPE, <html>,
// <body> and the sign-off.
//
// fragment is passed as template.HTML, i.e. deliberately not escaped again.
// That is only correct because fragment came out of render, which is
// html/template -- the attacker-controlled parts of it (torrent and resource
// names) were escaped when the fragment was built. Do not call this with a
// string from anywhere else.
func (s *Service) wrapEmail(fragment string, lang string) (string, error) {
	path := filepath.Join(s.templateDir, emailLayout)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("email layout not found: %s", path)
	}

	t, err := template.New(emailLayout).Funcs(s.funcs(lang)).ParseFiles(path)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse email layout")
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, map[string]any{"Content": template.HTML(fragment)}); err != nil {
		return "", errors.Wrap(err, "failed to execute email layout")
	}

	return buf.String(), nil
}

// funcs binds the translation helpers to one language, so a template writes
// {{ t "key" }} rather than threading the language through every call.
// Without a bundle both helpers return the key, which renders a readable
// placeholder instead of an empty email.
func (s *Service) funcs(lang string) template.FuncMap {
	if lang == "" {
		lang = i18n.DefaultLang
	}
	return template.FuncMap{
		"t": func(key string) string {
			if s.i18n == nil {
				return key
			}
			return i18n.TranslateWithLocalizer(s.i18n.Localizer(lang), key)
		},
		"tp": func(key string, args ...any) string {
			if s.i18n == nil {
				return key
			}
			data := make(map[string]any, len(args)/2)
			for i := 0; i+1 < len(args); i += 2 {
				data[fmt.Sprintf("%v", args[i])] = args[i+1]
			}
			return i18n.TranslateWithLocalizerData(s.i18n.Localizer(lang), key, data)
		},
	}
}

// T translates a key in the given language. Exposed because a subject line
// is not part of the template body but still has to be localised.
func (s *Service) T(lang, key string, args ...any) string {
	f, _ := s.funcs(lang)["tp"].(func(string, ...any) string)
	if f == nil {
		return key
	}
	return f(key, args...)
}

func (s *Service) resourceURL(resourceID string) string {
	return fmt.Sprintf("%s/%s", s.domain, resourceID)
}

func (s *Service) resourceData(r *vaultModels.Resource) map[string]any {
	return map[string]any{
		"Name":   r.Name,
		"URL":    s.resourceURL(r.ResourceID),
		"Domain": s.domain,
	}
}

func (s *Service) SendVaulted(to string, userID uuid.UUID, r *vaultModels.Resource) error {
	opts := SendOptions{
		To:       to,
		UserID:   userID,
		Key:      fmt.Sprintf("vaulted-%s", r.ResourceID),
		Title:    fmt.Sprintf("Your resource %s has been vaulted!", r.Name),
		Template: "vaulted.html",
		Data:     s.resourceData(r),
	}
	return s.Send(opts)
}

type expiringResource struct {
	Name string
	URL  string
}

func (s *Service) SendExpiring(to string, userID uuid.UUID, days int, resources []vaultModels.Resource) error {
	expResources := make([]expiringResource, len(resources))
	for i, r := range resources {
		expResources[i] = expiringResource{
			Name: r.Name,
			URL:  s.resourceURL(r.ResourceID),
		}
	}

	opts := SendOptions{
		To:       to,
		UserID:   userID,
		Key:      fmt.Sprintf("expiring-%d", days),
		Title:    fmt.Sprintf("Your resources will disappear in %d days!", days),
		Template: "expiring.html",
		Data: map[string]any{
			"Days":      days,
			"Resources": expResources,
			"Domain":    s.domain,
		},
	}
	return s.Send(opts)
}

func (s *Service) SendTransferTimeout(to string, userID uuid.UUID, r *vaultModels.Resource) error {
	timeoutStr := durafmt.Parse(s.transferTimeoutPeriod).LimitFirstN(2).String()
	data := s.resourceData(r)
	data["Timeout"] = timeoutStr
	opts := SendOptions{
		To:       to,
		UserID:   userID,
		Key:      fmt.Sprintf("transfer-timeout-%s", r.ResourceID),
		Title:    fmt.Sprintf("We were unable to transfer your resource %s", r.Name),
		Template: "transfer-timeout.html",
		Data:     data,
	}
	return s.Send(opts)
}

// mailOnly renders a template and puts it on the wire without writing
// anything to the notification journal. It is the exception to Send, not a
// variant of it, and exists for one class of message: transactional mail
// whose entire content is a secret addressed to whoever holds the mailbox.
//
// Send's guarantee -- the row is written first and unconditionally, because
// the row IS the notification -- is exactly wrong for such a message. The
// feed is readable by the account that submitted the address, so publishing
// a verification link there hands the token to the submitter, who can then
// confirm a mailbox they have no access to. That is the one thing
// verification exists to prevent, so the row must not exist at all rather
// than exist with its body redacted.
//
// Everything else keeps going through Send. Do not add a "skip the feed"
// switch to SendOptions: the property that every ordinary notification
// reaches the feed is only as strong as the number of ways there are to
// opt out of it.
//
// With no transport this is a no-op and not an error: an address is only
// ever offered where mail works (handlers/profile.emailSectionAvailable),
// so a mailless instance has nothing to send and nothing to report. An
// address that is not deliverable is the same kind of nothing -- and unlike
// Send there is no feed entry left behind to make the difference visible.
func (s *Service) mailOnly(to, subject, templateName string, data any) error {
	if !Deliverable(to) || !s.hasMail() {
		return nil
	}
	body, err := s.render(templateName, "", data)
	if err != nil {
		return errors.Wrap(err, "failed to render notification template")
	}
	// Never reaches the feed, but it is still a letter, so it still needs to
	// be a document -- the layout is what makes it one.
	letter, err := s.wrapEmail(body, "")
	if err != nil {
		return errors.Wrap(err, "failed to render email layout")
	}
	if err := s.mail.Send(to, subject, letter); err != nil {
		return errors.Wrap(err, "failed to send email")
	}
	return nil
}

// SendEmailVerification mails the single-use link that confirms a pending
// notification address (handlers/profile.setEmail).
//
// It goes through mailOnly rather than Send because the link is the token:
// a feed entry carrying it would let the account that submitted the address
// read the link out of its own feed and confirm a mailbox it never had
// access to. There is nothing to dedupe for the same reason -- the 24-hour
// window is a property of the journal, and each submission mints a fresh
// token (models.SetPendingEmail overwrites the old one) that must produce a
// fresh letter regardless.
func (s *Service) SendEmailVerification(to, link string) error {
	return s.mailOnly(to, "Confirm your notification email", "verify-email.html", map[string]any{
		"Link": link,
	})
}

func (s *Service) SendExpired(to string, userID uuid.UUID, r *vaultModels.Resource) error {
	opts := SendOptions{
		To:       to,
		UserID:   userID,
		Key:      fmt.Sprintf("expired-%s", r.ResourceID),
		Title:    fmt.Sprintf("Your resource %s has expired", r.Name),
		Template: "expired.html",
		Data:     s.resourceData(r),
	}
	return s.Send(opts)
}
