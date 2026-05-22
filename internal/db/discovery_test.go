package db

import (
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
