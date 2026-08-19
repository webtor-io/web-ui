package auth

import (
	"net"
	"net/http"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/webtor-io/web-ui/services/adminauth"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/libapi"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/template"
)

type LoginData struct {
	Instruction string
	// Card is non-nil when Instruction is one of the values that maps to a
	// rich contextual sign-in card (vault/library/discover). The template
	// just renders the keys — all routing logic lives here in the handler.
	Card *LoginCard
}

// LoginCard carries i18n keys for the contextual info card on /login. The
// concrete copy is owned by the locales (e.g. vault.signInCard.intro), the
// handler only decides which set of keys applies to the current `from` value.
type LoginCard struct {
	NoteKey     string
	IntroKey    string
	FeatureKeys []string
}

// loginCardFor returns the card descriptor for a given `from` value or nil if
// the `from` doesn't drive a contextual card. Keep the allowed set explicit so
// typos in the URL don't silently render an empty card.
func loginCardFor(from string) *LoginCard {
	switch from {
	case "vault":
		return &LoginCard{
			NoteKey:  "auth.login.vaultNote",
			IntroKey: "vault.signInCard.intro",
			FeatureKeys: []string{
				"vault.signInCard.feature1",
				"vault.signInCard.feature2",
				"vault.signInCard.feature3",
				"vault.signInCard.feature4",
			},
		}
	case "library":
		return &LoginCard{
			NoteKey:  "auth.login.libraryNote",
			IntroKey: "library.signInCard.intro",
			FeatureKeys: []string{
				"library.signInCard.feature1",
				"library.signInCard.feature2",
				"library.signInCard.feature3",
				"library.signInCard.feature4",
			},
		}
	case "discover":
		return &LoginCard{
			NoteKey:  "auth.login.discoverNote",
			IntroKey: "discover.signInCard.intro",
			FeatureKeys: []string{
				"discover.signInCard.feature1",
				"discover.signInCard.feature2",
				"discover.signInCard.feature3",
				"discover.signInCard.feature4",
			},
		}
	case "donate":
		return &LoginCard{
			NoteKey:  "auth.login.donateNote",
			IntroKey: "donate.signInCard.intro",
			FeatureKeys: []string{
				"donate.signInCard.feature1",
				"donate.signInCard.feature2",
				"donate.signInCard.feature3",
			},
		}
	}
	return nil
}

type LogoutData struct{}

type ProcessAuthData struct {
	ReturnURL string
}

type Handler struct {
	tb         template.Builder[*web.Context]
	adminStore *adminauth.Store
	// passwordFormActive is auth.Auth.AdminPasswordActive, bound as a func
	// value so it's the one place login and passwordLogin decide whether the
	// password branch governs the request (see that method's doc for why
	// AdminStore().IsConfigured() alone isn't enough), while still letting
	// tests inject the decision directly without constructing a full
	// *auth.Auth (its fields are unexported outside services/auth).
	passwordFormActive func(*gin.Context) bool
	loginLimiter       *libapi.RateLimiter
}

// PasswordLoginData drives templates/views/auth/password.html. Err carries an
// i18n key rather than a message so the copy stays in the locales.
type PasswordLoginData struct {
	Err string
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], a *auth.Auth) {
	h := &Handler{
		tb: tm.MustRegisterViews("auth/*").WithLayout("main"),
		// Five attempts at once, then one per five seconds. Enough that a
		// mistyped password is never noticed, little enough that guessing is
		// pointless.
		loginLimiter:       libapi.NewRateLimiterWith(0.2, 5),
		adminStore:         a.AdminStore(),
		passwordFormActive: a.AdminPasswordActive,
	}

	r.Use(func(c *gin.Context) {
		u := auth.GetUserFromContext(c)
		if u != nil && u.Expired {
			h.refresh(c)
			c.Abort()
			return
		}
	})

	r.GET("/login", h.login)
	r.POST("/login", h.passwordLogin)
	r.GET("/refresh", h.refresh)
	r.GET("/logout", h.logout)
	r.GET("/auth/verify", h.verify)
	r.GET("/auth/callback/google", h.callback)
	r.GET("/auth/callback/patreon", h.callback)
}

func (s *Handler) refresh(c *gin.Context) {
	s.tb.Build("auth/refresh").HTML(http.StatusOK, web.NewContext(c))
}

// renderPassword renders auth/password with the given status. It guards
// against a nil tb because Handler values built directly by tests (see
// password_login_test.go) don't wire a template.Builder — that needs the
// full app's multitemplate renderer and i18n helpers, none of which a fast
// handler-logic test should have to stand up. RegisterHandler always sets
// tb, so production never takes the fallback branch.
func (s *Handler) renderPassword(c *gin.Context, code int, data PasswordLoginData) {
	if s.tb == nil {
		c.Status(code)
		return
	}
	s.tb.Build("auth/password").HTML(code, web.NewContext(c).WithData(data))
}

