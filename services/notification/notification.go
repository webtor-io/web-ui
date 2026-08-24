package notification

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/hako/durafmt"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
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

// New builds the mailer. The i18n service is what lets a template say
// {{ t "email.something" }} instead of carrying English text: notifications
// are rendered in a cron job, where the URL prefix and the lang cookie that
// pick a language everywhere else do not exist. It may be nil, in which case
// templates fall back to their message keys — callers that have a bundle
// should always pass it.
// Store is the notification journal — what makes the 24-hour duplicate
// check possible. Mailer is the transport. Both are exported so a caller
// can assemble a Service from parts it already has (see NewWith), which is
// how the subscription end-to-end test drives real templates and real
// translations without SMTP or a database.
type Store = notificationStore

type Mailer = mailer

// NewWith assembles a Service from explicit parts.
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
	return &Service{
		i18n:  i18nSvc,
		store: &pgNotificationStore{db: db},
		mail: &smtpMailer{
			host:   c.String(common.SMTPHostFlag),
			port:   c.Int(common.SMTPPortFlag),
			user:   c.String(common.SMTPUserFlag),
			pass:   c.String(common.SMTPPassFlag),
			from:   c.String(common.SMTPFromFlag),
			secure: c.Bool(common.SMTPSecureFlag),
		},
		domain:                c.String(common.DomainFlag),
		templateDir:           "templates/notification",
		transferTimeoutPeriod: c.Duration(vault.VaultResourceTransferTimeoutPeriodFlag),
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

	if err := s.mail.Send(*n.To, opts.Title, body); err != nil {
		if errors.Is(err, ErrNotConfigured) {
			// Expected on an instance with no mail server. The feed entry
			// above is the delivery; say so once at debug volume rather than
			// reporting a failure that is not one.
			return nil
		}
		return errors.Wrap(err, "failed to send email")
	}
	return s.store.MarkMailed(ctx, n.NotificationID)
}

// MailConfigured reports whether this Service can actually send mail. It is
// the capability check a caller outside this package needs before offering
// a feature that only makes sense with a working SMTP server -- e.g. an
// address that can only be confirmed by emailing it a verification link.
// Without this there is nothing such a feature could do, so gating on it is
// what makes "verify before use" possible at all.
func (s *Service) MailConfigured() bool {
	return s.mail != nil && s.mail.Configured()
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

// SendEmailVerification mails the single-use link that confirms a pending
// notification address (handlers/profile.setEmail). The dedupe key is keyed
// on the token, not just the destination address: a re-submission of the
// same address gets a fresh token (models.SetPendingEmail overwrites the
// old one), and it must produce a fresh email too, or Service.Send's
// 24-hour duplicate window would silently swallow the resend while the
// stale, already-mailed link's token no longer matches anything in the
// database.
func (s *Service) SendEmailVerification(to string, userID uuid.UUID, token, link string) error {
	opts := SendOptions{
		To:       to,
		UserID:   userID,
		Key:      fmt.Sprintf("email-verify-%s", token),
		Title:    "Confirm your notification email",
		Template: "verify-email.html",
		Data: map[string]any{
			"Link": link,
		},
	}
	return s.Send(opts)
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
