package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Orwell-Yu/korthex/internal/config"
	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/Orwell-Yu/korthex/internal/llm"
	"github.com/Orwell-Yu/korthex/pkg/logparse"
)

// mockProvider implements llm.Provider for testing.
type mockProvider struct {
	chatResponses []*llm.Message
	chatErrors    []error
	callIndex     int
	providerName  string
	modelName     string
}

func (m *mockProvider) Chat(_ context.Context, _ []llm.Message, _ []llm.ToolDefinition) (*llm.Message, error) {
	if m.callIndex >= len(m.chatResponses) {
		return &llm.Message{Role: llm.RoleAssistant, Content: "no more responses configured"}, nil
	}
	idx := m.callIndex
	m.callIndex++
	var err error
	if idx < len(m.chatErrors) {
		err = m.chatErrors[idx]
	}
	return m.chatResponses[idx], err
}

func (m *mockProvider) ChatStream(_ context.Context, _ []llm.Message, _ []llm.ToolDefinition, _ chan<- llm.StreamDelta) error {
	return nil
}

func (m *mockProvider) ModelName() string {
	if m.modelName != "" {
		return m.modelName
	}
	return "mock-model"
}

func (m *mockProvider) ProviderName() string {
	if m.providerName != "" {
		return m.providerName
	}
	return "mock"
}

func (m *mockProvider) ValidateConnection(_ context.Context) error { return nil }

// collectEvents runs Execute and collects all emitted events.
func collectEvents(t *testing.T, agent Agent, query string) []AgentEvent {
	t.Helper()
	ch := make(chan AgentEvent, 100)
	ctx := context.Background()

	go func() {
		_ = agent.Execute(ctx, query, ch)
	}()

	var events []AgentEvent
	for evt := range ch {
		events = append(events, evt)
		if evt.Type == EventComplete {
			break
		}
	}
	return events
}

func newTestAgent(provider llm.Provider, k8sClient k8s.Client, sendLogs bool) Agent {
	cfg := config.AgentConfig{MaxIterations: 3, MaxHistoryTurns: 10}
	return New(provider, k8sClient, logparse.NewParser(), cfg, sendLogs, nil, nil, "")
}

// --- Test Cases ---

func TestExecute_SingleToolCall_Success(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListNamespacesFunc: func() ([]k8s.Namespace, error) {
				return []k8s.Namespace{
					{Name: "default", Status: "Active"},
					{Name: "kube-system", Status: "Active"},
				}, nil
			},
		},
	}

	provider := &mockProvider{
		chatResponses: []*llm.Message{
			{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{ID: "tc1", Name: "kubectl_get_namespaces", Arguments: map[string]string{}},
				},
			},
			{
				Role:    llm.RoleAssistant,
				Content: "You have 2 namespaces: default and kube-system.",
			},
		},
	}

	agent := newTestAgent(provider, mockK8s, true)
	events := collectEvents(t, agent, "list namespaces")

	// Expect: EventToolCall, EventToolResult, EventSummary, EventComplete
	hasToolCall := false
	hasToolResult := false
	hasSummary := false
	hasComplete := false

	for _, evt := range events {
		switch evt.Type {
		case EventToolCall:
			hasToolCall = true
			if evt.ToolName != "kubectl_get_namespaces" {
				t.Errorf("expected tool name kubectl_get_namespaces, got %s", evt.ToolName)
			}
			if evt.CommandDisplay != "kubectl get namespaces" {
				t.Errorf("expected command display 'kubectl get namespaces', got %q", evt.CommandDisplay)
			}
		case EventToolResult:
			hasToolResult = true
			if !strings.Contains(evt.ToolResult, "default") {
				t.Error("tool result should contain 'default'")
			}
		case EventSummary:
			hasSummary = true
			if !strings.Contains(evt.Text, "2 namespaces") {
				t.Errorf("summary should mention namespaces, got %q", evt.Text)
			}
		case EventComplete:
			hasComplete = true
		case EventStreamDelta, EventLogsReady, EventError, EventKubeSwitch:
			// not asserted in this test
		}
	}

	if !hasToolCall {
		t.Error("missing EventToolCall")
	}
	if !hasToolResult {
		t.Error("missing EventToolResult")
	}
	if !hasSummary {
		t.Error("missing EventSummary")
	}
	if !hasComplete {
		t.Error("missing EventComplete")
	}
}

