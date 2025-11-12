package geoip

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/urfave/cli"
	"github.com/webtor-io/lazymap"
)

const (
	geoipApiHostFlag = "geoip-api-host"
	geoipApiPortFlag = "geoip-api-port"
	geoipApiUseFlag  = "use-geoip-api"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   geoipApiHostFlag,
			Usage:  "geoip api host",
			EnvVar: "GEOIP_API_SERVICE_HOST",
		},
		cli.IntFlag{
			Name:   geoipApiPortFlag,
			Usage:  "geoip api port",
			EnvVar: "GEOIP_API_SERVICE_PORT",
			Value:  80,
		},
		cli.BoolFlag{
			Name:   geoipApiUseFlag,
			Usage:  "use geoip api",
			EnvVar: "USE_GEOIP_API",
		},
	)
}

type Data struct {
	Country         string `json:"country"`
	Latitude        string `json:"latitude"`
	Longitude       string `json:"longitude"`
	Continent       string `json:"continent"`
	Timezone        string `json:"timezone"`
	AccuracyRadius  int    `json:"accuracyRadius"`
	Asn             int    `json:"asn"`
	AsnOrganization string `json:"asnOrganization"`
	AsnNetwork      string `json:"asnNetwork"`
}

type Api struct {
	*lazymap.LazyMap[*Data]
	url string
	cl  *http.Client
}

func New(c *cli.Context, cl *http.Client) *Api {
	if !c.Bool(geoipApiUseFlag) {
		return nil
	}
	return &Api{
		LazyMap: lazymap.New[*Data](&lazymap.Config{
			Expire:      10 * time.Minute,
			ErrorExpire: 10 * time.Second,
		}),
		url: fmt.Sprintf("http://%v:%v", c.String(geoipApiHostFlag), c.Int(geoipApiPortFlag)),
		cl:  cl,
	}
}

func (s *Api) get(ip net.IP) (*Data, error) {
	requestURL := fmt.Sprintf("%v/%v", s.url, ip.String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	var data Data
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (s *Api) Get(ip net.IP) (*Data, error) {
	return s.LazyMap.Get(ip.String(), func() (resp *Data, err error) {
		return s.get(ip)
	})
}
