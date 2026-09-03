package auth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/adminauth"
	sv "github.com/webtor-io/web-ui/services/common"

	defaultErrors "errors"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/supertokens/supertokens-golang/ingredients/emaildelivery"
	"github.com/supertokens/supertokens-golang/recipe/dashboard"
	"github.com/supertokens/supertokens-golang/recipe/passwordless"
	"github.com/supertokens/supertokens-golang/recipe/passwordless/plessmodels"
	"github.com/supertokens/supertokens-golang/recipe/session"
	"github.com/supertokens/supertokens-golang/recipe/session/errors"
	"github.com/supertokens/supertokens-golang/recipe/session/sessmodels"
	"github.com/supertokens/supertokens-golang/recipe/thirdparty"
	"github.com/supertokens/supertokens-golang/recipe/thirdparty/tpmodels"
	"github.com/supertokens/supertokens-golang/recipe/usermetadata"
	"github.com/supertokens/supertokens-golang/recipe/userroles"
	"github.com/supertokens/supertokens-golang/supertokens"
	"github.com/urfave/cli"
)

const (
	SupertokensHostFlag     = "supertokens-host"
	SupertokensPortFlag     = "supertokens-port"
	googleClientIDFlag      = "google-client-id"
	googleClientSecretFlag  = "google-client-secret"
	patreonClientIDFlag     = "patreon-client-id"
	patreonClientSecretFlag = "patreon-client-secret"
	overrideUserEmail       = "override-user-email"
	adminPasswordFlag       = "admin-password"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   SupertokensHostFlag,
			Usage:  "supertokens host",
			Value:  "",
			EnvVar: "SUPERTOKENS_SERVICE_HOST",
		},
		cli.IntFlag{
			Name:   SupertokensPortFlag,
			Usage:  "supertokens port",
			EnvVar: "SUPERTOKENS_SERVICE_PORT",
		},
		cli.StringFlag{
			Name:   googleClientIDFlag,
			Usage:  "google oauth client id",
			EnvVar: "GOOGLE_CLIENT_ID",
		},
		cli.StringFlag{
			Name:   googleClientSecretFlag,
			Usage:  "google oauth client secret",
			EnvVar: "GOOGLE_CLIENT_SECRET",
		},
		cli.StringFlag{
			Name:   patreonClientIDFlag,
			Usage:  "patreon oauth client id",
			EnvVar: "PATREON_CLIENT_ID",
		},
		cli.StringFlag{
			Name:   patreonClientSecretFlag,
			Usage:  "patreon oauth client secret",
			EnvVar: "PATREON_CLIENT_SECRET",
		},
		cli.StringFlag{
			Name:   overrideUserEmail,
			Usage:  "override user email",
			EnvVar: "OVERRIDE_USER_EMAIL",
		},
		cli.StringFlag{
			Name:   adminPasswordFlag,
			Usage:  "password for the single self-hosted administrator; overrides the stored one and disables changing it from the profile",
			EnvVar: "ADMIN_PASSWORD",
		},
	)
}

type Auth struct {
	url                 string
	smtpUser            string
	smtpPass            string
	smtpFrom            string
	smtpSecure          bool
	smtpHost            string
	smtpPort            int
	domain              string
	cl                  *http.Client
	pg                  *cs.PG
	googleClientID      string
	googleClientSecret  string
	patreonClientID     string
	patreonClientSecret string
	hasSupetokens       bool
	overrideUserEmail   string
	adminStore          *adminauth.Store
}

func New(c *cli.Context, cl *http.Client, pg *cs.PG) *Auth {
	return &Auth{
		url:                 "http://" + c.String(SupertokensHostFlag) + ":" + c.String(SupertokensPortFlag),
		hasSupetokens:       c.String(SupertokensHostFlag) != "" && c.String(SupertokensPortFlag) != "",
		smtpUser:            c.String(sv.SMTPUserFlag),
		smtpPass:            c.String(sv.SMTPPassFlag),
		smtpFrom:            c.String(sv.SMTPFromFlag),
		smtpHost:            c.String(sv.SMTPHostFlag),
		smtpSecure:          c.BoolT(sv.SMTPSecureFlag),
		smtpPort:            c.Int(sv.SMTPPortFlag),
		domain:              c.String(sv.DomainFlag),
		cl:                  cl,
		pg:                  pg,
		googleClientID:      c.String(googleClientIDFlag),
		googleClientSecret:  c.String(googleClientSecretFlag),
		patreonClientID:     c.String(patreonClientIDFlag),
		patreonClientSecret: c.String(patreonClientSecretFlag),
		overrideUserEmail:   c.String(overrideUserEmail),
		adminStore:          adminauth.NewStore(c.String(adminPasswordFlag), adminauth.NewPGRepo(pg)),
	}
}

