package app

import (
	"context"

	"github.com/jgcastro09/sessionhub/internal/gitstate"
	"github.com/jgcastro09/sessionhub/internal/registry"
)

// These bridge internal/registry.Service the same way tasks.go bridges
// internal/tasks.Service — see the note there about ctx.

// EnsureRegistryWatcher (idempotent) reconciles the running Fase 1.5
// filesystem watcher for projectID against its current Config.Watch policy:
// starts one if the policy is now enabled and none is running, stops one if
// the policy is now disabled and one is running. Call this after attaching
// a new project or saving registry config so a person never has to restart
// the app for a watch-policy change to take effect; App.New already calls
// it for every already-attached project at startup.
func (a *App) EnsureRegistryWatcher(projectID string) {
	a.registryWatchersMu.Lock()
	defer a.registryWatchersMu.Unlock()
	existing, running := a.registryWatchers[projectID]

	cfg, err := a.Registry.LoadConfig(projectID)
	if err != nil {
		return
	}
	if !cfg.Watch.Enabled {
		if running {
			_ = existing.Close()
			delete(a.registryWatchers, projectID)
		}
		return
	}
	if running {
		return
	}
	w, err := a.Registry.StartWatcher(projectID)
	if err != nil || w == nil {
		return // a transient error never blocks the app
	}
	a.registryWatchers[projectID] = w
}

func (a *App) stopRegistryWatchers() {
	a.registryWatchersMu.Lock()
	watchers := a.registryWatchers
	a.registryWatchers = nil
	a.registryWatchersMu.Unlock()
	for _, w := range watchers {
		_ = w.Close()
	}
}

func (a *App) WebRegistryConfigGet(ctx context.Context, projectID string) (registry.Config, error) {
	return a.Registry.LoadConfig(projectID)
}

func (a *App) WebRegistryConfigPut(ctx context.Context, projectID string, cfg registry.Config) error {
	if err := a.Registry.SaveConfig(projectID, cfg); err != nil {
		return err
	}
	a.EnsureRegistryWatcher(projectID)
	return nil
}

func (a *App) WebRegistryTaxonomyGet(ctx context.Context, projectID string) (registry.Taxonomy, error) {
	return a.Registry.LoadTaxonomy(projectID)
}

func (a *App) WebRegistryTaxonomyPut(ctx context.Context, projectID string, tax registry.Taxonomy) error {
	return a.Registry.SaveTaxonomy(projectID, tax)
}

func (a *App) WebRegistryList(ctx context.Context, projectID string) ([]registry.Entry, error) {
	return a.Registry.List(projectID, false)
}

func (a *App) WebRegistryGet(ctx context.Context, projectID, entryID string) (registry.Entry, error) {
	return a.Registry.Get(projectID, entryID)
}

// WebRegistrySearch ensures freshness (Fase 1.5) before searching:
// best-effort, since a transient reconciliation failure should never turn a
// working search into a hard failure — the search still runs against
// whatever is currently on disk either way.
func (a *App) WebRegistrySearch(ctx context.Context, projectID string, q registry.SearchQuery) ([]registry.SearchResult, int, error) {
	_, _ = a.Registry.EnsureFresh(projectID)
	return a.Registry.Search(projectID, q)
}

// WebRegistrySemanticSearch falls back to full-text search on any semantic
// search failure — model still downloading, engine failed to start, a
// transient error. The Web Panel should never show a hard failure just
// because the local embedding engine isn't ready yet.
func (a *App) WebRegistrySemanticSearch(ctx context.Context, projectID, query string, limit int) ([]registry.SearchResult, error) {
	results, err := a.Registry.SemanticSearch(ctx, projectID, query, limit)
	if err != nil {
		results, _, searchErr := a.Registry.Search(projectID, registry.SearchQuery{Text: query, Limit: limit})
		return results, searchErr
	}
	return results, nil
}

func (a *App) WebRegistryScan(ctx context.Context, projectID string) ([]registry.Entry, error) {
	return a.Registry.Scan(projectID)
}

func (a *App) WebRegistryHealth(ctx context.Context, projectID string) (registry.HealthReport, error) {
	return a.Registry.Health(projectID)
}

func (a *App) WebRegistryReview(ctx context.Context, projectID, entryID string, input registry.ReviewInput) (registry.Entry, error) {
	return a.Registry.Review(projectID, entryID, input)
}

func (a *App) WebRegistrySource(ctx context.Context, projectID, entryID string) (string, error) {
	_, _ = a.Registry.EnsureFresh(projectID)
	return a.Registry.ReadSource(projectID, entryID)
}

func (a *App) WebRegistryEnsureFresh(ctx context.Context, projectID string) (registry.EnsureFreshResult, error) {
	return a.Registry.EnsureFresh(projectID)
}

func (a *App) WebRegistrySourceHistory(ctx context.Context, projectID, entryID string, limit int) ([]gitstate.FileRevision, error) {
	return a.Registry.SourceHistory(ctx, projectID, entryID, limit)
}

func (a *App) WebRegistrySourceAtRevision(ctx context.Context, projectID, entryID, ref string) (string, error) {
	return a.Registry.SourceAtRevision(ctx, projectID, entryID, ref)
}

func (a *App) WebRegistryGitStatus(ctx context.Context, projectID string) (registry.GitCorrelation, error) {
	return a.Registry.GitStatus(ctx, projectID)
}

func (a *App) WebRegistryGraph(ctx context.Context, projectID string) (registry.Graph, []registry.DependencyIssue, error) {
	_, _ = a.Registry.EnsureFresh(projectID)
	return a.Registry.Graph(projectID)
}

func (a *App) WebRegistryEntryGraph(ctx context.Context, projectID, entryID string, depth, limit int) (registry.Graph, error) {
	_, _ = a.Registry.EnsureFresh(projectID)
	return a.Registry.EntryGraph(projectID, entryID, depth, limit)
}

func (a *App) WebRegistryPending(ctx context.Context, projectID string) ([]registry.PendingFile, error) {
	return a.Registry.Pending(projectID)
}

func (a *App) WebRegistryReviewQueue(ctx context.Context, projectID string, limit, offset int) ([]registry.Entry, int, error) {
	return a.Registry.ReviewQueue(projectID, limit, offset)
}

func (a *App) WebRegistryStats(ctx context.Context, projectID string) (registry.Stats, error) {
	return a.Registry.Stats(projectID)
}