func TestExecute_MultipleToolCalls(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListNamespacesFunc: func() ([]k8s.Namespace, error) {
				return []k8s.Namespace{{Name: "default", Status: "Active"}}, nil
			},
			ListDeploymentsFunc: func(ns string) ([]k8s.Deployment, error) {
				return []k8s.Deployment{{Name: "web", Namespace: ns, Replicas: 3, Ready: 3}}, nil
			},
		},
	}

	provider := &mockProvider{
		chatResponses: []*llm.Message{
			{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{ID: "tc1", Name: "kubectl_get_namespaces", Arguments: map[string]string{}},
					{ID: "tc2", Name: "kubectl_get_deployments", Arguments: map[string]string{"namespace": "default"}},
				},
			},
			{
				Role:    llm.RoleAssistant,
				Content: "Found 1 namespace and 1 deployment.",
			},
		},
	}

	agent := newTestAgent(provider, mockK8s, true)
	events := collectEvents(t, agent, "overview")

	toolCallCount := 0
	toolResultCount := 0
	for _, evt := range events {
		if evt.Type == EventToolCall {
			toolCallCount++
		}
		if evt.Type == EventToolResult {
			toolResultCount++
		}
	}

	if toolCallCount != 2 {
		t.Errorf("expected 2 tool calls, got %d", toolCallCount)
	}
	if toolResultCount != 2 {
		t.Errorf("expected 2 tool results, got %d", toolResultCount)
	}
}

func TestExecute_SelfCorrection(t *testing.T) {
	callCount := 0
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
				callCount++
				if callCount == 1 {
					return nil, fmt.Errorf("namespace not found: invalid-ns")
				}
				return []k8s.Pod{{Name: "web-abc", Namespace: ns, Status: "Running"}}, nil
			},
		},
	}

	provider := &mockProvider{
		chatResponses: []*llm.Message{
			// Iteration 1: LLM calls with wrong namespace
			{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{ID: "tc1", Name: "kubectl_get_pods", Arguments: map[string]string{"namespace": "invalid-ns"}},
				},
			},
			// Iteration 2: LLM corrects itself
			{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{ID: "tc2", Name: "kubectl_get_pods", Arguments: map[string]string{"namespace": "default"}},
				},
			},
			// Iteration 3: LLM summarizes
			{
				Role:    llm.RoleAssistant,
				Content: "Found pod web-abc in default namespace.",
			},
		},
	}

	agent := newTestAgent(provider, mockK8s, true)
	events := collectEvents(t, agent, "list pods")

	hasError := false
	hasSummary := false
	for _, evt := range events {
		if evt.Type == EventError && strings.Contains(evt.Text, "namespace not found") {
			hasError = true
		}
		if evt.Type == EventSummary {
			hasSummary = true
		}
	}

	if !hasError {
		t.Error("expected error event from first failed attempt")
	}
	if !hasSummary {
		t.Error("expected summary after self-correction")
	}
}

func TestExecute_MaxIterationsReached(t *testing.T) {
	mockK8s := &k8s.MockClient{}

	// LLM always returns tool calls, never finishes
	provider := &mockProvider{
		chatResponses: []*llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "tc1", Name: "kubectl_get_namespaces", Arguments: map[string]string{}},
			}},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "tc2", Name: "kubectl_get_namespaces", Arguments: map[string]string{}},
			}},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "tc3", Name: "kubectl_get_namespaces", Arguments: map[string]string{}},
			}},
		},
	}

	agent := newTestAgent(provider, mockK8s, true)
	events := collectEvents(t, agent, "something")

	hasMaxIterError := false
	hasComplete := false
	for _, evt := range events {
		if evt.Type == EventError && strings.Contains(evt.Text, "Max iterations reached") {
			hasMaxIterError = true
		}
		if evt.Type == EventComplete {
			hasComplete = true
		}
	}

	if !hasMaxIterError {
		t.Error("expected 'Max iterations reached' error")
	}
	if !hasComplete {
		t.Error("expected EventComplete after max iterations")
	}
}