func (s *Auth) Init() error {
	if !s.hasSupetokens {
		return nil
	}
	fromEmail := s.smtpFrom
	if fromEmail == "" {
		fromEmail = s.smtpUser
	}
	smtpSettings := emaildelivery.SMTPSettings{
		Host: s.smtpHost,
		From: emaildelivery.SMTPFrom{
			Name:  "Webtor",
			Email: fromEmail,
		},
		Username: &s.smtpUser,
		Port:     s.smtpPort,
		Password: s.smtpPass,
		Secure:   s.smtpSecure,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         s.smtpHost,
		},
	}
	apiBasePath := "/auth"
	websiteBasePath := "/auth"
	return supertokens.Init(supertokens.TypeInput{
		// Debug: true,
		Supertokens: &supertokens.ConnectionInfo{
			// https://try.supertokens.com is for demo purposes. Replace this with the address of your core instance (sign up on supertokens.com), or self host a core.
			ConnectionURI: s.url,
			// APIKey: <API_KEY(if configured)>,
		},
		AppInfo: supertokens.AppInfo{
			AppName:         "webtor",
			APIDomain:       s.domain,
			WebsiteDomain:   s.domain,
			APIBasePath:     &apiBasePath,
			WebsiteBasePath: &websiteBasePath,
		},
		RecipeList: []supertokens.Recipe{
			passwordless.Init(plessmodels.TypeInput{
				FlowType: "MAGIC_LINK",
				ContactMethodEmail: plessmodels.ContactMethodEmailConfig{
					Enabled: true,
				},
				EmailDelivery: &emaildelivery.TypeInput{
					Service: passwordless.MakeSMTPService(emaildelivery.SMTPServiceConfig{
						Settings: smtpSettings,
						Override: func(originalImplementation emaildelivery.SMTPInterface) emaildelivery.SMTPInterface {
							*originalImplementation.GetContent = func(input emaildelivery.EmailType, userContext supertokens.UserContext) (emaildelivery.EmailContent, error) {

								email := input.PasswordlessLogin.Email

								// magic link
								urlWithLinkCode := *input.PasswordlessLogin.UrlWithLinkCode
								body := fmt.Sprintf("<a href=\"%v\">Login to your account!</a>", urlWithLinkCode)

								// send some custom email content
								return emaildelivery.EmailContent{
									Body:    body,
									IsHtml:  true,
									Subject: "Login to your account!",
									ToEmail: email,
								}, nil

							}

							return originalImplementation
						},
					}),
				},
			}),
			thirdparty.Init(&tpmodels.TypeInput{
				SignInAndUpFeature: tpmodels.TypeInputSignInAndUp{
					Providers: []tpmodels.ProviderInput{
						{
							Config: tpmodels.ProviderConfig{
								ThirdPartyId: "google",
								Clients: []tpmodels.ProviderClientConfig{
									{
										ClientID:     s.googleClientID,
										ClientSecret: s.googleClientSecret,
									},
								},
							},
						},
						{
							Config: tpmodels.ProviderConfig{
								ThirdPartyId:          "patreon",
								AuthorizationEndpoint: "https://www.patreon.com/oauth2/authorize",
								TokenEndpoint:         "https://www.patreon.com/api/oauth2/token",
								TokenEndpointBodyParams: map[string]interface{}{
									"grant_type":    "authorization_code",
									"client_id":     s.patreonClientID,
									"client_secret": s.patreonClientSecret,
								},
								Clients: []tpmodels.ProviderClientConfig{
									{
										ClientID:     s.patreonClientID,
										ClientSecret: s.patreonClientSecret,
										Scope:        []string{"identity", "identity[email]"},
									},
								},
							},
							Override: func(originalImplementation *tpmodels.TypeProvider) *tpmodels.TypeProvider {
								originalImplementation.GetUserInfo = func(oAuthTokens map[string]interface{}, userContext *map[string]interface{}) (tpmodels.TypeUserInfo, error) {
									accessToken, _ := oAuthTokens["access_token"].(string)
									identityURL := "https://www.patreon.com/api/oauth2/v2/identity?fields[user]=email"
									req, err := http.NewRequest("GET", identityURL, nil)
									if err != nil {
										log.WithError(err).Error("patreon identity: build request failed")
										return tpmodels.TypeUserInfo{}, err
									}
									req.Header.Set("Authorization", "Bearer "+accessToken)
									req.Header.Set("Content-Type", "application/json")
									res, err := s.cl.Do(req)
									if err != nil {
										log.WithError(err).Error("patreon identity: http call failed")
										return tpmodels.TypeUserInfo{}, err
									}
									defer func(Body io.ReadCloser) {
										_ = Body.Close()
									}(res.Body)
									body, err := io.ReadAll(res.Body)
									if err != nil {
										log.WithError(err).WithField("status", res.StatusCode).Error("patreon identity: read body failed")
										return tpmodels.TypeUserInfo{}, err
									}
									bodyPreview := string(body)
									if len(bodyPreview) > 1024 {
										bodyPreview = bodyPreview[:1024]
									}
									if res.StatusCode < 200 || res.StatusCode >= 300 {
										log.WithField("status", res.StatusCode).WithField("body", bodyPreview).Error("patreon identity: non-2xx response")
										return tpmodels.TypeUserInfo{}, fmt.Errorf("patreon identity: status %d", res.StatusCode)
									}
									var data PatreonIdentityResponse
									if err := json.Unmarshal(body, &data); err != nil {
										log.WithError(err).WithField("body", bodyPreview).Error("patreon identity: unmarshal failed")
										return tpmodels.TypeUserInfo{}, err
									}
									if data.Data.ID == "" || data.Data.Attributes.Email == "" {
										log.WithField("has_id", data.Data.ID != "").WithField("has_email", data.Data.Attributes.Email != "").WithField("body", bodyPreview).Error("patreon identity: empty id or email")
										return tpmodels.TypeUserInfo{}, fmt.Errorf("patreon identity: missing id or email")
									}
									return tpmodels.TypeUserInfo{
										ThirdPartyUserId: data.Data.ID,
										Email: &tpmodels.EmailStruct{
											ID:         data.Data.Attributes.Email,
											IsVerified: true,
										},
										RawUserInfoFromProvider: tpmodels.TypeRawUserInfoFromProvider{
											FromUserInfoAPI: map[string]interface{}{},
										},
									}, nil
								}
								return originalImplementation
							},
						},
					},
				},
			}),
			session.Init(nil), // initializes session features
			dashboard.Init(nil),
			usermetadata.Init(nil),
			userroles.Init(nil),
		},
	})
}

