// Package registry implements the Code Registry: a declaratively indexed,
// hash-authoritative catalog of a Project's source tree, stored as JSON
// records under <project>/.shproject/registry, with a rebuildable SQLite
// cache (index.sqlite3) for full-text and semantic search. .shproject/
// records/*.json is the only canonical store; everything else the package
// produces (the SQLite cache, computed relationship graphs, health reports)
// is fully re-derivable from it.
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
	// StatusDeprecated is an explicit human/agent mark; the file may still
	// exist on disk but is no longer considered part of the maintained tree.
	// Scan never sets or clears this — only Review does.
	StatusDeprecated Status = "deprecated"
)

// Kind is a file's structural role, assigned declaratively from path/
// extension rules (see classification.go). It is always recomputed on scan.
type Kind string

const (
	KindImplementation Kind = "implementation"
	KindHeader         Kind = "header"
	KindTest           Kind = "test"
	KindTool           Kind = "tool"
	KindComponent      Kind = "component"
	KindConfig         Kind = "config"
	KindDocs           Kind = "docs"
	KindScript         Kind = "script"
	KindOther          Kind = "other"
)

// ReviewStatus tracks whether a human/agent has confirmed an entry's
// semantic fields (Description/Responsibilities/Criticality/RelatedFiles)
// match its current content. See applyDiscovery in scanner.go for the one
// rule that governs it: any content-hash change forces NeedsReview,
// unconditionally, regardless of the prior status.
type ReviewStatus string

const (
	ReviewNeedsReview ReviewStatus = "needs_review"
	ReviewReviewed    ReviewStatus = "reviewed"
)

// Criticality is a reviewer-assigned risk tier. Never set automatically.
type Criticality string

const (
	CriticalityStandard  Criticality = "standard"
	CriticalityImportant Criticality = "important"
	CriticalityCritical  Criticality = "critical"
)

// SymbolRef is one detected top-level identifier and the 1-based line it
// starts on, so the Web Panel can jump directly to it.
type SymbolRef struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

// Entry is one Code Registry record. Fields are split into three groups by
// ownership, and Scan never blurs the line between them:
//
//   - declarative/structural facts (Category, Module, Area, Role, Symbols,
//     Includes/Imports/Exports/Dependencies, size/line/hash/mtime) are always
//     recomputed from the file's path and content on every scan — they can
//     never go stale because nothing ever "freezes" them;
//   - human/agent-authored fields (Description, Responsibilities,
//     Criticality, RelatedFiles) are never touched by Scan, only by Review;
//   - review-state fields (Hash vs LastReviewedHash, ReviewStatus) are the
//     bridge between the two: Scan may advance Hash, and the moment it does
//     so for a changed file it also force-sets ReviewStatus to NeedsReview,
//     so a reviewed file can never silently drift out of sync with its own
//     description. Only Review ever sets LastReviewedHash/ReviewReviewed.
type Entry struct {
	EntryID   string `json:"entry_id"`
	Path      string `json:"path"`
	Filename  string `json:"filename"`
	Extension string `json:"extension"`
	Language  string `json:"language"`
	Kind      Kind   `json:"kind"`

	// --- declarative facts, always recomputed from Path on every scan ---
	Category string `json:"category"`
	Module   string `json:"module"`
	Area     string `json:"area"`
	Role     string `json:"role"`

	// --- structural facts, always recomputed from content on every scan ---
	Symbols      map[string][]SymbolRef `json:"symbols,omitempty"`
	Includes     []string               `json:"includes,omitempty"`
	Imports      []string               `json:"imports,omitempty"`
	Exports      []string               `json:"exports,omitempty"`
	Dependencies []string               `json:"dependencies,omitempty"`

	LineCount      int       `json:"line_count"`
	SizeBytes      int64     `json:"size_bytes"`
	LastModifiedAt time.Time `json:"last_modified_at"`
	Hash           string    `json:"hash"`
	Status         Status    `json:"status"`

	// --- human/agent-authored, never touched by Scan() ---
	Description      string      `json:"description,omitempty"`
	Responsibilities []string    `json:"responsibilities,omitempty"`
	Criticality      Criticality `json:"criticality,omitempty"`
	RelatedFiles     []string    `json:"related_files,omitempty"` // entry_id, confirmed

	// --- auto-computed, scan-refreshed, but not human-owned ---
	ProbableRelatedFiles []string `json:"probable_related_files,omitempty"` // entry_id, same-stem heuristic
	PreviousPaths        []string `json:"previous_paths,omitempty"`         // rename history, capped 20

	// --- review state ---
	LastReviewedHash string       `json:"last_reviewed_hash,omitempty"`
	ReviewStatus     ReviewStatus `json:"review_status"`
	ReviewedAt       *time.Time   `json:"reviewed_at,omitempty"`
	ReviewedBy       string       `json:"reviewed_by,omitempty"`

	LastScannedAt time.Time `json:"last_scanned_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Stale reports whether the entry's current content hash has moved past
// what was last confirmed by a human/agent review — the single source of
// truth for "does this need another look," independent of ReviewStatus
// (which Scan sets, but health.go re-derives this independently to catch
// any future regression in the scan path).
func (e Entry) Stale() bool {
	return e.ReviewStatus != ReviewReviewed || e.Hash != e.LastReviewedHash
}

// recordFile is the on-disk shape of one records/<category>.json file.
type recordFile struct {
	Entries []Entry `json:"entries"`
}

// PendingFile is a text-looking file that failed the eligibility whitelist
// (policy.go) — surfaced in the "Pending Classification" queue instead of
// being silently indexed or silently dropped. Fully derived: rewritten in
// whole on every scan, never hand-edited.
type PendingFile struct {
	Path        string    `json:"path"`
	Extension   string    `json:"extension"`
	SizeBytes   int64     `json:"size_bytes"`
	DetectedAt  time.Time `json:"detected_at"`
	SuggestedBy string    `json:"suggested_reason,omitempty"` // e.g. "text content, unrecognized extension"
}

// pendingFile is the on-disk shape of pending.json.
type pendingFileList struct {
	Files []PendingFile `json:"files"`
}