func TestExecute_SafetyDenied(t *testing.T) {
	mockK8s := &k8s.MockClient{}

	provider := &mockProvider{
		chatResponses: []*llm.Message{
			{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{ID: "tc1", Name: "kubectl_delete_pod", Arguments: map[string]string{"namespace": "prod", "name": "web-abc"}},
				},
			},
			{
				Role:    llm.RoleAssistant,
				Content: "I cannot delete pods. Only read-only operations are available.",
			},
		},
	}

	agent := newTestAgent(provider, mockK8s, true)
	events := collectEvents(t, agent, "delete pod web-abc")

	hasDenied := false
	for _, evt := range events {
		if evt.Type == EventError && strings.Contains(evt.Text, "denied") {
			hasDenied = true
		}
	}

	if !hasDenied {
		t.Error("expected denial event for non-whitelisted tool")
	}
}

func TestSafetyChecker_AllowsAllReadOnlyTools(t *testing.T) {
	checker := NewSafetyChecker()
	tools := []string{
		"kubectl_get_namespaces",
		"kubectl_get_deployments",
		"kubectl_get_pods",
		"kubectl_logs",
		"kubectl_logs_selector",
		"kubectl_describe",
		"kubectl_get_events",
		"navigate_resource_browser",
		"get_log_viewer_state",
		"search_visible_logs",
		// Phase 2
		"kubectl_get_statefulsets",
		"kubectl_get_daemonsets",
		"kubectl_get_jobs",
		"kubectl_get_cronjobs",
		"severity_stats",
		"compare_logs",
		"get_pod_metrics",
		"trace_logs",
		"bookmark_log_lines",
		"list_kubeconfigs",
		"switch_kubeconfig",
	}

	for _, tool := range tools {
		level, reason := checker.Check(tool, nil)
		if level != SafetyAllowed {
			t.Errorf("expected SafetyAllowed for %s, got level=%d reason=%q", tool, level, reason)
		}
	}
}

func TestSafetyChecker_DeniesWriteOperations(t *testing.T) {
	checker := NewSafetyChecker()
	tools := []string{
		"kubectl_delete_pod",
		"kubectl_scale",
		"kubectl_apply",
		"kubectl_restart",
	}

	for _, tool := range tools {
		level, reason := checker.Check(tool, nil)
		if level != SafetyDenied {
			t.Errorf("expected SafetyDenied for %s, got level=%d", tool, level)
		}
		if reason == "" {
			t.Errorf("expected denial reason for %s", tool)
		}
	}
}

func TestExecute_SendLogsFalse(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockLogs: &k8s.MockLogStreamer{
			GetLogsFunc: func(_ context.Context, req k8s.LogRequest) ([]k8s.LogLine, error) {
				return []k8s.LogLine{
					{PodName: "web-abc", Content: "ERROR: something failed"},
					{PodName: "web-abc", Content: "INFO: recovering"},
					{PodName: "web-abc", Content: "INFO: ready"},
				}, nil
			},
		},
	}

	provider := &mockProvider{
		chatResponses: []*llm.Message{
			{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{ID: "tc1", Name: "kubectl_logs", Arguments: map[string]string{
						"namespace": "default",
						"podName":   "web-abc",
					}},
				},
			},
			{
				Role:    llm.RoleAssistant,
				Content: "3 log lines returned. Check the Log Viewer for details.",
			},
		},
	}

	agent := newTestAgent(provider, mockK8s, false) // sendLogs=false
	events := collectEvents(t, agent, "show logs for web-abc")

	hasLogsReady := false
	var toolResultText string
	for _, evt := range events {
		if evt.Type == EventLogsReady {
			hasLogsReady = true
			if len(evt.LogLines) != 3 {
				t.Errorf("expected 3 log lines in LogsReady, got %d", len(evt.LogLines))
			}
		}
		if evt.Type == EventToolResult && evt.ToolName == "kubectl_logs" {
			toolResultText = evt.ToolResult
		}
	}

	if !hasLogsReady {
		t.Error("expected EventLogsReady even with sendLogs=false")
	}

	// Tool result should contain only status, not log content
	if !strings.Contains(toolResultText, "Success: 3 log lines returned") {
		t.Errorf("expected status-only tool result, got %q", toolResultText)
	}
	if strings.Contains(toolResultText, "ERROR: something failed") {
		t.Error("tool result should NOT contain actual log content when sendLogs=false")
	}
}

