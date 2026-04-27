package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/Orwell-Yu/korthex/internal/config"
)

// anthropicProvider implements Provider using the Anthropic SDK.
type anthropicProvider struct {
	client      *anthropic.Client
	model       string
	temperature float64
	maxTokens   int
}

// NewAnthropicProvider creates a Provider backed by the Anthropic API.
func NewAnthropicProvider(cfg config.LLMConfig) (Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("%w: API key is required for provider %q", ErrAuth, cfg.Provider)
	}

	opts := []option.RequestOption{}
	// When a custom BaseURL is set (proxy/gateway), use Bearer token auth;
	// otherwise use the standard X-Api-Key header for the official Anthropic API.
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithAuthToken(cfg.APIKey), option.WithBaseURL(cfg.BaseURL))
	} else {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}

	client := anthropic.NewClient(opts...)

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	return &anthropicProvider{
		client:      &client,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		maxTokens:   maxTokens,
	}, nil
}

func (p *anthropicProvider) ModelName() string    { return p.model }
func (p *anthropicProvider) ProviderName() string { return "anthropic" }

func (p *anthropicProvider) ValidateConnection(ctx context.Context) error {
	_, err := p.Chat(ctx, []Message{
		{Role: RoleUser, Content: "ping"},
	}, nil)
	if err != nil {
		return fmt.Errorf("llm: connection validation failed: %w", err)
	}
	return nil
}

func (p *anthropicProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error) {
	params := p.buildParams(messages, tools)

	slog.Debug("llm request", "provider", "anthropic", "model", p.model, "tools", len(tools))

	resp, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, mapAnthropicError(err)
	}

	msg := &Message{
		Role: RoleAssistant,
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			msg.Content += block.Text
		case "tool_use":
			toolUse := block.AsToolUse()
			toolCall, err := parseAnthropicToolUse(toolUse)
			if err != nil {
				slog.Warn("failed to parse tool use input", "error", err, "name", toolUse.Name)
				continue
			}
			msg.ToolCalls = append(msg.ToolCalls, toolCall)
		}
	}

	return msg, nil
}

func (p *anthropicProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, ch chan<- StreamDelta) error {
	defer close(ch)

	params := p.buildParams(messages, tools)

	slog.Debug("llm stream request", "provider", "anthropic", "model", p.model, "tools", len(tools))

	stream := p.client.Messages.NewStreaming(ctx, params)

	// Track tool use blocks being built during streaming
	type toolUseAcc struct {
		id          string
		name        string
		partialJSON string
	}
	var currentToolUse *toolUseAcc

	for stream.Next() {
		event := stream.Current()

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				currentToolUse = &toolUseAcc{
					id:   event.ContentBlock.ID,
					name: event.ContentBlock.Name,
				}
			}

		case "content_block_delta":
			delta := event.Delta
			switch delta.Type {
			case "text_delta":
				ch <- StreamDelta{Content: delta.Text}
			case "input_json_delta":
				if currentToolUse != nil {
					currentToolUse.partialJSON += delta.PartialJSON
				}
			}

		case "content_block_stop":
			if currentToolUse != nil {
				tc, err := parseAnthropicRawJSON(currentToolUse.id, currentToolUse.name, currentToolUse.partialJSON)
				if err == nil {
					ch <- StreamDelta{ToolCall: &tc}
				}
				currentToolUse = nil
			}

		case "message_delta":
			msgDelta := event.AsMessageDelta()
			ch <- StreamDelta{
				Done:       true,
				StopReason: string(msgDelta.Delta.StopReason),
			}
		}
	}

	if err := stream.Err(); err != nil {
		return mapAnthropicError(err)
	}

	return nil
}

