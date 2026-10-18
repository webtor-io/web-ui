package resource

import (
	"strings"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
)

// File info sits on the file card of a video or an audio file only, last in
// the row, posts to /file-info into a dialog of its own (a press while
// watching leaves the player playing), is found by the deep link
// #action=file-info and the Umami click event by the action's name, and is a
// neutral 44 px icon on a phone with its name for screen readers; the row is
// then marked crowded, which drops Watch's and Download's icons there.
func TestFileCardFileInfoButton(t *testing.T) {
	tpl := resourceTemplates(t)
	for _, c := range []struct {
		format ra.MediaFormat
		want   bool
	}{{ra.Video, true}, {ra.Audio, true}, {ra.Image, false}, {"", false}} {
		gd := intentPage("", false)
		gd.Item.MediaFormat = c.format
		out := execute(t, tpl, "file", pageContext("ru", gd))
		has := strings.Contains(out, `action="/ru/file-info"`)
		if has != c.want {
			t.Errorf("%q: button %v, want %v", c.format, has, c.want)
			continue
		}
		if !has {
			if strings.Contains(out, "info-modal-") {
				t.Errorf("%q: a dialog without its button", c.format)
			}
			if strings.Contains(out, " data-crowded>") {
				t.Errorf("%q: the row is marked crowded without File info", c.format)
			}
			continue
		}
		// The row's last button: nothing but the row's end after its form.
		fi := strings.Index(out, `<form class="file-info`)
		if end := strings.Index(out[fi:], "</form>"); end < 0 || !strings.HasPrefix(strings.TrimSpace(out[fi+end+len("</form>"):]), "</div>") {
			t.Errorf("%q: File info is not the row's last button", c.format)
		}
		for _, w := range []string{
			`class="file-info shrink-0"`,
			`data-async-target="#log-item-mp4-info"`,
			`getElementById('info-modal-item-mp4')`,
			`<dialog id="info-modal-item-mp4"`,
			`<div id="log-item-mp4-info"`,
			`class="btn btn-ghost border border-w-line max-sm:w-11 max-sm:px-0 whitespace-nowrap" data-umami-event="file-info" title="О файле"`,
			`<span class="max-sm:sr-only">О файле</span>`,
			`sm:gap-3" data-crowded>`,
			`<span class="inline-flex max-sm:group-data-crowded/actions:hidden">`,
		} {
			if !strings.Contains(out, w) {
				t.Errorf("%q: missing %q", c.format, w)
			}
		}
	}
}
