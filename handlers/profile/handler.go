package profile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/handlers/donate"
	"github.com/webtor-io/web-ui/models"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/adminauth"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/data_export"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/libapi"
	"github.com/webtor-io/web-ui/services/notification"
	pay "github.com/webtor-io/web-ui/services/payments"
	rss "github.com/webtor-io/web-ui/services/release_subscription"
	"github.com/webtor-io/web-ui/services/s3"
	"github.com/webtor-io/web-ui/services/stremio"
	ua "github.com/webtor-io/web-ui/services/url_alias"
	usettings "github.com/webtor-io/web-ui/services/user_settings"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/template"
)

// BackendTypeInfo represents information about a streaming backend type
type BackendTypeInfo struct {
	Type        string
	DisplayName string
}

// S3Credentials is what a user pastes into rclone, the aws CLI or Cyberduck.
// The secret is derived from the access key (services/s3.DeriveSecretKey), so
// there is nothing extra to store and rotating the key rotates both.
type S3Credentials struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string
}

// APICredentials is what a script sends as `Authorization: Bearer <key>`.
// Unlike the S3 secret the key is stored, not derived, so it is shown once per
// page render and can only be rotated, never recovered.
type APICredentials struct {
	Endpoint string
	Key      string
}

type Data struct {
	StremioAddonURL       string
	WebDAVURL             string
	S3                    *S3Credentials
	API                   *APICredentials
	APIDocsURL            string
	Devices               []DeviceItem
	EmbedDomains          []models.EmbedDomain
	AddonUrls             []models.StremioAddonUrl
	TorznabIndexers       []models.TorznabIndexer
	Subscriptions         []models.ReleaseSubscription
	SubscriptionLimit     int
	StremioSettings       *models.StremioSettingsData
	StreamingBackends     []*models.StreamingBackend
	AvailableBackendTypes []BackendTypeInfo
	Is4KAvailable         bool
	MinBitrateFor4KMbps   int64
	VaultStats            *vault.UserStats
	UserSettings          *models.UserSettings
	ErrKey                string
	DisableWebDAV         bool
	DisableS3             bool
	DisableAPI            bool
	DisableEmbed          bool
	// IdentityEditable gates the administrator-password section: it must
	// only ever render when no external identity provider owns this
	// account's identity (auth.IdentityManagedExternally is false), never
	// when SuperTokens does. AdminPasswordActive is not used here because it
	// also requires a password to already be configured, which is exactly
	// the state this section exists to get the instance out of (setting the
	// very first password).
	IdentityEditable bool
	// PasswordSet and PasswordManagedEnv drive the password section's copy
	// and form: whether a "current password" field is needed at all, and
	// whether the form should be refused because ADMIN_PASSWORD governs it.
	PasswordSet        bool
	PasswordManagedEnv bool
	// HasPayments toggles the "my payments" link: shown only when the user
	// has at least one crypto payment (Patreon history lives on patreon.com).
	HasPayments bool
	// Billing feeds the "billed through <provider>, manage or cancel here"
	// line on a paid tier card. 39% of 2026-08 support mail was "how do I
	// cancel" from people who never connected the charge to the provider.
	Billing notification.Billing
	// ShowEmailSection gates the notification-email section. One capability:
	// mail can actually be sent (notification.Service.MailConfigured()).
	// Without SMTP there is nothing an address could achieve and no way to
	// verify one. It used to also require that no external identity provider
	// owned this account -- see emailSectionAvailable for why that was the
	// wrong question.
	ShowEmailSection bool
	// NotificationEmail is where mail actually goes: the confirmed address if
	// one was ever verified, otherwise the account's own Email -- the same
	// choice notification.RecipientEmail makes when sending, so this page
	// cannot name a destination the sender would not use.
	//
	// "" when neither is deliverable, which is the self-hosted case before an
	// address is set: Email there is the literal sentinel "admin", and showing
	// that would be exactly the confusion this section exists to remove.
	NotificationEmail string
	// PendingEmail is the address currently awaiting confirmation, or "" if
	// none is pending.
	PendingEmail string
}

