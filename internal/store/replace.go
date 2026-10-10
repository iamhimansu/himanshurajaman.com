package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/iamhimansu/himanshurajaman.com/internal/content"
)

// Dataset is a complete, validated snapshot ready to become the cache. Ingest
// builds it fully in memory, then Replace swaps it in atomically.
type Dataset struct {
	Posts     []content.Post
	Resume    []ResumeSection
	Settings  map[string]json.RawMessage
	CommitSHA string
	Ref       string
	SyncedAt  time.Time
}

// Replace atomically replaces the entire cache in one writer transaction. If
// anything fails the transaction rolls back, so a previously good cache is left
// intact — a failed refresh never wipes good data.
func (s *Store) Replace(ctx context.Context, ds *Dataset) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Clear every table (child rows first; CASCADE would cover post_tags, but
	// being explicit keeps the intent obvious).
	for _, stmt := range []string{
		`DELETE FROM post_tags`,
		`DELETE FROM posts_fts`,
		`DELETE FROM posts`,
		`DELETE FROM tags`,
		`DELETE FROM resume_sections`,
		`DELETE FROM settings`,
		`DELETE FROM sync_state`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("clear (%s): %w", stmt, err)
		}
	}

	tagID := map[string]int64{} // tag slug -> id, cached across posts
	for _, p := range ds.Posts {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO posts
				(slug,title,summary,document,html,published,published_at,updated_at,cover_image,reading_minutes,file_path,file_sha)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.Slug, p.Title, p.Summary, p.Document, p.HTML, boolInt(p.Published),
			nullStr(p.PublishedAt), nullStr(p.UpdatedAt), nullStr(p.CoverImage),
			p.ReadingMinutes, p.FilePath, p.FileSHA,
		)
		if err != nil {
			return fmt.Errorf("insert post %q: %w", p.Slug, err)
		}
		postID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("post id %q: %w", p.Slug, err)
		}

		var tagNames []string
		for _, t := range p.Tags {
			id, ok := tagID[t.Slug]
			if !ok {
				if _, err := tx.ExecContext(ctx,
					`INSERT OR IGNORE INTO tags(name,slug) VALUES(?,?)`, t.Name, t.Slug); err != nil {
					return fmt.Errorf("insert tag %q: %w", t.Slug, err)
				}
				if err := tx.QueryRowContext(ctx,
					`SELECT id FROM tags WHERE slug=?`, t.Slug).Scan(&id); err != nil {
					return fmt.Errorf("resolve tag %q: %w", t.Slug, err)
				}
				tagID[t.Slug] = id
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO post_tags(post_id,tag_id) VALUES(?,?)`, postID, id); err != nil {
				return fmt.Errorf("link tag %q: %w", t.Slug, err)
			}
			tagNames = append(tagNames, t.Name)
		}

		// FTS row: rowid == posts.id, body is plain text (no HTML in the index).
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO posts_fts(rowid,title,body,tags) VALUES(?,?,?,?)`,
			postID, p.Title, p.PlainText, strings.Join(tagNames, " ")); err != nil {
			return fmt.Errorf("insert fts %q: %w", p.Slug, err)
		}
	}

	for _, rs := range ds.Resume {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO resume_sections(kind,position,data) VALUES(?,?,?)`,
			rs.Kind, rs.Position, rs.Data); err != nil {
			return fmt.Errorf("insert resume section %q: %w", rs.Kind, err)
		}
	}

	for k, v := range ds.Settings {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO settings(key,value) VALUES(?,?)`, k, string(v)); err != nil {
			return fmt.Errorf("insert setting %q: %w", k, err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sync_state(id,commit_sha,source_ref,schema_version,synced_at) VALUES(1,?,?,?,?)`,
		ds.CommitSHA, ds.Ref, SchemaVersion, ds.SyncedAt.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("insert sync_state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
