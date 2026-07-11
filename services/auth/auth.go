package auth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	sv "github.com/webtor-io/web-ui/services/common"

	defaultErrors "errors"

	"github.com/gin-contrib/cors"
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
}

func (s *User) HasAuth() bool {
	return s.ID != uuid.Nil
}

func makeUserFromContext(c *gin.Context) *User {
	u := &User{}
	uc := c.Request.Context().Value(UserContext{})
	su, ok := uc.(*models.User)
	if ok {
		u.ID = su.UserID
		u.Email = su.Email
		u.PatreonUserID = su.PatreonUserID
		u.Tier = su.Tier
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
	if c.Query(sv.AccessTokenParamName) != "" {
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

func (s *Auth) RegisterHandler(r *gin.Engine) {
	if !s.hasSupetokens {
		r.Use(func(c *gin.Context) {
			s.registerAdminUser(c)

			c.Next()
		})
		return
	}
	// CORS
	r.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			return true
		},
		AllowMethods:     []string{"GET", "POST", "DELETE", "PUT", "OPTIONS"},
		AllowHeaders:     append([]string{"content-type"}, supertokens.GetAllCORSHeaders()...),
		MaxAge:           1 * time.Minute,
		AllowCredentials: true,
	}))

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

func IsAdmin(c *gin.Context) bool {
	v := c.Request.Context().Value(IsAdminContext{})
	isAdmin, ok := v.(bool)
	if !ok {
		return false
	}
	return isAdmin
}

func HasAuth(c *gin.Context) {
	u := GetUserFromContext(c)
	if !u.HasAuth() {
		c.Status(http.StatusUnauthorized)
		return
	}
	c.Next()
}
