package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Orwell-Yu/korthex/internal/config"
	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/Orwell-Yu/korthex/internal/llm"
	"github.com/Orwell-Yu/korthex/pkg/logparse"
)

// toolExecutor implements ToolExecutor, dispatching tool calls to k8s.Client methods.
type toolExecutor struct {
	k8sClient k8s.Client
	logBuffer LogBufferReader
	parser    logparse.Parser
}

// NewToolExecutor creates a ToolExecutor backed by the given k8s.Client.
func NewToolExecutor(k8sClient k8s.Client) ToolExecutor {
	return &toolExecutor{
		k8sClient: k8sClient,
		parser:    logparse.NewParser(),
	}
}

// ToolDefinitions returns the Phase 1+2 read-only tool definitions.
func (t *toolExecutor) ToolDefinitions() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		{
			Name:        "kubectl_get_namespaces",
			Description: "List all namespaces in the cluster with their status. Use the filter parameter in large clusters to narrow results.",
			Parameters: []llm.ParameterDef{
				{Name: "filter", Type: "string", Description: "Substring filter on namespace name (case-insensitive). Use this when the cluster has many namespaces.", Required: false},
			},
		},
		{
			Name:        "kubectl_get_deployments",
			Description: "List deployments in a namespace with replica counts and status.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "filter", Type: "string", Description: "Substring filter on deployment name (case-insensitive)", Required: false},
			},
		},
		{
			Name:        "kubectl_get_pods",
			Description: "List pods in a namespace, optionally filtered by label selector.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "labelSelector", Type: "string", Description: "Label selector (e.g., app=order-service)", Required: false},
				{Name: "filter", Type: "string", Description: "Substring filter on pod name (case-insensitive)", Required: false},
			},
		},
		{
			Name:        "kubectl_logs",
			Description: "Get logs from a specific pod. Use for targeted single-pod log retrieval.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "podName", Type: "string", Description: "Name of the pod", Required: true},
				{Name: "container", Type: "string", Description: "Container name (empty = all containers)", Required: false},
				{Name: "since", Type: "string", Description: "Duration (e.g., 1h, 30m, 2h30m)", Required: false},
				{Name: "sinceTime", Type: "string", Description: "RFC3339 timestamp (e.g., 2024-01-01T00:00:00Z)", Required: false},
				{Name: "tailLines", Type: "string", Description: "Number of lines from the end", Required: false},
				{Name: "previous", Type: "string", Description: "Get logs from previous container instance (true/false)", Required: false},
				{Name: "grepPattern", Type: "string", Description: "Regex pattern to filter log lines", Required: false},
			},
		},
		{
			Name:        "kubectl_logs_selector",
			Description: "Get logs from all pods matching a label selector. Prefer this over kubectl_logs when querying a service.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "labelSelector", Type: "string", Description: "Label selector (e.g., app=order-service)", Required: true},
				{Name: "since", Type: "string", Description: "Duration (e.g., 1h, 30m)", Required: false},
				{Name: "sinceTime", Type: "string", Description: "RFC3339 timestamp", Required: false},
				{Name: "tailLines", Type: "string", Description: "Number of lines from the end per pod", Required: false},
				{Name: "previous", Type: "string", Description: "Get logs from previous container instance (true/false)", Required: false},
				{Name: "grepPattern", Type: "string", Description: "Regex pattern to filter log lines", Required: false},
			},
		},
		{
			Name:        "kubectl_describe",
			Description: "Get detailed description of a Kubernetes resource including events.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "kind", Type: "string", Description: "Resource kind (pod, deployment, statefulset, daemonset, job, cronjob)", Required: true},
				{Name: "name", Type: "string", Description: "Resource name", Required: true},
			},
		},
		{
			Name:        "kubectl_get_events",
			Description: "List events in a namespace, useful for diagnosing issues.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "fieldSelector", Type: "string", Description: "Field selector to filter events (e.g., involvedObject.name=my-pod)", Required: false},
			},
		},
		{
			Name:        "navigate_resource_browser",
			Description: "Navigate the TUI Resource Browser panel to a specific resource level. Call this whenever your analysis involves specific namespaces, deployments, or pods so the user can interact with them in the left panel. Navigate to the most specific level you can confidently determine.",
			Parameters: []llm.ParameterDef{
				{Name: "level", Type: "string", Description: "Target level: namespace, deployment, or pod", Required: true, Enum: []string{"namespace", "deployment", "pod"}},
				{Name: "namespace", Type: "string", Description: "Target namespace (required for deployment/pod level)", Required: false},
				{Name: "name", Type: "string", Description: "Resource name to highlight (namespace, deployment, or pod name)", Required: false},
			},
		},
		{
			Name:        "get_log_viewer_state",
			Description: "Get the current state of the TUI Log Viewer buffer: line count, severity distribution, and source pods. Call this BEFORE kubectl_logs or kubectl_logs_selector to check if relevant logs are already loaded.",
			Parameters:  []llm.ParameterDef{},
		},
		{
			Name:        "search_visible_logs",
			Description: "Search within logs already loaded in the TUI Log Viewer buffer using a regex pattern. Much faster than re-fetching via kubectl. Use this when the Log Viewer already has relevant logs.",
			Parameters: []llm.ParameterDef{
				{Name: "pattern", Type: "string", Description: "Regex pattern to search for in log lines", Required: true},
				{Name: "maxResults", Type: "string", Description: "Maximum number of matching lines to return (default: 50)", Required: false},
			},
		},
		// Phase 2: Resource type tools
		{
			Name:        "kubectl_get_statefulsets",
			Description: "List StatefulSets in a namespace with replica counts. Use for databases, caches, and other stateful workloads.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "filter", Type: "string", Description: "Substring filter on StatefulSet name (case-insensitive)", Required: false},
			},
		},
		{
			Name:        "kubectl_get_daemonsets",
			Description: "List DaemonSets in a namespace with scheduling counts. Use for node-level agents like logging and monitoring.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "filter", Type: "string", Description: "Substring filter on DaemonSet name (case-insensitive)", Required: false},
			},
		},
		{
			Name:        "kubectl_get_jobs",
			Description: "List Jobs in a namespace with completion status. Use for batch processing workloads.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "filter", Type: "string", Description: "Substring filter on Job name (case-insensitive)", Required: false},
			},
		},
		{
			Name:        "kubectl_get_cronjobs",
			Description: "List CronJobs in a namespace with schedule and suspension status.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "filter", Type: "string", Description: "Substring filter on CronJob name (case-insensitive)", Required: false},
			},
		},
		// Phase 2: Analysis tools
		{
			Name:        "severity_stats",
			Description: "Compute severity distribution statistics from the Log Viewer buffer. Shows counts of FATAL, ERROR, WARN, INFO, DEBUG per pod.",
			Parameters:  []llm.ParameterDef{},
		},
		{
			Name:        "compare_logs",
			Description: "Compare log severity patterns between two time ranges to identify trends. Useful for before/after analysis.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "labelSelector", Type: "string", Description: "Label selector to identify the service", Required: true},
				{Name: "range1Since", Type: "string", Description: "Start of first range as duration (e.g., 2h)", Required: true},
				{Name: "range1Until", Type: "string", Description: "End of first range as duration (e.g., 1h)", Required: true},
				{Name: "range2Since", Type: "string", Description: "Start of second range as duration (e.g., 1h)", Required: true},
			},
		},
		{
			Name:        "get_pod_metrics",
			Description: "Get CPU and memory usage metrics for a pod (requires Metrics Server). Returns current resource consumption.",
			Parameters: []llm.ParameterDef{
				{Name: "namespace", Type: "string", Description: "Kubernetes namespace", Required: true},
				{Name: "podName", Type: "string", Description: "Pod name", Required: true},
			},
		},
		{
			Name:        "trace_logs",
			Description: "Search for a trace/request/correlation ID across logs in the Log Viewer buffer. Useful for tracing a request across microservices.",
			Parameters: []llm.ParameterDef{
				{Name: "traceID", Type: "string", Description: "The trace/request/correlation ID to search for", Required: true},
			},
		},
		// Phase 2: UI tool
		{
			Name:        "bookmark_log_lines",
			Description: "Bookmark specific log lines in the Log Viewer for later reference. The user can navigate between bookmarks with n/N keys.",
			Parameters: []llm.ParameterDef{
				{Name: "lineIndices", Type: "string", Description: "Comma-separated line indices to bookmark (0-based)", Required: true},
				{Name: "reason", Type: "string", Description: "Reason for bookmarking (shown in bookmark list)", Required: false},
			},
		},
		// Cluster switching
		{
			Name:        "list_kubeconfigs",
			Description: "List all available kubeconfig files under ~/.kube/ and their contexts. Use BEFORE switch_kubeconfig to show the user what clusters are available.",
			Parameters:  []llm.ParameterDef{},
		},
		{
			Name:        "switch_kubeconfig",
			Description: "Switch to a different Kubernetes cluster by changing kubeconfig file and/or context. Use when user asks to switch cluster, change context, or connect to a different environment (e.g. 'switch to staging', 'connect to prod cluster'). Returns available contexts for confirmation.",
			Parameters: []llm.ParameterDef{
				{Name: "kubeconfig", Type: "string", Description: "Path to kubeconfig file. Leave empty to keep current file.", Required: false},
				{Name: "context", Type: "string", Description: "Context name to switch to.", Required: true},
			},
		},
	}
}

