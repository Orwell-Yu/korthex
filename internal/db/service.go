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
	pythonPath  string
	discovered  sync.Map // namespace/podName → *DatabaseInfo (from Discover)
}

// New creates a new DatabaseService wired with all required dependencies.
func New(podExec k8s.PodExecutor, resources k8s.ResourceLister, inspector k8s.PodInspector, cfg config.DatabaseConfig) DatabaseService {
	var timeout time.Duration
	if cfg.Query.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.Query.TimeoutSeconds) * time.Second
	}
	pythonPath := cfg.PythonPath
	if pythonPath == "" {
		pythonPath = "/app/server/.venv/bin/python"
	}
	connEnvVars := cfg.ConnStringEnvVars
	if len(connEnvVars) == 0 {
		connEnvVars = []string{"DATABASE_URL", "USER_MYSQL_URL", "MYSQL_URL", "POSTGRES_URL"}
	}
	exec := &executor{
		podExec:         podExec,
		creds:           &sync.Map{},
		maxRowsPerTable: cfg.Query.MaxRowsPerTable,
		timeout:         timeout,
		pythonPath:      pythonPath,
	}

	return &service{
		discoverer: &discoverer{
			k8sClient:     resources,
			inspector:     inspector,
			podExec:       podExec,
			connEnvVars:   connEnvVars,
			imagePatterns: cfg.Discovery.ImagePatterns,
		},
		credFetcher: &credentialFetcher{inspector: inspector},
		schemaInsp:  &schemaInspector{exec: exec},
		exec:        exec,
		pythonPath:  pythonPath,
	}
}

func (s *service) Discover(ctx context.Context, namespace string) ([]DatabaseInfo, error) {
	infos, err := s.discoverer.Discover(namespace)
	if err != nil {
		return nil, err
	}
	// Cache full discovery records keyed by stable ID (namespace/podName/database)
	// so one pod fronting multiple databases keeps a record per database instead of
	// the last one clobbering the rest.
	for i := range infos {
		info := infos[i]
		s.discovered.Store(info.ID, &info)
	}
	return infos, nil
}

func (s *service) GetCredentials(ctx context.Context, namespace, podName string) (*Credentials, error) {
	// No database specified: resolve the (single) database for this pod. If the pod
	// fronts several, callers should go through Query/GetSchema with an explicit
	// database; here we pick the first match for the display-only credentials tool.
	info, _ := s.resolveInfo(namespace, podName, "")
	return s.credentialsFor(ctx, namespace, podName, info)
}

// credentialsFor builds and caches credentials for a specific discovered database.
func (s *service) credentialsFor(ctx context.Context, namespace, podName string, info *DatabaseInfo) (*Credentials, error) {
	var creds *Credentials
	var err error
	if info != nil && info.ConnEnvVar != "" {
		// RDS-via-pod: parse the connection string from the pod's environment.
		creds, err = s.credFetcher.GetCredentialsFromConnString(ctx, namespace, podName, info.ConnEnvVar)
		if err != nil {
			// env-name mode does NOT need the password Go-side: the in-pod python
			// reads os.environ[ConnEnvVar] at query time (K8s materializes secret-
			// sourced env into the container). So when we can't resolve the plaintext
			// (e.g. the Secret isn't readable from here), fall back to a no-password
			// execution credential carrying just the env var name + known metadata.
			creds = &Credentials{
				Host:     info.Host,
				Port:     info.Port,
				Database: info.Database,
			}
		}
	} else {
		// In-cluster DB pod fallback.
		creds, err = s.credFetcher.GetCredentials(ctx, namespace, podName)
		if err != nil {
			return nil, err
		}
	}

	// Annotate execution context (python interpreter + driver) for the executor.
	enriched := *creds
	if info != nil && info.PythonPath != "" {
		enriched.PythonPath = info.PythonPath
	} else {
		enriched.PythonPath = s.pythonPath
	}
	database := enriched.Database
	if info != nil {
		enriched.Driver = driverFor(info.DBType)
		// env-name mode: in-pod python reads the connection string from this env
		// var, so the password never enters the exec argv.
		enriched.ConnEnvVar = info.ConnEnvVar
		if info.Database != "" {
			database = info.Database
		}
	} else {
		enriched.Driver = driverFor(detectTypeFromPort(enriched.Port))
	}

	s.exec.storeCredentials(namespace, podName, database, &enriched)
	return &enriched, nil
}

