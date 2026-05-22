package db

import (
	"context"
	"fmt"
	"testing"

	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentials_MySQLRootPassword(t *testing.T) {
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{
				"mysql": {
					{Name: "MYSQL_ROOT_PASSWORD", Value: "rootpass"},
					{Name: "MYSQL_DATABASE", Value: "mydb"},
				},
			}, nil
		},
	}

	f := &credentialFetcher{inspector: inspector}
	creds, err := f.GetCredentials(context.Background(), "default", "mysql-0")
	require.NoError(t, err)
	assert.Equal(t, "root", creds.Username)
	assert.Equal(t, "rootpass", creds.Password)
	assert.Equal(t, "mydb", creds.Database)
}

func TestCredentials_PostgresPassword(t *testing.T) {
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{
				"postgres": {
					{Name: "POSTGRES_PASSWORD", Value: "pgpass"},
					{Name: "PGUSER", Value: "pgadmin"},
					{Name: "POSTGRES_DB", Value: "app"},
				},
			}, nil
		},
	}

	f := &credentialFetcher{inspector: inspector}
	creds, err := f.GetCredentials(context.Background(), "default", "pg-0")
	require.NoError(t, err)
	assert.Equal(t, "pgadmin", creds.Username)
	assert.Equal(t, "pgpass", creds.Password)
	assert.Equal(t, "app", creds.Database)
}

func TestCredentials_CacheHit(t *testing.T) {
	callCount := 0
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			callCount++
			return map[string][]k8s.EnvVar{
				"mysql": {
					{Name: "MYSQL_ROOT_PASSWORD", Value: "pass"},
				},
			}, nil
		},
	}

	f := &credentialFetcher{inspector: inspector}

	_, err := f.GetCredentials(context.Background(), "default", "mysql-0")
	require.NoError(t, err)

	_, err = f.GetCredentials(context.Background(), "default", "mysql-0")
	require.NoError(t, err)

	assert.Equal(t, 1, callCount, "expected only one k8s call due to cache")
}

func TestCredentials_NoCreds(t *testing.T) {
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{
				"app": {
					{Name: "APP_PORT", Value: "8080"},
				},
			}, nil
		},
		GetPodEnvFromSecretsFunc: func(ns, pod string) (map[string][]k8s.EnvFromSource, error) {
			return map[string][]k8s.EnvFromSource{}, nil
		},
	}

	f := &credentialFetcher{inspector: inspector}
	_, err := f.GetCredentials(context.Background(), "default", "app-0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no credentials found")
}

func TestCredentials_FromSecretKeyRef(t *testing.T) {
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{
				"mysql": {
					{Name: "MYSQL_ROOT_PASSWORD", SecretName: "mysql-secret", SecretKey: "password"},
					{Name: "MYSQL_DATABASE", Value: "mydb"},
				},
			}, nil
		},
		GetSecretDataFunc: func(ctx context.Context, ns, secretName string) (map[string]string, error) {
			if secretName == "mysql-secret" {
				return map[string]string{"password": "secret-pass"}, nil
			}
			return nil, fmt.Errorf("not found")
		},
		GetPodEnvFromSecretsFunc: func(ns, pod string) (map[string][]k8s.EnvFromSource, error) {
			return map[string][]k8s.EnvFromSource{}, nil
		},
	}

	f := &credentialFetcher{inspector: inspector}
	creds, err := f.GetCredentials(context.Background(), "default", "mysql-0")
	require.NoError(t, err)
	assert.Equal(t, "secret-pass", creds.Password)
	assert.Equal(t, "mydb", creds.Database)
}

func TestCredentials_FromEnvFromSecret(t *testing.T) {
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{
				"postgres": {},
			}, nil
		},
		GetPodEnvFromSecretsFunc: func(ns, pod string) (map[string][]k8s.EnvFromSource, error) {
			return map[string][]k8s.EnvFromSource{
				"postgres": {
					{SecretName: "pg-creds"},
				},
			}, nil
		},
		GetSecretDataFunc: func(ctx context.Context, ns, secretName string) (map[string]string, error) {
			if secretName == "pg-creds" {
				return map[string]string{
					"POSTGRES_PASSWORD": "fromenv",
					"POSTGRES_USER":     "admin",
					"POSTGRES_DB":       "proddb",
				}, nil
			}
			return nil, fmt.Errorf("not found")
		},
	}

	f := &credentialFetcher{inspector: inspector}
	creds, err := f.GetCredentials(context.Background(), "default", "pg-0")
	require.NoError(t, err)
	assert.Equal(t, "fromenv", creds.Password)
	assert.Equal(t, "admin", creds.Username)
	assert.Equal(t, "proddb", creds.Database)
}

