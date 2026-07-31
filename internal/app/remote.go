package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/jgcastro09/sessionhub/internal/config"
	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/executor"
	"github.com/jgcastro09/sessionhub/internal/remote"
	"github.com/jgcastro09/sessionhub/internal/terminal"
)

func (a *App) StartRemoteHost(address string) error {
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()
	if a.remoteHost != nil {
		return fmt.Errorf("remote host is already listening on %s", a.remoteHost.Address())
	}
	host, err := remote.Listen(a.ctx, address, a, remote.Device{Name: a.remoteName(), Version: a.Version})
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
	host, err := remote.Listen(a.ctx, ":0", a, remote.Device{Name: a.remoteName(), Version: a.Version})
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
	discovery, err := remote.StartDiscovery(a.ctx, a.remoteName(), a.Version, a.Paths.Root, port, a.networkSettings.TailscaleEnabled)
	if err != nil {
		_ = host.Close()
		return err
	}
	a.remoteHost, a.remoteDiscovery = host, discovery
	return nil
}

type NetworkStatus struct {
	LocalIPs          []string
	TailscaleIPs      []string
	TailscaleDetected bool
	TailscaleEnabled  bool
}

func (a *App) RemoteNetworkStatus() NetworkStatus {
	a.remoteMu.Lock()
	settings := a.networkSettings
	a.remoteMu.Unlock()
	addresses := remote.LocalNetworkAddresses()
	return NetworkStatus{LocalIPs: addresses.LocalIPs, TailscaleIPs: addresses.TailscaleIPs, TailscaleDetected: len(addresses.TailscaleIPs) > 0, TailscaleEnabled: settings.TailscaleEnabled}
}

// SetTailscaleEnabled toggles only Tailscale discovery. The host keeps its
// LAN listener and local peer discovery active in both cases.
func (a *App) SetTailscaleEnabled(enabled bool) error {
	a.remoteMu.Lock()
	next := a.networkSettings
	next.TailscaleEnabled = enabled
	if err := config.SaveNetworkSettings(a.Paths.NetworkSettings, next); err != nil {
		a.remoteMu.Unlock()
		return err
	}
	a.networkSettings = next
	discovery := a.remoteDiscovery
	a.remoteMu.Unlock()
	if discovery != nil {
		discovery.SetTailscaleEnabled(enabled)
	}
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

func (a *App) RemoteExecutorStatuses(ctx context.Context) ([]remote.ExecutorStatus, error) {
	executors, err := a.Store.ListExecutors(ctx)
	if err != nil {
		return nil, err
	}
	sessions, err := a.Store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	statuses := make([]remote.ExecutorStatus, 0, len(executors))
	for _, cfg := range executors {
		live := false
		for _, session := range sessions {
			if a.Executors.IsActive(session.ID, cfg.ID) {
				live = true
				break
			}
		}
		statuses = append(statuses, remote.ExecutorStatus{ExecutorID: cfg.ID, LoginKnown: cfg.InstallDir != "", Activated: executor.HasLoginState(cfg), Live: live})
	}
	return statuses, nil
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
