package ai_client

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	aoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

func TestProviderSelection(t *testing.T) {
	for _, tc := range []struct {
		anthropic, openai string
		want              Provider
	}{
		{"", "", ""}, {" \t", "\n", ""}, {"a", "", Anthropic},
		{"", "o", OpenAI}, {" \t", " o ", OpenAI}, {" a ", "o", Anthropic},
	} {
		t.Run(string(tc.want)+tc.anthropic+tc.openai, func(t *testing.T) {
			for _, name := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENAI_BASE_URL"} {
				t.Setenv(name, "")
			}
			set := flag.NewFlagSet("test", flag.ContinueOnError)
			for _, f := range RegisterFlags(nil) {
				f.Apply(set)
			}
			if err := set.Parse([]string{"--anthropic-api-key", tc.anthropic, "--openai-api-key", tc.openai}); err != nil {
				t.Fatal(err)
			}
			ctx := cli.NewContext(nil, set, nil)
			if got := ProviderFromCLI(ctx); got != tc.want {
				t.Fatalf("provider = %q, want %q", got, tc.want)
			}
			client := New(ctx)
			if tc.want == "" {
				if client != nil {
					t.Fatal("no credentials must return interface-nil")
				}
			} else if client == nil || client.Provider() != tc.want {
				t.Fatalf("client = %v, want %s", client, tc.want)
			}
		})
	}
}

func TestProviderCredentialsFromEnvironment(t *testing.T) {
	for _, provider := range []Provider{Anthropic, OpenAI} {
		t.Run(string(provider), func(t *testing.T) {
			t.Setenv("ANTHROPIC_API_KEY", "")
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv("OPENAI_BASE_URL", "https://example.test/v1")
			if provider == Anthropic {
				t.Setenv("ANTHROPIC_API_KEY", "a")
			} else {
				t.Setenv("OPENAI_API_KEY", "o")
			}
			set := flag.NewFlagSet("env", flag.ContinueOnError)
			for _, f := range RegisterFlags(nil) {
				f.Apply(set)
			}
			ctx := cli.NewContext(nil, set, nil)
			if ProviderFromCLI(ctx) != provider || New(ctx).Provider() != provider {
				t.Fatal("environment key did not select provider")
			}
			if ctx.String(FlagOpenAIBaseURL) != "https://example.test/v1" {
				t.Fatal("base URL environment override lost")
			}
		})
	}
}

func testClient(provider Provider, url string) Client {
	if provider == OpenAI {
		return newOpenAIClient("test-key", url+"/v1")
	}
	return newAnthropicClient("test-key", aoption.WithBaseURL(url))
}

func testRequest() Request {
	return Request{
		Model: "test-model", MaxTokens: 384, Temperature: 0,
		System:   []SystemBlock{{Text: "rules", Cache: true}, {Text: "fresh releases", Cache: true}},
		Messages: []Message{{Role: "user", Content: "old query"}, {Role: "assistant", Content: "old answer"}, {Role: "user", Content: "new query"}},
	}
}

