package db

import (
	"fmt"
	"strings"
	"unicode"

	pganalyze "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
	"github.com/xwb1989/sqlparser"
)

// CheckSQL validates that a SQL statement is read-only.
// Returns nil if safe, error with reason if rejected.
func CheckSQL(dbType DatabaseType, sql string) error {
	// Layer 1: Keyword whitelist (<1ms)
	if err := checkKeywords(sql); err != nil {
		return err
	}
	// Layer 2: AST parser (<10ms)
	return checkAST(dbType, sql)
}

// Allowed first keywords (read-only statements).
var allowedKeywords = map[string]bool{
	"SELECT":   true,
	"SHOW":     true,
	"DESCRIBE": true,
	"EXPLAIN":  true,
	"WITH":     true,
}

// Blocked first keywords (write/DDL/DCL statements).
var blockedKeywords = map[string]bool{
	"INSERT":   true,
	"UPDATE":   true,
	"DELETE":   true,
	"DROP":     true,
	"ALTER":    true,
	"TRUNCATE": true,
	"CREATE":   true,
	"GRANT":    true,
	"REVOKE":   true,
}

// checkKeywords performs Layer 1 keyword-based validation.
func checkKeywords(sql string) error {
	normalized := stripLeadingComments(sql)
	normalized = strings.TrimSpace(normalized)

	if normalized == "" {
		return fmt.Errorf("SQL rejected: empty statement")
	}

	// Check for multi-statement (unquoted semicolons)
	if hasUnquotedSemicolon(normalized) {
		return fmt.Errorf("SQL rejected: multiple statements are not allowed")
	}

	// Extract first keyword
	firstWord := extractFirstKeyword(normalized)
	upper := strings.ToUpper(firstWord)

	if blockedKeywords[upper] {
		return fmt.Errorf("SQL rejected: statement type '%s' is not allowed (read-only mode)", upper)
	}

	if !allowedKeywords[upper] {
		return fmt.Errorf("SQL rejected: statement type '%s' is not recognized as read-only", upper)
	}

	return nil
}

// checkAST performs Layer 2 AST-based validation.
func checkAST(dbType DatabaseType, sql string) error {
	switch dbType {
	case MySQL:
		return checkASTMySQL(sql)
	case PostgreSQL:
		return checkASTPostgreSQL(sql)
	default:
		return fmt.Errorf("SQL rejected: unsupported database type '%s'", dbType)
	}
}

// checkASTMySQL uses xwb1989/sqlparser to verify the SQL is a SELECT-type statement.
// xwb1989/sqlparser predates MySQL 8 CTE (WITH), so it returns a parse error on
// `WITH ... SELECT ...`. We cannot blindly accept on parse failure — an adversarial
// statement could trip the parser too — so the fallback path does a defense-in-depth
// keyword scan on the comment-stripped, string-literal-stripped SQL to ensure no
// write keyword survives anywhere in the statement (not just position 0).
func checkASTMySQL(sql string) error {
	stmt, err := sqlparser.Parse(sql)
	if err == nil {
		switch stmt.(type) {
		case *sqlparser.Select, *sqlparser.Show, *sqlparser.Union, *sqlparser.ParenSelect, *sqlparser.OtherRead:
			// OtherRead covers EXPLAIN, DESCRIBE
			return nil
		default:
			return fmt.Errorf("SQL rejected: AST analysis detected non-SELECT statement")
		}
	}

	// Fallback path: parser doesn't support this grammar (commonly WITH).
	// Only accept statements whose first keyword is in our known-safe set.
	normalized := strings.TrimSpace(stripLeadingComments(sql))
	firstWord := strings.ToUpper(extractFirstKeyword(normalized))
	if firstWord != "WITH" && firstWord != "EXPLAIN" && firstWord != "DESCRIBE" {
		return fmt.Errorf("SQL rejected: failed to parse MySQL statement: %w", err)
	}

	// Strip every comment and quoted string, then scan the remainder for write keywords.
	// This catches things like `WITH cte AS (DELETE FROM t RETURNING ...)` that
	// xwb1989/sqlparser cannot inspect structurally.
	scrubbed := scrubCommentsAndStrings(sql)
	upper := strings.ToUpper(scrubbed)
	for kw := range blockedKeywords {
		if containsWord(upper, kw) {
			return fmt.Errorf("SQL rejected: AST fallback found forbidden keyword '%s' in MySQL statement (parser does not support this grammar; refusing to take the risk)", kw)
		}
	}
	return nil
}

