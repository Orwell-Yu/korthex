package db

import (
	"context"
	"testing"

	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makePod(name string, opts ...func(*k8s.Pod)) k8s.Pod {
	p := k8s.Pod{
		Name:      name,
		Namespace: "default",
		Status:    "Running",
		Labels:    map[string]string{},
	}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

func withImage(image string) func(*k8s.Pod) {
	return func(p *k8s.Pod) {
		if len(p.Containers) == 0 {
			p.Containers = append(p.Containers, k8s.Container{})
		}
		p.Containers[0].Image = image
	}
}

func withPort(port int32) func(*k8s.Pod) {
	return func(p *k8s.Pod) {
		if len(p.Containers) == 0 {
			p.Containers = append(p.Containers, k8s.Container{})
		}
		p.Containers[0].Ports = append(p.Containers[0].Ports, k8s.ContainerPort{
			ContainerPort: port,
			Protocol:      "TCP",
		})
	}
}

func withLabel(key, value string) func(*k8s.Pod) {
	return func(p *k8s.Pod) {
		p.Labels[key] = value
	}
}

func withOwner(kind, name string) func(*k8s.Pod) {
	return func(p *k8s.Pod) {
		p.OwnerKind = kind
		p.OwnerName = name
	}
}

func TestDiscovery_ImageMySQL(t *testing.T) {
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("mysql-0", withImage("mysql:8.0")),
			}, nil
		},
	}

	d := &discoverer{k8sClient: mock}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, MySQL, results[0].DBType)
	assert.Equal(t, "mysql-0", results[0].PodName)
	assert.Equal(t, 3306, results[0].Port)
}

func TestDiscovery_ImagePostgres(t *testing.T) {
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("pg-0", withImage("postgres:15")),
			}, nil
		},
	}

	d := &discoverer{k8sClient: mock}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, PostgreSQL, results[0].DBType)
	assert.Equal(t, 5432, results[0].Port)
}

func TestDiscovery_ImageWithRegistry(t *testing.T) {
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("db-0", withImage("docker.io/library/mysql:8.0")),
			}, nil
		},
	}

	d := &discoverer{k8sClient: mock}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, MySQL, results[0].DBType)
}

func TestDiscovery_PortMySQL(t *testing.T) {
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("custom-db-0", withPort(3306)),
			}, nil
		},
	}

	d := &discoverer{k8sClient: mock}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, MySQL, results[0].DBType)
}

func TestDiscovery_PortPostgreSQL(t *testing.T) {
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("custom-pg-0", withPort(5432)),
			}, nil
		},
	}

	d := &discoverer{k8sClient: mock}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, PostgreSQL, results[0].DBType)
}

func TestDiscovery_LabelKubernetesName(t *testing.T) {
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("pg-0", withLabel("app.kubernetes.io/name", "postgresql")),
			}, nil
		},
	}

	d := &discoverer{k8sClient: mock}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, PostgreSQL, results[0].DBType)
}

func TestDiscovery_LabelApp(t *testing.T) {
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("mysql-0", withLabel("app", "mysql")),
			}, nil
		},
	}

	d := &discoverer{k8sClient: mock}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, MySQL, results[0].DBType)
}

func TestDiscovery_StatefulSetOwner(t *testing.T) {
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("my-mysql-0", withOwner("StatefulSet", "my-mysql")),
			}, nil
		},
	}

	d := &discoverer{k8sClient: mock}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, MySQL, results[0].DBType)
}

func TestDiscovery_NoMatch(t *testing.T) {
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("nginx-0", withImage("nginx:latest")),
				makePod("app-0", withPort(8080)),
			}, nil
		},
	}

	d := &discoverer{k8sClient: mock}
	results, err := d.Discover("default")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestDiscovery_Deduplicated(t *testing.T) {
	// Pod matches both image AND port — should appear only once
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			p := makePod("mysql-0", withImage("mysql:8.0"), withPort(3306))
			return []k8s.Pod{p}, nil
		},
	}

	d := &discoverer{k8sClient: mock}
	results, err := d.Discover("default")
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestDiscovery_CustomImagePattern(t *testing.T) {
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("mydb-0", withImage("company/custom-mysql:v1")),
			}, nil
		},
	}

	d := &discoverer{
		k8sClient:     mock,
		imagePatterns: []string{"custom-mysql"},
	}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, MySQL, results[0].DBType)
}