func checkRequest(t *testing.T, r *http.Request, provider Provider, tool bool) {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
		return
	}
	if r.Method != http.MethodPost {
		t.Errorf("method = %s", r.Method)
	}
	if body["model"] != "test-model" {
		t.Errorf("model = %v", body["model"])
	}
	if provider == Anthropic {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("Anthropic path/auth: %s %s", r.URL.Path, r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-beta") != "prompt-caching-2024-07-31" {
			t.Error("cache beta header missing")
		}
		if body["max_tokens"] != float64(384) || body["temperature"] != float64(0) {
			t.Errorf("token/temperature: %v", body)
		}
		system, ok := body["system"].([]any)
		if !ok || len(system) != 2 {
			t.Errorf("system = %v", body["system"])
			return
		}
		for _, b := range system {
			if b.(map[string]any)["cache_control"].(map[string]any)["type"] != "ephemeral" {
				t.Error("cache breakpoint missing")
			}
		}
		messages := body["messages"].([]any)
		if len(messages) != 3 || messages[1].(map[string]any)["role"] != "assistant" {
			t.Errorf("history = %v", messages)
		}
		if tool && body["tool_choice"].(map[string]any)["name"] != "return_candidates" {
			t.Error("wrong tool choice")
		}
	} else {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("OpenAI path/auth: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		if body["max_output_tokens"] != float64(384) || body["store"] != false {
			t.Errorf("token/store: %v", body)
		}
		if _, exists := body["temperature"]; exists {
			t.Error("temperature must be omitted for reasoning-model compatibility")
		}
		if body["instructions"] != "rules\n\nfresh releases" {
			t.Errorf("instructions = %v", body["instructions"])
		}
		messages := body["input"].([]any)
		if len(messages) != 3 || messages[1].(map[string]any)["role"] != "assistant" || messages[2].(map[string]any)["content"] != "new query" {
			t.Errorf("history = %v", messages)
		}
		if tool {
			if body["tool_choice"].(map[string]any)["name"] != "return_candidates" || body["parallel_tool_calls"] != false {
				t.Error("forced tool choice missing")
			}
			function := body["tools"].([]any)[0].(map[string]any)
			if function["strict"] != false || function["parameters"].(map[string]any)["type"] != "object" {
				t.Errorf("function = %v", function)
			}
		}
	}
	if !tool && body["stream"] != true {
		t.Error("stream flag missing")
	}
}

func TestCallTool(t *testing.T) {
	for _, provider := range []Provider{Anthropic, OpenAI} {
		for _, tc := range []struct {
			name, tool, arguments, status string
			wantErr                       bool
		}{
			{"with_leading_text", "return_candidates", `{"candidates":[{"title":"Вот это драма","year":2026}]}`, "completed", false},
			{"wrong_tool", "other_tool", `{}`, "completed", true},
			{"no_tool", "", `{}`, "completed", true},
			{"invalid_arguments", "return_candidates", `{"candidates":`, "completed", true},
			{"empty_arguments", "return_candidates", "", "completed", true},
			{"incomplete", "return_candidates", `{"candidates":[]}`, "incomplete", true},
		} {
			if provider == Anthropic && (tc.name == "invalid_arguments" || tc.name == "incomplete") {
				continue
			}
			t.Run(string(provider)+"/"+tc.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					checkRequest(t, r, provider, true)
					w.Header().Set("Content-Type", "application/json")
					if provider == Anthropic {
						content := []any{map[string]any{"type": "text", "text": "thinking"}}
						if tc.tool != "" {
							block := map[string]any{"type": "tool_use", "id": "tool_1", "name": tc.tool}
							if tc.arguments != "" {
								block["input"] = json.RawMessage(tc.arguments)
							}
							content = append(content, block)
						}
						json.NewEncoder(w).Encode(map[string]any{"type": "message", "model": "returned-model", "stop_reason": "tool_use", "content": content, "usage": map[string]any{"input_tokens": 42, "output_tokens": 9}})
					} else {
						output := []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "thinking"}}}}
						if tc.tool != "" {
							output = append(output, map[string]any{"type": "function_call", "name": tc.tool, "arguments": tc.arguments, "call_id": "call_1"})
						}
						json.NewEncoder(w).Encode(map[string]any{"model": "returned-model", "status": tc.status, "output": output, "usage": map[string]any{"input_tokens": 42, "output_tokens": 9}})
					}
				}))
				defer server.Close()
				client := testClient(provider, server.URL)
				resp, err := client.CallTool(context.Background(), testRequest(), Tool{Name: "return_candidates", Properties: map[string]any{"candidates": map[string]any{"type": "array"}}, Required: []string{"candidates"}})
				if (err != nil) != tc.wantErr {
					t.Fatalf("response = %+v, error = %v, wantErr = %t", resp, err, tc.wantErr)
				}
				if !tc.wantErr && (string(resp.Input) != tc.arguments || resp.Usage.Model != "returned-model" || resp.Usage.InputTokens != 42 || resp.Usage.OutputTokens != 9) {
					t.Fatalf("response = %+v", resp)
				}
			})
		}
	}
}

func writeEvent(w http.ResponseWriter, event map[string]any) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
	w.(http.Flusher).Flush()
}

