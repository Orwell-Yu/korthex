package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"

	"github.com/Orwell-Yu/korthex/internal/config"
)

// openaiProvider implements Provider using the OpenAI SDK.
// Also used for "custom" providers with OpenAI-compatible API endpoints.
type openaiProvider struct {
	client       *openai.Client
	model        string
	providerName string
	temperature  float64
	maxTokens    int
}

// NewOpenAIProvider creates a Provider backed by the OpenAI API (or a custom OpenAI-compatible endpoint).
func NewOpenAIProvider(cfg config.LLMConfig) (Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("%w: API key is required for provider %q", ErrAuth, cfg.Provider)
	}

	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}

	client := openai.NewClient(opts...)

	name := cfg.Provider
	if name == "" {
		name = "openai"
	}

	return &openaiProvider{
		client:       &client,
		model:        cfg.Model,
		providerName: name,
		temperature:  cfg.Temperature,
		maxTokens:    cfg.MaxTokens,
	}, nil
}

func (p *openaiProvider) ModelName() string    { return p.model }
func (p *openaiProvider) ProviderName() string { return p.providerName }

func (p *openaiProvider) ValidateConnection(ctx context.Context) error {
	_, err := p.Chat(ctx, []Message{
		{Role: RoleUser, Content: "ping"},
	}, nil)
	if err != nil {
		return fmt.Errorf("llm: connection validation failed: %w", err)
	}
	return nil
}

func (p *openaiProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error) {
	params := p.buildParams(messages, tools)

	slog.Debug("llm request", "provider", p.providerName, "model", p.model, "tools", len(tools))

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, mapOpenAIError(err)
	}

	if len(resp.Choices) == 0 {
		return &Message{Role: RoleAssistant}, nil
	}

	choice := resp.Choices[0]
	msg := &Message{
		Role:    RoleAssistant,
		Content: choice.Message.Content,
	}

	for _, tc := range choice.Message.ToolCalls {
		toolCall, err := parseOpenAIToolCall(tc)
		if err != nil {
			slog.Warn("failed to parse tool call arguments", "error", err, "name", tc.Function.Name)
			continue
		}
		msg.ToolCalls = append(msg.ToolCalls, toolCall)
	}

	return msg, nil
}

func (p *openaiProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, ch chan<- StreamDelta) error {
	defer close(ch)

	params := p.buildParams(messages, tools)

	slog.Debug("llm stream request", "provider", p.providerName, "model", p.model, "tools", len(tools))

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	acc := openai.ChatCompletionAccumulator{}

	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)

		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		finishReason := chunk.Choices[0].FinishReason

		sd := StreamDelta{
			Content: delta.Content,
		}

		// Check if a tool call just finished in the accumulator
		if tc, ok := acc.JustFinishedToolCall(); ok {
			parsed, err := parseOpenAIFinishedToolCall(tc)
			if err == nil {
				sd.ToolCall = &parsed
			}
		}

		if finishReason != "" {
			sd.Done = true
			sd.StopReason = finishReason
		}

		ch <- sd
	}

	if err := stream.Err(); err != nil {
		return mapOpenAIError(err)
	}

	return nil
}

func (p *openaiProvider) buildParams(messages []Message, tools []ToolDefinition) openai.ChatCompletionNewParams {
	params := openai.ChatCompletionNewParams{
		Model:    p.model,
		Messages: convertToOpenAIMessages(messages),
	}

	if p.temperature > 0 {
		params.Temperature = openai.Float(p.temperature)
	}
	if p.maxTokens > 0 {
		params.MaxCompletionTokens = openai.Int(int64(p.maxTokens))
	}

	if len(tools) > 0 {
		params.Tools = convertToOpenAITools(tools)
		params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: openai.String("auto"),
		}
	}

	return params
}

