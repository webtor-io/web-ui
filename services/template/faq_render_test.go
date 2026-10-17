package template

import (
	"bytes"
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	hc "github.com/webtor-io/web-ui/handlers/common"
)

// renderFAQ renders the page body for tool (nil = the home page) and returns
// its FAQ section, whitespace-normalised. The faqSchema stub echoes its pairs
// (see aboutPageFuncs), so the JSON-LD order is part of the result.
func renderFAQ(t *testing.T, tpl *template.Template, tool *hc.Tool) string {
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
	return strings.TrimSpace(whitespace.ReplaceAllString(faq, " "))
}

// faqPages is the home page plus every tool, keyed by the snapshot name.
func faqPages() map[string]*hc.Tool {
	out := map[string]*hc.Tool{"home": nil}
	for i := range hc.Tools {
		out[hc.Tools[i].Url] = &hc.Tools[i]
	}
	return out
}

// TestFAQSectionSnapshots pins the FAQ section of the home page and of every
// tool page, JSON-LD pairs included. The snapshots were taken AFTER the
// per-tool questions moved from a hardcoded torrent_to_ddl branch to
// Tool.FAQ (2026-10-17), so they pin the current output, not the old one.
// The refactor was checked by hand against main's about.html rendered with
// the old faqSchema signature: the home page, torrent-to-ddl and the 17
// pages without FAQ of their own differ from it only in the openTorrentApp
// guide link (/open-torrents-on-pc → /open-torrent-file); the three guides
// also drop the how-to items they answer and add their own questions. Run
// with -update after an intentional change and read the diff — never
// bulk-update: the FAQ is on every search landing page.
func TestFAQSectionSnapshots(t *testing.T) {
	tpl := aboutPageTemplates(t)
	dir := filepath.Join("testdata", "faq")
	if *updateGolden {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, tool := range faqPages() {
		t.Run(name, func(t *testing.T) {
			got := renderFAQ(t, tpl, tool)
			path := filepath.Join(dir, name+".html")
			if *updateGolden {
				if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("no snapshot for %s (run with -update): %v", name, err)
			}
			if got != strings.TrimSpace(string(want)) {
				t.Errorf("%s: FAQ section renders differently from its snapshot; rerun with -update to inspect the diff", name)
			}
		})
	}
}

var (
	faqEchoRe       = regexp.MustCompile(`<!--faq: (.*?)-->`)
	faqTitleRe      = regexp.MustCompile(`collapse-title text-base font-semibold">([^<]+)<`)
	toolFaqAnswerRe = regexp.MustCompile(`leading-relaxed">(tool\.[A-Za-z0-9]+\.about\.faq\.q\d+\.answer)<`)
)

// TestToolFAQItemsFollowTheTool: a page's own questions come from Tool.FAQ
// and nowhere else. On a page with FAQ: N the N pairs are in the JSON-LD and
// in the collapse list, in order, after the shared questions; on a page with
// FAQ: 0 — the home page among them — there are none.
func TestToolFAQItemsFollowTheTool(t *testing.T) {
	tpl := aboutPageTemplates(t)
	withFAQ, without := 0, 0
	for name, tool := range faqPages() {
		t.Run(name, func(t *testing.T) {
			out := renderFAQ(t, tpl, tool)
			var wantQ, wantPairs []string
			if tool != nil {
				for _, p := range tool.FaqPairs() {
					wantQ = append(wantQ, p[0])
					wantPairs = append(wantPairs, p[0]+"|"+p[1])
				}
			}
			if len(wantQ) > 0 {
				withFAQ++
			} else {
				without++
			}

			// JSON-LD: the tool's pairs, after the shared ones.
			echo := faqEchoRe.FindStringSubmatch(out)
			if echo == nil {
				t.Fatal("no faqSchema call in the FAQ section")
			}
			var gotPairs []string
			for _, p := range strings.Fields(echo[1]) {
				if strings.HasPrefix(p, "tool.") {
					gotPairs = append(gotPairs, p)
				}
			}
			if strings.Join(gotPairs, " ") != strings.Join(wantPairs, " ") {
				t.Errorf("schema tool pairs = %v, want %v", gotPairs, wantPairs)
			}
			if len(gotPairs) > 0 && !strings.HasSuffix(strings.TrimSpace(echo[1]), gotPairs[len(gotPairs)-1]) {
				t.Errorf("tool pairs are not last in the schema: %s", echo[1])
			}

			// Markup: the same questions, as collapse items after the shared list.
			var gotQ, gotA []string
			sharedLast := -1
			for i, m := range faqTitleRe.FindAllStringSubmatch(out, -1) {
				switch {
				case strings.HasPrefix(m[1], "tool."):
					gotQ = append(gotQ, m[1])
				default:
					sharedLast = i
					if len(gotQ) > 0 {
						t.Errorf("shared question %s rendered after a tool question", m[1])
					}
				}
			}
			if strings.Join(gotQ, " ") != strings.Join(wantQ, " ") {
				t.Errorf("collapse questions = %v, want %v", gotQ, wantQ)
			}
			if sharedLast < 0 {
				t.Error("no shared questions rendered")
			}
			for _, m := range toolFaqAnswerRe.FindAllStringSubmatch(out, -1) {
				gotA = append(gotA, m[1])
			}
			var wantA []string
			if tool != nil {
				for _, p := range tool.FaqPairs() {
					wantA = append(wantA, p[1])
				}
			}
			if strings.Join(gotA, " ") != strings.Join(wantA, " ") {
				t.Errorf("collapse answers = %v, want %v", gotA, wantA)
			}
		})
	}
	if withFAQ == 0 || without == 0 {
		t.Fatalf("%d pages with FAQ, %d without — one side of the check ran against nothing", withFAQ, without)
	}
}

// TestFAQAsksEachQuestionOnce: a guide's own FAQ repeats the shared how-to
// question it answers in full, so about.html leaves the shared item off that
// guide — in the collapse list and in the JSON-LD alike. Two Questions with
// one name in a FAQPage let Google pick the one-sentence answer for the
// query the guide exists to win. Keys are echoed, so the questions are
// resolved through en.json: the copy is where the duplicate is.
func TestFAQAsksEachQuestionOnce(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "locales", "en.json"))
	if err != nil {
		t.Fatal(err)
	}
	var en map[string]any
	if err := json.Unmarshal(b, &en); err != nil {
		t.Fatal(err)
	}
	text := func(key string) string {
		if s, ok := en[key].(string); ok {
			return s
		}
		return key
	}
	tpl := aboutPageTemplates(t)
	for name, tool := range faqPages() {
		t.Run(name, func(t *testing.T) {
			out := renderFAQ(t, tpl, tool)
			echo := faqEchoRe.FindStringSubmatch(out)
			if echo == nil {
				t.Fatal("no faqSchema call in the FAQ section")
			}
			var schema, shown []string
			for _, p := range strings.Fields(echo[1]) {
				schema = append(schema, strings.SplitN(p, "|", 2)[0])
			}
			for _, m := range faqTitleRe.FindAllStringSubmatch(out, -1) {
				shown = append(shown, m[1])
			}
			// The AI items are in the list and not in the schema (aiEnabled
			// is a stub here), so the two lists are checked apart.
			for where, keys := range map[string][]string{"schema": schema, "list": shown} {
				seen := map[string]string{}
				for _, k := range keys {
					if prev, ok := seen[text(k)]; ok {
						t.Errorf("%s asks %q twice: %s and %s", where, text(k), prev, k)
					}
					seen[text(k)] = k
				}
			}
		})
	}
}
