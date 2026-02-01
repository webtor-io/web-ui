package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/webtor-io/lazymap"
)

const (
	apiHostFlag   = "vault-service-host"
	apiPortFlag   = "vault-service-port"
	apiSecureFlag = "vault-secure"
)

func RegisterApiFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   apiHostFlag,
			Usage:  "vault service host",
			EnvVar: "VAULT_SERVICE_HOST",
		},
		cli.IntFlag{
			Name:   apiPortFlag,
			Usage:  "vault service port",
			EnvVar: "VAULT_SERVICE_PORT",
			Value:  80,
		},
		cli.BoolFlag{
			Name:   apiSecureFlag,
			Usage:  "vault secure (https)",
			EnvVar: "VAULT_SECURE",
		},
	)
}

// ErrorResponse represents an error response from the Vault API
type ErrorResponse struct {
	Error string `json:"error"`
}

// Resource represents a resource in the Vault
type Resource struct {
	ResourceID string    `json:"resource_id"`
	Status     int       `json:"status"`
	StoredSize int64     `json:"stored_size"`
	TotalSize  int64     `json:"total_size"`
	Error      string    `json:"error"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Status constants for Resource
const (
	StatusQueued     = 0
	StatusProcessing = 1
	StatusCompleted  = 2
	StatusFailed     = 3
)

// Api provides methods to interact with the Vault API
type Api struct {
	url            string
	cl             *http.Client
	resourcesCache *lazymap.LazyMap[*Resource]
}

// New creates a new Vault API client
func NewApi(c *cli.Context, cl *http.Client) *Api {
	host := c.String(apiHostFlag)

	// Return nil if host is not configured
	if host == "" {
		return nil
	}

	port := c.Int(apiPortFlag)
	secure := c.Bool(apiSecureFlag)

	protocol := "http"
	if secure {
		protocol = "https"
	}
	u := fmt.Sprintf("%v://%v:%v", protocol, host, port)

	log.Infof("vault api endpoint %v", u)

	return &Api{
		url: u,
		cl:  cl,
		resourcesCache: lazymap.New[*Resource](&lazymap.Config{
			Expire: time.Minute,
		}),
	}
}

// doRequestRaw performs a raw HTTP request to the Vault API
func (s *Api) doRequestRaw(ctx context.Context, url string, method string, data []byte) (res *http.Response, err error) {
	var payload io.Reader

	if data != nil {
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, payload)

	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}

	res, err = s.cl.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute request")
	}

	return res, nil
}

// doRequest performs an HTTP request and unmarshals the response
func (s *Api) doRequest(ctx context.Context, url string, method string, data []byte, v any) (bool, error) {
	res, err := s.doRequestRaw(ctx, url, method, data)
	if err != nil {
		return false, err
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(res.Body)

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return false, errors.Wrap(err, "failed to read response body")
	}

	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusAccepted {
		if v != nil {
			err = json.Unmarshal(body, v)
			if err != nil {
				return false, errors.Wrap(err, "failed to unmarshal response")
			}
		}
		return true, nil
	} else if res.StatusCode == http.StatusNotFound {
		return false, nil
	} else {
		var e ErrorResponse
		err = json.Unmarshal(body, &e)
		if err != nil {
			return false, errors.Wrapf(err, "failed to parse error response status=%v body=%v url=%v", res.StatusCode, string(body), url)
		}
		return false, errors.Errorf("vault api error: %s", e.Error)
	}
}

// GetResource retrieves a resource by ID from the Vault
func (s *Api) GetResource(ctx context.Context, resourceID string) (*Resource, error) {
	u := fmt.Sprintf("%s/resource/%s", s.url, resourceID)
	resource := &Resource{}
	found, err := s.doRequest(ctx, u, "GET", nil, resource)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get resource")
	}
	if !found {
		return nil, nil
	}
	return resource, nil
}

// GetResourceCached retrieves a resource by ID with caching
func (s *Api) GetResourceCached(ctx context.Context, resourceID string) (*Resource, error) {
	return s.resourcesCache.Get(resourceID, func() (*Resource, error) {
		return s.GetResource(ctx, resourceID)
	})
}

// PutResource queues a resource for storage in the Vault
func (s *Api) PutResource(ctx context.Context, resourceID string) (*Resource, error) {
	u := fmt.Sprintf("%s/resource/%s", s.url, resourceID)
	resource := &Resource{}
	_, err := s.doRequest(ctx, u, "PUT", nil, resource)
	if err != nil {
		return nil, errors.Wrap(err, "failed to put resource")
	}
	return resource, nil
}

// DeleteResource queues a resource for deletion from the Vault
func (s *Api) DeleteResource(ctx context.Context, resourceID string) (*Resource, error) {
	u := fmt.Sprintf("%s/resource/%s", s.url, resourceID)
	resource := &Resource{}
	found, err := s.doRequest(ctx, u, "DELETE", nil, resource)
	if err != nil {
		return nil, errors.Wrap(err, "failed to delete resource")
	}
	if !found {
		return nil, nil
	}
	return resource, nil
}

// GetProgress returns the storage progress as a percentage (0-100)
func (r *Resource) GetProgress() float64 {
	if r.TotalSize == 0 {
		return 0
	}
	return float64(r.StoredSize) / float64(r.TotalSize) * 100
}
