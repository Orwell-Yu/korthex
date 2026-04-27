package app

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"k8s.io/klog/v2"

	"github.com/Orwell-Yu/korthex/internal/agent"
	"github.com/Orwell-Yu/korthex/internal/config"
	"github.com/Orwell-Yu/korthex/internal/history"
	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/Orwell-Yu/korthex/internal/llm"
	"github.com/Orwell-Yu/korthex/internal/ui"
	"github.com/Orwell-Yu/korthex/pkg/logparse"
	"github.com/Orwell-Yu/korthex/pkg/redact"

	tea "github.com/charmbracelet/bubbletea"
)

// App is the top-level application lifecycle manager.
// It wires all modules together and manages startup/shutdown.
type App struct {
	config       *config.Config
	k8sClient    k8s.Client
	llmProvider  llm.Provider
	agent        agent.Agent
	logFile      *os.File
	historyStore history.Store
	sessionID    string
}

// New creates a new App instance, initializing all dependencies in order:
// logging → K8s client → LLM provider → agent.
func New(cfg *config.Config) (*App, error) {
	// 1. Setup logging (slog → file, per SPEC §6.8)
	logDir := filepath.Join(os.TempDir(), "korthex")
	if err := os.MkdirAll(logDir, 0750); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	logFile, err := os.OpenFile( //nolint:gosec // log path is derived from os.TempDir(), not user input
		filepath.Join(logDir, "korthex.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600,
	)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	level := slog.LevelWarn
	if os.Getenv("KORTHEX_LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: level})))
	slog.Info("korthex starting", "config_provider", cfg.LLM.Provider)

	// Redirect client-go's klog to the same log file to prevent TUI pollution.
	// klog v2 requires flag-based init to fully suppress stderr output.
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	_ = fs.Set("logtostderr", "false")
	_ = fs.Set("stderrthreshold", "FATAL")
	klog.SetOutput(logFile)

	// 2. Create and connect K8s client
	k8sClient := k8s.NewClient()
	if err := k8sClient.Connect(cfg.Kubernetes.Kubeconfig, cfg.Kubernetes.DefaultContext); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("k8s connect: %w", err)
	}
	slog.Info("k8s connected", "context", k8sClient.CurrentContext())

	// 3. Create LLM provider and validate connection
	registry := llm.NewRegistry()
	llmProvider, err := registry.Create(cfg.LLM)
	if err != nil {
		k8sClient.Disconnect()
		_ = logFile.Close()
		return nil, fmt.Errorf("create llm provider: %w", err)
	}

	validateCtx, validateCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer validateCancel()
	if err := llmProvider.ValidateConnection(validateCtx); err != nil {
		k8sClient.Disconnect()
		_ = logFile.Close()
		return nil, fmt.Errorf("llm validate: %w", err)
	}
	slog.Info("llm validated", "provider", llmProvider.ProviderName(), "model", llmProvider.ModelName())

	// 4. Create redaction engine (Phase 2)
	var redactEngine *redact.Engine
	if cfg.Privacy.Redaction.Enabled {
		redactEngine = redact.NewEngine(redact.RedactionConfig{
			Enabled:        cfg.Privacy.Redaction.Enabled,
			DisableBuiltin: cfg.Privacy.Redaction.DisableBuiltin,
			Rules:          convertRuleConfigs(cfg.Privacy.Redaction.Rules),
		})
		slog.Info("redaction engine enabled")
	}

	// 5. Create history store (Phase 2)
	var historyStore history.Store
	var sessionID string
	if cfg.History.Enabled && cfg.History.DBPath != "" {
		var err2 error
		historyStore, err2 = history.NewStore(cfg.History.DBPath)
		if err2 != nil {
			slog.Warn("history store unavailable, continuing without persistence", "error", err2)
		} else {
			cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cleanCancel()
			if err3 := historyStore.CleanExpired(cleanCtx, cfg.History.RetentionDays); err3 != nil {
				slog.Warn("history cleanup failed", "error", err3)
			}
			sessCtx, sessCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer sessCancel()
			sessionID, err2 = historyStore.CreateSession(sessCtx, k8sClient.CurrentContext())
			if err2 != nil {
				slog.Warn("history session creation failed", "error", err2)
			}
			slog.Info("history store initialized", "db_path", cfg.History.DBPath, "session", sessionID)
		}
	}

	// 6. Create agent
	parser := logparse.NewParser()
	agentInstance := agent.New(llmProvider, k8sClient, parser, cfg.Agent, cfg.LLM.SendLogs, redactEngine, historyStore, sessionID)

	return &App{
		config:       cfg,
		k8sClient:    k8sClient,
		llmProvider:  llmProvider,
		agent:        agentInstance,
		logFile:      logFile,
		historyStore: historyStore,
		sessionID:    sessionID,
	}, nil
}

// Run starts the Bubble Tea TUI and blocks until it exits.
// It monitors ctx for cancellation (e.g. SIGTERM) and gracefully quits the TUI.
func (a *App) Run(ctx context.Context) error {
	appModel := ui.NewAppModel(a.agent, a.k8sClient, a.config, a.historyStore)
	a.agent.SetLogBufferReader(appModel.LogBuffer())
	p := tea.NewProgram(&appModel, tea.WithAltScreen(), tea.WithMouseCellMotion())
	appModel.SetProgram(p)

	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	// Clear the main screen buffer so the pre-TUI splash doesn't reappear.
	fmt.Print("\033[2J\033[H")
	return nil
}

// Shutdown performs graceful cleanup of all resources.
func (a *App) Shutdown() {
	// End history session
	if a.historyStore != nil && a.sessionID != "" {
		endCtx, endCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer endCancel()
		if err := a.historyStore.EndSession(endCtx, a.sessionID, ""); err != nil {
			slog.Warn("history session end failed", "error", err)
		}
		if err := a.historyStore.Close(); err != nil {
			slog.Warn("history store close failed", "error", err)
		}
	}

	if a.k8sClient != nil {
		a.k8sClient.Disconnect()
	}
	slog.Info("korthex shutdown complete")
	klog.Flush()
	if a.logFile != nil {
		_ = a.logFile.Close()
	}
}

// convertRuleConfigs converts config.RuleConfig to redact.RuleConfig.
func convertRuleConfigs(rules []config.RuleConfig) []redact.RuleConfig {
	out := make([]redact.RuleConfig, len(rules))
	for i, r := range rules {
		out[i] = redact.RuleConfig{
			Name:        r.Name,
			Pattern:     r.Pattern,
			Replacement: r.Replacement,
		}
	}
	return out
}
