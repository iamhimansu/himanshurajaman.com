package store

import (
	"context"
	"database/sql"
	"errors"
)

// SyncState is the single-row record of the last successful ingest.
type SyncState struct {
	CommitSHA     string `json:"commit_sha"`
	SourceRef     string `json:"source_ref"`
	SchemaVersion int    `json:"schema_version"`
	SyncedAt      string `json:"synced_at"`
}

// SyncState returns the recorded sync state. The bool is false when no ingest
// has committed yet (the row is absent).
func (s *Store) SyncState(ctx context.Context) (*SyncState, bool, error) {
	var ss SyncState
	err := s.reader.QueryRowContext(ctx, `
		SELECT commit_sha, source_ref, schema_version, synced_at
		FROM sync_state WHERE id = 1`).
		Scan(&ss.CommitSHA, &ss.SourceRef, &ss.SchemaVersion, &ss.SyncedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &ss, true, nil
}
