package resource

import (
	"html/template"
	"strings"
	"testing"

	uuid "github.com/satori/go.uuid"
	proto "github.com/webtor-io/claims-provider/proto"

	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/handlers/index"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/job"
	w "github.com/webtor-io/web-ui/services/web"
)

// dropzoneCtx renders the home page's "main" with uploads on or off: the real
// view, the job log and the refusal card, the rest stubbed.
func dropzoneCtx(t *testing.T, ctx *w.Context, enabled bool) string {
	t.Helper()
	funcs := intentFuncs(t)
	funcs["uploadsEnabled"] = func() bool { return enabled }
	// A script a template brings is visible, not dropped.
	funcs["asset"] = func(n string) template.HTML { return template.HTML("[asset " + n + "]") }
	tpl, err := template.New("index.html").Funcs(funcs).ParseFiles(
		"../../templates/views/index.html",
		"../../templates/partials/load/progress.html",
		"../../templates/partials/job_log.html",
		"../../templates/partials/about/heading.html",
		"../../templates/partials/private/refusal.html",
		"../../templates/partials/upload/log.html",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stubTemplates(t, tpl, "hero_backdrop", "hero_wave", "onboarding_checklist", "continue_watching", "discover", "promo", "about")
	return execute(t, tpl, "main", ctx)
}

// dropzone renders the page for user at path, with Vault set up.
func dropzone(t *testing.T, user *auth.User, data any, path string, enabled bool) string {
	t.Helper()
	return dropzoneCtx(t, &w.Context{Lang: "en", Path: path, User: user, Data: data, Vault: true}, enabled)
}

// zone is the drop zone's form, from its opening tag to its </form>.
func zone(t *testing.T, main string) string {
	t.Helper()
	i := strings.Index(main, `class="dropzone"`)
	if i < 0 {
		t.Fatalf("no drop zone:\n%s", main)
	}
	i = strings.LastIndex(main[:i], "<form")
	return main[i : i+strings.Index(main[i:], "</form>")+len("</form>")]
}

// The drop zone takes a guest's own files only to show the sign-in prompt,
// and that prompt is declared inside a <template>: its impression goes out
// when it is shown, not when the page loads. It shows where a signed-in
// user's upload shows its progress, above the search field: one line and a
// button, like "My subtitles", not the refusal card with a lock that someone
// opening another account's files gets.
func TestDropzoneUploads(t *testing.T) {
	const (
		tplOpen   = `<template id="upload-sign-in">`
		busy      = `data-upload-busy="Wait until your files finish uploading"`
		form      = `class="dropzone"`
		folder    = `Upload a folder`
		dropText  = `<span class="text-w-pinkL font-semibold">Drop a .torrent or your own files</span> — or click to choose.`
		quotaHint = `Space from deleted files and folders is freed within 20 minutes.`
	)
	guest := &auth.User{}
	signedIn := &auth.User{ID: uuid.NewV4()}

	t.Run("guest", func(t *testing.T) {
		main := dropzone(t, guest, &index.Data{}, "/", true)
		i := strings.Index(main, tplOpen)
		if i < 0 {
			t.Fatalf("no sign-in template:\n%s", main)
		}
		j := i + strings.Index(main[i:], "</template>")
		card := main[i:j]
		for _, want := range []string{
			`data-umami-impression-code="upload"`,
			`data-umami-impression-reason="sign_in"`,
			`/login?return-url=%2f"`,
			`>Sign in to upload your own files — only you will see them.</p>`,
			`>Sign in</a>`,
		} {
			if !strings.Contains(card, want) {
				t.Errorf("the card lacks %q:\n%s", want, card)
			}
		}
		// No lock, no "Private files" heading: those are the refusal's. No
		// script either: drop.js counts the prompt when it shows it, and a
		// script in the clone would run and count it again.
		for _, s := range []string{"Private ", "<svg", "<h3", "[asset "} {
			if strings.Contains(card, s) {
				t.Errorf("the card has %q:\n%s", s, card)
			}
		}
		slot := strings.Index(main, `class="upload-sign-in-slot`)
		if slot < 0 || slot > i || slot > strings.Index(main, `class="join w-full"`) {
			t.Errorf("the slot at %d, the template at %d, the search field at %d: want the slot and its template above the field", slot, i, strings.Index(main, `class="join w-full"`))
		}
		for _, want := range []string{"data-uploads", dropText, folder, busy} {
			if !strings.Contains(main, want) {
				t.Errorf("no %q", want)
			}
		}
		if !strings.Contains(openTag(t, main, form), "data-upload-vault") {
			t.Errorf("the form does not say Vault is set up: %s", openTag(t, main, form))
		}
		if strings.Contains(main, "Your files:") || strings.Contains(main, quotaHint) {
			t.Error("a guest has a quota line")
		}
		if strings.Contains(main, "data-upload-log") {
			t.Error("a guest has the upload log: a guest's drop shows the card")
		}
		if strings.Contains(main, "Magnet links go in the field above") {
			t.Error("the zone still points at the field above")
		}
		// "Upload a folder" is a button under the zone, not in it
		// (services/template.TestFolderLinkLeadsTheRowUnderTheZone); its
		// picker is not in the zone either: a click on it would bubble up to
		// the zone and open the file picker.
		z := zone(t, main)
		if strings.Contains(z, folder) || !strings.Contains(openTag(t, main, "dropzone-folder"), `type="button"`) {
			t.Errorf("want the folder button under the zone, not in it:\n%s", main)
		}
		if strings.Contains(z, "dropzone-dir") || !strings.Contains(main, "dropzone-dir") {
			t.Errorf("the folder picker is not next to the zone:\n%s", main)
		}
		const event = `data-umami-impression="private-refused-shown"`
		if n := strings.Count(main, event); n != 1 {
			t.Fatalf("the refusal is declared %d times, want once", n)
		}
		if k := strings.Index(main, event); k < i || k > j {
			t.Error("the refusal is declared outside the template: it would be sent on page load")
		}
	})

	t.Run("guest, tool page", func(t *testing.T) {
		main := dropzone(t, guest, &index.Data{Tool: common.ToolByURL("magnet-to-torrent")}, "/magnet-to-torrent", true)
		if !strings.Contains(main, `return-url=%2fmagnet-to-torrent"`) {
			t.Errorf("the sign-in link does not come back to the tool page:\n%s", main)
		}
	})

	// The page about uploading one's own files, with the real copy: the zone
	// says own files, the badge the own-files half only, and nothing of the
	// magnet field and the demo is left.
	t.Run("uploads page", func(t *testing.T) {
		const path = "/private-cloud-media-server"
		data := &index.Data{Tool: common.ToolByURL("private-cloud-media-server")}
		if data.Tool == nil || !data.Tool.Uploads {
			t.Fatal("no uploads page")
		}
		free := &claims.Data{Context: &proto.Context{Tier: &proto.Tier{Id: 0}}}
		for _, u := range []*auth.User{guest, signedIn} {
			main := dropzoneCtx(t, &w.Context{Lang: "en", Path: path, User: u, Data: data, Vault: true, Claims: free}, true)
			for _, want := range []string{
				`<span class="text-w-pinkL font-semibold">Drop your own files or a folder</span> — or click to choose.`,
				"Your files: up to 10 GB, with a subscription up to 50 GB</span>",
				folder, "data-uploads",
			} {
				if !strings.Contains(main, want) {
					t.Errorf("signed in %v: no %q", u.HasAuth(), want)
				}
			}
			for _, s := range []string{"Drop a .torrent", "Torrents:", "Magnet link or infohash", "Try the demo", "Speed test", `name="resource" value="magnet:`} {
				if strings.Contains(main, s) {
					t.Errorf("signed in %v: %q on the uploads page", u.HasAuth(), s)
				}
			}
			if got := strings.Contains(main, tplOpen); got != !u.HasAuth() {
				t.Errorf("signed in %v: sign-in template %v", u.HasAuth(), got)
			}
			if !u.HasAuth() && !strings.Contains(main, `return-url=%2fprivate-cloud-media-server"`) {
				t.Error("the sign-in link does not come back to the page")
			}
			// No search field here: the prompt, like the log, is above the zone.
			if !u.HasAuth() && strings.Index(main, "upload-sign-in-slot") > strings.Index(main, `class="dropzone"`) {
				t.Error("the sign-in slot is under the drop zone")
			}
		}
	})

	t.Run("signed in", func(t *testing.T) {
		main := dropzone(t, signedIn, &index.Data{}, "/", true)
		if strings.Contains(main, tplOpen) || strings.Contains(main, "upload-sign-in-slot") {
			t.Error("a signed-in user got the sign-in template or its slot")
		}
		// The upload's log: where a torrent's load shows its log, above
		// the search field, hidden until a drop.
		tag := openTag(t, main, "data-upload-log")
		for _, want := range []string{`class="hidden progress-alert progress-alert-block`, `data-async-target="main"`} {
			if !strings.Contains(tag, want) {
				t.Errorf("the log lacks %q: %s", want, tag)
			}
		}
		if strings.Index(main, "data-upload-log") > strings.Index(main, `class="join w-full"`) {
			t.Error("the log is under the search field")
		}
		for _, want := range []string{`class="job-stage"`, "data-upload-open", "data-upload-sign-in", "got it!"} {
			if !strings.Contains(main, want) {
				t.Errorf("the log lacks %q", want)
			}
		}
		if !strings.Contains(main, busy) {
			t.Error("the form has no busy text")
		}
		// The quota is the profile's line, not the drop zone's.
		if strings.Contains(main, "Your files:") || strings.Contains(main, quotaHint) {
			t.Error("a quota line under the drop zone")
		}
		if !strings.Contains(openTag(t, main, form), "data-upload-vault") {
			t.Errorf("the form does not say Vault is set up: %s", openTag(t, main, form))
		}
	})

	t.Run("signed in, no Vault", func(t *testing.T) {
		main := dropzoneCtx(t, &w.Context{Lang: "en", Path: "/", User: signedIn,
			Data: &index.Data{}}, true)
		for _, want := range []string{"data-uploads", busy} {
			if !strings.Contains(main, want) {
				t.Errorf("no %q", want)
			}
		}
		if strings.Contains(main, "data-upload-vault") {
			t.Error("Vault is not set up, yet the form says it is")
		}
	})

	t.Run("uploads off", func(t *testing.T) {
		main := dropzone(t, guest, &index.Data{}, "/", false)
		for _, s := range []string{tplOpen, "upload-sign-in-slot", "data-uploads", "data-upload-busy", "data-upload-vault", "dropzone-folder", "data-upload-log"} {
			if strings.Contains(main, s) {
				t.Errorf("uploads are off, yet %q is there", s)
			}
		}
		if want := `<span class="text-w-pinkL font-semibold">Drop torrent file here</span> or click to browse`; !strings.Contains(main, want) {
			t.Errorf("the torrent drop zone lost its text, want %q:\n%s", want, main)
		}
	})

	t.Run("the submit's page", func(t *testing.T) {
		d := newPostData(&job.Job{ID: "j", Queue: "load"}, &PostArgs{})
		for _, u := range []*auth.User{guest, signedIn} {
			main := dropzone(t, u, d, "/", true)
			if got := strings.Contains(main, tplOpen); got != !u.HasAuth() {
				t.Errorf("signed in %v: sign-in template %v", u.HasAuth(), got)
			}
			if strings.Contains(main, "Your files:") {
				t.Errorf("signed in %v: a quota line on the submit's page", u.HasAuth())
			}
			// The load's log, then the upload's: both under the hero.
			if i := strings.Index(main, "data-upload-log"); (i >= 0) != u.HasAuth() || (i >= 0 && i < strings.Index(main, `id="log-load"`)) {
				t.Errorf("signed in %v: the upload log at %d, the load's at %d", u.HasAuth(), i, strings.Index(main, `id="log-load"`))
			}
		}
	})
}

// The page about uploading one's own files has no features or FAQ section
// (partials/about.html), so a guest's navbar points "Features" and "FAQ" at
// the home page's sections there, as the menu does on every other page,
// instead of at anchors that are not on the page. Elsewhere they stay as
// they were.
func TestNavOnUploadsPagePointsHome(t *testing.T) {
	tpl, err := template.New("nav.html").Funcs(intentFuncs(t)).ParseFiles("../../templates/partials/nav.html")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stubTemplates(t, tpl, "lang_suggest_banner", "open_instance_banner")
	nav := func(tool string) string {
		return execute(t, tpl, "nav", &w.Context{Lang: "en", Path: "/" + tool, User: &auth.User{},
			Data: &index.Data{Tool: common.ToolByURL(tool)}})
	}
	const bare = `<li><a href="#features" class="text-w-sub hover:text-base-content transition-colors">`
	own := nav("private-cloud-media-server")
	for _, want := range []string{`<li><a href="/#features" class="text-w-sub hover:text-base-content transition-colors">`,
		`<li><a href="/#faq" class="text-w-sub hover:text-base-content transition-colors">`,
		`<li><a href="#how" class="text-w-sub hover:text-base-content transition-colors">`} {
		if !strings.Contains(own, want) {
			t.Errorf("uploads page: no %q", want)
		}
	}
	if strings.Contains(own, bare) {
		t.Error("uploads page: Features points at an anchor the page does not have")
	}
	for _, tool := range []string{"", "magnet-to-torrent"} {
		if out := nav(tool); !strings.Contains(out, bare) || !strings.Contains(out, `<li><a href="#faq" class="text-w-sub`) {
			t.Errorf("/%s: the navbar's anchors changed", tool)
		}
	}
}
