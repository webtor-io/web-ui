package recommendations

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urfave/cli"
	ac "github.com/webtor-io/web-ui/services/ai_client"
)

func aiTestCLI(t *testing.T, args ...string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("ai", flag.ContinueOnError)
	for _, f := range RegisterFlags(ac.RegisterFlags(nil)) {
		switch f := f.(type) {
		case cli.StringFlag:
			t.Setenv(f.EnvVar, "")
		case cli.BoolFlag:
			t.Setenv(f.EnvVar, "")
		case cli.IntFlag:
			t.Setenv(f.EnvVar, "")
		}
		f.Apply(set)
	}
	if err := set.Parse(args); err != nil {
		t.Fatal(err)
	}
	return cli.NewContext(nil, set, nil)
}

func TestAIConfigDefaultsAndVisibility(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		model   string
		visible bool
	}{
		{"no_key", []string{"--ai-recommendations-enabled"}, ac.DefaultAnthropicModel, false},
		{"openai_disabled", []string{"--openai-api-key", "o"}, ac.DefaultOpenAIModel, false},
		{"openai", []string{"--ai-recommendations-enabled", "--openai-api-key", "o"}, ac.DefaultOpenAIModel, true},
		{"anthropic", []string{"--ai-recommendations-enabled", "--anthropic-api-key", "a"}, ac.DefaultAnthropicModel, true},
		{"both", []string{"--ai-recommendations-enabled", "--anthropic-api-key", "a", "--openai-api-key", "o"}, ac.DefaultAnthropicModel, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := aiTestCLI(t, tc.args...)
			cfg := ConfigFromCLI(ctx)
			for _, tier := range []Tier{TierFree, TierPaid} {
				if got := cfg.ResolveModel(tier); got != tc.model {
					t.Fatalf("model = %s, want %s", got, tc.model)
				}
			}
			if got := cfg.ResolveChipsModel(); got != tc.model {
				t.Fatalf("chips model = %s", got)
			}
			if got := NewHelper(ctx).AiEnabled(); got != tc.visible {
				t.Fatalf("visible = %t, want %t", got, tc.visible)
			}
		})
	}
}

func TestAIModelOverrides(t *testing.T) {
	cfg := ConfigFromCLI(aiTestCLI(t, "--openai-api-key", "o", "--ai-recommendations-model", "common", "--ai-recommendations-paid-model", "paid"))
	if cfg.ResolveModel(TierFree) != "common" || cfg.ResolveModel(TierPaid) != "paid" || cfg.ResolveChipsModel() != "common" {
		t.Fatalf("override chain = %+v", cfg)
	}
	cfg.FreeModel = "free"
	cfg.ChipsModel = "chips"
	if cfg.ResolveModel(TierFree) != "free" || cfg.ResolveChipsModel() != "chips" {
		t.Fatal("specific overrides lost")
	}
}

func TestOpenAIRecommendationAndChipConsumers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var request struct {
			Stream       bool   `json:"stream"`
			Instructions string `json:"instructions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if !request.Stream {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"completed","model":"gpt-4.1-mini","output":[{"type":"function_call","name":"return_chips","arguments":"{\"chips\":[{\"label\":\"Space\",\"query\":\"space films\"}]}"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// Both flows parse incrementally despite JSON objects split across deltas.
		deltas := []string{`{"title":"Inter`, `stellar","year":2014,"reason":"space"}`}
		if request.Instructions == systemPromptChips {
			deltas = []string{`{"label":"Space",`, `"query":"space films","icon":""}`}
		}
		for _, delta := range deltas {
			fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\n", delta)
			w.(http.Flusher).Flush()
		}
		fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"gpt-4.1-mini\",\"usage\":{\"input_tokens\":100,\"output_tokens\":30}}}\n\n")
	}))
	defer server.Close()
	ctx := aiTestCLI(t, "--openai-api-key", "o", "--openai-base-url", server.URL+"/v1")
	s := &AIService{client: ac.New(ctx), freeModel: ac.DefaultOpenAIModel, chipsModel: ac.DefaultOpenAIModel}
	items := make(chan recommendationItem, 2)
	if err := s.streamAIItemsText(context.Background(), &UserContext{Locale: "en"}, "space films", []Message{{Role: "user", Content: "previous query"}, {Role: "assistant", Content: "previous answer"}}, TierFree, items); err != nil {
		t.Fatal(err)
	}
	item, ok := <-items
	if !ok || item.Title != "Interstellar" || item.Year != 2014 {
		t.Fatalf("item = %+v", item)
	}
	if _, ok := <-items; ok {
		t.Fatal("item channel must close")
	}
	chips := make(chan Chip, 2)
	if err := s.streamAIChipsText(context.Background(), "suggest chips", TierFree, chips); err != nil {
		t.Fatal(err)
	}
	chip, ok := <-chips
	if !ok || chip.Label != "Space" || chip.Query != "space films" || strings.TrimSpace(chip.ID) == "" {
		t.Fatalf("chip = %+v", chip)
	}
	if _, ok := <-chips; ok {
		t.Fatal("chip channel must close")
	}
	plainChips, err := s.callAIForChips(context.Background(), "suggest chips", TierFree)
	if err != nil || len(plainChips) != 1 || plainChips[0].Label != "Space" {
		t.Fatalf("non-streaming chips = %+v, err = %v", plainChips, err)
	}
}
