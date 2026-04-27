package llm

import (
	"fmt"
	"sort"

	"github.com/Orwell-Yu/korthex/internal/config"
)

// providerConstructor is a function that creates a Provider from config.
type providerConstructor func(cfg config.LLMConfig) (Provider, error)

// registry implements the Registry interface.
type registry struct {
	constructors map[string]providerConstructor
}

// NewRegistry creates a new Registry with all supported providers registered.
func NewRegistry() Registry {
	r := &registry{
		constructors: make(map[string]providerConstructor),
	}
	r.constructors["openai"] = NewOpenAIProvider
	r.constructors["anthropic"] = NewAnthropicProvider
	r.constructors["gemini"] = NewGeminiProvider
	return r
}

// Create looks up the constructor by cfg.Provider and calls it.
func (r *registry) Create(cfg config.LLMConfig) (Provider, error) {
	constructor, ok := r.constructors[cfg.Provider]
	if !ok {
		return nil, fmt.Errorf("llm: unsupported provider %q, supported: %v", cfg.Provider, r.SupportedProviders())
	}
	return constructor(cfg)
}

// SupportedProviders returns the registered provider names in sorted order.
func (r *registry) SupportedProviders() []string {
	names := make([]string, 0, len(r.constructors))
	for name := range r.constructors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
