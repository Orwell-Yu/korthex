package db

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sqlFromArgv returns the SQL from a python exec argv (the final positional arg).
func sqlFromArgv(cmd []string) string {
	if len(cmd) == 0 {
		return ""
	}
	return cmd[len(cmd)-1]
}

func newMockSchemaInspector(responses map[string]*QueryResult) *schemaInspector {
	mockExec := &k8s.MockPodExecutor{
		ExecInPodFunc: func(ctx context.Context, ns, pod, container string, cmd []string) ([]byte, []byte, error) {
			sql := sqlFromArgv(cmd)
			if result, ok := responses[sql]; ok {
				return renderJSONOutput(result), nil, nil
			}
			return []byte(""), nil, nil
		},
	}

	creds := &sync.Map{}
	creds.Store(credsKey("default", "mysql-0", "testdb"), &Credentials{
		Username:   "root",
		Password:   "pass",
		Database:   "testdb",
		Host:       "rm-x",
		Port:       3306,
		PythonPath: "/app/server/.venv/bin/python",
	})

	exec := &executor{
		podExec:         mockExec,
		creds:           creds,
		maxRowsPerTable: 100,
		pythonPath:      "/app/server/.venv/bin/python",
	}

	return &schemaInspector{exec: exec}
}

// renderJSONOutput converts a QueryResult to the JSON the in-pod python emits.
func renderJSONOutput(qr *QueryResult) []byte {
	if qr == nil {
		return []byte(`{"columns":[],"rows":[]}`)
	}
	cols := qr.Columns
	if cols == nil {
		cols = []string{}
	}
	rows := qr.Rows
	if rows == nil {
		rows = [][]string{}
	}
	b, _ := json.Marshal(struct {
		Columns []string   `json:"columns"`
		Rows    [][]string `json:"rows"`
	}{cols, rows})
	return b
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
			sql := sqlFromArgv(cmd)
			if result, ok := responses[sql]; ok {
				return renderJSONOutput(result), nil, nil
			}
			return []byte(""), nil, nil
		},
	}

	creds := &sync.Map{}
	creds.Store(credsKey("default", "mysql-0", "testdb"), &Credentials{
		Username:   "root",
		Password:   "pass",
		Database:   "testdb",
		Host:       "rm-x",
		Port:       3306,
		PythonPath: "/app/server/.venv/bin/python",
	})

	exec := &executor{
		podExec:         mockExec,
		creds:           creds,
		maxRowsPerTable: 100,
		pythonPath:      "/app/server/.venv/bin/python",
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
