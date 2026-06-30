package db

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Orwell-Yu/korthex/internal/k8s"
)

// discoverer finds databases reachable from a K8s namespace.
//
// Primary strategy (RDS-via-pod): scan running application pods' env vars for
// connection strings (DATABASE_URL etc.) pointing at external managed databases,
// then probe each candidate pod for a usable python interpreter (the exec entry).
// Fallback strategy: image/port/label matching for genuine in-cluster DB pods,
// used ONLY when no connection strings are found.
type discoverer struct {
	k8sClient     k8s.ResourceLister
	inspector     k8s.PodInspector
	podExec       k8s.PodExecutor // used to probe pods for a usable python interpreter
	connEnvVars   []string        // env var name suffixes that may hold a connection string
	imagePatterns []string        // custom in-cluster image patterns (fallback)
}

// Discover scans pods in a namespace and returns detected databases.
func (d *discoverer) Discover(namespace string) ([]DatabaseInfo, error) {
	pods, err := d.k8sClient.ListPods(namespace)
	if err != nil {
		return nil, err
	}

	// Primary: connection strings in running app pods. Dedup by (host, database)
	// so one logical RDS database appears once even when many pods can reach it.
	// Among candidates for the same database, prefer a pod with a usable python.
	byConn := make(map[string]DatabaseInfo)
	pythonCache := make(map[string]string) // podName → probed python path ("" = none)

	for _, pod := range pods {
		// Skip pods that can never serve as an exec entry point: one-shot
		// init/migration Jobs (often Succeeded) and any non-Running pod.
		if isExcludedPod(pod) {
			continue
		}
		if d.inspector == nil {
			continue
		}
		for _, info := range d.discoverFromEnv(pod, namespace) {
			key := info.Host + "/" + info.Database
			existing, seen := byConn[key]
			// If we already have a pod with a working python for this DB, keep it.
			if seen && existing.PythonPath != "" {
				continue
			}
			// Probe this candidate's python (cached per pod).
			py, ok := pythonCache[pod.Name]
			if !ok {
				py = d.probePython(namespace, pod.Name)
				pythonCache[pod.Name] = py
			}
			info.PythonPath = py
			// Prefer a candidate with a usable python; otherwise keep the first seen.
			if !seen || (existing.PythonPath == "" && py != "") {
				byConn[key] = info
			}
		}
	}

	if len(byConn) > 0 {
		results := make([]DatabaseInfo, 0, len(byConn))
		for _, info := range byConn {
			results = append(results, info)
		}
		return results, nil
	}

	// Fallback (only when no connection strings found): genuine in-cluster DB pods.
	var results []DatabaseInfo
	seenPod := make(map[string]bool)
	for _, pod := range pods {
		if isExcludedPod(pod) {
			continue
		}
		if info, ok := d.detectDB(pod, namespace); ok && !seenPod[pod.Name] {
			seenPod[pod.Name] = true
			info.ID = dbID(namespace, pod.Name, info.Database)
			results = append(results, info)
		}
	}
	return results, nil
}

// pythonProbeScript finds a python interpreter that can import a DB driver. It
// prints "USABLE:<path>" on success or "NONE". Verified against pods with a full
// venv, a bare python3 (no drivers), and no python at all.
const pythonProbeScript = `for py in /app/server/.venv/bin/python /usr/local/bin/python3 python3 python; do
  if [ -x "$py" ] || command -v "$py" >/dev/null 2>&1; then
    if "$py" -c "import asyncpg" 2>/dev/null || "$py" -c "import pymysql" 2>/dev/null; then
      echo "USABLE:$py"; exit 0
    fi
  fi
done
echo "NONE"`

