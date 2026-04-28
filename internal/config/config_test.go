package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Load Tests ---

func TestLoad_ValidConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	kubeconfigPath := filepath.Join(dir, "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte("dummy"), 0644))

	content := `
kubernetes:
  kubeconfig: ` + kubeconfigPath + `
  default_context: test-ctx
llm:
  provider: anthropic
  api_key: sk-test-key
  model: claude-sonnet-4-20250514
  temperature: 0.2
  max_tokens: 2048
  send_logs: false
agent:
  max_iterations: 5
  max_history_turns: 20
ui:
  theme: nord
  log_lines_limit: 5000
  log_page_size: 500
  default_log_since: 2h
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0644))

	mgr := NewManager(cfgPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	assert.Equal(t, kubeconfigPath, cfg.Kubernetes.Kubeconfig)
	assert.Equal(t, "test-ctx", cfg.Kubernetes.DefaultContext)
	assert.Equal(t, "anthropic", cfg.LLM.Provider)
	assert.Equal(t, "sk-test-key", cfg.LLM.APIKey)
	assert.Equal(t, "claude-sonnet-4-20250514", cfg.LLM.Model)
	assert.Equal(t, 0.2, cfg.LLM.Temperature)
	assert.Equal(t, 2048, cfg.LLM.MaxTokens)
	assert.Equal(t, false, cfg.LLM.SendLogs)
	assert.Equal(t, 5, cfg.Agent.MaxIterations)
	assert.Equal(t, 20, cfg.Agent.MaxHistoryTurns)
	assert.Equal(t, "nord", cfg.UI.Theme)
	assert.Equal(t, 5000, cfg.UI.LogLinesLimit)
	assert.Equal(t, 500, cfg.UI.LogPageSize)
	assert.Equal(t, "2h", cfg.UI.DefaultLogSince)
}

func TestLoad_EnvVarOverride_KORTHEX(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
llm:
  provider: openai
  api_key: from-file
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0644))

	t.Setenv("KORTHEX_LLM_API_KEY", "from-env")

	mgr := NewManager(cfgPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	assert.Equal(t, "from-env", cfg.LLM.APIKey)
}

func TestLoad_EnvVarOverride_ProviderSpecific(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		envVar   string
		envValue string
	}{
		{"OpenAI", "openai", "OPENAI_API_KEY", "openai-env-key"},
		{"Anthropic", "anthropic", "ANTHROPIC_API_KEY", "anthropic-env-key"},
		{"Gemini", "gemini", "GEMINI_API_KEY", "gemini-env-key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")

			content := `
llm:
  provider: ` + tt.provider + `
  api_key: from-file
`
			require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0644))
			t.Setenv(tt.envVar, tt.envValue)

			mgr := NewManager(cfgPath)
			cfg, err := mgr.Load()
			require.NoError(t, err)

			assert.Equal(t, tt.envValue, cfg.LLM.APIKey)
		})
	}
}

func TestLoad_KORTHEX_EnvVar_Beats_ProviderSpecific(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
llm:
  provider: openai
  api_key: from-file
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0644))

	t.Setenv("KORTHEX_LLM_API_KEY", "korthex-key")
	t.Setenv("OPENAI_API_KEY", "openai-key")

	mgr := NewManager(cfgPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	assert.Equal(t, "korthex-key", cfg.LLM.APIKey)
}

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nonexistent", "config.yaml")

	mgr := NewManager(cfgPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	assert.Equal(t, "openai", cfg.LLM.Provider)
	assert.Equal(t, "gpt-4o", cfg.LLM.Model)
	assert.Equal(t, 0.1, cfg.LLM.Temperature)
	assert.Equal(t, 4096, cfg.LLM.MaxTokens)
	assert.Equal(t, true, cfg.LLM.SendLogs)
	assert.Equal(t, 20, cfg.Agent.MaxIterations)
	assert.Equal(t, 20, cfg.Agent.MaxHistoryTurns)
	assert.Equal(t, "dark", cfg.UI.Theme)
	assert.Equal(t, 10000, cfg.UI.LogLinesLimit)
	assert.Equal(t, 1000, cfg.UI.LogPageSize)
	assert.Equal(t, "1h", cfg.UI.DefaultLogSince)
}

// --- Validate Tests ---

func TestValidate(t *testing.T) {
	// Create a real kubeconfig file for valid tests
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte("dummy"), 0644))

	validConfig := func() *Config {
		return &Config{
			Kubernetes: KubernetesConfig{
				Kubeconfig: kubeconfigPath,
			},
			LLM: LLMConfig{
				Provider: "openai",
				APIKey:   "sk-test",
				Model:    "gpt-4o",
			},
			Agent: AgentConfig{
				MaxIterations:   3,
				MaxHistoryTurns: 10,
			},
			UI: UIConfig{
				LogLinesLimit: 10000,
			},
		}
	}

	tests := []struct {
		name      string
		modify    func(cfg *Config)
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "valid config",
			modify:  func(cfg *Config) {},
			wantErr: false,
		},
		{
			name:      "missing kubeconfig path",
			modify:    func(cfg *Config) { cfg.Kubernetes.Kubeconfig = "" },
			wantErr:   true,
			errSubstr: "kubernetes.kubeconfig is required",
		},
		{
			name:      "kubeconfig file does not exist",
			modify:    func(cfg *Config) { cfg.Kubernetes.Kubeconfig = "/nonexistent/path" },
			wantErr:   true,
			errSubstr: "kubernetes.kubeconfig file does not exist",
		},
		{
			name:      "invalid provider",
			modify:    func(cfg *Config) { cfg.LLM.Provider = "invalid" },
			wantErr:   true,
			errSubstr: "llm.provider must be one of",
		},
		{
			name:      "empty provider",
			modify:    func(cfg *Config) { cfg.LLM.Provider = "" },
			wantErr:   true,
			errSubstr: "llm.provider is required",
		},
		{
			name:      "missing API key",
			modify:    func(cfg *Config) { cfg.LLM.APIKey = "" },
			wantErr:   true,
			errSubstr: "llm.api_key is required",
		},
		{
			name: "custom provider treated as invalid",
			modify: func(cfg *Config) {
				cfg.LLM.Provider = "custom"
			},
			wantErr:   true,
			errSubstr: "llm.provider must be one of",
		},
		{
			name:      "max_iterations zero",
			modify:    func(cfg *Config) { cfg.Agent.MaxIterations = 0 },
			wantErr:   true,
			errSubstr: "agent.max_iterations must be > 0",
		},
		{
			name:      "max_history_turns negative",
			modify:    func(cfg *Config) { cfg.Agent.MaxHistoryTurns = -1 },
			wantErr:   true,
			errSubstr: "agent.max_history_turns must be > 0",
		},
		{
			name:      "log_lines_limit zero",
			modify:    func(cfg *Config) { cfg.UI.LogLinesLimit = 0 },
			wantErr:   true,
			errSubstr: "ui.log_lines_limit must be > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.modify(cfg)

			mgr := NewManager()
			err := mgr.Validate(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := &Config{
		Kubernetes: KubernetesConfig{Kubeconfig: ""},
		LLM:        LLMConfig{Provider: "", APIKey: ""},
		Agent:      AgentConfig{MaxIterations: 0, MaxHistoryTurns: 0},
		UI:         UIConfig{LogLinesLimit: 0},
	}

	mgr := NewManager()
	err := mgr.Validate(cfg)
	require.Error(t, err)

	errStr := err.Error()
	assert.Contains(t, errStr, "kubernetes.kubeconfig is required")
	assert.Contains(t, errStr, "llm.provider is required")
	assert.Contains(t, errStr, "llm.api_key is required")
	assert.Contains(t, errStr, "agent.max_iterations must be > 0")
	assert.Contains(t, errStr, "agent.max_history_turns must be > 0")
	assert.Contains(t, errStr, "ui.log_lines_limit must be > 0")
}

// --- Save Tests ---

func TestSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	original := &Config{
		Kubernetes: KubernetesConfig{
			Kubeconfig:     "/some/path/kubeconfig",
			DefaultContext: "my-ctx",
		},
		LLM: LLMConfig{
			Provider:    "anthropic",
			APIKey:      "sk-ant-test",
			Model:       "claude-sonnet-4-20250514",
			BaseURL:     "",
			Temperature: 0.3,
			MaxTokens:   8192,
			SendLogs:    false,
		},
		Agent: AgentConfig{
			MaxIterations:   7,
			MaxHistoryTurns: 15,
		},
		UI: UIConfig{
			Theme:           "dracula",
			LogLinesLimit:   20000,
			LogPageSize:     2000,
			DefaultLogSince: "30m",
		},
	}

	mgr := NewManager(cfgPath)
	require.NoError(t, mgr.Save(original))

	loaded, err := mgr.Load()
	require.NoError(t, err)

	assert.Equal(t, original.Kubernetes.Kubeconfig, loaded.Kubernetes.Kubeconfig)
	assert.Equal(t, original.Kubernetes.DefaultContext, loaded.Kubernetes.DefaultContext)
	assert.Equal(t, original.LLM.Provider, loaded.LLM.Provider)
	assert.Equal(t, original.LLM.APIKey, loaded.LLM.APIKey)
	assert.Equal(t, original.LLM.Model, loaded.LLM.Model)
	assert.Equal(t, original.LLM.Temperature, loaded.LLM.Temperature)
	assert.Equal(t, original.LLM.MaxTokens, loaded.LLM.MaxTokens)
	assert.Equal(t, original.LLM.SendLogs, loaded.LLM.SendLogs)
	assert.Equal(t, original.Agent.MaxIterations, loaded.Agent.MaxIterations)
	assert.Equal(t, original.Agent.MaxHistoryTurns, loaded.Agent.MaxHistoryTurns)
	assert.Equal(t, original.UI.Theme, loaded.UI.Theme)
	assert.Equal(t, original.UI.LogLinesLimit, loaded.UI.LogLinesLimit)
	assert.Equal(t, original.UI.LogPageSize, loaded.UI.LogPageSize)
	assert.Equal(t, original.UI.DefaultLogSince, loaded.UI.DefaultLogSince)
}

func TestSave_CreatesParentDirectory(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nested", "deep", "config.yaml")

	mgr := NewManager(cfgPath)
	cfg := applyDefaults()
	require.NoError(t, mgr.Save(cfg))

	_, err := os.Stat(cfgPath)
	assert.NoError(t, err)
}

func TestSave_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	mgr := NewManager(cfgPath)
	cfg := applyDefaults()
	require.NoError(t, mgr.Save(cfg))

	info, err := os.Stat(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

// --- NeedsSetup Tests ---

func TestNeedsSetup_ConfigExists(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("test"), 0644))

	w := NewWizard(cfgPath)
	assert.False(t, w.NeedsSetup())
}

func TestNeedsSetup_ConfigMissing(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nonexistent.yaml")

	w := NewWizard(cfgPath)
	assert.True(t, w.NeedsSetup())
}

// --- ConfigPath Test ---

func TestConfigPath_Default(t *testing.T) {
	mgr := NewManager()
	path := mgr.ConfigPath()
	assert.Contains(t, path, ".config/korthex/config.yaml")
}

func TestConfigPath_Custom(t *testing.T) {
	mgr := NewManager("/custom/path/config.yaml")
	assert.Equal(t, "/custom/path/config.yaml", mgr.ConfigPath())
}

// --- Wizard buildConfig Test ---

func TestWizardModel_BuildConfig(t *testing.T) {
	m := newWizardModel(KubeDetection{
		KubeconfigPath: "/home/user/.kube/config",
		Contexts:       []string{"dev", "staging", "prod"},
		CurrentContext: "staging",
	})

	// Simulate selections
	m.contextCursor = 2  // prod
	m.providerCursor = 1 // anthropic
	m.apiKeyInput.SetValue("sk-ant-test")

	cfg := m.buildConfig()

	assert.Equal(t, "/home/user/.kube/config", cfg.Kubernetes.Kubeconfig)
	assert.Equal(t, "prod", cfg.Kubernetes.DefaultContext)
	assert.Equal(t, "anthropic", cfg.LLM.Provider)
	assert.Equal(t, "sk-ant-test", cfg.LLM.APIKey)
	assert.Equal(t, "claude-sonnet-4-20250514", cfg.LLM.Model)
	assert.Equal(t, 0.1, cfg.LLM.Temperature)
	assert.Equal(t, 4096, cfg.LLM.MaxTokens)
	assert.Equal(t, true, cfg.LLM.SendLogs)
}

func TestWizardModel_BuildConfig_CustomModel(t *testing.T) {
	m := newWizardModel(KubeDetection{
		KubeconfigPath: "/home/user/.kube/config",
		Contexts:       []string{"dev"},
		CurrentContext: "dev",
	})

	m.providerCursor = 0 // openai
	m.apiKeyInput.SetValue("sk-test")
	m.modelInput.SetValue("gpt-4-turbo")

	cfg := m.buildConfig()

	assert.Equal(t, "gpt-4-turbo", cfg.LLM.Model)
}

// --- Load with defaults fill ---

func TestLoad_PartialFile_FillsDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
llm:
  provider: gemini
  api_key: test-key
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0644))

	mgr := NewManager(cfgPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	// Explicitly set values
	assert.Equal(t, "gemini", cfg.LLM.Provider)
	assert.Equal(t, "test-key", cfg.LLM.APIKey)

	// Defaults should be filled
	assert.Equal(t, "gpt-4o", cfg.LLM.Model)
	assert.Equal(t, 0.1, cfg.LLM.Temperature)
	assert.Equal(t, 4096, cfg.LLM.MaxTokens)
	assert.Equal(t, true, cfg.LLM.SendLogs) // Bug A regression: must not be false
	assert.Equal(t, 20, cfg.Agent.MaxIterations)
	assert.Equal(t, 20, cfg.Agent.MaxHistoryTurns)
	assert.Equal(t, "dark", cfg.UI.Theme)
	assert.Equal(t, 10000, cfg.UI.LogLinesLimit)
	assert.Equal(t, 1000, cfg.UI.LogPageSize)
	assert.Equal(t, "1h", cfg.UI.DefaultLogSince)
}

func TestLoad_ExplicitZeroTemperature(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
llm:
  provider: openai
  api_key: test-key
  temperature: 0
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0644))

	mgr := NewManager(cfgPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	// Bug B regression: explicit temperature: 0 must be preserved, not overwritten to 0.1
	assert.Equal(t, float64(0), cfg.LLM.Temperature)
}

func TestLoad_ExplicitSendLogsFalse(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
llm:
  provider: openai
  api_key: test-key
  send_logs: false
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0644))

	mgr := NewManager(cfgPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	// Explicit send_logs: false must be respected
	assert.Equal(t, false, cfg.LLM.SendLogs)
}

func TestLoad_CustomProviderMigratedToOpenAI(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
llm:
  provider: custom
  api_key: test-key
  base_url: https://api.example.com
  model: local-model
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0644))

	mgr := NewManager(cfgPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	// "custom" should be migrated to "openai" by Load()
	assert.Equal(t, "openai", cfg.LLM.Provider)
	assert.Equal(t, "https://api.example.com", cfg.LLM.BaseURL)
}

// --- ParseContexts Tests ---

func writeKubeconfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0644))
	return p
}

const multiContextKubeconfig = `apiVersion: v1
kind: Config
current-context: staging
contexts:
- name: dev
  context:
    cluster: dev-cluster
    user: dev-user
- name: staging
  context:
    cluster: staging-cluster
    user: staging-user
- name: prod
  context:
    cluster: prod-cluster
    user: prod-user
clusters:
- name: dev-cluster
- name: staging-cluster
- name: prod-cluster
users:
- name: dev-user
- name: staging-user
- name: prod-user
`

const singleContextKubeconfig = `apiVersion: v1
kind: Config
current-context: only
contexts:
- name: only
  context:
    cluster: only-cluster
    user: only-user
`

const noContextKubeconfig = `apiVersion: v1
kind: Config
`

func TestParseContexts_MultipleContexts(t *testing.T) {
	dir := t.TempDir()
	p := writeKubeconfig(t, dir, "config", multiContextKubeconfig)

	ctxs, err := ParseContexts(p)
	require.NoError(t, err)
	require.Len(t, ctxs, 3)

	assert.Equal(t, "dev", ctxs[0].Name)
	assert.Equal(t, "dev-cluster", ctxs[0].Cluster)
	assert.Equal(t, "dev-user", ctxs[0].User)
	assert.False(t, ctxs[0].Current)

	assert.Equal(t, "staging", ctxs[1].Name)
	assert.Equal(t, "staging-cluster", ctxs[1].Cluster)
	assert.Equal(t, "staging-user", ctxs[1].User)
	assert.True(t, ctxs[1].Current)

	assert.Equal(t, "prod", ctxs[2].Name)
	assert.Equal(t, "prod-cluster", ctxs[2].Cluster)
	assert.Equal(t, "prod-user", ctxs[2].User)
	assert.False(t, ctxs[2].Current)
}

func TestParseContexts_SingleContext(t *testing.T) {
	dir := t.TempDir()
	p := writeKubeconfig(t, dir, "config", singleContextKubeconfig)

	ctxs, err := ParseContexts(p)
	require.NoError(t, err)
	require.Len(t, ctxs, 1)

	assert.Equal(t, "only", ctxs[0].Name)
	assert.Equal(t, "only-cluster", ctxs[0].Cluster)
	assert.Equal(t, "only-user", ctxs[0].User)
	assert.True(t, ctxs[0].Current)
}

func TestParseContexts_NoContexts(t *testing.T) {
	dir := t.TempDir()
	p := writeKubeconfig(t, dir, "config", noContextKubeconfig)

	ctxs, err := ParseContexts(p)
	require.NoError(t, err)
	assert.Empty(t, ctxs)
}

func TestParseContexts_FileNotFound(t *testing.T) {
	_, err := ParseContexts("/nonexistent/kubeconfig")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading kubeconfig")
}

func TestParseContexts_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	p := writeKubeconfig(t, dir, "config", "{{invalid yaml")

	_, err := ParseContexts(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing kubeconfig")
}

func TestParseContexts_NoCurrentContext(t *testing.T) {
	dir := t.TempDir()
	content := `apiVersion: v1
kind: Config
contexts:
- name: alpha
  context:
    cluster: alpha-cluster
    user: alpha-user
`
	p := writeKubeconfig(t, dir, "config", content)

	ctxs, err := ParseContexts(p)
	require.NoError(t, err)
	require.Len(t, ctxs, 1)
	assert.Equal(t, "alpha", ctxs[0].Name)
	assert.False(t, ctxs[0].Current)
}

// --- DiscoverKubeconfigs Tests ---

func TestDiscoverKubeconfigs_KubeDir(t *testing.T) {
	dir := t.TempDir()
	writeKubeconfig(t, dir, "config", singleContextKubeconfig)
	writeKubeconfig(t, dir, "config-staging", multiContextKubeconfig)
	// Should be excluded: .lock file
	writeKubeconfig(t, dir, "config.lock", "ignored")
	// Should be excluded: non-config file
	writeKubeconfig(t, dir, "other.yaml", "ignored")
	// Should be excluded: directory named config-dir
	require.NoError(t, os.Mkdir(filepath.Join(dir, "config-dir"), 0755))

	entries := DiscoverKubeconfigs(dir, "")
	require.Len(t, entries, 2)

	// Sorted by path
	assert.Contains(t, entries[0].Path, "config")
	assert.Contains(t, entries[1].Path, "config-staging")
	assert.Equal(t, 1, entries[0].ContextCount)
	assert.Equal(t, 3, entries[1].ContextCount)
}

func TestDiscoverKubeconfigs_EnvKubeconfig(t *testing.T) {
	dir := t.TempDir()
	p1 := writeKubeconfig(t, dir, "kc1", singleContextKubeconfig)
	p2 := writeKubeconfig(t, dir, "kc2", multiContextKubeconfig)

	envVal := p1 + ":" + p2
	entries := DiscoverKubeconfigs("", envVal)
	require.Len(t, entries, 2)

	// Sorted by path
	paths := []string{entries[0].Path, entries[1].Path}
	assert.Contains(t, paths, p1)
	assert.Contains(t, paths, p2)
}

func TestDiscoverKubeconfigs_EnvAndDir_Deduplicated(t *testing.T) {
	dir := t.TempDir()
	p := writeKubeconfig(t, dir, "config", singleContextKubeconfig)

	// Same file referenced in both env and dir
	entries := DiscoverKubeconfigs(dir, p)
	require.Len(t, entries, 1)
	assert.Equal(t, p, entries[0].Path)
	assert.Equal(t, 1, entries[0].ContextCount)
}

func TestDiscoverKubeconfigs_EnvNonexistentIgnored(t *testing.T) {
	entries := DiscoverKubeconfigs("", "/nonexistent/path1:/nonexistent/path2")
	assert.Empty(t, entries)
}

func TestDiscoverKubeconfigs_EmptyInputs(t *testing.T) {
	entries := DiscoverKubeconfigs("", "")
	assert.Empty(t, entries)
}

func TestDiscoverKubeconfigs_NonexistentDir(t *testing.T) {
	entries := DiscoverKubeconfigs("/nonexistent/dir", "")
	assert.Empty(t, entries)
}

func TestDiscoverKubeconfigs_EnvDirectoryIgnored(t *testing.T) {
	dir := t.TempDir()
	// Directory should be skipped in env path
	entries := DiscoverKubeconfigs("", dir)
	assert.Empty(t, entries)
}

func TestDiscoverKubeconfigs_SortedByPath(t *testing.T) {
	dir := t.TempDir()
	writeKubeconfig(t, dir, "config-z", singleContextKubeconfig)
	writeKubeconfig(t, dir, "config-a", singleContextKubeconfig)
	writeKubeconfig(t, dir, "config-m", singleContextKubeconfig)

	entries := DiscoverKubeconfigs(dir, "")
	require.Len(t, entries, 3)

	assert.Contains(t, entries[0].Path, "config-a")
	assert.Contains(t, entries[1].Path, "config-m")
	assert.Contains(t, entries[2].Path, "config-z")
}

func TestDiscoverKubeconfigs_InvalidFileCountsZero(t *testing.T) {
	dir := t.TempDir()
	writeKubeconfig(t, dir, "config", "{{not valid yaml")

	entries := DiscoverKubeconfigs(dir, "")
	require.Len(t, entries, 1)
	assert.Equal(t, 0, entries[0].ContextCount)
}

func TestDiscoverKubeconfigs_EnvEmptyPathsIgnored(t *testing.T) {
	dir := t.TempDir()
	p := writeKubeconfig(t, dir, "kc", singleContextKubeconfig)

	// Empty segments should be ignored
	envVal := ":" + p + "::"
	entries := DiscoverKubeconfigs("", envVal)
	require.Len(t, entries, 1)
	assert.Equal(t, p, entries[0].Path)
}
