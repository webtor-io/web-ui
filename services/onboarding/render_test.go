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

// go build does not parse Go templates and the interesting branches only
// render for users in specific states, so the partial is exercised here
// directly: every locale, every checklist state. Two failure modes this
// catches that nothing else does — an unresolved translation key (the i18n
// helper returns the key verbatim, so a missing key surfaces as literal
// "onboarding." text in the output) and a branch no smoke test happens to hit.
//
// web.CtxData and web.LangURL are stubbed rather than imported: services/web
// pulls in the abuse-store and torrent-store protobufs, which collide at
// registration time and panic any test binary linking both. Stubbing keeps
// this package inside plain `go test ./...`. The stub langPath is deliberately
// distinctive so assertions below still prove the partial hands it the right
// arguments — prefixing itself belongs to web.LangURL and is tested there.

const partialPath = "../../templates/partials/onboarding_checklist.html"

type ctx struct {
	Lang string
}

type ctxData struct {
	Ctx  *ctx
	Data *Checklist
}

func langPathStub(lang string, path string) string {
	return "[" + lang + "]" + path
}

func assetStub(name string) template.HTML {
	return template.HTML(`<script src="/assets/` + name + `"></script>`)
}

func newRenderer(t *testing.T) *template.Template {
	t.Helper()

	h := i18n.NewHelper(i18n.New(os.DirFS("../../locales")))

	tmpl, err := template.New("onboarding_checklist.html").Funcs(template.FuncMap{
		"t":        h.T,
		"tp":       h.Tp,
		"langPath": langPathStub,
		"asset":    assetStub,
	}).ParseFiles(partialPath)
	if err != nil {
		t.Fatalf("failed to parse partial: %v", err)
	}
	return tmpl
}

func render(t *testing.T, tmpl *template.Template, lang string, cl *Checklist) string {
	t.Helper()
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "onboarding_checklist", &ctxData{
		Ctx:  &ctx{Lang: lang},
		Data: cl,
	}); err != nil {
		t.Fatalf("lang=%s: execute failed: %v", lang, err)
	}
	return buf.String()
}

func TestRenderAllLocalesAndStates(t *testing.T) {
	tmpl := newRenderer(t)
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	states := map[string]*models.OnboardingProgress{
		"fresh":       {CreatedAt: now.Add(-time.Hour)},
		"halfDone":    {CreatedAt: now.Add(-time.Hour), HasLibrary: true, HasVault: true},
		"stremioDone": {CreatedAt: now.Add(-time.Hour), HasStremio: true},
	}

	if len(i18n.SupportedLangs) == 0 {
		t.Fatal("no locales discovered")
	}

	for _, lang := range i18n.SupportedLangs {
		for name, p := range states {
			cl := build(p, true, paidUser, trialOn, now)
			if cl == nil {
				t.Fatalf("state %q unexpectedly produced no checklist", name)
			}
			out := render(t, tmpl, lang, cl)

			if strings.Contains(out, "onboarding.") {
				t.Errorf("lang=%s state=%s: unresolved translation key in output:\n%s", lang, name, out)
			}
			if !strings.Contains(out, `id="onboarding-checklist"`) {
				t.Errorf("lang=%s state=%s: card did not render", lang, name)
			}
			// Every step except the always-done account row renders its own
			// link, and they all look alike.
			wantLinks := 0
			for _, st := range cl.Steps {
				if st.Path != "" {
					wantLinks++
				}
			}
			if got := strings.Count(out, "data-umami-event="); got != wantLinks {
				t.Errorf("lang=%s state=%s: expected %d links, got %d", lang, name, wantLinks, got)
			}
			// The collapse control must ship with the card, otherwise the
			// toggle silently does nothing.
			if !strings.Contains(out, "data-checklist-toggle") {
				t.Errorf("lang=%s state=%s: collapse toggle missing", lang, name)
			}
			if !strings.Contains(out, "onboarding_checklist.js") {
				t.Errorf("lang=%s state=%s: collapse script not loaded", lang, name)
			}
			// Fragment links must never be async: async navigation swaps
			// #main and drops the fragment, landing the user at the top of
			// the profile page instead of the Stremio block.
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "#stremio") && strings.Contains(line, "data-async-target") {
					t.Errorf("lang=%s state=%s: fragment link must not be async: %s", lang, name, strings.TrimSpace(line))
				}
			}
		}
	}
}

