package enrich

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	ac "github.com/webtor-io/web-ui/services/ai_client"
)

func TestOpenAIEnrichment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model      string `json:"model"`
			ToolChoice struct {
				Name string `json:"name"`
			} `json:"tool_choice"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != ac.DefaultOpenAIModel || body.ToolChoice.Name != aiResolveToolName {
			t.Errorf("request = %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"completed","model":"gpt-4.1-mini","output":[{"type":"function_call","name":"return_candidates","arguments":"{\"candidates\":[{\"title\":\"Вот это драма\",\"year\":2026,\"language\":\"ru\"},{\"title\":\"The Drama\",\"year\":null}]}"}],"usage":{"input_tokens":100,"output_tokens":30}}`)
	}))
	defer server.Close()
	set := flag.NewFlagSet("enrich", flag.ContinueOnError)
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
	if err := set.Parse([]string{"--ai-enrich-enabled", "--openai-api-key", "o", "--openai-base-url", server.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	ctx := cli.NewContext(nil, set, nil)
	resolver := New(ctx, ac.New(ctx), &cs.PG{})
	if resolver == nil || resolver.model != ac.DefaultOpenAIModel {
		t.Fatalf("resolver = %+v", resolver)
	}
	year := int16(2026)
	candidates, err := resolver.callAI(context.Background(), "Vot.eto.drama.2026.mkv", "Vot eto drama", &year, models.ContentTypeMovie)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].Title != "Вот это драма" || candidates[0].Year == nil || *candidates[0].Year != 2026 || candidates[1].Year != nil {
		t.Fatalf("candidates = %+v", candidates)
	}
	if err := set.Set(aiResolveModelFlag, "custom-openai-model"); err != nil {
		t.Fatal(err)
	}
	if got := New(ctx, ac.New(ctx), &cs.PG{}).model; got != "custom-openai-model" {
		t.Fatalf("explicit model = %q", got)
	}
}
