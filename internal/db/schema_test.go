package db

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// extractSQLFromShellCmd parses the inline `sh -c` script produced by the MySQL
// and PostgreSQL adapters and returns the SQL passed via `-e 'SQL'` or `-c 'SQL'`.
// Mirrors the single-quote escaping rules of shellEscape (' → '\'').
// extractSQLFromShellCmd parses the inline `sh -c` script produced by the MySQL
// and PostgreSQL adapters and returns the SQL passed via `-e 'SQL'` or `-c 'SQL'`.
// Mirrors the single-quote escaping rules of shellEscape: an embedded apostrophe
// is rendered as '"'"' (close quote, double-quoted apostrophe, reopen quote).
func extractSQLFromShellCmd(script string) string {
	for _, flag := range []string{"-e ", "-c "} {
		idx := strings.Index(script, flag)
		if idx < 0 {
			continue
		}
		rest := script[idx+len(flag):]
		if len(rest) == 0 || rest[0] != '\'' {
			continue
		}
		var b strings.Builder
		i := 1
		for i < len(rest) {
			if rest[i] == '\'' {
				// shellEscape encodes ' as '"'"' (5 bytes after the close quote).
				if i+4 < len(rest) && rest[i+1] == '"' && rest[i+2] == '\'' && rest[i+3] == '"' && rest[i+4] == '\'' {
					b.WriteByte('\'')
					i += 5
					continue
				}
				return b.String()
			}
			b.WriteByte(rest[i])
			i++
		}
		return b.String()
	}
	return ""
}

func newMockSchemaInspector(responses map[string]*QueryResult) *schemaInspector {
	callIdx := 0
	order := make([]string, 0, len(responses))
	for k := range responses {
		order = append(order, k)
	}

	mockExec := &k8s.MockPodExecutor{
		ExecInPodFunc: func(ctx context.Context, ns, pod, container string, cmd []string) ([]byte, []byte, error) {
			// Both adapters now wrap in `sh -c "<inline cmd>"`. Extract the SQL by
			// scanning the inline script for the `-e 'SQL'` (mysql) or `-c 'SQL'` (psql)
			// argument; shell-quoted SQL is delimited by surrounding single quotes.
			var sql string
			if len(cmd) >= 3 && cmd[0] == "sh" && cmd[1] == "-c" {
				sql = extractSQLFromShellCmd(cmd[2])
			} else {
				// Legacy direct-exec form (kept for any future adapter that doesn't wrap).
				for i, c := range cmd {
					if c == "-e" || c == "-c" {
						if i+1 < len(cmd) {
							sql = cmd[i+1]
						}
						break
					}
				}
			}

			// Return based on SQL content keywords
			if result, ok := responses[sql]; ok {
				return renderMySQLOutput(result), nil, nil
			}

			// Fallback: return empty
			callIdx++
			return []byte(""), nil, nil
		},
	}

	creds := &sync.Map{}
	creds.Store("default/mysql-0", &Credentials{
		Username: "root",
		Password: "pass",
		Database: "testdb",
	})

	exec := &executor{
		podExec:         mockExec,
		creds:           creds,
		maxRowsPerTable: 100,
	}

	return &schemaInspector{exec: exec}
}

// renderMySQLOutput converts a QueryResult to MySQL tab-separated format.
func renderMySQLOutput(qr *QueryResult) []byte {
	if qr == nil || len(qr.Columns) == 0 {
		return []byte("")
	}

	var lines []string
	lines = append(lines, joinTabs(qr.Columns))
	for _, row := range qr.Rows {
		lines = append(lines, joinTabs(row))
	}
	return []byte(joinLines(lines))
}

func joinTabs(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\t"
		}
		result += p
	}
	return result
}

func joinLines(lines []string) string {
	result := ""
	for _, l := range lines {
		result += l + "\n"
	}
	return result
}

