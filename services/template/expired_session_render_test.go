package template

import (
	"bytes"
	"html/template"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
)

// Nothing else renders layouts/main.html or partials/nav.html, and both run
// on every page: a broken block there takes the whole site down at startup.
// These templates are parsed with every function they call stubbed, the
// interstitial is rendered for real (with the locales), and so is the
// navbar a visitor sees on it.

var (
	tplAction  = regexp.MustCompile(`(?s)\{\{(.*?)\}\}`)
	tplComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	tplString  = regexp.MustCompile("\"(?:[^\"\\\\]|\\\\.)*\"|`[^`]*`")
	tplIdent   = regexp.MustCompile(`(?:^|[^.$\w])([a-zA-Z]\w*)`)
)

// Keywords and the builtin functions: never stubbed.
var tplKeywords = map[string]bool{
	"if": true, "else": true, "end": true, "range": true, "with": true, "define": true,
	"template": true, "block": true, "break": true, "continue": true, "nil": true,
	"true": true, "false": true, "and": true, "or": true, "not": true, "eq": true,
	"ne": true, "lt": true, "le": true, "gt": true, "ge": true, "len": true,
	"index": true, "print": true, "printf": true, "println": true, "html": true,
	"js": true, "urlquery": true, "call": true, "slice": true,
}

// stubFuncs defines, as a no-op, every function the files call that funcs
// does not already provide, so they can be parsed without the app's helpers.
func stubFuncs(t *testing.T, funcs template.FuncMap, files ...string) template.FuncMap {
	t.Helper()
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := tplComment.ReplaceAllString(string(b), "")
		for _, a := range tplAction.FindAllStringSubmatch(src, -1) {
			for _, m := range tplIdent.FindAllStringSubmatch(tplString.ReplaceAllString(a[1], `""`), -1) {
				if name := m[1]; !tplKeywords[name] && funcs[name] == nil {
					funcs[name] = func(...any) any { return nil }
				}
			}
		}
	}
	return funcs
}

func i18nT(t *testing.T) func(string, string) template.HTML {
	t.Helper()
	locales, err := os.OpenRoot("../../locales")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locales.Close() })
	h := i18n.NewHelper(i18n.New(locales.FS()))
	return func(lang, key string) template.HTML { return template.HTML(h.T(lang, key)) }
}

func TestExpiredSessionTemplatesParse(t *testing.T) {
	files := []string{
		"../../templates/layouts/main.html",
		"../../templates/partials/nav.html",
		"../../templates/views/auth/refresh.html",
		"../../templates/views/auth/login.html",
	}
	for _, f := range files {
		if _, err := template.New("x").Funcs(stubFuncs(t, template.FuncMap{}, f)).ParseFiles(f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func TestRefreshInterstitialRenders(t *testing.T) {
	f := "../../templates/views/auth/refresh.html"
	tpl, err := template.New("refresh").Funcs(stubFuncs(t, template.FuncMap{"t": i18nT(t)}, f)).ParseFiles(f)
	if err != nil {
		t.Fatal(err)
	}
	for lang, want := range map[string][]string{
		"en": {"Signing you in", "This is taking longer than usual.", "Your account is fine", "needs JavaScript"},
		"ru": {"Входим в аккаунт", "Это занимает дольше обычного.", "С аккаунтом всё в порядке", "нужен JavaScript"},
	} {
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "main", map[string]any{"Lang": lang}); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		out := buf.String()
		if strings.Contains(out, "auth.refresh.") || strings.Contains(out, "<no value>") {
			t.Errorf("%s: a key or value did not resolve:\n%s", lang, out)
		}
		for _, w := range append(want, `id="auth-refresh"`, `id="auth-refresh-failed"`, "<noscript>", "location.reload()") {
			if !strings.Contains(out, w) {
				t.Errorf("%s: output is missing %q", lang, w)
			}
		}
		if strings.Contains(strings.ToLower(out), "token") {
			t.Errorf("%s: the word token reached the visitor:\n%s", lang, out)
		}
	}
}

// The interstitial must never show a signed-in visitor the anonymous
// controls: "Login" and "Donate" told paying users they had been signed out.
func TestNavOnExpiredSessionShowsNoAnonymousControls(t *testing.T) {
	f := "../../templates/partials/nav.html"
	funcs := stubFuncs(t, template.FuncMap{
		"t":       i18nT(t),
		"hasAuth": func(u *auth.User) bool { return u.HasAuth() },
	}, f)
	tpl, err := template.New("nav").Funcs(funcs).ParseFiles(f)
	if err != nil {
		t.Fatal(err)
	}
	render := func(ctx map[string]any) string {
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "nav", ctx); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	out := render(map[string]any{"Lang": "en", "User": &auth.User{Expired: true}, "SessionRefresh": true})
	if !strings.Contains(out, "skeleton") {
		t.Errorf("no placeholder where the account controls go:\n%s", out)
	}
	for _, bad := range []string{"/login", "/donate", ">Login<", "Donate"} {
		if strings.Contains(out, bad) {
			t.Errorf("expired-session navbar contains %q:\n%s", bad, out)
		}
	}

	// /login and the sign-in callbacks render with an expired user too, and
	// they are real pages: the placeholder there never went away.
	out = render(map[string]any{"Lang": "en", "User": &auth.User{Expired: true}})
	if strings.Contains(out, "skeleton") || !strings.Contains(out, "Login") {
		t.Errorf("a real page with an expired user got the interstitial's navbar:\n%s", out)
	}
}
