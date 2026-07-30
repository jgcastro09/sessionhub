package app

import (
	"context"
	"fmt"

	"github.com/nodestage/sessionhub/internal/domain"
	"github.com/nodestage/sessionhub/internal/remote"
	"github.com/nodestage/sessionhub/internal/terminal"
)

func (a *App) StartRemoteHost(address string) error {
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()
	if a.remoteHost != nil {
		return fmt.Errorf("remote host is already listening on %s", a.remoteHost.Address())
	}
	host, err := remote.Listen(a.ctx, address, a)
	if err != nil {
		return err
	}
	a.remoteHost = host
	return nil
}

func (a *App) StopRemoteHost() error {
	a.remoteMu.Lock()
	host := a.remoteHost
	a.remoteHost = nil
	a.remoteMu.Unlock()
	if host == nil {
		return nil
	}
	return host.Close()
}

func (a *App) RemoteHostAddress() string {
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()
	if a.remoteHost == nil {
		return ""
	}
	return a.remoteHost.Address()
}

func (a *App) RemoteSessions(ctx context.Context) ([]domain.Session, error) {
	return a.Store.ListSessions(ctx)
}

func (a *App) RemoteExecutors(ctx context.Context) ([]domain.ExecutorConfig, error) {
	return a.Store.ListExecutors(ctx)
}

func (a *App) RemoteTerminal(instanceID string) (*terminal.Session, bool) {
	return a.Terminals.Get(instanceID)
}

func (a *App) RemoteMetrics(ctx context.Context, sessionID string) (domain.Metric, error) {
	return a.Store.AggregateMetrics(ctx, sessionID)
}

func (a *App) RemoteLogs(ctx context.Context, sessionID string, limit int) ([]domain.LogEntry, error) {
	return a.Store.ListLogs(ctx, sessionID, limit)
}

func (a *App) RemoteCheckpoint(ctx context.Context, sessionID, name string) (domain.Checkpoint, error) {
	if name == "" {
		name = "Remote checkpoint"
	}
	return a.Context.Checkpoint(ctx, sessionID, name, false)
}

func (a *App) RemoteRunQueue(ctx context.Context, sessionID string) error {
	return a.Automation.RunQueueOnce(ctx, sessionID)
}

func (a *App) RemoteDecideApproval(
	ctx context.Context,
	approvalID, actor, decision string,
) (domain.Approval, error) {
	return a.Store.DecideApproval(ctx, approvalID, actor, decision)
}

func (a *App) RemoteCancelWork(ctx context.Context, workID string) error {
	return a.Store.ResolveRecognizedWork(ctx, workID, domain.StateCanceled,
		"remote_cancel", nil, "canceled by remote controller")
}