type User struct {
	ID            uuid.UUID
	Email         string
	Expired       bool
	PatreonUserID *string
	IsNew         bool
	Tier          string
	// CreatedAt is the account's registration time, surfaced so handlers can
	// answer "how old is this account" without a query. Populated on every
	// path that yields a user: GetOrCreateUser selects the full row on lookup
	// and go-pg adds RETURNING for the defaulted column on insert. Callers
	// should still treat a zero value as "don't know" rather than "very old",
	// since it only occurs when there is no user row at all.
	CreatedAt time.Time
	// NotificationEmail mirrors models.User.NotificationEmail -- the
	// confirmed address a self-hosted operator verified, kept separate from
	// Email (identity) for the reasons on that field's doc comment. Nil on
	// every webtor.io account. Callers picking a mail recipient should go
	// through notification.RecipientEmail(u.Email, u.NotificationEmail)
	// rather than reading either field directly.
	NotificationEmail *string
}

func (s *User) HasAuth() bool {
	return s.ID != uuid.Nil
}

func makeUserFromContext(c *gin.Context) *User {
	u := &User{}
	uc := c.Request.Context().Value(UserContext{})
	su, ok := uc.(*models.User)
	// A type assertion on an interface holding a typed nil (*models.User)(nil)
	// still succeeds -- ok is true even though su is nil -- so nilness must
	// be checked explicitly here rather than folded into `if ok`, or any
	// future path that puts a nil *models.User into the context turns into a
	// dereference below.
	if ok && su != nil {
		u.ID = su.UserID
		u.Email = su.Email
		u.PatreonUserID = su.PatreonUserID
		u.Tier = su.Tier
		u.CreatedAt = su.CreatedAt
		u.NotificationEmail = su.NotificationEmail
	}
	inc := c.Request.Context().Value(IsNewContext{})
	isNew, ok := inc.(bool)
	if ok {
		u.IsNew = isNew
	}
	return u
}