func TestHistory_FIFOTruncation(t *testing.T) {
	hm := NewHistoryManager(3, "system prompt")

	for i := 0; i < 5; i++ {
		hm.AppendUserMessage(fmt.Sprintf("query %d", i))
		hm.AppendAssistantMessage(llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprintf("response %d", i)})
	}

	msgs := hm.GetMessages()

	// System prompt + 3 turns * 2 messages each = 7
	if len(msgs) != 7 {
		t.Errorf("expected 7 messages (1 system + 3*2 turns), got %d", len(msgs))
	}

	// First message is always system prompt
	if msgs[0].Role != llm.RoleSystem {
		t.Error("first message should be system prompt")
	}

	// Oldest turns (0, 1) should be truncated; newest (2, 3, 4) remain
	if msgs[1].Content != "query 2" {
		t.Errorf("expected oldest remaining query to be 'query 2', got %q", msgs[1].Content)
	}
}

func TestHistory_ToolResultCompression(t *testing.T) {
	hm := NewHistoryManager(10, "system")

	hm.AppendUserMessage("query")
	hm.AppendAssistantMessage(llm.Message{Role: llm.RoleAssistant, Content: "calling tool"})

	// Generate a result > 2000 chars
	bigResult := strings.Repeat("INFO: normal log line\n", 200)
	bigResult += "ERROR: something went wrong\n"
	bigResult += strings.Repeat("WARN: potential issue\n", 50)

	hm.AppendToolResult("tc1", "kubectl_logs", bigResult)

	msgs := hm.GetMessages()

	// Find the tool result message
	var toolMsg *llm.Message
	for i := range msgs {
		if msgs[i].Role == llm.RoleTool {
			toolMsg = &msgs[i]
			break
		}
	}

	if toolMsg == nil {
		t.Fatal("tool result message not found")
	}

	if len(toolMsg.Content) >= len(bigResult) {
		t.Error("tool result should be compressed")
	}

	if !strings.Contains(toolMsg.Content, "[TRUNCATED]") {
		t.Error("compressed result should contain [TRUNCATED] marker")
	}

	if !strings.Contains(toolMsg.Content, "ERROR:") {
		t.Error("compressed result should contain ERROR count in stats")
	}
}

func TestExecute_NoToolCalls_DirectSummary(t *testing.T) {
	mockK8s := &k8s.MockClient{}

	provider := &mockProvider{
		chatResponses: []*llm.Message{
			{
				Role:    llm.RoleAssistant,
				Content: "I can help you with Kubernetes operations. What would you like to do?",
			},
		},
	}

	agent := newTestAgent(provider, mockK8s, true)
	events := collectEvents(t, agent, "hello")

	hasSummary := false
	hasComplete := false
	for _, evt := range events {
		if evt.Type == EventSummary {
			hasSummary = true
		}
		if evt.Type == EventComplete {
			hasComplete = true
		}
		if evt.Type == EventToolCall {
			t.Error("should not have tool calls for a greeting")
		}
	}

	if !hasSummary {
		t.Error("expected EventSummary for direct response")
	}
	if !hasComplete {
		t.Error("expected EventComplete")
	}
}

func TestExecute_LLMRetry_RateLimit(t *testing.T) {
	mockK8s := &k8s.MockClient{}

	provider := &mockProvider{
		chatResponses: []*llm.Message{
			nil, // first call fails
			{Role: llm.RoleAssistant, Content: "Recovered after rate limit."},
		},
		chatErrors: []error{
			llm.ErrRateLimit,
			nil,
		},
	}

	cfg := config.AgentConfig{MaxIterations: 3, MaxHistoryTurns: 10}
	agent := New(provider, mockK8s, logparse.NewParser(), cfg, true, nil, nil, "")

	events := collectEvents(t, agent, "hello")

	hasSummary := false
	for _, evt := range events {
		if evt.Type == EventSummary && evt.Text == "Recovered after rate limit." {
			hasSummary = true
		}
	}

	if !hasSummary {
		t.Error("expected successful recovery after rate limit retry")
	}
}

