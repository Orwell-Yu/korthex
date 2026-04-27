package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Orwell-Yu/korthex/internal/config"
)

func TestRegistry_SupportedProviders(t *testing.T) {
	r := NewRegistry()
	providers := r.SupportedProviders()

	assert.Contains(t, providers, "openai")
	assert.Contains(t, providers, "anthropic")
	assert.Contains(t, providers, "gemini")
	assert.Len(t, providers, 3)
}

func TestRegistry_Create_InvalidProvider(t *testing.T) {
	r := NewRegistry()
	_, err := r.Create(config.LLMConfig{
		Provider: "nonexistent",
		APIKey:   "key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider")
}

func TestRegistry_Create_ValidProvider(t *testing.T) {
	r := NewRegistry()

	// OpenAI
	p, err := r.Create(config.LLMConfig{
		Provider: "openai",
		APIKey:   "test-key",
		Model:    "gpt-4",
	})
	require.NoError(t, err)
	assert.Equal(t, "openai", p.ProviderName())
	assert.Equal(t, "gpt-4", p.ModelName())

	// Anthropic
	p, err = r.Create(config.LLMConfig{
		Provider:  "anthropic",
		APIKey:    "test-key",
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
	})
	require.NoError(t, err)
	assert.Equal(t, "anthropic", p.ProviderName())

	// "custom" is no longer a registered provider — backward compat is in config.Load()
	_, err = r.Create(config.LLMConfig{
		Provider: "custom",
		APIKey:   "test-key",
		Model:    "local-model",
		BaseURL:  "http://localhost:8080",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider")
}

func TestRegistry_Create_MissingAPIKey(t *testing.T) {
	r := NewRegistry()

	_, err := r.Create(config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4",
	})
	require.Error(t, err)
	assert.True(t, IsAuth(err))
}
