package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config is the root configuration struct for Korthex.
type Config struct {
	Kubernetes KubernetesConfig
	LLM        LLMConfig
	Agent      AgentConfig
	UI         UIConfig
	Privacy    PrivacyConfig
	History    HistoryConfig
	Analysis   AnalysisConfig
}

type KubernetesConfig struct {
	Kubeconfig     string // path, e.g. ~/.kube/config
	DefaultContext string
}

type LLMConfig struct {
	Provider    string // "openai" | "anthropic" | "gemini"
	APIKey      string // prefer env var: KORTHEX_LLM_API_KEY
	Model       string
	BaseURL     string // optional: for proxy/gateway endpoints
	Temperature float64
	MaxTokens   int
	SendLogs    bool // false = AI only generates commands, does not receive raw logs
}

type AgentConfig struct {
	MaxIterations   int // Agentic Loop max iterations (default: 20)
	MaxHistoryTurns int // conversation context retained turns (default: 20)
}

type UIConfig struct {
	Theme           string // dark | light | dracula | nord
	LogLinesLimit   int    // Ring Buffer max lines (default: 10000)
	LogPageSize     int    // lines per page (default: 1000)
	DefaultLogSince string // default time range (default: "1h")
}

// Phase 2 config types

type PrivacyConfig struct {
	Redaction RedactionConfig
}

type RedactionConfig struct {
	Enabled        bool
	Rules          []RuleConfig
	DisableBuiltin []string
}

type RuleConfig struct {
	Name        string
	Pattern     string
	Replacement string
}

type HistoryConfig struct {
	Enabled       bool
	RetentionDays int
	DBPath        string
}

type AnalysisConfig struct {
	TraceIDPatterns []TracePatternConfig
}

type TracePatternConfig struct {
	Name    string
	Pattern string
}

// Manager provides config load/save/validate capabilities.
type Manager interface {
	Load() (*Config, error)
	Save(cfg *Config) error
	Validate(cfg *Config) error
	ConfigPath() string
}

// KubeDetection holds information detected from kubeconfig.
type KubeDetection struct {
	KubeconfigPath string
	Contexts       []string
	CurrentContext string
}

// Wizard drives the first-run setup wizard.
type Wizard interface {
	Run(detected KubeDetection) (*Config, error)
	NeedsSetup() bool
}

// validProviders lists the allowed LLM provider values.
var validProviders = map[string]bool{
	"openai":    true,
	"anthropic": true,
	"gemini":    true,
}

// providerEnvVars maps provider names to their environment variable.
var providerEnvVars = map[string]string{
	"openai":    "OPENAI_API_KEY",
	"anthropic": "ANTHROPIC_API_KEY",
	"gemini":    "GEMINI_API_KEY",
}

// manager implements Manager using Viper.
type manager struct {
	configPath string
}

// NewManager creates a new config Manager.
// An optional configPath override can be provided (used for testing);
// if empty, defaults to ~/.config/korthex/config.yaml.
func NewManager(configPath ...string) Manager {
	p := defaultConfigPath()
	if len(configPath) > 0 && configPath[0] != "" {
		p = configPath[0]
	}
	return &manager{configPath: p}
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "~"
	}
	return filepath.Join(home, ".config", "korthex", "config.yaml")
}

// ConfigPath returns the path to the config file.
func (m *manager) ConfigPath() string {
	return m.configPath
}

