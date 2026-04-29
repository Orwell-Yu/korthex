package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Orwell-Yu/korthex/internal/config"
	"github.com/Orwell-Yu/korthex/internal/history"
	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/Orwell-Yu/korthex/internal/llm"
	"github.com/Orwell-Yu/korthex/pkg/logparse"
	"github.com/Orwell-Yu/korthex/pkg/redact"
)

// agentImpl is the concrete implementation of Agent.
type agentImpl struct {
	provider llm.Provider
	tools    ToolExecutor
	safety   SafetyChecker
	history  *HistoryManager
	parser   logparse.Parser

	// Phase 2: redaction and persistence
	redactEngine     *redact.Engine
	historyStore     history.Store
	sessionID        string
	redactionEnabled bool

	clusterCtx    ClusterContext
	maxIterations int
	sendLogs      bool
}

// New creates an Agent with the given dependencies.
func New(provider llm.Provider, k8sClient k8s.Client, parser logparse.Parser, cfg config.AgentConfig, sendLogs bool, redactEngine *redact.Engine, historyStore history.Store, sessionID string) Agent {
	maxIter := cfg.MaxIterations
	// -1 means unlimited; 0 or unset also treated as unlimited
	if maxIter == 0 {
		maxIter = -1
	}
	maxTurns := cfg.MaxHistoryTurns
	if maxTurns <= 0 {
		maxTurns = 20
	}

	redactionEnabled := redactEngine != nil

	a := &agentImpl{
		provider:         provider,
		tools:            NewToolExecutor(k8sClient),
		safety:           NewSafetyChecker(),
		parser:           parser,
		maxIterations:    maxIter,
		sendLogs:         sendLogs,
		redactEngine:     redactEngine,
		historyStore:     historyStore,
		sessionID:        sessionID,
		redactionEnabled: redactionEnabled,
	}

	systemPrompt := BuildSystemPrompt(a.clusterCtx, provider.ProviderName(), sendLogs, redactionEnabled)
	a.history = NewHistoryManager(maxTurns, systemPrompt)

	return a
}

// SetClusterContext updates the cluster context and rebuilds the system prompt.
func (a *agentImpl) SetClusterContext(ctx ClusterContext) {
	a.clusterCtx = ctx
	systemPrompt := BuildSystemPrompt(ctx, a.provider.ProviderName(), a.sendLogs, a.redactionEnabled)
	a.history.SetSystemPrompt(systemPrompt)
}

// SetLogBufferReader injects the Log Viewer buffer reader into the tool executor.
func (a *agentImpl) SetLogBufferReader(reader LogBufferReader) {
	if te, ok := a.tools.(*toolExecutor); ok {
		te.logBuffer = reader
	}
}

// ClearHistory resets conversation history.
func (a *agentImpl) ClearHistory() {
	a.history.ClearHistory()
}

