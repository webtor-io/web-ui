package onboarding

import (
	"bytes"
	"html/template"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/i18n"
)

// Parsing the whole navbar would drag in a dozen unrelated helpers, so this
// isolates the counter block: it still renders the real markup with real
// translations, which is what could break.
func navCounterSnippet(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../templates/partials/nav.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, `{{ if .Onboarding }}`)
	if start < 0 {
		t.Fatal("navbar has no onboarding counter block")
	}
	end := strings.Index(s[start:], "{{ end }}")
	if end < 0 {
		t.Fatal("onboarding counter block is unterminated")
	}
	return s[start : start+end+len("{{ end }}")]
}

func TestNavCounterRenders(t *testing.T) {
	h := i18n.NewHelper(i18n.New(os.DirFS("../../locales")))
	tmpl := template.Must(template.New("nav-counter").Funcs(template.FuncMap{
		"tp":       h.Tp,
		"langPath": func(l, p string) string { return "[" + l + "]" + p },
	}).Parse(navCounterSnippet(t)))

	type ctx struct {
		Lang       string
		Onboarding *models.OnboardingChecklist
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, &ctx{Lang: "ru", Onboarding: &models.OnboardingChecklist{Done: 2, Total: 3}}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`data-umami-event="onboarding-nav"`,
		`href="[ru]/#onboarding-checklist"`,
		`2/3`,
		`Выполнено 2 из 3 шагов`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("navbar counter missing %q in:\n%s", want, out)
		}
	}
	// It opens the modal in place; targeting main would navigate the page away,
	// which is the behaviour this replaced.
	if !strings.Contains(out, `data-async-target="#onboarding-modal"`) {
		t.Error("the counter must open the modal, not navigate")
	}
	if strings.Contains(out, `data-async-target="main"`) {
		t.Error("the counter must not swap the page")
	}

	buf.Reset()
	if err := tmpl.Execute(&buf, &ctx{Lang: "ru"}); err != nil {
		t.Fatalf("execute without checklist: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("counter must render nothing without a checklist, got:\n%s", buf.String())
	}
}

// The modal is what the navbar counter opens, so it must render the same rows
// as the home card and must not be baked into the page.
func TestNavModalRendersOnDemand(t *testing.T) {
	b, err := os.ReadFile("../../templates/partials/nav.html")
	if err != nil {
		t.Fatal(err)
	}
	nav := string(b)

	if !strings.Contains(nav, `data-async-target="#onboarding-modal"`) {
		t.Error("the counter must target the modal container")
	}
	// The navbar is fixed + backdrop-blur, and a filtered ancestor becomes the
	// containing block for fixed children — a dialog rendered inside it is
	// clipped to the 72px bar instead of covering the viewport.
	if strings.Contains(nav, `id="onboarding-modal"`) {
		t.Error("the modal container must not live inside the navbar — it would be clipped to it")
	}

	lb, err := os.ReadFile("../../templates/layouts/main.html")
	if err != nil {
		t.Fatal(err)
	}
	layout := string(lb)
	// The container declares how to re-render itself; without this the modal
	// would show whatever was true when the page was first rendered.
	if !strings.Contains(layout, `id="onboarding-modal" data-async-layout=`) {
		t.Error("the layout must host the modal container with data-async-layout")
	}
	// It must stay a real link, so no-JS users still reach the home card.
	if !strings.Contains(nav, `#onboarding-checklist"`) {
		t.Error("the counter must keep an href to the home card as a no-JS fallback")
	}
}

func TestModalRendersRowsAndClosesOnNavigation(t *testing.T) {
	tmpl := newRenderer(t)
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	cl := build(&models.OnboardingProgress{CreatedAt: now.Add(-time.Hour), HasLibrary: true}, true, false, now)

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "onboarding_checklist/modal", &ctxData{Ctx: &ctx{Lang: "ru"}, Data: cl}); err != nil {
		t.Fatalf("execute modal: %v", err)
	}
	out := buf.String()

	// showModal(), not the `open` attribute: `open` is a non-modal dialog with
	// no Esc, no focus trap and no inert background, and DaisyUI styling makes
	// that look correct.
	if strings.Contains(out, "<dialog class=\"modal\" id=\"onboarding-checklist-dialog\" open>") {
		t.Error("the dialog must not use the non-modal `open` attribute")
	}
	if !strings.Contains(out, "showModal()") {
		t.Error("the dialog must be opened with showModal()")
	}
	if !strings.Contains(out, `aria-labelledby="onboarding-checklist-dialog-title"`) {
		t.Error("the dialog needs an accessible name")
	}
	// The dialog nests inside the async container. Sharing its id would make
	// getElementById return the container — a <div>, which has no close() —
	// so the close-on-navigate handler would throw and the dialog would hang
	// over the page the user just asked for.
	if strings.Contains(out, `id="onboarding-modal"`) {
		t.Error("the dialog must not reuse the container id")
	}
	if !strings.Contains(out, `getElementById('onboarding-checklist-dialog')`) {
		t.Error("the close handler must look up the dialog, not the container")
	}
	// Same rows as the card, including the locked ones.
	for _, want := range []string{"Откройте торрент", "PRO", `data-umami-event="onboarding-library"`, `data-umami-event="onboarding-pro-vault"`} {
		if !strings.Contains(out, want) {
			t.Errorf("modal missing %q", want)
		}
	}
	// Async step links swap the page behind the dialog, so it has to close —
	// and the listener must be in the capture phase, because async.js calls
	// stopPropagation() in the link's own handler and nothing bubbles.
	if !strings.Contains(out, "d.close()") {
		t.Error("the modal must close itself when a step link is followed")
	}
	if !strings.Contains(out, "},true)") {
		t.Error("the close listener must use the capture phase, or async links never reach it")
	}
	if strings.Contains(out, `id="onboarding-checklist"`) {
		t.Error("the modal must not duplicate the home card's element id")
	}
	// Both live on the home page at once, so every id the modal emits must be
	// absent from the card and vice versa.
	card := render(t, tmpl, "ru", cl)
	for _, id := range idsIn(out) {
		if strings.Contains(card, `id="`+id+`"`) {
			t.Errorf("id %q appears in both the card and the modal", id)
		}
	}

	buf.Reset()
	if err := tmpl.ExecuteTemplate(&buf, "onboarding_checklist/modal", &ctxData{Ctx: &ctx{Lang: "ru"}}); err != nil {
		t.Fatalf("execute empty modal: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "" {
		t.Error("no checklist must render no modal")
	}
}

var idRe = regexp.MustCompile(`id="([^"]+)"`)

func idsIn(html string) []string {
	var out []string
	for _, m := range idRe.FindAllStringSubmatch(html, -1) {
		out = append(out, m[1])
	}
	return out
}
