package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Orwell-Yu/korthex/internal/config"
	"github.com/Orwell-Yu/korthex/internal/k8s"
)

// service implements DatabaseService by delegating to specialized components.
type service struct {
	discoverer  *discoverer
	credFetcher *credentialFetcher
	schemaInsp  *schemaInspector
	exec        *executor
	dbTypes     sync.Map // namespace/podName → DatabaseType (cache for discovered types)
}

// New creates a new DatabaseService wired with all required dependencies.
func New(podExec k8s.PodExecutor, resources k8s.ResourceLister, inspector k8s.PodInspector, cfg config.DatabaseConfig) DatabaseService {
	var timeout time.Duration
	if cfg.Query.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.Query.TimeoutSeconds) * time.Second
	}
	exec := &executor{
		podExec:         podExec,
		creds:           &sync.Map{},
		maxRowsPerTable: cfg.Query.MaxRowsPerTable,
		timeout:         timeout,
	}

	return &service{
		discoverer: &discoverer{
			k8sClient:     resources,
			imagePatterns: cfg.Discovery.ImagePatterns,
		},
		credFetcher: &credentialFetcher{inspector: inspector},
		schemaInsp:  &schemaInspector{exec: exec},
		exec:        exec,
	}
}

func (s *service) Discover(ctx context.Context, namespace string) ([]DatabaseInfo, error) {
	infos, err := s.discoverer.Discover(namespace)
	if err != nil {
		return nil, err
	}
	// Cache discovered types for later use
	for _, info := range infos {
		key := info.Namespace + "/" + info.PodName
		s.dbTypes.Store(key, info.DBType)
	}
	return infos, nil
}

func (s *service) GetCredentials(ctx context.Context, namespace, podName string) (*Credentials, error) {
	creds, err := s.credFetcher.GetCredentials(ctx, namespace, podName)
	if err != nil {
		return nil, err
	}
	// Also store in executor's cache for query use
	s.exec.storeCredentials(namespace, podName, creds)
	return creds, nil
}

func (s *service) GetSchema(ctx context.Context, namespace, podName, database string) ([]TableSchema, []ForeignKey, error) {
	dbType, err := s.resolveDBType(namespace, podName)
	if err != nil {
		return nil, nil, err
	}
	return s.schemaInsp.GetSchema(ctx, namespace, podName, database, dbType)
}

func (s *service) Query(ctx context.Context, namespace, podName, database, sql string, limit int) (*QueryResult, error) {
	dbType, err := s.resolveDBType(namespace, podName)
	if err != nil {
		return nil, err
	}
	return s.exec.Query(ctx, namespace, podName, database, sql, limit, dbType)
}

func (s *service) GetForeignKeys(ctx context.Context, namespace, podName, database, table string) ([]ForeignKey, error) {
	dbType, err := s.resolveDBType(namespace, podName)
	if err != nil {
		return nil, err
	}
	return s.schemaInsp.GetForeignKeys(ctx, namespace, podName, database, table, dbType)
}

// resolveDBType looks up the cached DatabaseType for a pod. The caller must have
// invoked Discover() first so the type is known; we refuse to guess because the
// wrong adapter sends a non-existent CLI to the pod and can corrupt parsing.
func (s *service) resolveDBType(namespace, podName string) (DatabaseType, error) {
	key := namespace + "/" + podName
	if dbType, ok := s.dbTypes.Load(key); ok {
		return dbType.(DatabaseType), nil
	}
	return "", fmt.Errorf("database type unknown for %s/%s; call discover_databases first", namespace, podName)
}
