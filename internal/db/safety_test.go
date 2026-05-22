package db

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckSQL_AllowedStatements(t *testing.T) {
	// Statements valid for both MySQL and PostgreSQL
	bothTests := []struct {
		name string
		sql  string
	}{
		{"simple select", "SELECT * FROM users WHERE id = 1"},
		{"join select", "SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id"},
		{"explain", "EXPLAIN SELECT * FROM users"},
		{"CTE with", "WITH cte AS (SELECT * FROM users) SELECT * FROM cte"},
		{"quoted apostrophe", "SELECT * FROM users WHERE name = 'O''Brien'"},
		{"comment inside", "SELECT * FROM users /* just a comment */"},
	}

	for _, tt := range bothTests {
		t.Run(tt.name+"_mysql", func(t *testing.T) {
			err := CheckSQL(MySQL, tt.sql)
			assert.NoError(t, err, "MySQL should allow: %s", tt.sql)
		})
		t.Run(tt.name+"_postgres", func(t *testing.T) {
			err := CheckSQL(PostgreSQL, tt.sql)
			assert.NoError(t, err, "PostgreSQL should allow: %s", tt.sql)
		})
	}

	// MySQL-specific allowed statements
	mysqlTests := []struct {
		name string
		sql  string
	}{
		{"show tables", "SHOW TABLES"},
		{"describe", "DESCRIBE users"},
	}

	for _, tt := range mysqlTests {
		t.Run(tt.name+"_mysql", func(t *testing.T) {
			err := CheckSQL(MySQL, tt.sql)
			assert.NoError(t, err, "MySQL should allow: %s", tt.sql)
		})
	}

	// PostgreSQL-specific allowed statements
	pgTests := []struct {
		name string
		sql  string
	}{
		{"show setting", "SHOW server_version"},
	}

	for _, tt := range pgTests {
		t.Run(tt.name+"_postgres", func(t *testing.T) {
			err := CheckSQL(PostgreSQL, tt.sql)
			assert.NoError(t, err, "PostgreSQL should allow: %s", tt.sql)
		})
	}
}

func TestCheckSQL_KeywordRejection(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		keyword string
	}{
		{"insert", "INSERT INTO users (name) VALUES ('test')", "INSERT"},
		{"update", "UPDATE users SET name = 'test'", "UPDATE"},
		{"delete", "DELETE FROM users WHERE id = 1", "DELETE"},
		{"drop table", "DROP TABLE users", "DROP"},
		{"alter table", "ALTER TABLE users ADD COLUMN age INT", "ALTER"},
		{"truncate", "TRUNCATE TABLE users", "TRUNCATE"},
		{"create table", "CREATE TABLE test (id INT)", "CREATE"},
		{"grant", "GRANT ALL ON users TO public", "GRANT"},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_mysql", func(t *testing.T) {
			err := CheckSQL(MySQL, tt.sql)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.keyword)
			assert.Contains(t, err.Error(), "not allowed")
		})
		t.Run(tt.name+"_postgres", func(t *testing.T) {
			err := CheckSQL(PostgreSQL, tt.sql)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.keyword)
			assert.Contains(t, err.Error(), "not allowed")
		})
	}
}

func TestCheckSQL_MultiStatementRejection(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{"select then drop", "SELECT 1; DROP TABLE users"},
		{"select then insert", "SELECT 1; INSERT INTO users VALUES (1)"},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_mysql", func(t *testing.T) {
			err := CheckSQL(MySQL, tt.sql)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "multiple statements")
		})
		t.Run(tt.name+"_postgres", func(t *testing.T) {
			err := CheckSQL(PostgreSQL, tt.sql)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "multiple statements")
		})
	}
}

func TestCheckSQL_ASTCatchesEdgeCases(t *testing.T) {
	// Multi-statement with SELECT first — keyword layer catches semicolons
	tests := []struct {
		name string
		sql  string
	}{
		{"select then delete", "SELECT * FROM users; DELETE FROM users"},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_mysql", func(t *testing.T) {
			err := CheckSQL(MySQL, tt.sql)
			assert.Error(t, err)
		})
		t.Run(tt.name+"_postgres", func(t *testing.T) {
			err := CheckSQL(PostgreSQL, tt.sql)
			assert.Error(t, err)
		})
	}
}

func TestCheckSQL_SemicolonInsideQuotes_Allowed(t *testing.T) {
	// Semicolons inside quotes should NOT trigger rejection
	sql := "SELECT * FROM users WHERE name = 'test;value'"
	assert.NoError(t, CheckSQL(MySQL, sql))
	assert.NoError(t, CheckSQL(PostgreSQL, sql))
}

func TestCheckSQL_LeadingComments(t *testing.T) {
	// Leading comments should be stripped before keyword check
	sql := "-- some comment\nSELECT * FROM users"
	assert.NoError(t, CheckSQL(MySQL, sql))
	assert.NoError(t, CheckSQL(PostgreSQL, sql))

	sql2 := "/* block comment */ SELECT * FROM users"
	assert.NoError(t, CheckSQL(MySQL, sql2))
	assert.NoError(t, CheckSQL(PostgreSQL, sql2))
}

