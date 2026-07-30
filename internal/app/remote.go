package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/remote"
	"github.com/jgcastro09/sessionhub/internal/terminal"
)

func (a *App) StartRemoteHost(address string) error {
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()
	if a.remoteHost != nil {
		return fmt.Errorf("remote host is already listening on %s", a.remoteHost.Address())
	}
	host, err := remote.Listen(a.ctx, address, a, remote.Device{Name: a.remoteName()})
	if err != nil {
		return err
	}
	a.remoteHost = host
	return nil
}

// StartAutomaticRemote starts the in-process host on an ephemeral TCP port
// and announces it over local discovery. Both components stop with App.Close.
func (a *App) StartAutomaticRemote() error {
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()
	if a.remoteHost != nil {
		return nil
	}
	host, err := remote.Listen(a.ctx, ":0", a, remote.Device{Name: a.remoteName()})
	if err != nil {
		return err
	}
	_, rawPort, err := net.SplitHostPort(host.Address())
	if err != nil {
		_ = host.Close()
		return err
	}
	var port int
	if _, err := fmt.Sscanf(rawPort, "%d", &port); err != nil {
		_ = host.Close()
		return err
	}
	discovery, err := remote.StartDiscovery(a.ctx, a.remoteName(), a.Paths.Root, port)
	if err != nil {
		_ = host.Close()
		return err
	}
	a.remoteHost, a.remoteDiscovery = host, discovery
	return nil
}

func (a *App) StopRemoteHost() error {
	a.remoteMu.Lock()
	host := a.remoteHost
	discovery := a.remoteDiscovery
	a.remoteHost = nil
	a.remoteDiscovery = nil
	a.remoteMu.Unlock()
	var result error
	if discovery != nil {
		result = discovery.Close()
	}
	if host != nil {
		result = errors.Join(result, host.Close())
	}
	return result
}

func (a *App) RemoteDevices() []remote.Device {
	a.remoteMu.Lock()
	discovery := a.remoteDiscovery
	a.remoteMu.Unlock()
	if discovery == nil {
		return nil
	}
	return discovery.Devices()
}

func (a *App) RemoteHostStatus() remote.Status {
	a.remoteMu.Lock()
	host := a.remoteHost
	a.remoteMu.Unlock()
	if host == nil {
		return remote.Status{}
	}
	return host.Status()
}

func (a *App) remoteName() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "SessionHub"
	}
	return name
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

func (a *App) RemoteStartTerminal(ctx context.Context, sessionID, executorID string, width, height int) (domain.Instance, error) {
	_, instance, err := a.Executors.StartOrReuse(ctx, sessionID, executorID, width, height)
	return instance, err
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