func GetUserFromContext(c *gin.Context) *User {
	if IsAdmin(c) {
		return makeUserFromContext(c)
	}
	// A token-derived identity is whatever services/access_token actually
	// put in the request context, not the mere presence of a query
	// parameter. Trusting the parameter meant any route treated a token as
	// proof of identity even where nothing had resolved or scope-checked it.
	if su, ok := c.Request.Context().Value(UserContext{}).(*models.User); ok && su != nil {
		return makeUserFromContext(c)
	}
	if sessionContainer := session.GetSessionFromRequestContext(c.Request.Context()); sessionContainer != nil {
		return makeUserFromContext(c)
	}
	u := &User{}
	if err := c.Request.Context().Value(ErrorContext{}); err != nil {
		if defaultErrors.As(err.(error), &errors.TryRefreshTokenError{}) {
			u.Expired = true
		}
	}
	return u
}

type ErrorContext struct{}

type UserContext struct{}
type IsNewContext struct{}

func (s *Auth) myVerifySession(c *gin.Context, options *sessmodels.VerifySessionOptions, otherHandler http.HandlerFunc) {
	w, r := c.Writer, c.Request
	sess, err := session.GetSession(r, w, options)
	if err != nil {
		ctx := context.WithValue(r.Context(), ErrorContext{}, err)
		r := r.WithContext(ctx)
		if defaultErrors.As(err, &errors.TryRefreshTokenError{}) {
			if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				otherHandler(w, r)
				return
			}
			// session exists but the access token expired
		} else if defaultErrors.As(err, &errors.UnauthorizedError{}) {
			otherHandler(w, r)
			return
			// session does not exist anymore — proceed as anonymous
		} else if defaultErrors.As(err, &errors.InvalidClaimError{}) {
			otherHandler(w, r)
			return
			// user is missing some required claim
		}

		// Any remaining error means SuperTokens couldn't handle it — its
		// core is unreachable (auth-DB blip). Hand it to the centralized
		// ErrorHandler (services/web) to render the friendly page, instead
		// of a bare 500. The error is still logged there.
		err = supertokens.ErrorHandler(err, r, w)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
		}
		return
	}
	if sess != nil {
		ctx := context.WithValue(r.Context(), sessmodels.SessionContext, sess)
		u, isNew, err := s.createUser(r.Context(), sess)
		if err != nil {
			// App DB unreachable while materializing the user — same class.
			_ = c.Error(err)
			c.Abort()
			return
		}
		ctx = context.WithValue(ctx, UserContext{}, u)
		ctx = context.WithValue(ctx, IsNewContext{}, isNew)
		otherHandler(w, r.WithContext(ctx))
	} else {
		otherHandler(w, r)
	}
}

func (s *Auth) createUser(ctx context.Context, sess sessmodels.SessionContainer) (u *models.User, isNew bool, err error) {
	db := s.pg.Get()
	if db == nil {
		// Database outage is transient and recoverable, unlike an identity
		// provider that yields no email. Degrading a brief blip to "no user"
		// is better than turning it into a broken response. The nil user is
		// safe because makeUserFromContext checks for it explicitly.
		return
	}
	userID := sess.GetUserID()

	if s.overrideUserEmail != "" {
		return models.GetOrCreateUser(ctx, db, s.overrideUserEmail, nil)
	}

	// Try to get user from passwordless first
	userInfo, err := passwordless.GetUserByID(userID)
	if err == nil && userInfo != nil && userInfo.Email != nil {
		return models.GetOrCreateUser(ctx, db, *userInfo.Email, nil)
	}

	// If not found in passwordless, try third-party
	tpUserInfo, err := thirdparty.GetUserByID(userID)
	if err == nil && tpUserInfo != nil && tpUserInfo.Email != "" {
		var patreonUserID *string = nil
		if tpUserInfo.ThirdParty.ID == "patreon" {
			patreonUserID = &tpUserInfo.ThirdParty.UserID
		}
		return models.GetOrCreateUser(ctx, db, tpUserInfo.Email, patreonUserID)
	}
	// Every branch above either returned or carried its own error. Getting
	// here with err == nil means supertokens resolved the session but never
	// yielded an email (unreachable today: Google always returns one, and
	// the Patreon override in Init rejects a sign-in without one) -- return
	// an explicit error instead of falling through with u still nil. A bare
	// return here would hand myVerifySession a nil error alongside a nil
	// *models.User, which it would then store in the request context as a
	// typed nil that satisfies makeUserFromContext's type assertion and gets
	// dereferenced.
	if err == nil {
		err = fmt.Errorf("createUser: no email resolved for supertokens user %s", userID)
	}
	return
}

