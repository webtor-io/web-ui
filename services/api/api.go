package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/services/common"

	"github.com/pkg/errors"

	"github.com/urfave/cli"

	ra "github.com/webtor-io/rest-api/services"

	"github.com/dgrijalva/jwt-go"

	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
)

const (
	apiKeyFlag                      = "webtor-key"
	apiSecretFlag                   = "webtor-secret"
	apiSecureFlag                   = "webtor-rest-api-secure"
	apiHostFlag                     = "webtor-rest-api-host"
	apiPortFlag                     = "webtor-rest-api-port"
	apiExpireFlag                   = "webtor-rest-api-expire"
	rapidApiKeyFlag                 = "rapidapi-key"
	rapidApiHostFlag                = "rapidapi-host"
	useInternalTorrentHTTPProxyFlag = "use-internal-torrent-http-proxy"
	torrentHTTPProxyHostFlag        = "torrent-http-proxy-host"
	torrentHTTPProxyPortFlag        = "torrent-http-proxy-port"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   apiHostFlag,
			Usage:  "webtor rest-api host",
			EnvVar: "REST_API_SERVICE_HOST",
		},
		cli.IntFlag{
			Name:   apiPortFlag,
			Usage:  "webtor rest-api port",
			EnvVar: "REST_API_SERVICE_PORT",
			Value:  80,
		},
		cli.BoolFlag{
			Name:   apiSecureFlag,
			Usage:  "webtor rest-api secure (https)",
			EnvVar: "REST_API_SECURE",
		},
		cli.IntFlag{
			Name:   apiExpireFlag,
			Usage:  "webtor rest-api expire in days",
			EnvVar: "REST_API_EXPIRE",
			Value:  1,
		},
		cli.StringFlag{
			Name:   apiKeyFlag,
			Usage:  "webtor api key",
			Value:  "",
			EnvVar: "WEBTOR_API_KEY",
		},
		cli.StringFlag{
			Name:   apiSecretFlag,
			Usage:  "webtor api secret",
			Value:  "",
			EnvVar: "WEBTOR_API_SECRET",
		},
		cli.StringFlag{
			Name:   rapidApiHostFlag,
			Usage:  "RapidAPI host",
			Value:  "",
			EnvVar: "RAPIDAPI_HOST",
		},
		cli.StringFlag{
			Name:   rapidApiKeyFlag,
			Usage:  "RapidAPI key",
			Value:  "",
			EnvVar: "RAPIDAPI_KEY",
		},
		cli.BoolFlag{
			Name:   useInternalTorrentHTTPProxyFlag,
			Usage:  "use internal torrent http proxy",
			EnvVar: "USE_INTERNAL_TORRENT_HTTP_PROXY",
		},
		cli.StringFlag{
			Name:   torrentHTTPProxyHostFlag,
			Usage:  "torrent http proxy host",
			EnvVar: "TORRENT_HTTP_PROXY_SERVICE_HOST",
		},
		cli.IntFlag{
			Name:   torrentHTTPProxyPortFlag,
			Usage:  "torrent http proxy port",
			EnvVar: "TORRENT_HTTP_PROXY_SERVICE_PORT",
			Value:  80,
		},
	)
}

type EventData struct {
	Total     int64 `json:"total"`
	Completed int   `json:"completed"`
	Peers     int   `json:"peers"`
	Status    int   `json:"status"`
	Pieces    []struct {
		Position int  `json:"position"`
		Complete bool `json:"complete"`
		Priority int  `json:"priority"`
	} `json:"pieces"`
	Seeders  int `json:"seeders"`
	Leechers int `json:"leechers"`
}

type ExtSubtitle struct {
	Srclang string `json:"srclang"`
	Label   string `json:"label"`
	Src     string `json:"src"`
	Format  string `json:"format"`
	Id      string `json:"id"`
	Hash    string `json:"hash"`
}

