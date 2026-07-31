package app

import (
	"context"

	"github.com/jgcastro09/sessionhub/internal/registry"
)

// These bridge internal/registry.Service the same way tasks.go bridges
// internal/tasks.Service — see the note there about ctx.

func (a *App) WebRegistryList(ctx context.Context, projectID string) ([]registry.Entry, error) {
	return a.Registry.List(projectID, false)
}

func (a *App) WebRegistryGet(ctx context.Context, projectID, entryID string) (registry.Entry, error) {
	return a.Registry.Get(projectID, entryID)
}

func (a *App) WebRegistrySearch(ctx context.Context, projectID, query string, limit int) ([]registry.SearchResult, error) {
	return a.Registry.Search(projectID, query, limit)
}

// WebRegistrySemanticSearch falls back to lexical search on any semantic
// search failure — model still downloading, engine failed to start, a
// transient error — per plan 5.4 and section 9: the download "nunca bloqueia
// ... busca lexical enquanto está em andamento ou falha." The Web Panel
// should never show a hard failure just because the local engine isn't
// ready yet.
func (a *App) WebRegistrySemanticSearch(ctx context.Context, projectID, query string, limit int) ([]registry.SearchResult, error) {
	results, err := a.Registry.SemanticSearch(ctx, projectID, query, limit)
	if err != nil {
		return a.Registry.Search(projectID, query, limit)
	}
	return results, nil
}

func (a *App) WebRegistryScan(ctx context.Context, projectID string) ([]registry.Entry, error) {
	return a.Registry.Scan(projectID)
}

func (a *App) WebRegistryHealth(ctx context.Context, projectID string) (registry.CoverageReport, error) {
	return a.Registry.Validate(projectID)
}

func (a *App) WebRegistryReview(ctx context.Context, projectID, entryID string, input registry.ReviewInput) (registry.Entry, error) {
	return a.Registry.Review(projectID, entryID, input)
}

func (a *App) WebRegistrySource(ctx context.Context, projectID, entryID string) (string, error) {
	return a.Registry.ReadSource(projectID, entryID)
}

func (a *App) WebRegistryGitStatus(ctx context.Context, projectID string) (registry.GitCorrelation, error) {
	return a.Registry.GitStatus(ctx, projectID)
}
