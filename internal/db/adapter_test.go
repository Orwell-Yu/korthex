package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Adapter Registry Tests ---

func TestAdapterFor_MySQL(t *testing.T) {
	a, ok := AdapterFor(MySQL)
	require.True(t, ok)
	assert.Equal(t, MySQL, a.Type())
}

func TestAdapterFor_PostgreSQL(t *testing.T) {
	a, ok := AdapterFor(PostgreSQL)
	require.True(t, ok)
	assert.Equal(t, PostgreSQL, a.Type())
}

func TestAdapterFor_Unknown(t *testing.T) {
	_, ok := AdapterFor("mongodb")
	assert.False(t, ok)
}

// --- MySQL Adapter Tests ---

func TestMySQLAdapter_BuildCommand(t *testing.T) {
	a := &MySQLAdapter{}
	creds := Credentials{
		Username: "root",
		Password: "secret",
		Database: "testdb",
	}

	cmd := a.BuildCommand("SELECT * FROM users", creds)
	assert.Equal(t, []string{"sh", "-c"}, cmd[:2])
	assert.Contains(t, cmd[2], "MYSQL_PWD='secret'")
	assert.Contains(t, cmd[2], "mysql -u 'root' 'testdb'")
	assert.Contains(t, cmd[2], "-e 'SELECT * FROM users'")
	assert.Contains(t, cmd[2], "--batch")
	assert.Contains(t, cmd[2], "--raw")
	// Password must not leak onto the command line as `-p<password>`.
	assert.NotContains(t, cmd[2], "-psecret")
	assert.NotContains(t, cmd[2], "-p'secret'")
}

// TestMySQLAdapter_BuildCommand_PasswordWithSpecialChars — the password is
// shell-escaped so special characters (spaces, quotes, $) don't break the
// `sh -c` wrapper or leak as separate tokens.
func TestMySQLAdapter_BuildCommand_PasswordWithSpecialChars(t *testing.T) {
	a := &MySQLAdapter{}
	creds := Credentials{
		Username: "root",
		Password: "p@ss w'or$d",
		Database: "testdb",
	}
	cmd := a.BuildCommand("SELECT 1", creds)
	// shellEscape encodes an embedded apostrophe as '"'"' (close quote, "'", reopen quote).
	assert.Contains(t, cmd[2], `MYSQL_PWD='p@ss w'"'"'or$d'`)
}

func TestMySQLAdapter_ParseOutput_Normal(t *testing.T) {
	a := &MySQLAdapter{}
	// Tab-separated output with 3 columns, 5 rows
	raw := []byte("id\tname\temail\n1\tAlice\talice@example.com\n2\tBob\tbob@example.com\n3\tCharlie\tcharlie@example.com\n4\tDave\tdave@example.com\n5\tEve\teve@example.com\n")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)

	assert.Equal(t, []string{"id", "name", "email"}, result.Columns)
	assert.Equal(t, 5, result.RowCount)
	assert.Equal(t, []string{"1", "Alice", "alice@example.com"}, result.Rows[0])
	assert.Equal(t, []string{"5", "Eve", "eve@example.com"}, result.Rows[4])
}

func TestMySQLAdapter_ParseOutput_Empty(t *testing.T) {
	a := &MySQLAdapter{}
	// Header only, no data rows
	raw := []byte("id\tname\temail\n")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)

	assert.Equal(t, []string{"id", "name", "email"}, result.Columns)
	assert.Equal(t, 0, result.RowCount)
	assert.Nil(t, result.Rows)
}

func TestMySQLAdapter_ParseOutput_NullValues(t *testing.T) {
	a := &MySQLAdapter{}
	raw := []byte("id\tname\tage\n1\tAlice\t\\N\n2\t\\N\t25\n")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)

	assert.Equal(t, 2, result.RowCount)
	assert.Equal(t, []string{"1", "Alice", "NULL"}, result.Rows[0])
	assert.Equal(t, []string{"2", "NULL", "25"}, result.Rows[1])
}

func TestMySQLAdapter_ParseOutput_WarningsKept(t *testing.T) {
	a := &MySQLAdapter{}
	// Generic "Warning:" lines are no longer silenced (operator should see deprecation
	// notices etc.). The first non-empty line is now treated as the header row.
	raw := []byte("Warning: deprecated feature X\nid\tname\n1\tAlice\n2\tBob\n")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)

	// Header is the warning line because we no longer skip it; downstream consumers
	// should see real warnings and decide how to surface them.
	assert.Equal(t, []string{"Warning: deprecated feature X"}, result.Columns)
}

