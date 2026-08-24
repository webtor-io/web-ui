package notification

import (
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gopkg.in/gomail.v2"
)

type mailer interface {
	Send(to, subject, body string) error
	// Configured reports whether this mailer can actually reach an SMTP
	// server. It is the capability a caller outside this package needs
	// before offering anything that depends on mail actually going out
	// (e.g. an address that can only be confirmed by emailing it a link) --
	// asking the mailer directly keeps that answer tied to the one place
	// that knows how SMTP was set up, instead of a caller re-reading flags.
	Configured() bool
}

// ErrNotConfigured means no SMTP server is configured, so nothing was even
// attempted. It is not a delivery failure and callers must not treat it as
// one -- in particular, a notification recorded as mailed on the strength of
// this would suppress the real send once SMTP arrives.
var ErrNotConfigured = errors.New("smtp is not configured")

type smtpMailer struct {
	host   string
	port   int
	user   string
	pass   string
	from   string
	secure bool
}

func (m *smtpMailer) Configured() bool {
	return m.host != ""
}

func (m *smtpMailer) fromAddr() string {
	if m.from != "" {
		return m.from
	}
	return m.user
}

func (m *smtpMailer) Send(to, subject, body string) error {
	if m.host == "" {
		log.Warn("SMTP host not configured, skipping email sending")
		return ErrNotConfigured
	}

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
