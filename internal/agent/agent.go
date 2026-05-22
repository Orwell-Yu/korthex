package agent

import (
	"context"

	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/Orwell-Yu/korthex/internal/llm"
	"github.com/Orwell-Yu/korthex/pkg/logparse"
)

// Agent is the core AI conversation orchestrator.
type Agent interface {
	Execute(ctx context.Context, userQuery string, ch chan<- AgentEvent) error
	ClearHistory()
	SetClusterContext(ctx ClusterContext)
	SetLogBufferReader(reader LogBufferReader)
}

// LogBufferReader provides read access to the TUI's Log Viewer buffer.
// Implemented by *ui.RingBuffer via implicit interface satisfaction.
type LogBufferReader interface {
	Slice() []logparse.LogEntry
	Len() int
}

// ClusterContext provides cluster state for the agent's system prompt.
type ClusterContext struct {
	ContextName string
	Namespace   string   // currently focused namespace
	Deployments []string // known deployments in current namespace
}

// AgentEvent is the event stream consumed by the UI.
type AgentEvent struct {
	Type      AgentEventType
	Iteration int
	MaxIter   int

	// Populated based on Type:
	Text           string            // EventStreamDelta, EventSummary, EventError
	ToolName       string            // EventToolCall, EventToolResult
	ToolArgs       map[string]string // EventToolCall
	ToolResult     string            // EventToolResult (may be truncated)
	CommandDisplay string            // EventToolCall: human-readable kubectl equivalent
	LogLines       []k8s.LogLine     // EventLogsReady: for Log Viewer display
	StreamDelta    string            // EventStreamDelta: LLM incremental text

	// EventKubeSwitch fields
	SwitchKubeconfig string // kubeconfig path (empty = keep current)
	SwitchContext    string // target context name

	// EventMetricsUpdate fields
	Metrics AgentMetrics // current snapshot of token metrics
}

// AgentEventType enumerates the types of agent events.
type AgentEventType int

const (
	EventStreamDelta  AgentEventType = iota // LLM incremental text
	EventToolCall                           // Agent is invoking a tool
	EventToolResult                         // Tool returned result
	EventLogsReady                          // Log lines ready for Log Viewer
	EventSummary                            // Final analysis summary
	EventError                              // Error (may trigger retry)
	EventComplete                           // Agentic loop completed
	EventKubeSwitch                         // Agent requests kubeconfig/context switch
	EventMetricsUpdate                      // Token metrics snapshot updated
)

// ToolExecutor dispatches tool calls to K8s operations.
type ToolExecutor interface {
	ExecuteTool(ctx context.Context, name string, args map[string]string) (result string, logs []k8s.LogLine, err error)
	ToolDefinitions() []llm.ToolDefinition
}

// SafetyLevel represents the safety classification of an operation.
type SafetyLevel int

const (
	SafetyAllowed   SafetyLevel = iota // Phase 1: execute directly (get, list, logs, describe, events)
	SafetyDangerous                    // Phase 3: triple confirmation (scale, rollout, restart)
	SafetyCritical                     // Phase 3: triple confirmation + type resource name (delete, drain)
	SafetyDenied                       // Not supported in current Phase
)

// SafetyChecker validates whether a tool operation is permitted.
type SafetyChecker interface {
	Check(toolName string, args map[string]string) (level SafetyLevel, reason string)
}
