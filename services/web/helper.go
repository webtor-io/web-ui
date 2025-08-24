package web

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"math/rand/v2"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/webtor-io/web-ui/handlers/static"
	"github.com/webtor-io/web-ui/services/abuse_store"
	"github.com/webtor-io/web-ui/services/common"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"

	h "github.com/dustin/go-humanize"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/obfuscator"
)

func (s *Helper) MakeJobLogURL(j *job.Job) string {
	return fmt.Sprintf("/queue/%v/job/%v/log", j.Queue, j.ID)
}

func (s *Helper) Log(err error) error {
	log.Error(err)
	return err
}

func (s *Helper) ShortErr(err error) string {
	return strings.Split(err.Error(), ":")[0]
}

func (s *Helper) BitsForHumans(b int64) string {
	return h.Bytes(uint64(b))
}

func (s *Helper) Dev() bool {
	return gin.Mode() == "debug"
}

func (s *Helper) Has(obj any, fieldName string) bool {
	value := reflect.Indirect(reflect.ValueOf(obj))
	field := value.FieldByName(fieldName)
	return field.IsValid() && !field.IsNil()
}

type Helper struct {
	assetsHost    string
	assetsPath    string
	useAuth       bool
	domain        string
	demoMagnet    string
	demoTorrent   string
	ah            *AssetHashes
	useAbuseStore bool
}

func NewHelper(c *cli.Context) *Helper {
	return &Helper{
		demoMagnet:    c.String(common.DemoMagnetFlag),
		demoTorrent:   c.String(common.DemoTorrentFlag),
		assetsHost:    c.String(static.AssetsHostFlag),
		assetsPath:    c.String(static.AssetsPathFlag),
		useAuth:       c.Bool(auth.UseFlag),
		useAbuseStore: c.Bool(abuse_store.UseFlag),
		domain:        c.String(common.DomainFlag),
		ah:            NewAssetHashes(c.String(static.AssetsPathFlag)),
	}
}

func (s *Helper) TimeBetween(from string, to string) bool {
	ft, err := time.Parse(time.DateTime, from)
	if err != nil {
		panic(err)
	}
	tt, err := time.Parse(time.DateTime, to)
	if err != nil {
		panic(err)
	}
	now := time.Now()
	return now.After(ft) && now.Before(tt)
}

func (s *Helper) CheckProb(probability float64) bool {
	// Ensure the probability is within bounds [0.0, 1.0]
	if probability < 0.0 || probability > 1.0 {
		panic("Probability must be between 0 and 1")
	}
	// Generate a random float between 0 and 1
	return rand.Float64() < probability
}

func (s *Helper) HasAds(c *claims.Data) bool {
	if c == nil {
		return false
	}
	return !c.Claims.Site.NoAds
}

func (s *Helper) IsPaid(c *claims.Data) bool {
	if c == nil {
		return true
	}
	return c.Context.Tier.Id != 0
}

func (s *Helper) TierName(c *claims.Data) string {
	if c == nil {
		return "free"
	}
	return c.Context.Tier.Name
}

func (s *Helper) UseAuth() bool {
	return s.useAuth
}

func (s *Helper) HasAuth(u *auth.User) bool {
	return u.HasAuth()
}

func (s *Helper) UseAbuseStore() bool {
	return s.useAbuseStore
}

func (s *Helper) Domain() string {
	return s.domain
}

func (s *Helper) DemoMagnet() template.URL {
	return template.URL(s.demoMagnet)
}

func (s *Helper) DemoTorrent() template.URL {
	return template.URL(s.demoTorrent)
}

func (s *Helper) IsDemoMagnet(m string) bool {
	return strings.HasPrefix(m, s.demoMagnet)
}

func (s *Helper) Obfuscate(in string) string {
	return obfuscator.Obfuscate(in)
}

func (s *Helper) Base64(in []byte) string {
	return base64.StdEncoding.EncodeToString(in)
}

func (s *Helper) Json(in any) template.JS {
	out, _ := json.Marshal(in)
	return template.JS(out)
}

func (s *Helper) Asset(in string) template.HTML {
	t := ""
	if strings.HasSuffix(in, ".js") {
		t = "<script type=\"text/javascript\" async src=\"%v\"></script>"
	} else if strings.HasSuffix(in, ".css") && s.Dev() {
		in = strings.TrimSuffix(in, ".css") + ".js"
		t = "<script type=\"text/javascript\" src=\"%v\"></script>"
	} else if strings.HasSuffix(in, ".css") {
		t = "<link href=\"%v\" rel=\"stylesheet\"/>"
	}
	path := s.assetsHost + "/assets/" + in
	if !s.Dev() {
		hash, _ := s.ah.Get(in)
		path += "?" + hash
	}
	return template.HTML(fmt.Sprintf(t, path))
}

func (s *Helper) DevAsset(in string) template.HTML {
	if s.Dev() {
		return s.Asset("dev/" + in)
	}
	return ""
}

func (s *Helper) Pwd(in string) string {
	parts := strings.Split(in, "/")
	pwd := strings.Join(parts[:len(parts)-1], "/")
	if pwd == "" {
		pwd = "/"
	}
	return pwd
}

type AssetHashes struct {
	lazymap.LazyMap[string]
	path string
}

func (s *AssetHashes) get(name string) (hash string, err error) {
	f, err := os.Open(s.path + "/" + name)
	if err != nil {
		return "", err
	}
	md5Hash := md5.New()
	if _, err := io.Copy(md5Hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(md5Hash.Sum(nil)), nil
}

func (s *AssetHashes) Get(name string) (string, error) {
	return s.LazyMap.Get(name, func() (string, error) {
		f, err := os.Open(s.path + "/" + name)
		if err != nil {
			return "", err
		}
		md5Hash := md5.New()
		if _, err := io.Copy(md5Hash, f); err != nil {
			return "", err
		}
		return hex.EncodeToString(md5Hash.Sum(nil)), nil
	})
}

func NewAssetHashes(path string) *AssetHashes {
	return &AssetHashes{
		LazyMap: lazymap.New[string](&lazymap.Config{}),
		path:    path,
	}
}

func (s *Helper) Now() time.Time {
	return time.Now()
}

func (s *Helper) Float1(f float64) string {
	return fmt.Sprintf("%.1f", f)
}

func (s *Helper) ProfileName(u *auth.User) string {
	if u == nil ||
		strings.HasSuffix(u.Email, "@privaterelay.appleid.com") ||
		u.Email == "" {
		return "profile"
	}
	return u.Email

}