// loginView picks which view GET /login renders and its data. Split out from
// login so a test can check the decision (view name) without needing a
// working template.Builder to render through — see renderPassword's doc for
// why that matters.
func (s *Handler) loginView(c *gin.Context) (view string, data any) {
	if s.passwordFormActive != nil && s.passwordFormActive(c) {
		return "auth/password", PasswordLoginData{}
	}
	instruction := "default"
	if c.Query("from") != "" {
		instruction = c.Query("from")
	}
	return "auth/login", LoginData{
		Instruction: instruction,
		Card:        loginCardFor(instruction),
	}
}

func (s *Handler) login(c *gin.Context) {
	if c.Query("return-url") != "" {
		session := sessions.Default(c)
		session.Set("return-url", c.Query("return-url"))
		_ = session.Save()
	}
	view, data := s.loginView(c)
	if s.tb == nil {
		c.Status(http.StatusOK)
		return
	}
	s.tb.Build(view).HTML(http.StatusOK, web.NewContext(c).WithData(data))
}

// loginLimiterKey keys the login rate limiter on the direct TCP peer
// (RemoteAddr) rather than c.ClientIP(). ClientIP() trusts X-Forwarded-For,
// and nothing in this application calls gin's SetTrustedProxies — so it
// trusts every proxy and returns the leftmost XFF value, which an attacker
// controls outright. The self-hosted nginx makes this worse, not better: its
// upstream directive is $proxy_add_x_forwarded_for, which *appends* to
// whatever the client already sent instead of replacing it, so a script can
// hand itself a fresh bucket on every single request just by varying that
// header. RemoteAddr can't be spoofed that way — it's the actual socket peer,
// which behind nginx is always 127.0.0.1. That collapses every login attempt
// on this instance into one shared bucket, which is the right brake here, not
// a compromise: a self-hosted instance has exactly one administrator, so
// there is no "other tenant" a global cap could unfairly punish. It slows a
// remote attacker to one attempt per five seconds and never locks the real
// administrator out, since there's nothing IP-specific to lock.
//
// Do not change this to ClientIP(), and do not add SetTrustedProxies to make
// ClientIP() safe here — that would change ClientIP() app-wide (geoip, the
// API rate limiter) with production consequences well beyond this form.
func loginLimiterKey(c *gin.Context) string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return host
}

// passwordLogin verifies the single administrator password. A failure returns
// 401 with the form re-rendered — never a redirect, so a script cannot tell
// success from failure by following one.
func (s *Handler) passwordLogin(c *gin.Context) {
	if s.adminStore == nil || s.passwordFormActive == nil || !s.passwordFormActive(c) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if s.loginLimiter != nil {
		if retryAfter, ok := s.loginLimiter.Take(loginLimiterKey(c)); !ok {
			c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			s.renderPassword(c, http.StatusTooManyRequests, PasswordLoginData{Err: "auth.password.tooManyAttempts"})
			return
		}
	}
	if !s.adminStore.Verify(c.Request.Context(), c.PostForm("password")) {
		s.renderPassword(c, http.StatusUnauthorized, PasswordLoginData{Err: "auth.password.wrong"})
		return
	}
	session := sessions.Default(c)
	session.Set(auth.AdminSessionKey, true)
	// Task 5 sends unauthenticated navigations to /login?return-url=<path>,
	// and the GET handler above stashes that value in the session. Send a
	// successful login back where the visitor was headed, then clear it —
	// the same lifecycle processAuth uses for the OAuth/magic-link paths.
	returnURL := "/"
	if v, ok := session.Get("return-url").(string); ok && v != "" {
		returnURL = v
		session.Delete("return-url")
	}
	if err := session.Save(); err != nil {
		s.renderPassword(c, http.StatusInternalServerError, PasswordLoginData{Err: "auth.password.sessionFailed"})
		return
	}
	c.Redirect(http.StatusFound, returnURL)
}

func (s *Handler) logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Delete(auth.AdminSessionKey)
	_ = session.Save()
	s.tb.Build("auth/logout").HTML(http.StatusOK, web.NewContext(c).WithData(LogoutData{}))
}

func (s *Handler) verify(c *gin.Context) {
	s.processAuth(c, "auth/verify")
}

func (s *Handler) callback(c *gin.Context) {
	s.processAuth(c, "auth/callback")
}

func (s *Handler) processAuth(c *gin.Context, t string) {
	session := sessions.Default(c)
	var returnURL string
	if session.Get("return-url") != nil {
		returnURL = session.Get("return-url").(string)
		session.Delete("return-url")
		_ = session.Save()
	}
	s.tb.Build(t).HTML(http.StatusOK, web.NewContext(c).WithData(&ProcessAuthData{
		ReturnURL: returnURL,
	}))
}