func TestDiscovery_MultiplePods(t *testing.T) {
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("mysql-0", withImage("mysql:8.0")),
				makePod("pg-0", withImage("postgres:15")),
				makePod("redis-0", withImage("redis:7")),
			}, nil
		},
	}

	d := &discoverer{k8sClient: mock}
	results, err := d.Discover("default")
	require.NoError(t, err)
	assert.Len(t, results, 2) // mysql + postgres, not redis

	types := map[DatabaseType]bool{}
	for _, r := range results {
		types[r.DBType] = true
	}
	assert.True(t, types[MySQL])
	assert.True(t, types[PostgreSQL])
}

func TestDiscovery_ExcludesJobAndInitPods(t *testing.T) {
	mock := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				// Looks like a DB by name, but is a one-shot Job → must be excluded.
				makePod("init-users-database-q7cfx", withImage("mysql:8.0"), withOwner("Job", "init-users-database"), func(p *k8s.Pod) { p.Status = "Succeeded" }),
				// init- prefix, even if Running → excluded.
				makePod("init-biz-database-blvs4", withImage("postgres:15")),
				// migration pod → excluded.
				makePod("migration-runner", withImage("mysql:8.0"), func(p *k8s.Pod) { p.Status = "Succeeded" }),
				// A genuine running DB pod → kept.
				makePod("mysql-0", withImage("mysql:8.0")),
			}, nil
		},
	}

	d := &discoverer{k8sClient: mock}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "mysql-0", results[0].PodName)
}

func TestDiscovery_FromEnv_DedupAndType(t *testing.T) {
	// Two running business pods both reference the same PG database, plus one
	// references a MySQL database. Discovery should dedup the shared PG by
	// (host, database) and detect both engine types from connection-string schemes.
	lister := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("biz-server-1"),
				makePod("biz-server-2"),
				makePod("user-server-1"),
			}, nil
		},
	}
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			switch pod {
			case "biz-server-1", "biz-server-2":
				return map[string][]k8s.EnvVar{
					"app": {{Name: "DATABASE_URL", Value: "postgresql+asyncpg://biz:pw@pgm-x.rds:6432/bizdb"}},
				}, nil
			case "user-server-1":
				return map[string][]k8s.EnvVar{
					"app": {{Name: "USER_MYSQL_URL", Value: "mysql+aiomysql://u:pw@rm-y.rds:3306/usersdb"}},
				}, nil
			}
			return nil, nil
		},
	}

	d := &discoverer{
		k8sClient:   lister,
		inspector:   inspector,
		connEnvVars: []string{"DATABASE_URL", "USER_MYSQL_URL"},
	}
	results, err := d.Discover("default")
	require.NoError(t, err)

	// Expect 2 logical databases: bizdb (PG, deduped across 2 pods) + usersdb (MySQL).
	require.Len(t, results, 2)

	byDB := map[string]DatabaseInfo{}
	for _, r := range results {
		byDB[r.Database] = r
	}
	require.Contains(t, byDB, "bizdb")
	require.Contains(t, byDB, "usersdb")
	assert.Equal(t, PostgreSQL, byDB["bizdb"].DBType)
	assert.Equal(t, 6432, byDB["bizdb"].Port)
	assert.Equal(t, "DATABASE_URL", byDB["bizdb"].ConnEnvVar)
	assert.Equal(t, MySQL, byDB["usersdb"].DBType)
	assert.Equal(t, 3306, byDB["usersdb"].Port)
}

func TestParseConnString(t *testing.T) {
	tests := []struct {
		raw      string
		wantType DatabaseType
		wantHost string
		wantPort int
		wantDB   string
		wantOK   bool
	}{
		{"postgresql+asyncpg://u:p@h:6432/db", PostgreSQL, "h", 6432, "db", true},
		{"mysql+aiomysql://u:p@h:3306/db", MySQL, "h", 3306, "db", true},
		{"postgres://u:p@h/db", PostgreSQL, "h", 5432, "db", true}, // default port
		{"redis://h:6379", "", "", 0, "", false},                   // unsupported
		{"not a url", "", "", 0, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			typ, host, port, db, ok := parseConnString(tt.raw)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantType, typ)
				assert.Equal(t, tt.wantHost, host)
				assert.Equal(t, tt.wantPort, port)
				assert.Equal(t, tt.wantDB, db)
			}
		})
	}
}

func TestNormalizeImageName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"mysql:8.0", "mysql"},
		{"docker.io/library/mysql:8.0", "mysql"},
		{"postgres:15-alpine", "postgres"},
		{"registry.example.com:5000/team/postgres:latest", "postgres"},
		{"postgis/postgis:15-3.3", "postgis"},
		{"mariadb", "mariadb"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeImageName(tt.input))
		})
	}
}