// Load reads configuration from file and applies env var overrides.
func (m *manager) Load() (*Config, error) {
	cfg := applyDefaults()

	v := viper.New()
	v.SetConfigFile(m.configPath)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		// If config file doesn't exist, return defaults
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			applyEnvOverrides(cfg)
			return cfg, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			applyEnvOverrides(cfg)
			return cfg, nil
		}
		// For PathError (file not found), return defaults
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			applyEnvOverrides(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Only override defaults for keys explicitly present in the config file.
	// This avoids Viper's zero-value problem: GetBool returns false and
	// GetFloat64 returns 0.0 for missing keys, which would silently
	// overwrite defaults like SendLogs=true and Temperature=0.1.
	if v.IsSet("kubernetes.kubeconfig") {
		cfg.Kubernetes.Kubeconfig = v.GetString("kubernetes.kubeconfig")
	}
	if v.IsSet("kubernetes.default_context") {
		cfg.Kubernetes.DefaultContext = v.GetString("kubernetes.default_context")
	}

	if v.IsSet("llm.provider") {
		cfg.LLM.Provider = v.GetString("llm.provider")
	}
	if v.IsSet("llm.api_key") {
		cfg.LLM.APIKey = v.GetString("llm.api_key")
	}
	if v.IsSet("llm.model") {
		cfg.LLM.Model = v.GetString("llm.model")
	}
	if v.IsSet("llm.base_url") {
		cfg.LLM.BaseURL = v.GetString("llm.base_url")
	}
	if v.IsSet("llm.temperature") {
		cfg.LLM.Temperature = v.GetFloat64("llm.temperature")
	}
	if v.IsSet("llm.max_tokens") {
		cfg.LLM.MaxTokens = v.GetInt("llm.max_tokens")
	}
	if v.IsSet("llm.send_logs") {
		cfg.LLM.SendLogs = v.GetBool("llm.send_logs")
	}

	if v.IsSet("agent.max_iterations") {
		cfg.Agent.MaxIterations = v.GetInt("agent.max_iterations")
	}
	if v.IsSet("agent.max_history_turns") {
		cfg.Agent.MaxHistoryTurns = v.GetInt("agent.max_history_turns")
	}

	if v.IsSet("ui.theme") {
		cfg.UI.Theme = v.GetString("ui.theme")
	}
	if v.IsSet("ui.log_lines_limit") {
		cfg.UI.LogLinesLimit = v.GetInt("ui.log_lines_limit")
	}
	if v.IsSet("ui.log_page_size") {
		cfg.UI.LogPageSize = v.GetInt("ui.log_page_size")
	}
	if v.IsSet("ui.default_log_since") {
		cfg.UI.DefaultLogSince = v.GetString("ui.default_log_since")
	}

	// Phase 2: Privacy
	if v.IsSet("privacy.redaction.enabled") {
		cfg.Privacy.Redaction.Enabled = v.GetBool("privacy.redaction.enabled")
	}
	if v.IsSet("privacy.redaction.disable_builtin") {
		cfg.Privacy.Redaction.DisableBuiltin = v.GetStringSlice("privacy.redaction.disable_builtin")
	}
	if v.IsSet("privacy.redaction.rules") {
		var rules []RuleConfig
		if err := v.UnmarshalKey("privacy.redaction.rules", &rules); err == nil {
			cfg.Privacy.Redaction.Rules = rules
		}
	}

	// Phase 2: History
	if v.IsSet("history.enabled") {
		cfg.History.Enabled = v.GetBool("history.enabled")
	}
	if v.IsSet("history.retention_days") {
		cfg.History.RetentionDays = v.GetInt("history.retention_days")
	}
	if v.IsSet("history.db_path") {
		cfg.History.DBPath = v.GetString("history.db_path")
	}

	// Phase 2: Analysis
	if v.IsSet("analysis.trace_id_patterns") {
		var patterns []TracePatternConfig
		if err := v.UnmarshalKey("analysis.trace_id_patterns", &patterns); err == nil {
			cfg.Analysis.TraceIDPatterns = patterns
		}
	}

	// Env vars always override file values
	applyEnvOverrides(cfg)

	// Backward compatibility: migrate deprecated "custom" provider to "openai".
	// Old configs stored provider="custom" for custom BaseURL endpoints;
	// now users should use the actual provider name (openai/anthropic) + base_url.
	if cfg.LLM.Provider == "custom" {
		cfg.LLM.Provider = "openai"
	}

	return cfg, nil
}

