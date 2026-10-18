package resource

import (
	"strings"
	"testing"

	ra "github.com/webtor-io/rest-api/services"
)

// File info sits on the file card of a video or an audio file only, posts to
// /file-info into the card's log (where the deep link #action=file-info and
// the Umami click event find it by the action's name), and is an icon on a
// phone with its name kept for screen readers.
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
			continue
		}
		for _, w := range []string{
			`class="file-info shrink-0"`,
			`data-async-target="#log-item-mp4"`,
			`data-umami-event="file-info"`,
			`title="О файле"`,
			`<span class="max-sm:sr-only">О файле</span>`,
		} {
			if !strings.Contains(out, w) {
				t.Errorf("%q: missing %q", c.format, w)
			}
		}
	}
}
