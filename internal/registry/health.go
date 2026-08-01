package registry

import (
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
	OrphanedEntries       []string          `json:"orphaned_entries"`       // entry_id, Status==missing
	StaleReviews          []string          `json:"stale_reviews"`          // entry_id, Hash != LastReviewedHash (independently re-derived)
	SchemaIssues          []SchemaIssue     `json:"schema_issues"`          // reviewed entry missing a required field
	ClassificationIssues  []string          `json:"classification_issues"`  // entry_id, uncategorized or unmoduled
	DanglingRelationships []string          `json:"dangling_relationships"` // entry_id, RelatedFiles references a nonexistent entry
	DependencyIssues      []DependencyIssue `json:"dependency_issues"`      // ambiguous import resolution (graph.go)

	PendingClassificationCount int `json:"pending_classification_count"`

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
	result, err := scan(root, cfg)
	if err != nil {
		return HealthReport{}, err
	}
	entries, err := loadAllEntries(root)
	if err != nil {
		return HealthReport{}, err
	}
	pending, err := loadPending(root)
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

	report := HealthReport{GeneratedAt: time.Now().UTC(), PendingClassificationCount: len(pending)}

	for _, d := range result.Files {
		if _, ok := byPath[d.RelPath]; !ok {
			report.MissingPaths = append(report.MissingPaths, d.RelPath)
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
		len(report.OrphanedEntries) == 0 &&
		len(report.StaleReviews) == 0 &&
		len(report.SchemaIssues) == 0 &&
		len(report.ClassificationIssues) == 0 &&
		len(report.DanglingRelationships) == 0 &&
		len(report.DependencyIssues) == 0 &&
		report.PendingClassificationCount == 0

	return report, nil
}
