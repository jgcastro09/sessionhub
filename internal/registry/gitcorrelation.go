package registry

import (
	"context"

	"github.com/jgcastro09/sessionhub/internal/gitstate"
)

// GitCorrelation is gitstate.State plus which Registry entries the working
// tree currently touches — read-only, per plan 5.3: it never alters the
// worktree, index, or refs, it only cross-references gitstate.Inspect's
// output against entries already on disk.
type GitCorrelation struct {
	Git            gitstate.State `json:"git"`
	ChangedEntries []string       `json:"changed_entries"` // entry_id, not path
}

// GitStatus correlates the project's current Git state with its Registry
// entries. A project outside a Git repository is not an error — Git is
// optional context, not a requirement — it just returns a zero State.
func (s *Service) GitStatus(ctx context.Context, projectID string) (GitCorrelation, error) {
	root, err := s.root(projectID)
	if err != nil {
		return GitCorrelation{}, err
	}
	entries, err := s.List(projectID, false)
	if err != nil {
		return GitCorrelation{}, err
	}
	state, err := gitstate.Inspect(ctx, root)
	if err != nil {
		if err == gitstate.ErrNotRepository {
			return GitCorrelation{}, nil
		}
		return GitCorrelation{}, err
	}
	changedPaths := make(map[string]bool, len(state.Files))
	for _, f := range state.Files {
		changedPaths[f.Path] = true
	}
	correlation := GitCorrelation{Git: state}
	for _, entry := range entries {
		if changedPaths[entry.Path] {
			correlation.ChangedEntries = append(correlation.ChangedEntries, entry.EntryID)
		}
	}
	return correlation, nil
}