// --- RDS-via-pod: python probe, venv preference, conditional fallback ---

// probeExec returns a MockPodExecutor whose probe result depends on pod name:
// pods in withVenv report a usable venv python; all others report NONE.
func probeExec(withVenv map[string]bool) *k8s.MockPodExecutor {
	return &k8s.MockPodExecutor{
		ExecInPodFunc: func(ctx context.Context, ns, pod, container string, cmd []string) ([]byte, []byte, error) {
			if withVenv[pod] {
				return []byte("USABLE:/app/server/.venv/bin/python\n"), nil, nil
			}
			return []byte("NONE\n"), nil, nil
		},
	}
}

func TestDiscovery_PrefersPodWithVenv(t *testing.T) {
	// Two pods reach the same PG database; only biz-server has a usable python.
	lister := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("worker-1"),     // no venv
				makePod("biz-server-1"), // has venv
			}, nil
		},
	}
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{
				"app": {{Name: "DATABASE_URL", Value: "postgresql+asyncpg://u:p@pgm-x.rds:6432/bizdb"}},
			}, nil
		},
	}
	d := &discoverer{
		k8sClient:   lister,
		inspector:   inspector,
		podExec:     probeExec(map[string]bool{"biz-server-1": true}),
		connEnvVars: []string{"DATABASE_URL"},
	}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "biz-server-1", results[0].PodName, "should pick the pod with a usable python")
	assert.Equal(t, "/app/server/.venv/bin/python", results[0].PythonPath)
}

func TestDiscovery_NoPythonMarksEmptyPath(t *testing.T) {
	lister := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{makePod("muleteam-1")}, nil
		},
	}
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{
				"app": {{Name: "DATABASE_URL", Value: "postgresql://u:p@pgm-x.rds:5432/db"}},
			}, nil
		},
	}
	d := &discoverer{
		k8sClient:   lister,
		inspector:   inspector,
		podExec:     probeExec(map[string]bool{}), // nothing has venv
		connEnvVars: []string{"DATABASE_URL"},
	}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "", results[0].PythonPath, "no usable python → empty path (query will error clearly)")
}

func TestDiscovery_SuffixMatchesPrefixedEnvVar(t *testing.T) {
	// MULETEAM_APP_DATABASE_URL must be recognized via suffix match.
	lister := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{makePod("muleteam-1")}, nil
		},
	}
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{
				"app": {{Name: "MULETEAM_APP_DATABASE_URL", Value: "postgresql://u:p@h:5432/appdb"}},
			}, nil
		},
	}
	d := &discoverer{
		k8sClient:   lister,
		inspector:   inspector,
		podExec:     probeExec(map[string]bool{"muleteam-1": true}),
		connEnvVars: []string{"DATABASE_URL"},
	}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "appdb", results[0].Database)
	assert.Equal(t, "MULETEAM_APP_DATABASE_URL", results[0].ConnEnvVar)
}

func TestDiscovery_FallbackOnlyWhenNoConnStrings(t *testing.T) {
	// A pod that looks like an in-cluster DB by image, but NO connection strings
	// anywhere → fallback should run and detect it.
	lister := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{makePod("mysql-0", withImage("mysql:8.0"))}, nil
		},
	}
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{}, nil // no conn strings
		},
	}
	d := &discoverer{
		k8sClient:   lister,
		inspector:   inspector,
		podExec:     probeExec(map[string]bool{}),
		connEnvVars: []string{"DATABASE_URL"},
	}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, MySQL, results[0].DBType)
}

func TestDiscovery_FallbackSuppressedWhenConnStringFound(t *testing.T) {
	// One pod exposes a connection string AND another looks like an in-cluster DB
	// by image. Because a connection string was found, fallback must NOT run, so the
	// misleading "pg-like" pod is not reported.
	lister := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{
				makePod("app-1"),
				makePod("muleteam-pg-1", withImage("postgres:15")),
			}, nil
		},
	}
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			if pod == "app-1" {
				return map[string][]k8s.EnvVar{
					"app": {{Name: "DATABASE_URL", Value: "postgresql://u:p@rds-x:5432/db"}},
				}, nil
			}
			return map[string][]k8s.EnvVar{}, nil
		},
	}
	d := &discoverer{
		k8sClient:   lister,
		inspector:   inspector,
		podExec:     probeExec(map[string]bool{"app-1": true}),
		connEnvVars: []string{"DATABASE_URL"},
	}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "app-1", results[0].PodName, "fallback must be suppressed; only the conn-string DB is reported")
}

// --- #2 multi-DB per pod, #3 secret-sourced conn string ---