func TestSchema_GetSchema_Tables(t *testing.T) {
	// Prepare responses keyed by SQL
	adapter, _ := AdapterFor(MySQL)
	listTablesSQL := adapter.ListTablesSQL("testdb")
	descUsersSQL := adapter.DescribeTableSQL("testdb", "users")
	descOrdersSQL := adapter.DescribeTableSQL("testdb", "orders")
	fkSQL := adapter.ForeignKeysSQL("testdb")

	responses := map[string]*QueryResult{
		listTablesSQL: {
			Columns: []string{"TABLE_NAME"},
			Rows:    [][]string{{"users"}, {"orders"}},
		},
		descUsersSQL: {
			Columns: []string{"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "COLUMN_KEY", "COLUMN_DEFAULT"},
			Rows: [][]string{
				{"id", "int", "NO", "PRI", "NULL"},
				{"name", "varchar", "YES", "", "NULL"},
				{"email", "varchar", "NO", "", "NULL"},
			},
		},
		descOrdersSQL: {
			Columns: []string{"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "COLUMN_KEY", "COLUMN_DEFAULT"},
			Rows: [][]string{
				{"id", "int", "NO", "PRI", "NULL"},
				{"user_id", "int", "NO", "", "NULL"},
				{"total", "decimal", "YES", "", "0.00"},
			},
		},
		fkSQL: {
			Columns: []string{"TABLE_NAME", "COLUMN_NAME", "REFERENCED_TABLE_NAME", "REFERENCED_COLUMN_NAME"},
			Rows: [][]string{
				{"orders", "user_id", "users", "id"},
			},
		},
	}

	inspector := newMockSchemaInspector(responses)

	tables, fks, err := inspector.GetSchema(context.Background(), "default", "mysql-0", "testdb", MySQL)
	require.NoError(t, err)

	require.Len(t, tables, 2)
	assert.Equal(t, "users", tables[0].Name)
	assert.Equal(t, "orders", tables[1].Name)

	// Check users columns
	require.Len(t, tables[0].Columns, 3)
	assert.Equal(t, "id", tables[0].Columns[0].Name)
	assert.True(t, tables[0].Columns[0].IsPrimary)
	assert.False(t, tables[0].Columns[0].IsNullable)
	assert.Equal(t, "name", tables[0].Columns[1].Name)
	assert.True(t, tables[0].Columns[1].IsNullable)

	// Check orders columns
	require.Len(t, tables[1].Columns, 3)
	assert.Equal(t, "0.00", tables[1].Columns[2].Default)

	// Check foreign keys
	require.Len(t, fks, 1)
	assert.Equal(t, "orders", fks[0].ChildTable)
	assert.Equal(t, "user_id", fks[0].ChildColumn)
	assert.Equal(t, "users", fks[0].ParentTable)
	assert.Equal(t, "id", fks[0].ParentColumn)
}

func TestSchema_CacheHit(t *testing.T) {
	callCount := 0
	adapter, _ := AdapterFor(MySQL)
	listTablesSQL := adapter.ListTablesSQL("testdb")
	fkSQL := adapter.ForeignKeysSQL("testdb")

	responses := map[string]*QueryResult{
		listTablesSQL: {
			Columns: []string{"TABLE_NAME"},
			Rows:    [][]string{},
		},
		fkSQL: {
			Columns: []string{"TABLE_NAME", "COLUMN_NAME", "REFERENCED_TABLE_NAME", "REFERENCED_COLUMN_NAME"},
			Rows:    [][]string{},
		},
	}

	mockExec := &k8s.MockPodExecutor{
		ExecInPodFunc: func(ctx context.Context, ns, pod, container string, cmd []string) ([]byte, []byte, error) {
			callCount++
			var sql string
			for i, c := range cmd {
				if c == "-e" || c == "-c" {
					if i+1 < len(cmd) {
						sql = cmd[i+1]
					}
					break
				}
			}
			if result, ok := responses[sql]; ok {
				return renderMySQLOutput(result), nil, nil
			}
			return []byte(""), nil, nil
		},
	}

	creds := &sync.Map{}
	creds.Store("default/mysql-0", &Credentials{
		Username: "root",
		Password: "pass",
		Database: "testdb",
	})

	exec := &executor{
		podExec:         mockExec,
		creds:           creds,
		maxRowsPerTable: 100,
	}

	inspector := &schemaInspector{exec: exec}

	// First call
	_, _, err := inspector.GetSchema(context.Background(), "default", "mysql-0", "testdb", MySQL)
	require.NoError(t, err)
	firstCallCount := callCount

	// Second call — should hit cache
	_, _, err = inspector.GetSchema(context.Background(), "default", "mysql-0", "testdb", MySQL)
	require.NoError(t, err)

	assert.Equal(t, firstCallCount, callCount, "expected no additional exec calls due to cache")
}

func TestSchema_GetForeignKeys_Filtered(t *testing.T) {
	adapter, _ := AdapterFor(MySQL)
	listTablesSQL := adapter.ListTablesSQL("testdb")
	descUsersSQL := adapter.DescribeTableSQL("testdb", "users")
	descOrdersSQL := adapter.DescribeTableSQL("testdb", "orders")
	descItemsSQL := adapter.DescribeTableSQL("testdb", "items")
	fkSQL := adapter.ForeignKeysSQL("testdb")

	responses := map[string]*QueryResult{
		listTablesSQL: {
			Columns: []string{"TABLE_NAME"},
			Rows:    [][]string{{"users"}, {"orders"}, {"items"}},
		},
		descUsersSQL: {
			Columns: []string{"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "COLUMN_KEY", "COLUMN_DEFAULT"},
			Rows:    [][]string{{"id", "int", "NO", "PRI", "NULL"}},
		},
		descOrdersSQL: {
			Columns: []string{"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "COLUMN_KEY", "COLUMN_DEFAULT"},
			Rows:    [][]string{{"id", "int", "NO", "PRI", "NULL"}, {"user_id", "int", "NO", "", "NULL"}},
		},
		descItemsSQL: {
			Columns: []string{"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "COLUMN_KEY", "COLUMN_DEFAULT"},
			Rows:    [][]string{{"id", "int", "NO", "PRI", "NULL"}, {"order_id", "int", "NO", "", "NULL"}},
		},
		fkSQL: {
			Columns: []string{"TABLE_NAME", "COLUMN_NAME", "REFERENCED_TABLE_NAME", "REFERENCED_COLUMN_NAME"},
			Rows: [][]string{
				{"orders", "user_id", "users", "id"},
				{"items", "order_id", "orders", "id"},
			},
		},
	}

	inspector := newMockSchemaInspector(responses)

	// Get FKs for "orders" — should include both (as child and parent)
	fks, err := inspector.GetForeignKeys(context.Background(), "default", "mysql-0", "testdb", "orders", MySQL)
	require.NoError(t, err)
	assert.Len(t, fks, 2) // orders→users and items→orders

	// Get FKs for "users" — should include orders→users only
	fks, err = inspector.GetForeignKeys(context.Background(), "default", "mysql-0", "testdb", "users", MySQL)
	require.NoError(t, err)
	assert.Len(t, fks, 1)
	assert.Equal(t, "orders", fks[0].ChildTable)
}

func TestExtractTableNames(t *testing.T) {
	result := &QueryResult{
		Columns: []string{"TABLE_NAME"},
		Rows:    [][]string{{"users"}, {"orders"}, {""}},
	}
	names := extractTableNames(result)
	assert.Equal(t, []string{"users", "orders"}, names)
}

func TestExtractTableNames_Nil(t *testing.T) {
	assert.Nil(t, extractTableNames(nil))
	assert.Nil(t, extractTableNames(&QueryResult{}))
}

func TestParseColumns(t *testing.T) {
	result := &QueryResult{
		Columns: []string{"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE", "COLUMN_KEY", "COLUMN_DEFAULT"},
		Rows: [][]string{
			{"id", "int", "NO", "PRI", "NULL"},
			{"name", "varchar", "YES", "", "unnamed"},
		},
	}
	cols := parseColumns(result)
	require.Len(t, cols, 2)

	assert.Equal(t, "id", cols[0].Name)
	assert.Equal(t, "int", cols[0].DataType)
	assert.False(t, cols[0].IsNullable)
	assert.True(t, cols[0].IsPrimary)
	assert.Empty(t, cols[0].Default) // "NULL" → empty

	assert.Equal(t, "name", cols[1].Name)
	assert.True(t, cols[1].IsNullable)
	assert.False(t, cols[1].IsPrimary)
	assert.Equal(t, "unnamed", cols[1].Default)
}

func TestParseForeignKeys(t *testing.T) {
	result := &QueryResult{
		Columns: []string{"TABLE_NAME", "COLUMN_NAME", "REFERENCED_TABLE_NAME", "REFERENCED_COLUMN_NAME"},
		Rows: [][]string{
			{"orders", "user_id", "users", "id"},
		},
	}
	fks := parseForeignKeys(result)
	require.Len(t, fks, 1)
	assert.Equal(t, "orders", fks[0].ChildTable)
	assert.Equal(t, "user_id", fks[0].ChildColumn)
	assert.Equal(t, "users", fks[0].ParentTable)
	assert.Equal(t, "id", fks[0].ParentColumn)
}
