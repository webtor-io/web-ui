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

// New builds the mailer. The i18n service is what lets a template say
// {{ t "email.something" }} instead of carrying English text: notifications
// are rendered in a cron job, where the URL prefix and the lang cookie that
// pick a language everywhere else do not exist. It may be nil, in which case
// templates fall back to their message keys — callers that have a bundle
// should always pass it.
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

	// 1. Check for duplicates in the last 24 hours
	last, err := s.store.GetLastByKeyAndTo(ctx, opts.Key, opts.To)
	if err != nil {
		return errors.Wrap(err, "failed to check for duplicate notification")
	}
	if last != nil && time.Since(last.CreatedAt) < 24*time.Hour {
		log.WithFields(log.Fields{
			"key": opts.Key,
			"to":  opts.To,
		}).Info("duplicate notification, skipping")
		return nil
	}

	// 2. Render template
	body, err := s.render(opts.Template, opts.Lang, opts.Data)
	if err != nil {
		return errors.Wrap(err, "failed to render notification template")
	}

	// 3. Save to DB
	n := &models.Notification{
		Key:      opts.Key,
		Title:    opts.Title,
		Template: opts.Template,
		Body:     body,
		To:       opts.To,
	}
	err = s.store.Create(ctx, n)
	if err != nil {
		return errors.Wrap(err, "failed to save notification to db")
	}

	err = s.mail.Send(opts.To, opts.Title, body)
	if err != nil {
		return errors.Wrap(err, "failed to send email")
	}

	return nil
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

func (s *Service) SendVaulted(to string, r *vaultModels.Resource) error {
	opts := SendOptions{
		To:       to,
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

func (s *Service) SendExpiring(to string, days int, resources []vaultModels.Resource) error {
	expResources := make([]expiringResource, len(resources))
	for i, r := range resources {
		expResources[i] = expiringResource{
			Name: r.Name,
			URL:  s.resourceURL(r.ResourceID),
		}
	}

	opts := SendOptions{
		To:       to,
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

func (s *Service) SendTransferTimeout(to string, r *vaultModels.Resource) error {
	timeoutStr := durafmt.Parse(s.transferTimeoutPeriod).LimitFirstN(2).String()
	data := s.resourceData(r)
	data["Timeout"] = timeoutStr
	opts := SendOptions{
		To:       to,
		Key:      fmt.Sprintf("transfer-timeout-%s", r.ResourceID),
		Title:    fmt.Sprintf("We were unable to transfer your resource %s", r.Name),
		Template: "transfer-timeout.html",
		Data:     data,
	}
	return s.Send(opts)
}

func (s *Service) SendExpired(to string, r *vaultModels.Resource) error {
	opts := SendOptions{
		To:       to,
		Key:      fmt.Sprintf("expired-%s", r.ResourceID),
		Title:    fmt.Sprintf("Your resource %s has expired", r.Name),
		Template: "expired.html",
		Data:     s.resourceData(r),
	}
	return s.Send(opts)
}
