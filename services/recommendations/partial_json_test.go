package recommendations

import (
	"encoding/json"
	"testing"
)

func collectNDJSON(t *testing.T, chunks ...string) []claudeItem {
	t.Helper()
	var got []claudeItem
	ex := newNDJSONItemsExtractor(func(raw json.RawMessage) {
		var item claudeItem
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Errorf("invalid item JSON: %s — %v", raw, err)
			return
		}
		got = append(got, item)
	})
	for _, c := range chunks {
		ex.write(c)
	}
	return got
}

func TestNDJSONItemsExtractor_OneObjectPerLine(t *testing.T) {
	got := collectNDJSON(t, `{"title":"A","year":1999,"reason":"x"}
{"title":"B","year":2020,"reason":"y"}
{"title":"C","year":2024,"reason":"z"}`)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d: %+v", len(got), got)
	}
	if got[0].Title != "A" || got[2].Title != "C" {
		t.Fatalf("wrong order/content: %+v", got)
	}
}

func TestNDJSONItemsExtractor_OneByteAtATime(t *testing.T) {
	src := `{"title":"A","year":1,"reason":"x"}
{"title":"B","year":2,"reason":"y"}`
	chunks := make([]string, len(src))
	for i := 0; i < len(src); i++ {
		chunks[i] = string(src[i])
	}
	got := collectNDJSON(t, chunks...)
	if len(got) != 2 || got[0].Title != "A" || got[1].Title != "B" {
		t.Fatalf("byte-by-byte streaming failed: %+v", got)
	}
}

func TestNDJSONItemsExtractor_ArbitraryWhitespace(t *testing.T) {
	got := collectNDJSON(t,
		`   {"title":"A","year":1,"reason":"x"}    {"title":"B","year":2,"reason":"y"}   `,
	)
	if len(got) != 2 {
		t.Fatalf("whitespace handling failed: %+v", got)
	}
}

func TestNDJSONItemsExtractor_StringWithBraces(t *testing.T) {
	got := collectNDJSON(t,
		`{"title":"X","year":1,"reason":"a } b { c"}`,
	)
	if len(got) != 1 || got[0].Reason != "a } b { c" {
		t.Fatalf("string-content braces broke scanner: %+v", got)
	}
}

func TestNDJSONItemsExtractor_DoesNotEmitPartialObject(t *testing.T) {
	var emitted int
	ex := newNDJSONItemsExtractor(func(_ json.RawMessage) { emitted++ })
	ex.write(`{"title":"Halfway","year":2024,"reaso`)
	if emitted != 0 {
		t.Fatalf("emitted before close brace: %d", emitted)
	}
	ex.write(`n":"yes"}`)
	if emitted != 1 {
		t.Fatalf("should have emitted 1, got %d", emitted)
	}
}

func TestNDJSONItemsExtractor_LeadingPreambleIsSkipped(t *testing.T) {
	// Claude sometimes prepends a tiny bit of commentary despite our
	// instructions. Anything before the first '{' is harmless to the
	// scanner — it stays at depth 0, ignoring non-brace chars.
	got := collectNDJSON(t,
		`Sure! Here you go:
{"title":"A","year":1,"reason":"x"}`,
	)
	if len(got) != 1 || got[0].Title != "A" {
		t.Fatalf("preamble skipping failed: %+v", got)
	}
}

func TestNDJSONItemsExtractor_NestedObjectInsideReason(t *testing.T) {
	// Reason as a string that itself looks like JSON should not confuse
	// depth tracking.
	got := collectNDJSON(t,
		`{"title":"X","year":1999,"reason":"like {a:1, b:2}"}`,
	)
	if len(got) != 1 || got[0].Reason != "like {a:1, b:2}" {
		t.Fatalf("nested-looking string broke scanner: %+v", got)
	}
}
