package db

import "strings"

// Adapter abstracts MySQL/PostgreSQL CLI differences.
type Adapter interface {
	BuildCommand(sql string, creds Credentials) []string
	ParseOutput(raw []byte) (*QueryResult, error)
	ListTablesSQL(database string) string
	DescribeTableSQL(database, table string) string
	ForeignKeysSQL(database string) string
	DetectCLI() []string
	Type() DatabaseType
}

var adapters = map[DatabaseType]Adapter{}

// RegisterAdapter registers a database adapter by its type.
func RegisterAdapter(a Adapter) {
	adapters[a.Type()] = a
}

// AdapterFor returns the adapter for the given database type.
func AdapterFor(dbType DatabaseType) (Adapter, bool) {
	a, ok := adapters[dbType]
	return a, ok
}

// shellEscape wraps a string in single quotes for safe shell interpolation.
// Internal single quotes are escaped as '"'"' (end quote, literal quote, start quote).
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// sanitizeSQLIdentifier rejects identifiers containing characters that could
// enable SQL injection in INFORMATION_SCHEMA queries (single quote, semicolon, comment markers).
func sanitizeSQLIdentifier(name string) string {
	return strings.ReplaceAll(name, "'", "''")
}

func init() {
	RegisterAdapter(&MySQLAdapter{})
	RegisterAdapter(&PostgresAdapter{})
}