// checkASTPostgreSQL uses wasilibs/go-pgquery to verify the SQL is a SELECT-type statement.
// CTEs (WITH clauses) are recursively walked to ensure every inner statement is also a SELECT.
// Without this, the parser accepts `WITH x AS (DELETE FROM t RETURNING *) SELECT * FROM x`,
// which is a real write disguised as a SELECT.
func checkASTPostgreSQL(sql string) error {
	tree, err := pgquery.Parse(sql)
	if err != nil {
		return fmt.Errorf("SQL rejected: failed to parse PostgreSQL statement: %w", err)
	}

	stmts := tree.GetStmts()
	if len(stmts) == 0 {
		return fmt.Errorf("SQL rejected: no statements found")
	}

	if len(stmts) > 1 {
		return fmt.Errorf("SQL rejected: AST analysis detected multiple statements")
	}

	stmt := stmts[0].GetStmt()

	if sel := stmt.GetSelectStmt(); sel != nil {
		return checkPGSelectStmtReadOnly(sel)
	}
	if stmt.GetExplainStmt() != nil || stmt.GetVariableShowStmt() != nil {
		return nil
	}

	return fmt.Errorf("SQL rejected: AST analysis detected non-SELECT statement")
}

// checkPGSelectStmtReadOnly verifies a SelectStmt — including its WithClause CTEs —
// is purely read-only. Each CTE's body must itself be a SelectStmt (recursively).
func checkPGSelectStmtReadOnly(sel *pganalyze.SelectStmt) error {
	with := sel.GetWithClause()
	if with == nil {
		return nil
	}
	for _, cteNode := range with.GetCtes() {
		cte := cteNode.GetCommonTableExpr()
		if cte == nil {
			return fmt.Errorf("SQL rejected: AST analysis found a non-CTE node inside WITH clause")
		}
		body := cte.GetCtequery()
		if body == nil {
			return fmt.Errorf("SQL rejected: AST analysis found an empty CTE '%s'", cte.GetCtename())
		}
		inner := body.GetSelectStmt()
		if inner == nil {
			return fmt.Errorf("SQL rejected: CTE '%s' is not a SELECT (write CTEs like INSERT/UPDATE/DELETE are not allowed)", cte.GetCtename())
		}
		// Recurse — a CTE's SELECT may itself have nested CTEs.
		if err := checkPGSelectStmtReadOnly(inner); err != nil {
			return err
		}
	}
	return nil
}

// stripLeadingComments removes leading SQL comments (-- and /* */).
func stripLeadingComments(sql string) string {
	s := strings.TrimSpace(sql)

	for {
		if strings.HasPrefix(s, "--") {
			// Skip until end of line
			idx := strings.IndexByte(s, '\n')
			if idx == -1 {
				return ""
			}
			s = strings.TrimSpace(s[idx+1:])
		} else if strings.HasPrefix(s, "/*") {
			// Skip until */
			idx := strings.Index(s, "*/")
			if idx == -1 {
				return ""
			}
			s = strings.TrimSpace(s[idx+2:])
		} else {
			break
		}
	}

	return s
}

// extractFirstKeyword extracts the first word from a SQL string.
func extractFirstKeyword(sql string) string {
	var b strings.Builder
	for _, r := range sql {
		if unicode.IsLetter(r) || r == '_' {
			b.WriteRune(r)
		} else {
			break
		}
	}
	return b.String()
}

