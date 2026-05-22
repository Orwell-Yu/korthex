package db

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// schemaInspector provides schema introspection via INFORMATION_SCHEMA queries.
type schemaInspector struct {
	exec        *executor
	schemaCache sync.Map // namespace/pod/database → *cachedSchema
}

type cachedSchema struct {
	tables      []TableSchema
	foreignKeys []ForeignKey
	fetchedAt   time.Time
}

// GetSchema returns table schemas and foreign keys for a database, using cache when available.
func (s *schemaInspector) GetSchema(ctx context.Context, namespace, podName, database string, dbType DatabaseType) ([]TableSchema, []ForeignKey, error) {
	key := fmt.Sprintf("%s/%s/%s", namespace, podName, database)
	if cached, ok := s.schemaCache.Load(key); ok {
		cs := cached.(*cachedSchema)
		return cs.tables, cs.foreignKeys, nil
	}

	adapter, ok := AdapterFor(dbType)
	if !ok {
		return nil, nil, fmt.Errorf("unsupported database type: %s", dbType)
	}

	// 1. List tables
	tablesResult, err := s.exec.execSQL(ctx, namespace, podName, database, adapter.ListTablesSQL(database), dbType)
	if err != nil {
		return nil, nil, fmt.Errorf("list tables: %w", err)
	}

	tableNames := extractTableNames(tablesResult)
	if len(tableNames) == 0 {
		empty := &cachedSchema{fetchedAt: time.Now()}
		s.schemaCache.Store(key, empty)
		return nil, nil, nil
	}

	// 2. For each table, get column details
	tables := make([]TableSchema, 0, len(tableNames))
	for _, tableName := range tableNames {
		colResult, err := s.exec.execSQL(ctx, namespace, podName, database, adapter.DescribeTableSQL(database, tableName), dbType)
		if err != nil {
			// Skip table on error, don't fail entire schema fetch
			tables = append(tables, TableSchema{Name: tableName})
			continue
		}
		columns := parseColumns(colResult)
		tables = append(tables, TableSchema{
			Name:    tableName,
			Columns: columns,
		})
	}

	// 3. Get foreign keys
	fkResult, err := s.exec.execSQL(ctx, namespace, podName, database, adapter.ForeignKeysSQL(database), dbType)
	var foreignKeys []ForeignKey
	if err == nil {
		foreignKeys = parseForeignKeys(fkResult)
	}

	// Cache
	s.schemaCache.Store(key, &cachedSchema{
		tables:      tables,
		foreignKeys: foreignKeys,
		fetchedAt:   time.Now(),
	})

	return tables, foreignKeys, nil
}

// GetForeignKeys returns foreign keys for a specific table.
func (s *schemaInspector) GetForeignKeys(ctx context.Context, namespace, podName, database, table string, dbType DatabaseType) ([]ForeignKey, error) {
	_, fks, err := s.GetSchema(ctx, namespace, podName, database, dbType)
	if err != nil {
		return nil, err
	}

	var filtered []ForeignKey
	for _, fk := range fks {
		if fk.ChildTable == table || fk.ParentTable == table {
			filtered = append(filtered, fk)
		}
	}
	return filtered, nil
}

// RefreshSchema clears the schema cache for a given database.
func (s *schemaInspector) RefreshSchema(namespace, podName, database string) {
	key := fmt.Sprintf("%s/%s/%s", namespace, podName, database)
	s.schemaCache.Delete(key)
}

// extractTableNames pulls table names from the first column of the query result.
func extractTableNames(result *QueryResult) []string {
	if result == nil || len(result.Rows) == 0 {
		return nil
	}
	names := make([]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) > 0 && strings.TrimSpace(row[0]) != "" {
			names = append(names, strings.TrimSpace(row[0]))
		}
	}
	return names
}

// parseColumns converts query result rows into Column structs.
// Expected columns: column_name, data_type, is_nullable, column_key, column_default
func parseColumns(result *QueryResult) []Column {
	if result == nil || len(result.Rows) == 0 {
		return nil
	}

	columns := make([]Column, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) < 3 {
			continue
		}
		col := Column{
			Name:     strings.TrimSpace(row[0]),
			DataType: strings.TrimSpace(row[1]),
		}

		// IS_NULLABLE
		nullable := strings.TrimSpace(row[2])
		col.IsNullable = strings.EqualFold(nullable, "YES") || strings.EqualFold(nullable, "true")

		// COLUMN_KEY / is_primary (column index 3)
		if len(row) > 3 {
			keyVal := strings.TrimSpace(row[3])
			col.IsPrimary = strings.EqualFold(keyVal, "PRI")
		}

		// COLUMN_DEFAULT (column index 4)
		if len(row) > 4 {
			def := strings.TrimSpace(row[4])
			if def != "NULL" && def != "" {
				col.Default = def
			}
		}

		columns = append(columns, col)
	}
	return columns
}

// parseForeignKeys converts query result rows into ForeignKey structs.
// Expected columns: table_name, column_name, referenced_table_name, referenced_column_name
func parseForeignKeys(result *QueryResult) []ForeignKey {
	if result == nil || len(result.Rows) == 0 {
		return nil
	}

	var fks []ForeignKey
	for _, row := range result.Rows {
		if len(row) < 4 {
			continue
		}
		fks = append(fks, ForeignKey{
			ChildTable:   strings.TrimSpace(row[0]),
			ChildColumn:  strings.TrimSpace(row[1]),
			ParentTable:  strings.TrimSpace(row[2]),
			ParentColumn: strings.TrimSpace(row[3]),
		})
	}
	return fks
}