type Handler struct {
	tb           template.Builder[*web.Context]
	ual          *ua.UrlAlias
	at           *at.AccessToken
	pg           *cs.PG
	claims       *claims.Claims
	vault        *vault.Vault
	userSettings *usettings.Service
	payments     *pay.Client
	releaseSubs  *rss.Service
	notification *notification.Service
	// billing: who takes the money and where it is managed; zero when no
	// provider is configured, and the tier card then says nothing about it.
	billing       notification.Billing
	disableWebDAV bool
	disableS3     bool
	disableAPI    bool
	disableEmbed  bool
	s3Secret      string
	s3Endpoint    string
	apiEndpoint   string
	domain        string
	adminStore    *adminauth.Store
	// identityEditable is auth.IdentityManagedExternally negated: true when
	// this deployment's own user row is the operator's identity (nothing
	// external -- SuperTokens -- owns it). The password section gates on this
	// alone rather than on AdminPasswordActive -- the latter also requires a
	// password to already exist, exactly the state the section exists to get
	// the instance out of (setting the very first password).
	//
	// The email section deliberately does NOT depend on it; see
	// emailSectionAvailable for why that condition was the wrong question.
	identityEditable bool
	// emailLimiter bounds how often one account may ask for a verification
	// mail. Without it POST /profile/email is a spam relay wearing this
	// instance's sender domain: every submission mints a fresh token, so the
	// notification journal's 24-hour window cannot suppress a repeat -- and
	// the verification mail bypasses that journal entirely anyway, to keep the
	// token out of the in-app feed. Nothing else stood between an
	// authenticated user and unbounded mail to an address of their choosing.
	//
	// Keyed on the account, not the client IP: the abuse is per-account, and
	// an attacker changes IP far more cheaply than they create accounts.
	emailLimiter *libapi.RateLimiter
}

func RegisterHandler(c *cli.Context, r *gin.Engine, tm *template.Manager[*web.Context], a *auth.Auth, at *at.AccessToken, ual *ua.UrlAlias, pg *cs.PG, cl *claims.Claims, v *vault.Vault, us *usettings.Service, payments *pay.Client, releaseSubs *rss.Service, ns *notification.Service, rdb redis.UniversalClient) {
	h := &Handler{
		tb:               tm.MustRegisterViews("profile/*").WithLayout("main"),
		at:               at,
		ual:              ual,
		pg:               pg,
		claims:           cl,
		vault:            v,
		userSettings:     us,
		payments:         payments,
		releaseSubs:      releaseSubs,
		notification:     ns,
		billing:          donate.Billing(c),
		disableWebDAV:    c.Bool(common.DisableWebDAVFlag),
		disableS3:        c.Bool(common.DisableS3Flag),
		disableAPI:       c.Bool(common.DisableAPIFlag),
		disableEmbed:     c.Bool(common.DisableEmbedFlag),
		s3Secret:         s3.SigningSecret(c),
		s3Endpoint:       s3.PublicEndpoint(c),
		apiEndpoint:      libapi.PublicEndpoint(c),
		domain:           c.String(common.DomainFlag),
		adminStore:       a.AdminStore(),
		identityEditable: !a.IdentityManagedExternally(),
		// Three in a row, then one per five minutes. Three covers a typo
		// corrected twice; a loop stops being useful immediately. Setting an
		// address is a once-in-an-account-lifetime action, so a limit this
		// tight costs a real user nothing.
		emailLimiter: libapi.NewRateLimiterWith(1.0/300.0, 3).WithRedis(rdb, "rl:email-verify"),
	}
	r.GET("/profile", h.get)
	r.GET("/profile/email/verify/:token", h.verifyEmail)
	gr := r.Group("/profile")
	gr.Use(auth.HasAuth)
	gr.POST("/delete", h.delete)
	gr.GET("/export", h.export)
	gr.POST("/settings", h.updateSettings)
	gr.POST("/password", h.setPassword)
	gr.POST("/email", h.setEmail)
}

