package template

import (
	"bytes"
	"fmt"
	"html/template"
	"regexp"
	"strings"
	"testing"

	hc "github.com/webtor-io/web-ui/handlers/common"
)

func renderHeading(t *testing.T, tpl *template.Template, tool *hc.Tool) string {
	t.Helper()
	ctx := map[string]interface{}{"Lang": "en"}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "tool_heading", map[string]interface{}{"Ctx": ctx, "Data": tool}); err != nil {
		t.Fatalf("render /%s: %v", tool.Url, err)
	}
	return strings.TrimSpace(whitespace.ReplaceAllString(buf.String(), " "))
}

var h1Re = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)

// TestGuideHeadingIsTheQuestion: on a guide the H1 is the question in the
// Title and the Benefit is the line under it. Google shows the H1 in place of
// our <title> on these pages, and a Benefit-only H1 put a fragment ("Without
// Installing a Torrent Client") in the result with the question dropped.
func TestGuideHeadingIsTheQuestion(t *testing.T) {
	tpl := aboutTemplates(t)
	guides := 0
	for i := range hc.Tools {
		tool := &hc.Tools[i]
		if !tool.Guide {
			continue
		}
		guides++
		t.Run(tool.Url, func(t *testing.T) {
			out := renderHeading(t, tpl, tool)
			h1s := h1Re.FindAllStringSubmatch(out, -1)
			if len(h1s) != 1 {
				t.Fatalf("%d <h1> rendered, want exactly one: %s", len(h1s), out)
			}
			if !strings.Contains(h1s[0][1], tool.Title) {
				t.Errorf("the H1 does not carry the question (%s): %s", tool.Title, h1s[0][1])
			}
			if strings.Contains(h1s[0][1], tool.Benefit) {
				t.Errorf("the H1 still carries the benefit (%s): %s", tool.Benefit, h1s[0][1])
			}
			rest := out[strings.Index(out, "</h1>"):]
			if !regexp.MustCompile(`<p[^>]*>` + regexp.QuoteMeta(tool.Benefit) + `</p>`).MatchString(rest) {
				t.Errorf("the benefit is not the line under the H1: %s", out)
			}
		})
	}
	// Without a guide in the list the checks above pass by running nothing.
	if guides == 0 {
		t.Fatal("no tool is marked Guide — the test ran against nothing")
	}
}

// TestToolHeadingIsUnchangedOutsideGuides pins every other tool page to the
// hero H1 it had before guides got their own: the Benefit, byte for byte the
// markup templates/views/index.html used to inline.
func TestToolHeadingIsUnchangedOutsideGuides(t *testing.T) {
	tpl := aboutTemplates(t)
	others := 0
	for i := range hc.Tools {
		tool := &hc.Tools[i]
		if tool.Guide {
			continue
		}
		others++
		t.Run(tool.Url, func(t *testing.T) {
			want := fmt.Sprintf(`<h1 class="relative z-10 text-[clamp(2.4rem,5vw,3.8rem)] font-extrabold tracking-tighter leading-[1.1] max-w-[780px] mb-5 text-balance"> <span class="gradient-text">%s</span> </h1>`, tool.Benefit)
			if got := renderHeading(t, tpl, tool); got != want {
				t.Errorf("heading changed:\n got %s\nwant %s", got, want)
			}
		})
	}
	if others == 0 {
		t.Fatal("every tool is a guide — the test ran against nothing")
	}
}

// aboutPageTemplates adds templates/partials/about.html — the whole tool/home
// page body with its shared FAQ — to the about partials, with stubs for the
// funcs it calls. Every stub stands for a func the template manager
// registers (web.Helper, i18n.Helper, recommendations.Helper).
func aboutPageTemplates(t *testing.T) *template.Template {
	t.Helper()
	funcs := aboutFuncs()
	funcs["kebabToSnake"] = func(s string) string { return strings.ReplaceAll(s, "-", "_") }
	funcs["faqSchema"] = func(lang string, pairs ...string) template.HTML { return "" }
	funcs["aiEnabled"] = func() bool { return true }
	funcs["aiFreeQuota"] = func() int { return 0 }
	funcs["aiPaidQuota"] = func() int { return 0 }
	funcs["blogLang"] = func(lang string) string { return lang }
	funcs["tools"] = func() []hc.Tool { return hc.Tools }
	funcs["apiDocsURL"] = func() string { return "" }
	return parseAboutTemplates(t, funcs,
		"../../templates/partials/about.html",
		"../../templates/partials/about/*.html")
}

