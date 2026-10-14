package store

import (
	"context"
	"database/sql"
	"errors"
)

// PageDetail is the public page view payload (published-only lookups).
type PageDetail struct {
	Slug    string  `json:"slug"`
	Title   string  `json:"title"`
	HTML    string  `json:"html"`
	Summary *string `json:"summary"`
	Updated *string `json:"updated"`
}

// AdminPageSummary is one row of the admin pages gridview (drafts included).
// PostCount is posts.page == slug across ALL statuses (admin sees drafts).
type AdminPageSummary struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Nav       bool   `json:"nav"`
	Published bool   `json:"published"`
	PostCount int    `json:"post_count"`
}

// AdminPage is the full editable page the admin page editor opens.
type AdminPage struct {
	Slug      string
	Title     string
	Nav       bool
	NavLabel  string
	NavOrder  int
	Published bool
	Summary   string
	Updated   string
	Document  string
	FileSHA   string
}

// NavPage is one computed nav item from a published nav-enabled page.
type NavPage struct {
	Slug  string `json:"slug"`
	Label string `json:"label"`
}

// GetPage returns a single published page, or ErrNotFound if missing or a draft.
func (s *Store) GetPage(ctx context.Context, slug string) (*PageDetail, error) {
	var d PageDetail
	err := s.reader.QueryRowContext(ctx, `
		SELECT slug, title, html, summary, updated
		FROM pages WHERE slug=? AND published=1`, slug).
		Scan(&d.Slug, &d.Title, &d.HTML, &d.Summary, &d.Updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetAdminPage returns a single page regardless of published state, or
// ErrNotFound if the slug is absent.
func (s *Store) GetAdminPage(ctx context.Context, slug string) (*AdminPage, error) {
	var (
		p        AdminPage
		nav      int
		pub      int
		navLabel sql.NullString
		summary  sql.NullString
		updated  sql.NullString
	)
	err := s.reader.QueryRowContext(ctx, `
		SELECT slug, title, nav, nav_label, nav_order, published, summary, updated, blocks, file_sha
		FROM pages WHERE slug=?`, slug).
		Scan(&p.Slug, &p.Title, &nav, &navLabel, &p.NavOrder, &pub, &summary, &updated, &p.Document, &p.FileSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Nav = nav != 0
	p.Published = pub != 0
	p.NavLabel = navLabel.String
	p.Summary = summary.String
	p.Updated = updated.String
	return &p, nil
}

// ListPages returns every page (drafts included), ordered (nav_order ASC,
// title ASC), each with its post count across ALL statuses. It returns a
// non-nil empty slice when there are no rows.
func (s *Store) ListPages(ctx context.Context) ([]AdminPageSummary, error) {
	rows, err := s.reader.QueryContext(ctx, `
		SELECT pg.slug, pg.title, pg.nav, pg.published,
		       (SELECT COUNT(*) FROM posts p WHERE p.page = pg.slug) AS post_count
		FROM pages pg
		ORDER BY pg.nav_order ASC, pg.title ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AdminPageSummary{}
	for rows.Next() {
		var (
			it  AdminPageSummary
			nav int
			pub int
		)
		if err := rows.Scan(&it.Slug, &it.Title, &nav, &pub, &it.PostCount); err != nil {
			return nil, err
		}
		it.Nav = nav != 0
		it.Published = pub != 0
		out = append(out, it)
	}
	return out, rows.Err()
}

// ListNavPages returns published, nav-enabled pages ordered (nav_order ASC,
// title ASC); the label falls back to the title when nav_label is absent. It
// returns a non-nil empty slice when there are no rows.
func (s *Store) ListNavPages(ctx context.Context) ([]NavPage, error) {
	rows, err := s.reader.QueryContext(ctx, `
		SELECT slug, COALESCE(NULLIF(nav_label, ''), title) AS label
		FROM pages WHERE published=1 AND nav=1
		ORDER BY nav_order ASC, title ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []NavPage{}
	for rows.Next() {
		var np NavPage
		if err := rows.Scan(&np.Slug, &np.Label); err != nil {
			return nil, err
		}
		out = append(out, np)
	}
	return out, rows.Err()
}

// CountPostsByPage returns the number of posts filed under slug, across ALL
// statuses.
func (s *Store) CountPostsByPage(ctx context.Context, slug string) (int, error) {
	var n int
	err := s.reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM posts WHERE page=?`, slug).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// PostsByPage returns the posts filed under slug, newest-first. When
// publishedOnly is true only published posts are returned (public page view);
// the admin nested list passes false. It reuses the standard summary projection
// and attachTags, with no page column in the SELECT. Returns a non-nil empty
// slice when there are no rows.
func (s *Store) PostsByPage(ctx context.Context, slug string, publishedOnly bool) ([]PostSummary, error) {
	q := `SELECT p.id,p.slug,p.title,p.summary,p.published_at,p.updated_at,p.cover_image,p.reading_minutes FROM posts p WHERE p.page = ?`
	if publishedOnly {
		q += ` AND p.published = 1`
	}
	q += ` ORDER BY p.published_at DESC, p.id DESC`

	rows, err := s.reader.QueryContext(ctx, q, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items, err := scanSummaries(rows)
	if err != nil {
		return nil, err
	}
	if err := s.attachTags(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}
