package db

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Adapter abstracts MySQL/PostgreSQL execution differences.
//
// BuildCommand returns the exec argv plus an optional stdin payload. Credentials
// are NEVER placed in the argv: in env-name mode the in-pod python reads the
// connection string from os.environ; in stdin mode the password travels through
// stdin. Both keep secrets out of API Server audit logs and `ps` output.
type Adapter interface {
	BuildCommand(sql string, creds Credentials) (argv []string, stdin []byte)
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

// sanitizeSQLIdentifier rejects identifiers containing characters that could
// enable SQL injection in INFORMATION_SCHEMA queries (single quote, semicolon, comment markers).
func sanitizeSQLIdentifier(name string) string {
	return strings.ReplaceAll(name, "'", "''")
}

func pythonPathOr(creds Credentials) string {
	if creds.PythonPath != "" {
		return creds.PythonPath
	}
	return "/app/server/.venv/bin/python"
}

// buildPythonCommand assembles the exec argv + stdin for an in-pod python script.
//
//   - env-name mode (creds.ConnEnvVar != ""): argv = [py, -c, script, "env", connEnvVar, sql].
//     The script reads os.environ[connEnvVar]. No secret in argv or stdin.
//   - stdin mode (creds.ConnEnvVar == ""): argv = [py, -c, script, "stdin", sql] and stdin
//     carries a JSON object {host,port,user,password,database}. No secret in argv.
//
// The mode token ("env"/"stdin") is argv[3] so the script knows where to get the
// connection details.
func buildPythonCommand(creds Credentials, script, sql string) ([]string, []byte) {
	py := pythonPathOr(creds)
	if creds.ConnEnvVar != "" {
		return []string{py, "-c", script, "env", creds.ConnEnvVar, sql}, nil
	}
	payload, _ := json.Marshal(map[string]any{
		"host":     creds.Host,
		"port":     creds.Port,
		"user":     creds.Username,
		"password": creds.Password,
		"database": creds.Database,
	})
	return []string{py, "-c", script, "stdin", sql}, payload
}

// parseJSONResult decodes the JSON emitted by the in-pod python scripts. All
// cells are strings (python stringifies values; NULL → "NULL"), so this is far
// more robust than separator-based parsing.
func parseJSONResult(raw []byte) (*QueryResult, error) {
	s := strings.TrimSpace(sanitizeUTF8(raw))
	if s == "" {
		return &QueryResult{}, nil
	}
	// stdout should be pure JSON, but be defensive: decode the last non-empty line.
	if i := strings.LastIndexByte(s, '\n'); i != -1 {
		if tail := strings.TrimSpace(s[i+1:]); tail != "" {
			s = tail
		}
	}

	var payload struct {
		Columns []string   `json:"columns"`
		Rows    [][]string `json:"rows"`
	}
	if err := json.Unmarshal([]byte(s), &payload); err != nil {
		preview := s
		if len(preview) > 300 {
			preview = preview[:300]
		}
		return nil, fmt.Errorf("unexpected query output (not JSON): %s", preview)
	}

	return &QueryResult{
		Columns:  payload.Columns,
		Rows:     payload.Rows,
		RowCount: len(payload.Rows),
	}, nil
}

func init() {
	RegisterAdapter(&MySQLAdapter{})
	RegisterAdapter(&PostgresAdapter{})
}