// ExecuteTool dispatches a tool call to the appropriate handler.
func (t *toolExecutor) ExecuteTool(ctx context.Context, name string, args map[string]string) (string, []k8s.LogLine, error) {
	switch name {
	case "kubectl_get_namespaces":
		return t.getNamespaces(args)
	case "kubectl_get_deployments":
		return t.getDeployments(args)
	case "kubectl_get_pods":
		return t.getPods(args)
	case "kubectl_logs":
		return t.getLogs(ctx, args)
	case "kubectl_logs_selector":
		return t.getLogsBySelector(ctx, args)
	case "kubectl_describe":
		return t.describe(args)
	case "kubectl_get_events":
		return t.getEvents(args)
	case "navigate_resource_browser":
		return t.navigateResourceBrowser(args)
	case "get_log_viewer_state":
		return t.getLogViewerState()
	case "search_visible_logs":
		return t.searchVisibleLogs(args)
	// Phase 2: Resource type tools
	case "kubectl_get_statefulsets":
		return t.getStatefulSets(args)
	case "kubectl_get_daemonsets":
		return t.getDaemonSets(args)
	case "kubectl_get_jobs":
		return t.getJobs(args)
	case "kubectl_get_cronjobs":
		return t.getCronJobs(args)
	// Phase 2: Analysis tools
	case "severity_stats":
		return t.severityStats()
	case "compare_logs":
		return t.compareLogs(ctx, args)
	case "get_pod_metrics":
		return t.getPodMetrics(args)
	case "trace_logs":
		return t.traceLogs(args)
	// Phase 2: UI tool
	case "bookmark_log_lines":
		return t.bookmarkLogLines(args)
	// Cluster switching
	case "list_kubeconfigs":
		return t.listKubeconfigs()
	case "switch_kubeconfig":
		return t.switchKubeconfig(args)
	default:
		return "", nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// GenerateCommandDisplay produces a human-readable kubectl equivalent string.
func GenerateCommandDisplay(name string, args map[string]string) string {
	switch name {
	case "kubectl_get_namespaces":
		cmd := "kubectl get namespaces"
		if f := args["filter"]; f != "" {
			cmd += fmt.Sprintf(" | grep -i %s", f)
		}
		return cmd
	case "kubectl_get_deployments":
		cmd := fmt.Sprintf("kubectl get deployments -n %s", args["namespace"])
		if f := args["filter"]; f != "" {
			cmd += fmt.Sprintf(" | grep -i %s", f)
		}
		return cmd
	case "kubectl_get_pods":
		cmd := fmt.Sprintf("kubectl get pods -n %s", args["namespace"])
		if sel := args["labelSelector"]; sel != "" {
			cmd += fmt.Sprintf(" -l %s", sel)
		}
		if f := args["filter"]; f != "" {
			cmd += fmt.Sprintf(" | grep -i %s", f)
		}
		return cmd
	case "kubectl_logs":
		cmd := fmt.Sprintf("kubectl logs %s -n %s", args["podName"], args["namespace"])
		if c := args["container"]; c != "" {
			cmd += fmt.Sprintf(" -c %s", c)
		}
		if s := args["since"]; s != "" {
			cmd += fmt.Sprintf(" --since=%s", s)
		}
		if tl := args["tailLines"]; tl != "" {
			cmd += fmt.Sprintf(" --tail=%s", tl)
		}
		if args["previous"] == "true" {
			cmd += " --previous"
		}
		return cmd
	case "kubectl_logs_selector":
		cmd := fmt.Sprintf("kubectl logs -l %s -n %s --all-containers", args["labelSelector"], args["namespace"])
		if s := args["since"]; s != "" {
			cmd += fmt.Sprintf(" --since=%s", s)
		}
		if tl := args["tailLines"]; tl != "" {
			cmd += fmt.Sprintf(" --tail=%s", tl)
		}
		if args["previous"] == "true" {
			cmd += " --previous"
		}
		return cmd
	case "kubectl_describe":
		return fmt.Sprintf("kubectl describe %s %s -n %s", args["kind"], args["name"], args["namespace"])
	case "kubectl_get_events":
		cmd := fmt.Sprintf("kubectl get events -n %s", args["namespace"])
		if fs := args["fieldSelector"]; fs != "" {
			cmd += fmt.Sprintf(" --field-selector=%s", fs)
		}
		return cmd
	case "navigate_resource_browser":
		target := args["level"]
		if ns := args["namespace"]; ns != "" {
			target = ns + "/" + target
		}
		if name := args["name"]; name != "" {
			target += "/" + name
		}
		return fmt.Sprintf("[Navigate] → %s", target)
	case "get_log_viewer_state":
		return "[TUI] Log Viewer status"
	case "search_visible_logs":
		return fmt.Sprintf("[TUI] grep -E %q (visible logs)", args["pattern"])
	// Phase 2
	case "kubectl_get_statefulsets":
		cmd := fmt.Sprintf("kubectl get statefulsets -n %s", args["namespace"])
		if f := args["filter"]; f != "" {
			cmd += fmt.Sprintf(" | grep -i %s", f)
		}
		return cmd
	case "kubectl_get_daemonsets":
		cmd := fmt.Sprintf("kubectl get daemonsets -n %s", args["namespace"])
		if f := args["filter"]; f != "" {
			cmd += fmt.Sprintf(" | grep -i %s", f)
		}
		return cmd
	case "kubectl_get_jobs":
		cmd := fmt.Sprintf("kubectl get jobs -n %s", args["namespace"])
		if f := args["filter"]; f != "" {
			cmd += fmt.Sprintf(" | grep -i %s", f)
		}
		return cmd
	case "kubectl_get_cronjobs":
		cmd := fmt.Sprintf("kubectl get cronjobs -n %s", args["namespace"])
		if f := args["filter"]; f != "" {
			cmd += fmt.Sprintf(" | grep -i %s", f)
		}
		return cmd
	case "severity_stats":
		return "[Analysis] severity distribution"
	case "compare_logs":
		return fmt.Sprintf("[Analysis] compare logs -l %s -n %s (range1: %s→%s, range2: %s→now)",
			args["labelSelector"], args["namespace"], args["range1Since"], args["range1Until"], args["range2Since"])
	case "get_pod_metrics":
		return fmt.Sprintf("kubectl top pod %s -n %s", args["podName"], args["namespace"])
	case "trace_logs":
		return fmt.Sprintf("[Analysis] trace %q (visible logs)", args["traceID"])
	case "bookmark_log_lines":
		return fmt.Sprintf("[TUI] bookmark lines %s", args["lineIndices"])
	case "list_kubeconfigs":
		return "ls ~/.kube/ && kubectl config get-contexts"
	case "switch_kubeconfig":
		cmd := "kubectl config use-context " + args["context"]
		if kc := args["kubeconfig"]; kc != "" {
			cmd += " --kubeconfig=" + kc
		}
		return cmd
	default:
		return name
	}
}

// --- Tool handlers ---

func (t *toolExecutor) getNamespaces(args map[string]string) (string, []k8s.LogLine, error) {
	namespaces, err := t.k8sClient.Resources().ListNamespaces()
	if err != nil {
		return "", nil, fmt.Errorf("list namespaces: %w", err)
	}

	filter := strings.ToLower(args["filter"])

	var b strings.Builder
	b.WriteString("NAME\tSTATUS\n")
	count := 0
	for _, ns := range namespaces {
		if filter != "" && !strings.Contains(strings.ToLower(ns.Name), filter) {
			continue
		}
		fmt.Fprintf(&b, "%s\t%s\n", ns.Name, ns.Status)
		count++
	}
	if filter != "" {
		fmt.Fprintf(&b, "\n(%d of %d namespaces match filter %q)", count, len(namespaces), args["filter"])
	}
	return b.String(), nil, nil
}

func (t *toolExecutor) getDeployments(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	if ns == "" {
		return "", nil, fmt.Errorf("namespace is required")
	}

	deployments, err := t.k8sClient.Resources().ListDeployments(ns)
	if err != nil {
		return "", nil, fmt.Errorf("list deployments: %w", err)
	}

	filter := strings.ToLower(args["filter"])

	var b strings.Builder
	b.WriteString("NAME\tREADY\tAVAILABLE\tSELECTOR\n")
	count := 0
	for _, d := range deployments {
		if filter != "" && !strings.Contains(strings.ToLower(d.Name), filter) {
			continue
		}
		sel := formatSelector(d.Selector)
		fmt.Fprintf(&b, "%s\t%d/%d\t%d\t%s\n", d.Name, d.Ready, d.Replicas, d.Available, sel)
		count++
	}
	if filter != "" {
		fmt.Fprintf(&b, "\n(%d of %d deployments match filter %q)", count, len(deployments), args["filter"])
	}
	return b.String(), nil, nil
}

func (t *toolExecutor) getPods(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	if ns == "" {
		return "", nil, fmt.Errorf("namespace is required")
	}

	var pods []k8s.Pod
	var err error

	if sel := args["labelSelector"]; sel != "" {
		selectorMap := parseLabelSelector(sel)
		pods, err = t.k8sClient.Resources().ListPodsBySelector(ns, selectorMap)
	} else {
		pods, err = t.k8sClient.Resources().ListPods(ns)
	}
	if err != nil {
		return "", nil, fmt.Errorf("list pods: %w", err)
	}

	filter := strings.ToLower(args["filter"])

	var b strings.Builder
	b.WriteString("NAME\tSTATUS\tRESTARTS\tAGE\n")
	count := 0
	for _, p := range pods {
		if filter != "" && !strings.Contains(strings.ToLower(p.Name), filter) {
			continue
		}
		fmt.Fprintf(&b, "%s\t%s\t%d\t%s\n", p.Name, p.Status, p.Restarts, formatAge(p.Age))
		count++
	}
	if filter != "" {
		fmt.Fprintf(&b, "\n(%d of %d pods match filter %q)", count, len(pods), args["filter"])
	}
	return b.String(), nil, nil
}

func (t *toolExecutor) getLogs(ctx context.Context, args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	podName := args["podName"]
	if ns == "" || podName == "" {
		return "", nil, fmt.Errorf("namespace and podName are required")
	}

	req := k8s.LogRequest{
		Namespace: ns,
		PodName:   podName,
		Container: args["container"],
		Follow:    false,
	}
	applyLogOptions(&req, args)

	lines, err := t.k8sClient.Logs().GetLogs(ctx, req)
	if err != nil {
		return "", nil, fmt.Errorf("get logs: %w", err)
	}

	lines, err = filterLogLines(lines, args["grepPattern"])
	if err != nil {
		return "", nil, fmt.Errorf("grep filter: %w", err)
	}

	result := formatLogLines(lines)
	return result, lines, nil
}

func (t *toolExecutor) getLogsBySelector(ctx context.Context, args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	labelSel := args["labelSelector"]
	if ns == "" || labelSel == "" {
		return "", nil, fmt.Errorf("namespace and labelSelector are required")
	}

	selectorMap := parseLabelSelector(labelSel)

	// Find pods matching the selector
	pods, err := t.k8sClient.Resources().ListPodsBySelector(ns, selectorMap)
	if err != nil {
		return "", nil, fmt.Errorf("list pods by selector: %w", err)
	}
	if len(pods) == 0 {
		// Attempt label selector discovery via deployment
		return t.discoverAndGetLogs(ctx, ns, labelSel, args)
	}

	var allLines []k8s.LogLine
	for _, pod := range pods {
		req := k8s.LogRequest{
			Namespace: ns,
			PodName:   pod.Name,
			Container: args["container"],
			Follow:    false,
		}
		applyLogOptions(&req, args)

		lines, err := t.k8sClient.Logs().GetLogs(ctx, req)
		if err != nil {
			// Log error but continue with other pods
			allLines = append(allLines, k8s.LogLine{
				PodName: pod.Name,
				Content: fmt.Sprintf("[ERROR] Failed to get logs: %v", err),
			})
			continue
		}
		allLines = append(allLines, lines...)
	}

	allLines, err = filterLogLines(allLines, args["grepPattern"])
	if err != nil {
		return "", nil, fmt.Errorf("grep filter: %w", err)
	}

	result := formatLogLines(allLines)
	return result, allLines, nil
}

func (t *toolExecutor) discoverAndGetLogs(ctx context.Context, ns, labelSel string, args map[string]string) (string, []k8s.LogLine, error) {
	// Extract service name from selector (e.g., "app=order-service" → "order-service")
	parts := strings.SplitN(labelSel, "=", 2)
	serviceName := labelSel
	if len(parts) == 2 {
		serviceName = parts[1]
	}

	// Try exact deployment match
	dep, err := t.k8sClient.Resources().FindDeploymentByName(ns, serviceName)
	if err == nil && dep != nil {
		// Found deployment, use its selector
		pods, err := t.k8sClient.Resources().ListPodsBySelector(ns, dep.Selector)
		if err != nil {
			return "", nil, fmt.Errorf("list pods by deployment selector: %w", err)
		}

		var allLines []k8s.LogLine
		for _, pod := range pods {
			req := k8s.LogRequest{
				Namespace: ns,
				PodName:   pod.Name,
				Follow:    false,
			}
			applyLogOptions(&req, args)

			lines, err := t.k8sClient.Logs().GetLogs(ctx, req)
			if err != nil {
				continue
			}
			allLines = append(allLines, lines...)
		}

		allLines, err = filterLogLines(allLines, args["grepPattern"])
		if err != nil {
			return "", nil, fmt.Errorf("grep filter: %w", err)
		}

		result := formatLogLines(allLines)
		return result, allLines, nil
	}

	// Fuzzy search fallback
	results, err := t.k8sClient.Resources().SearchResources(ns, serviceName)
	if err != nil {
		return "", nil, fmt.Errorf("search resources: %w", err)
	}
	if len(results) == 0 {
		return "No matching resources found for: " + serviceName, nil, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "No exact match for '%s'. Did you mean one of these?\n", serviceName)
	for _, r := range results {
		fmt.Fprintf(&b, "- %s/%s (namespace: %s)\n", r.Kind, r.Name, r.Namespace)
	}
	return b.String(), nil, nil
}

func (t *toolExecutor) describe(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	kind := args["kind"]
	name := args["name"]
	if ns == "" || kind == "" || name == "" {
		return "", nil, fmt.Errorf("namespace, kind, and name are required")
	}

	result, err := t.k8sClient.Describer().Describe(ns, kind, name)
	if err != nil {
		return "", nil, fmt.Errorf("describe %s/%s: %w", kind, name, err)
	}
	return result, nil, nil
}

func (t *toolExecutor) getEvents(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	if ns == "" {
		return "", nil, fmt.Errorf("namespace is required")
	}

	events, err := t.k8sClient.Events().ListEvents(ns)
	if err != nil {
		return "", nil, fmt.Errorf("list events: %w", err)
	}

	// Client-side field selector filtering
	if fs := args["fieldSelector"]; fs != "" {
		events = filterEvents(events, fs)
	}

	var b strings.Builder
	b.WriteString("LAST SEEN\tTYPE\tREASON\tOBJECT\tMESSAGE\n")
	for _, e := range events {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\n",
			formatTimeAgo(e.LastSeen), e.Type, e.Reason, e.Object, e.Message)
	}
	return b.String(), nil, nil
}

func (t *toolExecutor) navigateResourceBrowser(args map[string]string) (string, []k8s.LogLine, error) {
	level := args["level"]
	ns := args["namespace"]
	name := args["name"]

	switch level {
	case "namespace":
		return "Navigated Resource Browser to namespace list", nil, nil
	case "deployment":
		if ns == "" {
			return "", nil, fmt.Errorf("namespace is required for deployment level")
		}
		msg := fmt.Sprintf("Navigated Resource Browser to deployments in %s", ns)
		if name != "" {
			msg += fmt.Sprintf(", highlighting %s", name)
		}
		return msg, nil, nil
	case "pod":
		if ns == "" {
			return "", nil, fmt.Errorf("namespace is required for pod level")
		}
		msg := fmt.Sprintf("Navigated Resource Browser to pods in %s", ns)
		if name != "" {
			msg += fmt.Sprintf(", highlighting %s", name)
		}
		return msg, nil, nil
	default:
		return "", nil, fmt.Errorf("invalid level %q: must be namespace, deployment, or pod", level)
	}
}

func (t *toolExecutor) getLogViewerState() (string, []k8s.LogLine, error) {
	if t.logBuffer == nil {
		return "Log Viewer is empty. No logs are currently loaded.", nil, nil
	}

	entries := t.logBuffer.Slice()
	if len(entries) == 0 {
		return "Log Viewer is empty. No logs are currently loaded.", nil, nil
	}
	pods := make(map[string]int)
	severityCounts := map[string]int{
		"FATAL": 0, "ERROR": 0, "WARN": 0, "INFO": 0, "DEBUG": 0,
	}

	for _, e := range entries {
		if e.PodName != "" {
			pods[e.PodName]++
		}
		switch e.Severity {
		case logparse.SeverityFatal:
			severityCounts["FATAL"]++
		case logparse.SeverityError:
			severityCounts["ERROR"]++
		case logparse.SeverityWarn:
			severityCounts["WARN"]++
		case logparse.SeverityInfo:
			severityCounts["INFO"]++
		case logparse.SeverityDebug:
			severityCounts["DEBUG"]++
		case logparse.SeverityUnknown:
			// no-op: uncategorized lines
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Log Viewer Buffer: %d lines loaded\n\n", len(entries))

	b.WriteString("Source pods:\n")
	for pod, count := range pods {
		fmt.Fprintf(&b, "  - %s (%d lines)\n", pod, count)
	}

	b.WriteString("\nSeverity distribution:\n")
	for _, sev := range []string{"FATAL", "ERROR", "WARN", "INFO", "DEBUG"} {
		if severityCounts[sev] > 0 {
			fmt.Fprintf(&b, "  - %s: %d\n", sev, severityCounts[sev])
		}
	}

	return b.String(), nil, nil
}

func (t *toolExecutor) searchVisibleLogs(args map[string]string) (string, []k8s.LogLine, error) {
	pattern := args["pattern"]
	if pattern == "" {
		return "", nil, fmt.Errorf("pattern is required")
	}

	if t.logBuffer == nil {
		return "Log Viewer is empty. No logs to search.", nil, nil
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", nil, fmt.Errorf("invalid regex pattern %q: %w", pattern, err)
	}

	maxResults := 50
	if mr := args["maxResults"]; mr != "" {
		if n, err := strconv.Atoi(mr); err == nil && n > 0 {
			maxResults = n
		}
	}

	entries := t.logBuffer.Slice()
	if len(entries) == 0 {
		return "Log Viewer is empty. No logs to search.", nil, nil
	}
	var b strings.Builder
	matchCount := 0
	totalMatches := 0

	for _, e := range entries {
		if re.MatchString(e.Raw) {
			totalMatches++
			if matchCount < maxResults {
				if e.PodName != "" {
					fmt.Fprintf(&b, "[%s] ", e.PodName)
				}
				b.WriteString(e.Raw)
				b.WriteByte('\n')
				matchCount++
			}
		}
	}

	if totalMatches == 0 {
		return fmt.Sprintf("No matches found for pattern %q in %d log lines.", pattern, len(entries)), nil, nil
	}

	header := fmt.Sprintf("Found %d matches for pattern %q in %d log lines", totalMatches, pattern, len(entries))
	if totalMatches > maxResults {
		header += fmt.Sprintf(" (showing first %d)", maxResults)
	}
	header += ":\n\n"

	return header + b.String(), nil, nil
}

// --- Phase 2: Resource type handlers ---

func (t *toolExecutor) getStatefulSets(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	if ns == "" {
		return "", nil, fmt.Errorf("namespace is required")
	}

	ssList, err := t.k8sClient.Resources().ListStatefulSets(ns)
	if err != nil {
		return "", nil, fmt.Errorf("list statefulsets: %w", err)
	}

	filter := strings.ToLower(args["filter"])

	var b strings.Builder
	b.WriteString("NAME\tREADY\tSELECTOR\n")
	count := 0
	for _, ss := range ssList {
		if filter != "" && !strings.Contains(strings.ToLower(ss.Name), filter) {
			continue
		}
		sel := formatSelector(ss.Selector)
		fmt.Fprintf(&b, "%s\t%d/%d\t%s\n", ss.Name, ss.Ready, ss.Replicas, sel)
		count++
	}
	if filter != "" {
		fmt.Fprintf(&b, "\n(%d of %d statefulsets match filter %q)", count, len(ssList), args["filter"])
	}
	return b.String(), nil, nil
}

func (t *toolExecutor) getDaemonSets(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	if ns == "" {
		return "", nil, fmt.Errorf("namespace is required")
	}

	dsList, err := t.k8sClient.Resources().ListDaemonSets(ns)
	if err != nil {
		return "", nil, fmt.Errorf("list daemonsets: %w", err)
	}

	filter := strings.ToLower(args["filter"])

	var b strings.Builder
	b.WriteString("NAME\tDESIRED\tCURRENT\tREADY\tSELECTOR\n")
	count := 0
	for _, ds := range dsList {
		if filter != "" && !strings.Contains(strings.ToLower(ds.Name), filter) {
			continue
		}
		sel := formatSelector(ds.Selector)
		fmt.Fprintf(&b, "%s\t%d\t%d\t%d\t%s\n", ds.Name, ds.DesiredScheduled, ds.CurrentScheduled, ds.Ready, sel)
		count++
	}
	if filter != "" {
		fmt.Fprintf(&b, "\n(%d of %d daemonsets match filter %q)", count, len(dsList), args["filter"])
	}
	return b.String(), nil, nil
}

func (t *toolExecutor) getJobs(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	if ns == "" {
		return "", nil, fmt.Errorf("namespace is required")
	}

	jobs, err := t.k8sClient.Resources().ListJobs(ns)
	if err != nil {
		return "", nil, fmt.Errorf("list jobs: %w", err)
	}

	filter := strings.ToLower(args["filter"])

	var b strings.Builder
	b.WriteString("NAME\tCOMPLETIONS\tDURATION\tSELECTOR\n")
	count := 0
	for _, j := range jobs {
		if filter != "" && !strings.Contains(strings.ToLower(j.Name), filter) {
			continue
		}
		status := fmt.Sprintf("%d/%d", j.Succeeded, j.Completions)
		if j.Failed > 0 {
			status += fmt.Sprintf(" (%d failed)", j.Failed)
		}
		sel := formatSelector(j.Selector)
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", j.Name, status, formatDuration(j.Duration), sel)
		count++
	}
	if filter != "" {
		fmt.Fprintf(&b, "\n(%d of %d jobs match filter %q)", count, len(jobs), args["filter"])
	}
	return b.String(), nil, nil
}

func (t *toolExecutor) getCronJobs(args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	if ns == "" {
		return "", nil, fmt.Errorf("namespace is required")
	}

	cjList, err := t.k8sClient.Resources().ListCronJobs(ns)
	if err != nil {
		return "", nil, fmt.Errorf("list cronjobs: %w", err)
	}

	filter := strings.ToLower(args["filter"])

	var b strings.Builder
	b.WriteString("NAME\tSCHEDULE\tSUSPEND\tACTIVE\tLAST SCHEDULE\n")
	count := 0
	for _, cj := range cjList {
		if filter != "" && !strings.Contains(strings.ToLower(cj.Name), filter) {
			continue
		}
		suspend := "False"
		if cj.Suspend {
			suspend = "True"
		}
		lastSched := "<none>"
		if cj.LastScheduleTime != nil {
			lastSched = formatTimeAgo(*cj.LastScheduleTime)
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%d\t%s\n", cj.Name, cj.Schedule, suspend, cj.Active, lastSched)
		count++
	}
	if filter != "" {
		fmt.Fprintf(&b, "\n(%d of %d cronjobs match filter %q)", count, len(cjList), args["filter"])
	}
	return b.String(), nil, nil
}

// --- Phase 2: Analysis handlers ---

func (t *toolExecutor) severityStats() (string, []k8s.LogLine, error) {
	if t.logBuffer == nil || t.logBuffer.Len() == 0 {
		return "Log Viewer is empty. Load logs first with kubectl_logs or kubectl_logs_selector.", nil, nil
	}

	entries := t.logBuffer.Slice()

	// Per-pod severity distribution
	type podStats struct {
		fatal, err, warn, info, debug, unknown int
	}
	perPod := make(map[string]*podStats)

	for _, e := range entries {
		pod := e.PodName
		if pod == "" {
			pod = "<unknown>"
		}
		s, ok := perPod[pod]
		if !ok {
			s = &podStats{}
			perPod[pod] = s
		}
		switch e.Severity {
		case logparse.SeverityFatal:
			s.fatal++
		case logparse.SeverityError:
			s.err++
		case logparse.SeverityWarn:
			s.warn++
		case logparse.SeverityInfo:
			s.info++
		case logparse.SeverityDebug:
			s.debug++
		case logparse.SeverityUnknown:
			s.unknown++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Severity Distribution (%d total lines):\n\n", len(entries))
	b.WriteString("POD\tFATAL\tERROR\tWARN\tINFO\tDEBUG\tOTHER\n")
	for pod, s := range perPod {
		fmt.Fprintf(&b, "%s\t%d\t%d\t%d\t%d\t%d\t%d\n",
			pod, s.fatal, s.err, s.warn, s.info, s.debug, s.unknown)
	}
	return b.String(), nil, nil
}

func (t *toolExecutor) compareLogs(ctx context.Context, args map[string]string) (string, []k8s.LogLine, error) {
	ns := args["namespace"]
	labelSel := args["labelSelector"]
	if ns == "" || labelSel == "" {
		return "", nil, fmt.Errorf("namespace and labelSelector are required")
	}

	r1Since, err := time.ParseDuration(args["range1Since"])
	if err != nil {
		return "", nil, fmt.Errorf("invalid range1Since duration: %w", err)
	}
	r1Until, err := time.ParseDuration(args["range1Until"])
	if err != nil {
		return "", nil, fmt.Errorf("invalid range1Until duration: %w", err)
	}
	r2Since, err := time.ParseDuration(args["range2Since"])
	if err != nil {
		return "", nil, fmt.Errorf("invalid range2Since duration: %w", err)
	}

	selectorMap := parseLabelSelector(labelSel)
	pods, err := t.k8sClient.Resources().ListPodsBySelector(ns, selectorMap)
	if err != nil {
		return "", nil, fmt.Errorf("list pods: %w", err)
	}
	if len(pods) == 0 {
		return "No pods match the selector: " + labelSel, nil, nil
	}

	// Fetch and count severity for each range
	countSev := func(since, sinceTime time.Duration) (map[string]int, int, error) {
		counts := map[string]int{"FATAL": 0, "ERROR": 0, "WARN": 0, "INFO": 0, "DEBUG": 0}
		total := 0
		for _, pod := range pods {
			req := k8s.LogRequest{
				Namespace: ns,
				PodName:   pod.Name,
				Since:     since,
			}
			if sinceTime > 0 {
				st := time.Now().Add(-sinceTime)
				req.SinceTime = &st
			}
			lines, err := t.k8sClient.Logs().GetLogs(ctx, req)
			if err != nil {
				continue
			}
			for _, l := range lines {
				total++
				entry := t.parser.Parse(l.PodName, l.Container, l.Content)
				switch entry.Severity { //nolint:exhaustive // only counting known severities
				case logparse.SeverityFatal:
					counts["FATAL"]++
				case logparse.SeverityError:
					counts["ERROR"]++
				case logparse.SeverityWarn:
					counts["WARN"]++
				case logparse.SeverityInfo:
					counts["INFO"]++
				case logparse.SeverityDebug:
					counts["DEBUG"]++
				}
			}
		}
		return counts, total, nil
	}

	range1, total1, _ := countSev(r1Since, r1Until)
	range2, total2, _ := countSev(r2Since, 0)

	var b strings.Builder
	fmt.Fprintf(&b, "Log Comparison for %s (namespace: %s)\n\n", labelSel, ns)
	fmt.Fprintf(&b, "SEVERITY\tRANGE 1 (%s→%s ago)\tRANGE 2 (%s→now)\tCHANGE\n",
		args["range1Since"], args["range1Until"], args["range2Since"])

	for _, sev := range []string{"FATAL", "ERROR", "WARN", "INFO", "DEBUG"} {
		c1, c2 := range1[sev], range2[sev]
		change := ""
		if c1 > 0 {
			pct := float64(c2-c1) / float64(c1) * 100
			if pct > 0 {
				change = fmt.Sprintf("+%.0f%%", pct)
			} else {
				change = fmt.Sprintf("%.0f%%", pct)
			}
		} else if c2 > 0 {
			change = "new"
		}
		fmt.Fprintf(&b, "%s\t%d\t%d\t%s\n", sev, c1, c2, change)
	}
	fmt.Fprintf(&b, "\nTotal lines: %d → %d\n", total1, total2)
	return b.String(), nil, nil
}

func (t *toolExecutor) getPodMetrics(args map[string]string) (string, []k8s.LogLine, error) {
	// Metrics API is not available in Phase 2 (requires metrics-server integration).
	// Return a graceful fallback message.
	return fmt.Sprintf("Metrics API is not available. To check resource usage for pod %s/%s, "+
		"use kubectl_describe to see resource limits/requests, or check the container status for OOMKilled events.",
		args["namespace"], args["podName"]), nil, nil
}

func (t *toolExecutor) traceLogs(args map[string]string) (string, []k8s.LogLine, error) {
	traceID := args["traceID"]
	if traceID == "" {
		return "", nil, fmt.Errorf("traceID is required")
	}

	if t.logBuffer == nil || t.logBuffer.Len() == 0 {
		return "Log Viewer is empty. Load logs first with kubectl_logs or kubectl_logs_selector.", nil, nil
	}

	entries := t.logBuffer.Slice()
	var b strings.Builder
	matchCount := 0

	for _, e := range entries {
		if strings.Contains(e.Raw, traceID) {
			matchCount++
			if e.PodName != "" {
				fmt.Fprintf(&b, "[%s] ", e.PodName)
			}
			b.WriteString(e.Raw)
			b.WriteByte('\n')
		}
	}

	if matchCount == 0 {
		return fmt.Sprintf("No log lines containing trace ID %q found in %d buffered lines.", traceID, len(entries)), nil, nil
	}

	header := fmt.Sprintf("Found %d lines matching trace ID %q in %d buffered lines:\n\n", matchCount, traceID, len(entries))
	return header + b.String(), nil, nil
}

func (t *toolExecutor) bookmarkLogLines(args map[string]string) (string, []k8s.LogLine, error) {
	indices := args["lineIndices"]
	if indices == "" {
		return "", nil, fmt.Errorf("lineIndices is required")
	}

	// Parse comma-separated indices
	parts := strings.Split(indices, ",")
	var parsed []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if n, err := strconv.Atoi(p); err == nil {
			parsed = append(parsed, n)
		}
	}

	if len(parsed) == 0 {
		return "", nil, fmt.Errorf("no valid line indices provided")
	}

	reason := args["reason"]
	if reason == "" {
		reason = "AI bookmarked"
	}

	// The actual bookmarking is handled by the UI via EventToolCall interception.
	// We return a message that describes what was bookmarked.
	return fmt.Sprintf("Bookmarked %d log lines (indices: %s). Reason: %s. "+
		"Press ' to view bookmarks, n/N to navigate between them.", len(parsed), indices, reason), nil, nil
}

func (t *toolExecutor) listKubeconfigs() (string, []k8s.LogLine, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "ERROR: cannot determine home directory", nil, nil
	}
	kubeDir := filepath.Join(home, ".kube")
	envKC := os.Getenv("KUBECONFIG")

	entries := config.DiscoverKubeconfigs(kubeDir, envKC)
	if len(entries) == 0 {
		return "No kubeconfig files found in ~/.kube/ or $KUBECONFIG.", nil, nil
	}

	// Current context for reference
	currentKC, currentCtx := t.k8sClient.ContextInfo()

	var b strings.Builder
	fmt.Fprintf(&b, "Current: context=%q kubeconfig=%q\n\n", currentCtx, currentKC)
	fmt.Fprintf(&b, "Available kubeconfig files (%d):\n", len(entries))

	for _, e := range entries {
		fmt.Fprintf(&b, "\n## %s (%d contexts)\n", e.Path, e.ContextCount)
		ctxs, err := config.ParseContexts(e.Path)
		if err != nil {
			fmt.Fprintf(&b, "  (error reading: %v)\n", err)
			continue
		}
		for _, c := range ctxs {
			marker := "  "
			if e.Path == currentKC && c.Name == currentCtx {
				marker = "* "
			}
			fmt.Fprintf(&b, "  %s%s (cluster: %s)\n", marker, c.Name, c.Cluster)
		}
	}

	b.WriteString("\nTo switch, call switch_kubeconfig with context=<name> and optionally kubeconfig=<path>.")
	return b.String(), nil, nil
}

func (t *toolExecutor) switchKubeconfig(args map[string]string) (string, []k8s.LogLine, error) {
	ctx := args["context"]
	if ctx == "" {
		return "ERROR: context parameter is required", nil, nil
	}

	kubeconfig := args["kubeconfig"]
	if kubeconfig == "" {
		kp, _ := t.k8sClient.ContextInfo()
		kubeconfig = kp
	}

	if kubeconfig == "" {
		return "ERROR: no kubeconfig path available. Please specify the kubeconfig parameter.", nil, nil
	}

	ctxs, err := config.ParseContexts(kubeconfig)
	if err != nil {
		return fmt.Sprintf("ERROR: cannot read kubeconfig %s: %v", kubeconfig, err), nil, nil
	}

	found := false
	for _, c := range ctxs {
		if c.Name == ctx {
			found = true
			break
		}
	}
	if !found {
		available := make([]string, 0, len(ctxs))
		for _, c := range ctxs {
			available = append(available, c.Name)
		}
		return fmt.Sprintf("ERROR: context %q not found in %s. Available contexts: %s", ctx, kubeconfig, strings.Join(available, ", ")), nil, nil
	}

	return fmt.Sprintf("Switching to context %q (kubeconfig: %s). The UI will perform the connection switch.", ctx, kubeconfig), nil, nil
}

// --- Helpers ---

func parseLabelSelector(sel string) map[string]string {
	result := make(map[string]string)
	for _, part := range strings.Split(sel, ",") {
		part = strings.TrimSpace(part)
		if kv := strings.SplitN(part, "=", 2); len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}
	return result
}

func formatSelector(sel map[string]string) string {
	if len(sel) == 0 {
		return "<none>"
	}
	pairs := make([]string, 0, len(sel))
	for k, v := range sel {
		pairs = append(pairs, k+"="+v)
	}
	return strings.Join(pairs, ",")
}

func formatLogLines(lines []k8s.LogLine) string {
	var b strings.Builder
	for _, l := range lines {
		if l.PodName != "" {
			fmt.Fprintf(&b, "[%s", l.PodName)
			if l.Container != "" {
				fmt.Fprintf(&b, "/%s", l.Container)
			}
			b.WriteString("] ")
		}
		b.WriteString(l.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func formatAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	return formatAge(d)
}

func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "<unknown>"
	}
	return formatAge(time.Since(t))
}

func applyLogOptions(req *k8s.LogRequest, args map[string]string) {
	if s := args["since"]; s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			req.Since = d
		}
	}
	if st := args["sinceTime"]; st != "" {
		if t, err := time.Parse(time.RFC3339, st); err == nil {
			req.SinceTime = &t
		}
	}
	if tl := args["tailLines"]; tl != "" {
		if n, err := strconv.ParseInt(tl, 10, 64); err == nil {
			req.TailLines = &n
		}
	}
	if args["previous"] == "true" {
		req.Previous = true
	}
}

// filterLogLines filters log lines by a regex pattern. Returns all lines if pattern is empty.
func filterLogLines(lines []k8s.LogLine, pattern string) ([]k8s.LogLine, error) {
	if pattern == "" {
		return lines, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid grep pattern %q: %w", pattern, err)
	}
	filtered := make([]k8s.LogLine, 0, len(lines))
	for _, l := range lines {
		if re.MatchString(l.Content) {
			filtered = append(filtered, l)
		}
	}
	return filtered, nil
}

// filterEvents applies client-side field selector filtering to events.
// Supports "involvedObject.name=X", "involvedObject.kind=X", "type=X", "reason=X".
func filterEvents(events []k8s.Event, fieldSelector string) []k8s.Event {
	selectors := parseFieldSelector(fieldSelector)
	if len(selectors) == 0 {
		return events
	}

	filtered := make([]k8s.Event, 0, len(events))
	for _, e := range events {
		if matchEventFields(e, selectors) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func parseFieldSelector(fs string) map[string]string {
	result := make(map[string]string)
	for _, part := range strings.Split(fs, ",") {
		part = strings.TrimSpace(part)
		if kv := strings.SplitN(part, "=", 2); len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}
	return result
}

func matchEventFields(e k8s.Event, selectors map[string]string) bool {
	// e.Object is "Kind/Name" format
	parts := strings.SplitN(e.Object, "/", 2)
	kind, name := "", ""
	if len(parts) == 2 {
		kind = parts[0]
		name = parts[1]
	}

	for field, val := range selectors {
		switch field {
		case "involvedObject.name":
			if !strings.EqualFold(name, val) {
				return false
			}
		case "involvedObject.kind":
			if !strings.EqualFold(kind, val) {
				return false
			}
		case "type":
			if !strings.EqualFold(e.Type, val) {
				return false
			}
		case "reason":
			if !strings.EqualFold(e.Reason, val) {
				return false
			}
		}
	}
	return true
}
