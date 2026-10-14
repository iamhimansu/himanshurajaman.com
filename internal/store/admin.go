package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
)

// AdminPostSummary is one row of the admin post list (drafts included).
type AdminPostSummary struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Published   bool     `json:"published"`
	PublishedAt string   `json:"published_at"`
	UpdatedAt   string   `json:"updated_at"`
	Tags        []string `json:"tags"`
}

// AdminPost is the full editable post the admin editor opens: all metadata
// columns, the Editor.js content document, and the repo blob sha as of the last
// sync.
type AdminPost struct {
	Slug        string
	Title       string
	Summary     string
	Document    string // Editor.js content OutputData (the edit source)
	Published   bool
	PublishedAt string
	UpdatedAt   string
	CoverImage  string
	Page        string
	FileSHA     string
	Tags        []string
}

// AdminListPosts returns all posts (drafts included), newest first by
// (published_at DESC, id DESC). status filters published IN (…): "published",
// "draft", or (default/unknown) "all". page == "" means no page filter (every
// post, unchanged behavior); page != "" appends "AND page = ?" so only posts
// filed under that slug are returned.
func (s *Store) AdminListPosts(ctx context.Context, status, page string) ([]AdminPostSummary, error) {
	var where string
	switch status {
	case "published":
		where = ` WHERE published = 1`
	case "draft":
		where = ` WHERE published = 0`
	default:
		where = ``
	}

	var args []any
	if page != "" {
		if where == "" {
			where = ` WHERE page = ?`
		} else {
			where += ` AND page = ?`
		}
		args = append(args, page)
	}

	rows, err := s.reader.QueryContext(ctx, `
		SELECT id, slug, title, summary, published, published_at, updated_at
		FROM posts`+where+`
		ORDER BY published_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var (
		items []AdminPostSummary
		ids   []int64
		byID  = map[int64]int{} // post id -> index in items
	)
	for rows.Next() {
		var (
			id       int64
			it       AdminPostSummary
			pub      int
			pubAt    sql.NullString
			updAt    sql.NullString
		)
		if err := rows.Scan(&id, &it.Slug, &it.Title, &it.Summary, &pub, &pubAt, &updAt); err != nil {
			return nil, err
		}
		it.Published = pub != 0
		it.PublishedAt = pubAt.String
		it.UpdatedAt = updAt.String
		it.Tags = []string{}
		byID[id] = len(items)
		ids = append(ids, id)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tagsByPost, err := s.tagsForPosts(ctx, ids)
	if err != nil {
		return nil, err
	}
	for id, idx := range byID {
		if t := tagsByPost[id]; t != nil {
			items[idx].Tags = t
		}
	}
	if items == nil {
		items = []AdminPostSummary{}
	}
	return items, nil
}

// GetAdminPost returns a single post regardless of published state, or
// ErrNotFound if the slug is absent.
func (s *Store) GetAdminPost(ctx context.Context, slug string) (*AdminPost, error) {
	var (
		id    int64
		p     AdminPost
		pub   int
		pubAt sql.NullString
		updAt sql.NullString
		cover sql.NullString
		page  sql.NullString
	)
	err := s.reader.QueryRowContext(ctx, `
		SELECT id, slug, title, summary, document, published, published_at, updated_at, cover_image, page, file_sha
		FROM posts WHERE slug = ?`, slug).
		Scan(&id, &p.Slug, &p.Title, &p.Summary, &p.Document, &pub, &pubAt, &updAt, &cover, &page, &p.FileSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Published = pub != 0
	p.PublishedAt = pubAt.String
	p.UpdatedAt = updAt.String
	p.CoverImage = cover.String
	p.Page = page.String

	tagsByPost, err := s.tagsForPosts(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	p.Tags = tagsByPost[id]
	if p.Tags == nil {
		p.Tags = []string{}
	}
	return &p, nil
}

// UpsertPost inserts or updates a single post in one writer transaction,
// keeping posts_fts.rowid == posts.id and the tag rows consistent. Callers must
// set FilePath (schema NOT NULL) plus Document/HTML/PlainText/Tags/dates so the
// row matches ingest's shape.
func (s *Store) UpsertPost(ctx context.Context, p content.Post) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM posts WHERE slug = ?`, p.Slug).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, ierr := tx.ExecContext(ctx, `
			INSERT INTO posts
				(slug,title,summary,document,html,published,published_at,updated_at,cover_image,reading_minutes,page,file_path,file_sha)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.Slug, p.Title, p.Summary, p.Document, p.HTML, boolInt(p.Published),
			nullStr(p.PublishedAt), nullStr(p.UpdatedAt), nullStr(p.CoverImage),
			p.ReadingMinutes, nullStr(p.Page), p.FilePath, p.FileSHA)
		if ierr != nil {
			return ierr
		}
		if id, ierr = res.LastInsertId(); ierr != nil {
			return ierr
		}
	case err != nil:
		return err
	default:
		if _, uerr := tx.ExecContext(ctx, `
			UPDATE posts SET
				title=?,summary=?,document=?,html=?,published=?,published_at=?,updated_at=?,
				cover_image=?,reading_minutes=?,page=?,file_path=?,file_sha=?
			WHERE id=?`,
			p.Title, p.Summary, p.Document, p.HTML, boolInt(p.Published),
			nullStr(p.PublishedAt), nullStr(p.UpdatedAt), nullStr(p.CoverImage),
			p.ReadingMinutes, nullStr(p.Page), p.FilePath, p.FileSHA, id); uerr != nil {
			return uerr
		}
		if _, derr := tx.ExecContext(ctx, `DELETE FROM post_tags WHERE post_id=?`, id); derr != nil {
			return derr
		}
		if _, derr := tx.ExecContext(ctx, `DELETE FROM posts_fts WHERE rowid=?`, id); derr != nil {
			return derr
		}
	}

	var tagNames []string
	for _, t := range p.Tags {
		if _, terr := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO tags(name,slug) VALUES(?,?)`, t.Name, t.Slug); terr != nil {
			return terr
		}
		var tagID int64
		if terr := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE slug=?`, t.Slug).Scan(&tagID); terr != nil {
			return terr
		}
		if _, terr := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO post_tags(post_id,tag_id) VALUES(?,?)`, id, tagID); terr != nil {
			return terr
		}
		tagNames = append(tagNames, t.Name)
	}

	if _, ferr := tx.ExecContext(ctx,
		`INSERT INTO posts_fts(rowid,title,body,tags) VALUES(?,?,?,?)`,
		id, p.Title, p.PlainText, strings.Join(tagNames, " ")); ferr != nil {
		return ferr
	}

	if err := deleteOrphanTags(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePost removes a post by slug in one writer transaction (CASCADE drops
// post_tags; the FTS row and orphan tags are cleaned explicitly). It is a no-op
// if the slug is absent.
func (s *Store) DeletePost(ctx context.Context, slug string) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM posts WHERE slug=?`, slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // no-op
	}
	if err != nil {
		return err
	}

	if _, derr := tx.ExecContext(ctx, `DELETE FROM posts WHERE id=?`, id); derr != nil {
		return derr
	}
	if _, derr := tx.ExecContext(ctx, `DELETE FROM posts_fts WHERE rowid=?`, id); derr != nil {
		return derr
	}
	if err := deleteOrphanTags(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceResume replaces every resume section in one writer transaction.
func (s *Store) ReplaceResume(ctx context.Context, sections []ResumeSection) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, derr := tx.ExecContext(ctx, `DELETE FROM resume_sections`); derr != nil {
		return derr
	}
	for _, rs := range sections {
		if _, ierr := tx.ExecContext(ctx,
			`INSERT INTO resume_sections(kind,position,data) VALUES(?,?,?)`,
			rs.Kind, rs.Position, rs.Data); ierr != nil {
			return ierr
		}
	}
	return tx.Commit()
}

// PostExists reports whether a post with slug exists (drafts included).
func (s *Store) PostExists(ctx context.Context, slug string) (bool, error) {
	var one int
	err := s.reader.QueryRowContext(ctx, `SELECT 1 FROM posts WHERE slug=?`, slug).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetPostFileSHA returns the stored repo blob sha for a post, or ErrNotFound.
func (s *Store) GetPostFileSHA(ctx context.Context, slug string) (string, error) {
	var sha string
	err := s.reader.QueryRowContext(ctx, `SELECT file_sha FROM posts WHERE slug=?`, slug).Scan(&sha)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return sha, nil
}

// UpsertPage inserts or updates a single page in one writer transaction. Pages
// carry no FTS/tag state, so it is simpler than UpsertPost: SELECT id by slug,
// then INSERT or UPDATE every column.
func (s *Store) UpsertPage(ctx context.Context, pg content.Page) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM pages WHERE slug = ?`, pg.Slug).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, ierr := tx.ExecContext(ctx, `
			INSERT INTO pages
				(slug,title,nav,nav_label,nav_order,published,summary,updated,html,blocks,file_path,file_sha)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			pg.Slug, pg.Title, boolInt(pg.Nav), nullStr(pg.NavLabel), pg.NavOrder,
			boolInt(pg.Published), nullStr(pg.Summary), nullStr(pg.Updated),
			pg.HTML, pg.Document, pg.FilePath, pg.FileSHA); ierr != nil {
			return ierr
		}
	case err != nil:
		return err
	default:
		if _, uerr := tx.ExecContext(ctx, `
			UPDATE pages SET
				title=?,nav=?,nav_label=?,nav_order=?,published=?,summary=?,updated=?,
				html=?,blocks=?,file_path=?,file_sha=?
			WHERE id=?`,
			pg.Title, boolInt(pg.Nav), nullStr(pg.NavLabel), pg.NavOrder,
			boolInt(pg.Published), nullStr(pg.Summary), nullStr(pg.Updated),
			pg.HTML, pg.Document, pg.FilePath, pg.FileSHA, id); uerr != nil {
			return uerr
		}
	}
	return tx.Commit()
}

// DeletePage removes a page by slug in one writer transaction. It is a no-op if
// the slug is absent. Posts filed under the page keep their page value (now
// dangling), consistent with FR-POST-5.
func (s *Store) DeletePage(ctx context.Context, slug string) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE slug=?`, slug)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // no-op
	}
	return tx.Commit()
}

// PageExists reports whether a page with slug exists (drafts included).
func (s *Store) PageExists(ctx context.Context, slug string) (bool, error) {
	var one int
	err := s.reader.QueryRowContext(ctx, `SELECT 1 FROM pages WHERE slug=?`, slug).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetPageFileSHA returns the stored repo blob sha for a page, or ErrNotFound.
func (s *Store) GetPageFileSHA(ctx context.Context, slug string) (string, error) {
	var sha string
	err := s.reader.QueryRowContext(ctx, `SELECT file_sha FROM pages WHERE slug=?`, slug).Scan(&sha)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return sha, nil
}

// deleteOrphanTags removes tags no longer referenced by any post.
func deleteOrphanTags(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx,
		`DELETE FROM tags WHERE id NOT IN (SELECT DISTINCT tag_id FROM post_tags)`)
	return err
}
