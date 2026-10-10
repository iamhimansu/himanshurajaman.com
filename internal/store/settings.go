package store

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// Settings returns the whole settings object, each stored key mapped to its raw
// JSON value so the client gets exactly what was ingested.
func (s *Store) Settings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = json.RawMessage(v)
	}
	return out, rows.Err()
}

// PostsPerPage returns settings.postsPerPage, or 0 if unset/invalid. The value
// is stored as a JSON number (e.g. "10"), so a plain integer parse works.
func (s *Store) PostsPerPage(ctx context.Context) int {
	var v string
	if err := s.reader.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='postsPerPage'`).Scan(&v); err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0
	}
	return n
}
