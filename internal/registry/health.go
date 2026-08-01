package registry

import (
	"fmt"
	"strings"
	"time"
)

// SchemaIssue flags a reviewed entry missing a field a reviewed entry
// should have — the review form should have populated it.
type SchemaIssue struct {
	EntryID string `json:"entry_id"`
	Path    string `json:"path"`
	Field   string `json:"field"`
	Problem string `json:"problem"`
}

// HealthReport is a strict superset of the old CoverageReport: coverage,
// hashes/review staleness, schema completeness, classification,
// relationships, dependencies, and pending classification all roll into one
// Healthy bool. An inconsistent registry — in any one of these dimensions —
// is never reported healthy.
type HealthReport struct {
	GeneratedAt time.Time `json:"generated_at"`

	MissingPaths          []string          `json:"missing_paths"`          // discovered on disk, no active entry
	PendingRescan         []string          `json:"pending_rescan"`         // active entry, but disk facts (hash/size/category/symbols/deps) drifted since last Scan()
	OrphanedEntries       []string          `json:"orphaned_entries"`       // entry_id, Status==missing
	StaleReviews          []string          `json:"stale_reviews"`          // entry_id, Hash != LastReviewedHash (independently re-derived)
	SchemaIssues          []SchemaIssue     `json:"schema_issues"`          // reviewed entry missing a required field
	ClassificationIssues  []string          `json:"classification_issues"`  // entry_id, uncategorized or unmoduled
	DanglingRelationships []string          `json:"dangling_relationships"` // entry_id, RelatedFiles references a nonexistent entry
	DependencyIssues      []DependencyIssue `json:"dependency_issues"`      // ambiguous import resolution (graph.go)

	PendingClassificationCount int `json:"pending_classification_count"`

	// LastScanAt/LastFullScanAt are read from the derived scan-state cache
	// (zero value if Scan/ScanFull has never run for this project) — Health
	// itself always re-derives ground truth regardless of these, they are
	// reported only so a caller can explain *why* something is stale.
	LastScanAt       time.Time `json:"last_scan_at,omitempty"`
	LastFullScanAt   time.Time `json:"last_full_scan_at,omitempty"`
	StalenessReasons []string  `json:"staleness_reasons,omitempty"`

	Healthy bool `json:"healthy"`
}

// Health re-derives ground truth (re-scans without writing, exactly like
// the old Validate) and layers every additional check over the stored
// entry set. StaleReviews is recomputed independently from stored
// Hash/LastReviewedHash/ReviewStatus (via Entry.Stale()) rather than
// trusted from the scan step, so a health check catches the review-
// invalidation bug class even if a future change to the scan path
// reintroduces it.
func (s *Service) Health(projectID string) (HealthReport, error) {
	root, err := s.root(projectID)
	if err != nil {
		return HealthReport{}, err
	}
	cfg, err := loadConfig(root)
	if err != nil {
		return HealthReport{}, err
	}
	// Health passes the persisted fingerprint cache as a read-only speed
	// hint (a matching size+mtime means the cached hash is trustworthy) but
	// never persists what scan() returns — Health must never look
	// "healthier" or "less healthy" merely because it ran, and it must
	// never depend on Scan() having run first.
	scanState, err := loadScanState(root)
	if err != nil {
		return HealthReport{}, err
	}
	result, err := scan(root, cfg, scanState.Fingerprints, false)
	if err != nil {
		return HealthReport{}, err
	}
	entries, err := loadAllEntries(root)
	if err != nil {
		return HealthReport{}, err
	}

	byPath := make(map[string]Entry, len(entries))
	byID := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byID[e.EntryID] = e
		if e.Status == StatusActive {
			byPath[e.Path] = e
		}
	}

	report := HealthReport{
		GeneratedAt:                time.Now().UTC(),
		PendingClassificationCount: len(result.Pending),
		LastScanAt:                 scanState.LastScanAt,
		LastFullScanAt:             scanState.LastFullScanAt,
	}

	for _, d := range result.Files {
		e, ok := byPath[d.RelPath]
		if !ok {
			report.MissingPaths = append(report.MissingPaths, d.RelPath)
			continue
		}
		if discoveryDiffersFromEntry(e, d) {
			report.PendingRescan = append(report.PendingRescan, d.RelPath)
		}
	}

	for _, e := range entries {
		if e.Status == StatusMissing {
			report.OrphanedEntries = append(report.OrphanedEntries, e.EntryID)
			continue
		}
		if e.Status != StatusActive {
			continue
		}
		if e.Stale() {
			report.StaleReviews = append(report.StaleReviews, e.EntryID)
		}
		if e.ReviewStatus == ReviewReviewed {
			if strings.TrimSpace(e.Description) == "" {
				report.SchemaIssues = append(report.SchemaIssues, SchemaIssue{
					EntryID: e.EntryID, Path: e.Path, Field: "description", Problem: "reviewed entry has no description",
				})
			}
			if len(e.Responsibilities) == 0 {
				report.SchemaIssues = append(report.SchemaIssues, SchemaIssue{
					EntryID: e.EntryID, Path: e.Path, Field: "responsibilities", Problem: "reviewed entry has no responsibilities",
				})
			}
			if e.Criticality == "" {
				report.SchemaIssues = append(report.SchemaIssues, SchemaIssue{
					EntryID: e.EntryID, Path: e.Path, Field: "criticality", Problem: "reviewed entry has no criticality",
				})
			}
		}
		if e.Category == "" || e.Category == "uncategorized" || e.Module == "" {
			report.ClassificationIssues = append(report.ClassificationIssues, e.EntryID)
		}
		for _, rel := range e.RelatedFiles {
			if _, ok := byID[rel]; !ok {
				report.DanglingRelationships = append(report.DanglingRelationships, e.EntryID)
				break
			}
		}
	}

	_, depIssues := BuildGraph(entries)
	report.DependencyIssues = depIssues

	report.Healthy = len(report.MissingPaths) == 0 &&
		len(report.PendingRescan) == 0 &&
		len(report.OrphanedEntries) == 0 &&
		len(report.StaleReviews) == 0 &&
		len(report.SchemaIssues) == 0 &&
		len(report.ClassificationIssues) == 0 &&
		len(report.DanglingRelationships) == 0 &&
		len(report.DependencyIssues) == 0 &&
		report.PendingClassificationCount == 0

	if !report.Healthy {
		report.StalenessReasons = healthStalenessReasons(report)
	}

	return report, nil
}

// healthStalenessReasons turns the report's already-computed issue lists
// into short human-readable explanations of why the registry is unhealthy —
// the plan's "motivo de desatualização," derived rather than duplicated.
func healthStalenessReasons(r HealthReport) []string {
	var reasons []string
	if n := len(r.MissingPaths); n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d file(s) on disk have no registry entry yet", n))
	}
	if n := len(r.PendingRescan); n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d entrie(s) are out of date with their file on disk — run a scan", n))
	}
	if n := len(r.OrphanedEntries); n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d entrie(s) reference a file no longer on disk", n))
	}
	if n := len(r.StaleReviews); n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d review(s) are invalidated by a content change", n))
	}
	if n := len(r.SchemaIssues); n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d reviewed entrie(s) are missing a required field", n))
	}
	if n := len(r.ClassificationIssues); n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d entrie(s) are uncategorized or unmoduled", n))
	}
	if n := len(r.DanglingRelationships); n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d entrie(s) reference a related file that no longer exists", n))
	}
	if n := len(r.DependencyIssues); n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d dependency reference(s) are ambiguous", n))
	}
	if r.PendingClassificationCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d file(s) are pending classification", r.PendingClassificationCount))
	}
	return reasons
}
