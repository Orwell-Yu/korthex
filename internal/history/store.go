package history

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Store provides conversation history persistence.
type Store interface {
	CreateSession(ctx context.Context, cluster string) (sessionID string, err error)
	EndSession(ctx context.Context, sessionID string, summary string) error
	AddMessage(ctx context.Context, sessionID string, msg Message) error
	LoadSession(ctx context.Context, sessionID string) ([]Message, error)
	Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)
	CleanExpired(ctx context.Context, retentionDays int) error
	Close() error
}

// Message represents a single conversation message.
type Message struct {
	Role      string // user | assistant | tool_call | tool_result
	Content   string
	Namespace string
	CreatedAt time.Time
}

// SearchQuery defines parameters for history search.
type SearchQuery struct {
	Keyword   string
	Namespace string
	After     time.Time
	Limit     int
}

// SearchResult contains a matching session and its preview messages.
type SearchResult struct {
	SessionID string
	Cluster   string
	StartedAt time.Time
	Summary   string
	Messages  []Message
}

// Session represents a conversation session row.
type Session struct {
	ID        string
	Cluster   string
	StartedAt time.Time
	EndedAt   *time.Time
	Summary   string
}

// compressThreshold is the character limit above which tool results are compressed.
const compressThreshold = 2000

// compressPreview is the number of characters to keep as a preview.
const compressPreview = 500

// CompressToolResult truncates long tool results for storage.
func CompressToolResult(content string) string {
	if len(content) <= compressThreshold {
		return content
	}
	lines := strings.Count(content, "\n")
	return fmt.Sprintf("%s\n\n[compressed: %d chars, %d lines truncated]",
		content[:compressPreview], len(content), lines)
}

// sqliteStore implements Store using modernc.org/sqlite.
type sqliteStore struct {
	db *sql.DB
}

// NewStore opens (or creates) the SQLite database at dbPath and ensures the schema.
func NewStore(dbPath string) (Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create history dir %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open history db: %w", err)
	}

	// Limit connections to 1 for SQLite (serialized access).
	db.SetMaxOpenConns(1)

	if err := ensureSchema(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ensure schema: %w", err)
	}

	return &sqliteStore{db: db}, nil
}

// NewMemoryStore creates an in-memory Store for testing.
func NewMemoryStore() (Store, error) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := ensureSchema(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqliteStore{db: db}, nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("create tables: %w", err)
	}

	// FTS5 virtual table: try to create, ignore "already exists".
	if _, err := db.ExecContext(ctx, ftsSQL); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("create fts table: %w", err)
		}
		// Table exists — check if it uses the trigram tokenizer.
		// If not (legacy), rebuild it for CJK support.
		if needsFTSMigration(ctx, db) {
			slog.Info("migrating FTS5 table to trigram tokenizer for CJK support")
			if err := migrateFTS(ctx, db); err != nil {
				return fmt.Errorf("migrate fts to trigram: %w", err)
			}
		}
	}

	if _, err := db.ExecContext(ctx, triggerSQL); err != nil {
		return fmt.Errorf("create triggers: %w", err)
	}

	return nil
}

// needsFTSMigration checks if the existing FTS5 table lacks the trigram tokenizer.
func needsFTSMigration(ctx context.Context, db *sql.DB) bool {
	var ddl string
	err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='messages_fts'`).Scan(&ddl)
	if err != nil {
		return false
	}
	return !strings.Contains(strings.ToLower(ddl), "trigram")
}

// migrateFTS drops the old FTS table and recreates it with trigram tokenizer,
// then re-populates from existing messages.
func migrateFTS(ctx context.Context, db *sql.DB) error {
	// Drop triggers first so the FTS drop doesn't cascade errors.
	_, _ = db.ExecContext(ctx, `DROP TRIGGER IF EXISTS messages_ai`) //nolint:errcheck // best-effort
	_, _ = db.ExecContext(ctx, `DROP TRIGGER IF EXISTS messages_ad`) //nolint:errcheck // best-effort

	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS messages_fts`); err != nil {
		return fmt.Errorf("drop old fts: %w", err)
	}
	if _, err := db.ExecContext(ctx, ftsSQL); err != nil {
		return fmt.Errorf("create new fts: %w", err)
	}

	// Re-populate FTS from existing messages.
	if _, err := db.ExecContext(ctx, `INSERT INTO messages_fts(rowid, content, namespace)
		SELECT id, content, COALESCE(namespace, '') FROM messages`); err != nil {
		return fmt.Errorf("repopulate fts: %w", err)
	}
	return nil
}

func (s *sqliteStore) CreateSession(ctx context.Context, cluster string) (string, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, cluster, started_at) VALUES (?, ?, ?)`,
		id, cluster, now.Format(time.RFC3339))
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	slog.Debug("history session created", "id", id, "cluster", cluster)
	return id, nil
}

func (s *sqliteStore) EndSession(ctx context.Context, sessionID string, summary string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET ended_at = ?, summary = ? WHERE id = ?`,
		now.Format(time.RFC3339), summary, sessionID)
	if err != nil {
		return fmt.Errorf("end session %s: %w", sessionID, err)
	}
	return nil
}

func (s *sqliteStore) AddMessage(ctx context.Context, sessionID string, msg Message) error {
	content := msg.Content
	if msg.Role == "tool_result" {
		content = CompressToolResult(content)
	}

	createdAt := msg.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (session_id, role, content, namespace, created_at) VALUES (?, ?, ?, ?, ?)`,
		sessionID, msg.Role, content, msg.Namespace, createdAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("add message: %w", err)
	}
	return nil
}

func (s *sqliteStore) LoadSession(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT role, content, COALESCE(namespace, ''), created_at
		 FROM messages WHERE session_id = ? ORDER BY created_at ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session %s: %w", sessionID, err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var role, content, ns, createdStr string
		if err := rows.Scan(&role, &content, &ns, &createdStr); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		createdAt, _ := time.Parse(time.RFC3339, createdStr)
		msgs = append(msgs, Message{
			Role:      role,
			Content:   content,
			Namespace: ns,
			CreatedAt: createdAt,
		})
	}
	return msgs, rows.Err()
}

func (s *sqliteStore) CleanExpired(ctx context.Context, retentionDays int) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format(time.RFC3339)

	// Delete FTS entries for expired messages first (before message DELETE trigger fires).
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM messages_fts WHERE rowid IN (
			SELECT m.id FROM messages m
			JOIN sessions s ON m.session_id = s.id
			WHERE s.started_at < ?
		)`, cutoff)
	if err != nil {
		return fmt.Errorf("clean expired fts: %w", err)
	}

	// Delete messages for expired sessions.
	// The DELETE trigger will try to remove from FTS again but the rows are already gone,
	// so we temporarily drop and recreate the trigger.
	_, _ = s.db.ExecContext(ctx, `DROP TRIGGER IF EXISTS messages_ad`)

	_, err = s.db.ExecContext(ctx,
		`DELETE FROM messages WHERE session_id IN (SELECT id FROM sessions WHERE started_at < ?)`,
		cutoff)
	if err != nil {
		return fmt.Errorf("clean expired messages: %w", err)
	}

	// Restore the delete trigger.
	_, _ = s.db.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
		INSERT INTO messages_fts(messages_fts, rowid, content, namespace)
		VALUES ('delete', old.id, old.content, COALESCE(old.namespace, ''));
	END`)

	// Delete expired sessions.
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE started_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("clean expired sessions: %w", err)
	}

	if rows, _ := result.RowsAffected(); rows > 0 {
		slog.Info("history cleanup", "deleted_sessions", rows, "retention_days", retentionDays)
	}
	return nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}
