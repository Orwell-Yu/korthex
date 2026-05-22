package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"

	"google.golang.org/genai"

	"github.com/Orwell-Yu/korthex/internal/config"
)

// geminiProvider implements Provider using the Google GenAI SDK.
type geminiProvider struct {
	client      *genai.Client
	model       string
	temperature float64
	maxTokens   int
}

// NewGeminiProvider creates a Provider backed by the Gemini API.
func NewGeminiProvider(cfg config.LLMConfig) (Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("%w: API key is required for provider %q", ErrAuth, cfg.Provider)
	}

	clientConfig := &genai.ClientConfig{
		APIKey:  cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
	}
	if cfg.BaseURL != "" {
		clientConfig.HTTPOptions = genai.HTTPOptions{
			BaseURL: cfg.BaseURL,
		}
	}

	client, err := genai.NewClient(context.Background(), clientConfig)
	if err != nil {
		return nil, fmt.Errorf("llm: failed to create Gemini client: %w", err)
	}

	return &geminiProvider{
		client:      client,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
	}, nil
}

func (p *geminiProvider) ModelName() string    { return p.model }
func (p *geminiProvider) ProviderName() string { return "gemini" }

func (p *geminiProvider) ValidateConnection(ctx context.Context) error {
	_, err := p.Chat(ctx, []Message{
		{Role: RoleUser, Content: "ping"},
	}, nil)
	if err != nil {
		return fmt.Errorf("llm: connection validation failed: %w", err)
	}
	return nil
}

func (p *geminiProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error) {
	contents, genConfig := p.buildRequest(messages, tools)

	slog.Debug("llm request", "provider", "gemini", "model", p.model, "tools", len(tools))

	resp, err := p.client.Models.GenerateContent(ctx, p.model, contents, genConfig)
	if err != nil {
		return nil, mapGeminiError(err)
	}

	return parseGeminiResponse(resp)
}

func (p *geminiProvider) ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition, ch chan<- StreamDelta) error {
	defer close(ch)

	contents, genConfig := p.buildRequest(messages, tools)

	slog.Debug("llm stream request", "provider", "gemini", "model", p.model, "tools", len(tools))

	var lastFinishReason genai.FinishReason
	var lastUsageMetadata *genai.GenerateContentResponseUsageMetadata

	for resp, err := range p.client.Models.GenerateContentStream(ctx, p.model, contents, genConfig) {
		if err != nil {
			return mapGeminiError(err)
		}

		if len(resp.Candidates) == 0 {
			continue
		}

		candidate := resp.Candidates[0]
		lastFinishReason = candidate.FinishReason

		// Track usage metadata (last one has final totals)
		if resp.UsageMetadata != nil {
			lastUsageMetadata = resp.UsageMetadata
		}

		if candidate.Content == nil {
			continue
		}

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				ch <- StreamDelta{Content: part.Text}
			}
			if part.FunctionCall != nil {
				tc := convertGeminiFunctionCall(part.FunctionCall)
				ch <- StreamDelta{ToolCall: &tc}
			}
		}
	}

	if lastFinishReason != "" {
		sd := StreamDelta{
			Done:       true,
			StopReason: string(lastFinishReason),
		}
		if lastUsageMetadata != nil {
			sd.Usage = &TokenUsage{
				InputTokens:    int(lastUsageMetadata.PromptTokenCount),
				OutputTokens:   int(lastUsageMetadata.CandidatesTokenCount),
				CacheReadTokens: int(lastUsageMetadata.CachedContentTokenCount),
			}
		}
		ch <- sd
	}

	return nil
}

func (p *geminiProvider) buildRequest(messages []Message, tools []ToolDefinition) ([]*genai.Content, *genai.GenerateContentConfig) {
	contents, systemInstruction := convertToGeminiContents(messages)

	genConfig := &genai.GenerateContentConfig{}

	if systemInstruction != "" {
		genConfig.SystemInstruction = genai.NewContentFromText(systemInstruction, genai.RoleUser)
	}

	if p.temperature > 0 {
		genConfig.Temperature = genai.Ptr(float32(p.temperature))
	}
	if p.maxTokens > 0 {
		tokens := p.maxTokens
		if tokens > math.MaxInt32 {
			tokens = math.MaxInt32
		}
		genConfig.MaxOutputTokens = int32(tokens) //nolint:gosec // overflow guarded above
	}

	if len(tools) > 0 {
		genConfig.Tools = convertToGeminiTools(tools)
	}

	return contents, genConfig
}

