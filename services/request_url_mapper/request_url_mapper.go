package request_url_mapper

import (
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	RequestURLMappingsFlag = "request-url-mappings"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   RequestURLMappingsFlag,
			Usage:  "JSON mapping of external URLs to internal URLs for request optimization",
			EnvVar: "REQUEST_URL_MAPPINGS",
		},
	)
}

// RequestURLMapper handles URL mapping for addon requests
type RequestURLMapper struct {
	mappings map[string]string
}

// NewRequestURLMapper creates a new RequestURLMapper from cli.Context
// The JSON format is: {"https://external.url": "http://internal.url", ...}
func NewRequestURLMapper(c *cli.Context) (*RequestURLMapper, error) {
	mappingsJSON := c.String(RequestURLMappingsFlag)
	if mappingsJSON == "" {
		return &RequestURLMapper{
			mappings: make(map[string]string),
		}, nil
	}

	var mappings map[string]string
	if err := json.Unmarshal([]byte(mappingsJSON), &mappings); err != nil {
		return nil, errors.Wrap(err, "failed to parse REQUEST_URL_MAPPINGS")
	}

	log.WithField("mappings_count", len(mappings)).
		Info("initialized request URL mapper")

	return &RequestURLMapper{
		mappings: mappings,
	}, nil
}

// MapURL replaces the beginning of the URL according to the mappings
// If no mapping matches, returns the original URL
func (m *RequestURLMapper) MapURL(url string) string {
	if m.mappings == nil || len(m.mappings) == 0 {
		return url
	}

	for from, to := range m.mappings {
		if !strings.HasPrefix(url, from) {
			continue
		}
		mappedURL := to + strings.TrimPrefix(url, from)
		log.WithField("original_url", url).
			WithField("mapped_url", mappedURL).
			Debug("mapped request URL")
		return mappedURL
	}

	return url
}
