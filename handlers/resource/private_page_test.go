package resource

import (
	"encoding/json"
	"flag"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/models/pgtest"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/vault"
	w "github.com/webtor-io/web-ui/services/web"
)

const privHash = "0123456789abcdef0123456789abcdef01234567"

// privateRestAPI answers /resource/<privHash> and its .torrent with whatever
// the test last set.
func privateRestAPI(t *testing.T) (*httptest.Server, func(status int, body string)) {
	t.Helper()
	var mu sync.Mutex
	status, body := http.StatusNotFound, `{"error":"not found"}`
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/resource/"+privHash) {
			t.Errorf("unexpected rest-api call %s", r.URL.Path)
			rw.WriteHeader(http.StatusTeapot)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(status)
		_, _ = rw.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, func(s int, b string) {
		mu.Lock()
		defer mu.Unlock()
		status, body = s, b
	}
}

func getPage(r http.Handler, accept string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/"+privHash, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// rest-api's 403 for a private torrent is its own page: 403, kept by no cache,
// indexed by no one, and without the torrent's name -- rest-api did not give
// it. The page of whoever may play it carries the same two headers: it has
// the name. A 403 without a code (a ban) and a coded 503 keep the redirect.
func TestPrivateRefusalPage(t *testing.T) {
	restAPI, answer := privateRestAPI(t)
	anon := newResourceRouter(t, restAPI)
	user := &models.User{UserID: uuid.NewV4()}

	refused := func(t *testing.T, rec *httptest.ResponseRecorder) string {
		t.Helper()
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d, want 403 (Location %q)", rec.Code, rec.Header().Get("Location"))
		}
		if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Errorf("Cache-Control %q, want private, no-store", got)
		}
		if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
			t.Errorf("X-Robots-Tag %q, want noindex", got)
		}
		return rec.Body.String()
	}

	t.Run("anonymous", func(t *testing.T) {
		answer(http.StatusForbidden, `{"error":"private torrent: only a signed-in user who added it with their own tracker can play it","code":"private_torrent","reason":"sign_in"}`)
		body := refused(t, getPage(anon, ""))
		if want := "[private][key=error.private_sign_in][signin=true][upload=false][vault=false][target=main]"; body != want {
			t.Errorf("body %q, want %q", body, want)
		}
	})

	t.Run("the server takes no private torrents", func(t *testing.T) {
		answer(http.StatusForbidden, `{"error":"x","code":"private_unavailable"}`)
		if body := refused(t, getPage(anon, "")); !strings.Contains(body, "[key=error.private.unavailable][signin=false][upload=false]") {
			t.Errorf("body %q", body)
		}
	})

	t.Run("JSON", func(t *testing.T) {
		answer(http.StatusForbidden, `{"error":"x","code":"private_torrent","reason":"sign_in"}`)
		rec := getPage(anon, "application/json")
		refused(t, rec)
		var got map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("body %q: %v", rec.Body.String(), err)
		}
		if got["status"] != "error" || got["message"] != "error.private_sign_in" || len(got) != 2 {
			t.Errorf("body %v, want status=error message=error.private_sign_in", got)
		}
	})

	t.Run("signed in without a record", func(t *testing.T) {
		answer(http.StatusForbidden, `{"error":"x","code":"private_torrent","reason":"no_record"}`)
		// A Vault whose database does not answer: the page still renders,
		// without the line about the kept copy.
		v := vault.New(cli.NewContext(cli.NewApp(), flag.NewFlagSet("v", flag.ContinueOnError), nil), &vault.Api{}, nil, nil, &cs.PG{}, nil)
		r := newResourceRouterAs(t, restAPI, v, user)
		if body, want := refused(t, getPage(r, "")), "[private][key=error.private_no_record][signin=false][upload=true][vault=false][target=main]"; body != want {
			t.Errorf("body %q, want %q", body, want)
		}
	})

	t.Run("signed in without a record, kept in Vault", func(t *testing.T) {
		db := pgtest.Start(t)
		// Pledged, not stored yet: nothing is kept.
		res := &vaultModels.Resource{ResourceID: privHash, RequiredVP: 1, Name: "probe"}
		if _, err := db.Model(res).Insert(); err != nil {
			t.Fatal(err)
		}
		answer(http.StatusForbidden, `{"error":"x","code":"private_torrent","reason":"no_record"}`)
		v := vault.New(cli.NewContext(cli.NewApp(), flag.NewFlagSet("v", flag.ContinueOnError), nil), &vault.Api{}, nil, nil, pgtest.CS(t, db), nil)
		r := newResourceRouterAs(t, restAPI, v, user)
		if body := refused(t, getPage(r, "")); !strings.Contains(body, "[upload=true][vault=false]") {
			t.Errorf("not vaulted yet: body %q", body)
		}
		res.Vaulted = true
		if _, err := db.Model(res).WherePK().Column("vaulted").Update(); err != nil {
			t.Fatal(err)
		}
		if body := refused(t, getPage(r, "")); !strings.Contains(body, "[upload=true][vault=true]") {
			t.Errorf("vaulted: body %q, want the kept copy", body)
		}
	})

	t.Run("the page of whoever may play it", func(t *testing.T) {
		// rest-api decided the right; the page reads only Private. Anonymous
		// here because this router has no database for a signed-in page.
		answer(http.StatusOK, `{"id":"`+privHash+`","name":"probe","private":true}`)
		rec := getPage(anon, "")
		if rec.Code != http.StatusOK || rec.Body.String() != "[resource][name=probe]" {
			t.Fatalf("status %d body %q, want the resource page", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Errorf("Cache-Control %q, want private, no-store", got)
		}
		if got := rec.Header().Get("X-Robots-Tag"); got != "noindex" {
			t.Errorf("X-Robots-Tag %q, want noindex", got)
		}
		answer(http.StatusOK, `{"id":"`+privHash+`","name":"probe","private":false}`)
		rec = getPage(anon, "")
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "" || rec.Header().Get("X-Robots-Tag") != "noindex, follow" {
			t.Errorf("a public page: status %d Cache-Control %q X-Robots-Tag %q, want it as before",
				rec.Code, rec.Header().Get("Cache-Control"), rec.Header().Get("X-Robots-Tag"))
		}
	})

	for _, c := range []struct {
		name, body string
		status     int
		key        string
	}{
		{"a ban", `{"error":"forbidden: restricted by the rightholder"}`, http.StatusForbidden, "error.forbidden"},
		{"a coded 503", `{"error":"x","code":"private_verify_unavailable"}`, http.StatusServiceUnavailable, "error.private_verify_unavailable"},
	} {
		t.Run(c.name, func(t *testing.T) {
			answer(c.status, c.body)
			rec := getPage(anon, "")
			if rec.Code != http.StatusFound {
				t.Fatalf("status %d body %q, want the old redirect", rec.Code, rec.Body.String())
			}
			loc, _ := url.Parse(rec.Header().Get("Location"))
			if loc.Path != "/" || loc.Query().Get("err") != c.key {
				t.Errorf("Location %q, want /?err=%s", loc, c.key)
			}
		})
	}
}