// probePython returns the path to a usable python in the pod, or "" if none.
func (d *discoverer) probePython(namespace, podName string) string {
	if d.podExec == nil {
		return ""
	}
	stdout, _, err := d.podExec.ExecInPod(context.Background(), namespace, podName, "",
		[]string{"sh", "-c", pythonProbeScript})
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(stdout), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "USABLE:"); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// discoverFromEnv reads the pod's env vars and returns a DatabaseInfo for each
// recognized connection string. PodName is the pod itself (used as exec entry).
// Both direct-value env vars and secretKeyRef-sourced ones are considered: for
// the latter the value is resolved from the Secret only to derive metadata
// (type/host/database); the query itself still reads the env at runtime in-pod.
func (d *discoverer) discoverFromEnv(pod k8s.Pod, namespace string) []DatabaseInfo {
	envs, err := d.inspector.GetPodContainerEnvs(namespace, pod.Name)
	if err != nil {
		return nil
	}

	// Resolve each connection-string env var to its value. Direct values are used
	// as-is; secretKeyRef values are fetched from the Secret (first container wins).
	type connVar struct{ name, value string }
	var conns []connVar
	seen := make(map[string]bool)
	for _, containerEnvs := range envs {
		for _, ev := range containerEnvs {
			if seen[ev.Name] || !d.isConnEnvVar(ev.Name) {
				continue
			}
			value := ev.Value
			if value == "" && ev.SecretName != "" {
				if data, derr := d.inspector.GetSecretData(context.Background(), namespace, ev.SecretName); derr == nil {
					value = data[ev.SecretKey]
				}
			}
			if value == "" {
				continue
			}
			seen[ev.Name] = true
			conns = append(conns, connVar{ev.Name, value})
		}
	}

	// Also expand envFrom[].secretRef: each Secret's data keys become env vars in
	// the container (K8s behaviour), so a key matching a conn-string name is a
	// reachable connection string. The in-pod python reads os.environ[key] at runtime.
	if fromSecrets, ferr := d.inspector.GetPodEnvFromSecrets(namespace, pod.Name); ferr == nil {
		for _, sources := range fromSecrets {
			for _, src := range sources {
				data, derr := d.inspector.GetSecretData(context.Background(), namespace, src.SecretName)
				if derr != nil {
					continue
				}
				for key, value := range data {
					if seen[key] || !d.isConnEnvVar(key) || value == "" {
						continue
					}
					seen[key] = true
					conns = append(conns, connVar{key, value})
				}
			}
		}
	}

	var out []DatabaseInfo
	for _, c := range conns {
		dbType, host, port, database, ok := parseConnString(c.value)
		if !ok {
			continue
		}
		out = append(out, DatabaseInfo{
			ID:         dbID(namespace, pod.Name, database),
			Namespace:  namespace,
			PodName:    pod.Name,
			DBType:     dbType,
			Host:       host,
			Port:       port,
			Database:   database,
			ConnEnvVar: c.name,
			Label:      database + " (" + string(dbType) + ")",
			Detected:   time.Now(),
		})
	}
	return out
}

// isConnEnvVar reports whether an env var name looks like a DB connection string
// holder. Matching is by suffix so prefixed variants (e.g. MULETEAM_APP_DATABASE_URL)
// are covered, not just the exact configured names.
func (d *discoverer) isConnEnvVar(name string) bool {
	upper := strings.ToUpper(name)
	for _, want := range d.connEnvVars {
		w := strings.ToUpper(want)
		if upper == w || strings.HasSuffix(upper, "_"+w) {
			return true
		}
	}
	return false
}

// parseConnString extracts (type, host, port, database) from a SQLAlchemy-style
// or plain connection URL, tolerating "+driver" scheme suffixes.
func parseConnString(raw string) (DatabaseType, string, int, string, bool) {
	scheme := raw
	if i := strings.Index(scheme, "://"); i != -1 {
		scheme = scheme[:i]
	}
	scheme = strings.ToLower(scheme)
	if plus := strings.Index(scheme, "+"); plus != -1 {
		scheme = scheme[:plus] // strip "+asyncpg" / "+aiomysql"
	}

	var dbType DatabaseType
	var defaultPort int
	switch scheme {
	case "postgresql", "postgres":
		dbType, defaultPort = PostgreSQL, 5432
	case "mysql", "mariadb":
		dbType, defaultPort = MySQL, 3306
	default:
		return "", "", 0, "", false
	}

	// Normalize to a parseable URL (strip the +driver for net/url too).
	normalized := raw
	if i := strings.Index(raw, "://"); i != -1 {
		normalized = scheme + raw[i:]
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return "", "", 0, "", false
	}

	host := u.Hostname()
	if host == "" {
		return "", "", 0, "", false
	}
	port := defaultPort
	if p := u.Port(); p != "" {
		if pi, err := strconv.Atoi(p); err == nil {
			port = pi
		}
	}
	database := strings.TrimPrefix(u.Path, "/")
	return dbType, host, port, database, true
}

