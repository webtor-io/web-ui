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
	"github.com/webtor-io/web-ui/services/vault"
)

type Service struct {
	store                 notificationStore
	mail                  mailer
	domain                string
	templateDir           string
	transferTimeoutPeriod time.Duration
}

func New(c *cli.Context, db *pg.DB) *Service {
	return &Service{
		store: &pgNotificationStore{db: db},
		mail: &smtpMailer{
			host:   c.String(common.SMTPHostFlag),
			port:   c.Int(common.SMTPPortFlag),
			user:   c.String(common.SMTPUserFlag),
			pass:   c.String(common.SMTPPassFlag),
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
	body, err := s.render(opts.Template, opts.Data)
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

func (s *Service) render(templateName string, data any) (string, error) {
	path := filepath.Join(s.templateDir, templateName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("template not found: %s", path)
	}

	t, err := template.ParseFiles(path)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse template")
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", errors.Wrap(err, "failed to execute template")
	}

	return buf.String(), nil
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
