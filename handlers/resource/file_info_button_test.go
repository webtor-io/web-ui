package resource

import (
	"strings"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
)

// File info sits on the file card of a video or an audio file only, posts to
// /file-info into a dialog of its own (a press while watching leaves the
// player playing), is found by the deep link #action=file-info and the Umami
// click event by the action's name, and is a neutral, labelled line of its
// own on a phone.
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
			continue
		}
		for _, w := range []string{
			`class="file-info max-sm:basis-full max-sm:order-last"`,
			`data-async-target="#log-item-mp4-info"`,
			`getElementById('info-modal-item-mp4')`,
			`<dialog id="info-modal-item-mp4"`,
			`<div id="log-item-mp4-info"`,
			`class="btn btn-ghost border border-w-line whitespace-nowrap max-sm:w-full max-sm:px-3" data-umami-event="file-info"`,
		} {
			if !strings.Contains(out, w) {
				t.Errorf("%q: missing %q", c.format, w)
			}
		}
	}
}