// isExcludedPod reports whether a pod must never be treated as a database target.
func isExcludedPod(pod k8s.Pod) bool {
	if pod.OwnerKind == "Job" {
		return true
	}
	if pod.Status != "" && pod.Status != "Running" {
		return true
	}
	lower := strings.ToLower(pod.Name)
	for _, prefix := range []string{"init-", "migration-", "migrate-"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	if strings.Contains(lower, "-migrate-") || strings.Contains(lower, "-migration-") {
		return true
	}
	return false
}

// detectDB checks a pod for database signals (image, port, label, StatefulSet owner).
func (d *discoverer) detectDB(pod k8s.Pod, namespace string) (DatabaseInfo, bool) {
	info := DatabaseInfo{
		Namespace: namespace,
		PodName:   pod.Name,
		Detected:  time.Now(),
	}

	// Priority 1: Image name match
	for _, c := range pod.Containers {
		if dbType, port, ok := matchImage(c.Image, d.imagePatterns); ok {
			info.DBType = dbType
			info.Port = port
			return info, true
		}
	}

	// Priority 2: Container port match
	for _, c := range pod.Containers {
		for _, p := range c.Ports {
			if dbType, ok := matchPort(p.ContainerPort); ok {
				info.DBType = dbType
				info.Port = int(p.ContainerPort)
				return info, true
			}
		}
	}

	// Priority 3: Label match
	if dbType, ok := matchLabels(pod.Labels); ok {
		info.DBType = dbType
		if dbType == MySQL {
			info.Port = 3306
		} else {
			info.Port = 5432
		}
		return info, true
	}

	// Priority 4: StatefulSet with DB-like name
	if pod.OwnerKind == "StatefulSet" && pod.OwnerName != "" {
		if dbType, ok := matchStatefulSetName(pod.OwnerName); ok {
			info.DBType = dbType
			if dbType == MySQL {
				info.Port = 3306
			} else {
				info.Port = 5432
			}
			return info, true
		}
	}

	return DatabaseInfo{}, false
}

// Built-in image prefixes for database detection.
var builtinImagePrefixes = map[string]DatabaseType{
	"mysql":    MySQL,
	"mariadb":  MySQL,
	"percona":  MySQL,
	"postgres": PostgreSQL,
	"postgis":  PostgreSQL,
}

// matchImage checks if a container image matches known database image patterns.
func matchImage(image string, customPatterns []string) (DatabaseType, int, bool) {
	// Normalize: strip registry prefix and tag
	imageName := normalizeImageName(image)

	// Check built-in patterns
	for prefix, dbType := range builtinImagePrefixes {
		if strings.HasPrefix(imageName, prefix) {
			port := 3306
			if dbType == PostgreSQL {
				port = 5432
			}
			return dbType, port, true
		}
	}

	// Check custom patterns from config
	for _, pattern := range customPatterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if strings.Contains(imageName, pattern) {
			// Custom patterns: guess type from pattern content
			if strings.Contains(pattern, "mysql") || strings.Contains(pattern, "mariadb") {
				return MySQL, 3306, true
			}
			if strings.Contains(pattern, "postgres") || strings.Contains(pattern, "postgis") {
				return PostgreSQL, 5432, true
			}
		}
	}

	return "", 0, false
}

// normalizeImageName extracts the base image name, stripping registry and tag.
// e.g. "docker.io/library/mysql:8.0" → "mysql"
func normalizeImageName(image string) string {
	image = strings.ToLower(image)
	// Strip tag
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		// Avoid stripping port from registry URL (e.g., registry:5000/image:tag)
		afterColon := image[idx+1:]
		if !strings.Contains(afterColon, "/") {
			image = image[:idx]
		}
	}
	// Strip digest
	if idx := strings.Index(image, "@"); idx != -1 {
		image = image[:idx]
	}
	// Take the last path segment (strip registry/namespace prefix)
	if idx := strings.LastIndex(image, "/"); idx != -1 {
		image = image[idx+1:]
	}
	return image
}

// matchPort maps well-known database ports to DatabaseType.
func matchPort(port int32) (DatabaseType, bool) {
	switch port {
	case 3306:
		return MySQL, true
	case 5432:
		return PostgreSQL, true
	default:
		return "", false
	}
}

// Known label keys and values for database detection.
var dbLabelKeys = []string{
	"app.kubernetes.io/name",
	"app",
}

var labelValueToType = map[string]DatabaseType{
	"mysql":      MySQL,
	"mariadb":    MySQL,
	"percona":    MySQL,
	"postgresql": PostgreSQL,
	"postgres":   PostgreSQL,
	"postgis":    PostgreSQL,
}

// matchLabels checks pod labels for database indicators.
func matchLabels(labels map[string]string) (DatabaseType, bool) {
	for _, key := range dbLabelKeys {
		if val, ok := labels[key]; ok {
			val = strings.ToLower(val)
			if dbType, found := labelValueToType[val]; found {
				return dbType, true
			}
		}
	}
	return "", false
}

// matchStatefulSetName checks if a StatefulSet name suggests a database workload.
func matchStatefulSetName(name string) (DatabaseType, bool) {
	lower := strings.ToLower(name)
	mysqlIndicators := []string{"mysql", "mariadb", "percona"}
	pgIndicators := []string{"postgresql", "postgres", "postgis"}

	for _, ind := range mysqlIndicators {
		if strings.Contains(lower, ind) {
			return MySQL, true
		}
	}
	for _, ind := range pgIndicators {
		if strings.Contains(lower, ind) {
			return PostgreSQL, true
		}
	}
	return "", false
}