func TestMySQLAdapter_ParseOutput_MysqlPrefixedInfoSkipped(t *testing.T) {
	a := &MySQLAdapter{}
	// Lines beginning with "mysql:" (e.g., mysql client status banners) are still skipped.
	raw := []byte("mysql: [Note] connecting to mysqld\nid\tname\n1\tAlice\n")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "name"}, result.Columns)
	assert.Equal(t, 1, result.RowCount)
}

func TestMySQLAdapter_ParseOutput_CompletelyEmpty(t *testing.T) {
	a := &MySQLAdapter{}
	raw := []byte("")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, 0, result.RowCount)
	assert.Nil(t, result.Columns)
}

func TestMySQLAdapter_ParseOutput_NonUTF8(t *testing.T) {
	a := &MySQLAdapter{}
	// Include invalid UTF-8 byte
	raw := []byte("id\tname\n1\tAl\xfece\n")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, 1, result.RowCount)
	assert.Equal(t, "Al?ce", result.Rows[0][1]) // invalid byte replaced with ?
}

// --- PostgreSQL Adapter Tests ---

func TestPostgresAdapter_BuildCommand(t *testing.T) {
	a := &PostgresAdapter{}
	creds := Credentials{
		Username: "postgres",
		Password: "secret",
		Database: "testdb",
	}

	cmd := a.BuildCommand("SELECT * FROM users", creds)
	// PostgreSQL adapter wraps in sh -c with PGPASSWORD prefix
	assert.Equal(t, []string{"sh", "-c"}, cmd[:2])
	assert.Contains(t, cmd[2], "PGPASSWORD='secret'")
	assert.Contains(t, cmd[2], "psql -U 'postgres' -d 'testdb'")
	assert.Contains(t, cmd[2], "-c 'SELECT * FROM users'")
	assert.Contains(t, cmd[2], "--no-align")
	assert.Contains(t, cmd[2], "--field-separator=")
}

func TestPostgresAdapter_ParseOutput_Normal(t *testing.T) {
	a := &PostgresAdapter{}
	// Pipe-separated output with separator line and footer
	raw := []byte("id|name|email\n---+---+---\n1|Alice|alice@example.com\n2|Bob|bob@example.com\n3|Charlie|charlie@example.com\n4|Dave|dave@example.com\n5|Eve|eve@example.com\n(5 rows)\n")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)

	assert.Equal(t, []string{"id", "name", "email"}, result.Columns)
	assert.Equal(t, 5, result.RowCount)
	assert.Equal(t, []string{"1", "Alice", "alice@example.com"}, result.Rows[0])
	assert.Equal(t, []string{"5", "Eve", "eve@example.com"}, result.Rows[4])
}

func TestPostgresAdapter_ParseOutput_SeparatorSkipped(t *testing.T) {
	a := &PostgresAdapter{}
	raw := []byte("col1|col2\n----+----\nval1|val2\n(1 row)\n")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)

	assert.Equal(t, []string{"col1", "col2"}, result.Columns)
	assert.Equal(t, 1, result.RowCount)
	assert.Equal(t, []string{"val1", "val2"}, result.Rows[0])
}

func TestPostgresAdapter_ParseOutput_FooterSkipped(t *testing.T) {
	a := &PostgresAdapter{}
	raw := []byte("id|name\n--+--\n1|Alice\n2|Bob\n(2 rows)\n")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)

	assert.Equal(t, 2, result.RowCount)
}

func TestPostgresAdapter_ParseOutput_Empty(t *testing.T) {
	a := &PostgresAdapter{}
	raw := []byte("")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, 0, result.RowCount)
	assert.Nil(t, result.Columns)
}

func TestPostgresAdapter_ParseOutput_NullValues(t *testing.T) {
	a := &PostgresAdapter{}
	// PostgreSQL represents NULL as empty string in unaligned output
	raw := []byte("id|name|age\n--+--+--\n1||25\n2|Bob|\n(2 rows)\n")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)

	assert.Equal(t, 2, result.RowCount)
	assert.Equal(t, []string{"1", "NULL", "25"}, result.Rows[0])
	assert.Equal(t, []string{"2", "Bob", "NULL"}, result.Rows[1])
}

func TestPostgresAdapter_ParseOutput_HeaderOnlyNoRows(t *testing.T) {
	a := &PostgresAdapter{}
	raw := []byte("id|name|email\n--+--+--\n(0 rows)\n")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)

	assert.Equal(t, []string{"id", "name", "email"}, result.Columns)
	assert.Equal(t, 0, result.RowCount)
	assert.Nil(t, result.Rows)
}
