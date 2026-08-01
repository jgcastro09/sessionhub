package registry

import (
	"encoding/json"
	"os"
	"time"
)

// FileFingerprint is the cached, content-derived subset of discovered for
// one path: everything inspectFile would otherwise have to re-read the file
// and recompute. Cheap, path-derived facts (Language/Category/Module/Kind)
// are always recomputed fresh regardless of fingerprint match, since a
// config change can move a path to a different module/category without
// touching the file itself.
type FileFingerprint struct {
	Size      int64                  `json:"size"`
	ModTime   time.Time              `json:"mod_time"`
	Hash      string                 `json:"hash"`
	LineCount int                    `json:"line_count"`
	Symbols   map[string][]SymbolRef `json:"symbols,omitempty"`
	Includes  []string               `json:"includes,omitempty"`
	Imports   []string               `json:"imports,omitempty"`
	Exports   []string               `json:"exports,omitempty"`
}

// matches reports whether info's size and mtime are identical to the cached
// fingerprint — the fast, content-free signal that a file's bytes have not
// changed since the fingerprint was recorded.
func (f FileFingerprint) matches(size int64, modTime time.Time) bool {
	return f.Size == size && f.ModTime.Equal(modTime)
}

// ScanMetrics reports what one scan actually did, so "rescan without change
// is cheap" is a verifiable, reportable fact rather than an internal detail.
type ScanMetrics struct {
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	DurationMS int64     `json:"duration_ms"`
	Full       bool      `json:"full"` // fingerprint cache bypassed entirely

	FilesSeen       int `json:"files_seen"`
	FilesReused     int `json:"files_reused"`     // fingerprint matched: no read, no re-hash, no re-analysis
	FilesReanalyzed int `json:"files_reanalyzed"` // new, changed, or forced full scan

	BytesRead int64 `json:"bytes_read"`

	// FallbackReasons counts why a file was reanalyzed instead of reused:
	// "new", "fingerprint-mismatch", "full-scan".
	FallbackReasons map[string]int `json:"fallback_reasons,omitempty"`
}

// ScanState is the derived, rebuildable-from-a-full-scan cache backing
// incremental scanning and Health's scan-freshness reporting. Like
// index.sqlite3, it is never a source of truth — deleting it only costs one
// full re-read on the next scan.
type ScanState struct {
	LastScanAt     time.Time                  `json:"last_scan_at"`
	LastFullScanAt time.Time                  `json:"last_full_scan_at"`
	LastMetrics    ScanMetrics                `json:"last_metrics"`
	Fingerprints   map[string]FileFingerprint `json:"fingerprints"`
}

func loadScanState(root string) (ScanState, error) {
	data, err := os.ReadFile(scanStatePath(root))
	if os.IsNotExist(err) {
		return ScanState{Fingerprints: map[string]FileFingerprint{}}, nil
	}
	if err != nil {
		return ScanState{}, err
	}
	var state ScanState
	if err := json.Unmarshal(data, &state); err != nil {
		// A corrupted/foreign-format cache is never fatal — it only costs one
		// full re-read, exactly like a missing cache.
		return ScanState{Fingerprints: map[string]FileFingerprint{}}, nil
	}
	if state.Fingerprints == nil {
		state.Fingerprints = map[string]FileFingerprint{}
	}
	return state, nil
}

func saveScanState(root string, state ScanState) error {
	return writeJSONIfChanged(scanStatePath(root), state)
}