// getAvailableBackendTypes returns the list of available streaming backend types
func getAvailableBackendTypes() []BackendTypeInfo {
	return []BackendTypeInfo{
		{Type: string(models.StreamingBackendTypeRealDebrid), DisplayName: "Real-Debrid"},
		{Type: string(models.StreamingBackendTypeTorbox), DisplayName: "Torbox"},
	}
}

func (s *Handler) getStremioAddonURL(c *gin.Context) (string, error) {
	at, err := s.at.GetTokenByName(c, "stremio")
	if at == nil {
		return "", err
	}
	url := fmt.Sprintf("/%s/%s/stremio/", common.AccessTokenParamName, at.Token)

	// proxy=true, as docs/stremio.md has always required and as the WebDAV
	// sibling below already does. With proxy=false the alias answered every
	// request with a 301 whose Location contained the raw access token, which
	// the Stremio client then stored and replayed on each call — putting an
	// account credential into a third-party app's config, its logs and every
	// hop in between. Proxying keeps the token server-side.
	//
	// Note for existing users: CreateOrGetURLAlias matches on the URL, so
	// rows minted before this keep proxy=false until the token is
	// regenerated. Flipping them is a separate data fix.
	al, err := s.ual.Get(c.Request.Context(), url, true)
	if err != nil {
		return "", err
	}
	return al + "/manifest.json", nil

}

// getS3Credentials returns the endpoint/key/secret triple, or nil when the user
// has not issued S3 credentials yet (the profile then shows the generate
// button, same as WebDAV).
func (s *Handler) getS3Credentials(c *gin.Context) (*S3Credentials, error) {
	at, err := s.at.GetTokenByName(c, s3.TokenName)
	if at == nil {
		return nil, err
	}
	key := at.Token.String()
	return &S3Credentials{
		Endpoint:  s.s3Endpoint,
		AccessKey: key,
		SecretKey: s3.DeriveSecretKey(s.s3Secret, key),
		Region:    s3.DefaultRegion,
	}, nil
}

// DeviceItem is one row of the profile's connected-devices list: a per-device
// API key issued through the device flow (/device).
type DeviceItem struct {
	// Name is the display label; FullName is the access_token row name the
	// revoke form posts back.
	Name      string
	FullName  string
	CreatedAt time.Time
}

// getDevices lists the device-flow keys. The account's own "api", WebDAV and
// Stremio tokens are filtered out by prefix — they have their own sections.
func (s *Handler) getDevices(c *gin.Context) ([]DeviceItem, error) {
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database is not available")
	}
	u := auth.GetUserFromContext(c)
	tokens, err := models.ListUserAccessTokens(c.Request.Context(), db, u.ID)
	if err != nil {
		return nil, err
	}
	var out []DeviceItem
	for _, t := range tokens {
		if !strings.HasPrefix(t.Name, libapi.DeviceTokenPrefix) {
			continue
		}
		out = append(out, DeviceItem{
			Name:      strings.TrimPrefix(t.Name, libapi.DeviceTokenPrefix),
			FullName:  t.Name,
			CreatedAt: t.CreatedAt,
		})
	}
	return out, nil
}

// getAPICredentials returns the endpoint/key pair, or nil when the user has not
// issued a key yet (the profile then shows the generate button, same as S3).
func (s *Handler) getAPICredentials(c *gin.Context) (*APICredentials, error) {
	at, err := s.at.GetTokenByName(c, libapi.TokenName)
	if at == nil {
		return nil, err
	}
	return &APICredentials{
		Endpoint: s.apiEndpoint,
		Key:      at.Token.String(),
	}, nil
}

func (s *Handler) getWebDAVURL(c *gin.Context) (string, error) {
	at, err := s.at.GetTokenByName(c, "webdav")
	if at == nil {
		return "", err
	}
	url := fmt.Sprintf("/%s/%s/webdav/fs/", common.AccessTokenParamName, at.Token)

	al, err := s.ual.Get(c.Request.Context(), url, true)
	if err != nil {
		return "", err
	}
	return al + "/webdav/", nil
}