func TestExecute_LLMError_AuthNoRetry(t *testing.T) {
	mockK8s := &k8s.MockClient{}

	provider := &mockProvider{
		chatResponses: []*llm.Message{nil},
		chatErrors:    []error{llm.ErrAuth},
	}

	cfg := config.AgentConfig{MaxIterations: 3, MaxHistoryTurns: 10}
	agent := New(provider, mockK8s, logparse.NewParser(), cfg, true, nil, nil, "")

	events := collectEvents(t, agent, "hello")

	hasError := false
	for _, evt := range events {
		if evt.Type == EventError && strings.Contains(evt.Text, "authentication") {
			hasError = true
		}
	}

	if !hasError {
		t.Error("expected auth error without retry")
	}
}

func TestClearHistory(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	provider := &mockProvider{
		chatResponses: []*llm.Message{
			{Role: llm.RoleAssistant, Content: "response"},
		},
	}

	agent := newTestAgent(provider, mockK8s, true)
	_ = collectEvents(t, agent, "hello")

	agent.ClearHistory()

	// After clear, the internal agentImpl should have fresh history
	impl := agent.(*agentImpl)
	msgs := impl.history.GetMessages()

	// Should only have system prompt
	if len(msgs) != 1 {
		t.Errorf("after ClearHistory, expected only system prompt (1 msg), got %d", len(msgs))
	}
	if msgs[0].Role != llm.RoleSystem {
		t.Error("remaining message should be system prompt")
	}
}

func TestSetClusterContext(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	provider := &mockProvider{providerName: "anthropic"}

	agent := newTestAgent(provider, mockK8s, true)
	agent.SetClusterContext(ClusterContext{
		ContextName: "prod-cluster",
		Namespace:   "production",
		Deployments: []string{"web", "api"},
	})

	impl := agent.(*agentImpl)
	msgs := impl.history.GetMessages()

	systemPrompt := msgs[0].Content
	if !strings.Contains(systemPrompt, "prod-cluster") {
		t.Error("system prompt should contain context name")
	}
	if !strings.Contains(systemPrompt, "production") {
		t.Error("system prompt should contain namespace")
	}
	if !strings.Contains(systemPrompt, "web") {
		t.Error("system prompt should contain deployments")
	}
}

func TestExecute_SendLogsFalse_SearchVisibleLogs(t *testing.T) {
	mockK8s := &k8s.MockClient{}

	provider := &mockProvider{
		chatResponses: []*llm.Message{
			{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{
					{ID: "tc1", Name: "search_visible_logs", Arguments: map[string]string{
						"pattern": "ERROR",
					}},
				},
			},
			{
				Role:    llm.RoleAssistant,
				Content: "Found errors in the logs.",
			},
		},
	}

	a := newTestAgent(provider, mockK8s, false) // sendLogs=false
	// Inject a mock log buffer with content
	impl := a.(*agentImpl)
	te := impl.tools.(*toolExecutor)
	te.logBuffer = &mockLogBufferForAgent{
		entries: []logparse.LogEntry{
			{PodName: "web-abc", Raw: "ERROR: connection refused", Severity: logparse.SeverityError},
			{PodName: "web-abc", Raw: "INFO: retrying", Severity: logparse.SeverityInfo},
		},
	}

	events := collectEvents(t, a, "search for errors")

	var toolResultText string
	for _, evt := range events {
		if evt.Type == EventToolResult && evt.ToolName == "search_visible_logs" {
			toolResultText = evt.ToolResult
		}
	}

	// Tool result sent to LLM should NOT contain log content
	if strings.Contains(toolResultText, "connection refused") {
		t.Error("tool result should NOT contain actual log content when sendLogs=false")
	}
	if !strings.Contains(toolResultText, "send_logs=false") {
		t.Errorf("tool result should mention send_logs=false, got %q", toolResultText)
	}
}

// mockLogBufferForAgent implements LogBufferReader for agent-level tests.
type mockLogBufferForAgent struct {
	entries []logparse.LogEntry
}

func (m *mockLogBufferForAgent) Slice() []logparse.LogEntry { return m.entries }
func (m *mockLogBufferForAgent) Len() int                   { return len(m.entries) }
