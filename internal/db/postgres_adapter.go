package db

import (
	"fmt"
	"strings"
)

// PostgresAdapter implements the Adapter interface for PostgreSQL databases.
type PostgresAdapter struct{}

func (a *PostgresAdapter) Type() DatabaseType {
	return PostgreSQL
}

func (a *PostgresAdapter) BuildCommand(sql string, creds Credentials) []string {
	// kubectl exec cannot set env vars, so we wrap in sh -c with PGPASSWORD prefix.
	psqlCmd := fmt.Sprintf("PGPASSWORD=%s psql -U %s -d %s -c %s --no-align --tuples-only=off --field-separator='|'",
		shellEscape(creds.Password),
		shellEscape(creds.Username),
		shellEscape(creds.Database),
		shellEscape(sql),
	)
	return []string{"sh", "-c", psqlCmd}
}

func (a *PostgresAdapter) ParseOutput(raw []byte) (*QueryResult, error) {
	s := sanitizeUTF8(raw)

	if strings.TrimSpace(s) == "" {
		return &QueryResult{
			Columns:  nil,
			Rows:     nil,
			RowCount: 0,
		}, nil
	}

	lines := strings.Split(s, "\n")

	// Filter out empty lines and footer lines
	var dataLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip footer "(N rows)" line
		if isRowCountFooter(trimmed) {
			continue
		}
		dataLines = append(dataLines, line)
	}

	if len(dataLines) == 0 {
		return &QueryResult{
			Columns:  nil,
			Rows:     nil,
			RowCount: 0,
		}, nil
	}

	// First line is column headers
	columns := splitAndTrim(dataLines[0], "|")

	// Find and skip separator line (---+--- pattern)
	startIdx := 1
	if startIdx < len(dataLines) && isSeparatorLine(dataLines[startIdx]) {
		startIdx = 2
	}

	// Remaining lines are data rows
	var rows [][]string
	for _, line := range dataLines[startIdx:] {
		fields := splitAndTrim(line, "|")
		// Convert empty strings (PostgreSQL NULL representation) to "NULL"
		for i, f := range fields {
			if f == "" {
				fields[i] = "NULL"
			}
		}
		rows = append(rows, fields)
	}

	return &QueryResult{
		Columns:  columns,
		Rows:     rows,
		RowCount: len(rows),
	}, nil
}

func (a *PostgresAdapter) ListTablesSQL(database string) string {
	return "SELECT table_name FROM information_schema.tables " +
		"WHERE table_schema = 'public' AND table_type = 'BASE TABLE'"
}

func (a *PostgresAdapter) DescribeTableSQL(database, table string) string {
	t := sanitizeSQLIdentifier(table)
	return fmt.Sprintf(
		"SELECT column_name, data_type, is_nullable, "+
			"CASE WHEN pk.column_name IS NOT NULL THEN 'PRI' ELSE '' END AS column_key, "+
			"column_default "+
			"FROM information_schema.columns c "+
			"LEFT JOIN ("+
			"SELECT ku.column_name FROM information_schema.table_constraints tc "+
			"JOIN information_schema.key_column_usage ku ON tc.constraint_name = ku.constraint_name "+
			"WHERE tc.table_schema = 'public' AND tc.table_name = '%s' AND tc.constraint_type = 'PRIMARY KEY'"+
			") pk ON c.column_name = pk.column_name "+
			"WHERE c.table_schema = 'public' AND c.table_name = '%s' "+
			"ORDER BY c.ordinal_position",
		t, t,
	)
}

func (a *PostgresAdapter) ForeignKeysSQL(database string) string {
	return "SELECT kcu.table_name, kcu.column_name, " +
		"ccu.table_name AS referenced_table_name, ccu.column_name AS referenced_column_name " +
		"FROM information_schema.table_constraints tc " +
		"JOIN information_schema.key_column_usage kcu ON tc.constraint_name = kcu.constraint_name " +
		"JOIN information_schema.constraint_column_usage ccu ON tc.constraint_name = ccu.constraint_name " +
		"WHERE tc.table_schema = 'public' AND tc.constraint_type = 'FOREIGN KEY'"
}

func (a *PostgresAdapter) DetectCLI() []string {
	return []string{"which", "psql"}
}

// isSeparatorLine checks if a line is a psql separator (e.g., "---+---+---").
func isSeparatorLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return false
	}
	for _, c := range trimmed {
		if c != '-' && c != '+' {
			return false
		}
	}
	return true
}

// isRowCountFooter checks if a line is the psql footer like "(5 rows)" or "(0 rows)".
func isRowCountFooter(line string) bool {
	return strings.HasPrefix(line, "(") && strings.HasSuffix(line, "rows)") ||
		strings.HasPrefix(line, "(") && strings.HasSuffix(line, "row)")
}

// splitAndTrim splits a string by separator and trims whitespace from each field.
func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	result := make([]string, len(parts))
	for i, p := range parts {
		result[i] = strings.TrimSpace(p)
	}
	return result
}
