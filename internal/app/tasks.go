package app

import (
	"context"

	"github.com/jgcastro09/sessionhub/internal/events"
	"github.com/jgcastro09/sessionhub/internal/tasks"
)

// Subscribe backs the Web Panel's SSE endpoint (internal/webserver.Backend):
// a live feed of Task/Registry events for one project.
func (a *App) Subscribe(projectID string) (<-chan events.Event, func()) {
	return a.Events.Subscribe(projectID)
}

// These bridge internal/tasks.Service to the CLI, TUI, and Web Panel the
// same way RemoteXxx/WebXxx already bridge the rest of *App. ctx is accepted
// for interface consistency with the other Web* methods (and future
// cancellation) even though the underlying service is purely filesystem-bound
// today.

func (a *App) WebTasksList(ctx context.Context, projectID string, filter tasks.Filter) ([]tasks.Card, error) {
	return a.Tasks.List(projectID, filter)
}

func (a *App) WebTasksGet(ctx context.Context, projectID, taskID string) (tasks.Card, error) {
	return a.Tasks.Get(projectID, taskID)
}

func (a *App) WebTasksCreate(ctx context.Context, projectID string, input tasks.CreateInput) (tasks.Card, error) {
	return a.Tasks.Create(projectID, input)
}

func (a *App) WebTasksUpdate(ctx context.Context, projectID, taskID string, patch tasks.Patch) (tasks.Card, error) {
	return a.Tasks.Update(projectID, taskID, patch)
}

func (a *App) WebTasksSetStatus(ctx context.Context, projectID, taskID string, status tasks.Status) (tasks.Card, error) {
	return a.Tasks.SetStatus(projectID, taskID, status)
}

func (a *App) WebTasksAudit(ctx context.Context, projectID, taskID string) (tasks.AuditReport, error) {
	return a.Tasks.Audit(projectID, taskID)
}

// WebTasksImport parses a card draft written against the "Gerador de
// Cards" prompt contract and creates the resulting card when it validates.
func (a *App) WebTasksImport(ctx context.Context, projectID, text string) (tasks.Card, tasks.ImportResult, error) {
	return a.Tasks.Import(projectID, text)
}

// WebTasksClaims and friends expose the runtime-only "who is working this"
// state (plan 4.3): never written to .shproject/Git, shown in the Web Panel
// so multiple Executors can see and avoid stepping on each other.

func (a *App) WebTasksClaims(ctx context.Context, projectID string) ([]tasks.Claim, error) {
	return a.Tasks.ListClaims(projectID)
}

func (a *App) WebTasksClaim(ctx context.Context, projectID, taskID, executorID, terminalID string) ([]tasks.Claim, error) {
	return a.Tasks.Claim(projectID, taskID, executorID, terminalID)
}

func (a *App) WebTasksReleaseClaim(ctx context.Context, projectID, terminalID string) ([]tasks.Claim, error) {
	return a.Tasks.ReleaseClaim(projectID, terminalID)
}
