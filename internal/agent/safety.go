package agent

// safetyChecker implements SafetyChecker with a configurable whitelist map.
type safetyChecker struct {
	whitelist map[string]SafetyLevel
}

// NewSafetyChecker creates a SafetyChecker with the Phase 1+2+3 read-only tool whitelist.
func NewSafetyChecker() SafetyChecker {
	return &safetyChecker{
		whitelist: map[string]SafetyLevel{
			// Phase 1
			"kubectl_get_namespaces":    SafetyAllowed,
			"kubectl_get_deployments":   SafetyAllowed,
			"kubectl_get_pods":          SafetyAllowed,
			"kubectl_logs":              SafetyAllowed,
			"kubectl_logs_selector":     SafetyAllowed,
			"kubectl_describe":          SafetyAllowed,
			"kubectl_get_events":        SafetyAllowed,
			"navigate_resource_browser": SafetyAllowed,
			"get_log_viewer_state":      SafetyAllowed,
			"search_visible_logs":       SafetyAllowed,
			// Phase 2
			"kubectl_get_statefulsets": SafetyAllowed,
			"kubectl_get_daemonsets":   SafetyAllowed,
			"kubectl_get_jobs":         SafetyAllowed,
			"kubectl_get_cronjobs":     SafetyAllowed,
			"severity_stats":           SafetyAllowed,
			"compare_logs":             SafetyAllowed,
			"get_pod_metrics":          SafetyAllowed,
			"trace_logs":               SafetyAllowed,
			"bookmark_log_lines":       SafetyAllowed,
			"list_kubeconfigs":         SafetyAllowed,
			"switch_kubeconfig":        SafetyAllowed,
			// Phase 3: Database tools
			"discover_databases": SafetyAllowed,
			"get_db_credentials": SafetyAllowed,
			"get_db_schema":      SafetyAllowed,
			"query_database":     SafetyAllowed,
			"get_foreign_keys":   SafetyAllowed,
		},
	}
}

// Check validates whether a tool operation is permitted.
// Returns SafetyAllowed for whitelisted tools, SafetyDenied for everything else.
func (s *safetyChecker) Check(toolName string, args map[string]string) (SafetyLevel, string) {
	if level, ok := s.whitelist[toolName]; ok {
		return level, ""
	}
	return SafetyDenied, "Operation not supported in the current phase. Read-only operations only."
}
