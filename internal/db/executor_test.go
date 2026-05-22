package db

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Orwell-Yu/korthex/internal/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- wrapWithLimit tests ---

func TestWrapWithLimit_NoExistingLimit(t *testing.T) {
	got, eff := wrapWithLimit("SELECT * FROM users", 50, 100)
	assert.Equal(t, "SELECT * FROM (SELECT * FROM users) AS _kr_outer LIMIT 50", got)
	assert.Equal(t, 50, eff)
}

func TestWrapWithLimit_UserLimitGreaterThanConfig(t *testing.T) {
	got, eff := wrapWithLimit("SELECT * FROM users", 200, 100)
	assert.Equal(t, "SELECT * FROM (SELECT * FROM users) AS _kr_outer LIMIT 100", got)
	assert.Equal(t, 100, eff)
}

func TestWrapWithLimit_InnerLimitStillBoundedByOuter(t *testing.T) {
	// Inner LIMIT 200 is preserved, but the outer LIMIT 50 caps the final result.
	got, eff := wrapWithLimit("SELECT * FROM users LIMIT 200", 50, 100)
	assert.Equal(t, "SELECT * FROM (SELECT * FROM users LIMIT 200) AS _kr_outer LIMIT 50", got)
	assert.Equal(t, 50, eff)
}

func TestWrapWithLimit_ZeroLimits(t *testing.T) {
	got, eff := wrapWithLimit("SELECT * FROM users", 0, 0)
	assert.Equal(t, "SELECT * FROM users", got)
	assert.Equal(t, 0, eff)
}

func TestWrapWithLimit_OnlyConfigMax(t *testing.T) {
	got, eff := wrapWithLimit("SELECT * FROM users", 0, 100)
	assert.Equal(t, "SELECT * FROM (SELECT * FROM users) AS _kr_outer LIMIT 100", got)
	assert.Equal(t, 100, eff)
}

func TestWrapWithLimit_TrailingSemicolonStripped(t *testing.T) {
	got, _ := wrapWithLimit("SELECT * FROM users;", 50, 100)
	// Trailing ; would break the subquery wrap; the wrapper strips it.
	assert.NotContains(t, got, ";")
	assert.Equal(t, "SELECT * FROM (SELECT * FROM users) AS _kr_outer LIMIT 50", got)
}

// TestWrapWithLimit_CommentBypassResistance — the previous regex-based enforceLIMIT
// could be defeated by a trailing comment (`... LIMIT 9999 -- bypass`) because the
// `$`-anchored regex stopped matching. With subquery wrapping the outer LIMIT
// always applies, regardless of comments in the inner SQL.
func TestWrapWithLimit_CommentBypassResistance(t *testing.T) {
	tricky := []string{
		"SELECT * FROM users LIMIT 9999 -- evil comment",
		"SELECT * FROM users /* LIMIT 9999 */",
		"SELECT * FROM users -- LIMIT 9999\n",
		"SELECT * FROM users LIMIT 9999\n-- trailing",
	}
	for _, sql := range tricky {
		got, eff := wrapWithLimit(sql, 50, 100)
		assert.Contains(t, got, "AS _kr_outer LIMIT 50", "subquery cap must always be 50, got: %s", got)
		assert.Equal(t, 50, eff)
	}
}

// --- executor tests ---

func TestExecutor_Query_CallsExecInPod(t *testing.T) {
	var capturedNs, capturedPod string
	var capturedCmd []string

	mockExec := &k8s.MockPodExecutor{
		ExecInPodFunc: func(ctx context.Context, ns, pod, container string, cmd []string) ([]byte, []byte, error) {
			capturedNs = ns
			capturedPod = pod
			capturedCmd = cmd
			// Return MySQL-style tab-separated output
			return []byte("id\tname\n1\tAlice\n"), nil, nil
		},
	}

	creds := &sync.Map{}
	creds.Store("production/mysql-0", &Credentials{
		Username: "root",
		Password: "secret",
		Database: "testdb",
	})

	exec := &executor{
		podExec:         mockExec,
		creds:           creds,
		maxRowsPerTable: 100,
	}

	result, err := exec.Query(context.Background(), "production", "mysql-0", "testdb", "SELECT id, name FROM users", 50, MySQL)
	require.NoError(t, err)
	assert.Equal(t, "production", capturedNs)
	assert.Equal(t, "mysql-0", capturedPod)
	// MySQL adapter now wraps in `sh -c "...mysql..."`; the mysql binary name lives in cmd[2].
	require.GreaterOrEqual(t, len(capturedCmd), 3)
	assert.Equal(t, []string{"sh", "-c"}, capturedCmd[:2])
	assert.Contains(t, capturedCmd[2], "mysql")
	// Outer wrapWithLimit fence applies the row cap; inner SQL is preserved verbatim.
	assert.Contains(t, capturedCmd[2], "SELECT * FROM (SELECT id, name FROM users) AS _kr_outer LIMIT 50")
	assert.Equal(t, []string{"id", "name"}, result.Columns)
	assert.Equal(t, 1, result.RowCount)
}

func TestExecutor_Query_RejectsDangerousSQL(t *testing.T) {
	mockExec := &k8s.MockPodExecutor{}
	creds := &sync.Map{}
	creds.Store("production/mysql-0", &Credentials{
		Username: "root",
		Password: "secret",
		Database: "testdb",
	})

	exec := &executor{
		podExec:         mockExec,
		creds:           creds,
		maxRowsPerTable: 100,
	}

	_, err := exec.Query(context.Background(), "production", "mysql-0", "testdb", "DROP TABLE users", 50, MySQL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SQL rejected")
}

func TestExecutor_Query_NoCreds(t *testing.T) {
	mockExec := &k8s.MockPodExecutor{}
	exec := &executor{
		podExec:         mockExec,
		creds:           &sync.Map{},
		maxRowsPerTable: 100,
	}

	_, err := exec.Query(context.Background(), "production", "mysql-0", "testdb", "SELECT 1", 50, MySQL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "credentials not found")
}

func TestExecutor_Query_ExecError(t *testing.T) {
	mockExec := &k8s.MockPodExecutor{
		ExecInPodFunc: func(ctx context.Context, ns, pod, container string, cmd []string) ([]byte, []byte, error) {
			return nil, []byte("command not found"), fmt.Errorf("exit code 127")
		},
	}
	creds := &sync.Map{}
	creds.Store("production/mysql-0", &Credentials{
		Username: "root",
		Password: "secret",
		Database: "testdb",
	})

	exec := &executor{
		podExec:         mockExec,
		creds:           creds,
		maxRowsPerTable: 100,
	}

	_, err := exec.Query(context.Background(), "production", "mysql-0", "testdb", "SELECT 1", 50, MySQL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exec failed")
}

func TestExecutor_Query_UnsupportedType(t *testing.T) {
	mockExec := &k8s.MockPodExecutor{}
	creds := &sync.Map{}
	creds.Store("production/mongo-0", &Credentials{
		Username: "root",
		Password: "secret",
		Database: "testdb",
	})

	exec := &executor{
		podExec:         mockExec,
		creds:           creds,
		maxRowsPerTable: 100,
	}

	_, err := exec.Query(context.Background(), "production", "mongo-0", "testdb", "SELECT 1", 50, DatabaseType("mongodb"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported database type")
}