// Save writes the config to disk with 0600 permissions.
func (m *manager) Save(cfg *Config) error {
	dir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	v := viper.New()
	v.SetConfigType("yaml")

	v.Set("kubernetes.kubeconfig", cfg.Kubernetes.Kubeconfig)
	v.Set("kubernetes.default_context", cfg.Kubernetes.DefaultContext)

	v.Set("llm.provider", cfg.LLM.Provider)
	v.Set("llm.api_key", cfg.LLM.APIKey)
	v.Set("llm.model", cfg.LLM.Model)
	v.Set("llm.base_url", cfg.LLM.BaseURL)
	v.Set("llm.temperature", cfg.LLM.Temperature)
	v.Set("llm.max_tokens", cfg.LLM.MaxTokens)
	v.Set("llm.send_logs", cfg.LLM.SendLogs)

	v.Set("agent.max_iterations", cfg.Agent.MaxIterations)
	v.Set("agent.max_history_turns", cfg.Agent.MaxHistoryTurns)

	v.Set("ui.theme", cfg.UI.Theme)
	v.Set("ui.log_lines_limit", cfg.UI.LogLinesLimit)
	v.Set("ui.log_page_size", cfg.UI.LogPageSize)
	v.Set("ui.default_log_since", cfg.UI.DefaultLogSince)

	// Phase 2: Privacy
	v.Set("privacy.redaction.enabled", cfg.Privacy.Redaction.Enabled)
	if len(cfg.Privacy.Redaction.Rules) > 0 {
		v.Set("privacy.redaction.rules", cfg.Privacy.Redaction.Rules)
	}
	if len(cfg.Privacy.Redaction.DisableBuiltin) > 0 {
		v.Set("privacy.redaction.disable_builtin", cfg.Privacy.Redaction.DisableBuiltin)
	}

	// Phase 2: History
	v.Set("history.enabled", cfg.History.Enabled)
	v.Set("history.retention_days", cfg.History.RetentionDays)
	v.Set("history.db_path", cfg.History.DBPath)

	// Phase 2: Analysis
	if len(cfg.Analysis.TraceIDPatterns) > 0 {
		v.Set("analysis.trace_id_patterns", cfg.Analysis.TraceIDPatterns)
	}

	if err := v.WriteConfigAs(m.configPath); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	if err := os.Chmod(m.configPath, 0600); err != nil {
		return fmt.Errorf("setting config file permissions: %w", err)
	}

	return nil
}

// Validate checks the config for required fields and valid values.
// Returns all validation errors, not just the first one.
func (m *manager) Validate(cfg *Config) error {
	var errs []string

	// Kubernetes validation
	if cfg.Kubernetes.Kubeconfig == "" {
		errs = append(errs, "kubernetes.kubeconfig is required")
	} else if _, err := os.Stat(cfg.Kubernetes.Kubeconfig); err != nil {
		errs = append(errs, fmt.Sprintf("kubernetes.kubeconfig file does not exist: %s", cfg.Kubernetes.Kubeconfig))
	}

	// LLM validation
	if cfg.LLM.Provider == "" {
		errs = append(errs, "llm.provider is required")
	} else if !validProviders[cfg.LLM.Provider] {
		errs = append(errs, fmt.Sprintf("llm.provider must be one of: openai, anthropic, gemini (got: %s)", cfg.LLM.Provider))
	}

	if cfg.LLM.APIKey == "" {
		errs = append(errs, "llm.api_key is required (set KORTHEX_LLM_API_KEY or configure in config.yaml)")
	}

	// Agent validation
	if cfg.Agent.MaxIterations <= 0 {
		errs = append(errs, "agent.max_iterations must be > 0")
	}
	if cfg.Agent.MaxHistoryTurns <= 0 {
		errs = append(errs, "agent.max_history_turns must be > 0")
	}

	// UI validation
	if cfg.UI.LogLinesLimit <= 0 {
		errs = append(errs, "ui.log_lines_limit must be > 0")
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// applyDefaults returns a Config with all default values set.
func applyDefaults() *Config {
	return &Config{
		Kubernetes: KubernetesConfig{
			Kubeconfig: filepath.Join(homeDir(), ".kube", "config"),
		},
		LLM: LLMConfig{
			Provider:    "openai",
			Model:       "gpt-4o",
			Temperature: 0.1,
			MaxTokens:   4096,
			SendLogs:    true,
		},
		Agent: AgentConfig{
			MaxIterations:   20,
			MaxHistoryTurns: 20,
		},
		UI: UIConfig{
			Theme:           "dark",
			LogLinesLimit:   10000,
			LogPageSize:     1000,
			DefaultLogSince: "1h",
		},
		Privacy: PrivacyConfig{
			Redaction: RedactionConfig{
				Enabled: true,
			},
		},
		History: HistoryConfig{
			Enabled:       true,
			RetentionDays: 30,
			DBPath:        filepath.Join(homeDir(), ".korthex", "history.db"),
		},
		Analysis: AnalysisConfig{},
	}
}

// applyEnvOverrides applies environment variable overrides to the config.
// Priority: KORTHEX_LLM_API_KEY > provider-specific env > config file value.
func applyEnvOverrides(cfg *Config) {
	if key := os.Getenv("KORTHEX_LLM_API_KEY"); key != "" {
		cfg.LLM.APIKey = key
		return
	}
	if envVar, ok := providerEnvVars[cfg.LLM.Provider]; ok {
		if key := os.Getenv(envVar); key != "" {
			cfg.LLM.APIKey = key
		}
	}
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return home
}