func (s *Auth) verifySession(options *sessmodels.VerifySessionOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		s.myVerifySession(c, options, func(rw http.ResponseWriter, r *http.Request) {
			c.Request = c.Request.WithContext(r.Context())
			c.Next()
		})
		// we call Abort so that the next handler in the chain is not called, unless we call Next explicitly
		c.Abort()
	}
}

// corsOrigins lists the hosts allowed to make credentialed cross-origin
// requests. Only this site qualifies: the SuperTokens APIDomain and
// WebsiteDomain are the same host, so there is no second origin that needs to
// send cookies here.
//
// An unset domain yields an empty list, which denies every cross-origin
// credentialed request. That is the direction a misconfiguration should fall.
func (s *Auth) corsOrigins() []string {
	d := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(s.domain, "https://"), "http://"), "/")
	if d == "" {
		return nil
	}
	return []string{d}
}

// RegisterHandler wires SuperTokens session handling and its CORS policy.
//
// corsExemptPrefixes are path prefixes the SuperTokens CORS middleware must
// leave alone — the JSON API under /api/v1 owns its own CORS (wildcard,
// bearer-auth, PATCH included), and this global one would otherwise answer
// its preflights first with an origin-echo + credentials policy that both
// lacks PATCH and is wrong for a cookie-less API.
func (s *Auth) RegisterHandler(r *gin.Engine, corsExemptPrefixes ...string) {
	if !s.hasSupetokens {
		r.Use(func(c *gin.Context) {
			// Three states, in order of precedence:
			//   no password configured → open instance, auto-admin (legacy
			//     behaviour) plus a flag the banner renders from;
			//   password configured and the session carries the login mark →
			//     auto-admin;
			//   password configured and no mark → stay anonymous, which lands
			//     the request on HasAuth and from there on /login.
			if !s.adminStore.IsConfigured(c.Request.Context()) {
				ctx := context.WithValue(c.Request.Context(), IsOpenInstanceContext{}, true)
				c.Request = c.Request.WithContext(ctx)
				s.registerAdminUser(c)
				c.Next()
				return
			}
			if adminSessionActive(c) {
				s.registerAdminUser(c)
			}
			c.Next()
		})
		return
	}
	// CORS
	//
	// AllowOriginFunc used to return true unconditionally. Combined with
	// AllowCredentials that is the reflected-origin misconfiguration:
	// gin-contrib/cors leaves allowAllOrigins false whenever an origin func
	// is set, so it echoes the caller's Origin into
	// Access-Control-Allow-Origin and always emits
	// Access-Control-Allow-Credentials: true. Since the gin session cookie
	// is deliberately SameSite=None (see handlers/session, the embed flow
	// needs it), that cookie rides a cross-site fetch — so any website could
	// read the body of a page served under it, window._CSRF included.
	//
	// The site is its own only legitimate credentialed origin: APIDomain and
	// WebsiteDomain are the same host here, and nothing cross-origin needs
	// to send credentials. Everything genuinely public (the poster routes,
	// the /s alias, /api/v1) declares its own wildcard CORS *without*
	// credentials, which is the safe combination and is unaffected by this.
	allowedOrigins := map[string]bool{}
	for _, h := range s.corsOrigins() {
		allowedOrigins["https://"+h] = true
		allowedOrigins["http://"+h] = true
	}
	corsHandler := cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			return allowedOrigins[origin]
		},
		AllowMethods:     []string{"GET", "POST", "DELETE", "PUT", "OPTIONS"},
		AllowHeaders:     append([]string{"content-type"}, supertokens.GetAllCORSHeaders()...),
		MaxAge:           1 * time.Minute,
		AllowCredentials: true,
	})
	r.Use(corsWithExemptions(corsHandler, corsExemptPrefixes))

	r.Use(func(c *gin.Context) {
		supertokens.Middleware(http.HandlerFunc(
			func(rw http.ResponseWriter, r *http.Request) {
				c.Next()
			})).ServeHTTP(c.Writer, c.Request)
		// we call Abort so that the next handler in the chain is not called, unless we call Next explicitly
		c.Abort()
	})
	sessionRequired := false
	r.Use(s.verifySession(&sessmodels.VerifySessionOptions{
		SessionRequired: &sessionRequired,
	}))
}

