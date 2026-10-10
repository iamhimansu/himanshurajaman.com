package store

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
)

// Search runs an FTS5 query over title/body/tags, published posts only, ranked
// by bm25 (most relevant first). Pagination is keyset on (rank ASC, id ASC):
// cursor = base64url("<rank>|<id>"). bm25 returns smaller (more negative)
// values for better matches, so "rank ASC" is best-first and the next page is
// everything ranked after the last row shown.
func (s *Store) Search(ctx context.Context, rawQuery, cursor string, limit int) (items []PostSummary, nextCursor string, err error) {
	limit = clampLimit(limit)

	match := ftsQuery(rawQuery)
	if match == "" {
		return []PostSummary{}, "", nil
	}

	var (
		b    strings.Builder
		args []any
	)
	// Wrap in a subquery so the WHERE/ORDER BY can reference the bm25() alias.
	b.WriteString(`
		SELECT id,slug,title,summary,published_at,updated_at,cover_image,reading_minutes FROM (
			SELECT p.id AS id, p.slug AS slug, p.title AS title, p.summary AS summary,
			       p.published_at AS published_at, p.updated_at AS updated_at,
			       p.cover_image AS cover_image, p.reading_minutes AS reading_minutes,
			       bm25(posts_fts) AS rank
			FROM posts_fts
			JOIN posts p ON p.id = posts_fts.rowid
			WHERE posts_fts MATCH ? AND p.published = 1
		)`)
	args = append(args, match)

	if cursor != "" {
		rank, id, derr := decodeSearchCursor(cursor)
		if derr != nil {
			return nil, "", derr
		}
		b.WriteString(` WHERE (rank > ?) OR (rank = ? AND id > ?)`)
		args = append(args, rank, rank, id)
	}
	b.WriteString(` ORDER BY rank ASC, id ASC LIMIT ?`)
	args = append(args, limit+1)

	rows, err := s.reader.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	items, err = scanSummaries(rows)
	if err != nil {
		return nil, "", err
	}

	// Recompute each row's rank for cursor encoding by re-querying is wasteful;
	// instead we re-select rank alongside. Simpler: scanSummaries dropped rank,
	// so fetch it here via a parallel slice. To keep one scan, re-run with rank.
	if len(items) > limit {
		rank, rerr := s.rowRank(ctx, match, items[limit-1].id)
		if rerr != nil {
			return nil, "", rerr
		}
		nextCursor = encodeSearchCursor(rank, items[limit-1].id)
		items = items[:limit]
	}

	if err := s.attachTags(ctx, items); err != nil {
		return nil, "", err
	}
	return items, nextCursor, nil
}

// rowRank returns the bm25 rank of a single post for a given match query, used
// to build the next cursor without carrying rank through scanSummaries.
func (s *Store) rowRank(ctx context.Context, match string, id int64) (float64, error) {
	var rank float64
	err := s.reader.QueryRowContext(ctx, `
		SELECT bm25(posts_fts)
		FROM posts_fts JOIN posts p ON p.id = posts_fts.rowid
		WHERE posts_fts MATCH ? AND p.id = ? AND p.published = 1`, match, id).Scan(&rank)
	if err != nil {
		return 0, err
	}
	return rank, nil
}

// ftsQuery turns free text into a safe FTS5 MATCH expression: each whitespace
// token becomes a quoted phrase, ANDed together. Quoting prevents user input
// from being interpreted as FTS operators (which would raise syntax errors).
func ftsQuery(raw string) string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		terms = append(terms, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(terms, " ")
}

func encodeSearchCursor(rank float64, id int64) string {
	s := strconv.FormatFloat(rank, 'g', -1, 64) + "|" + strconv.FormatInt(id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func decodeSearchCursor(c string) (rank float64, id int64, err error) {
	raw, derr := base64.RawURLEncoding.DecodeString(c)
	if derr != nil {
		return 0, 0, ErrBadCursor
	}
	rs, is, ok := strings.Cut(string(raw), "|")
	if !ok {
		return 0, 0, ErrBadCursor
	}
	rank, derr = strconv.ParseFloat(rs, 64)
	if derr != nil {
		return 0, 0, ErrBadCursor
	}
	id, derr = strconv.ParseInt(is, 10, 64)
	if derr != nil {
		return 0, 0, ErrBadCursor
	}
	return rank, id, nil
}
