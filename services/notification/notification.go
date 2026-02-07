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
	"gopkg.in/gomail.v2"
)

type Service struct {
	smtpHost              string
	smtpPort              int
	smtpUser              string
	smtpPass              string
	smtpSecure            bool
	db                    *pg.DB
	domain                string
	transferTimeoutPeriod time.Duration
}

func New(c *cli.Context, db *pg.DB) *Service {
	return &Service{
		smtpHost:              c.String(common.SMTPHostFlag),
		smtpPort:              c.Int(common.SMTPPortFlag),
		smtpUser:              c.String(common.SMTPUserFlag),
		smtpPass:              c.String(common.SMTPPassFlag),
		smtpSecure:            c.Bool(common.SMTPSecureFlag),
		db:                    db,
		domain:                c.String(common.DomainFlag),
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
	last, err := models.GetLastNotificationByKeyAndTo(ctx, s.db, opts.Key, opts.To)
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
	err = models.CreateNotification(ctx, s.db, n)
	if err != nil {
		return errors.Wrap(err, "failed to save notification to db")
	}

	err = s.sendEmail(opts.To, opts.Title, body)
	if err != nil {
		return errors.Wrap(err, "failed to send email")
	}

	return nil
}

func (s *Service) render(templateName string, data any) (string, error) {
	path := filepath.Join("templates", "notification", templateName)
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

func (s *Service) sendEmail(to string, title string, body string) error {
	if s.smtpHost == "" {
		log.Warn("SMTP host not configured, skipping email sending")
		return nil
	}

	m := gomail.NewMessage()
	m.SetHeader("From", s.smtpUser)
	m.SetHeader("To", to)
	m.SetHeader("Subject", title)
	m.SetBody("text/html", body)

	d := gomail.NewDialer(s.smtpHost, s.smtpPort, s.smtpUser, s.smtpPass)
	d.SSL = s.smtpSecure

	if err := d.DialAndSend(m); err != nil {
		return errors.Wrap(err, "failed to send email via SMTP")
	}

	log.WithField("to", to).Info("email sent successfully")

	return nil
}

func (s *Service) SendVaulted(to string, r *vaultModels.Resource) error {
	opts := SendOptions{
		To:       to,
		Key:      fmt.Sprintf("vaulted-%s", r.ResourceID),
		Title:    fmt.Sprintf("Your resource %s has been vaulted!", r.Name),
		Template: "vaulted.html",
		Data: map[string]any{
			"Name":   r.Name,
			"URL":    fmt.Sprintf("%s/%s", s.domain, r.ResourceID),
			"Domain": s.domain,
		},
	}
	return s.Send(opts)
}

type ExpiringResource struct {
	Name string
	URL  string
}

func (s *Service) SendExpiring(to string, days int, resources []vaultModels.Resource) error {
	expResources := make([]ExpiringResource, len(resources))
	for i, r := range resources {
		expResources[i] = ExpiringResource{
			Name: r.Name,
			URL:  fmt.Sprintf("%s/%s", s.domain, r.ResourceID),
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
	opts := SendOptions{
		To:       to,
		Key:      fmt.Sprintf("transfer-timeout-%s", r.ResourceID),
		Title:    fmt.Sprintf("We were unable to transfer your resource %s", r.Name),
		Template: "transfer-timeout.html",
		Data: map[string]any{
			"Name":    r.Name,
			"URL":     fmt.Sprintf("%s/%s", s.domain, r.ResourceID),
			"Timeout": timeoutStr,
			"Domain":  s.domain,
		},
	}
	return s.Send(opts)
}

func (s *Service) SendExpired(to string, r *vaultModels.Resource) error {
	opts := SendOptions{
		To:       to,
		Key:      fmt.Sprintf("expired-%s", r.ResourceID),
		Title:    fmt.Sprintf("Your resource %s has expired", r.Name),
		Template: "expired.html",
		Data: map[string]any{
			"Name":   r.Name,
			"URL":    fmt.Sprintf("%s/%s", s.domain, r.ResourceID),
			"Domain": s.domain,
		},
	}
	return s.Send(opts)
}
