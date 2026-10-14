-- SQLite schema for the disposable read cache. Rebuilt from the content repo
-- on every boot; the .sqlite file is never committed.
--
-- journal_mode=WAL, busy_timeout, and foreign_keys are set per-connection at
-- open time (via the DSN), NOT here.

CREATE TABLE posts (
  id INTEGER PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  summary TEXT NOT NULL,
  document TEXT NOT NULL,
  html TEXT NOT NULL,
  published INTEGER NOT NULL DEFAULT 0,
  published_at TEXT,
  updated_at TEXT,
  cover_image TEXT,
  reading_minutes INTEGER NOT NULL DEFAULT 0,
  page TEXT,                      -- NULL/absent => general blog (loose slug ref, intentionally not a FK)
  file_path TEXT NOT NULL,
  file_sha TEXT NOT NULL
);
CREATE INDEX idx_posts_pub_date ON posts(published, published_at DESC);
CREATE INDEX idx_posts_page ON posts(page);

CREATE TABLE pages (
  id INTEGER PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  nav INTEGER NOT NULL DEFAULT 0,
  nav_label TEXT,                 -- NULL when absent; callers fall back to title
  nav_order INTEGER NOT NULL DEFAULT 1000000,
  published INTEGER NOT NULL DEFAULT 0,
  summary TEXT,                   -- NULL when absent
  updated TEXT,                   -- RFC3339 UTC or NULL
  html TEXT NOT NULL,
  blocks TEXT NOT NULL,           -- canonical Editor.js content OutputData (edit source)
  file_path TEXT NOT NULL,
  file_sha TEXT NOT NULL
);
-- Nav resolution reads published + nav rows ordered by (nav_order, title).
CREATE INDEX idx_pages_nav ON pages(published, nav, nav_order, title);

CREATE TABLE tags (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  slug TEXT NOT NULL UNIQUE
);

CREATE TABLE post_tags (
  post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (post_id, tag_id)
);
CREATE INDEX idx_post_tags_tag ON post_tags(tag_id);

CREATE TABLE resume_sections (
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL,
  position INTEGER NOT NULL,
  data TEXT NOT NULL
);
CREATE INDEX idx_resume_pos ON resume_sections(position);

CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE sync_state (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  commit_sha TEXT NOT NULL,
  source_ref TEXT NOT NULL,
  schema_version INTEGER NOT NULL,
  synced_at TEXT NOT NULL
);

-- Regular (not external-content) FTS5 table: rowid is kept equal to posts.id and
-- populated explicitly inside the ingest transaction (no triggers).
CREATE VIRTUAL TABLE posts_fts USING fts5 (
  title,
  body,
  tags,
  tokenize = 'porter unicode61'
);
