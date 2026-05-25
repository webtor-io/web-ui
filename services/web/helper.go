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
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	hc "github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/handlers/static"
	"github.com/webtor-io/web-ui/services/abuse_store"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/i18n"

	"github.com/gin-gonic/gin"
	"github.com/hako/durafmt"
	"github.com/urfave/cli"

	h "github.com/dustin/go-humanize"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/lazymap"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/obfuscator"
)

func (s *Helper) MakeJobLogURL(lang string, j *job.Job) string {
	return LangURL(lang, fmt.Sprintf("/queue/%v/job/%v/log", j.Queue, j.ID))
}

func (s *Helper) Log(err error) error {
	log.Error(err)
	return err
}

func ShortErr(err error) string {
	return strings.Split(err.Error(), ":")[0]
}
func LongErr(err error) template.HTML {
	parts := strings.Split(err.Error(), ": ")
	for i, p := range parts {
		parts[i] = template.HTMLEscapeString(p)
	}
	parts[0] = "<strong>" + parts[0] + "</strong>"
	return template.HTML(strings.Join(parts, "<br />"))
}

func wantsJSON(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Accept"), "application/json")
}

func RedirectWithErrorAndPath(c *gin.Context, path string, serr error) {
	errKey := ClassifyError(serr)
	log.WithError(serr).WithField("error_key", errKey).WithField("path", path).Warn("redirect with error")

	if wantsJSON(c) {
		c.JSON(http.StatusOK, gin.H{"status": "error", "message": errKey})
		return
	}
	u, err := url.Parse(path)
	if err != nil || u == nil {
		c.Redirect(http.StatusFound, path)
		return
	}
	q := u.Query()
	q.Set("status", "error")
	q.Set("err", errKey)
	q.Set("from", c.Request.URL.Path)
	u.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, u.String())
}

func RedirectWithError(c *gin.Context, serr error) {
	RedirectWithErrorAndPath(c, c.GetHeader("X-Return-Url"), serr)
}

func RedirectWithSuccess(c *gin.Context) {
	RedirectWithSuccessAndMessage(c, "")
}

func RedirectWithSuccessAndMessage(c *gin.Context, message string) {
	message = i18n.T(c, message)
	if wantsJSON(c) {
		resp := gin.H{"status": "success"}
		if message != "" {
			resp["message"] = message
		}
		c.JSON(http.StatusOK, resp)
		return
	}
	u, err := url.Parse(c.GetHeader("X-Return-Url"))
	if err != nil || u == nil {
		// if return url is invalid, attempt a plain redirect without query mutation
		c.Redirect(http.StatusFound, c.GetHeader("X-Return-Url"))
		return
	}
	q := u.Query()
	q.Set("status", "success")
	q.Set("from", c.Request.URL.Path)
	if message != "" {
		q.Set("message", message)
	}
	u.RawQuery = q.Encode()
	c.Redirect(http.StatusFound, u.String())
}

// WantsJSON reports whether the request prefers a JSON response.
// Exported so handlers can check before calling custom redirect logic.
func WantsJSON(c *gin.Context) bool {
	return wantsJSON(c)
}

func (s *Helper) ShortErr(err error) string {
	return ShortErr(err)
}

