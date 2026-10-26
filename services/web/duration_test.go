package web

import (
	"os"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/services/i18n"
)

// A duration reads in the viewer's language (durafmt, before, said "1 day"
// on the Russian help page) and is rounded up, so the time a freeze is said
// to have left never runs out before the server lets the pledge go.
func TestDurationRoundsUpInTheViewersLanguage(t *testing.T) {
	i18n.New(os.DirFS("../../locales"))
	h := &Helper{}
	const nb = "\u00a0"
	for _, c := range []struct {
		lang string
		d    time.Duration
		want string
	}{
		{"en", 59 * time.Second, "1" + nb + "min"},
		{"en", time.Second, "1" + nb + "min"},
		{"en", 45 * time.Minute, "45" + nb + "min"},
		{"en", 59*time.Minute + 30*time.Second, "1" + nb + "h"},
		{"en", 13*time.Hour + 19*time.Minute + 30*time.Second, "13" + nb + "h 20" + nb + "min"},
		{"en", 24 * time.Hour, "24" + nb + "h"},
		{"en", 47*time.Hour + 59*time.Minute + 30*time.Second, "2" + nb + "d"},
		{"en", 6*24*time.Hour + 22*time.Hour + 10*time.Minute, "6" + nb + "d 23" + nb + "h"},
		{"en", 6*24*time.Hour + 23*time.Hour + 30*time.Minute, "7" + nb + "d"},
		{"ru", 13*time.Hour + 20*time.Minute, "13" + nb + "ч 20" + nb + "мин"},
		{"ru", 24 * time.Hour, "24" + nb + "ч"},
		{"ru", 7 * 24 * time.Hour, "7" + nb + "дн."},
		{"de", 13*time.Hour + 20*time.Minute, "13" + nb + "Std. 20" + nb + "Min."},
		{"tr", 13*time.Hour + 20*time.Minute, "13" + nb + "sa 20" + nb + "dk"},
	} {
		if got := h.Duration(c.lang, c.d); got != c.want {
			t.Errorf("%s %v: %q, want %q", c.lang, c.d, got, c.want)
		}
	}
}
