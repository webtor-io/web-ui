package j

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/jobs/scripts"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"
)

// The magnet card has two faces: a dead magnet offers the ten-minute retry
// (and says so differently after that retry failed too); a broken link
// offers nothing to retry. Every locale must resolve every key.
func TestMagnetErrorCardRenders(t *testing.T) {
	render := magnetCard(t)
	dead := render("en", &MagnetErrorData{Kind: "dead", WaitedSec: 60, Magnet: "magnet:?xt=urn:btih:00"})
	for _, w := range []string{"Nobody is sharing", "for a minute", "60 s", `name="magnet-wait" value="long"`, `data-async-target="#log-load"`, "magnet-retry-long", "Try again for 10 minutes", `kind: 'dead'`} {
		if !strings.Contains(dead, w) {
			t.Errorf("dead: missing %q", w)
		}
	}
	long := render("en", &MagnetErrorData{Kind: "dead", WaitedSec: 600, Long: true, Magnet: "magnet:?xt=urn:btih:00"})
	if !strings.Contains(long, "Ten minutes") || !strings.Contains(long, "magnet-retry-long") {
		t.Error("after the long attempt the card must say so and still offer a retry")
	}
	invalid := render("en", &MagnetErrorData{Kind: "invalid"})
	if !strings.Contains(invalid, "magnet link is broken") || strings.Contains(invalid, "magnet-retry-long") || strings.Contains(invalid, "Searched for") {
		t.Errorf("invalid: must explain the link and offer no retry:\n%s", invalid)
	}
	// A private torrent: a magnet without a tracker address resolved to one
	// rest-api has not stored, and it stored nothing. Its own words -- the
	// link has no tracker address -- and both ways out: the .torrent, uploaded
	// into the same log host the retry targets, or a magnet that carries the
	// tracker. None of the dead card: no "searched for", no ten-minute retry,
	// nothing to sell, and its own Umami event so the dead/invalid series
	// stays what it was.
	private := render("en", &MagnetErrorData{Kind: "private", WaitedSec: 12, Magnet: "magnet:?xt=urn:btih:00", HintVideoID: "tt0000000"})
	for _, w := range []string{"private torrent", "no tracker address", "magnet link that includes your tracker", ".torrent file", `type="file"`, `name="resource"`, `accept=".torrent,application/x-bittorrent"`,
		`data-async-target="#log-load"`, `enctype="multipart/form-data"`, `name="hint_video_id" value="tt0000000"`, "this.form.requestSubmit()",
		// Umami gets the event names alone: no data argument that could
		// carry the link (the page URL itself is cut in lib/umami.js).
		"umami.track('magnet-private-upload')", "umami.track('magnet-private-shown')"} {
		if !strings.Contains(private, w) {
			t.Errorf("private: missing %q", w)
		}
	}
	// "use", not "paste": the card also answers Discover, where nothing was
	// pasted.
	for _, w := range []string{"magnet-retry-long", "magnet-wait", "Searched for", "magnet-failed-shown", "Nobody is sharing", "Vault", "donate", `name="resource" value=`, "urn:btih", "accepted", "passkey", "paste"} {
		if strings.Contains(private, w) {
			t.Errorf("private: must not carry %q:\n%s", w, private)
		}
	}
	paste := map[string]string{"ru": "вставьте", "es": "pega ", "de": "füge", "fr": "collez", "pt": "cole ", "it": "incolla", "pl": "wklej", "tr": "yapıştır", "nl": "plak", "cs": "vlož", "zh": "粘贴"}
	for _, lang := range []string{"ru", "es", "de", "fr", "pt", "it", "pl", "tr", "nl", "cs", "zh"} {
		for _, kind := range []string{"dead", "private"} {
			out := render(lang, &MagnetErrorData{Kind: kind, WaitedSec: 60, Magnet: "m"})
			if strings.Contains(out, "load.magnet.") {
				t.Errorf("%s %s: unresolved key in %s", lang, kind, out)
			}
			if kind == "private" && strings.Contains(out, paste[lang]) {
				t.Errorf("%s private: says %q, not use", lang, paste[lang])
			}
		}
	}
}