// Execute runs the agentic loop: user query → LLM → tool calls → iterate.
func (a *agentImpl) Execute(ctx context.Context, userQuery string, ch chan<- AgentEvent) error {
	a.history.AppendUserMessage(userQuery)
	a.persistMessage(ctx, "user", userQuery)

	for iteration := 1; a.maxIterations < 0 || iteration <= a.maxIterations; iteration++ {
		slog.Debug("agentic loop iteration", "iteration", iteration, "maxIterations", a.maxIterations)

		// Call LLM with retry for rate limit / timeout
		response, err := a.callLLMWithRetry(ctx)
		if err != nil {
			ch <- AgentEvent{
				Type:      EventError,
				Iteration: iteration,
				MaxIter:   a.maxIterations,
				Text:      fmt.Sprintf("LLM error: %v", err),
			}
			ch <- AgentEvent{Type: EventComplete, Iteration: iteration, MaxIter: a.maxIterations}
			return err
		}

		// No tool calls → LLM finished reasoning
		if len(response.ToolCalls) == 0 {
			a.history.AppendAssistantMessage(*response)
			a.persistMessage(ctx, "assistant", response.Content)
			ch <- AgentEvent{
				Type:      EventSummary,
				Iteration: iteration,
				MaxIter:   a.maxIterations,
				Text:      response.Content,
			}
			ch <- AgentEvent{Type: EventComplete, Iteration: iteration, MaxIter: a.maxIterations}
			return nil
		}

		// Has tool calls → execute them
		a.history.AppendAssistantMessage(*response)

		for _, tc := range response.ToolCalls {
			// Safety check
			level, reason := a.safety.Check(tc.Name, tc.Arguments)
			if level == SafetyDenied {
				slog.Warn("safety denied", "tool", tc.Name, "reason", reason)
				ch <- AgentEvent{
					Type:      EventError,
					Iteration: iteration,
					MaxIter:   a.maxIterations,
					Text:      "denied: " + reason,
					ToolName:  tc.Name,
				}
				a.history.AppendToolResult(tc.ID, tc.Name, "DENIED: "+reason)
				continue
			}

			// Emit tool call event with command display
			cmdDisplay := GenerateCommandDisplay(tc.Name, tc.Arguments)
			ch <- AgentEvent{
				Type:           EventToolCall,
				Iteration:      iteration,
				MaxIter:        a.maxIterations,
				ToolName:       tc.Name,
				ToolArgs:       tc.Arguments,
				CommandDisplay: cmdDisplay,
			}

			// Execute tool
			result, logs, err := a.tools.ExecuteTool(ctx, tc.Name, tc.Arguments)
			if err != nil {
				slog.Warn("tool execution error", "tool", tc.Name, "error", err)
				ch <- AgentEvent{
					Type:      EventError,
					Iteration: iteration,
					MaxIter:   a.maxIterations,
					Text:      err.Error(),
					ToolName:  tc.Name,
				}
				a.history.AppendToolResult(tc.ID, tc.Name, "ERROR: "+err.Error())
				continue
			}

			// Emit logs if available
			if len(logs) > 0 {
				ch <- AgentEvent{
					Type:      EventLogsReady,
					Iteration: iteration,
					MaxIter:   a.maxIterations,
					LogLines:  logs,
				}
			}

			// Determine what to send to LLM based on send_logs config
			llmResult := result
			if !a.sendLogs && len(logs) > 0 {
				llmResult = fmt.Sprintf("Success: %d log lines returned", len(logs))
			}
			// search_visible_logs embeds log content in result (not in logs slice).
			// When send_logs=false, replace with status-only message.
			if !a.sendLogs && tc.Name == "search_visible_logs" && result != "" {
				llmResult = redactSearchResult(result)
			}

			// Phase 2: Redaction pipeline — redact before sending to LLM.
			if a.redactEngine != nil && llmResult != "" {
				redacted, stats := a.redactEngine.Redact([]byte(llmResult))
				llmResult = string(redacted)
				if statsStr := redact.FormatStats(stats); statsStr != "" {
					llmResult += "\n" + statsStr
				}
			}

			ch <- AgentEvent{
				Type:       EventToolResult,
				Iteration:  iteration,
				MaxIter:    a.maxIterations,
				ToolName:   tc.Name,
				ToolResult: llmResult,
			}
			a.history.AppendToolResult(tc.ID, tc.Name, llmResult)
			a.persistMessage(ctx, "tool_result", llmResult)

			// Special: switch_kubeconfig emits EventKubeSwitch for UI (only on successful validation)
			if tc.Name == "switch_kubeconfig" && !strings.HasPrefix(result, "ERROR") {
				kc := tc.Arguments["kubeconfig"]
				if kc == "" {
					if te, ok := a.tools.(*toolExecutor); ok {
						kp, _ := te.k8sClient.ContextInfo()
						kc = kp
					}
				}
				ch <- AgentEvent{
					Type:             EventKubeSwitch,
					SwitchKubeconfig: kc,
					SwitchContext:    tc.Arguments["context"],
				}
			}
		}

		// Loop continues: LLM sees tool results on next iteration
	}

	// Max iterations reached
	slog.Warn("max iterations reached", "maxIterations", a.maxIterations)
	ch <- AgentEvent{
		Type:      EventError,
		Iteration: a.maxIterations,
		MaxIter:   a.maxIterations,
		Text:      "Max iterations reached",
	}
	ch <- AgentEvent{Type: EventComplete, Iteration: a.maxIterations, MaxIter: a.maxIterations}
	return nil
}

// persistMessage writes a message to the history store (fire-and-forget).
func (a *agentImpl) persistMessage(ctx context.Context, role, content string) {
	if a.historyStore == nil || a.sessionID == "" {
		return
	}
	if err := a.historyStore.AddMessage(ctx, a.sessionID, history.Message{
		Role:    role,
		Content: content,
	}); err != nil {
		slog.Warn("history persist failed", "role", role, "error", err)
	}
}

// callLLMWithRetry calls LLM.Chat with one automatic retry for rate limit / timeout errors.
func (a *agentImpl) callLLMWithRetry(ctx context.Context) (*llm.Message, error) {
	msgs := a.history.GetMessages()
	tools := a.tools.ToolDefinitions()

	response, err := a.provider.Chat(ctx, msgs, tools)
	if err == nil {
		return response, nil
	}

	// Only retry for rate limit / timeout
	if !errors.Is(err, llm.ErrRateLimit) && !errors.Is(err, llm.ErrTimeout) {
		return nil, err
	}

	slog.Warn("LLM call failed, retrying", "error", err)
	// Exponential backoff: wait 2 seconds before retry
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
	}

	return a.provider.Chat(ctx, msgs, tools)
}

// redactSearchResult keeps the first line (match count header) from a
// search_visible_logs result and strips the actual log content.
// Used when send_logs=false to respect the user's privacy setting.
func redactSearchResult(result string) string {
	if idx := strings.Index(result, "\n"); idx != -1 {
		return strings.TrimRight(result[:idx], ":\n") + " (log content not sent per send_logs=false)"
	}
	return result + " (log content not sent per send_logs=false)"
}