var faqSectionRe = regexp.MustCompile(`(?s)<section id="faq".*?</section>`)
var siteHrefRe = regexp.MustCompile(`href="/([a-z0-9-]+)"`)

// faqLinks renders the page body for tool (nil = the home page) and returns
// the site links inside its FAQ section, in order.
func faqLinks(t *testing.T, tpl *template.Template, tool *hc.Tool) []string {
	t.Helper()
	instruction := ""
	if tool != nil {
		instruction = tool.Url
	}
	ctx := map[string]interface{}{
		"Lang": "en",
		"Data": map[string]interface{}{"Tool": tool, "Instruction": instruction},
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "about", ctx); err != nil {
		t.Fatalf("render about: %v", err)
	}
	faq := faqSectionRe.FindString(buf.String())
	if faq == "" {
		t.Fatal("no FAQ section rendered")
	}
	var out []string
	for _, m := range siteHrefRe.FindAllStringSubmatch(faq, -1) {
		out = append(out, m[1])
	}
	return out
}

// TestFAQDoesNotLinkAGuideToItself: the FAQ is shared by the home page and all
// tool pages, and its answers point at the guides. On a guide, the link to
// that guide is a link to the page the reader is on — it is dropped there and
// only there.
func TestFAQDoesNotLinkAGuideToItself(t *testing.T) {
	tpl := aboutPageTemplates(t)
	home := faqLinks(t, tpl, nil)

	// The home page links every guide; otherwise a guide's own page would
	// pass below without the FAQ ever having linked it.
	for _, tool := range hc.Tools {
		if tool.Guide && !contains(home, tool.Url) {
			t.Fatalf("the home FAQ does not link /%s (links: %v) — the self-link check would be vacuous for it", tool.Url, home)
		}
	}

	for i := range hc.Tools {
		tool := &hc.Tools[i]
		t.Run(tool.Url, func(t *testing.T) {
			want := home
			if tool.Guide {
				want = without(home, tool.Url)
			}
			got := faqLinks(t, tpl, tool)
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("FAQ links = %v, want %v", got, want)
			}
		})
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func without(list []string, s string) []string {
	var out []string
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

// TestFreeCapLineFollowsTheCatalog: the comparison on /watch-torrents-ios
// states the free plan's speed cap with the catalog's number, and only when
// there is a cap to state and a plan that lifts it. A deployment without the
// storefront catalog renders no such line rather than a number or a plan it
// does not have.
func TestFreeCapLineFollowsTheCatalog(t *testing.T) {
	var ios *hc.Tool
	for i := range hc.Tools {
		if hc.Tools[i].Url == "watch-torrents-ios" {
			ios = &hc.Tools[i]
		}
	}
	if ios == nil {
		t.Fatal("/watch-torrents-ios is not in the tool list")
	}
	const key = "tool.watchTorrentsIos.about.utorrent.cap"
	for _, tc := range []struct {
		name  string
		plans bool
		rate  int
		want  bool
	}{
		{"production catalog", true, 5, true},
		{"no catalog", false, 0, false},
		{"plans, but the free tier is uncapped", true, 0, false},
		{"a capped free tier, but nothing to buy", false, 5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			funcs := aboutFuncs()
			funcs["hasPlans"] = func() bool { return tc.plans }
			funcs["freeRateMbps"] = func() int { return tc.rate }
			// Echo the params too, so the number is seen to come through.
			funcs["tp"] = func(lang, key string, args ...interface{}) string { return fmt.Sprint(key, args) }
			out := renderAbout(t, parseAboutTemplates(t, funcs, "../../templates/partials/about/*.html"), *ios)
			if got := strings.Contains(out, key); got != tc.want {
				t.Fatalf("cap line rendered = %v, want %v", got, tc.want)
			}
			if tc.want && !strings.Contains(out, key+"[Rate 5]") {
				t.Errorf("the cap line does not quote the catalog's rate: %s", out)
			}
		})
	}
}
