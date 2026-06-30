package db

import (
	"strings"
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

// --- BuildCommand: secrets never in argv ---

// containsValue reports whether any argv element equals or contains the secret.
func argvLeaks(argv []string, secret string) bool {
	for _, a := range argv {
		if secret != "" && strings.Contains(a, secret) {
			return true
		}
	}
	return false
}

// env-name mode: connection string read from os.environ in-pod; argv carries only
// the env var NAME and the SQL — never the password.
func TestMySQLAdapter_BuildCommand_EnvMode_NoPassword(t *testing.T) {
	a := &MySQLAdapter{}
	creds := Credentials{
		Username:   "root",
		Password:   "p@ss w'or$d",
		Database:   "testdb",
		Host:       "rm-xxx.rds.aliyuncs.com",
		Port:       3306,
		PythonPath: "/app/server/.venv/bin/python",
		ConnEnvVar: "USER_MYSQL_URL",
	}
	sql := "SELECT * FROM users"
	argv, stdin := a.BuildCommand(sql, creds)

	// argv: python -c <script> env <connEnvVar> sql
	require.Len(t, argv, 6)
	assert.Equal(t, "/app/server/.venv/bin/python", argv[0])
	assert.Equal(t, "-c", argv[1])
	assert.Contains(t, argv[2], "pymysql")
	assert.Equal(t, "env", argv[3])
	assert.Equal(t, "USER_MYSQL_URL", argv[4])
	assert.Equal(t, sql, argv[5])
	assert.Nil(t, stdin, "env mode needs no stdin")
	assert.False(t, argvLeaks(argv, creds.Password), "password must NOT appear in argv")
}

func TestPostgresAdapter_BuildCommand_EnvMode_NoPassword(t *testing.T) {
	a := &PostgresAdapter{}
	creds := Credentials{
		Username:   "postgres",
		Password:   "supersecret",
		Database:   "bizdb",
		Host:       "pgm-xxx.pg.rds.aliyuncs.com",
		Port:       6432,
		PythonPath: "/app/server/.venv/bin/python",
		ConnEnvVar: "DATABASE_URL",
	}
	argv, stdin := a.BuildCommand("SELECT 1", creds)

	require.Len(t, argv, 6)
	assert.Contains(t, argv[2], "asyncpg")
	assert.Equal(t, "env", argv[3])
	assert.Equal(t, "DATABASE_URL", argv[4])
	assert.Equal(t, "SELECT 1", argv[5])
	assert.Nil(t, stdin)
	assert.False(t, argvLeaks(argv, creds.Password), "password must NOT appear in argv")
}

// stdin mode (in-cluster fallback, no ConnEnvVar): password travels via stdin JSON,
// still never in argv.
func TestBuildCommand_StdinMode_PasswordInStdinNotArgv(t *testing.T) {
	a := &PostgresAdapter{}
	creds := Credentials{
		Username:   "postgres",
		Password:   "secret-in-stdin",
		Database:   "d",
		Host:       "10.0.0.5",
		Port:       5432,
		PythonPath: "/app/server/.venv/bin/python",
		// ConnEnvVar empty → stdin mode
	}
	argv, stdin := a.BuildCommand("SELECT 1", creds)

	require.Len(t, argv, 5)
	assert.Equal(t, "stdin", argv[3])
	assert.Equal(t, "SELECT 1", argv[4])
	assert.False(t, argvLeaks(argv, creds.Password), "password must NOT appear in argv")
	require.NotNil(t, stdin)
	assert.Contains(t, string(stdin), "secret-in-stdin", "password is delivered via stdin JSON")
	assert.Contains(t, string(stdin), `"host":"10.0.0.5"`)
}

func TestBuildCommand_DefaultPythonPath(t *testing.T) {
	a := &PostgresAdapter{}
	// PythonPath empty → fall back to the documented default.
	argv, _ := a.BuildCommand("SELECT 1", Credentials{ConnEnvVar: "DATABASE_URL"})
	assert.Equal(t, "/app/server/.venv/bin/python", argv[0])
}

// --- ParseOutput: JSON from the in-pod python scripts ---

func TestParseJSONResult_Normal(t *testing.T) {
	a := &MySQLAdapter{}
	raw := []byte(`{"columns":["id","name","email"],"rows":[["1","Alice","a@x.com"],["2","Bob","b@x.com"]]}`)

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "name", "email"}, result.Columns)
	assert.Equal(t, 2, result.RowCount)
	assert.Equal(t, []string{"1", "Alice", "a@x.com"}, result.Rows[0])
}

func TestParseJSONResult_NullValues(t *testing.T) {
	a := &PostgresAdapter{}
	raw := []byte(`{"columns":["id","name","age"],"rows":[["1","NULL","25"],["2","Bob","NULL"]]}`)

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, 2, result.RowCount)
	assert.Equal(t, []string{"1", "NULL", "25"}, result.Rows[0])
	assert.Equal(t, []string{"2", "Bob", "NULL"}, result.Rows[1])
}

func TestParseJSONResult_EmptyResult(t *testing.T) {
	a := &PostgresAdapter{}
	raw := []byte(`{"columns":[],"rows":[]}`)

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, 0, result.RowCount)
	assert.Empty(t, result.Columns)
}

func TestParseJSONResult_CompletelyEmpty(t *testing.T) {
	a := &MySQLAdapter{}
	result, err := a.ParseOutput([]byte(""))
	require.NoError(t, err)
	assert.Equal(t, 0, result.RowCount)
	assert.Nil(t, result.Columns)
}

func TestParseJSONResult_HeaderOnlyNoRows(t *testing.T) {
	a := &PostgresAdapter{}
	raw := []byte(`{"columns":["id","name"],"rows":[]}`)

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "name"}, result.Columns)
	assert.Equal(t, 0, result.RowCount)
}

// TrailingNoise: scripts print pure JSON, but if a warning slips onto an earlier
// line, we decode the last line.
func TestParseJSONResult_TrailingJSONLine(t *testing.T) {
	a := &MySQLAdapter{}
	raw := []byte("some warning to stderr leaked\n{\"columns\":[\"x\"],\"rows\":[[\"1\"]]}")

	result, err := a.ParseOutput(raw)
	require.NoError(t, err)
	assert.Equal(t, 1, result.RowCount)
	assert.Equal(t, []string{"x"}, result.Columns)
}

func TestParseJSONResult_NotJSON_Errors(t *testing.T) {
	a := &PostgresAdapter{}
	// A python traceback (e.g. auth failure) must surface as an error, not a panic.
	_, err := a.ParseOutput([]byte("Traceback (most recent call last):\n  asyncpg.InvalidPasswordError"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not JSON")
}

func TestParseJSONResult_NonUTF8(t *testing.T) {
	a := &MySQLAdapter{}
	// Invalid UTF-8 byte inside a value is sanitized before JSON decode.
	raw := []byte(`{"columns":["name"],"rows":[["Al` + "\xfe" + `ce"]]}`)
	result, err := a.ParseOutput(raw)
	require.NoError(t, err)
	require.Equal(t, 1, result.RowCount)
	assert.Equal(t, "Al?ce", result.Rows[0][0])
}
