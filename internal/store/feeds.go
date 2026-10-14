package store

import "context"

// SitemapPost is the minimal published-post shape the sitemap needs: its slug
// plus the dates used to derive <lastmod>.
type SitemapPost struct {
	Slug        string
	PublishedAt string
	UpdatedAt   *string
}

// SitemapPosts returns every published post newest-first for the sitemap. It
// reads only cached columns (never the stored document source) and returns a
// non-nil empty slice when there are no rows.
func (s *Store) SitemapPosts(ctx context.Context) ([]SitemapPost, error) {
	rows, err := s.reader.QueryContext(ctx, `
		SELECT slug, published_at, updated_at
		FROM posts WHERE published=1
		ORDER BY published_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SitemapPost{}
	for rows.Next() {
		var p SitemapPost
		if err := rows.Scan(&p.Slug, &p.PublishedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SitemapPage is the minimal published-page shape the sitemap needs: its slug
// plus the optional updated timestamp used to derive <lastmod>.
type SitemapPage struct {
	Slug    string
	Updated *string
}

// SitemapPages returns every published page for the sitemap, ordered by slug.
// It returns a non-nil empty slice when there are no rows.
func (s *Store) SitemapPages(ctx context.Context) ([]SitemapPage, error) {
	rows, err := s.reader.QueryContext(ctx, `
		SELECT slug, updated
		FROM pages WHERE published=1
		ORDER BY slug ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SitemapPage{}
	for rows.Next() {
		var p SitemapPage
		if err := rows.Scan(&p.Slug, &p.Updated); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FeedPost is the published-post shape the RSS feed needs. It carries the
// rendered HTML for content:encoded; the stored document source is never selected.
type FeedPost struct {
	Slug        string
	Title       string
	Summary     string
	HTML        string
	PublishedAt string
	UpdatedAt   *string
}

// FeedPosts returns up to limit published posts newest-first for the RSS feed.
// It selects only cached columns (never the stored document source) and returns
// a non-nil empty slice when there are no rows.
func (s *Store) FeedPosts(ctx context.Context, limit int) ([]FeedPost, error) {
	rows, err := s.reader.QueryContext(ctx, `
		SELECT slug, title, summary, html, published_at, updated_at
		FROM posts WHERE published=1
		ORDER BY published_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []FeedPost{}
	for rows.Next() {
		var p FeedPost
		if err := rows.Scan(&p.Slug, &p.Title, &p.Summary, &p.HTML, &p.PublishedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
