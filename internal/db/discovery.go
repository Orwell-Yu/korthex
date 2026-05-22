package db

import (
	"strings"
	"time"

	"github.com/Orwell-Yu/korthex/internal/k8s"
)

// discoverer finds database pods in a K8s namespace.
type discoverer struct {
	k8sClient     k8s.ResourceLister
	imagePatterns []string // custom patterns from config
}

// Discover scans pods in a namespace and returns detected database pods.
func (d *discoverer) Discover(namespace string) ([]DatabaseInfo, error) {
	pods, err := d.k8sClient.ListPods(namespace)
	if err != nil {
		return nil, err
	}

	var results []DatabaseInfo
	seen := make(map[string]bool)

	for _, pod := range pods {
		if info, ok := d.detectDB(pod, namespace); ok && !seen[pod.Name] {
			seen[pod.Name] = true
			results = append(results, info)
		}
	}
	return results, nil
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
