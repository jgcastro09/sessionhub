package registry

import (
	"context"
	"sort"

	"github.com/jgcastro09/sessionhub/internal/gitstate"
)

// Stats is the aggregate breakdown the Web Panel's Overview dashboard
// renders as charts/tiles.
type Stats struct {
	TotalFiles     int            `json:"total_files"`
	TotalLines     int            `json:"total_lines"`
	ByCategory     map[string]int `json:"by_category"`
	ByModule       map[string]int `json:"by_module"`
	ByArea         map[string]int `json:"by_area"`
	ByLanguage     map[string]int `json:"by_language"`
	ByKind         map[string]int `json:"by_kind"`
	ByReviewStatus map[string]int `json:"by_review_status"`
	ByCriticality  map[string]int `json:"by_criticality"`
}

// Stats aggregates counts across every active entry.
func (s *Service) Stats(projectID string) (Stats, error) {
	entries, err := s.List(projectID, false)
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{
		ByCategory: map[string]int{}, ByModule: map[string]int{}, ByArea: map[string]int{},
		ByLanguage: map[string]int{}, ByKind: map[string]int{}, ByReviewStatus: map[string]int{},
		ByCriticality: map[string]int{},
	}
	for _, e := range entries {
		stats.TotalFiles++
		stats.TotalLines += e.LineCount
		stats.ByCategory[e.Category]++
		stats.ByModule[e.Module]++
		if e.Area != "" {
			stats.ByArea[e.Area]++
		}
		stats.ByLanguage[e.Language]++
		stats.ByKind[string(e.Kind)]++
		stats.ByReviewStatus[string(e.ReviewStatus)]++
		if e.Criticality != "" {
			stats.ByCriticality[string(e.Criticality)]++
		}
	}
	return stats, nil
}

// Graph returns the full computed relationship graph and any dependency
// resolution issues for a project.
func (s *Service) Graph(projectID string) (Graph, []DependencyIssue, error) {
	entries, err := s.List(projectID, false)
	if err != nil {
		return Graph{}, nil, err
	}
	g, issues := BuildGraph(entries)
	return g, issues, nil
}

// EntryGraph returns the bounded impact-analysis subgraph rooted at
// entryID.
func (s *Service) EntryGraph(projectID, entryID string, depth, limit int) (Graph, error) {
	entries, err := s.List(projectID, false)
	if err != nil {
		return Graph{}, err
	}
	g, _ := BuildGraph(entries)
	return ImpactAnalysis(g, entryID, depth, limit), nil
}

// ReviewQueue returns every needs_review entry, most-recently-touched
// first, paginated.
func (s *Service) ReviewQueue(projectID string, limit, offset int) ([]Entry, int, error) {
	entries, err := s.List(projectID, false)
	if err != nil {
		return nil, 0, err
	}
	var queue []Entry
	for _, e := range entries {
		if e.ReviewStatus == ReviewNeedsReview {
			queue = append(queue, e)
		}
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i].UpdatedAt.After(queue[j].UpdatedAt) })
	total := len(queue)
	if offset < 0 {
		offset = 0
	}
	if offset > len(queue) {
		offset = len(queue)
	}
	end := len(queue)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return queue[offset:end], total, nil
}

// SourceHistory returns an entry's Git commit history. Git is optional
// context, not a requirement (matching GitStatus) — any error (not a repo,
// git unavailable, file never committed) is treated as "no history," never
// a hard failure.
func (s *Service) SourceHistory(ctx context.Context, projectID, entryID string, limit int) ([]gitstate.FileRevision, error) {
	root, err := s.root(projectID)
	if err != nil {
		return nil, err
	}
	entry, err := s.Get(projectID, entryID)
	if err != nil {
		return nil, err
	}
	revisions, gitErr := gitstate.FileHistory(ctx, root, entry.Path, limit)
	if gitErr != nil {
		return nil, nil
	}
	return revisions, nil
}

// SourceAtRevision returns an entry's content as of a specific Git ref.
func (s *Service) SourceAtRevision(ctx context.Context, projectID, entryID, ref string) (string, error) {
	root, err := s.root(projectID)
	if err != nil {
		return "", err
	}
	entry, err := s.Get(projectID, entryID)
	if err != nil {
		return "", err
	}
	return gitstate.FileAtRevision(ctx, root, ref, entry.Path)
}
