package notification

import (
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gopkg.in/gomail.v2"
)

// mailer is the transport a Service mails through. It has one method on
// purpose: a mailer that exists can send, and one that does not exist is
// how "this instance has no SMTP server" is spelled. There used to be a
// Configured() bool here, which meant a mailer could sit in the Service
// answering every call with "I cannot do this" -- the capability is now
// carried by the value's presence instead, so there is no second source of
// truth to keep in step.
type mailer interface {
	Send(to, subject, body string) error
}

type smtpMailer struct {
	host   string
	port   int
	user   string
	pass   string
	from   string
	secure bool
}

func (m *smtpMailer) fromAddr() string {
	if m.from != "" {
		return m.from
	}
	return m.user
}

// Send dials the configured SMTP server. There is no empty-host guard here
// because there is no way to get an smtpMailer without a host: New only
// builds one when the flag is set (see notification.go).
func (m *smtpMailer) Send(to, subject, body string) error {
	msg := gomail.NewMessage()
	msg.SetAddressHeader("From", m.fromAddr(), "Webtor")
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
