package app

import (
	"fmt"

	"github.com/jgcastro09/sessionhub/internal/config"
	"github.com/jgcastro09/sessionhub/internal/webserver"
)

// DefaultWebPanelPort is used whenever WebPanelSettings.Port is zero. Unlike
// Remote Mode's TCP protocol (found via mDNS-style discovery, so an
// ephemeral port is fine) the web panel is a URL a person types or bookmarks
// on their phone, so it needs to stay put across restarts.
const DefaultWebPanelPort = 8420

// StartWebPanel starts the HTTP web panel if WebPanelSettings.Enabled. Like
// Remote Mode, a bind failure (e.g. the port is already taken) is left to
// the caller to decide whether it's fatal — App.New treats it as a warning.
func (a *App) StartWebPanel() error {
	a.webMu.Lock()
	defer a.webMu.Unlock()
	if !a.webSettings.Enabled {
		return nil
	}
	if a.webServer != nil {
		return nil
	}
	server, err := webserver.Listen(a.ctx, fmt.Sprintf("0.0.0.0:%d", a.webPort()), a, a.webSettings.BindMode)
	if err != nil {
		return err
	}
	a.webServer = server
	return nil
}

func (a *App) webPort() int {
	if a.webSettings.Port > 0 {
		return a.webSettings.Port
	}
	return DefaultWebPanelPort
}

// WebPanelStatus reports the web panel's current state for the Settings
// screen: whether it's running, where, which bind mode is active, and (for
// WebBindLocal/WebBindBoth) the pairing code a phone needs to type in once.
type WebPanelStatus struct {
	Enabled      bool
	Active       bool
	Endpoint     string
	BindMode     config.WebBindMode
	PairingCode  string
	NeedsPairing bool
}

func (a *App) WebPanelStatus() WebPanelStatus {
	a.webMu.Lock()
	settings := a.webSettings
	server := a.webServer
	a.webMu.Unlock()
	status := WebPanelStatus{
		Enabled: settings.Enabled, BindMode: settings.BindMode,
		Active:       server != nil,
		NeedsPairing: settings.BindMode == config.WebBindLocal || settings.BindMode == config.WebBindBoth,
	}
	if server != nil {
		status.Endpoint = server.Address()
		if status.NeedsPairing {
			status.PairingCode = server.PairingCode()
		}
	}
	return status
}

// SetWebPanelEnabled starts or stops the web panel, mirroring
// SetRemoteEnabled: stopping ends every open connection rather than leaving
// a hidden HTTP service reachable.
func (a *App) SetWebPanelEnabled(enabled bool) error {
	a.webMu.Lock()
	next := a.webSettings
	if next.Enabled == enabled {
		a.webMu.Unlock()
		return nil
	}
	next.Enabled = enabled
	if err := config.SaveWebPanelSettings(a.Paths.WebSettings, next); err != nil {
		a.webMu.Unlock()
		return err
	}
	a.webSettings = next
	a.webMu.Unlock()
	if !enabled {
		return a.StopWebPanel()
	}
	return a.StartWebPanel()
}

// SetWebBindMode changes which networks may reach the panel and restarts it
// so the new mode takes effect immediately.
func (a *App) SetWebBindMode(mode config.WebBindMode) error {
	a.webMu.Lock()
	next := a.webSettings
	next.BindMode = mode
	if err := config.SaveWebPanelSettings(a.Paths.WebSettings, next); err != nil {
		a.webMu.Unlock()
		return err
	}
	a.webSettings = next
	running := a.webServer != nil
	a.webMu.Unlock()
	if !running {
		return nil
	}
	if err := a.StopWebPanel(); err != nil {
		return err
	}
	return a.StartWebPanel()
}

// RegenerateWebPairingCode invalidates the current pairing code (devices
// already paired keep working) and returns the new one, or "" if the panel
// isn't running.
func (a *App) RegenerateWebPairingCode() string {
	a.webMu.Lock()
	server := a.webServer
	a.webMu.Unlock()
	if server == nil {
		return ""
	}
	return server.RegeneratePairingCode()
}

func (a *App) StopWebPanel() error {
	a.webMu.Lock()
	server := a.webServer
	a.webServer = nil
	a.webMu.Unlock()
	if server == nil {
		return nil
	}
	return server.Close()
}