func TestCredentials_DatabaseURL(t *testing.T) {
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{
				"app": {
					{Name: "DATABASE_URL", Value: "postgresql://user:pass@dbhost:5432/mydb?sslmode=disable"},
				},
			}, nil
		},
	}

	f := &credentialFetcher{inspector: inspector}
	creds, err := f.GetCredentials(context.Background(), "default", "app-0")
	require.NoError(t, err)
	assert.Equal(t, "user", creds.Username)
	assert.Equal(t, "pass", creds.Password)
	assert.Equal(t, "mydb", creds.Database)
	assert.Equal(t, "dbhost", creds.Host)
}

func TestParseDatabaseURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected *Credentials
	}{
		{
			"postgres full",
			"postgresql://user:pass@host:5432/db",
			&Credentials{Username: "user", Password: "pass", Host: "host", Port: 5432, Database: "db"},
		},
		{
			"mysql full",
			"mysql://root:secret@127.0.0.1:3306/mydb",
			&Credentials{Username: "root", Password: "secret", Host: "127.0.0.1", Port: 3306, Database: "mydb"},
		},
		{
			"with query params",
			"postgres://u:p@h/db?sslmode=disable",
			&Credentials{Username: "u", Password: "p", Host: "h", Port: 5432, Database: "db"},
		},
		{
			"unsupported scheme",
			"redis://host:6379",
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDatabaseURL(tt.url)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expected.Username, result.Username)
				assert.Equal(t, tt.expected.Password, result.Password)
				assert.Equal(t, tt.expected.Database, result.Database)
				assert.Equal(t, tt.expected.Host, result.Host)
			}
		})
	}
}

// TestExtractCreds_MySQLPasswordWithoutUser — the official mysql image gives
// MYSQL_PASSWORD only to the user named in MYSQL_USER. If we see MYSQL_PASSWORD
// without MYSQL_USER, the password belongs to an unknown app account, not root.
// Returning root with the app password would attempt the wrong login and may
// trip rate-limiting. The extractor must skip this branch and let the next
// discovery strategy (Secret refs, envFrom, DATABASE_URL) run instead.
func TestExtractCreds_MySQLPasswordWithoutUser(t *testing.T) {
	envs := map[string]string{
		"MYSQL_PASSWORD": "apppass",
		"MYSQL_DATABASE": "appdb",
	}
	creds := extractCredsFromEnvs(envs)
	assert.Nil(t, creds, "MYSQL_PASSWORD without MYSQL_USER must not silently default to root")
}

// TestExtractCreds_MySQLAppUserAndPassword — when both MYSQL_PASSWORD and
// MYSQL_USER are present, build credentials for the app user (not root).
func TestExtractCreds_MySQLAppUserAndPassword(t *testing.T) {
	envs := map[string]string{
		"MYSQL_USER":     "appuser",
		"MYSQL_PASSWORD": "apppass",
		"MYSQL_DATABASE": "appdb",
	}
	creds := extractCredsFromEnvs(envs)
	require.NotNil(t, creds)
	assert.Equal(t, "appuser", creds.Username)
	assert.Equal(t, "apppass", creds.Password)
	assert.Equal(t, "appdb", creds.Database)
}

// TestExtractCreds_MySQLRootPasswordDefaultsToRoot — MYSQL_ROOT_PASSWORD is paired
// with the root account by image convention, so defaulting to "root" remains correct here.
func TestExtractCreds_MySQLRootPasswordDefaultsToRoot(t *testing.T) {
	envs := map[string]string{
		"MYSQL_ROOT_PASSWORD": "rootpass",
	}
	creds := extractCredsFromEnvs(envs)
	require.NotNil(t, creds)
	assert.Equal(t, "root", creds.Username)
	assert.Equal(t, "rootpass", creds.Password)
}