func deleteUser(ctx context.Context, db *pg.DB, userID uuid.UUID) error {
	return models.DeleteUser(ctx, db, userID)
}

func (s *Handler) delete(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	if err := deleteUser(c.Request.Context(), db, u.ID); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	c.Redirect(http.StatusFound, "/logout")
}

// buildExport is the pure-data level: load the full user record and assemble
// the export payload. Returns nil when the authenticated user_id no longer
// exists in the DB (e.g. mid-deletion race).
func buildExport(ctx context.Context, db *pg.DB, userID uuid.UUID) (*data_export.Export, error) {
	u, err := models.GetUserByID(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	return data_export.Build(ctx, db, u)
}

func (s *Handler) export(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	db := s.pg.Get()
	exp, err := buildExport(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to build data export"))
		return
	}
	if exp == nil {
		c.Redirect(http.StatusFound, "/logout")
		return
	}
	filename := fmt.Sprintf("webtor-data-export-%s.json", time.Now().UTC().Format("2006-01-02"))
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Cache-Control", "no-store")
	enc := json.NewEncoder(c.Writer)
	enc.SetIndent("", "  ")
	if err := enc.Encode(exp); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to write data export"))
		return
	}
}

func (s *Handler) get(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		// Preserve the path so a deep link into the profile (e.g. the Stremio
		// CTA on tool pages) lands back here after sign-in instead of the
		// default page. The #stremio fragment never reaches the server, so the
		// section anchor is lost — the block sits near the top for that reason.
		lang := i18n.GetLang(c)
		returnURL := i18n.LangPath(lang, c.Request.URL.Path)
		if rq := c.Request.URL.RawQuery; rq != "" {
			returnURL += "?" + rq
		}
		v := url.Values{"return-url": []string{returnURL}}
		c.Redirect(http.StatusFound, i18n.LangPath(lang, "/login")+"?"+v.Encode())
		return
	}
	stremioURL, err := s.getStremioAddonURL(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get stremio addon url"))
		return
	}
	// Dev-only: render the Stremio block as an account that has not generated
	// its addon URL yet. A developer's own account has one, so the pre-token
	// state — the one every new user meets — is otherwise unreviewable
	// without a second account. Same gate as the other debug switches.
	if gin.Mode() != gin.ReleaseMode && c.Query("preview") == "stremio-fresh" {
		stremioURL = ""
	}
	webdavURL, err := s.getWebDAVURL(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get webdav url"))
		return
	}
	s3Creds, err := s.getS3Credentials(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get s3 credentials"))
		return
	}
	apiCreds, err := s.getAPICredentials(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get api credentials"))
		return
	}

	devices, err := s.getDevices(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get devices"))
		return
	}

	// Get user domains
	db := s.pg.Get()
	domains, err := models.GetUserDomains(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get user domains"))
		return
	}

	// Get user addon URLs
	addonUrls, err := models.GetAllUserStremioAddonUrls(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get user addon urls"))
		return
	}

	// Get user Torznab indexers
	torznabIndexers, err := models.GetAllUserTorznabIndexers(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get user torznab indexers"))
		return
	}

	// Get Stremio settings. When the user has never saved settings, prefill
	// the preferred language with the current UI language so the dropdown
	// shows a sensible default — saving the form locks it in.
	existingSS, err := models.GetUserStremioSettings(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get stremio settings"))
		return
	}
	var ss *models.StremioSettingsData
	if existingSS == nil {
		ss = models.GetDefaultStremioSettings()
		if l := stremio.LanguageByCode(i18n.GetLang(c)); l != nil {
			ss.PreferredLanguage = l.Code
		}
	} else {
		ss = existingSS.Settings
	}

	// Release subscriptions the user holds. The section lists them; the
	// entry points that create them live in Discover and on resource pages.
	var subscriptions []models.ReleaseSubscription
	if s.releaseSubs != nil {
		subscriptions, err = s.releaseSubs.List(c.Request.Context(), u.ID)
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get release subscriptions"))
			return
		}
	}

	// Get user streaming backends
	streamingBackends, err := models.GetUserStreamingBackends(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get user streaming backends"))
		return
	}

	// Get vault statistics if vault service is available
	var vaultStats *vault.UserStats
	if s.vault != nil {
		vaultStats, _, err = s.vault.GetUserStats(c.Request.Context(), u)
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get vault user stats"))
			return
		}
	}

	// Per-user preferences (adult-content visibility, etc). Missing row
	// falls through to Default() — the toggle renders unchecked.
	userSettings, err := s.userSettings.Get(c.Request.Context(), u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get user settings"))
		return
	}

	hasPayments := false
	if s.payments != nil {
		pctx, pcancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		items, perr := s.payments.ListPayments(pctx, u.ID.String())
		pcancel()
		// A history hiccup must not take the profile down — just hide the link.
		hasPayments = perr == nil && len(items) > 0
	}

	// The email section's two capability gates: our own row must be the
	// identity (not an external provider's) and mail must be sendable.
	// Only then is a pending or confirmed address even worth reading.
	showEmailSection := s.emailSectionAvailable()
	notificationEmail := ""
	pendingEmail := ""
	if showEmailSection {
		if full, ferr := models.GetUserByID(c.Request.Context(), db, u.ID); ferr == nil && full != nil {
			// The address mail actually goes to, which is what the reader
			// wants to know -- the same choice RecipientEmail makes when
			// sending, so the page cannot claim a destination the sender
			// would not use.
			//
			// Deliverable is what keeps the identity address out of it where
			// it is not one: in self-hosted Email is the literal sentinel
			// "admin", and showing that would be exactly the confusion this
			// section exists to remove. On webtor.io it is a real address, and
			// showing it is the point -- an account with nothing set still has
			// somewhere its mail goes, and saying so is how a reader learns
			// they are overriding rather than filling in a blank.
			effective := notification.RecipientEmail(full.Email, full.NotificationEmail)
			if notification.Deliverable(effective) {
				notificationEmail = effective
			}
			if full.PendingEmail != nil {
				pendingEmail = *full.PendingEmail
			}
		}
	}

	s.tb.Build("profile/get").HTML(http.StatusOK, web.NewContext(c).WithData(&Data{
		StremioAddonURL:       stremioURL,
		WebDAVURL:             webdavURL,
		S3:                    s3Creds,
		API:                   apiCreds,
		APIDocsURL:            s.apiEndpoint + "/docs/index.html",
		Devices:               devices,
		EmbedDomains:          domains,
		AddonUrls:             addonUrls,
		TorznabIndexers:       torznabIndexers,
		Subscriptions:         subscriptions,
		SubscriptionLimit:     rss.FreeTierLimit,
		StremioSettings:       ss,
		StreamingBackends:     streamingBackends,
		AvailableBackendTypes: getAvailableBackendTypes(),
		VaultStats:            vaultStats,
		UserSettings:          userSettings,
		ErrKey:                c.Query("err"),
		HasPayments:           hasPayments,
		Billing:               s.billing,
		DisableWebDAV:         s.disableWebDAV,
		DisableS3:             s.disableS3,
		DisableAPI:            s.disableAPI,
		DisableEmbed:          s.disableEmbed,
		IdentityEditable:      s.identityEditable,
		PasswordSet:           s.adminStore != nil && s.adminStore.IsConfigured(c.Request.Context()),
		PasswordManagedEnv:    s.adminStore != nil && s.adminStore.ManagedByEnv(),
		ShowEmailSection:      showEmailSection,
		NotificationEmail:     notificationEmail,
		PendingEmail:          pendingEmail,
	}))
}

