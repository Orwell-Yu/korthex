package history

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Search queries the FTS5 index for matching messages and returns results
// grouped by session with preview messages.
// For short keywords (< 3 chars), falls back to LIKE search because the
// trigram tokenizer requires at least 3 characters.
func (s *sqliteStore) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}

	var sqlStr string
	var args []any

	if query.Keyword == "" {
		// No keyword: list recent sessions with their first user message as preview.
		timeFilter := ""
		if !query.After.IsZero() {
			timeFilter = " AND m.created_at > ?"
			args = append(args, query.After.Format(time.RFC3339))
		}

		sqlStr = fmt.Sprintf(`
			SELECT s.id, s.cluster, s.started_at, COALESCE(s.summary, ''),
			       m.role, m.content, COALESCE(m.namespace, ''), m.created_at
			FROM messages m
			JOIN sessions s ON m.session_id = s.id
			WHERE m.role = 'user'%s
			ORDER BY m.created_at DESC
			LIMIT ?
		`, timeFilter)
	} else if len([]rune(query.Keyword)) >= 3 {
		// trigram FTS5 search (>= 3 chars).
		ftsQuery := escapeFTS5(query.Keyword)
		if query.Namespace != "" {
			ftsQuery = fmt.Sprintf("(%s) AND namespace:%s", ftsQuery, escapeFTS5(query.Namespace))
		}

		where := "messages_fts MATCH ?"
		args = append(args, ftsQuery)

		timeFilter := ""
		if !query.After.IsZero() {
			timeFilter = " AND m.created_at > ?"
			args = append(args, query.After.Format(time.RFC3339))
		}

		sqlStr = fmt.Sprintf(`
			SELECT s.id, s.cluster, s.started_at, COALESCE(s.summary, ''),
			       m.role, m.content, COALESCE(m.namespace, ''), m.created_at
			FROM messages_fts
			JOIN messages m ON messages_fts.rowid = m.id
			JOIN sessions s ON m.session_id = s.id
			WHERE %s%s
			ORDER BY m.created_at DESC
			LIMIT ?
		`, where, timeFilter)
	} else {
		// LIKE fallback for short keywords (< 3 chars).
		where := "m.content LIKE ?"
		args = append(args, "%"+query.Keyword+"%")

		if query.Namespace != "" {
			where += " AND m.namespace LIKE ?"
			args = append(args, "%"+query.Namespace+"%")
		}

		timeFilter := ""
		if !query.After.IsZero() {
			timeFilter = " AND m.created_at > ?"
			args = append(args, query.After.Format(time.RFC3339))
		}

		sqlStr = fmt.Sprintf(`
			SELECT s.id, s.cluster, s.started_at, COALESCE(s.summary, ''),
			       m.role, m.content, COALESCE(m.namespace, ''), m.created_at
			FROM messages m
			JOIN sessions s ON m.session_id = s.id
			WHERE %s%s
			ORDER BY m.created_at DESC
			LIMIT ?
		`, where, timeFilter)
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("search history: %w", err)
	}
	defer rows.Close()

	// Group results by session.
	sessionMap := make(map[string]*SearchResult)
	var order []string

	for rows.Next() {
		var (
			sid, cluster, startedStr, summary string
			role, content, ns, createdStr     string
		)
		if err := rows.Scan(&sid, &cluster, &startedStr, &summary,
			&role, &content, &ns, &createdStr); err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}

		startedAt, _ := time.Parse(time.RFC3339, startedStr)
		createdAt, _ := time.Parse(time.RFC3339, createdStr)

		if _, exists := sessionMap[sid]; !exists {
			sessionMap[sid] = &SearchResult{
				SessionID: sid,
				Cluster:   cluster,
				StartedAt: startedAt,
				Summary:   summary,
			}
			order = append(order, sid)
		}

		sessionMap[sid].Messages = append(sessionMap[sid].Messages, Message{
			Role:      role,
			Content:   content,
			Namespace: ns,
			CreatedAt: createdAt,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search results: %w", err)
	}

	results := make([]SearchResult, 0, len(order))
	for _, sid := range order {
		results = append(results, *sessionMap[sid])
	}
	return results, nil
}

// escapeFTS5 escapes special FTS5 characters in a search term.
// FTS5 treats *, ", -, NEAR, AND, OR, NOT as operators.
func escapeFTS5(s string) string {
	// Wrap in double quotes if it contains special characters.
	if strings.ContainsAny(s, `"*-(){}[]^~:`) {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}
