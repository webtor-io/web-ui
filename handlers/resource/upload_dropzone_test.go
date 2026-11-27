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

// The drop zone takes a guest's own files only to show the sign-in card, and
// that card is declared inside a <template>: its impression goes out when
// the card is shown, not when the page loads.
func TestDropzoneUploads(t *testing.T) {
	const (
		tplOpen   = `<template id="upload-sign-in">`
		busy      = `data-upload-busy="Wait until the upload finishes"`
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
		} {
			if !strings.Contains(card, want) {
				t.Errorf("the card lacks %q:\n%s", want, card)
			}
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
		// "Upload a folder" is a button inside the zone; its picker is not:
		// a click on it would bubble up to the zone and open the file picker.
		z := zone(t, main)
		if !strings.Contains(z, folder) || !strings.Contains(openTag(t, z, "dropzone-folder"), `type="button"`) {
			t.Errorf("no folder button inside the zone:\n%s", z)
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
		}
	})

	t.Run("signed in", func(t *testing.T) {
		main := dropzone(t, signedIn, &index.Data{}, "/", true)
		if strings.Contains(main, tplOpen) {
			t.Error("a signed-in user got the sign-in template")
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
		for _, s := range []string{tplOpen, "data-uploads", "data-upload-busy", "data-upload-vault", "dropzone-folder", "data-upload-log"} {
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
