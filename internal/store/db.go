// Package store owns the SQLite read cache: opening it with the right pragmas,
// creating the schema, atomically replacing its contents at ingest, and serving
// the read queries the public API needs.
package store

import (
	"database/sql"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	// modernc.org/sqlite is a pure-Go (CGO-free) SQLite with FTS5 built in. It
	// registers the database/sql driver named "sqlite".
	_ "modernc.org/sqlite"
)

// SchemaVersion is bumped whenever schema.sql changes. The cache is disposable,
// so there are no migrations: the version is part of the temp-file name, so a
// bump simply starts from a fresh file and re-ingests from the content repo.
//
// v2: the post body column `document` stores the Editor.js content OutputData
// (the editable source), rendered to HTML on the server at ingest.
const SchemaVersion = 2

const (
	defaultLimit = 10
	maxLimit     = 50
)

// Sentinel errors surfaced to the HTTP layer.
var (
	ErrNotFound  = errors.New("not found")
	ErrBadCursor = errors.New("invalid cursor")
)

//go:embed schema.sql
var schemaSQL string

// Store holds two connection pools against the same WAL database file:
// a single serialized writer and a multi-connection reader pool. WAL lets the
// readers run concurrently with the writer without blocking.
type Store struct {
	writer *sql.DB
	reader *sql.DB
	path   string
}

// DefaultPath returns the temp-file path for the cache, honoring an override.
// The schema version is embedded in the name so a bump lands on a fresh file.
func DefaultPath(override string) string {
	if override != "" {
		return override
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("portfolio-v%d.sqlite", SchemaVersion))
}

// pragmaDSN builds a modernc.org/sqlite DSN. modernc applies every `_pragma`
// query parameter to each new connection, so WAL, the busy timeout, and FK
// enforcement are set uniformly on both pools without a connect hook.
func pragmaDSN(path string) string {
	return "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(ON)" +
		"&_pragma=synchronous(NORMAL)"
}

// Open removes any stale cache file (the cache is disposable, so a clean file
// guarantees a clean schema), opens the writer and reader pools, and creates
// the schema.
func Open(path string) (*Store, error) {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Remove(p)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite dir: %w", err)
		}
	}

	dsn := pragmaDSN(path)

	writer, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	// A single writer connection serializes all writes (SQLite allows only one
	// writer at a time; this avoids SQLITE_BUSY churn under the write lock).
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)

	reader, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("open reader: %w", err)
	}
	readers := runtime.NumCPU()
	if readers < 4 {
		readers = 4
	}
	reader.SetMaxOpenConns(readers)
	reader.SetMaxIdleConns(readers)

	s := &Store{writer: writer, reader: reader, path: path}
	if err := s.init(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) init() error {
	if _, err := s.writer.Exec(schemaSQL); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	// Touch the writer so the DB file (and WAL) exist before readers attach.
	var jm string
	if err := s.writer.QueryRow("PRAGMA journal_mode").Scan(&jm); err != nil {
		return fmt.Errorf("verify journal_mode: %w", err)
	}
	if !strings.EqualFold(jm, "wal") {
		return fmt.Errorf("expected WAL journal mode, got %q", jm)
	}
	return nil
}

// Path returns the on-disk path of the cache file.
func (s *Store) Path() string { return s.path }

// Close closes both pools.
func (s *Store) Close() error {
	var errW, errR error
	if s.writer != nil {
		errW = s.writer.Close()
	}
	if s.reader != nil {
		errR = s.reader.Close()
	}
	if errW != nil {
		return errW
	}
	return errR
}

// --- small shared helpers ---

func clampLimit(n int) int {
	switch {
	case n <= 0:
		return defaultLimit
	case n > maxLimit:
		return maxLimit
	default:
		return n
	}
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullStr maps "" to a SQL NULL so empty optional fields aren't stored as "".
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// encodeCursor / decodeCursor implement keyset pagination over
// (published_at DESC, id DESC): cursor = base64url("<published_at>|<id>").
func encodeCursor(publishedAt string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(publishedAt + "|" + strconv.FormatInt(id, 10)))
}

func decodeCursor(c string) (publishedAt string, id int64, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", 0, ErrBadCursor
	}
	pub, idStr, ok := strings.Cut(string(raw), "|")
	if !ok {
		return "", 0, ErrBadCursor
	}
	id, err = strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return "", 0, ErrBadCursor
	}
	return pub, id, nil
}
