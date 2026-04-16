package recommendations

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/redis/go-redis/v9"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

// decodeBlocks parses raw JSON into a slice of ContentBlockUnion the way
// the SDK does on the wire. Lets us build realistic fixtures without
// instantiating the real HTTP client.
func decodeBlocks(t *testing.T, raw string) []anthropic.ContentBlockUnion {
	t.Helper()
	var out []anthropic.ContentBlockUnion
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode blocks: %v", err)
	}
	return out
}

func TestExtractToolUseInput_HappyPath(t *testing.T) {
	blocks := decodeBlocks(t, `[
		{"type": "text", "text": "Okay, here you go:"},
		{"type": "tool_use", "id": "toolu_1", "name": "return_recommendations",
		 "input": {"items": [{"title": "Interstellar", "year": 2014, "reason": "because you liked Tenet"}]}}
	]`)

	raw, err := extractToolUseInput(blocks, "return_recommendations")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	var payload struct {
		Items []claudeItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Title != "Interstellar" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestExtractToolUseInput_NoMatchingTool(t *testing.T) {
	blocks := decodeBlocks(t, `[
		{"type": "text", "text": "I cannot help with that."}
	]`)
	_, err := extractToolUseInput(blocks, "return_recommendations")
	if err == nil {
		t.Fatal("expected error when no tool_use block is present")
	}
}

func TestExtractToolUseInput_WrongToolName(t *testing.T) {
	blocks := decodeBlocks(t, `[
		{"type": "tool_use", "id": "toolu_2", "name": "some_other_tool",
		 "input": {"anything": "goes"}}
	]`)
	_, err := extractToolUseInput(blocks, "return_recommendations")
	if err == nil {
		t.Fatal("expected error when tool name mismatches")
	}
}

func TestExtractToolUseInput_SkipsNonMatchingBlocks(t *testing.T) {
	// Claude sometimes emits a thinking block or a leading text block even
	// with tool_choice forced — make sure we keep scanning.
	blocks := decodeBlocks(t, `[
		{"type": "text", "text": "Thinking..."},
		{"type": "tool_use", "id": "t1", "name": "return_chips",
		 "input": {"chips": [{"label": "x", "query": "y"}]}}
	]`)
	raw, err := extractToolUseInput(blocks, "return_chips")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(string(raw), `"chips"`) {
		t.Fatalf("unexpected content: %s", raw)
	}
}

func TestChipsCacheKey_DeterministicAndTimeBucketed(t *testing.T) {
	uid := uuid.NewV4()
	uc := &UserContext{Locale: "en", DayOfWeek: "Monday", TimeOfDay: "evening"}

	k1 := chipsCacheKey(uid, uc)
	k2 := chipsCacheKey(uid, uc)
	if k1 != k2 {
		t.Fatalf("key not deterministic: %q vs %q", k1, k2)
	}

	uc2 := &UserContext{Locale: "en", DayOfWeek: "Monday", TimeOfDay: "morning"}
	if chipsCacheKey(uid, uc2) == k1 {
		t.Fatal("morning and evening must get different cache slots")
	}

	// Each supported locale must get its own cache slot — otherwise a user
	// flipping the language switcher would see chips in the previous
	// locale until the TTL expires.
	seen := map[string]string{"en": k1}
	for _, loc := range []string{"ru", "es", "de", "fr", "pt", "it"} {
		ucL := &UserContext{Locale: loc, DayOfWeek: "Monday", TimeOfDay: "evening"}
		k := chipsCacheKey(uid, ucL)
		for prevLoc, prevKey := range seen {
			if k == prevKey {
				t.Fatalf("locales %q and %q must get different cache slots, both got %q", loc, prevLoc, k)
			}
		}
		seen[loc] = k
	}

	if chipsCacheKey(uuid.NewV4(), uc) == k1 {
		t.Fatal("different users must get different cache slots")
	}
}

func TestNormalizeLocale(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Direct hits for each supported locale.
		{"en", "en"},
		{"ru", "ru"},
		{"es", "es"},
		{"de", "de"},
		{"fr", "fr"},
		{"pt", "pt"},
		{"it", "it"},
		// Country / region subtags get clipped to the 2-letter base.
		{"en-US", "en"},
		{"ru-RU", "ru"},
		{"pt-BR", "pt"},
		{"pt-PT", "pt"},
		{"es-419", "es"},
		// Whitespace and case are tolerated.
		{"  EN  ", "en"},
		{"De-de", "de"},
		// Anything we don't teach Claude about falls back to en.
		{"zh", "en"},
		{"ja", "en"},
		{"ar", "en"},
		{"", "en"},
		{"x", "en"}, // too short
		// Defensive: the prefix matches a supported locale but the full
		// string is some weird tag — we still take the prefix.
		{"english", "en"},
	}
	for _, c := range cases {
		got := normalizeLocale(c.in)
		if got != c.want {
			t.Errorf("normalizeLocale(%q): want %q, got %q", c.in, c.want, got)
		}
	}
}

func TestDefaultChips_PerLocaleCoverage(t *testing.T) {
	// Every locale registered in supportedLocales must have a chip set —
	// otherwise users on that language fall through to English chips and
	// see a mismatched locale in their cold-start experience.
	for loc := range supportedLocales {
		chips := defaultChips(loc)
		if len(chips) == 0 {
			t.Errorf("locale %q: defaultChips returned empty set", loc)
			continue
		}
		// Sanity: labels must be non-empty and queries must be present.
		for i, c := range chips {
			if c.Label == "" {
				t.Errorf("locale %q chip[%d]: empty label", loc, i)
			}
			if c.Query == "" {
				t.Errorf("locale %q chip[%d]: empty query", loc, i)
			}
		}
	}

	// Unknown locale falls back to English (not empty, not a panic).
	chips := defaultChips("zz")
	if len(chips) == 0 {
		t.Error("unknown locale fallback returned empty set")
	}
}

func TestShortHash_StableAndHex(t *testing.T) {
	a := shortHash("Интерстеллар")
	b := shortHash("Интерстеллар")
	if a != b {
		t.Fatalf("hash not stable: %q vs %q", a, b)
	}
	if len(a) != 12 {
		t.Fatalf("want 12 hex chars, got %d (%s)", len(a), a)
	}
	if shortHash("other") == a {
		t.Fatal("hashes must differ for different inputs")
	}
}

// --- RedisChipsCache (Redis wire compat via miniredis) ---

func TestRedisChipsCache_RoundTrip(t *testing.T) {
	mr := miniredis.RunT(t)
	cl := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	c := NewRedisChipsCache(cl)

	ctx := context.Background()
	if v, err := c.Get(ctx, "absent"); err != nil || v != nil {
		t.Fatalf("absent: got v=%v err=%v, want nil/nil", v, err)
	}

	val := &ChipsResponse{
		Chips: []Chip{
			{ID: "a1", Label: "Sleepy Monday", Query: "cozy films"},
		},
		GeneratedAt: 1712000000,
		Tier:        "paid",
	}
	if err := c.Set(ctx, "k1", val, 1*time.Hour); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, err := c.Get(ctx, "k1")
	if err != nil || got == nil {
		t.Fatalf("get after set: got=%v err=%v", got, err)
	}
	if len(got.Chips) != 1 || got.Chips[0].Label != "Sleepy Monday" {
		t.Fatalf("unexpected payload: %+v", got)
	}

	if err := c.Del(ctx, "k1"); err != nil {
		t.Fatalf("del: %v", err)
	}
	if got, _ := c.Get(ctx, "k1"); got != nil {
		t.Fatalf("expected nil after del, got %+v", got)
	}
}

func TestRedisChipsCache_CorruptEntryIsMiss(t *testing.T) {
	mr := miniredis.RunT(t)
	cl := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	c := NewRedisChipsCache(cl)
	ctx := context.Background()

	// Put invalid JSON directly under the prefixed key.
	if err := cl.Set(ctx, "ai_rec:chips:garbage", "{not: json", time.Hour).Err(); err != nil {
		t.Fatalf("raw set: %v", err)
	}
	got, err := c.Get(ctx, "garbage")
	if err != nil {
		t.Fatalf("get corrupt should not error: %v", err)
	}
	if got != nil {
		t.Fatalf("corrupt entry must be treated as miss, got %+v", got)
	}
}

func TestMemoryChipsCache_RoundTrip(t *testing.T) {
	c := newMemoryChipsCache()
	ctx := context.Background()

	if v, _ := c.Get(ctx, "k"); v != nil {
		t.Fatal("expected miss")
	}
	_ = c.Set(ctx, "k", &ChipsResponse{Tier: "free"}, time.Hour)
	got, _ := c.Get(ctx, "k")
	if got == nil || got.Tier != "free" {
		t.Fatalf("unexpected: %+v", got)
	}
	_ = c.Del(ctx, "k")
	if v, _ := c.Get(ctx, "k"); v != nil {
		t.Fatal("expected miss after del")
	}
}

// --- Prompt helpers (validate they actually contain expected bits) ---

func TestUserPromptForRecommend_ContainsContextAndQuery(t *testing.T) {
	uc := &UserContext{
		Locale:      "ru",
		DayOfWeek:   "Monday",
		TimeOfDay:   "evening",
		LocalHour:   20,
		HistoryText: "- Interstellar (2014) [liked]",
		HistorySize: 1,
	}
	p := userPromptForRecommend(uc, "тупые фильмы про космонавтов", 8, 15)

	// "NDJSON" replaces the old `return_recommendations` tool reference —
	// the streaming pipeline asks Claude for plain-text NDJSON output, not
	// a tool_use call.
	for _, needle := range []string{"Monday", "evening", "ru", "Interstellar", "тупые фильмы про космонавтов", "NDJSON"} {
		if !strings.Contains(p, needle) {
			t.Errorf("prompt missing %q:\n%s", needle, p)
		}
	}
}

func TestUserPromptForRecommend_NewUserNoHistoryBlock(t *testing.T) {
	uc := &UserContext{
		Locale: "en", DayOfWeek: "Friday", TimeOfDay: "night", LocalHour: 23,
	}
	p := userPromptForRecommend(uc, "something scary", 8, 15)
	if !strings.Contains(p, "new user") {
		t.Errorf("expected cold-start marker:\n%s", p)
	}
}

func TestBucketTimeOfDay(t *testing.T) {
	cases := []struct {
		hour int
		want string
	}{
		{5, "morning"}, {11, "morning"},
		{12, "afternoon"}, {16, "afternoon"},
		{17, "evening"}, {22, "evening"},
		{23, "night"}, {0, "night"}, {4, "night"},
	}
	for _, c := range cases {
		if got := bucketTimeOfDay(c.hour); got != c.want {
			t.Errorf("hour %d: want %q, got %q", c.hour, c.want, got)
		}
	}
}

func TestClientClock_Validation(t *testing.T) {
	valid := ClientClock{DayOfWeek: "monday", Hour: 14}
	if !valid.IsValid() {
		t.Fatal("lowercase Monday 14:00 should be valid")
	}
	if c := valid.Canonical(); c.DayOfWeek != "Monday" {
		t.Fatalf("canonical: want Monday, got %q", c.DayOfWeek)
	}

	cases := []ClientClock{
		{DayOfWeek: "", Hour: 12},
		{DayOfWeek: "Funday", Hour: 12},
		{DayOfWeek: "Monday", Hour: -1},
		{DayOfWeek: "Monday", Hour: 24},
	}
	for _, c := range cases {
		if c.IsValid() {
			t.Errorf("%+v should be invalid", c)
		}
	}
}

// --- History rendering ---

func TestRenderHistory_TagPriority(t *testing.T) {
	one := int16(1)
	neg := int16(-1)
	rows := []models.RatedMovie{
		{Title: "Liked Movie", Rating: &one, Watched: true},
		{Title: "Disliked Movie", Rating: &neg, Watched: true},
		{Title: "Just Watched", Watched: true},
		{Title: "Queued Only"},
	}
	out := renderHistory(rows)
	if !strings.Contains(out, "Liked Movie") || !strings.Contains(out, "[liked]") {
		t.Errorf("missing liked tag: %s", out)
	}
	if !strings.Contains(out, "[disliked]") {
		t.Errorf("missing disliked tag: %s", out)
	}
	if !strings.Contains(out, "[watched]") {
		t.Errorf("missing watched tag: %s", out)
	}
	if !strings.Contains(out, "[queued]") {
		t.Errorf("missing queued tag: %s", out)
	}
}

func TestRenderHistory_EmptyReturnsEmpty(t *testing.T) {
	if renderHistory(nil) != "" {
		t.Fatal("expected empty string for nil rows")
	}
}
