package kinopoisk_unofficial

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	keyFlag    = "kinopoisk-unofficial-api-key"
	hostFlag   = "kinopoisk-unofficial-api-host"
	portFlag   = "kinopoisk-unofficial-api-port"
	secureFlag = "kinopoisk-unofficial-api-secure"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   hostFlag,
			Usage:  "kinopoisk api host",
			EnvVar: "KINOPOISK_UNOFFICIAL_API_HOST",
			Value:  "kinopoiskapiunofficial.tech",
		},
		cli.IntFlag{
			Name:   portFlag,
			Usage:  "kinopoisk unofficial api port",
			EnvVar: "KINOPOISK_UNOFFICIAL_API_PORT",
			Value:  443,
		},
		cli.BoolTFlag{
			Name:   secureFlag,
			Usage:  "kinopoisk unofficial api secure (https)",
			EnvVar: "KINOPOISK_UNOFFICIAL_API_SECURE",
		},
		cli.StringFlag{
			Name:   keyFlag,
			Usage:  "kinopoisk unofficial api key",
			Value:  "",
			EnvVar: "KINOPOISK_UNOFFICIAL_API_KEY",
		},
	)
}

type Api struct {
	url            string
	cl             *http.Client
	prepareRequest func(r *http.Request) (*http.Request, error)
}

type SearchResponse struct {
	Films []struct {
		FilmID    int    `json:"filmId"`
		NameRu    string `json:"nameRu"`
		NameEn    string `json:"nameEn"`
		Year      string `json:"year"`
		Rating    string `json:"rating"`
		PosterURL string `json:"posterUrl"`
	} `json:"films"`
}

type FilmByIDResponse struct {
	KinopoiskID int            `json:"kinopoiskId"`
	Raw         map[string]any `json:"-"`
}

func New(c *cli.Context, cl *http.Client) *Api {
	host := c.String(hostFlag)
	port := c.Int(portFlag)
	secure := c.BoolT(secureFlag)
	key := c.String(keyFlag)
	if key == "" {
		return nil
	}

	protocol := "http"
	if secure {
		protocol = "https"
	}

	u := fmt.Sprintf("%v://%v:%v", protocol, host, port)
	prepareRequest := func(r *http.Request) (*http.Request, error) {
		r.Header.Set("X-API-KEY", key)
		r.Header.Set("Accept", "application/json")
		return r, nil
	}

	log.Infof("kinopoisk unofficial api endpoint %v", u)

	return &Api{
		url:            u,
		cl:             cl,
		prepareRequest: prepareRequest,
	}
}

func (api *Api) SearchByTitleAndYear(ctx context.Context, title string, year *int16) (*SearchResponse, error) {
	title = strings.TrimSpace(title)
	if year != nil {
		title = fmt.Sprintf("%s %d", title, *year)
	}

	u, _ := url.Parse(fmt.Sprintf("%s/api/v2.1/films/search-by-keyword", api.url))
	q := u.Query()
	q.Set("keyword", title)
	q.Set("page", "1")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

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

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var raw SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, errors.Wrap(err, "decode response")
	}

	return &raw, nil
}

func (api *Api) GetByKpID(ctx context.Context, kpID int) (*FilmByIDResponse, error) {
	u := fmt.Sprintf("%s/api/v2.2/films/%d", api.url, kpID)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}

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

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result FilmByIDResponse
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read response")
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.Wrap(err, "decode response")
	}
	if err := json.Unmarshal(data, &result.Raw); err != nil {
		return nil, errors.Wrap(err, "decode raw metadata")
	}

	return &result, nil
}
