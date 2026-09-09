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
	// The action pair backs an *invisible* widget that gates the five job
	// starts (download/stream/preview) for anonymous visitors; the pair
	// above backs the managed widget on the support form. Separate widgets
	// because the modes differ, and so that one can be turned off alone.
	actionSiteKeyFlag   = "turnstile-action-site-key"
	actionSecretKeyFlag = "turnstile-action-secret-key"
	verifyURL           = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
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
		cli.StringFlag{
			Name:   actionSiteKeyFlag,
			Usage:  "Cloudflare Turnstile site key of the invisible widget gating downloads and streams",
			EnvVar: "TURNSTILE_ACTION_SITE_KEY",
		},
		cli.StringFlag{
			Name:   actionSecretKeyFlag,
			Usage:  "Cloudflare Turnstile secret key of the invisible widget gating downloads and streams",
			EnvVar: "TURNSTILE_ACTION_SECRET_KEY",
		},
	)
}

type Service struct {
	siteKey   string
	secretKey string
	client    *http.Client
}

func New(c *cli.Context) *Service {
	return newService(c.String(siteKeyFlag), c.String(secretKeyFlag))
}

// NewAction is the verifier for the invisible widget on job starts; nil
// when the pair is not configured, and then nothing is checked.
func NewAction(c *cli.Context) *Service {
	return newService(c.String(actionSiteKeyFlag), c.String(actionSecretKeyFlag))
}

func newService(sk, secret string) *Service {
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
	siteKey       string
	actionSiteKey string
}

func NewHelper(c *cli.Context) *Helper {
	return &Helper{
		siteKey:       c.String(siteKeyFlag),
		actionSiteKey: c.String(actionSiteKeyFlag),
	}
}

// UseActionTurnstile reports whether job starts are gated by the invisible
// widget; templates load the Turnstile script and render its container
// only then.
func (h *Helper) UseActionTurnstile() bool {
	return h.actionSiteKey != ""
}

func (h *Helper) ActionTurnstileSiteKey() string {
	return h.actionSiteKey
}

func (h *Helper) UseTurnstile() bool {
	return h.siteKey != ""
}

func (h *Helper) TurnstileSiteKey() string {
	return h.siteKey
}