// hasUnquotedSemicolon checks if there's a semicolon outside of quoted strings.
func hasUnquotedSemicolon(sql string) bool {
	inSingleQuote := false
	inDoubleQuote := false
	i := 0

	for i < len(sql) {
		ch := sql[i]

		if ch == '\'' && !inDoubleQuote {
			if inSingleQuote {
				// Check for escaped quote ('')
				if i+1 < len(sql) && sql[i+1] == '\'' {
					i += 2
					continue
				}
				inSingleQuote = false
			} else {
				inSingleQuote = true
			}
		} else if ch == '"' && !inSingleQuote {
			if inDoubleQuote {
				// Check for escaped quote ("")
				if i+1 < len(sql) && sql[i+1] == '"' {
					i += 2
					continue
				}
				inDoubleQuote = false
			} else {
				inDoubleQuote = true
			}
		} else if ch == ';' && !inSingleQuote && !inDoubleQuote {
			return true
		}

		i++
	}

	return false
}

// scrubCommentsAndStrings replaces SQL comments (-- ..., /* ... */) and quoted
// string contents with spaces, preserving overall offsets so a later word-boundary
// keyword scan does not get fooled by `'DELETE'` inside a string literal or
// `-- DROP TABLE` inside a comment.
func scrubCommentsAndStrings(sql string) string {
	out := make([]byte, len(sql))
	for i := range out {
		out[i] = sql[i]
	}
	i := 0
	for i < len(sql) {
		// Line comment
		if i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-' {
			for i < len(sql) && sql[i] != '\n' {
				out[i] = ' '
				i++
			}
			continue
		}
		// Block comment
		if i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*' {
			end := strings.Index(sql[i+2:], "*/")
			if end == -1 {
				for j := i; j < len(sql); j++ {
					out[j] = ' '
				}
				return string(out)
			}
			for j := i; j < i+2+end+2; j++ {
				out[j] = ' '
			}
			i += 2 + end + 2
			continue
		}
		// Single-quoted string
		if sql[i] == '\'' {
			out[i] = ' '
			i++
			for i < len(sql) {
				if sql[i] == '\'' {
					// Escaped ''
					if i+1 < len(sql) && sql[i+1] == '\'' {
						out[i] = ' '
						out[i+1] = ' '
						i += 2
						continue
					}
					out[i] = ' '
					i++
					break
				}
				// Backslash escape inside string
				if sql[i] == '\\' && i+1 < len(sql) {
					out[i] = ' '
					out[i+1] = ' '
					i += 2
					continue
				}
				out[i] = ' '
				i++
			}
			continue
		}
		// Double-quoted identifier or string — for write-keyword scanning, treat as opaque
		if sql[i] == '"' {
			out[i] = ' '
			i++
			for i < len(sql) {
				if sql[i] == '"' {
					if i+1 < len(sql) && sql[i+1] == '"' {
						out[i] = ' '
						out[i+1] = ' '
						i += 2
						continue
					}
					out[i] = ' '
					i++
					break
				}
				out[i] = ' '
				i++
			}
			continue
		}
		// Backtick identifier
		if sql[i] == '`' {
			out[i] = ' '
			i++
			for i < len(sql) && sql[i] != '`' {
				out[i] = ' '
				i++
			}
			if i < len(sql) {
				out[i] = ' '
				i++
			}
			continue
		}
		i++
	}
	return string(out)
}

// containsWord reports whether keyword (already upper-case) appears as a
// whole word in scrubbed (also upper-case). Word boundary = non-letter,
// non-digit, non-underscore.
func containsWord(scrubbed, keyword string) bool {
	idx := 0
	for {
		pos := strings.Index(scrubbed[idx:], keyword)
		if pos < 0 {
			return false
		}
		start := idx + pos
		end := start + len(keyword)
		before := byte(' ')
		if start > 0 {
			before = scrubbed[start-1]
		}
		after := byte(' ')
		if end < len(scrubbed) {
			after = scrubbed[end]
		}
		if !isIdentChar(before) && !isIdentChar(after) {
			return true
		}
		idx = end
	}
}

func isIdentChar(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_'
}