// magnetCard renders templates/views/load/errors/magnet.html in a language.
func magnetCard(t *testing.T) func(lang string, d *MagnetErrorData) string {
	t.Helper()
	locales, err := os.OpenRoot("../locales")
	if err != nil {
		t.Fatalf("locales: %v", err)
	}
	t.Cleanup(func() { _ = locales.Close() })
	helper := i18n.NewHelper(i18n.New(locales.FS()))
	tpl, err := template.New("magnet.html").Funcs(template.FuncMap{
		"t":        helper.T,
		"tp":       helper.Tp,
		"tn":       helper.Tn,
		"langPath": func(lang, p string) string { return p },
	}).ParseFiles("../templates/views/load/errors/magnet.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	type ctx struct {
		Lang string
		Data *MagnetErrorData
	}
	return func(lang string, d *MagnetErrorData) string {
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "main", &ctx{Lang: lang, Data: d}); err != nil {
			t.Fatalf("render %s: %v", lang, err)
		}
		return buf.String()
	}
}

// The dead-magnet card is stored with the load job and replayed to whoever
// loads the same torrent in the same language, through a log endpoint that
// needs no account (the job id is sha1(infohash/lang)), and the job logger
// logged it. A magnet's tr= can carry a private tracker's passkey, so the card
// knows the infohash only: its retry takes the query this browser submitted
// from the log host (partials/load/progress.html, progressLog.js), and falls
// back to the infohash alone.
func TestMagnetErrorCardKeepsNoTrackers(t *testing.T) {
	const (
		hash    = "08ada5a7a6183aae1e09d831df6748d566095a10"
		passkey = "0123456789abcdef0123456789abcdef"
	)
	q := "magnet:?xt=urn:btih:" + hash + "&dn=Sintel&tr=http%3A%2F%2Ftracker.invalid%2Fannounce%3Fpasskey%3D" + passkey
	d := newMagnetErrorData(&scripts.LoadArgs{Query: q, HintVideoID: "tt0000001"},
		&scripts.MagnetError{Kind: "dead", Waited: time.Minute}, hash)
	if d.Magnet != "magnet:?xt=urn:btih:"+hash {
		t.Errorf("Magnet = %q, want the infohash alone", d.Magnet)
	}
	out := magnetCard(t)("en", d)
	if strings.Contains(out, passkey) || strings.Contains(out, "tracker.invalid") || strings.Contains(out, "Sintel") {
		t.Errorf("the card carries the query:\n%s", out)
	}
	for _, w := range []string{
		`<input type="hidden" name="resource" value="magnet:?xt=urn:btih:` + hash + `" data-load-query />`,
		`name="hint_video_id" value="tt0000001"`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
}

// The host the card's retry reads the query from: rendered for this browser
// alone (the answer to its own submit), never stored. An attribute, not a
// hidden input -- the div-host redirect turns the host's hidden inputs into
// query params of the resource page (progressLog.js), and the query must not
// ride there.
func TestLoadProgressPartialCarriesTheQuery(t *testing.T) {
	tpl := progressPartial(t)
	type args struct{ Query string }
	type postData struct {
		Job  interface{}
		Args *args
	}
	render := func(d postData) string {
		ctx := struct {
			Lang string
			Data postData
		}{Lang: "en", Data: d}
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "load/progress", &ctx); err != nil {
			t.Fatalf("render: %v", err)
		}
		return buf.String()
	}
	const q = "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10&tr=udp%3A%2F%2Ftracker.invalid%3A6969"
	out := render(postData{Job: struct{}{}, Args: &args{Query: q}})
	host := out[strings.Index(out, "<div"):]
	host = host[:strings.Index(host, ">")]
	if want := `data-load-query="magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10&amp;tr=udp%3A%2F%2Ftracker.invalid%3A6969"`; !strings.Contains(host, want) {
		t.Errorf("the host tag %q lacks %q", host, want)
	}
	if strings.Contains(out, `name="resource"`) {
		t.Errorf("the query must not be a hidden input of the host:\n%s", out)
	}
	for name, d := range map[string]postData{
		"no args": {Job: struct{}{}},
		"a file":  {Job: struct{}{}, Args: &args{}},
	} {
		if out := render(d); strings.Contains(out, "data-load-query") {
			t.Errorf("%s: a host without a query carries one:\n%s", name, out)
		}
	}
}