func TestRenderNilChecklistProducesNothing(t *testing.T) {
	tmpl := newRenderer(t)
	// Every activated or older account renders a nil checklist; it must come
	// out empty rather than panic or leave an empty bordered card behind.
	if out := render(t, tmpl, "en", nil); strings.TrimSpace(out) != "" {
		t.Errorf("expected empty output for a nil checklist, got:\n%s", out)
	}
}

// The fragment has to be appended outside langPath — passing "/profile#stremio"
// through it would prefix the wrong string and break non-default locales.
func TestRenderAppendsFragmentOutsideLangPath(t *testing.T) {
	tmpl := newRenderer(t)
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	cl := build(&models.OnboardingProgress{CreatedAt: now.Add(-time.Hour)}, true, paidUser, trialOn, now)
	out := render(t, tmpl, "ru", cl)

	if !strings.Contains(out, `href="[ru]/profile#stremio"`) {
		t.Errorf("expected the fragment appended after langPath, got:\n%s", out)
	}
}

// All four sections are reachable at once — no step waits on another.
func TestRenderLinksEverySection(t *testing.T) {
	tmpl := newRenderer(t)
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	out := render(t, tmpl, "en", build(&models.OnboardingProgress{CreatedAt: now.Add(-time.Hour)}, true, paidUser, trialOn, now))
	for _, want := range []string{
		`data-umami-event="onboarding-library"`,
		`data-umami-event="onboarding-discover"`,
		`data-umami-event="onboarding-vault"`,
		`data-umami-event="onboarding-stremio"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing link: %s", want)
		}
	}
	for _, want := range []string{`href="[en]/lib/"`, `href="[en]/discover"`, `href="[en]/vault"`, `href="[en]/profile#stremio"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing destination: %s", want)
		}
	}
}

// A done step keeps the same three parts as an outstanding one; only its icon
// and title colour change.
func TestRenderDoneStepKeepsDescriptionAndLink(t *testing.T) {
	tmpl := newRenderer(t)
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	out := render(t, tmpl, "en", build(&models.OnboardingProgress{CreatedAt: now.Add(-time.Hour), HasLibrary: true}, true, paidUser, trialOn, now))
	if !strings.Contains(out, `data-umami-event="onboarding-library"`) {
		t.Error("a completed step must keep its link")
	}
}

// The free-tier card must show the PRO badge on the locked steps and must not
// offer them as links.
func TestRenderLockedStepsShowProBadgeAndQuietPlansLink(t *testing.T) {
	tmpl := newRenderer(t)
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	for _, lang := range i18n.SupportedLangs {
		cl := build(&models.OnboardingProgress{CreatedAt: now.Add(-time.Hour)}, true, false, trialOn, now)
		out := render(t, tmpl, lang, cl)

		if strings.Contains(out, "onboarding.") {
			t.Errorf("lang=%s: unresolved translation key in the locked variant", lang)
		}
		if strings.Count(out, ">PRO<") != 2 {
			t.Errorf("lang=%s: expected a PRO badge on both locked steps, got %d", lang, strings.Count(out, ">PRO<"))
		}
		// The section links are replaced by quiet /donate links, not dropped.
		for _, banned := range []string{`data-umami-event="onboarding-vault"`, `data-umami-event="onboarding-stremio"`} {
			if strings.Contains(out, banned) {
				t.Errorf("lang=%s: locked step must not link to its own section (%s)", lang, banned)
			}
		}
		for _, want := range []string{`data-umami-event="onboarding-pro-vault"`, `data-umami-event="onboarding-pro-stremio"`} {
			if !strings.Contains(out, want) {
				t.Errorf("lang=%s: locked step must offer the plans link (%s)", lang, want)
			}
		}
		if got := strings.Count(out, `href="[`+lang+`]/donate"`); got != 2 {
			t.Errorf("lang=%s: expected 2 donate links, got %d", lang, got)
		}
		// A real button, because a quiet text link got 0 clicks from 219 free
		// users — but still not the pink accent, which belongs to the steps
		// the user can actually complete.
		for _, line := range strings.Split(out, "\n") {
			if !strings.Contains(line, `href="[`+lang+`]/donate"`) {
				continue
			}
			if strings.Contains(line, "text-w-pinkL") || strings.Contains(line, "btn-pink") {
				t.Errorf("lang=%s: the upsell must not take the steps' accent: %s", lang, strings.TrimSpace(line))
			}
			if !strings.Contains(line, "btn btn-xs btn-soft") {
				t.Errorf("lang=%s: the upsell must be a button, not a text link: %s", lang, strings.TrimSpace(line))
			}
		}
		// The steps a free user can act on stay clickable.
		for _, want := range []string{`data-umami-event="onboarding-library"`, `data-umami-event="onboarding-discover"`} {
			if !strings.Contains(out, want) {
				t.Errorf("lang=%s: actionable step lost its link (%s)", lang, want)
			}
		}
	}
}