// convertToOpenAIMessages converts unified Messages to OpenAI message format.
func convertToOpenAIMessages(messages []Message) []openai.ChatCompletionMessageParamUnion {
	var result []openai.ChatCompletionMessageParamUnion
	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			result = append(result, openai.SystemMessage(msg.Content))
		case RoleUser:
			result = append(result, openai.UserMessage(msg.Content))
		case RoleAssistant:
			if len(msg.ToolCalls) > 0 {
				am := openai.ChatCompletionAssistantMessageParam{
					ToolCalls: convertToOpenAIToolCallParams(msg.ToolCalls),
				}
				if msg.Content != "" {
					am.Content.OfString = openai.Opt(msg.Content)
				}
				result = append(result, openai.ChatCompletionMessageParamUnion{OfAssistant: &am})
			} else {
				result = append(result, openai.AssistantMessage(msg.Content))
			}
		case RoleTool:
			result = append(result, openai.ToolMessage(msg.Content, msg.ToolCallID))
		}
	}
	return result
}

// convertToOpenAIToolCallParams converts unified ToolCalls to OpenAI param format.
func convertToOpenAIToolCallParams(toolCalls []ToolCall) []openai.ChatCompletionMessageToolCallParam {
	var result []openai.ChatCompletionMessageToolCallParam
	for _, tc := range toolCalls {
		argsJSON, _ := json.Marshal(tc.Arguments)
		result = append(result, openai.ChatCompletionMessageToolCallParam{
			ID: tc.ID,
			Function: openai.ChatCompletionMessageToolCallFunctionParam{
				Name:      tc.Name,
				Arguments: string(argsJSON),
			},
		})
	}
	return result
}

// convertToOpenAITools converts unified ToolDefinitions to OpenAI tool params.
func convertToOpenAITools(tools []ToolDefinition) []openai.ChatCompletionToolParam {
	var result []openai.ChatCompletionToolParam
	for _, t := range tools {
		result = append(result, openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        t.Name,
				Description: openai.String(t.Description),
				Parameters:  buildJSONSchema(t.Parameters),
			},
		})
	}
	return result
}

// buildJSONSchema converts ParameterDefs to a JSON Schema map for OpenAI.
func buildJSONSchema(params []ParameterDef) shared.FunctionParameters {
	properties := make(map[string]any)
	var required []string

	for _, p := range params {
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

	schema := shared.FunctionParameters{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// parseOpenAIToolCall parses an OpenAI tool call response into our unified ToolCall.
func parseOpenAIToolCall(tc openai.ChatCompletionMessageToolCall) (ToolCall, error) {
	args := make(map[string]string)
	if tc.Function.Arguments != "" {
		var raw map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &raw); err != nil {
			return ToolCall{}, fmt.Errorf("failed to parse tool call arguments: %w", err)
		}
		for k, v := range raw {
			args[k] = fmt.Sprintf("%v", v)
		}
	}

	return ToolCall{
		ID:        tc.ID,
		Name:      tc.Function.Name,
		Arguments: args,
		RawArgs:   tc.Function.Arguments,
	}, nil
}

// parseOpenAIFinishedToolCall converts an accumulated finished tool call to our unified ToolCall.
func parseOpenAIFinishedToolCall(tc openai.FinishedChatCompletionToolCall) (ToolCall, error) {
	args := make(map[string]string)
	if tc.Arguments != "" {
		var raw map[string]any
		if err := json.Unmarshal([]byte(tc.Arguments), &raw); err != nil {
			return ToolCall{}, fmt.Errorf("failed to parse tool call arguments: %w", err)
		}
		for k, v := range raw {
			args[k] = fmt.Sprintf("%v", v)
		}
	}

	return ToolCall{
		ID:        tc.ID,
		Name:      tc.Name,
		Arguments: args,
		RawArgs:   tc.Arguments,
	}, nil
}

// mapOpenAIError converts OpenAI SDK errors to standardized sentinel errors.
func mapOpenAIError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}

	var apiErr *openai.Error
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