type IsAdminContext struct{}

func (s *Auth) registerAdminUser(c *gin.Context) {
	db := s.pg.Get()
	if db == nil {
		return
	}
	u, isNew, err := models.GetOrCreateUser(c.Request.Context(), db, "admin", nil)
	if err != nil {
		log.WithError(err).Error("failed to create admin user")
		return
	}
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, UserContext{}, u)
	ctx = context.WithValue(ctx, IsNewContext{}, isNew)
	ctx = context.WithValue(ctx, IsAdminContext{}, true)
	c.Request = c.Request.WithContext(ctx)
}

// IsOpenInstanceContext marks a request served by an instance that has no
// administrator password at all.
type IsOpenInstanceContext struct{}

// IsOpenInstance reports whether this instance is running without a password.
// Templates use it to render the "set a password" banner.
func IsOpenInstance(c *gin.Context) bool {
	v := c.Request.Context().Value(IsOpenInstanceContext{})
	open, ok := v.(bool)
	return ok && open
}

// AdminSessionKey is the session entry that records a successful password
// login. It lives in the session store already configured in
// handlers/session, so it inherits that cookie's HttpOnly/Secure settings.
const AdminSessionKey = "admin-authenticated"

func adminSessionActive(c *gin.Context) bool {
	v := sessions.Default(c).Get(AdminSessionKey)
	active, ok := v.(bool)
	return ok && active
}

// AdminPasswordActive reports whether the password branch governs this
// request. It is false wherever SuperTokens is configured, which is what
// keeps production untouched.
//
// This is the single place that decision gets made. handlers/auth's GET
// /login and POST /login both call it (via the func value RegisterHandler
// hands them) instead of re-deriving "is the password form active" from
// AdminStore().IsConfigured() themselves — IsConfigured alone is not enough:
// it fails closed (returns true) on a repository error, which is correct for
// self-hosted but would otherwise show the password form on webtor.io during
// a transient Postgres blip if hasSupetokens weren't checked too.
func (s *Auth) AdminPasswordActive(c *gin.Context) bool {
	if s.hasSupetokens {
		return false
	}
	if c == nil {
		return s.adminStore != nil
	}
	return s.adminStore.IsConfigured(c.Request.Context())
}

// AdminStore exposes the password store to the login and profile handlers.
func (s *Auth) AdminStore() *adminauth.Store {
	return s.adminStore
}

// IdentityManagedExternally reports whether an external identity provider
// owns this user's identity. The profile page's password section uses this
// — not AdminPasswordActive — to decide whether to show itself:
// AdminPasswordActive also requires a password to already be configured,
// which is exactly the state this section exists to get the instance out of
// (setting the very first password). When SuperTokens is configured, a
// user's email is also what claims.go keys the tier lookup on and what
// models.GetOrCreateUser matches Patreon accounts by — letting a user edit
// it here would silently detach both, which is why the email section
// depends on this same capability. Callers deciding whether an address is
// theirs to change, or whether a local administrator password may exist at
// all, both belong on this method.
func (s *Auth) IdentityManagedExternally() bool {
	return s.hasSupetokens
}

// NewForAdminPasswordTest builds a minimal Auth exposing only the two fields
// AdminPasswordActive reads. It exists so handlers/auth's tests can exercise
// the real gating decision — not a hand-rolled stand-in for it — without
// going through New(), which needs a live *cli.Context, *cs.PG and
// http.Client that a fast handler-logic test has no business standing up.
// Every other field stays zero; nothing but AdminPasswordActive should be
// called on the result.
func NewForAdminPasswordTest(hasSupertokens bool, store *adminauth.Store) *Auth {
	return &Auth{hasSupetokens: hasSupertokens, adminStore: store}
}

