package db

import (
	"context"
	"time"
)

// DatabaseType identifies the database engine variant.
type DatabaseType string

const (
	MySQL      DatabaseType = "mysql"
	PostgreSQL DatabaseType = "postgresql"
)

// DatabaseInfo describes a discovered database reachable from the cluster.
//
// In the RDS-via-pod model, the database lives outside the cluster (managed RDS)
// and is reached by exec-ing into a running application Pod that holds the
// connection string in its environment. PodName is therefore the *application*
// pod used as an exec entry point, not a database container.
type DatabaseInfo struct {
	ID          string // stable identifier: namespace/podName/database (dedup + lookup key)
	Namespace   string
	PodName     string // application pod used as exec entry point
	ServiceName string // associated Service (optional)
	DBType      DatabaseType
	Port        int
	Database    string
	ConnEnvVar  string // env var on PodName holding the connection string
	Host        string // RDS host (for dedup / display)
	Label       string // display label, e.g. "biz (postgresql)"
	PythonPath  string // usable in-pod python interpreter (probed); "" if none found
	Detected    time.Time
}

// TableSchema describes a database table and its columns.
type TableSchema struct {
	Name    string
	Columns []Column
}

// Column describes a single column in a table.
type Column struct {
	Name       string
	DataType   string
	IsNullable bool
	IsPrimary  bool
	Default    string
}

// ForeignKey describes a foreign key relationship between two tables.
type ForeignKey struct {
	ChildTable   string
	ChildColumn  string
	ParentTable  string
	ParentColumn string
}

// QueryResult holds the structured output of a SQL query execution.
type QueryResult struct {
	Columns   []string
	Rows      [][]string
	RowCount  int
	Truncated bool // true if rows were capped by limit
}

// Credentials holds database connection credentials (in-memory only, never persisted).
type Credentials struct {
	Username string
	Password string
	Database string
	Host     string // RDS host (external), reached from inside the exec pod
	Port     int

	// RDS-via-pod execution context: the SQL is run by invoking PythonPath inside
	// the exec pod with the appropriate Driver, connecting out to Host:Port.
	PythonPath string // e.g. /app/server/.venv/bin/python
	Driver     string // "asyncpg" (PostgreSQL) or "pymysql" (MySQL)

	// ConnEnvVar names the pod env var holding the full connection string. When
	// set, SQL is executed by having the in-pod python read os.environ[ConnEnvVar]
	// itself — the password is NEVER placed in the exec argv. When empty (in-cluster
	// DB fallback), the password is passed via stdin instead.
	ConnEnvVar string
}

// DatabaseService is the top-level DB interface, consumed by agent tools.
type DatabaseService interface {
	Discover(ctx context.Context, namespace string) ([]DatabaseInfo, error)
	GetCredentials(ctx context.Context, namespace, podName string) (*Credentials, error)
	GetSchema(ctx context.Context, namespace, podName, database string) ([]TableSchema, []ForeignKey, error)
	Query(ctx context.Context, namespace, podName, database, sql string, limit int) (*QueryResult, error)
	GetForeignKeys(ctx context.Context, namespace, podName, database, table string) ([]ForeignKey, error)
}
