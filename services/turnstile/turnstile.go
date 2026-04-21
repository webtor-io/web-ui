package turnstile

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

const (
	siteKeyFlag   = "turnstile-site-key"
	secretKeyFlag = "turnstile-secret-key"
	verifyURL     = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   siteKeyFlag,
			Usage:  "Cloudflare Turnstile site key",
			EnvVar: "TURNSTILE_SITE_KEY",
		},
		cli.StringFlag{
			Name:   secretKeyFlag,
			Usage:  "Cloudflare Turnstile secret key",
			EnvVar: "TURNSTILE_SECRET_KEY",
		},
	)
}

type Service struct {
	siteKey   string
	secretKey string
	client    *http.Client
}

func New(c *cli.Context) *Service {
	sk := c.String(siteKeyFlag)
	secret := c.String(secretKeyFlag)
	if sk == "" || secret == "" {
		return nil
	}
	return &Service{
		siteKey:   sk,
		secretKey: secret,
		client:    http.DefaultClient,
	}
}

type verifyResponse struct {
	Success bool `json:"success"`
}

func (s *Service) Validate(token string, remoteIP string) error {
	if token == "" {
		return errors.New("missing turnstile token")
	}
	form := url.Values{
		"secret":   {s.secretKey},
		"response": {token},
	}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	resp, err := s.client.PostForm(verifyURL, form)
	if err != nil {
		return errors.Wrap(err, "failed to verify turnstile token")
	}
	defer resp.Body.Close()
	var result verifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return errors.Wrap(err, "failed to decode turnstile response")
	}
	if !result.Success {
		return errors.New("turnstile verification failed")
	}
	return nil
}

func (s *Service) SiteKey() string {
	return s.siteKey
}

// Helper provides template functions for Turnstile.
type Helper struct {
	siteKey string
}

func NewHelper(c *cli.Context) *Helper {
	return &Helper{
		siteKey: c.String(siteKeyFlag),
	}
}

func (h *Helper) UseTurnstile() bool {
	return h.siteKey != ""
}

func (h *Helper) TurnstileSiteKey() string {
	return h.siteKey
}
