package db

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
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

// GetCredentialsFromConnString resolves credentials by reading a specific
// connection-string env var off the pod and parsing it. Used by the RDS-via-pod
// discovery path, where DatabaseInfo.ConnEnvVar names the variable to read.
// Both direct-value env vars and secretKeyRef-sourced ones are supported (the
// latter has an empty Value in the PodSpec, so the Secret is read instead).
// Results are cached under namespace/podName/envVar.
func (f *credentialFetcher) GetCredentialsFromConnString(ctx context.Context, namespace, podName, envVar string) (*Credentials, error) {
	key := namespace + "/" + podName + "/" + envVar
	if cached, ok := f.cache.Load(key); ok {
		return cached.(*Credentials), nil
	}

	envs, err := f.inspector.GetPodContainerEnvs(namespace, podName)
	if err != nil {
		return nil, fmt.Errorf("get pod envs: %w", err)
	}
	for _, containerEnvs := range envs {
		for _, ev := range containerEnvs {
			if ev.Name != envVar {
				continue
			}
			value := ev.Value
			if value == "" && ev.SecretName != "" {
				// secretKeyRef: resolve the value from the Secret.
				if data, derr := f.inspector.GetSecretData(ctx, namespace, ev.SecretName); derr == nil {
					value = data[ev.SecretKey]
				}
			}
			if value == "" {
				continue
			}
			creds := parseDatabaseURL(value)
			if creds == nil {
				return nil, fmt.Errorf("env %s on %s/%s is not a valid connection string", envVar, namespace, podName)
			}
			f.cache.Store(key, creds)
			return creds, nil
		}
	}
	return nil, fmt.Errorf("env %s not found on %s/%s", envVar, namespace, podName)
}

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

// parseDatabaseURL parses a connection URL like:
// postgresql+asyncpg://user:password@host:port/dbname
// mysql+aiomysql://user:password@host:port/dbname
// The "+driver" scheme suffix and URL-encoded credentials are handled.
func parseDatabaseURL(rawURL string) *Credentials {
	scheme := rawURL
	if i := strings.Index(scheme, "://"); i != -1 {
		scheme = strings.ToLower(scheme[:i])
	}
	if plus := strings.Index(scheme, "+"); plus != -1 {
		scheme = scheme[:plus] // strip "+asyncpg" / "+aiomysql"
	}

	var creds Credentials
	switch scheme {
	case "postgresql", "postgres":
		creds.Port = 5432
	case "mysql", "mariadb":
		creds.Port = 3306
	default:
		return nil
	}

	// Normalize scheme so net/url can parse it, then use the standard parser
	// (handles URL-encoded user/password correctly).
	normalized := rawURL
	if i := strings.Index(rawURL, "://"); i != -1 {
		normalized = scheme + rawURL[i:]
	}
	u, err := url.Parse(normalized)
	if err != nil || u.Host == "" {
		return nil
	}

	creds.Host = u.Hostname()
	if p := u.Port(); p != "" {
		if pi, err := strconv.Atoi(p); err == nil {
			creds.Port = pi
		}
	}
	if u.User != nil {
		creds.Username = u.User.Username()
		if pwd, ok := u.User.Password(); ok {
			creds.Password = pwd // already URL-decoded by net/url
		}
	}
	creds.Database = strings.TrimPrefix(u.Path, "/")

	if creds.Password == "" {
		return nil
	}
	return &creds
}
