package ai_client

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/pkg/errors"
)

type openAIClient struct{ client openai.Client }

func newOpenAIClient(key, baseURL string, opts ...option.RequestOption) Client {
	opts = append([]option.RequestOption{option.WithAPIKey(key), option.WithBaseURL(baseURL)}, opts...)
	return &openAIClient{client: openai.NewClient(opts...)}
}

func (*openAIClient) Provider() Provider { return OpenAI }

func openAIParams(req Request) responses.ResponseNewParams {
	var system []string
	for _, block := range req.System {
		system = append(system, block.Text)
	}
	params := responses.ResponseNewParams{
		Model: req.Model, MaxOutputTokens: openai.Int(req.MaxTokens),
		Instructions: openai.String(strings.Join(system, "\n\n")),
		Store:        openai.Bool(false),
		// Do not send temperature: reasoning models reject it. Each model's
		// default sampling is used, while prompts and output parsing are shared.
	}
	var messages responses.ResponseInputParam
	for _, message := range req.Messages {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		messages = append(messages, responses.ResponseInputItemUnionParam{OfMessage: &responses.EasyInputMessageParam{
			Role:    responses.EasyInputMessageRole(message.Role),
			Content: responses.EasyInputMessageContentUnionParam{OfString: openai.String(message.Content)},
		}})
	}
	params.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: messages}
	return params
}

func openAIUsage(resp responses.Response) Usage {
	return Usage{
		Model: resp.Model, InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens,
		CacheReadTokens:   resp.Usage.InputTokensDetails.CachedTokens,
		CacheCreateTokens: resp.Usage.InputTokensDetails.CacheWriteTokens,
		StopReason:        string(resp.Status),
	}
}

func openAIResponseError(resp responses.Response) error {
	if resp.Status == responses.ResponseStatusCompleted {
		return nil
	}
	return errors.Errorf("openai response %s: %s %s", resp.Status, resp.Error.Message, resp.IncompleteDetails.Reason)
}

func (c *openAIClient) CallTool(ctx context.Context, req Request, tool Tool) (ToolResponse, error) {
	params := openAIParams(req)
	params.Tools = []responses.ToolUnionParam{{OfFunction: &responses.FunctionToolParam{
		Name: tool.Name, Description: openai.String(tool.Description),
		// Preserve existing optional properties in the shared tool schemas.
		Strict:     openai.Bool(false),
		Parameters: map[string]any{"type": "object", "properties": tool.Properties, "required": tool.Required},
	}}}
	params.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{OfFunctionTool: &responses.ToolChoiceFunctionParam{Name: tool.Name}}
	params.ParallelToolCalls = openai.Bool(false)
	resp, err := c.client.Responses.New(ctx, params)
	if err != nil {
		return ToolResponse{}, errors.Wrap(err, "openai responses.new")
	}
	usage := openAIUsage(*resp)
	if err := openAIResponseError(*resp); err != nil {
		return ToolResponse{Usage: usage}, err
	}
	for _, item := range resp.Output {
		if item.Type == "function_call" && item.Name == tool.Name {
			raw := json.RawMessage(item.AsFunctionCall().Arguments)
			if !json.Valid(raw) {
				return ToolResponse{Usage: usage}, errors.Errorf("openai: invalid tool arguments for %s", tool.Name)
			}
			return ToolResponse{Input: raw, Usage: usage}, nil
		}
	}
	return ToolResponse{Usage: usage}, errors.Errorf("openai: tool %s not called", tool.Name)
}

func (c *openAIClient) StreamText(ctx context.Context, req Request, onText func(string, Usage)) (Usage, error) {
	stream := c.client.Responses.NewStreaming(ctx, openAIParams(req))
	defer stream.Close()
	usage := Usage{Model: req.Model}
	complete := false
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" {
				onText(event.Delta, usage)
			}
		case "response.completed":
			usage = openAIUsage(event.Response)
			if err := openAIResponseError(event.Response); err != nil {
				return usage, err
			}
			complete = true
		case "response.failed", "response.incomplete":
			return openAIUsage(event.Response), openAIResponseError(event.Response)
		case "error":
			return usage, errors.Errorf("openai stream: %s: %s", event.Code, event.Message)
		}
	}
	if err := stream.Err(); err != nil {
		return usage, errors.Wrap(err, "openai stream")
	}
	if err := ctx.Err(); err != nil {
		return usage, err
	}
	if !complete {
		return usage, errors.New("openai stream ended without response.completed")
	}
	return usage, nil
}
