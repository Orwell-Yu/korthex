package history

const schemaSQL = `
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    cluster     TEXT NOT NULL,
    started_at  DATETIME NOT NULL,
    ended_at    DATETIME,
    summary     TEXT
);

CREATE TABLE IF NOT EXISTS messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    role        TEXT NOT NULL,
    content     TEXT NOT NULL,
    namespace   TEXT,
    created_at  DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id);
CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_cluster ON sessions(cluster);
`

// ftsSQL is separated because FTS5 virtual tables don't support IF NOT EXISTS
// in all SQLite builds. We handle the "already exists" error in ensureSchema.
// trigram tokenizer is required for CJK (Chinese/Japanese/Korean) text search.
const ftsSQL = `CREATE VIRTUAL TABLE messages_fts USING fts5(content, namespace, tokenize='trigram');`

// triggerSQL keeps the FTS index in sync with the messages table.
const triggerSQL = `
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, content, namespace)
    VALUES (new.id, new.content, COALESCE(new.namespace, ''));
END;

CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, content, namespace)
    VALUES ('delete', old.id, old.content, COALESCE(old.namespace, ''));
END;
`
