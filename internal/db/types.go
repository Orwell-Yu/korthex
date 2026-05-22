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

// DatabaseInfo describes a discovered database Pod in a K8s cluster.
type DatabaseInfo struct {
	Namespace   string
	PodName     string
	ServiceName string // associated Service (optional)
	DBType      DatabaseType
	Port        int
	Database    string
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
	Host     string // usually localhost (inside Pod)
	Port     int
}

// DatabaseService is the top-level DB interface, consumed by agent tools.
type DatabaseService interface {
	Discover(ctx context.Context, namespace string) ([]DatabaseInfo, error)
	GetCredentials(ctx context.Context, namespace, podName string) (*Credentials, error)
	GetSchema(ctx context.Context, namespace, podName, database string) ([]TableSchema, []ForeignKey, error)
	Query(ctx context.Context, namespace, podName, database, sql string, limit int) (*QueryResult, error)
	GetForeignKeys(ctx context.Context, namespace, podName, database, table string) ([]ForeignKey, error)
}
