package db

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Orwell-Yu/korthex/internal/k8s"
)

// executor orchestrates the kubectl exec SQL pipeline.
type executor struct {
	podExec         k8s.PodExecutor
	creds           *sync.Map // namespace/podName → *Credentials
	maxRowsPerTable int
	timeout         time.Duration // per-query timeout (0 = no per-query timeout)
	pythonPath      string        // python interpreter inside the exec pod
}

// execSQL executes a raw SQL statement via kubectl exec (no safety check).
func (e *executor) execSQL(ctx context.Context, namespace, podName, database, sql string, dbType DatabaseType) (*QueryResult, error) {
	adapter, ok := AdapterFor(dbType)
	if !ok {
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}

	creds, err := e.getCachedCredentials(namespace, podName, database)
	if err != nil {
		return nil, fmt.Errorf("no credentials for %s/%s (db %q): %w", namespace, podName, database, err)
	}

	// Override database if specified
	execCreds := *creds
	if database != "" {
		execCreds.Database = database
	}
	// Ensure execution context is set even if creds came from an older cache entry.
	if execCreds.PythonPath == "" {
		execCreds.PythonPath = e.pythonPath
	}
	if execCreds.Driver == "" {
		execCreds.Driver = driverFor(dbType)
	}

	cmd, stdin := adapter.BuildCommand(sql, execCreds)

	if e.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}

	// Secrets travel via stdin (in-cluster fallback) or via the pod's own env
	// (RDS-via-pod) — never in the argv. Use the stdin-capable exec when needed.
	stdout, stderr, err := e.podExec.ExecInPodWithStdin(ctx, namespace, podName, "", cmd, stdin)
	if err != nil {
		return nil, fmt.Errorf("exec failed: %w (stderr: %s)", err, string(stderr))
	}

	result, err := adapter.ParseOutput(stdout)
	if err != nil {
		return nil, fmt.Errorf("parse output: %w", err)
	}

	return result, nil
}

// Query executes a SQL statement with safety checks and LIMIT enforcement.
func (e *executor) Query(ctx context.Context, namespace, podName, database, sql string, limit int, dbType DatabaseType) (*QueryResult, error) {
	if err := CheckSQL(dbType, sql); err != nil {
		return nil, err
	}

	wrapped, effectiveLimit := wrapWithLimit(sql, limit, e.maxRowsPerTable)

	result, err := e.execSQL(ctx, namespace, podName, database, wrapped, dbType)
	if err != nil {
		return nil, err
	}

	if effectiveLimit > 0 && result.RowCount >= effectiveLimit {
		result.Truncated = true
	}

	return result, nil
}

// getCachedCredentials retrieves credentials from the cache. Keyed by
// (namespace, podName, database) because one pod can front multiple databases
// with different connection strings.
func (e *executor) getCachedCredentials(namespace, podName, database string) (*Credentials, error) {
	if cached, ok := e.creds.Load(credsKey(namespace, podName, database)); ok {
		return cached.(*Credentials), nil
	}
	return nil, fmt.Errorf("credentials not found")
}

// storeCredentials stores credentials in the cache keyed by (ns, pod, database).
func (e *executor) storeCredentials(namespace, podName, database string, creds *Credentials) {
	e.creds.Store(credsKey(namespace, podName, database), creds)
}

func credsKey(namespace, podName, database string) string {
	return namespace + "/" + podName + "/" + database
}

// wrapWithLimit guarantees the executed SQL returns at most effectiveLimit rows.
//
// Why a subquery fence instead of regex-replacing LIMIT N:
//   - SQL comments (`-- ...`, `/* ... */`) anywhere in the statement can hide an existing
//     LIMIT from a tail-anchored regex, and the parser can be fooled by a comment that
//     mentions "LIMIT 9999" while the real query has no limit at all.
//   - PostgreSQL allows `LIMIT ALL`, `OFFSET ... FETCH FIRST ... ROWS ONLY`, and
//     trailing semicolons. MySQL allows `LIMIT a, b` and `LIMIT a OFFSET b`. Trying to
//     normalize all variants in regex is fragile.
//
// Wrapping in `SELECT * FROM (<sql>) AS _kr_outer LIMIT N` defers the bound to the
// engine, which always honors the outer LIMIT regardless of inner syntax. The inner
// statement's own LIMIT/OFFSET still applies, so users can paginate inside; we only
// guarantee an absolute upper bound.
//
// Trailing semicolons / whitespace are stripped because they would make the subquery
// syntactically invalid; multi-statement SQL has already been rejected by safety.go.
//
// Returns the (possibly wrapped) SQL and the effective row cap (0 = no cap).
func wrapWithLimit(sql string, userLimit, configMax int) (string, int) {
	if userLimit <= 0 && configMax <= 0 {
		return sql, 0
	}

	effective := configMax
	if userLimit > 0 && (effective <= 0 || userLimit < effective) {
		effective = userLimit
	}
	if effective <= 0 {
		return sql, 0
	}

	trimmed := strings.TrimRight(sql, " \t\n\r;")
	wrapped := fmt.Sprintf("SELECT * FROM (%s) AS _kr_outer LIMIT %d", trimmed, effective)
	return wrapped, effective
}
