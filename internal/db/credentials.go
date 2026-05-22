package db

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Orwell-Yu/korthex/internal/k8s"
)

// credentialFetcher discovers and caches database credentials from pod specs and secrets.
type credentialFetcher struct {
	inspector k8s.PodInspector
	cache     sync.Map // namespace/podName → *Credentials
}

// GetCredentials returns credentials for a database pod, using cache when available.
func (f *credentialFetcher) GetCredentials(ctx context.Context, namespace, podName string) (*Credentials, error) {
	key := namespace + "/" + podName
	if cached, ok := f.cache.Load(key); ok {
		return cached.(*Credentials), nil
	}

	creds, err := f.discoverCredentials(ctx, namespace, podName)
	if err != nil {
		return nil, err
	}

	f.cache.Store(key, creds)
	return creds, nil
}

// discoverCredentials runs the credential discovery chain:
// 1. Pod env vars (direct values)
// 2. Pod env vars from Secret refs (secretKeyRef)
// 3. envFrom secretRef entries
func (f *credentialFetcher) discoverCredentials(ctx context.Context, namespace, podName string) (*Credentials, error) {
	envs, err := f.inspector.GetPodContainerEnvs(namespace, podName)
	if err != nil {
		return nil, fmt.Errorf("get pod envs: %w", err)
	}

	// Flatten all container envs into one map (first container wins for conflicts)
	flatEnvs := make(map[string]string)
	var secretRefs []k8s.EnvVar // env vars sourced from secrets

	for _, containerEnvs := range envs {
		for _, ev := range containerEnvs {
			if ev.SecretName != "" {
				secretRefs = append(secretRefs, ev)
			} else if ev.Value != "" {
				if _, exists := flatEnvs[ev.Name]; !exists {
					flatEnvs[ev.Name] = ev.Value
				}
			}
		}
	}

	// Step 1: Try direct env vars
	if creds := extractCredsFromEnvs(flatEnvs); creds != nil {
		return creds, nil
	}

	// Step 2: Try resolving secretKeyRef env vars
	for _, ref := range secretRefs {
		secretData, err := f.inspector.GetSecretData(ctx, namespace, ref.SecretName)
		if err != nil {
			continue // secret may not be accessible
		}
		if val, ok := secretData[ref.SecretKey]; ok {
			flatEnvs[ref.Name] = val
		}
	}
	if creds := extractCredsFromEnvs(flatEnvs); creds != nil {
		return creds, nil
	}

	// Step 3: Try envFrom secretRef entries
	envFromSources, err := f.inspector.GetPodEnvFromSecrets(namespace, podName)
	if err == nil {
		for _, sources := range envFromSources {
			for _, src := range sources {
				secretData, err := f.inspector.GetSecretData(ctx, namespace, src.SecretName)
				if err != nil {
					continue
				}
				for k, v := range secretData {
					if _, exists := flatEnvs[k]; !exists {
						flatEnvs[k] = v
					}
				}
			}
		}
		if creds := extractCredsFromEnvs(flatEnvs); creds != nil {
			return creds, nil
		}
	}

	return nil, fmt.Errorf("no credentials found for %s/%s", namespace, podName)
}

// MySQL env var names. Distinguished by privilege scope: MYSQL_ROOT_PASSWORD pairs
// with the root account, MYSQL_PASSWORD pairs with the user named in MYSQL_USER —
// they are NOT interchangeable, so extractCredsFromEnvs reads them separately.
var mysqlUserVars = []string{"MYSQL_USER"}
var mysqlDBVars = []string{"MYSQL_DATABASE"}

// PostgreSQL env var names in priority order.
var pgPasswordVars = []string{"POSTGRES_PASSWORD", "PGPASSWORD"}
var pgUserVars = []string{"POSTGRES_USER", "PGUSER"}
var pgDBVars = []string{"POSTGRES_DB", "PGDATABASE"}

// Generic env var names.
var genericPasswordVars = []string{"DATABASE_PASSWORD", "DB_PASSWORD"}
var genericUserVars = []string{"DATABASE_USER", "DB_USER", "DATABASE_USERNAME", "DB_USERNAME"}
var genericDBVars = []string{"DATABASE_NAME", "DB_NAME"}

