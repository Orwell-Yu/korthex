package ui

import (
	"github.com/Orwell-Yu/korthex/internal/agent"
	"github.com/Orwell-Yu/korthex/internal/k8s"
)

// PanelID identifies a UI panel.
type PanelID int

const (
	PanelResource PanelID = iota
	PanelLogViewer
	PanelChat
)

// LayoutMode determines the panel layout arrangement.
type LayoutMode int

const (
	LayoutFull      LayoutMode = iota // Three panels: resource | logs / chat
	LayoutChatFocus                   // Chat dominant, logs collapsed
	LayoutLogFocus                    // Full screen logs
)

// AgentEventMsg wraps an agent event for Bubble Tea's message system.
type AgentEventMsg agent.AgentEvent

// NavigateToLogsMsg requests navigation to a pod's log stream.
type NavigateToLogsMsg struct {
	Namespace string
	PodName   string
	Container string
}

// LogsLoadedMsg signals that log lines have been loaded.
type LogsLoadedMsg struct {
	LogLines []k8s.LogLine
}

// LogLineMsg carries a single log line for manual streaming.
type LogLineMsg struct {
	Line k8s.LogLine
}

// InformerUpdateMsg signals an informer cache update.
type InformerUpdateMsg struct {
	Namespace    string
	ResourceType string
}

// ClusterConnectedMsg signals successful cluster connection.
type ClusterConnectedMsg struct {
	Namespaces []string
}

// NavigateToResourceMsg requests the Resource Browser to navigate to a specific level.
// Emitted when the agent calls navigate_resource_browser, so the left panel syncs automatically.
type NavigateToResourceMsg struct {
	Level     int    // levelNamespace=0, levelDeployment=1, levelPod=2
	Namespace string // target namespace (used when Level >= 1)
	Name      string // optional: resource name to highlight (deployment or pod name)
}

// ErrorMsg carries an error through Bubble Tea's message system.
type ErrorMsg struct {
	Err error
}

// --- Phase 2 messages ---

// MetricsUpdateMsg carries updated token metrics for the Chat panel header.
type MetricsUpdateMsg struct {
	Metrics agent.AgentMetrics
}

// BookmarkMsg signals the AI wants to bookmark specific log lines.
type BookmarkMsg struct {
	LineIndices []int
	Reason      string
}

// --- Phase 3 messages ---

// DataResultMsg carries a parsed query_database result for the Data Viewer panel.
// NOTE: Emitted by app.go when query_database tool returns. Consumed by Data Viewer (S5).
type DataResultMsg struct {
	TableName string // table name for tab label
	JoinPath  string // relation breadcrumb, e.g. "orders -> order_items via order_id"
	IsPrimary bool   // first query = pinned tab, not FIFO-evicted
	Columns   []string
	Rows      [][]string
	RowCount  int
	Truncated bool
	Database  string
	Namespace string
	PodName   string
}

// resourceItemsLoadedMsg carries resource items loaded via the registry pattern.
type resourceItemsLoadedMsg struct {
	Kind  k8s.ResourceKind
	Items []k8s.ResourceItem
}

// showPodDetailMsg requests the pod detail overlay for a specific pod.
type showPodDetailMsg struct {
	Namespace string
	PodName   string
}

// showHistorySearchMsg signals that the history search overlay should open.
type showHistorySearchMsg struct{}

// showHelpMsg signals that the help overlay should toggle.
type showHelpMsg struct{}

// historySessionLoadedMsg carries the full message list of a restored session.
type historySessionLoadedMsg struct {
	Messages []ChatMessage
}

// showKubeSwitchMsg triggers the KubeSwitch overlay.
type showKubeSwitchMsg struct{}

// kubeSwitchExecuteMsg is sent when user confirms kubeconfig+context selection
// or when the agent triggers a switch via the switch_kubeconfig tool.
type kubeSwitchExecuteMsg struct {
	Kubeconfig string
	Context    string
	FromAgent  bool // true when triggered by agent (don't cancel the running agent)
}

// kubeSwitchCompleteMsg carries the result of a kubeconfig switch attempt.
type kubeSwitchCompleteMsg struct {
	Kubeconfig  string // kubeconfig path on success
	ContextName string // new context name on success
	Err         error  // non-nil on failure
}