func TestDiscovery_SamePodMultipleDatabases(t *testing.T) {
	// One pod fronts both a PG and a MySQL database via two conn-string env vars.
	// Both must be returned with distinct IDs (no overwrite).
	lister := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{makePod("biz-server-1")}, nil
		},
	}
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{
				"app": {
					{Name: "DATABASE_URL", Value: "postgresql+asyncpg://u:p@pgm-x.rds:6432/bizdb"},
					{Name: "USER_MYSQL_URL", Value: "mysql+aiomysql://u:p@rm-y.rds:3306/usersdb"},
				},
			}, nil
		},
	}
	d := &discoverer{
		k8sClient:   lister,
		inspector:   inspector,
		podExec:     probeExec(map[string]bool{"biz-server-1": true}),
		connEnvVars: []string{"DATABASE_URL", "USER_MYSQL_URL"},
	}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 2, "both databases on the same pod must be returned, not overwritten")

	byDB := map[string]DatabaseInfo{}
	ids := map[string]bool{}
	for _, r := range results {
		byDB[r.Database] = r
		ids[r.ID] = true
	}
	require.Len(t, ids, 2, "IDs must be distinct per database")
	assert.Equal(t, PostgreSQL, byDB["bizdb"].DBType)
	assert.Equal(t, "DATABASE_URL", byDB["bizdb"].ConnEnvVar)
	assert.Equal(t, MySQL, byDB["usersdb"].DBType)
	assert.Equal(t, "USER_MYSQL_URL", byDB["usersdb"].ConnEnvVar)
}

func TestDiscovery_SecretSourcedConnString(t *testing.T) {
	// DATABASE_URL comes from a secretKeyRef (Value empty); discovery must resolve
	// the Secret to derive metadata, and still record the env var for in-pod reads.
	lister := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{makePod("app-1")}, nil
		},
	}
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{
				"app": {{Name: "DATABASE_URL", SecretName: "db-secret", SecretKey: "url"}},
			}, nil
		},
		GetSecretDataFunc: func(ctx context.Context, ns, secret string) (map[string]string, error) {
			return map[string]string{"url": "postgresql+asyncpg://u:p@pgm-z.rds:5432/securedb"}, nil
		},
	}
	d := &discoverer{
		k8sClient:   lister,
		inspector:   inspector,
		podExec:     probeExec(map[string]bool{"app-1": true}),
		connEnvVars: []string{"DATABASE_URL"},
	}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1, "secret-sourced connection string must be discovered")
	assert.Equal(t, "securedb", results[0].Database)
	assert.Equal(t, PostgreSQL, results[0].DBType)
	assert.Equal(t, "DATABASE_URL", results[0].ConnEnvVar)
}

// TestDiscovery_EnvFromSecretRef — connection string injected via
// envFrom[].secretRef (no explicit env var in PodSpec) must still be discovered.
func TestDiscovery_EnvFromSecretRef(t *testing.T) {
	lister := &k8s.MockResourceLister{
		ListPodsFunc: func(ns string) ([]k8s.Pod, error) {
			return []k8s.Pod{makePod("app-1")}, nil
		},
	}
	inspector := &k8s.MockPodInspector{
		GetPodContainerEnvsFunc: func(ns, pod string) (map[string][]k8s.EnvVar, error) {
			return map[string][]k8s.EnvVar{"app": {}}, nil // no direct/secretKeyRef env
		},
		GetPodEnvFromSecretsFunc: func(ns, pod string) (map[string][]k8s.EnvFromSource, error) {
			return map[string][]k8s.EnvFromSource{"app": {{SecretName: "app-env"}}}, nil
		},
		GetSecretDataFunc: func(ctx context.Context, ns, secretName string) (map[string]string, error) {
			return map[string]string{
				"DATABASE_URL": "postgresql+asyncpg://u:p@pgm-w.rds:5432/envfromdb",
				"OTHER_CONFIG": "not-a-db",
			}, nil
		},
	}
	d := &discoverer{
		k8sClient:   lister,
		inspector:   inspector,
		podExec:     probeExec(map[string]bool{"app-1": true}),
		connEnvVars: []string{"DATABASE_URL"},
	}
	results, err := d.Discover("default")
	require.NoError(t, err)
	require.Len(t, results, 1, "envFrom secretRef connection string must be discovered")
	assert.Equal(t, "envfromdb", results[0].Database)
	assert.Equal(t, "DATABASE_URL", results[0].ConnEnvVar)
	assert.Equal(t, PostgreSQL, results[0].DBType)
}