type MediaProbe struct {
	Format struct {
		FormatName string `json:"format_name"`
		BitRate    string `json:"bit_rate"`
		Duration   string `json:"duration"`
		Tags       struct {
			CompatibleBrands string    `json:"compatible_brands"`
			Copyright        string    `json:"copyright"`
			CreationTime     time.Time `json:"creation_time"`
			Description      string    `json:"description"`
			Encoder          string    `json:"encoder"`
			MajorBrand       string    `json:"major_brand"`
			MinorVersion     string    `json:"minor_version"`
			Title            string    `json:"title"`
		} `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecName string `json:"codec_name"`
		CodecType string `json:"codec_type"`
		Width     int    `json:"width,omitempty"`
		Height    int    `json:"height,omitempty"`
		BitRate   string `json:"bit_rate"`
		Duration  string `json:"duration"`
		Tags      struct {
			CreationTime time.Time `json:"creation_time"`
			HandlerName  string    `json:"handler_name"`
			Language     string    `json:"language"`
			VendorId     string    `json:"vendor_id"`
			Title        string    `json:"title"`
		} `json:"tags"`
		Index         int    `json:"index,omitempty"`
		Channels      int    `json:"channels,omitempty"`
		ChannelLayout string `json:"channel_layout,omitempty"`
		SampleRate    string `json:"sample_rate,omitempty"`
	} `json:"streams"`
}

type Claims struct {
	jwt.StandardClaims
	Rate          string `json:"rate,omitempty"`
	Role          string `json:"role,omitempty"`
	SessionID     string `json:"sessionID"`
	Domain        string `json:"domain"`
	Agent         string `json:"agent"`
	RemoteAddress string `json:"remoteAddress"`
}

type Api struct {
	url                         string
	prepareRequest              func(r *http.Request, c *Claims) (*http.Request, error)
	cl                          *http.Client
	domain                      string
	expire                      int
	torrentCache                *lazymap.LazyMap[[]byte]
	listResponseCache           *lazymap.LazyMap[*ra.ListResponse]
	resourcesCache              *lazymap.LazyMap[*ra.ResourceResponse]
	useInternalTorrentHTTPProxy bool
	torrentHTTPProxyHost        string
	torrentHTTPProxyPort        int
}

type ListResourceContentOutputType string

const (
	OutputList ListResourceContentOutputType = "list"
	OutputTree ListResourceContentOutputType = "tree"
)

type ListResourceContentArgs struct {
	Limit  uint
	Offset uint
	Path   string
	Output ListResourceContentOutputType
}

func (s *ListResourceContentArgs) ToQuery() url.Values {
	q := url.Values{}
	limit := uint(10)
	offset := s.Offset
	path := "/"
	output := OutputList
	if s.Limit > 0 {
		limit = s.Limit
	}
	if s.Path != "" {
		path = s.Path
	}
	if s.Output != "" {
		output = s.Output
	}
	q.Set("limit", strconv.Itoa(int(limit)))
	q.Set("offset", strconv.Itoa(int(offset)))
	q.Set("path", path)
	q.Set("output", string(output))
	return q
}

func New(c *cli.Context, cl *http.Client) *Api {
	host := c.String(apiHostFlag)
	port := c.Int(apiPortFlag)
	secure := c.Bool(apiSecureFlag)
	secret := c.String(apiSecretFlag)
	expire := c.Int(apiExpireFlag)
	key := c.String(apiKeyFlag)
	rapidApiHost := c.String(rapidApiHostFlag)
	rapidApiKey := c.String(rapidApiKeyFlag)
	if rapidApiHost != "" {
		host = rapidApiHost
		port = 443
		secure = true
	}
	protocol := "http"
	if secure {
		protocol = "https"
	}
	u := fmt.Sprintf("%v://%v:%v", protocol, host, port)
	prepareRequest := func(r *http.Request, cl *Claims) (*http.Request, error) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, cl)
		tokenString, err := token.SignedString([]byte(secret))
		if err != nil {
			return nil, err
		}
		r.Header.Set("X-Token", tokenString)
		r.Header.Set("X-Api-Key", key)
		return r, nil
	}
	if rapidApiHost != "" && rapidApiKey != "" {
		log.Info("using RapidAPI")
		prepareRequest = func(r *http.Request, cl *Claims) (*http.Request, error) {
			r.Header.Set("X-RapidAPI-Host", rapidApiHost)
			r.Header.Set("X-RapidAPI-Key", rapidApiKey)
			return r, nil
		}
	}
	log.Infof("api endpoint %v", u)
	apiURL, _ := url.Parse(c.String(common.DomainFlag))
	return &Api{
		url:            u,
		cl:             cl,
		prepareRequest: prepareRequest,
		domain:         apiURL.Hostname(),
		expire:         expire,
		torrentCache: lazymap.New[[]byte](&lazymap.Config{
			Expire: time.Minute,
		}),
		resourcesCache: lazymap.New[*ra.ResourceResponse](&lazymap.Config{
			Expire: time.Minute,
		}),
		listResponseCache: lazymap.New[*ra.ListResponse](&lazymap.Config{
			Expire: time.Minute,
		}),
		useInternalTorrentHTTPProxy: c.Bool(useInternalTorrentHTTPProxyFlag),
		torrentHTTPProxyHost:        c.String(torrentHTTPProxyHostFlag),
		torrentHTTPProxyPort:        c.Int(torrentHTTPProxyPortFlag),
	}
}

func (s *Api) GetResource(ctx context.Context, c *Claims, infohash string) (e *ra.ResourceResponse, err error) {
	u := s.url + "/resource/" + infohash
	e = &ra.ResourceResponse{}
	err = s.doRequest(ctx, c, u, "GET", nil, e)
	if e.ID == "" {
		e = nil
	}
	return
}

func (s *Api) GetResourceCached(ctx context.Context, c *Claims, infohash string) (e *ra.ResourceResponse, err error) {
	return s.resourcesCache.Get(infohash, func() (*ra.ResourceResponse, error) {
		return s.GetResource(ctx, c, infohash)
	})
}

func (s *Api) GetTorrent(ctx context.Context, c *Claims, infohash string) (closer io.ReadCloser, err error) {
	u := s.url + "/resource/" + infohash + ".torrent"
	res, err := s.doRequestRaw(ctx, c, u, "GET", nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

func (s *Api) GetTorrentCached(ctx context.Context, c *Claims, infohash string) ([]byte, error) {
	return s.torrentCache.Get(infohash, func() ([]byte, error) {
		resp, err := s.GetTorrent(ctx, c, infohash)
		if err != nil {
			return nil, err
		}
		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp)
		data, err := io.ReadAll(resp)
		if err != nil {
			return nil, err
		}
		return data, nil
	})

}

func (s *Api) StoreResource(ctx context.Context, c *Claims, resource []byte) (e *ra.ResourceResponse, err error) {
	u := s.url + "/resource"
	e = &ra.ResourceResponse{}
	err = s.doRequest(ctx, c, u, "POST", resource, e)
	if e.ID == "" {
		e = nil
	}
	return
}

func (s *Api) ListResourceContent(ctx context.Context, c *Claims, infohash string, args *ListResourceContentArgs) (e *ra.ListResponse, err error) {
	u := s.url + "/resource/" + infohash + "/list?" + args.ToQuery().Encode()
	e = &ra.ListResponse{}
	err = s.doRequest(ctx, c, u, "GET", nil, e)
	return
}

func (s *Api) ListResourceContentCached(ctx context.Context, c *Claims, infohash string, args *ListResourceContentArgs) (*ra.ListResponse, error) {
	key := infohash + fmt.Sprintf("%+v", args)
	return s.listResponseCache.Get(key, func() (*ra.ListResponse, error) {
		return s.ListResourceContent(ctx, c, infohash, args)
	})
}

func (s *Api) doRequestRaw(ctx context.Context, c *Claims, url string, method string, data []byte) (res *http.Response, err error) {
	var payload io.Reader

	if data != nil {
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, payload)

	if err != nil {
		return
	}

	req, err = s.prepareRequest(req, c)

	if err != nil {
		return
	}

	res, err = s.cl.Do(req)
	if err != nil {
		return
	}

	return
}

func (s *Api) doRequest(ctx context.Context, c *Claims, url string, method string, data []byte, v any) error {
	res, err := s.doRequestRaw(ctx, c, url, method, data)
	if err != nil {
		return err
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(res.Body)

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode == http.StatusOK {
		err = json.Unmarshal(body, v)
		if err != nil {
			return err
		}
		return nil
	} else if res.StatusCode == http.StatusNotFound {
		return nil
	} else if res.StatusCode == http.StatusForbidden {
		return errors.Errorf("access is forbidden url=%v", url)
	} else {
		var e ra.ErrorResponse
		err = json.Unmarshal(body, &e)
		if err != nil {
			return errors.Wrapf(err, "failed to parse status=%v body=%v url=%v", res.StatusCode, body, url)
		}
		return errors.New(e.Error)
	}
}

func (s *Api) ExportResourceContent(ctx context.Context, c *Claims, infohash string, itemID string, imdbID string) (e *ra.ExportResponse, err error) {
	u := s.url + "/resource/" + infohash + "/export/" + itemID
	if imdbID != "" {
		u += "?imdb-id=" + imdbID
	}
	e = &ra.ExportResponse{}
	err = s.doRequest(ctx, c, u, "GET", nil, e)
	// if e.Source.ID == nil
	// 	e = nil
	// }
	return
}

func (s *Api) Download(ctx context.Context, u string) (io.ReadCloser, error) {
	return s.DownloadWithRange(ctx, u, 0, -1)
}

func (s *Api) makeTorrentHTTPProxyRequest(ctx context.Context, u string) (*http.Request, error) {
	if s.useInternalTorrentHTTPProxy {
		internal := fmt.Sprintf("%v:%v", s.torrentHTTPProxyHost, s.torrentHTTPProxyPort)
		ur, err := url.Parse(u)
		if err != nil {
			return nil, err
		}
		ur.Host = internal
		ur.Scheme = "http"
		u = ur.String()
	}
	return http.NewRequestWithContext(ctx, "GET", u, nil)
}

func (s *Api) DownloadWithRange(ctx context.Context, u string, start int, end int) (io.ReadCloser, error) {
	req, err := s.makeTorrentHTTPProxyRequest(ctx, u)
	if err != nil {
		log.WithError(err).Error("failed to make new request")
		return nil, err
	}
	if start != 0 || end != -1 {
		startStr := strconv.Itoa(start)
		endStr := ""
		if end != -1 {
			endStr = strconv.Itoa(end)
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%v-%v", startStr, endStr))
	}
	res, err := s.cl.Do(req)
	if err != nil {
		log.WithError(err).Error("failed to do request")
		return nil, err
	}
	b := res.Body
	return b, nil
}

type OpenSubtitleTrack struct {
	ID string
	*ra.ExportTrack
}

func (s *Api) GetOpenSubtitles(ctx context.Context, u string) ([]OpenSubtitleTrack, error) {
	req, err := s.makeTorrentHTTPProxyRequest(ctx, u)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make new request")
	}
	res, err := s.cl.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to do request")
	}
	b := res.Body
	defer func(b io.ReadCloser) {
		_ = b.Close()
	}(b)
	var esubs []ExtSubtitle
	var subs []OpenSubtitleTrack
	data, err := io.ReadAll(b)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read data")
	}
	err = json.Unmarshal(data, &esubs)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal data=%v", string(data))
	}
	for _, esub := range esubs {
		subs = append(subs, OpenSubtitleTrack{
			ExportTrack: &ra.ExportTrack{
				Src:     s.makeSubtitleURL(u, esub),
				Kind:    "subtitles",
				SrcLang: esub.Srclang,
				Label:   esub.Label,
			},
			ID: esub.Id,
		})
	}
	return subs, nil
}

func (s *Api) GetMediaProbe(ctx context.Context, u string) (*MediaProbe, error) {
	req, err := s.makeTorrentHTTPProxyRequest(ctx, u)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make new request")
	}
	res, err := s.cl.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to do request")
	}
	b := res.Body
	defer func(b io.ReadCloser) {
		_ = b.Close()
	}(b)
	mb := MediaProbe{}
	data, err := io.ReadAll(b)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read data")
	}
	err = json.Unmarshal(data, &mb)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal data=%v", string(data))
	}
	return &mb, nil
}

func (s *Api) Stats(ctx context.Context, u string) (chan EventData, error) {
	req, err := s.makeTorrentHTTPProxyRequest(ctx, u)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make new request")
	}
	res, err := s.cl.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to do request")
	}
	ch := make(chan EventData)
	go func() {
		b := res.Body
		defer func() {
			close(ch)
			_ = b.Close()
		}()
		scanner := bufio.NewScanner(b)
		scanner.Split(bufio.ScanLines)

		t := ""
		for scanner.Scan() {
			if ctx.Err() != nil {
				log.WithError(ctx.Err()).Error("context error")
				break
			}
			if scanner.Err() != nil {
				log.WithError(scanner.Err()).Error("scanner error")
				break
			}
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				t = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
				continue
			}
			if t == "statupdate" && strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				var event EventData
				err := json.Unmarshal([]byte(data), &event)
				if err != nil {
					log.WithError(err).Errorf("failed to unmarshal data=%v line=%v", data, line)
					continue
				}
				select {
				case ch <- event:
					continue
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}

func (s *Api) makeSubtitleURL(u string, esub ExtSubtitle) string {
	src, _ := url.Parse(u)
	path := ""
	pathParts := strings.Split(src.Path, "/")
	pathParts = pathParts[:len(pathParts)-1]
	path = strings.Join(pathParts, "/") + esub.Src
	src.Path = path
	res := src.String()
	if esub.Format == "srt" {
		res = s.convertToVTT(res)
	}
	return res
}

func (*Api) convertToVTT(u string) string {
	src, _ := url.Parse(u)
	nameParts := strings.Split(src.Path, "/")
	name := strings.Join(nameParts[len(nameParts)-1:], "/")
	src.Path += "~vtt/" + strings.TrimSuffix(name, ".srt") + ".vtt"
	return src.String()
}

func (s *Api) AttachExternalSubtitle(ei ra.ExportItem, u string) string {
	res := s.AttachExternalFile(ei, u)
	format := "vtt"

	src, _ := url.Parse(u)
	if strings.HasSuffix(src.Path, ".srt") {
		format = "srt"
	}
	if format == "srt" {
		res = s.convertToVTT(res)
	}
	return res
}

func (s *Api) AttachExternalFile(ei ra.ExportItem, u string) string {
	src, _ := url.Parse(ei.URL)
	nameParts := strings.Split(u, "/")
	name := strings.Join(nameParts[len(nameParts)-1:], "/")
	encodedURL := url.QueryEscape(base64.StdEncoding.EncodeToString([]byte(u)))
	src.Path = fmt.Sprintf("/ext/%s/%s", encodedURL, name)
	return src.String()
}

func getRemoteAddress(c *gin.Context) string {
	if addr := c.Request.Header.Get(gin.PlatformCloudflare); addr != "" {
		return addr
	}
	return c.ClientIP()
}

type ClaimsContext struct{}

func (s *Api) MakeClaimsFromContext(c *gin.Context, domain string, uc *claims.Data, sessionID string) (*Claims, error) {
	cl := &Claims{
		SessionID:     sessionID,
		Domain:        domain,
		RemoteAddress: getRemoteAddress(c),
		Agent:         c.Request.Header.Get("User-Agent"),
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: time.Now().Add(time.Duration(s.expire) * 24 * time.Hour).Unix(),
		},
	}
	if uc != nil {
		cl.Role = uc.Context.Tier.Name
		rate := uc.Claims.Connection.Rate
		if rate > 0 {
			cl.Rate = fmt.Sprintf("%dM", rate)
		}
	}

	return cl, nil
}

func GetClaimsFromContext(c *gin.Context) *Claims {
	return c.Request.Context().Value(ClaimsContext{}).(*Claims)
}

func GenerateSessionID(c *gin.Context) string {
	sess, _ := c.Cookie("session")
	u := auth.GetUserFromContext(c)
	if u.Email != "" {
		sess = GenerateSessionIDFromUser(u)
	}
	return sess
}

func GenerateSessionIDFromUser(u *auth.User) string {
	h := sha1.New()
	h.Write([]byte(u.ID.String()))
	hash := hex.EncodeToString(h.Sum(nil))
	return hash
}

func (s *Api) RegisterHandler(r *gin.Engine) {
	r.Use(func(c *gin.Context) {
		uc := claims.GetFromContext(c)
		c, err := s.SetClaims(c, s.domain, uc, GenerateSessionID(c))
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		c.Next()
	})
}

func (s *Api) SetClaims(c *gin.Context, domain string, uc *claims.Data, sessionID string) (*gin.Context, error) {
	ac, err := s.MakeClaimsFromContext(c, domain, uc, sessionID)
	if err != nil {
		return nil, err
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ClaimsContext{}, ac))
	return c, nil
}