// convertToGeminiContents converts unified Messages to Gemini Content format.
// Returns the contents slice and any system instruction extracted.
func convertToGeminiContents(messages []Message) ([]*genai.Content, string) {
	var systemInstruction string
	var contents []*genai.Content

	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			if systemInstruction != "" {
				systemInstruction += "\n"
			}
			systemInstruction += msg.Content

		case RoleUser:
			contents = append(contents, genai.NewContentFromText(msg.Content, genai.RoleUser))

		case RoleAssistant:
			if len(msg.ToolCalls) > 0 {
				content := &genai.Content{Role: genai.RoleModel}
				if msg.Content != "" {
					content.Parts = append(content.Parts, genai.NewPartFromText(msg.Content))
				}
				for _, tc := range msg.ToolCalls {
					args := make(map[string]any)
					for k, v := range tc.Arguments {
						args[k] = v
					}
					content.Parts = append(content.Parts, genai.NewPartFromFunctionCall(tc.Name, args))
				}
				contents = append(contents, content)
			} else {
				contents = append(contents, genai.NewContentFromText(msg.Content, genai.RoleModel))
			}

		case RoleTool:
			contents = append(contents, &genai.Content{
				Role: genai.RoleUser,
				Parts: []*genai.Part{
					genai.NewPartFromFunctionResponse(msg.Name, map[string]any{
						"result": msg.Content,
					}),
				},
			})
		}
	}

	return contents, systemInstruction
}

// convertToGeminiTools converts unified ToolDefinitions to Gemini tool format.
func convertToGeminiTools(tools []ToolDefinition) []*genai.Tool {
	var decls []*genai.FunctionDeclaration
	for _, t := range tools {
		decl := &genai.FunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
		}

		if len(t.Parameters) > 0 {
			properties := make(map[string]*genai.Schema)
			var required []string

			for _, p := range t.Parameters {
				schema := &genai.Schema{
					Type:        geminiTypeFromString(p.Type),
					Description: p.Description,
				}
				if len(p.Enum) > 0 {
					schema.Enum = p.Enum
				}
				properties[p.Name] = schema
				if p.Required {
					required = append(required, p.Name)
				}
			}

			decl.Parameters = &genai.Schema{
				Type:       genai.TypeObject,
				Properties: properties,
				Required:   required,
			}
		}

		decls = append(decls, decl)
	}

	return []*genai.Tool{{FunctionDeclarations: decls}}
}

// geminiTypeFromString converts a JSON Schema type string to genai.Type.
func geminiTypeFromString(t string) genai.Type {
	switch t {
	case "string":
		return genai.TypeString
	case "integer":
		return genai.TypeInteger
	case "number":
		return genai.TypeNumber
	case "boolean":
		return genai.TypeBoolean
	case "array":
		return genai.TypeArray
	case "object":
		return genai.TypeObject
	default:
		return genai.TypeString
	}
}

// parseGeminiResponse converts a Gemini response to our unified Message.
func parseGeminiResponse(resp *genai.GenerateContentResponse) (*Message, error) {
	if len(resp.Candidates) == 0 {
		return &Message{Role: RoleAssistant}, nil
	}

	candidate := resp.Candidates[0]
	if candidate.Content == nil {
		return &Message{Role: RoleAssistant}, nil
	}

	msg := &Message{Role: RoleAssistant}

	// Extract token usage
	if resp.UsageMetadata != nil {
		msg.Usage = TokenUsage{
			InputTokens:    int(resp.UsageMetadata.PromptTokenCount),
			OutputTokens:   int(resp.UsageMetadata.CandidatesTokenCount),
			CacheReadTokens: int(resp.UsageMetadata.CachedContentTokenCount),
		}
	}

	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			msg.Content += part.Text
		}
		if part.FunctionCall != nil {
			tc := convertGeminiFunctionCall(part.FunctionCall)
			msg.ToolCalls = append(msg.ToolCalls, tc)
		}
	}

	return msg, nil
}

// convertGeminiFunctionCall converts a Gemini FunctionCall to our unified ToolCall.
// Gemini returns Args as map[string]any — we convert to map[string]string and preserve RawArgs.
func convertGeminiFunctionCall(fc *genai.FunctionCall) ToolCall {
	args := make(map[string]string)
	for k, v := range fc.Args {
		args[k] = fmt.Sprintf("%v", v)
	}

	rawJSON, _ := json.Marshal(fc.Args)

	return ToolCall{
		ID:        fc.ID,
		Name:      fc.Name,
		Arguments: args,
		RawArgs:   string(rawJSON),
	}
}

// mapGeminiError converts Gemini SDK errors to standardized sentinel errors.
func mapGeminiError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}

	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case 429:
			return fmt.Errorf("%w: %v", ErrRateLimit, err)
		case 401, 403:
			return fmt.Errorf("%w: %v", ErrAuth, err)
		case 404:
			return fmt.Errorf("%w: %v", ErrModelUnavailable, err)
		}
	}

	return err
}
