package db

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCredentialsFor_EnvNameMode_NoPlaintextStillExecutable verifies that when the
// connection string is secret-sourced and the plaintext cannot be resolved
// Go-side, env-name mode still produces an executable credential (ConnEnvVar set,
// no password needed) — the in-pod python reads os.environ at query time.
func TestCredentialsFor_EnvNameMode_NoPlaintextStillExecutable(t *testing.T) {
	inspector := &k8s.MockPodInspector{
		// conn env var exists as secretKeyRef, but the Secret is unreadable here.
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{
				"app": {{Name: "DATABASE_URL", SecretName: "locked", SecretKey: "url"}},
			}, nil
		},
		GetSecretDataFunc: func(ctx context.Context, ns, secretName string) (map[string]string, error) {
			return nil, fmt.Errorf("forbidden") // cannot read the secret
		},
	}
	execCreds := &sync.Map{}
	svc := &service{
		credFetcher: &credentialFetcher{inspector: inspector},
		exec:        &executor{creds: execCreds, pythonPath: "/app/server/.venv/bin/python"},
		pythonPath:  "/app/server/.venv/bin/python",
	}

	info := &DatabaseInfo{
		ID: dbID("default", "app-1", "bizdb"), Namespace: "default", PodName: "app-1",
		DBType: PostgreSQL, Host: "pgm-z.rds", Port: 6432, Database: "bizdb",
		ConnEnvVar: "DATABASE_URL", PythonPath: "/app/server/.venv/bin/python",
	}

	creds, err := svc.credentialsFor(context.Background(), "default", "app-1", info)
	require.NoError(t, err, "env-name mode must not fail just because plaintext is unavailable")
	assert.Equal(t, "DATABASE_URL", creds.ConnEnvVar, "ConnEnvVar drives in-pod env read")
	assert.Equal(t, "asyncpg", creds.Driver)
	assert.Equal(t, "bizdb", creds.Database)
	assert.Empty(t, creds.Password, "no plaintext password is needed in env-name mode")

	// And it is cached under (ns, pod, database) so Query/GetSchema find it.
	cached, err := svc.exec.getCachedCredentials("default", "app-1", "bizdb")
	require.NoError(t, err)
	assert.Equal(t, "DATABASE_URL", cached.ConnEnvVar)
}