func (p *anthropicProvider) buildParams(messages []Message, tools []ToolDefinition) anthropic.MessageNewParams {
	system, msgs := splitAnthropicMessages(messages)

	params := anthropic.MessageNewParams{
		Model:     p.model,
		MaxTokens: int64(p.maxTokens),
		Messages:  msgs,
	}

	if system != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: system},
		}
	}

	if p.temperature > 0 {
		params.Temperature = anthropic.Float(p.temperature)
	}

	if len(tools) > 0 {
		params.Tools = convertToAnthropicTools(tools)
	}

	return params
}

// splitAnthropicMessages separates system messages and converts the rest to Anthropic format.
// Anthropic requires system prompt as a separate parameter, not in the messages array.
func splitAnthropicMessages(messages []Message) (string, []anthropic.MessageParam) {
	var systemContent string
	var result []anthropic.MessageParam

	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			if systemContent != "" {
				systemContent += "\n"
			}
			systemContent += msg.Content

		case RoleUser:
			result = append(result, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(msg.Content)},
			})

		case RoleAssistant:
			var blocks []anthropic.ContentBlockParamUnion
			if msg.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
			}
			for _, tc := range msg.ToolCalls {
				inputJSON, _ := json.Marshal(tc.Arguments)
				var input any
				_ = json.Unmarshal(inputJSON, &input)
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    tc.ID,
						Name:  tc.Name,
						Input: input,
					},
				})
			}
			result = append(result, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleAssistant,
				Content: blocks,
			})

		case RoleTool:
			result = append(result, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{anthropic.NewToolResultBlock(msg.ToolCallID, msg.Content, false)},
			})
		}
	}

	return systemContent, result
}

// convertToAnthropicTools converts unified ToolDefinitions to Anthropic tool params.
func convertToAnthropicTools(tools []ToolDefinition) []anthropic.ToolUnionParam {
	var result []anthropic.ToolUnionParam
	for _, t := range tools {
		properties := make(map[string]any)
		var required []string

		for _, p := range t.Parameters {
			prop := map[string]any{
				"type":        p.Type,
				"description": p.Description,
			}
			if len(p.Enum) > 0 {
				prop["enum"] = p.Enum
			}
			properties[p.Name] = prop
			if p.Required {
				required = append(required, p.Name)
			}
		}

		result = append(result, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: properties,
					Required:   required,
				},
			},
		})
	}
	return result
}

// parseAnthropicToolUse converts an Anthropic ToolUseBlock to our unified ToolCall.
func parseAnthropicToolUse(toolUse anthropic.ToolUseBlock) (ToolCall, error) {
	args := make(map[string]string)
	rawArgs := string(toolUse.Input)

	var raw map[string]any
	if err := json.Unmarshal(toolUse.Input, &raw); err != nil {
		return ToolCall{}, fmt.Errorf("failed to parse tool use input: %w", err)
	}
	for k, v := range raw {
		args[k] = fmt.Sprintf("%v", v)
	}

	return ToolCall{
		ID:        toolUse.ID,
		Name:      toolUse.Name,
		Arguments: args,
		RawArgs:   rawArgs,
	}, nil
}

// parseAnthropicRawJSON parses accumulated JSON from streaming into a ToolCall.
func parseAnthropicRawJSON(id, name, rawJSON string) (ToolCall, error) {
	args := make(map[string]string)
	if rawJSON != "" {
		var raw map[string]any
		if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
			return ToolCall{}, fmt.Errorf("failed to parse streamed tool call JSON: %w", err)
		}
		for k, v := range raw {
			args[k] = fmt.Sprintf("%v", v)
		}
	}

	return ToolCall{
		ID:        id,
		Name:      name,
		Arguments: args,
		RawArgs:   rawJSON,
	}, nil
}

// mapAnthropicError converts Anthropic SDK errors to standardized sentinel errors.
func mapAnthropicError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}

	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusTooManyRequests:
			return fmt.Errorf("%w: %v", ErrRateLimit, err)
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("%w: %v", ErrAuth, err)
		case http.StatusNotFound:
			return fmt.Errorf("%w: %v", ErrModelUnavailable, err)
		}
	}

	return err
}
