package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"github.com/webtor-io/lazymap"
)

const (
	useFlag           = "use-payments"
	webhookHostFlag   = "webhook-service-host"
	webhookPortFlag   = "webhook-service-port"
	webhookSecureFlag = "webhook-secure"

	// Provider is the default provider identifier for crypto checkouts,
	// passed to the webhook service's provider-agnostic invoice API.
	Provider = "nowpayments"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.BoolFlag{
			Name:   useFlag,
			Usage:  "enable prepaid membership purchases via the webhook invoice API",
			EnvVar: "USE_PAYMENTS",
		},
		cli.StringFlag{
			Name:   webhookHostFlag,
			Usage:  "webhook service host (auto-injected by kubernetes)",
			EnvVar: "WEBHOOK_SERVICE_HOST",
		},
		cli.IntFlag{
			Name:   webhookPortFlag,
			Usage:  "webhook service port",
			EnvVar: "WEBHOOK_SERVICE_PORT",
			Value:  80,
		},
		cli.BoolFlag{
			Name:   webhookSecureFlag,
			Usage:  "webhook secure (https)",
			EnvVar: "WEBHOOK_SECURE",
		},
	)
}

type Price struct {
	TierID     int     `json:"tier_id"`
	TierName   string  `json:"tier_name"`
	PeriodDays int     `json:"period_days"`
	AmountUSD  float64 `json:"amount_usd"`
	// Available is a pointer so a webhook build that predates the field
	// (absent key) reads as available rather than as false.
	Available *bool `json:"available"`
}

// IsAvailable reports whether the plan can currently be purchased.
func (p Price) IsAvailable() bool {
	return p.Available == nil || *p.Available
}

type Payment struct {
	PaymentID  string  `json:"id"`
	UserID     string  `json:"user_id"`
	Status     string  `json:"status"`
	TierID     int     `json:"tier_id"`
	PeriodDays int     `json:"period_days"`
	AmountUSD  float64 `json:"amount_usd"`
	InvoiceURL string  `json:"url"`
}

type Invoice struct {
	PaymentID  string  `json:"id"`
	InvoiceURL string  `json:"url"`
	AmountUSD  float64 `json:"amount_usd"`
}

type Client struct {
	url         string
	cl          *http.Client
	pricesCache *lazymap.LazyMap[[]Price]
}

// New returns nil unless payments are enabled AND the webhook service is
// resolvable — callers treat a nil client as "crypto payments disabled".
// Host/port come from the kubernetes-injected WEBHOOK_SERVICE_* env vars, so
// only the USE_PAYMENTS switch lives in the deployment values.
func New(c *cli.Context) *Client {
	if !c.Bool(useFlag) {
		return nil
	}
	host := c.String(webhookHostFlag)
	if host == "" {
		return nil
	}
	scheme := "http"
	if c.Bool(webhookSecureFlag) {
		scheme = "https"
	}
	return &Client{
		url: fmt.Sprintf("%s://%s:%d", scheme, host, c.Int(webhookPortFlag)),
		cl:  &http.Client{Timeout: 30 * time.Second},
		pricesCache: lazymap.New[[]Price](&lazymap.Config{
			Expire:      5 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
	}
}

func (s *Client) Prices(_ context.Context) ([]Price, error) {
	return s.pricesCache.Get("prices", func() ([]Price, error) {
		// Deliberately not the caller's context: the fetch is shared
		// across requests via lazymap, and one cancelled request must not
		// fail it for every waiter (the error would be cached too).
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var out struct {
			Prices []Price `json:"prices"`
		}
		if err := s.do(ctx, http.MethodGet, "/prices", nil, &out); err != nil {
			return nil, err
		}
		return out.Prices, nil
	})
}

type CreateInvoiceRequest struct {
	Provider   string `json:"provider"`
	UserID     string `json:"user_id"`
	Email      string `json:"email"`
	TierID     int    `json:"tier_id"`
	PeriodDays int    `json:"period_days"`
}

// CreateInvoice PUTs a freshly generated uuid. The API treats PUT to an
// existing id as "return the stored invoice", so any retry that reuses the
// id (proxy replay, future retry logic) cannot double-charge.
func (s *Client) CreateInvoice(ctx context.Context, req *CreateInvoiceRequest) (*Invoice, error) {
	if req.Provider == "" {
		req.Provider = Provider
	}
	id := uuid.NewString()
	var out Invoice
	if err := s.do(ctx, http.MethodPut, "/invoice/"+id, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type PaymentListItem struct {
	PaymentID   string    `json:"id"`
	Provider    string    `json:"provider"`
	Status      string    `json:"status"`
	TierID      int       `json:"tier_id"`
	TierName    string    `json:"tier_name"`
	PeriodDays  int       `json:"period_days"`
	AmountUSD   float64   `json:"amount_usd"`
	PayCurrency string    `json:"pay_currency"`
	InvoiceURL  string    `json:"url"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListPayments returns the user's payment history, newest first.
func (s *Client) ListPayments(ctx context.Context, userID string) ([]PaymentListItem, error) {
	var out struct {
		Invoices []PaymentListItem `json:"invoices"`
	}
	if err := s.do(ctx, http.MethodGet, "/invoices?user_id="+url.QueryEscape(userID), nil, &out); err != nil {
		return nil, err
	}
	return out.Invoices, nil
}

func (s *Client) GetPayment(ctx context.Context, id string) (*Payment, error) {
	var out Payment
	if err := s.do(ctx, http.MethodGet, "/invoice/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Client) do(ctx context.Context, method, path string, in any, out any) error {
	var body io.Reader
	if in != nil {
		bb, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(bb)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.url+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := s.cl.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.Errorf("gateway request %v %v failed status=%v", method, path, res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}
