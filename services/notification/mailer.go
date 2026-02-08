package notification

import (
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gopkg.in/gomail.v2"
)

type mailer interface {
	Send(to, subject, body string) error
}

type smtpMailer struct {
	host   string
	port   int
	user   string
	pass   string
	secure bool
}

func (m *smtpMailer) Send(to, subject, body string) error {
	if m.host == "" {
		log.Warn("SMTP host not configured, skipping email sending")
		return nil
	}

	msg := gomail.NewMessage()
	msg.SetHeader("From", m.user)
	msg.SetHeader("To", to)
	msg.SetHeader("Subject", subject)
	msg.SetBody("text/html", body)

	d := gomail.NewDialer(m.host, m.port, m.user, m.pass)
	d.SSL = m.secure

	if err := d.DialAndSend(msg); err != nil {
		return errors.Wrap(err, "failed to send email via SMTP")
	}

	log.WithField("to", to).Info("email sent successfully")

	return nil
}
