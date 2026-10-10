package store

import (
	"context"
	"encoding/json"
)

// ResumeSection is one stored resume row. Data is the raw JSON of that section
// (the "basics" object, or an entry from the resume's "sections" array).
type ResumeSection struct {
	Kind     string
	Position int
	Data     string // JSON
}

// ResumeSectionOut is the API shape for a resume section. Data is inlined raw
// JSON so the client receives the original structure untouched.
type ResumeSectionOut struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// Resume returns resume sections ordered by position ascending.
func (s *Store) Resume(ctx context.Context) ([]ResumeSectionOut, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT kind, data FROM resume_sections ORDER BY position ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ResumeSectionOut{}
	for rows.Next() {
		var kind, data string
		if err := rows.Scan(&kind, &data); err != nil {
			return nil, err
		}
		out = append(out, ResumeSectionOut{Kind: kind, Data: json.RawMessage(data)})
	}
	return out, rows.Err()
}
