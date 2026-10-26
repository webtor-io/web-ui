package web

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/services/i18n"
)

// A duration reads in the viewer's language (durafmt, before, said "1 day"
// on the Russian help page) and is rounded up, so the time a freeze is said
// to have left never runs out before the server lets the pledge go. It is
// one unbreakable token: every space below is a no-break one in the result,
// the one between its parts too (a phone broke "13 h / 20 min").
func TestDurationRoundsUpInTheViewersLanguage(t *testing.T) {
	i18n.New(os.DirFS("../../locales"))
	h := &Helper{}
	for _, c := range []struct {
		lang string
		d    time.Duration
		want string
	}{
		{"en", 59 * time.Second, "1 min"},
		{"en", time.Second, "1 min"},
		{"en", 45 * time.Minute, "45 min"},
		{"en", 59*time.Minute + 30*time.Second, "1 h"},
		{"en", 13*time.Hour + 19*time.Minute + 30*time.Second, "13 h 20 min"},
		{"en", 24 * time.Hour, "24 h"},
		{"en", 47*time.Hour + 59*time.Minute + 30*time.Second, "2 d"},
		{"en", 6*24*time.Hour + 22*time.Hour + 10*time.Minute, "6 d 23 h"},
		{"en", 6*24*time.Hour + 23*time.Hour + 30*time.Minute, "7 d"},
		{"ru", 13*time.Hour + 20*time.Minute, "13 ч 20 мин"},
		{"ru", 24 * time.Hour, "24 ч"},
		{"ru", 7 * 24 * time.Hour, "7 дн."},
		{"de", 13*time.Hour + 20*time.Minute, "13 Std. 20 Min."},
		{"tr", 13*time.Hour + 20*time.Minute, "13 sa 20 dk"},
		{"tr", 7 * 24 * time.Hour, "7 gün"},
	} {
		want := strings.ReplaceAll(c.want, " ", "\u00a0")
		if got := h.Duration(c.lang, c.d); got != want {
			t.Errorf("%s %v: %q, want %q", c.lang, c.d, got, want)
		}
	}
}
