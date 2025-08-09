package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"net/http"
	"time"

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
	sv "github.com/webtor-io/web-ui/services"

	defaultErrors "errors"
)

const (
	supertokensHostFlag   = "supertokens-host"
	supertokensPortFlag   = "supertokens-port"
	UseFlag               = "use-auth"
	googleClientIDFlag    = "google-client-id"
	googleClientSecretFlag = "google-client-secret"
)

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   supertokensHostFlag,
			Usage:  "supertokens host",
			Value:  "",
			EnvVar: "SUPERTOKENS_SERVICE_HOST",
		},
		cli.IntFlag{
			Name:   supertokensPortFlag,
			Usage:  "supertokens port",
			EnvVar: "SUPERTOKENS_SERVICE_PORT",
		},
		cli.BoolFlag{
			Name:   UseFlag,
			Usage:  "use auth",
			EnvVar: "USE_AUTH",
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
	)
}

type Auth struct {
	url                string
	smtpUser           string
	smtpPass           string
	smtpSecure         bool
	smtpHost           string
	smtpPort           int
	domain             string
	pg                 *cs.PG
	googleClientID     string
	googleClientSecret string
}

func New(c *cli.Context, pg *cs.PG) *Auth {
	if !c.Bool(UseFlag) {
		return nil
	}
	return &Auth{
		url:                c.String(supertokensHostFlag) + ":" + c.String(supertokensPortFlag),
		smtpUser:           c.String(sv.SMTPUserFlag),
		smtpPass:           c.String(sv.SMTPPassFlag),
		smtpHost:           c.String(sv.SMTPHostFlag),
		smtpSecure:         c.BoolT(sv.SMTPSecureFlag),
		smtpPort:           c.Int(sv.SMTPPortFlag),
		domain:             c.String(sv.DomainFlag),
		pg:                 pg,
		googleClientID:     c.String(googleClientIDFlag),
		googleClientSecret: c.String(googleClientSecretFlag),
	}
}

func (s *Auth) Init() error {
	smtpSettings := emaildelivery.SMTPSettings{
		Host: s.smtpHost,
		From: emaildelivery.SMTPFrom{
			Name:  "Webtor",
			Email: s.smtpUser,
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
	ID       uuid.UUID
	Email    string
	Expired  bool
	HasToken bool
}

func (s *User) HasAuth() bool {
	return s.Email != ""
}

func GetUserFromContext(c *gin.Context) *User {
	u := &User{}
	// TODO: Make something better
	if c.Query(sv.AccessTokenParamName) != "" {
		uc := c.Request.Context().Value(UserContext{})
		su, ok := uc.(*models.User)
		if ok {
			u.ID = su.UserID
			u.Email = su.Email
		}
		return u
	}
	if sessionContainer := session.GetSessionFromRequestContext(c.Request.Context()); sessionContainer != nil {
		uc := c.Request.Context().Value(UserContext{})
		su, ok := uc.(*models.User)
		if ok {
			u.ID = su.UserID
			u.Email = su.Email
		}
	}
	if err := c.Request.Context().Value(ErrorContext{}); err != nil {
		if defaultErrors.As(err.(error), &errors.TryRefreshTokenError{}) {
			u.Expired = true
		}
	}
	return u
}

type ErrorContext struct{}

type UserContext struct{}

func (s *Auth) myVerifySession(options *sessmodels.VerifySessionOptions, otherHandler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, err := session.GetSession(r, w, options)
		//err = errors.TryRefreshTokenError{}
		if err != nil {
			ctx := context.WithValue(r.Context(), ErrorContext{}, err)
			r := r.WithContext(ctx)
			if defaultErrors.As(err, &errors.TryRefreshTokenError{}) {
				if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
					otherHandler(w, r)
					return
				}
				// This means that the session exists, but the access token
				// has expired.

				// You can handle this in a custom way by sending a 401.
				// Or you can call the errorHandler middleware as shown below
			} else if defaultErrors.As(err, &errors.UnauthorizedError{}) {
				otherHandler(w, r)
				return
				// This means that the session does not exist anymore.

				// You can handle this in a custom way by sending a 401.
				// Or you can call the errorHandler middleware as shown below
			} else if defaultErrors.As(err, &errors.InvalidClaimError{}) {
				otherHandler(w, r)
				return
				// The user is missing some required claim.
				// You can pass the missing claims to the frontend and handle it there
			}

			// OR you can use this errorHandler which will
			// handle all of the above errors in the default way
			err = supertokens.ErrorHandler(err, r, w)
			if err != nil {
				log.WithError(err).Error("failed to handle error")
				w.WriteHeader(500)
			}
			return
		}
		if sess != nil {
			ctx := context.WithValue(r.Context(), sessmodels.SessionContext, sess)
			u, err := s.createUser(sess)
			if err != nil {
				log.WithError(err).Error("failed to create user")
				w.WriteHeader(500)
			} else {
				ctx = context.WithValue(ctx, UserContext{}, u)
			}

			otherHandler(w, r.WithContext(ctx))
		} else {
			otherHandler(w, r)
		}
	}
}

func (s *Auth) createUser(sess sessmodels.SessionContainer) (u *models.User, err error) {
	db := s.pg.Get()
	if db == nil {
		return
	}
	userID := sess.GetUserID()
	
	// Try to get user from passwordless first
	userInfo, err := passwordless.GetUserByID(userID)
	if err == nil && userInfo.Email != nil {
		return models.GetOrCreateUser(db, *userInfo.Email)
	}
	
	// If not found in passwordless, try third-party
	tpUserInfo, err := thirdparty.GetUserByID(userID)
	if err != nil {
		return
	}
	return models.GetOrCreateUser(db, tpUserInfo.Email)
}

func (s *Auth) verifySession(options *sessmodels.VerifySessionOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		s.myVerifySession(options, func(rw http.ResponseWriter, r *http.Request) {
			c.Request = c.Request.WithContext(r.Context())
			c.Next()
		})(c.Writer, c.Request)
		// we call Abort so that the next handler in the chain is not called, unless we call Next explicitly
		c.Abort()
	}
}

func (s *Auth) RegisterHandler(r *gin.Engine) {
	// CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
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

func HasAuth(c *gin.Context) {
	u := GetUserFromContext(c)
	if !u.HasAuth() {
		c.Status(http.StatusUnauthorized)
		return
	}
	c.Next()
}
