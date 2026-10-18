package i18n

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestFaqSchemaAppendsThePageListAfterTheLiteralPairs: the literal "q|a"
// pairs are the shared FAQ and the slice is the page's own (Tool.FaqPairs);
// the page's items come last, tags are stripped from answers, and a pair
// whose translation is missing is dropped rather than emitted half-empty.
func TestFaqSchemaAppendsThePageListAfterTheLiteralPairs(t *testing.T) {
	h := helperWith(t, map[string]string{
		"en.json": `{"shared.q": "Shared?", "shared.a": "Yes.", "page.q": "Page?", "page.a": "<strong>Bold</strong> answer."}`,
	})
	out := string(h.FaqSchema("en", [][2]string{{"page.q", "page.a"}, {"missing.q", "missing.a"}}, "shared.q|shared.a"))
	raw := strings.TrimSuffix(strings.TrimPrefix(out, `<script type="application/ld+json">`), `</script>`)
	var page struct {
		MainEntity []struct {
			Name           string `json:"name"`
			AcceptedAnswer struct {
				Text string `json:"text"`
			} `json:"acceptedAnswer"`
		} `json:"mainEntity"`
	}
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatalf("not a JSON-LD block: %v\n%s", err, out)
	}
	if len(page.MainEntity) != 2 || page.MainEntity[0].Name != "Shared?" || page.MainEntity[1].Name != "Page?" {
		t.Fatalf("items = %+v, want Shared? then Page?", page.MainEntity)
	}
	if got := page.MainEntity[1].AcceptedAnswer.Text; got != "Bold answer." {
		t.Errorf("tags not stripped from the page answer: %q", got)
	}
	if h.FaqSchema("en", nil) != "" {
		t.Error("no pairs should render no block")
	}
}