// The log host partial is called with the root context ($) — a view's dot is
// its Data, and "$.Lang" inside the partial once resolved against PostData
// and failed at request time. Render it the way index.html and the retry
// layout do.
func TestLoadProgressPartialRenders(t *testing.T) {
	tpl := progressPartial(t)
	type postData struct{ Job interface{} }
	ctx := struct {
		Lang string
		Data postData
	}{Lang: "ru", Data: postData{Job: struct{}{}}}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "load/progress", &ctx); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, w := range []string{`data-async-progress-log="/ru/queue/load/job/x/log"`, "home.gotIt", "load.js", `class="log-target"`, `class="job-stage"`, "action.showSteps"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
	if strings.Contains(out, "<form") {
		t.Error("the host must be a div: cards inside carry their own forms")
	}
	// Data without a Tool field (anything but PostData and index.Data): no
	// input, and no template error either — hence the `has` guard.
	if strings.Contains(out, `name="tool"`) {
		t.Errorf("a host rendered without a tool must not carry one:\n%s", out)
	}
}

// The tool page a submit came from rides to the resource page as a hidden
// input of the log host: progressLog.js's div-host redirect turns the host's
// hidden inputs into query params (/<hash>?tool=<slug>). The slug cannot go
// into the job's own redirect — the log is shared by everyone who loads the
// same torrent in the same language.
func TestLoadProgressPartialCarriesTheTool(t *testing.T) {
	tpl := progressPartial(t)
	type postData struct {
		Job  interface{}
		Tool *common.Tool
	}
	render := func(d postData) string {
		ctx := struct {
			Lang string
			Data postData
		}{Lang: "en", Data: d}
		var buf bytes.Buffer
		if err := tpl.ExecuteTemplate(&buf, "load/progress", &ctx); err != nil {
			t.Fatalf("render: %v", err)
		}
		return buf.String()
	}
	out := render(postData{Job: struct{}{}, Tool: common.ToolByURL("magnet-to-torrent")})
	host := strings.Index(out, "data-async-progress-log=")
	input := strings.Index(out, `<input type="hidden" name="tool" value="magnet-to-torrent" />`)
	closeHost := strings.Index(out, "alert-close-wrapper")
	if input < 0 {
		t.Fatalf("the host does not carry the tool:\n%s", out)
	}
	// Inside the host div, which is what progressLog.js reads
	// (this.el.querySelectorAll('input[type=hidden]')).
	if !(host < input && input < closeHost) {
		t.Errorf("the tool input must sit inside the log host:\n%s", out)
	}
	if home := render(postData{Job: struct{}{}}); strings.Contains(home, `name="tool"`) {
		t.Errorf("the home page's submit must not carry a tool:\n%s", home)
	}
}

func progressPartial(t *testing.T) *template.Template {
	t.Helper()
	tpl, err := template.New("progress.html").Funcs(template.FuncMap{
		"t":             func(lang, key string, args ...interface{}) string { return key },
		"makeJobLogURL": func(lang string, j interface{}) string { return "/" + lang + "/queue/load/job/x/log" },
		"asset":         func(p string) template.HTML { return template.HTML("<script src=\"" + p + "\"></script>") },
		"has":           (&web.Helper{}).Has,
	}).ParseFiles("../templates/partials/load/progress.html", "../templates/partials/job_log.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tpl
}

// Each face of the card is counted under its own err_key in "user error
// shown": a private torrent must not swell the dead-magnet count.
func TestMagnetErrKey(t *testing.T) {
	for kind, want := range map[string]string{
		"dead":    "error.magnet_no_metadata",
		"invalid": "error.magnet_invalid",
		"private": "error.private_magnet",
	} {
		if got := magnetErrKey(&scripts.MagnetError{Kind: kind}); got != want {
			t.Errorf("%s: %s, want %s", kind, got, want)
		}
	}
}
