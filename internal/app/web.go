package app

import (
	"context"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

// These three read-only methods round out what internal/webserver.Backend
// needs beyond the Remote* methods in remote.go: the web panel is
// monitoring-only in v1, so it has no equivalents for the write-capable
// Remote* methods (RemoteStartTerminal, RemoteDecideApproval, etc).

func (a *App) WebQueue(ctx context.Context, sessionID string) ([]domain.QueueItem, error) {
	return a.Store.ListQueue(ctx, sessionID)
}

func (a *App) WebSchedules(ctx context.Context, sessionID string) ([]domain.Schedule, error) {
	return a.Store.ListSchedules(ctx, sessionID)
}

func (a *App) WebPipelines(ctx context.Context, sessionID string) ([]domain.Pipeline, error) {
	return a.Store.ListPipelines(ctx, sessionID)
}
