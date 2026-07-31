// Package registry implements the Code Registry service described in
// plans/project-task-manager-code-registry.md: a semantic inventory of a
// Project's source tree, stored as JSON records under
// <project>/.shproject/registry/records, with .shproject as the only
// canonical store. There is no server, no Python runtime, and no import
// path from the legacy "modules/Code Registry" reference implementation.
package registry

import "time"

// Status describes an entry's relationship to the current working tree.
type Status string

const (
	// StatusActive means the file backing this entry was seen on the most
	// recent scan.
	StatusActive Status = "active"
	// StatusMissing means the entry's path no longer exists and no rename
	// (matching hash) was found for it. The entry is kept, not deleted, so
	// tasks that reference it and human review notes are not lost silently.
	StatusMissing Status = "missing"
)

// Entry is one Code Registry record (plan section 5.2). Fields split into
// two groups: automatic facts the scanner may always overwrite, and
// human-reviewed fields the scanner must preserve once Reviewed is true.
type Entry struct {
	EntryID string `json:"entry_id"`

	// --- automatic facts, always safe for the scanner to overwrite ---
	Path     string   `json:"path"`
	Category string   `json:"category"`
	Language string   `json:"language"`
	Hash     string   `json:"hash"`
	Size     int64    `json:"size"`
	Lines    int      `json:"lines"`
	Symbols  []string `json:"symbols,omitempty"`
	Status   Status   `json:"status"`

	// --- human-reviewed fields, preserved once Reviewed is true ---
	Module             string     `json:"module,omitempty"`
	Description        string     `json:"description,omitempty"`
	Responsibilities   []string   `json:"responsibilities,omitempty"`
	Criticality        string     `json:"criticality,omitempty"`
	RelationsConfirmed []string   `json:"relations_confirmed,omitempty"`
	RelationsProbable  []string   `json:"relations_probable,omitempty"`
	Reviewed           bool       `json:"reviewed"`
	ReviewedAt         *time.Time `json:"reviewed_at,omitempty"`

	// --- semantic index, maintained by EnsureSemanticIndex ---
	// Embedding is the vector for this entry's indexed text, computed by
	// whatever Embedder is wired in (internal/embedding's local llama.cpp
	// engine). EmbeddingHash records which Hash it was computed from, so a
	// changed file is re-embedded automatically and an unchanged one never
	// costs another embedding call.
	Embedding     []float32 `json:"embedding,omitempty"`
	EmbeddingHash string    `json:"embedding_hash,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// recordFile is the on-disk shape of one records/<category>.json file.
type recordFile struct {
	Entries []Entry `json:"entries"`
}

// Redacted drops the raw embedding vector — 384 floats of no use to a human
// or the Web Panel, only ever needed internally for cosine comparison — the
// same "strip the heavy/internal field before this leaves the process"
// convention domain.ExecutorConfig.Redacted() already uses for secrets.
// EmbeddingHash stays, since "has this file been semantically indexed?" is
// useful to show.
func (e Entry) Redacted() Entry {
	e.Embedding = nil
	return e
}

// RedactedEntries applies Redacted to a whole slice, for list/search
// responses.
func RedactedEntries(entries []Entry) []Entry {
	out := make([]Entry, len(entries))
	for i, e := range entries {
		out[i] = e.Redacted()
	}
	return out
}