func (s *service) GetSchema(ctx context.Context, namespace, podName, database string) ([]TableSchema, []ForeignKey, error) {
	dbType, err := s.resolveDBType(namespace, podName, database)
	if err != nil {
		return nil, nil, err
	}
	if err := s.ensureCredentials(ctx, namespace, podName, database); err != nil {
		return nil, nil, err
	}
	return s.schemaInsp.GetSchema(ctx, namespace, podName, database, dbType)
}

func (s *service) Query(ctx context.Context, namespace, podName, database, sql string, limit int) (*QueryResult, error) {
	dbType, err := s.resolveDBType(namespace, podName, database)
	if err != nil {
		return nil, err
	}
	if err := s.ensureCredentials(ctx, namespace, podName, database); err != nil {
		return nil, err
	}
	return s.exec.Query(ctx, namespace, podName, database, sql, limit, dbType)
}

func (s *service) GetForeignKeys(ctx context.Context, namespace, podName, database, table string) ([]ForeignKey, error) {
	dbType, err := s.resolveDBType(namespace, podName, database)
	if err != nil {
		return nil, err
	}
	if err := s.ensureCredentials(ctx, namespace, podName, database); err != nil {
		return nil, err
	}
	return s.schemaInsp.GetForeignKeys(ctx, namespace, podName, database, table, dbType)
}

// ensureCredentials fetches and caches credentials for a (pod, database) if not
// already done, so schema/query calls work even when the agent skips
// get_db_credentials.
func (s *service) ensureCredentials(ctx context.Context, namespace, podName, database string) error {
	if _, err := s.exec.getCachedCredentials(namespace, podName, database); err == nil {
		return nil
	}
	info, _ := s.resolveInfo(namespace, podName, database)
	_, err := s.credentialsFor(ctx, namespace, podName, info)
	return err
}

// resolveInfo returns the cached DatabaseInfo for (pod, database). When database
// is empty, it returns the first record found for that pod (and namespace).
func (s *service) resolveInfo(namespace, podName, database string) (*DatabaseInfo, bool) {
	if database != "" {
		if v, ok := s.discovered.Load(dbID(namespace, podName, database)); ok {
			return v.(*DatabaseInfo), true
		}
	}
	// Fallback: scan for any record matching this pod (database unknown/empty).
	var found *DatabaseInfo
	s.discovered.Range(func(_, v any) bool {
		info := v.(*DatabaseInfo)
		if info.Namespace == namespace && info.PodName == podName {
			if database == "" || info.Database == database {
				found = info
				return false
			}
		}
		return true
	})
	return found, found != nil
}

// resolveDBType looks up the cached DatabaseType for (pod, database). The caller
// must have invoked Discover() first; we refuse to guess because the wrong adapter
// would send a non-existent driver to the pod.
func (s *service) resolveDBType(namespace, podName, database string) (DatabaseType, error) {
	if info, ok := s.resolveInfo(namespace, podName, database); ok {
		return info.DBType, nil
	}
	return "", fmt.Errorf("database unknown for %s/%s (db %q); call discover_databases first", namespace, podName, database)
}

// dbID builds the stable discovery key.
func dbID(namespace, podName, database string) string {
	return namespace + "/" + podName + "/" + database
}

// driverFor maps a database type to the python driver used inside the exec pod.
func driverFor(dbType DatabaseType) string {
	switch dbType {
	case PostgreSQL:
		return "asyncpg"
	case MySQL:
		return "pymysql"
	default:
		return ""
	}
}

// detectTypeFromPort is a last-resort guess for the in-cluster fallback path.
func detectTypeFromPort(port int) DatabaseType {
	if port == 5432 {
		return PostgreSQL
	}
	return MySQL
}