func (s *Helper) LongErr(err error) template.HTML {
	return LongErr(err)
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

func (s *Helper) SeoFriendly(text string) string {
	return strings.ReplaceAll(text, "→", "to")
}

type Helper struct {
	assetsHost     string
	assetsPath     string
	useAuth        bool
	domain         string
	demoMagnet     string
	demoTorrent    string
	ah             *AssetHashes
	useAbuseStore  bool
	useSuperTokens bool
}

func NewHelper(c *cli.Context) *Helper {
	return &Helper{
		demoMagnet:     c.String(common.DemoMagnetFlag),
		demoTorrent:    c.String(common.DemoTorrentFlag),
		assetsHost:     c.String(static.AssetsHostFlag),
		assetsPath:     c.String(static.AssetsPathFlag),
		useAbuseStore:  c.Bool(abuse_store.UseFlag),
		domain:         c.String(common.DomainFlag),
		ah:             NewAssetHashes(c.String(static.AssetsPathFlag)),
		useSuperTokens: c.String(auth.SupertokensHostFlag) != "",
	}
}

func (s *Helper) TimeBetween(from string, to string) bool {
	ft, err := time.Parse(time.DateTime, from)
	if err != nil {
		log.WithError(err).WithField("from", from).Error("failed to parse 'from' time in TimeBetween")
		return false
	}
	tt, err := time.Parse(time.DateTime, to)
	if err != nil {
		log.WithError(err).WithField("to", to).Error("failed to parse 'to' time in TimeBetween")
		return false
	}
	now := time.Now()
	return now.After(ft) && now.Before(tt)
}

func (s *Helper) CheckProb(probability float64) bool {
	// Ensure the probability is within bounds [0.0, 1.0]
	if probability < 0.0 || probability > 1.0 {
		log.WithField("probability", probability).Error("probability must be between 0 and 1 in CheckProb")
		return false
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

func (s *Helper) CanManageEmbedDomains(c *claims.Data) bool {
	if c == nil {
		return false
	}
	return c.Claims != nil && c.Claims.Embed.NoAds
}

func (s *Helper) TierName(c *claims.Data) string {
	if c == nil {
		return "free"
	}
	return c.Context.Tier.Name
}

func (s *Helper) UseSuperTokens() bool {
	return s.useSuperTokens
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

func (s *Helper) DomainWithoutProtocol() string {
	return strings.TrimPrefix(strings.TrimPrefix(s.domain, "https://"), "http://")
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
	*lazymap.LazyMap[string]
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

// KebabToSnake converts kebab-case strings to snake_case
func (s *Helper) KebabToSnake(str string) string {
	return strings.ReplaceAll(str, "-", "_")
}

// Printf formats a string using fmt.Sprintf
func (s *Helper) Printf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

// Tools returns the list of all available tool pages
func (s *Helper) Tools() []hc.Tool {
	return hc.Tools
}

// Duration formats a time.Duration into a human-readable string
func (s *Helper) Duration(d time.Duration) string {
	return durafmt.Parse(d).LimitFirstN(2).String()
}

// TimeAgo formats a time.Time into a relative string like "3 hours ago"
func (s *Helper) TimeAgo(t time.Time) string {
	return h.Time(t)
}

// TimeAgoLang formats a time.Time into a localized relative string.
// Template usage: {{ timeAgoLang $.Lang .CreatedAt }}
func (s *Helper) TimeAgoLang(lang string, t time.Time) string {
	d := time.Since(t)
	loc := i18n.GlobalService().Localizer(lang)
	tr := func(key string) string { return i18n.TranslateWithLocalizer(loc, key) }

	var key string
	var n int
	switch {
	case d < time.Minute:
		return tr("time.now")
	case d < 2*time.Minute:
		return tr("time.minute")
	case d < time.Hour:
		key, n = "time.minutes", int(d.Minutes())
	case d < 2*time.Hour:
		return tr("time.hour")
	case d < 24*time.Hour:
		key, n = "time.hours", int(d.Hours())
	case d < 48*time.Hour:
		return tr("time.day")
	case d < 7*24*time.Hour:
		key, n = "time.days", int(d.Hours()/24)
	case d < 14*24*time.Hour:
		return tr("time.week")
	case d < 30*24*time.Hour:
		key, n = "time.weeks", int(d.Hours()/(24*7))
	case d < 60*24*time.Hour:
		return tr("time.month")
	case d < 365*24*time.Hour:
		key, n = "time.months", int(d.Hours()/(24*30))
	case d < 2*365*24*time.Hour:
		return tr("time.year")
	default:
		key, n = "time.years", int(d.Hours()/(24*365))
	}
	return fmt.Sprintf(tr(key), n)
}

// Deref dereferences a pointer to float64, returning 0 if the pointer is nil
func (s *Helper) DerefFloat64(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

// DerefInt16 dereferences a pointer to int16, returning 0 if nil.
// Returns int for easy use in template comparisons.
func (s *Helper) DerefInt16(p *int16) int {
	if p == nil {
		return 0
	}
	return int(*p)
}

// Deref dereferences a pointer to string, returning "" if nil. Lets
// templates use `{{ if deref .Name }}` for nullable string columns.
func (s *Helper) Deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Seq returns a slice of consecutive integers [from..to] inclusive.
// Used in templates for star-rating loops: {{ range seq 1 10 }}.
func (s *Helper) Seq(from, to int) []int {
	if to < from {
		return nil
	}
	result := make([]int, 0, to-from+1)
	for i := from; i <= to; i++ {
		result = append(result, i)
	}
	return result
}

// --- i18n URL helpers ---

// LangInfo holds display info for the language switcher.
type LangInfo struct {
	Code    string
	Name    string
	URL     string
	Current bool
}

// HrefLang holds data for <link rel="alternate" hreflang="..."> tags.
type HrefLang struct {
	Lang string
	URL  string
}

// CtxData wraps arbitrary template data with the full web.Context.
// Used to pass context to sub-templates that receive non-Context data
// (e.g. ButtonItem, VaultButton) without polluting their structs.
//
// Template usage: {{ template "button" withContext $ (makeFileDownload $ .) }}
// In sub-template: {{ t .Ctx.Lang .Data.Name }}, {{ .Ctx.User | hasAuth }}, etc.
type CtxData struct {
	Ctx  *Context
	Data any
}

// WithContext wraps data with the full web.Context for sub-template rendering.
func (s *Helper) WithContext(ctx *Context, data any) *CtxData {
	return &CtxData{Ctx: ctx, Data: data}
}

// BlogLang maps the current site language to a blog language code.
// The Hugo blog only supports en and ru; everything else falls back to en.
// Template usage: {{ blogLang $.Lang }}
func (s *Helper) BlogLang(lang string) string {
	if lang == "ru" {
		return "ru"
	}
	return "en"
}

// LangURL returns the URL for a given language and path.
// English (default) has no prefix; other languages get /{lang}{path}.
func LangURL(lang string, path string) string {
	if lang == i18n.DefaultLang {
		return path
	}
	if path == "/" {
		return "/" + lang + "/"
	}
	return "/" + lang + path
}

// LangURL is the template-callable wrapper.
// Template usage: {{ langURL "ru" $.Path }}
func (s *Helper) LangURL(lang string, path string) string {
	return LangURL(lang, path)
}

// Langs returns info for all supported languages (for the language switcher).
// Template usage: {{ range langs $.Lang $.Path }}
//
// For non-default languages the switcher points at the prefixed URL
// (`/ru/foo`) — prefix match in the HTTP middleware updates the cookie
// automatically. For the default language (English) the switcher needs an
// explicit `?lang=en` hint so the middleware knows the user is *choosing*
// English rather than following an arbitrary bare URL; otherwise the cookie
// still holds the previous preference and the no-prefix redirect below would
// bounce the user back to the prefixed page.
func (s *Helper) Langs(currentLang string, path string) []LangInfo {
	langs := make([]LangInfo, 0, len(i18n.SupportedLangs))
	for _, code := range i18n.SupportedLangs {
		url := s.LangURL(code, path)
		if code == i18n.DefaultLang {
			if strings.Contains(url, "?") {
				url += "&lang=" + code
			} else {
				url += "?lang=" + code
			}
		}
		langs = append(langs, LangInfo{
			Code:    code,
			Name:    i18n.LangNames[code],
			URL:     url,
			Current: code == currentLang,
		})
	}
	return langs
}

// HrefLangs returns alternate-language links for SEO (hreflang tags).
// Template usage: {{ range hrefLangs $.Path }}
func (s *Helper) HrefLangs(path string) []HrefLang {
	links := make([]HrefLang, 0, len(i18n.SupportedLangs)+1)
	for _, code := range i18n.SupportedLangs {
		if code == i18n.DefaultLang {
			links = append(links, HrefLang{Lang: code, URL: s.domain + path})
		} else if path == "/" {
			links = append(links, HrefLang{Lang: code, URL: s.domain + "/" + code + "/"})
		} else {
			links = append(links, HrefLang{Lang: code, URL: s.domain + "/" + code + path})
		}
	}
	links = append(links, HrefLang{Lang: "x-default", URL: s.domain + path})
	return links
}

// LangPath prefixes a path with the language if not default.
// Template usage: {{ langPath $.Lang "/discover" }}
func (s *Helper) LangPath(lang string, path string) string {
	return s.LangURL(lang, path)
}

// PosterURL builds the unified resource-keyed poster URL, slotting in
// the auth-gated `/raw/` segment only when both conditions hold:
//   - the resource is classified as adult (isAdult=true), AND
//   - the request user has opted into seeing adult content unblurred
//     (ctx.UserSettings.ShowAdult=true)
//
// Non-adult resources never go through /raw/ — the endpoint would
// return the byte-identical image (no blur to suppress) at the cost
// of an extra auth check. Anonymous users get the default URL too;
// the /raw/ route requires sign-in.
//
// Exposed as both a package function (for handler-side callers and
// other helpers) and a Helper method (registered as the `posterURL`
// template func via reflection) so each call site picks the form
// that reads best at the call site.
//
// Template usage: {{ posterURL .ResourceID 240 .IsAdult $.Ctx }}
func PosterURL(resourceID string, width int, isAdult bool, ctx *Context) string {
	if isAdult && ctx != nil && ctx.UserSettings != nil && ctx.UserSettings.ShowAdult {
		return fmt.Sprintf("/lib/poster/raw/%s/%d.jpg", resourceID, width)
	}
	return fmt.Sprintf("/lib/poster/%s/%d.jpg", resourceID, width)
}

func (s *Helper) PosterURL(resourceID string, width int, isAdult bool, ctx *Context) string {
	return PosterURL(resourceID, width, isAdult, ctx)
}

// ShowAdultBadge tells templates whether to render the 18+ overlay on
// an adult card. False for users who've opted in to see adult content
// unblurred — the badge would just clutter every card once the blur
// is gone. Returns false for non-adult resources unconditionally so
// templates can use this as a single guard without an outer
// `if .IsAdult` check.
//
// Template usage: {{ if showAdultBadge .IsAdult $.Ctx }}
func ShowAdultBadge(isAdult bool, ctx *Context) bool {
	if !isAdult {
		return false
	}
	if ctx != nil && ctx.UserSettings != nil && ctx.UserSettings.ShowAdult {
		return false
	}
	return true
}

func (s *Helper) ShowAdultBadge(isAdult bool, ctx *Context) bool {
	return ShowAdultBadge(isAdult, ctx)
}

// CanonicalURL returns the full canonical URL for the current page and language.
// Template usage: {{ canonicalURL $.Lang $.Path }}
func (s *Helper) CanonicalURL(lang string, path string) string {
	if lang == i18n.DefaultLang {
		return s.domain + path
	}
	if path == "/" {
		return s.domain + "/" + lang + "/"
	}
	return s.domain + "/" + lang + path
}
