package ai_client

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/pkg/errors"
)

type anthropicClient struct{ client anthropic.Client }

func newAnthropicClient(key string, opts ...option.RequestOption) Client {
	opts = append([]option.RequestOption{
		option.WithAPIKey(key),
		option.WithHeader("anthropic-beta", "prompt-caching-2024-07-31"),
	}, opts...)
	return &anthropicClient{client: anthropic.NewClient(opts...)}
}

func (*anthropicClient) Provider() Provider { return Anthropic }

func anthropicParams(req Request) anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model: anthropic.Model(req.Model), MaxTokens: req.MaxTokens,
		Temperature: anthropic.Float(req.Temperature),
	}
	for _, block := range req.System {
		b := anthropic.TextBlockParam{Text: block.Text}
		if block.Cache {
			b.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		params.System = append(params.System, b)
	}
	for _, message := range req.Messages {
		switch message.Role {
		case "user":
			params.Messages = append(params.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock(message.Content)))
		case "assistant":
			params.Messages = append(params.Messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(message.Content)))
		}
	}
	return params
}

func (c *anthropicClient) CallTool(ctx context.Context, req Request, tool Tool) (ToolResponse, error) {
	params := anthropicParams(req)
	params.Tools = []anthropic.ToolUnionParam{{OfTool: &anthropic.ToolParam{
		Name: tool.Name, Description: anthropic.String(tool.Description),
		InputSchema: anthropic.ToolInputSchemaParam{Properties: tool.Properties, Required: tool.Required},
	}}}
	params.ToolChoice = anthropic.ToolChoiceParamOfTool(tool.Name)
	resp, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return ToolResponse{}, errors.Wrap(err, "anthropic messages.new")
	}
	usage := Usage{
		Model: string(resp.Model), InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens,
		CacheReadTokens: resp.Usage.CacheReadInputTokens, CacheCreateTokens: resp.Usage.CacheCreationInputTokens,
		StopReason: string(resp.StopReason),
	}
	for _, b := range resp.Content {
		if b.Type == "tool_use" && b.Name == tool.Name {
			if !json.Valid(b.Input) {
				return ToolResponse{Usage: usage}, errors.Errorf("anthropic: invalid tool input for %s", tool.Name)
			}
			return ToolResponse{Input: json.RawMessage(b.Input), Usage: usage}, nil
		}
	}
	return ToolResponse{Usage: usage}, errors.Errorf("anthropic: tool %s not called", tool.Name)
}

func (c *anthropicClient) StreamText(ctx context.Context, req Request, onText func(string, Usage)) (Usage, error) {
	stream := c.client.Messages.NewStreaming(ctx, anthropicParams(req))
	defer stream.Close()
	usage := Usage{Model: req.Model}
	complete := false
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "message_start":
			usage.Model = string(event.Message.Model)
			usage.InputTokens = event.Message.Usage.InputTokens
			usage.CacheReadTokens = event.Message.Usage.CacheReadInputTokens
			usage.CacheCreateTokens = event.Message.Usage.CacheCreationInputTokens
		case "content_block_delta":
			if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				onText(event.Delta.Text, usage)
			}
		case "message_delta":
			if event.Usage.InputTokens > 0 {
				usage.InputTokens = event.Usage.InputTokens
			}
			if event.Usage.OutputTokens > 0 {
				usage.OutputTokens = event.Usage.OutputTokens
			}
			if event.Usage.CacheReadInputTokens > 0 {
				usage.CacheReadTokens = event.Usage.CacheReadInputTokens
			}
			if event.Usage.CacheCreationInputTokens > 0 {
				usage.CacheCreateTokens = event.Usage.CacheCreationInputTokens
			}
			usage.StopReason = string(event.Delta.StopReason)
		case "message_stop":
			complete = true
		}
	}
	if err := stream.Err(); err != nil {
		return usage, errors.Wrap(err, "anthropic stream")
	}
	if err := ctx.Err(); err != nil {
		return usage, err
	}
	if !complete {
		return usage, errors.New("anthropic stream ended without message_stop")
	}
	return usage, nil
}
