package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/Orwell-Yu/korthex/pkg/logparse"
)

func newTestToolExecutor(mockK8s k8s.Client) ToolExecutor {
	return NewToolExecutor(mockK8s)
}

func TestToolExecution_GetNamespaces(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListNamespacesFunc: func() ([]k8s.Namespace, error) {
				return []k8s.Namespace{
					{Name: "default", Status: "Active"},
					{Name: "production", Status: "Active"},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, logs, err := executor.ExecuteTool(context.Background(), "kubectl_get_namespaces", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logs != nil {
		t.Error("get_namespaces should not return log lines")
	}
	if !strings.Contains(result, "default") || !strings.Contains(result, "production") {
		t.Errorf("result should contain namespace names, got: %s", result)
	}
}

func TestToolExecution_GetDeployments(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListDeploymentsFunc: func(ns string) ([]k8s.Deployment, error) {
				if ns != "default" {
					return nil, fmt.Errorf("namespace not found: %s", ns)
				}
				return []k8s.Deployment{
					{Name: "web", Namespace: "default", Replicas: 3, Ready: 3, Available: 3, Selector: map[string]string{"app": "web"}},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)

	result, _, err := executor.ExecuteTool(context.Background(), "kubectl_get_deployments",
		map[string]string{"namespace": "default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "web") {
		t.Errorf("result should contain deployment name, got: %s", result)
	}
	if !strings.Contains(result, "app=web") {
		t.Errorf("result should contain selector, got: %s", result)
	}
}

func TestToolExecution_GetPods(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
				return []k8s.Pod{
					{Name: "web-abc", Namespace: ns, Status: "Running", Restarts: 0, Age: 2 * time.Hour},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, _, err := executor.ExecuteTool(context.Background(), "kubectl_get_pods",
		map[string]string{"namespace": "default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "web-abc") {
		t.Errorf("result should contain pod name, got: %s", result)
	}
	if !strings.Contains(result, "Running") {
		t.Errorf("result should contain pod status, got: %s", result)
	}
}

func TestToolExecution_GetPodsWithSelector(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListPodsBySelectorFunc: func(ns string, sel map[string]string) ([]k8s.Pod, error) {
				if sel["app"] != "web" {
					return nil, nil
				}
				return []k8s.Pod{
					{Name: "web-abc", Namespace: ns, Status: "Running"},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, _, err := executor.ExecuteTool(context.Background(), "kubectl_get_pods",
		map[string]string{"namespace": "default", "labelSelector": "app=web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "web-abc") {
		t.Errorf("result should contain matched pod, got: %s", result)
	}
}

func TestToolExecution_Logs(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockLogs: &k8s.MockLogStreamer{
			GetLogsFunc: func(_ context.Context, req k8s.LogRequest) ([]k8s.LogLine, error) {
				if req.PodName != "web-abc" {
					return nil, fmt.Errorf("pod not found: %s", req.PodName)
				}
				return []k8s.LogLine{
					{PodName: "web-abc", Content: "INFO: started"},
					{PodName: "web-abc", Content: "ERROR: connection refused"},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, logs, err := executor.ExecuteTool(context.Background(), "kubectl_logs",
		map[string]string{"namespace": "default", "podName": "web-abc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 log lines, got %d", len(logs))
	}
	if !strings.Contains(result, "ERROR: connection refused") {
		t.Errorf("result should contain log content, got: %s", result)
	}
}

func TestToolExecution_LogsSelector(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListPodsBySelectorFunc: func(ns string, sel map[string]string) ([]k8s.Pod, error) {
				if sel["app"] == "web" {
					return []k8s.Pod{
						{Name: "web-abc", Namespace: ns},
						{Name: "web-def", Namespace: ns},
					}, nil
				}
				return nil, nil
			},
		},
		MockLogs: &k8s.MockLogStreamer{
			GetLogsFunc: func(_ context.Context, req k8s.LogRequest) ([]k8s.LogLine, error) {
				return []k8s.LogLine{
					{PodName: req.PodName, Content: "log from " + req.PodName},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, logs, err := executor.ExecuteTool(context.Background(), "kubectl_logs_selector",
		map[string]string{"namespace": "default", "labelSelector": "app=web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 log lines (one per pod), got %d", len(logs))
	}
	if !strings.Contains(result, "web-abc") || !strings.Contains(result, "web-def") {
		t.Errorf("result should contain logs from both pods, got: %s", result)
	}
}

func TestToolExecution_Describe(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockDescriber: &k8s.MockResourceDescriber{
			DescribeFunc: func(ns, kind, name string) (string, error) {
				return fmt.Sprintf("Name: %s\nNamespace: %s\nKind: %s\nStatus: Running", name, ns, kind), nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, _, err := executor.ExecuteTool(context.Background(), "kubectl_describe",
		map[string]string{"namespace": "default", "kind": "pod", "name": "web-abc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "web-abc") {
		t.Errorf("result should contain resource name, got: %s", result)
	}
}

func TestToolExecution_GetEvents(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockEvents: &k8s.MockEventLister{
			ListEventsFunc: func(ns string) ([]k8s.Event, error) {
				return []k8s.Event{
					{Type: "Warning", Reason: "BackOff", Message: "Back-off restarting", Object: "Pod/web-abc", LastSeen: time.Now()},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, _, err := executor.ExecuteTool(context.Background(), "kubectl_get_events",
		map[string]string{"namespace": "default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "BackOff") {
		t.Errorf("result should contain event reason, got: %s", result)
	}
}

func TestToolExecution_MissingRequiredParams(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	executor := newTestToolExecutor(mockK8s)

	tests := []struct {
		name string
		tool string
		args map[string]string
	}{
		{"deployments without ns", "kubectl_get_deployments", map[string]string{}},
		{"pods without ns", "kubectl_get_pods", map[string]string{}},
		{"logs without ns", "kubectl_logs", map[string]string{"podName": "abc"}},
		{"logs without pod", "kubectl_logs", map[string]string{"namespace": "default"}},
		{"logs_selector without ns", "kubectl_logs_selector", map[string]string{"labelSelector": "app=x"}},
		{"logs_selector without sel", "kubectl_logs_selector", map[string]string{"namespace": "default"}},
		{"describe without ns", "kubectl_describe", map[string]string{"kind": "pod", "name": "abc"}},
		{"events without ns", "kubectl_get_events", map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := executor.ExecuteTool(context.Background(), tt.tool, tt.args)
			if err == nil {
				t.Errorf("expected error for missing required params in %s", tt.tool)
			}
		})
	}
}

func TestLabelSelectorDiscovery(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			// Selector match returns nothing — triggers discovery
			ListPodsBySelectorFunc: func(ns string, sel map[string]string) ([]k8s.Pod, error) {
				if sel["app"] == "order-service" {
					// Return pods after discovery
					return []k8s.Pod{{Name: "order-svc-abc", Namespace: ns}}, nil
				}
				return nil, nil
			},
			// Exact deployment match succeeds
			FindDeploymentByNameFunc: func(ns, name string) (*k8s.Deployment, error) {
				if name == "order-service" {
					return &k8s.Deployment{
						Name:      "order-service",
						Namespace: ns,
						Selector:  map[string]string{"app": "order-service"},
					}, nil
				}
				return nil, fmt.Errorf("not found: %s", name)
			},
		},
		MockLogs: &k8s.MockLogStreamer{
			GetLogsFunc: func(_ context.Context, req k8s.LogRequest) ([]k8s.LogLine, error) {
				return []k8s.LogLine{
					{PodName: req.PodName, Content: "discovered log line"},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)

	// Use a selector that won't directly match pods, triggering discovery
	result, logs, err := executor.ExecuteTool(context.Background(), "kubectl_logs_selector",
		map[string]string{"namespace": "default", "labelSelector": "svc=order-service"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) == 0 {
		t.Error("expected logs from discovered deployment")
	}
	if !strings.Contains(result, "discovered log line") {
		t.Errorf("expected discovered log content, got: %s", result)
	}
}

func TestLabelSelectorDiscovery_FuzzyFallback(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockResources: &k8s.MockResourceLister{
			ListPodsBySelectorFunc: func(ns string, sel map[string]string) ([]k8s.Pod, error) {
				return nil, nil // no direct match
			},
			FindDeploymentByNameFunc: func(ns, name string) (*k8s.Deployment, error) {
				return nil, fmt.Errorf("not found") // no exact deployment match
			},
			SearchResourcesFunc: func(ns, query string) ([]k8s.SearchResult, error) {
				return []k8s.SearchResult{
					{Kind: "Deployment", Name: "order-service", Namespace: ns},
					{Kind: "Deployment", Name: "order-worker", Namespace: ns},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, _, err := executor.ExecuteTool(context.Background(), "kubectl_logs_selector",
		map[string]string{"namespace": "default", "labelSelector": "app=order-svc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Did you mean") {
		t.Errorf("expected fuzzy suggestions, got: %s", result)
	}
	if !strings.Contains(result, "order-service") || !strings.Contains(result, "order-worker") {
		t.Errorf("expected candidate names in result, got: %s", result)
	}
}

func TestCommandDisplay(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     map[string]string
		expected string
	}{
		{
			name:     "get namespaces",
			toolName: "kubectl_get_namespaces",
			args:     map[string]string{},
			expected: "kubectl get namespaces",
		},
		{
			name:     "get deployments",
			toolName: "kubectl_get_deployments",
			args:     map[string]string{"namespace": "prod"},
			expected: "kubectl get deployments -n prod",
		},
		{
			name:     "get pods",
			toolName: "kubectl_get_pods",
			args:     map[string]string{"namespace": "prod"},
			expected: "kubectl get pods -n prod",
		},
		{
			name:     "get pods with selector",
			toolName: "kubectl_get_pods",
			args:     map[string]string{"namespace": "prod", "labelSelector": "app=web"},
			expected: "kubectl get pods -n prod -l app=web",
		},
		{
			name:     "logs",
			toolName: "kubectl_logs",
			args:     map[string]string{"namespace": "prod", "podName": "web-abc"},
			expected: "kubectl logs web-abc -n prod",
		},
		{
			name:     "logs with options",
			toolName: "kubectl_logs",
			args:     map[string]string{"namespace": "prod", "podName": "web-abc", "container": "app", "since": "1h", "tailLines": "100"},
			expected: "kubectl logs web-abc -n prod -c app --since=1h --tail=100",
		},
		{
			name:     "logs with previous",
			toolName: "kubectl_logs",
			args:     map[string]string{"namespace": "prod", "podName": "web-abc", "previous": "true"},
			expected: "kubectl logs web-abc -n prod --previous",
		},
		{
			name:     "logs selector",
			toolName: "kubectl_logs_selector",
			args:     map[string]string{"namespace": "prod", "labelSelector": "app=order-service"},
			expected: "kubectl logs -l app=order-service -n prod --all-containers",
		},
		{
			name:     "describe",
			toolName: "kubectl_describe",
			args:     map[string]string{"namespace": "prod", "kind": "pod", "name": "web-abc"},
			expected: "kubectl describe pod web-abc -n prod",
		},
		{
			name:     "events",
			toolName: "kubectl_get_events",
			args:     map[string]string{"namespace": "prod"},
			expected: "kubectl get events -n prod",
		},
		{
			name:     "events with field selector",
			toolName: "kubectl_get_events",
			args:     map[string]string{"namespace": "prod", "fieldSelector": "involvedObject.name=web-abc"},
			expected: "kubectl get events -n prod --field-selector=involvedObject.name=web-abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateCommandDisplay(tt.toolName, tt.args)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestToolDefinitions_Count(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	executor := newTestToolExecutor(mockK8s)

	defs := executor.ToolDefinitions()
	if len(defs) != 21 {
		t.Errorf("expected 21 tool definitions, got %d", len(defs))
	}

	expectedNames := []string{
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
		// Cluster switching
		"list_kubeconfigs",
		"switch_kubeconfig",
	}

	for i, name := range expectedNames {
		if defs[i].Name != name {
			t.Errorf("tool %d: expected name %q, got %q", i, name, defs[i].Name)
		}
	}
}

func TestToolExecution_UnknownTool(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	executor := newTestToolExecutor(mockK8s)

	_, _, err := executor.ExecuteTool(context.Background(), "kubectl_delete_pod", map[string]string{})
	if err == nil {
		t.Error("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("error should mention unknown tool, got: %v", err)
	}
}

func TestToolExecution_LogsGrepPattern(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockLogs: &k8s.MockLogStreamer{
			GetLogsFunc: func(_ context.Context, req k8s.LogRequest) ([]k8s.LogLine, error) {
				return []k8s.LogLine{
					{PodName: "web-abc", Content: "INFO: started"},
					{PodName: "web-abc", Content: "ERROR: connection refused"},
					{PodName: "web-abc", Content: "INFO: retrying"},
					{PodName: "web-abc", Content: "ERROR: timeout"},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	result, logs, err := executor.ExecuteTool(context.Background(), "kubectl_logs",
		map[string]string{"namespace": "default", "podName": "web-abc", "grepPattern": "ERROR"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 filtered log lines, got %d", len(logs))
	}
	if !strings.Contains(result, "connection refused") {
		t.Error("result should contain 'connection refused'")
	}
	if strings.Contains(result, "started") {
		t.Error("result should not contain INFO lines")
	}
}

func TestToolExecution_LogsGrepPattern_InvalidRegex(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockLogs: &k8s.MockLogStreamer{
			GetLogsFunc: func(_ context.Context, _ k8s.LogRequest) ([]k8s.LogLine, error) {
				return []k8s.LogLine{{PodName: "web", Content: "test"}}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)
	_, _, err := executor.ExecuteTool(context.Background(), "kubectl_logs",
		map[string]string{"namespace": "default", "podName": "web", "grepPattern": "[invalid"})
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestToolExecution_EventsFieldSelector(t *testing.T) {
	mockK8s := &k8s.MockClient{
		MockEvents: &k8s.MockEventLister{
			ListEventsFunc: func(ns string) ([]k8s.Event, error) {
				return []k8s.Event{
					{Type: "Warning", Reason: "BackOff", Message: "Back-off restarting", Object: "Pod/web-abc", LastSeen: time.Now()},
					{Type: "Normal", Reason: "Scheduled", Message: "Successfully assigned", Object: "Pod/web-abc", LastSeen: time.Now()},
					{Type: "Warning", Reason: "Failed", Message: "Pull image failed", Object: "Pod/api-xyz", LastSeen: time.Now()},
				}, nil
			},
		},
	}

	executor := newTestToolExecutor(mockK8s)

	// Filter by involvedObject.name
	result, _, err := executor.ExecuteTool(context.Background(), "kubectl_get_events",
		map[string]string{"namespace": "default", "fieldSelector": "involvedObject.name=web-abc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "BackOff") {
		t.Error("result should contain BackOff event")
	}
	if strings.Contains(result, "api-xyz") {
		t.Error("result should not contain events for api-xyz")
	}

	// Filter by type
	result, _, err = executor.ExecuteTool(context.Background(), "kubectl_get_events",
		map[string]string{"namespace": "default", "fieldSelector": "type=Normal"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Scheduled") {
		t.Error("result should contain Normal/Scheduled event")
	}
	if strings.Contains(result, "BackOff") {
		t.Error("result should not contain Warning events")
	}
}

func TestToolExecution_NavigateResourceBrowser(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	executor := newTestToolExecutor(mockK8s)

	tests := []struct {
		name    string
		args    map[string]string
		wantErr bool
		wantStr string
	}{
		{
			name:    "namespace level",
			args:    map[string]string{"level": "namespace"},
			wantStr: "namespace list",
		},
		{
			name:    "deployment level with namespace",
			args:    map[string]string{"level": "deployment", "namespace": "production"},
			wantStr: "deployments in production",
		},
		{
			name:    "pod level with name highlight",
			args:    map[string]string{"level": "pod", "namespace": "production", "name": "web-abc"},
			wantStr: "highlighting web-abc",
		},
		{
			name:    "deployment without namespace",
			args:    map[string]string{"level": "deployment"},
			wantErr: true,
		},
		{
			name:    "invalid level",
			args:    map[string]string{"level": "container"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, logs, err := executor.ExecuteTool(context.Background(), "navigate_resource_browser", tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if logs != nil {
				t.Error("navigate should not return log lines")
			}
			if !strings.Contains(result, tt.wantStr) {
				t.Errorf("result should contain %q, got: %s", tt.wantStr, result)
			}
		})
	}
}

func TestCommandDisplay_NavigateResourceBrowser(t *testing.T) {
	tests := []struct {
		name string
		args map[string]string
		want string
	}{
		{
			name: "namespace level",
			args: map[string]string{"level": "namespace"},
			want: "[Navigate] → namespace",
		},
		{
			name: "deployment with namespace",
			args: map[string]string{"level": "deployment", "namespace": "prod"},
			want: "[Navigate] → prod/deployment",
		},
		{
			name: "pod with name",
			args: map[string]string{"level": "pod", "namespace": "prod", "name": "web-abc"},
			want: "[Navigate] → prod/pod/web-abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateCommandDisplay("navigate_resource_browser", tt.args)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// mockLogBuffer implements LogBufferReader for testing.
type mockLogBuffer struct {
	entries []logparse.LogEntry
}

func (m *mockLogBuffer) Slice() []logparse.LogEntry {
	return m.entries
}

func (m *mockLogBuffer) Len() int {
	return len(m.entries)
}

func TestToolExecution_GetLogViewerState_Empty(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	executor := newTestToolExecutor(mockK8s)

	result, logs, err := executor.ExecuteTool(context.Background(), "get_log_viewer_state", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logs != nil {
		t.Error("get_log_viewer_state should not return log lines")
	}
	if !strings.Contains(result, "empty") {
		t.Errorf("result should say empty when no buffer, got: %s", result)
	}
}

func TestToolExecution_GetLogViewerState_WithLogs(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	te := NewToolExecutor(mockK8s).(*toolExecutor)
	te.logBuffer = &mockLogBuffer{
		entries: []logparse.LogEntry{
			{PodName: "web-abc", Severity: logparse.SeverityError, Raw: "ERROR: connection refused"},
			{PodName: "web-abc", Severity: logparse.SeverityInfo, Raw: "INFO: started"},
			{PodName: "web-def", Severity: logparse.SeverityWarn, Raw: "WARN: high latency"},
			{PodName: "web-def", Severity: logparse.SeverityError, Raw: "ERROR: timeout"},
		},
	}

	result, _, err := te.ExecuteTool(context.Background(), "get_log_viewer_state", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "4 lines") {
		t.Errorf("result should contain line count, got: %s", result)
	}
	if !strings.Contains(result, "web-abc") || !strings.Contains(result, "web-def") {
		t.Errorf("result should contain pod names, got: %s", result)
	}
	if !strings.Contains(result, "ERROR: 2") {
		t.Errorf("result should contain ERROR count, got: %s", result)
	}
	if !strings.Contains(result, "WARN: 1") {
		t.Errorf("result should contain WARN count, got: %s", result)
	}
}

func TestToolExecution_SearchVisibleLogs(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	te := NewToolExecutor(mockK8s).(*toolExecutor)
	te.logBuffer = &mockLogBuffer{
		entries: []logparse.LogEntry{
			{PodName: "web-abc", Raw: "INFO: started server on port 8080"},
			{PodName: "web-abc", Raw: "ERROR: connection refused to database"},
			{PodName: "web-abc", Raw: "INFO: retrying connection"},
			{PodName: "web-abc", Raw: "ERROR: timeout after 30s"},
		},
	}

	result, logs, err := te.ExecuteTool(context.Background(), "search_visible_logs",
		map[string]string{"pattern": "ERROR"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logs != nil {
		t.Error("search_visible_logs should not return log lines")
	}
	if !strings.Contains(result, "2 matches") {
		t.Errorf("result should contain match count, got: %s", result)
	}
	if !strings.Contains(result, "connection refused") {
		t.Errorf("result should contain matched line, got: %s", result)
	}
	if strings.Contains(result, "started server") {
		t.Error("result should not contain non-matching lines")
	}
}

func TestToolExecution_SearchVisibleLogs_NoMatch(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	te := NewToolExecutor(mockK8s).(*toolExecutor)
	te.logBuffer = &mockLogBuffer{
		entries: []logparse.LogEntry{
			{PodName: "web-abc", Raw: "INFO: all good"},
		},
	}

	result, _, err := te.ExecuteTool(context.Background(), "search_visible_logs",
		map[string]string{"pattern": "FATAL"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "No matches") {
		t.Errorf("result should say no matches, got: %s", result)
	}
}

func TestToolExecution_SearchVisibleLogs_EmptyBuffer(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	executor := newTestToolExecutor(mockK8s)

	result, _, err := executor.ExecuteTool(context.Background(), "search_visible_logs",
		map[string]string{"pattern": "ERROR"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "empty") {
		t.Errorf("result should say empty, got: %s", result)
	}
}

func TestToolExecution_SearchVisibleLogs_InvalidRegex(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	te := NewToolExecutor(mockK8s).(*toolExecutor)
	te.logBuffer = &mockLogBuffer{
		entries: []logparse.LogEntry{{Raw: "test"}},
	}

	_, _, err := te.ExecuteTool(context.Background(), "search_visible_logs",
		map[string]string{"pattern": "[invalid"})
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestToolExecution_SearchVisibleLogs_MaxResults(t *testing.T) {
	mockK8s := &k8s.MockClient{}
	te := NewToolExecutor(mockK8s).(*toolExecutor)

	entries := make([]logparse.LogEntry, 100)
	for i := range entries {
		entries[i] = logparse.LogEntry{PodName: "web", Raw: fmt.Sprintf("ERROR: line %d", i)}
	}
	te.logBuffer = &mockLogBuffer{entries: entries}

	result, _, err := te.ExecuteTool(context.Background(), "search_visible_logs",
		map[string]string{"pattern": "ERROR", "maxResults": "5"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "100 matches") {
		t.Errorf("result should report total matches, got: %s", result)
	}
	if !strings.Contains(result, "showing first 5") {
		t.Errorf("result should indicate truncation, got: %s", result)
	}
}

func TestCommandDisplay_LogViewerTools(t *testing.T) {
	got := GenerateCommandDisplay("get_log_viewer_state", nil)
	if got != "[TUI] Log Viewer status" {
		t.Errorf("got %q", got)
	}

	got = GenerateCommandDisplay("search_visible_logs", map[string]string{"pattern": "ERROR|WARN"})
	if !strings.Contains(got, "grep -E") || !strings.Contains(got, "ERROR|WARN") {
		t.Errorf("got %q", got)
	}
}

func newTestKubeconfig(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "config")
	content := `apiVersion: v1
kind: Config
contexts:
- context:
    cluster: prod-cluster
    user: admin
  name: prod
- context:
    cluster: staging-cluster
    user: dev
  name: staging
current-context: prod
clusters:
- cluster:
    server: https://prod:6443
  name: prod-cluster
users:
- name: admin
  user: {}
`
	if err := os.WriteFile(kubeconfigPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write test kubeconfig: %v", err)
	}
	return kubeconfigPath
}

func TestToolExecution_SwitchKubeconfig(t *testing.T) {
	kubeconfigPath := newTestKubeconfig(t)

	mockK8s := &k8s.MockClient{
		ContextInfoFunc: func() (string, string) {
			return kubeconfigPath, "prod"
		},
	}

	te := NewToolExecutor(mockK8s).(*toolExecutor)

	result, logs, err := te.ExecuteTool(context.Background(), "switch_kubeconfig",
		map[string]string{"context": "staging"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logs != nil {
		t.Error("switch_kubeconfig should not return log lines")
	}
	if !strings.Contains(result, "Switching to context") {
		t.Errorf("expected success message, got: %s", result)
	}
	if !strings.Contains(result, "staging") {
		t.Errorf("result should mention target context, got: %s", result)
	}
}

func TestToolExecution_SwitchKubeconfig_InvalidContext(t *testing.T) {
	kubeconfigPath := newTestKubeconfig(t)

	mockK8s := &k8s.MockClient{
		ContextInfoFunc: func() (string, string) {
			return kubeconfigPath, "prod"
		},
	}

	te := NewToolExecutor(mockK8s).(*toolExecutor)

	result, _, err := te.ExecuteTool(context.Background(), "switch_kubeconfig",
		map[string]string{"context": "nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "ERROR") {
		t.Errorf("expected error result for invalid context, got: %s", result)
	}
	if !strings.Contains(result, "not found") {
		t.Errorf("expected 'not found' in result, got: %s", result)
	}
	if !strings.Contains(result, "prod") || !strings.Contains(result, "staging") {
		t.Errorf("expected available contexts listed, got: %s", result)
	}
}

func TestToolExecution_SwitchKubeconfig_MissingContext(t *testing.T) {
	mockK8s := &k8s.MockClient{}

	te := NewToolExecutor(mockK8s).(*toolExecutor)

	result, _, err := te.ExecuteTool(context.Background(), "switch_kubeconfig",
		map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "ERROR: context parameter is required") {
		t.Errorf("expected error about missing context param, got: %s", result)
	}
}

func TestCommandDisplay_SwitchKubeconfig(t *testing.T) {
	tests := []struct {
		name string
		args map[string]string
		want string
	}{
		{
			name: "context only",
			args: map[string]string{"context": "staging"},
			want: "kubectl config use-context staging",
		},
		{
			name: "with kubeconfig",
			args: map[string]string{"context": "staging", "kubeconfig": "/home/user/.kube/config"},
			want: "kubectl config use-context staging --kubeconfig=/home/user/.kube/config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateCommandDisplay("switch_kubeconfig", tt.args)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