// Status is otherwise carried by icon and colour alone, which a screen reader
// cannot see. Every row must ship a text equivalent.
func TestRenderStatusHasTextEquivalent(t *testing.T) {
	tmpl := newRenderer(t)
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	out := render(t, tmpl, "en", build(&models.OnboardingProgress{CreatedAt: now.Add(-time.Hour), HasLibrary: true}, true, false, trialOn, now))
	// Five row statuses plus the progress counter's spoken form.
	if got := strings.Count(out, `class="sr-only"`); got != 6 {
		t.Errorf("expected 5 row statuses + 1 counter label, got %d", got)
	}
	for _, want := range []string{"Done:", "To do:", "Locked:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing status text %q", want)
		}
	}
	// aria-label on a bare <span> is dropped (ARIA forbids naming role=generic),
	// so the digits are hidden and the sentence is sr-only text instead.
	if !strings.Contains(out, `<span class="sr-only">2 of 3 steps done</span>`) {
		t.Error("progress counter has no spoken form")
	}
	if !strings.Contains(out, `tabular-nums" aria-hidden="true">2/3<`) {
		t.Error("the bare digits must be hidden from assistive tech")
	}
}

// Without JS the toggle would be focusable and announced, yet do nothing, so it
// ships hidden and is revealed by script.
func TestRenderToggleIsHiddenUntilScriptRuns(t *testing.T) {
	tmpl := newRenderer(t)
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	out := render(t, tmpl, "en", build(&models.OnboardingProgress{CreatedAt: now.Add(-time.Hour)}, true, true, trialOn, now))
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "data-checklist-toggle") && !strings.Contains(line, "hidden") {
			t.Errorf("the toggle must render hidden: %s", strings.TrimSpace(line))
		}
	}
	// The inline snippet restores the collapsed state before paint.
	if !strings.Contains(out, "onboarding-checklist-collapsed") {
		t.Error("the pre-paint collapse snippet is missing")
	}
}

// The impression metric is the point of this round: the counts must reach the
// DOM, otherwise the module reports zeros and we would read "nobody sees it".
func TestRenderCarriesImpressionData(t *testing.T) {
	tmpl := newRenderer(t)
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	out := render(t, tmpl, "en", build(&models.OnboardingProgress{CreatedAt: now.Add(-time.Hour), HasLibrary: true}, true, false, trialOn, now))
	// Whitespace-tolerant: html/template pads numbers in attribute context.
	for attr, want := range map[string]string{"done": "2", "total": "3", "locked": "2"} {
		re := regexp.MustCompile(`data-onboarding-` + attr + `="\s*` + want + `\s*"`)
		if !re.MatchString(out) {
			t.Errorf("missing or wrong impression attribute %q (want %s)", attr, want)
		}
	}
}
