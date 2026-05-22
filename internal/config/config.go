package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
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
	Database   DatabaseConfig
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
	MaxIterations   int  // Agentic Loop max iterations (-1 = unlimited, default: -1)
	MaxHistoryTurns int  // conversation context retained turns (default: 20)
	TokenMetrics    bool // show token metrics in Chat header (default: true)
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

// Phase 3 config types

type DatabaseConfig struct {
	Discovery DiscoveryConfig `yaml:"discovery" mapstructure:"discovery"`
	Query     QueryConfig     `yaml:"query" mapstructure:"query"`
}

type DiscoveryConfig struct {
	Enabled       bool     `yaml:"enabled" mapstructure:"enabled"`
	ImagePatterns []string `yaml:"image_patterns" mapstructure:"image_patterns"`
}

type QueryConfig struct {
	MaxRowsPerTable  int `yaml:"max_rows_per_table" mapstructure:"max_rows_per_table"`
	MaxRelationPaths int `yaml:"max_relation_paths" mapstructure:"max_relation_paths"`
	TimeoutSeconds   int `yaml:"timeout_seconds" mapstructure:"timeout_seconds"`
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

// KubeconfigEntry represents a discovered kubeconfig file.
type KubeconfigEntry struct {
	Path         string // absolute path
	ContextCount int    // number of contexts in this file
}

// ContextEntry represents a context within a kubeconfig file.
type ContextEntry struct {
	Name    string // context name
	Cluster string // associated cluster name
	User    string // associated user name
	Current bool   // is this the file's current-context
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
	if v.IsSet("agent.token_metrics") {
		cfg.Agent.TokenMetrics = v.GetBool("agent.token_metrics")
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

	// Phase 3: Database
	if v.IsSet("database.discovery.enabled") {
		cfg.Database.Discovery.Enabled = v.GetBool("database.discovery.enabled")
	}
	if v.IsSet("database.discovery.image_patterns") {
		cfg.Database.Discovery.ImagePatterns = v.GetStringSlice("database.discovery.image_patterns")
	}
	if v.IsSet("database.query.max_rows_per_table") {
		cfg.Database.Query.MaxRowsPerTable = v.GetInt("database.query.max_rows_per_table")
	}
	if v.IsSet("database.query.max_relation_paths") {
		cfg.Database.Query.MaxRelationPaths = v.GetInt("database.query.max_relation_paths")
	}
	if v.IsSet("database.query.timeout_seconds") {
		cfg.Database.Query.TimeoutSeconds = v.GetInt("database.query.timeout_seconds")
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
	v.Set("agent.token_metrics", cfg.Agent.TokenMetrics)

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

	// Phase 3: Database
	v.Set("database.discovery.enabled", cfg.Database.Discovery.Enabled)
	if len(cfg.Database.Discovery.ImagePatterns) > 0 {
		v.Set("database.discovery.image_patterns", cfg.Database.Discovery.ImagePatterns)
	}
	v.Set("database.query.max_rows_per_table", cfg.Database.Query.MaxRowsPerTable)
	v.Set("database.query.max_relation_paths", cfg.Database.Query.MaxRelationPaths)
	v.Set("database.query.timeout_seconds", cfg.Database.Query.TimeoutSeconds)

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
	if cfg.Agent.MaxIterations == 0 || cfg.Agent.MaxIterations < -1 {
		errs = append(errs, "agent.max_iterations must be > 0 or -1 (unlimited)")
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
			MaxIterations:   -1,
			MaxHistoryTurns: 20,
			TokenMetrics:    true,
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
		Database: DatabaseConfig{
			Discovery: DiscoveryConfig{
				Enabled: true,
			},
			Query: QueryConfig{
				MaxRowsPerTable:  100,
				MaxRelationPaths: 10,
				TimeoutSeconds:   30,
			},
		},
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

// kubeconfigYAML is a minimal struct for parsing kubeconfig files.
type kubeconfigYAML struct {
	CurrentContext string `yaml:"current-context"`
	Contexts       []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	} `yaml:"contexts"`
}

// DiscoverKubeconfigs scans for kubeconfig files and returns them sorted by path.
// kubeDir is the directory to scan for config* files (typically ~/.kube).
// envKubeconfig is the raw $KUBECONFIG value (colon-separated paths).
func DiscoverKubeconfigs(kubeDir, envKubeconfig string) []KubeconfigEntry {
	seen := make(map[string]bool)
	var paths []string

	// 1. Parse $KUBECONFIG-style paths
	if envKubeconfig != "" {
		for _, p := range filepath.SplitList(envKubeconfig) {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				continue
			}
			info, err := os.Stat(abs)
			if err != nil || info.IsDir() {
				continue
			}
			if !seen[abs] {
				seen[abs] = true
				paths = append(paths, abs)
			}
		}
	}

	// 2. Scan kubeDir for files matching config* (exclude .lock and directories)
	if kubeDir != "" {
		entries, err := os.ReadDir(kubeDir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				if !strings.HasPrefix(name, "config") {
					continue
				}
				if strings.HasSuffix(name, ".lock") {
					continue
				}
				abs, err := filepath.Abs(filepath.Join(kubeDir, name))
				if err != nil {
					continue
				}
				if !seen[abs] {
					seen[abs] = true
					paths = append(paths, abs)
				}
			}
		}
	}

	// 3. Sort by path
	sort.Strings(paths)

	// 4. Build entries with context count
	entries := make([]KubeconfigEntry, 0, len(paths))
	for _, p := range paths {
		ctxs, err := ParseContexts(p)
		count := 0
		if err == nil {
			count = len(ctxs)
		}
		entries = append(entries, KubeconfigEntry{
			Path:         p,
			ContextCount: count,
		})
	}

	return entries
}

// ParseContexts parses a kubeconfig file and returns its contexts.
func ParseContexts(kubeconfigPath string) ([]ContextEntry, error) {
	data, err := os.ReadFile(kubeconfigPath) //nolint:gosec // kubeconfig path is user-provided by design
	if err != nil {
		return nil, fmt.Errorf("reading kubeconfig %s: %w", kubeconfigPath, err)
	}

	var kc kubeconfigYAML
	if err := yaml.Unmarshal(data, &kc); err != nil {
		return nil, fmt.Errorf("parsing kubeconfig %s: %w", kubeconfigPath, err)
	}

	entries := make([]ContextEntry, 0, len(kc.Contexts))
	for _, c := range kc.Contexts {
		entries = append(entries, ContextEntry{
			Name:    c.Name,
			Cluster: c.Context.Cluster,
			User:    c.Context.User,
			Current: c.Name == kc.CurrentContext,
		})
	}

	return entries, nil
}