// extractCredsFromEnvs attempts to build Credentials from a flat map of env vars.
func extractCredsFromEnvs(envs map[string]string) *Credentials {
	// Try MySQL vars. The official mysql image gives MYSQL_ROOT_PASSWORD to root and
	// MYSQL_PASSWORD only to the app user named in MYSQL_USER. If we see
	// MYSQL_PASSWORD with no MYSQL_USER, the app password is paired with an unknown
	// account; defaulting to root would attempt the *root* login with the *app*
	// password, which is wrong and may rate-limit auth. Only fall back to root when
	// MYSQL_ROOT_PASSWORD is the source.
	if rootPwd := firstMatch(envs, []string{"MYSQL_ROOT_PASSWORD"}); rootPwd != "" {
		return &Credentials{
			Password: rootPwd,
			Username: firstMatchOr(envs, mysqlUserVars, "root"),
			Database: firstMatchOr(envs, mysqlDBVars, ""),
			Host:     "localhost",
			Port:     3306,
		}
	}
	if appPwd := firstMatch(envs, []string{"MYSQL_PASSWORD"}); appPwd != "" {
		if user := firstMatch(envs, mysqlUserVars); user != "" {
			return &Credentials{
				Password: appPwd,
				Username: user,
				Database: firstMatchOr(envs, mysqlDBVars, ""),
				Host:     "localhost",
				Port:     3306,
			}
		}
		// MYSQL_PASSWORD present but no MYSQL_USER — keep walking the discovery
		// chain instead of guessing root. Return nil so callers try the next strategy.
	}

	// Try PostgreSQL vars
	if password := firstMatch(envs, pgPasswordVars); password != "" {
		creds := &Credentials{
			Password: password,
			Username: firstMatchOr(envs, pgUserVars, "postgres"),
			Database: firstMatchOr(envs, pgDBVars, ""),
			Host:     "localhost",
			Port:     5432,
		}
		return creds
	}

	// Try DATABASE_URL
	if dbURL, ok := envs["DATABASE_URL"]; ok && dbURL != "" {
		if creds := parseDatabaseURL(dbURL); creds != nil {
			return creds
		}
	}

	// Try generic vars
	if password := firstMatch(envs, genericPasswordVars); password != "" {
		creds := &Credentials{
			Password: password,
			Username: firstMatchOr(envs, genericUserVars, ""),
			Database: firstMatchOr(envs, genericDBVars, ""),
			Host:     "localhost",
		}
		return creds
	}

	return nil
}

// firstMatch returns the value of the first matching env var, or "".
func firstMatch(envs map[string]string, keys []string) string {
	for _, k := range keys {
		if v, ok := envs[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// firstMatchOr returns the first matching value, or fallback if none found.
func firstMatchOr(envs map[string]string, keys []string, fallback string) string {
	if v := firstMatch(envs, keys); v != "" {
		return v
	}
	return fallback
}

// parseDatabaseURL parses a DATABASE_URL like:
// postgresql://user:password@host:port/dbname
// mysql://user:password@host:port/dbname
func parseDatabaseURL(url string) *Credentials {
	// Strip scheme
	var creds Credentials
	rest := url
	if strings.HasPrefix(rest, "postgresql://") || strings.HasPrefix(rest, "postgres://") {
		creds.Port = 5432
		rest = rest[strings.Index(rest, "://")+3:]
	} else if strings.HasPrefix(rest, "mysql://") {
		creds.Port = 3306
		rest = rest[len("mysql://"):]
	} else {
		return nil
	}

	// Split user:pass@host:port/dbname
	atIdx := strings.LastIndex(rest, "@")
	if atIdx == -1 {
		return nil
	}

	userPass := rest[:atIdx]
	hostDBPart := rest[atIdx+1:]

	// Parse user:pass
	if colonIdx := strings.Index(userPass, ":"); colonIdx != -1 {
		creds.Username = userPass[:colonIdx]
		creds.Password = userPass[colonIdx+1:]
	} else {
		creds.Username = userPass
	}

	// Parse host:port/dbname
	slashIdx := strings.Index(hostDBPart, "/")
	if slashIdx != -1 {
		creds.Database = strings.Split(hostDBPart[slashIdx+1:], "?")[0] // strip query params
		hostDBPart = hostDBPart[:slashIdx]
	}

	if colonIdx := strings.Index(hostDBPart, ":"); colonIdx != -1 {
		creds.Host = hostDBPart[:colonIdx]
	} else {
		creds.Host = hostDBPart
	}

	if creds.Password == "" {
		return nil
	}

	return &creds
}
