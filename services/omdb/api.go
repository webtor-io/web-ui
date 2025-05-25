package omdb

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const (
	omdbApiKeyFlag    = "omdb-api-key"
	omdbApiSecureFlag = "omdb-api-secure"
	omdbApiHostFlag   = "omdb-api-host"
	omdbApiPortFlag   = "omdb-api-port"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   omdbApiHostFlag,
			Usage:  "omdb api host",
			EnvVar: "OMDB_API_HOST",
			Value:  "www.omdbapi.com",
		},
		cli.IntFlag{
			Name:   omdbApiPortFlag,
			Usage:  "omdb api port",
			EnvVar: "OMDB_API_PORT",
			Value:  443,
		},
		cli.BoolTFlag{
			Name:   omdbApiSecureFlag,
			Usage:  "omdb api secure (https)",
			EnvVar: "OMDB_API_SECURE",
		},
		cli.StringFlag{
			Name:   omdbApiKeyFlag,
			Usage:  "omdb api key",
			Value:  "",
			EnvVar: "OMDB_API_KEY",
		},
	)
}

type OmdbResponse struct {
	ImdbID string         `json:"imdbID"`
	Type   OmdbType       `json:"Type"`
	Raw    map[string]any `json:"-"`
}

type OmdbType string

const (
	OmdbTypeMovie   OmdbType = "movie"
	OmdbTypeSeries  OmdbType = "series"
	OmdbTypeEpisode OmdbType = "episode"
)

func (t OmdbType) String() string {
	return string(t)
}

type Api struct {
	url            string
	cl             *http.Client
	prepareRequest func(r *http.Request) (*http.Request, error)
}

func New(c *cli.Context, cl *http.Client) *Api {
	host := c.String(omdbApiHostFlag)
	port := c.Int(omdbApiPortFlag)
	secure := c.BoolT(omdbApiSecureFlag)
	key := c.String(omdbApiKeyFlag)
	if key == "" {
		return nil
	}
	protocol := "http"
	if secure {
		protocol = "https"
	}
	u := fmt.Sprintf("%v://%v:%v", protocol, host, port)
	prepareRequest := func(r *http.Request) (*http.Request, error) {
		q := r.URL.Query()
		q.Set("apikey", key)
		r.URL.RawQuery = q.Encode()
		return r, nil
	}
	log.Infof("omdb api endpoint %v", u)
	return &Api{
		url:            u,
		cl:             cl,
		prepareRequest: prepareRequest,
	}
}

func (api *Api) SearchByTitleAndYear(ctx context.Context, title string, year *int16, omdbType OmdbType) (*OmdbResponse, error) {
	title = strings.TrimSpace(strings.ToLower(title))

	reqURL := fmt.Sprintf("%s/", api.url)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}

	q := req.URL.Query()
	q.Set("t", title)
	q.Set("plot", "full")
	q.Set("type", omdbType.String())
	if year != nil {
		y := *year
		q.Set("y", strconv.Itoa(int(y)))
	}
	req.URL.RawQuery = q.Encode()

	req, err = api.prepareRequest(req)
	if err != nil {
		return nil, errors.Wrap(err, "prepare request")
	}

	resp, err := api.cl.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "request failed")
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, errors.Wrap(err, "decode response")
	}

	if r, ok := raw["Response"].(string); !ok || r != "True" {
		if strings.Contains(fmt.Sprintf("%s", raw["Error"]), "not found") {
			return nil, nil
		}
		return nil, errors.Errorf("omdb error: %v", raw["Error"])
	}

	imdbID, _ := raw["imdbID"].(string)
	tpe, _ := raw["Type"].(string)

	return &OmdbResponse{
		ImdbID: imdbID,
		Type:   OmdbType(tpe),
		Raw:    raw,
	}, nil
}
