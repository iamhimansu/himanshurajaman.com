package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// PostSummary is the list-card shape returned by /api/posts and /api/search.
type PostSummary struct {
	id             int64    // internal, for tag lookup; not serialized
	Slug           string   `json:"slug"`
	Title          string   `json:"title"`
	Summary        string   `json:"summary"`
	PublishedAt    string   `json:"published_at"`
	UpdatedAt      *string  `json:"updated_at"`
	CoverImage     *string  `json:"cover_image"`
	ReadingMinutes int      `json:"reading_minutes"`
	Tags           []string `json:"tags"`
}

// PostDetail is the full post returned by /api/posts/{slug}.
type PostDetail struct {
	Slug           string   `json:"slug"`
	Title          string   `json:"title"`
	Summary        string   `json:"summary"`
	HTML           string   `json:"html"`
	PublishedAt    string   `json:"published_at"`
	UpdatedAt      *string  `json:"updated_at"`
	CoverImage     *string  `json:"cover_image"`
	ReadingMinutes int      `json:"reading_minutes"`
	Tags           []string `json:"tags"`
}

// ListParams controls ListPosts.
type ListParams struct {
	Limit  int    // resolved default already applied by caller; still clamped
	Cursor string // base64url keyset cursor, or ""
	Tag    string // tag slug filter, or ""
}

// ListPosts returns published posts newest-first using keyset pagination on
// (published_at DESC, id DESC) — never OFFSET. It fetches limit+1 rows to tell
// whether a further page exists and, if so, returns a nextCursor.
func (s *Store) ListPosts(ctx context.Context, p ListParams) (items []PostSummary, nextCursor string, err error) {
	limit := clampLimit(p.Limit)

	var (
		b    strings.Builder
		args []any
	)
	b.WriteString(`SELECT p.id,p.slug,p.title,p.summary,p.published_at,p.updated_at,p.cover_image,p.reading_minutes FROM posts p`)
	if p.Tag != "" {
		b.WriteString(` JOIN post_tags pt ON pt.post_id=p.id JOIN tags t ON t.id=pt.tag_id`)
	}
	b.WriteString(` WHERE p.published=1`)
	if p.Tag != "" {
		b.WriteString(` AND t.slug=?`)
		args = append(args, p.Tag)
	}
	if p.Cursor != "" {
		curPub, curID, derr := decodeCursor(p.Cursor)
		if derr != nil {
			return nil, "", derr
		}
		b.WriteString(` AND (p.published_at < ? OR (p.published_at = ? AND p.id < ?))`)
		args = append(args, curPub, curPub, curID)
	}
	b.WriteString(` ORDER BY p.published_at DESC, p.id DESC LIMIT ?`)
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

	if len(items) > limit {
		last := items[limit-1]
		nextCursor = encodeCursor(last.PublishedAt, last.id)
		items = items[:limit]
	}
	if err := s.attachTags(ctx, items); err != nil {
		return nil, "", err
	}
	return items, nextCursor, nil
}

// GetPost returns a single published post, or ErrNotFound if missing or a draft.
func (s *Store) GetPost(ctx context.Context, slug string) (*PostDetail, error) {
	var (
		d  PostDetail
		id int64
	)
	err := s.reader.QueryRowContext(ctx, `
		SELECT id,slug,title,summary,html,published_at,updated_at,cover_image,reading_minutes
		FROM posts WHERE slug=? AND published=1`, slug).
		Scan(&id, &d.Slug, &d.Title, &d.Summary, &d.HTML, &d.PublishedAt, &d.UpdatedAt, &d.CoverImage, &d.ReadingMinutes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	tagsByPost, err := s.tagsForPosts(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	d.Tags = tagsByPost[id]
	if d.Tags == nil {
		d.Tags = []string{}
	}
	return &d, nil
}

// TagCount is a tag with its published-post count.
type TagCount struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Count int    `json:"count"`
}

// TagsWithCounts returns tags that have at least one published post, with
// counts reflecting published posts only.
func (s *Store) TagsWithCounts(ctx context.Context) ([]TagCount, error) {
	rows, err := s.reader.QueryContext(ctx, `
		SELECT t.name, t.slug, COUNT(*) AS n
		FROM tags t
		JOIN post_tags pt ON pt.tag_id = t.id
		JOIN posts p ON p.id = pt.post_id AND p.published = 1
		GROUP BY t.id
		ORDER BY n DESC, t.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TagCount{}
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Name, &tc.Slug, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

// scanSummaries scans the standard summary column list (id first). It returns a
// non-nil empty slice when there are no rows so the API renders [] (not null).
func scanSummaries(rows *sql.Rows) ([]PostSummary, error) {
	items := []PostSummary{}
	for rows.Next() {
		var it PostSummary
		if err := rows.Scan(&it.id, &it.Slug, &it.Title, &it.Summary,
			&it.PublishedAt, &it.UpdatedAt, &it.CoverImage, &it.ReadingMinutes); err != nil {
			return nil, err
		}
		it.Tags = []string{}
		items = append(items, it)
	}
	return items, rows.Err()
}

// attachTags fills in Tags for a page of summaries in one query.
func (s *Store) attachTags(ctx context.Context, items []PostSummary) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(items))
	for i := range items {
		ids[i] = items[i].id
	}
	byPost, err := s.tagsForPosts(ctx, ids)
	if err != nil {
		return err
	}
	for i := range items {
		if t := byPost[items[i].id]; t != nil {
			items[i].Tags = t
		}
	}
	return nil
}

func (s *Store) tagsForPosts(ctx context.Context, ids []int64) (map[int64][]string, error) {
	byPost := map[int64][]string{}
	if len(ids) == 0 {
		return byPost, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.reader.QueryContext(ctx, `
		SELECT pt.post_id, t.name
		FROM post_tags pt JOIN tags t ON t.id = pt.tag_id
		WHERE pt.post_id IN (`+placeholders(len(ids))+`)
		ORDER BY t.name ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pid int64
		var name string
		if err := rows.Scan(&pid, &name); err != nil {
			return nil, err
		}
		byPost[pid] = append(byPost[pid], name)
	}
	return byPost, rows.Err()
}
