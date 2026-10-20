package template

import (
	"bytes"
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The privacy policy and the FAQ name the AI provider ai_client selects from
// the configured keys (aiProvider), never a fixed one: production runs on
// OpenAI with an empty Anthropic key, and both pages named Anthropic.

func renderPrivacy(t *testing.T, provider string) string {
	t.Helper()
	tpl, err := template.New("privacy.html").
		Funcs(template.FuncMap{"aiProvider": func() string { return provider }}).
		ParseFiles("../../templates/views/legal/privacy.html")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "main", nil); err != nil {
		t.Fatal(err)
	}
	return whitespace.ReplaceAllString(buf.String(), " ")
}

func TestPrivacyNamesTheConfiguredAIProvider(t *testing.T) {
	for _, tc := range []struct {
		provider string
		want     []string
		absent   []string
	}{
		{"openai", []string{
			`href="https://openai.com"`,
			"OpenAI Ireland Limited",
			"Standard Contractual Clauses",
			"first request to OpenAI",
			"OpenAI retains API inputs and outputs in abuse-monitoring logs for up to 30&nbsp;days",
		}, []string{"Anthropic"}},
		{"anthropic", []string{
			"Anthropic, PBC",
			"Standard Contractual Clauses",
			"first request to Anthropic",
			"Anthropic retains API inputs and outputs for up to 30 days",
		}, []string{"OpenAI"}},
		{"", []string{"your watch history and queries are not sent to any AI provider"}, []string{"Anthropic", "OpenAI", "first request to"}},
	} {
		t.Run("provider="+tc.provider, func(t *testing.T) {
			out := renderPrivacy(t, tc.provider)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q", w)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(out, a) {
					t.Errorf("names %q", a)
				}
			}
			// Neither company is on dataprivacyframework.gov (whole list,
			// active and inactive, checked 2026-10-20); both DPAs use SCCs.
			if strings.Contains(out, "Data Privacy Framework") {
				t.Error("claims a Data Privacy Framework certification")
			}
		})
	}
}

func TestFAQNamesTheConfiguredAIProvider(t *testing.T) {
	for provider, key := range map[string]string{
		"openai":    "about.faq.aiModel.openai",
		"anthropic": "about.faq.aiModel.anthropic",
	} {
		t.Run(provider, func(t *testing.T) {
			funcs := aboutPageFuncs()
			funcs["aiProvider"] = func() string { return provider }
			out := renderFAQ(t, parseAboutTemplates(t, funcs,
				"../../templates/partials/about.html",
				"../../templates/partials/about/*.html"), nil)
			if !strings.Contains(out, ">"+key+"<") {
				t.Errorf("FAQ does not render %s", key)
			}
			if n := strings.Count(out, "about.faq.aiModel."); n != 2 { // .q and the answer
				t.Errorf("%d aiModel keys rendered, want the question and one answer", n)
			}
		})
	}
}

// TestAIModelAnswerNamesItsProviderInEveryLocale: a translation copied from
// the other provider's key would name the wrong company in that language.
func TestAIModelAnswerNamesItsProviderInEveryLocale(t *testing.T) {
	paths, err := filepath.Glob("../../locales/*.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no locales: %v", err)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		for key, own := range map[string][2]string{
			"about.faq.aiModel.anthropic": {"Anthropic", "OpenAI"},
			"about.faq.aiModel.openai":    {"OpenAI", "Anthropic"},
		} {
			if v, _ := m[key].(string); !strings.Contains(v, own[0]) || strings.Contains(v, own[1]) {
				t.Errorf("%s %s = %q: want %s, not %s", filepath.Base(p), key, v, own[0], own[1])
			}
		}
	}
}
