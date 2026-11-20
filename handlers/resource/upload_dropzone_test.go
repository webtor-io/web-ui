package resource

import (
	"html/template"
	"strings"
	"testing"

	uuid "github.com/satori/go.uuid"

	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/handlers/index"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/uploads"
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

// The drop zone takes a guest's own files only to show the sign-in card, and
// that card is declared inside a <template>: its impression goes out when
// the card is shown, not when the page loads.
func TestDropzoneUploads(t *testing.T) {
	const (
		tplOpen   = `<template id="upload-sign-in">`
		busy      = `data-upload-busy="Wait until the upload finishes"`
		form      = `class="dropzone"`
		folder    = `Upload a folder`
		dropText  = `Drop a .torrent or your own files and folders, or click to choose.`
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

	t.Run("signed in", func(t *testing.T) {
		main := dropzone(t, signedIn, &index.Data{UploadQuota: &uploads.Usage{Used: 3 << 30, Cap: 10 << 30}}, "/", true)
		if strings.Contains(main, tplOpen) {
			t.Error("a signed-in user got the sign-in template")
		}
		if !strings.Contains(main, busy) {
			t.Error("the form has no busy text")
		}
		if want := "Your files: 3.0\u00a0GB of 10\u00a0GB used"; !strings.Contains(main, want) {
			t.Errorf("no %q:\n%s", want, main)
		}
		if !strings.Contains(main, quotaHint) {
			t.Error("no quota hint")
		}
		if !strings.Contains(openTag(t, main, form), "data-upload-vault") {
			t.Errorf("the form does not say Vault is set up: %s", openTag(t, main, form))
		}
	})

	t.Run("signed in, no Vault", func(t *testing.T) {
		main := dropzoneCtx(t, &w.Context{Lang: "en", Path: "/", User: signedIn,
			Data: &index.Data{UploadQuota: &uploads.Usage{Used: 3 << 30, Cap: 10 << 30}}}, true)
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
		for _, s := range []string{tplOpen, "data-uploads", "data-upload-busy", "data-upload-vault", "dropzone-folder"} {
			if strings.Contains(main, s) {
				t.Errorf("uploads are off, yet %q is there", s)
			}
		}
		if !strings.Contains(main, "Drop torrent file here") {
			t.Error("the torrent drop zone lost its text")
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
		}
	})
}
