package support

import (
	"bytes"
	"html/template"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
)

// renderView executes the real view's "main" for data, as the site renders
// it: a view that breaks takes the process down at startup, and a missing
// key only shows as "<no value>".
func renderView(t *testing.T, view string, data *Data) string {
	t.Helper()
	locales, err := os.OpenRoot(localesDir)
	if err != nil {
		t.Fatal(err)
	}
	defer locales.Close()
	h := i18n.NewHelper(i18n.New(locales.FS()))
	tpl, err := template.New(view).Funcs(template.FuncMap{
		"t":                h.T,
		"langPath":         func(_, p string) string { return p },
		"useTurnstile":     func() bool { return false },
		"turnstileSiteKey": func() string { return "" },
		"asset":            func(string) template.HTML { return "" },
	}).ParseFiles("../../templates/views/support/" + view)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main", struct {
		Lang, ErrKey string
		Data         *Data
	}{"en", "", data}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "<no value>") {
		t.Error("the page rendered a missing value")
	}
	return out
}

var optionValue = regexp.MustCompile(`<option[^>]*value="(-?\d+)"`)

// The form lists the content reports first, asks a copyright notice for the
// reporter's name and both statements, and keeps what was typed.
func TestSupportFormRenders(t *testing.T) {
	out := renderView(t, "form.html", &Data{
		Form:       &Form{Cause: 0, ReporterName: "Example Studio", GoodFaith: true},
		CauseTypes: CauseTypes,
	})
	var got []string
	for _, m := range optionValue.FindAllStringSubmatch(out, -1) {
		got = append(got, m[1])
	}
	if want := []string{"-1", "0", "5", "4", "1", "2", "3"}; !slices.Equal(got, want) {
		t.Errorf("causes %q, want %q", got, want)
	}
	for _, want := range []string{
		`name="reporter_name"`, `value="Example Studio"`,
		// gin binds a bool with strconv.ParseBool: "on" would be refused.
		`name="good_faith" type="checkbox" value="true"`, `name="authorized" type="checkbox" value="true"`,
		english(t, "support.goodFaith"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the form lacks %q", want)
		}
	}
	if n := strings.Count(out, "checked"); n != 1 {
		t.Errorf("%d checked statements, want the one that was ticked", n)
	}
}

// The success page says the report waits for its confirmation only when
// abuse-store holds it.
func TestSupportSuccessRenders(t *testing.T) {
	pending, sent := english(t, "support.confirmationSent"), english(t, "support.successMessage")
	for confirm, want := range map[bool]string{true: pending, false: sent} {
		out := renderView(t, "success.html", &Data{ConfirmationSent: confirm})
		if !strings.Contains(out, want) {
			t.Errorf("confirmation_sent=%v: the page lacks %q", confirm, want)
		}
		if other := map[bool]string{true: sent, false: pending}[confirm]; strings.Contains(out, other) {
			t.Errorf("confirmation_sent=%v: the page also says %q", confirm, other)
		}
	}
}