// setPassword sets or changes the single administrator password. Changing an
// existing password requires the current one: otherwise a stolen session
// converts into a permanent takeover.
//
// The !s.identityEditable check is defense in depth, not the only thing
// standing between this route and webtor.io: adminauth's Postgres repo
// already scopes every read/write to the literal "email = 'admin'" row that
// only the local auto-admin flow ever creates, so on production Set would
// fail closed (0 rows affected) even without this check. But that
// protection lives one file away and depends on no real account ever having
// that email; checking identityEditable here means the route refuses
// outright instead of relying on that coincidence.
func (s *Handler) setPassword(c *gin.Context) {
	if s.adminStore == nil || !s.identityEditable {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	ctx := c.Request.Context()
	if s.adminStore.ManagedByEnv() {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	// Both refusals go through RedirectWithError, like the rest of this file:
	// it answers X-Return-Url rather than a hardcoded /profile (so the language
	// prefix and the page the visitor was on survive), sets status=error beside
	// the key, and answers JSON to a caller that asked for it. Hand-built
	// "?err=" redirects did none of that.
	if s.adminStore.IsConfigured(ctx) && !s.adminStore.Verify(ctx, c.PostForm("current")) {
		web.RedirectWithError(c, web.NewUserError("auth.password.wrongCurrent",
			errors.New("current password did not match")))
		return
	}
	if err := s.adminStore.Set(ctx, c.PostForm("new")); err != nil {
		if errors.Is(err, adminauth.ErrTooShort) {
			web.RedirectWithError(c, web.NewUserError("auth.password.tooShort", err))
			return
		}
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	// Setting the first password flips adminStore.IsConfigured() to true for
	// every request from now on, including this visitor's very next one. If
	// this session isn't also marked admin-authenticated here (the same mark
	// handlers/auth/handler.go's passwordLogin sets), the person who just set
	// the password immediately looks unauthenticated to auth.AdminPasswordActive
	// and gets bounced to the login form on the redirect below.
	session := sessions.Default(c)
	session.Set(auth.AdminSessionKey, true)
	if err := session.Save(); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	// Same answer every other form on this page gives: the async layer reads
	// status/message off the redirect it lands on and raises the toast. A
	// bare redirect to /profile left the page reloading whole and saying
	// nothing, which is how a form that worked still looked like one that
	// had not.
	web.RedirectWithSuccessAndMessage(c, "toast.passwordSet")
}

// pendingEmailTokenBytes is 256 bits of randomness, hex-encoded to a
// 64-character token -- long enough that guessing one is not a viable
// attack, short enough to sit comfortably in a URL.
const pendingEmailTokenBytes = 32

// pendingEmailTTL is how long a verification link stays live. Matches the
// migration 71 doc comment and this task's brief.
const pendingEmailTTL = 24 * time.Hour

func newEmailVerificationToken() (string, error) {
	b := make([]byte, pendingEmailTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", errors.Wrap(err, "failed to generate verification token")
	}
	return hex.EncodeToString(b), nil
}

// emailSectionAvailable is the notification-email capability: mail can
// actually be sent, so an address can be verified before anything is sent to
// it and there is a point in having one at all.
//
// It used to also require identityEditable -- no external identity provider --
// which kept the section off webtor.io entirely. That condition was answering
// the wrong question. It guarded against editing the identity address, and
// nothing here does: a confirmed address lands in notification_email, a column
// that exists precisely so identity stays untouched. Tier lookup and Patreon
// matching read Email and are unaffected; RecipientEmail falls back to Email
// whenever no notification address is set, which is every account until its
// owner sets one.
//
// It is one method rather than two copies of the same condition because both
// the render decision (get) and the write decision (setEmail) must answer it
// identically. They did not: get gated, setEmail did not, and a route that was
// never rendered was still reachable by any authenticated user.
func (s *Handler) emailSectionAvailable() bool {
	return s.notification != nil && s.notification.MailConfigured()
}

// setEmail accepts a notification address for this account, stores it as
// pending, and mails exactly one message containing the verification link.
// Nothing else is ever sent to a pending address -- SendEmailVerification is
// the only Send* call in this codebase that reads PendingEmail rather than
// the account's confirmed Email.
//
// The capability gate is enforced here, not merely in get. A POST does not
// prove the GET that served the form saw the same gate state -- and on a
// deployment where the form is never rendered at all (external identity
// provider, or no SMTP) the route would otherwise still accept: any
// authenticated user could name an arbitrary address and have this instance
// mail a verification link to it, once per POST, since each POST mints a
// fresh token and so is never a duplicate; and could point their own
// notification_email away from the address the identity provider owns.
// Same shape as setPassword's !identityEditable check, for the same reason.
//
// Deliverable is checked on top of that, because a gate that says mail can
// be sent says nothing about whether this particular string is an address.
func (s *Handler) setEmail(c *gin.Context) {
	if !s.emailSectionAvailable() {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	email := strings.TrimSpace(c.PostForm("email"))
	if !notification.Deliverable(email) {
		web.RedirectWithError(c, web.NewUserError("profile.email.invalid",
			errors.New("submitted address is not deliverable")))
		return
	}
	u := auth.GetUserFromContext(c)
	// After the address is parsed, before anything is sent: a malformed
	// address costs no mail and should not consume the budget for correcting
	// it. Keyed on the account for the reason on emailLimiter.
	if s.emailLimiter != nil {
		// No Retry-After header: this answers with a redirect, and the header
		// only means anything on a 429 or 503. The reader gets the wait as a
		// message instead; the exact seconds go to the log.
		if retryAfter, ok := s.emailLimiter.Take(u.ID.String()); !ok {
			web.RedirectWithError(c, web.NewUserError("profile.email.tooOften",
				errors.Errorf("verification rate limit, retry after %s", retryAfter)))
			return
		}
	}
	db := s.pg.Get()
	if db == nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	token, err := newEmailVerificationToken()
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	expiresAt := time.Now().Add(pendingEmailTTL)
	if err := models.SetPendingEmail(c.Request.Context(), db, u.ID, email, token, expiresAt); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	if s.notification != nil {
		link := fmt.Sprintf("%s/profile/email/verify/%s", s.domain, token)
		if err := s.notification.SendEmailVerification(email, link, i18n.GetLang(c)); err != nil {
			_ = c.Error(errors.Wrap(err, "failed to send verification email"))
		}
	}
	// A sent verification is a success, not an error. It went out as
	// ?err=profile.email.sent, which rendered it in the section's error slot
	// -- red styling for the one outcome the user was hoping for.
	web.RedirectWithSuccessAndMessage(c, "toast.verificationSent")
}

// verifyEmail promotes a pending address whose token matches and has not
// expired. Unauthenticated on purpose -- see models.VerifyPendingEmail's
// doc comment for why matching by token alone is both sufficient and safe:
// the link is meant to be clickable from whatever mail client opened it,
// which may not carry this instance's session.
func (s *Handler) verifyEmail(c *gin.Context) {
	db := s.pg.Get()
	if db == nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	ok, err := models.VerifyPendingEmail(c.Request.Context(), db, c.Param("token"))
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to verify pending email"))
		return
	}
	// Rendered here, not redirected to /profile: the click arrives from a mail
	// client in whatever browser it opens, which need not carry a session --
	// and on an instance with ONLY_AUTHORIZED on, /profile would answer that
	// with a login form and swallow the result. Both outcomes also used to
	// travel as ?err=..., putting a success in the profile's error slot.
	s.tb.Build("profile/email_verified").HTML(http.StatusOK,
		web.NewContext(c).WithData(&emailVerifiedData{OK: ok}))
}

// emailVerifiedData drives profile/email_verified.html. One field, because
// there are exactly two outcomes and neither carries the address: the page is
// reachable by anyone holding the link, so it says whether the link worked
// and nothing about whose account it belongs to.
type emailVerifiedData struct {
	OK bool
}

// updateSettings persists the toggles from the per-user settings
// section of the profile page. Form is data-async so the response
// re-renders the section in place; web.RedirectWithSuccess routes
// the async-loader back to X-Return-Url with the standard
// status=success query param attached.
func (s *Handler) updateSettings(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	us := &models.UserSettings{
		UserID:    u.ID,
		ShowAdult: c.PostForm("show_adult") == "true",
	}
	if err := s.userSettings.Set(c.Request.Context(), us); err != nil {
		web.RedirectWithError(c, err)
		return
	}
	web.RedirectWithSuccess(c)
}