// The partial the page and the load card share, as it renders: the text of
// the refusal, the way out, one Umami event with the code and the reason.
func TestPrivateRefusalPartialRenders(t *testing.T) {
	funcs := intentFuncs(t)
	tpl, err := template.New("private.html").Funcs(funcs).ParseFiles(
		"../../templates/views/resource/private.html",
		"../../templates/partials/private/refusal.html",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tr := funcs["t"].(func(string, string) string)
	render := func(lang string, r *w.PrivateRefusal) string {
		return execute(t, tpl, "main", &w.Context{Lang: lang, Path: "/" + privHash, Data: r})
	}
	signIn := w.NewPrivateRefusal(&api.PrivateError{Status: 403, Code: api.CodePrivateTorrent, Reason: api.ReasonSignIn}, false)
	signIn.Target = "main"
	upload := w.NewPrivateRefusal(&api.PrivateError{Status: 403, Code: api.CodePrivateTorrent, Reason: api.ReasonNoRecord}, true)
	upload.Target, upload.VaultKept = "#log-load", true

	for _, lang := range []string{"en", "ru"} {
		out := render(lang, signIn)
		for _, want := range []string{
			template.HTMLEscapeString(tr(lang, "private.page.title")),
			template.HTMLEscapeString(tr(lang, "error.private_sign_in")),
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing %q in\n%s", lang, want, out)
			}
		}
		if strings.Contains(out, "error.private") || strings.Contains(out, "private.page") {
			t.Errorf("%s: a key reached the page untranslated:\n%s", lang, out)
		}
	}

	out := render("en", signIn)
	if !strings.Contains(out, `/login?return-url=%2f`+privHash) {
		t.Errorf("no sign-in link back to the page:\n%s", out)
	}
	for _, banned := range []string{`enctype="multipart/form-data"`, tr("en", "private.page.vaultKept")} {
		if strings.Contains(out, banned) {
			t.Errorf("anonymous: %q rendered:\n%s", banned, out)
		}
	}

	out = render("en", upload)
	for _, want := range []string{
		`enctype="multipart/form-data"`, `data-async-target="#log-load"`, `accept=".torrent`,
		"private-reupload-upload", template.HTMLEscapeString(tr("en", "private.page.vaultKept")),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("signed in without a record: missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "/login") {
		t.Errorf("signed in: a sign-in link rendered:\n%s", out)
	}

	for _, r := range []*w.PrivateRefusal{signIn, upload} {
		out := render("en", r)
		if want := "track('private-refused-shown', {code: '" + r.Code + "', reason: '" + r.Reason + "'})"; !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
		for _, gone := range []string{"private-signin-shown", "private-reupload-shown"} {
			if strings.Contains(out, gone) {
				t.Errorf("%s fired: S29 has one event for every refusal", gone)
			}
		}
	}
	if title := strings.TrimSpace(execute(t, tpl, "title", &w.Context{Lang: "en", Data: signIn})); title != tr("en", "private.page.title") {
		t.Errorf("title %q", title)
	}
}

// GET /<hash>.torrent (Р12): a coded refusal is 403, a refusal without a code
// (a ban) and a torrent rest-api does not have are 404; never a 500.
func TestDownloadTorrentPrivate(t *testing.T) {
	restAPI, answer := privateRestAPI(t)
	r := newResourceRouter(t, restAPI)
	token, err := SignTorrentFileToken("test-secret", privHash, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, body   string
		status, want int
	}{
		{"private", `{"error":"x","code":"private_torrent_file"}`, http.StatusForbidden, http.StatusForbidden},
		{"a ban", `{"error":"forbidden: restricted by the rightholder"}`, http.StatusForbidden, http.StatusNotFound},
		{"not stored", `{"error":"not found"}`, http.StatusNotFound, http.StatusNotFound},
	} {
		answer(c.status, c.body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+privHash+".torrent?token="+url.QueryEscape(token), nil))
		if rec.Code != c.want || rec.Body.String() != "not available" {
			t.Errorf("%s: %d %q, want %d \"not available\"", c.name, rec.Code, rec.Body.String(), c.want)
		}
	}
}
