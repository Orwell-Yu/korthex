package db

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MySQLAdapter implements the Adapter interface for MySQL databases.
type MySQLAdapter struct{}

func (a *MySQLAdapter) Type() DatabaseType {
	return MySQL
}

func (a *MySQLAdapter) BuildCommand(sql string, creds Credentials) []string {
	// Wrap in `sh -c` and pass the password through MYSQL_PWD instead of `-p<password>`.
	// Two reasons:
	//   1. `mysql -p<password>` exposes the password to anyone who can `ps aux` in the
	//      pod's PID namespace. MYSQL_PWD is only visible to processes that can read
	//      /proc/<pid>/environ — same blast radius as kubectl exec itself.
	//   2. Without `-p` on the command line, MySQL no longer emits the
	//      "Using a password on the command line interface can be insecure" warning,
	//      so we don't have to scrub that string from stdout in ParseOutput.
	mysqlCmd := fmt.Sprintf("MYSQL_PWD=%s mysql -u %s %s -e %s --batch --raw",
		shellEscape(creds.Password),
		shellEscape(creds.Username),
		shellEscape(creds.Database),
		shellEscape(sql),
	)
	return []string{"sh", "-c", mysqlCmd}
}

func (a *MySQLAdapter) ParseOutput(raw []byte) (*QueryResult, error) {
	// Replace non-UTF-8 bytes
	s := sanitizeUTF8(raw)

	if strings.TrimSpace(s) == "" {
		return &QueryResult{
			Columns:  nil,
			Rows:     nil,
			RowCount: 0,
		}, nil
	}

	lines := strings.Split(s, "\n")

	// Filter only obvious mysql:* error/info prefixes. We deliberately do NOT silence
	// generic "Warning: ..." lines anymore — MYSQL_PWD removes the canonical "Using a
	// password on the command line ..." warning, and any remaining warning lines are
	// signals the operator should see (deprecation, truncation, charset issues, etc.).
	var dataLines []string
	for _, line := range lines {
		if isInfoLine(line) {
			continue
		}
		if line == "" {
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
	columns := strings.Split(dataLines[0], "\t")

	// Remaining lines are data rows
	var rows [][]string
	for _, line := range dataLines[1:] {
		fields := strings.Split(line, "\t")
		// Convert MySQL NULL representation
		for i, f := range fields {
			if f == "\\N" || f == "NULL" {
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

func (a *MySQLAdapter) ListTablesSQL(database string) string {
	return fmt.Sprintf(
		"SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = '%s' AND TABLE_TYPE = 'BASE TABLE'",
		sanitizeSQLIdentifier(database),
	)
}

func (a *MySQLAdapter) DescribeTableSQL(database, table string) string {
	return fmt.Sprintf(
		"SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT "+
			"FROM INFORMATION_SCHEMA.COLUMNS "+
			"WHERE TABLE_SCHEMA = '%s' AND TABLE_NAME = '%s' "+
			"ORDER BY ORDINAL_POSITION",
		sanitizeSQLIdentifier(database), sanitizeSQLIdentifier(table),
	)
}

func (a *MySQLAdapter) ForeignKeysSQL(database string) string {
	return fmt.Sprintf(
		"SELECT kcu.TABLE_NAME, kcu.COLUMN_NAME, kcu.REFERENCED_TABLE_NAME, kcu.REFERENCED_COLUMN_NAME "+
			"FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu "+
			"JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc "+
			"ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME AND kcu.TABLE_SCHEMA = rc.CONSTRAINT_SCHEMA "+
			"WHERE kcu.TABLE_SCHEMA = '%s' AND kcu.REFERENCED_TABLE_NAME IS NOT NULL",
		sanitizeSQLIdentifier(database),
	)
}

func (a *MySQLAdapter) DetectCLI() []string {
	return []string{"which", "mysql"}
}

// isInfoLine returns true if the line is a mysql CLI error/status banner that
// should be skipped from result parsing (lines prefixed with "mysql:"). Generic
// "Warning:" lines are intentionally kept so operators see real warnings.
func isInfoLine(line string) bool {
	return strings.HasPrefix(strings.ToLower(line), "mysql:")
}

// sanitizeUTF8 replaces non-UTF-8 bytes with "?".
func sanitizeUTF8(raw []byte) string {
	if utf8.Valid(raw) {
		return string(raw)
	}
	var b strings.Builder
	b.Grow(len(raw))
	for len(raw) > 0 {
		r, size := utf8.DecodeRune(raw)
		if r == utf8.RuneError && size == 1 {
			b.WriteByte('?')
		} else {
			b.WriteRune(r)
		}
		raw = raw[size:]
	}
	return b.String()
}