func TestStreamText(t *testing.T) {
	for _, provider := range []Provider{Anthropic, OpenAI} {
		t.Run(string(provider), func(t *testing.T) {
			firstDelta := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				checkRequest(t, r, provider, false)
				w.Header().Set("Content-Type", "text/event-stream")
				if provider == Anthropic {
					writeEvent(w, map[string]any{"type": "message_start", "message": map[string]any{"model": "returned-model", "usage": map[string]any{"input_tokens": 20, "cache_read_input_tokens": 10}}})
				}
				for i, text := range []string{`{"title":"Inter`, `stellar"}`} {
					if provider == Anthropic {
						writeEvent(w, map[string]any{"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": text}})
					} else {
						writeEvent(w, map[string]any{"type": "response.output_text.delta", "delta": text})
					}
					if i == 0 {
						select {
						case <-firstDelta:
						case <-r.Context().Done():
							return
						case <-time.After(3 * time.Second):
							t.Error("first delta was buffered until completion")
							return
						}
					}
				}
				if provider == Anthropic {
					writeEvent(w, map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"input_tokens": 42, "output_tokens": 9, "cache_read_input_tokens": 30, "cache_creation_input_tokens": 5}})
					writeEvent(w, map[string]any{"type": "message_stop"})
				} else {
					writeEvent(w, map[string]any{"type": "response.completed", "response": map[string]any{"model": "returned-model", "status": "completed", "usage": map[string]any{"input_tokens": 42, "output_tokens": 9, "input_tokens_details": map[string]any{"cached_tokens": 30, "cache_write_tokens": 5}}}})
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var deltas []string
			usage, err := testClient(provider, server.URL).StreamText(ctx, testRequest(), func(delta string, _ Usage) {
				deltas = append(deltas, delta)
				if len(deltas) == 1 {
					close(firstDelta)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(deltas, []string{`{"title":"Inter`, `stellar"}`}) {
				t.Fatalf("deltas = %q", deltas)
			}
			if usage.Model != "returned-model" || usage.InputTokens != 42 || usage.OutputTokens != 9 || usage.CacheReadTokens != 30 || usage.CacheCreateTokens != 5 {
				t.Fatalf("final usage = %+v", usage)
			}
		})
	}
}

func TestStreamFailures(t *testing.T) {
	for _, provider := range []Provider{Anthropic, OpenAI} {
		for _, mode := range []string{"truncated", "api_error", "bare_api_error", "invalid_json", "cancelled", "incomplete", "failed"} {
			if provider == Anthropic && (mode == "incomplete" || mode == "failed") {
				continue
			}
			t.Run(string(provider)+"/"+mode, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					if mode == "api_error" {
						writeEvent(w, map[string]any{"type": "error", "code": "rate_limit", "message": "failed", "error": map[string]any{"type": "overloaded_error", "message": "failed"}})
						return
					}
					if mode == "bare_api_error" {
						writeEvent(w, map[string]any{"type": "error", "code": "rate_limit", "message": "failed"})
						return
					}
					if mode == "incomplete" || mode == "failed" {
						writeEvent(w, map[string]any{"type": "response." + mode, "response": map[string]any{"status": mode, "incomplete_details": map[string]any{"reason": "max_output_tokens"}}})
						return
					}
					if mode == "invalid_json" {
						fmt.Fprint(w, "event: content_block_delta\ndata: {invalid\n\n")
						return
					}
					if provider == Anthropic {
						writeEvent(w, map[string]any{"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": "partial"}})
					} else {
						writeEvent(w, map[string]any{"type": "response.output_text.delta", "delta": "partial"})
					}
					if mode == "cancelled" {
						<-r.Context().Done()
					}
				}))
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, err := testClient(provider, server.URL).StreamText(ctx, testRequest(), func(string, Usage) {
					if mode == "cancelled" {
						cancel()
					}
				})
				if err == nil {
					t.Fatal("expected stream failure")
				}
				if mode == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation = %v", err)
				}
				if mode == "truncated" && !strings.Contains(err.Error(), "ended without") {
					t.Fatalf("truncation = %v", err)
				}
			})
		}
	}
}