func TestCheckSQL_EmptyInput(t *testing.T) {
	err := CheckSQL(MySQL, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty statement")
}

func TestCheckKeywords_OnlyLayer1(t *testing.T) {
	// Test keyword layer independently
	assert.NoError(t, checkKeywords("SELECT 1"))
	assert.NoError(t, checkKeywords("SHOW TABLES"))
	assert.NoError(t, checkKeywords("DESCRIBE users"))
	assert.NoError(t, checkKeywords("EXPLAIN SELECT 1"))
	assert.NoError(t, checkKeywords("WITH x AS (SELECT 1) SELECT * FROM x"))

	assert.Error(t, checkKeywords("INSERT INTO t VALUES (1)"))
	assert.Error(t, checkKeywords("UPDATE t SET x = 1"))
	assert.Error(t, checkKeywords("DELETE FROM t"))
	assert.Error(t, checkKeywords("DROP TABLE t"))
	assert.Error(t, checkKeywords("REVOKE ALL ON t FROM u"))
}

// TestCheckSQL_PostgresWriteCTERejection — Layer 1 keyword check only inspects the first
// token, so `WITH ... (DELETE/INSERT/UPDATE ...) SELECT ...` slips through. Layer 2's AST
// walker must recursively reject any CTE whose body is not a SELECT.
func TestCheckSQL_PostgresWriteCTERejection(t *testing.T) {
	writeCTEs := []struct {
		name string
		sql  string
	}{
		{"delete cte", "WITH d AS (DELETE FROM users RETURNING *) SELECT * FROM d"},
		{"insert cte", "WITH i AS (INSERT INTO users (name) VALUES ('x') RETURNING id) SELECT * FROM i"},
		{"update cte", "WITH u AS (UPDATE users SET name = 'x' RETURNING id) SELECT * FROM u"},
		{
			"nested delete cte",
			"WITH outer_cte AS (WITH inner_cte AS (DELETE FROM users RETURNING id) SELECT * FROM inner_cte) SELECT * FROM outer_cte",
		},
	}
	for _, tt := range writeCTEs {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckSQL(PostgreSQL, tt.sql)
			require.Error(t, err, "should reject write CTE: %s", tt.sql)
			assert.Contains(t, err.Error(), "CTE")
		})
	}
}

// TestCheckSQL_PostgresReadOnlyCTEsAccepted — pure-SELECT CTEs (including nested) must still pass.
func TestCheckSQL_PostgresReadOnlyCTEsAccepted(t *testing.T) {
	cases := []string{
		"WITH r AS (SELECT id FROM users) SELECT * FROM r",
		"WITH a AS (SELECT 1), b AS (SELECT 2) SELECT * FROM a JOIN b ON TRUE",
		"WITH outer_cte AS (WITH inner_cte AS (SELECT id FROM users) SELECT * FROM inner_cte) SELECT * FROM outer_cte",
	}
	for _, sql := range cases {
		assert.NoError(t, CheckSQL(PostgreSQL, sql), "should allow read-only CTE: %s", sql)
	}
}

// TestCheckSQL_MySQLWriteCTERejection — xwb1989/sqlparser does not understand WITH,
// so it returns an error and the AST layer falls back to a defense-in-depth keyword
// scan. The fallback must catch INSERT/UPDATE/DELETE/DROP keywords appearing
// anywhere in the statement (not just as the first token).
func TestCheckSQL_MySQLWriteCTERejection(t *testing.T) {
	dangerous := []struct {
		name string
		sql  string
	}{
		{"with delete", "WITH d AS (SELECT id FROM users) DELETE FROM users WHERE id IN (SELECT id FROM d)"},
		{"with insert", "WITH d AS (SELECT id FROM users) INSERT INTO archive SELECT * FROM d"},
		{"with update", "WITH d AS (SELECT id FROM users) UPDATE users SET name = 'x' WHERE id IN (SELECT id FROM d)"},
		{"with drop", "WITH d AS (SELECT id FROM users) DROP TABLE d"},
	}
	for _, tt := range dangerous {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckSQL(MySQL, tt.sql)
			require.Error(t, err, "MySQL fallback must reject: %s", tt.sql)
		})
	}
}

// TestCheckSQL_MySQLCommentedKeywordsIgnored — DELETE inside a comment or string
// must NOT trigger the fallback's keyword scan, because we strip comments/strings.
func TestCheckSQL_MySQLCommentedKeywordsIgnored(t *testing.T) {
	safe := []string{
		"WITH r AS (SELECT id FROM users) SELECT /* DELETE */ * FROM r",
		"WITH r AS (SELECT id FROM users) SELECT * FROM r WHERE name = 'DROP TABLE x'",
		"WITH r AS (SELECT id /* INSERT INTO ... */ FROM users) SELECT * FROM r",
	}
	for _, sql := range safe {
		assert.NoError(t, CheckSQL(MySQL, sql), "MySQL should accept (kw only in comment/string): %s", sql)
	}
}

// TestScrubCommentsAndStrings_RemovesWriteKeywords — direct unit test for the
// helper that powers the MySQL fallback.
func TestScrubCommentsAndStrings_RemovesWriteKeywords(t *testing.T) {
	cases := []struct {
		in       string
		wantSafe bool // true → scrubbed output should NOT contain "DELETE"
	}{
		{"SELECT 1 -- DELETE FROM t", true},
		{"SELECT 1 /* DELETE FROM t */", true},
		{"SELECT 'DELETE FROM t'", true},
		{"SELECT \"DELETE\" FROM t", true},  // double-quoted identifier
		{"SELECT `DELETE` FROM t", true},    // backtick identifier
		{"SELECT 'O''Brien' FROM users", true},
		{"DELETE FROM users", false}, // bare keyword survives
	}
	for _, tt := range cases {
		got := strings.ToUpper(scrubCommentsAndStrings(tt.in))
		hasDel := containsWord(got, "DELETE")
		if tt.wantSafe {
			assert.False(t, hasDel, "scrub should have removed DELETE in: %q (got %q)", tt.in, got)
		} else {
			assert.True(t, hasDel, "scrub should preserve bare DELETE in: %q (got %q)", tt.in, got)
		}
	}
}
