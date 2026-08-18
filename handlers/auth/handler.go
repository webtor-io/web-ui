package auth

import (
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
	tb           template.Builder[*web.Context]
	adminStore   *adminauth.Store
	loginLimiter *libapi.RateLimiter
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
		loginLimiter: libapi.NewRateLimiterWith(0.2, 5),
		adminStore:   a.AdminStore(),
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

func (s *Handler) login(c *gin.Context) {
	if c.Query("return-url") != "" {
		session := sessions.Default(c)
		session.Set("return-url", c.Query("return-url"))
		_ = session.Save()
	}
	if s.adminStore != nil && s.adminStore.IsConfigured(c.Request.Context()) {
		s.renderPassword(c, http.StatusOK, PasswordLoginData{})
		return
	}
	instruction := "default"
	if c.Query("from") != "" {
		instruction = c.Query("from")
	}
	ld := LoginData{
		Instruction: instruction,
		Card:        loginCardFor(instruction),
	}
	s.tb.Build("auth/login").HTML(http.StatusOK, web.NewContext(c).WithData(ld))
}

// passwordLogin verifies the single administrator password. A failure returns
// 401 with the form re-rendered — never a redirect, so a script cannot tell
// success from failure by following one.
func (s *Handler) passwordLogin(c *gin.Context) {
	if s.adminStore == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if s.loginLimiter != nil {
		if retryAfter, ok := s.loginLimiter.Take(c.ClientIP()); !ok {
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