// ForgetAdmin drops the admin identity from this request's context.
//
// The auth middleware resolves the user before any handler runs, so a
// handler that ends the session -- logout -- has already been handed a page
// context saying "signed in as admin". Rendering from it produced a sign-out
// page whose navbar still showed the account that had just been signed out,
// correcting itself only on the next navigation.
//
// Clearing IsAdminContext is enough: GetUserFromContext consults it first,
// and with no access-token query and no SuperTokens session the remaining
// paths yield an anonymous user. The UserContext value is deliberately left
// alone rather than overwritten with a nil *models.User -- an interface
// holding a typed nil is not nil, and that trap has been paid for twice in
// this package already.
func ForgetAdmin(c *gin.Context) {
	ctx := context.WithValue(c.Request.Context(), IsAdminContext{}, false)
	c.Request = c.Request.WithContext(ctx)
}

func IsAdmin(c *gin.Context) bool {
	v := c.Request.Context().Value(IsAdminContext{})
	isAdmin, ok := v.(bool)
	if !ok {
		return false
	}
	return isAdmin
}

// HasAuth rejects anonymous requests. It must abort, not merely return:
// gin's handler chain is a loop over a slice, and a middleware that returns
// without aborting hands control straight to the next handler — the 401
// status set here would then be overwritten by whatever that handler wrote,
// and every route behind this middleware would run with an empty user.
//
// A browser navigating to a protected page gets a redirect to the login form;
// everything else keeps the bare 401, because an XHR or an SSE stream cannot
// do anything useful with an HTML page.
func HasAuth(c *gin.Context) {
	u := GetUserFromContext(c)
	if !u.HasAuth() {
		if isNavigation(c.Request) {
			target := "/login"
			if p := c.Request.URL.Path; isSafeReturnPath(p) {
				target += "?return-url=" + url.QueryEscape(p)
			}
			c.Redirect(http.StatusFound, target)
			c.Abort()
			return
		}
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	c.Next()
}

// isNavigation reports whether the request is a browser navigating to a page,
// as opposed to a script fetching data. Sec-Fetch-Mode is authoritative where
// the browser sends it; the Accept check covers the rest.
func isNavigation(r *http.Request) bool {
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		return false
	}
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return mode == "navigate"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// isSafeReturnPath reports whether p is safe to hand back to /login as
// return-url. The login template renders that value straight into an href
// with no escaping (templates/partials/auth/form.html), so p must be a
// site-relative path that resolves the same way in every browser as it does
// in Go — never something a browser's URL parser rewrites into a
// scheme-relative or off-site reference.
//
// Two escape hatches beyond a literal "//" prefix matter here:
//   - a leading backslash: WHATWG URL parsing normalizes a leading "\" to
//     "/" for special schemes when a browser resolves an href, so
//     "/\evil.com" resolves exactly like "//evil.com" — off-site — even
//     though Go's net/url keeps the backslash verbatim in r.URL.Path and
//     never treats it as a host separator itself.
//   - ASCII control characters (tab, CR, LF, ...): browsers strip these
//     while parsing a URL, so "/\t/evil.com" collapses to "//evil.com" by
//     the time it's dereferenced, even though it looks like an ordinary
//     same-origin path here. Go will decode a percent-encoded control byte
//     (e.g. "%09") straight into r.URL.Path, so this is reachable.
func isSafeReturnPath(p string) bool {
	if p == "" || p[0] != '/' {
		return false
	}
	if len(p) > 1 && (p[1] == '/' || p[1] == '\\') {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 0x20 || p[i] == 0x7f {
			return false
		}
	}
	return true
}

// corsWithExemptions wraps the credentialed site-origin CORS handler so that
// paths under corsExemptPrefixes never reach it. gin-contrib/cors answers a
// disallowed Origin with 403 before any route runs, so a public surface that
// declares its own wildcard CORS on its route group is unreachable from a
// foreign origin unless its prefix is listed here — the group middleware
// only runs once the request gets past this global one. Exemption means "no
// CORS headers from this handler", not "allow": the surface's own policy is
// what the browser then sees. Incident 2026-09-03: /stremio and the
// /token/<id>/stremio rewrite were missing, so the Stremio web and desktop
// clients (which send Origin) got 403 on every addon call.
func corsWithExemptions(corsHandler gin.HandlerFunc, corsExemptPrefixes []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, p := range corsExemptPrefixes {
			if strings.HasPrefix(c.Request.URL.Path, p) {
				c.Next()
				return
			}
		}
		corsHandler(c)
	}
}
