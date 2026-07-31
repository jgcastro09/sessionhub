package app

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/jgcastro09/sessionhub/internal/config"
	"github.com/jgcastro09/sessionhub/internal/remote"
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
	Port         int
	BindMode     config.WebBindMode
	PairingCode  string
	NeedsPairing bool
	// LocalURL always works from this machine — loopback bypasses pairing
	// and bind-mode checks entirely (see internal/webserver/auth.go).
	LocalURL string
	// NetworkURLs are the http://<ip>:<port> links other devices can
	// actually reach given the current bind mode: LAN IPs when local
	// pairing is allowed, Tailscale IPs when Tailscale is allowed.
	NetworkURLs []string
}

func (a *App) WebPanelStatus() WebPanelStatus {
	a.webMu.Lock()
	settings := a.webSettings
	server := a.webServer
	a.webMu.Unlock()
	status := WebPanelStatus{
		Enabled: settings.Enabled, BindMode: settings.BindMode, Port: a.webPort(),
		Active:       server != nil,
		NeedsPairing: settings.BindMode == config.WebBindLocal || settings.BindMode == config.WebBindBoth,
	}
	if server != nil {
		status.Endpoint = server.Address()
		status.LocalURL = webPanelURL("127.0.0.1", status.Port)
		status.NetworkURLs = webPanelNetworkURLs(status.Port, settings.BindMode)
		if status.NeedsPairing {
			status.PairingCode = server.PairingCode()
		}
	}
	return status
}

func webPanelNetworkURLs(port int, mode config.WebBindMode) []string {
	addresses := remote.LocalNetworkAddresses()
	var urls []string
	if mode == config.WebBindLocal || mode == config.WebBindBoth {
		for _, ip := range addresses.LocalIPs {
			urls = append(urls, webPanelURL(ip, port))
		}
	}
	if mode == config.WebBindTailscale || mode == config.WebBindBoth {
		for _, ip := range addresses.TailscaleIPs {
			urls = append(urls, webPanelURL(ip, port))
		}
	}
	return urls
}

func webPanelURL(ip string, port int) string {
	if strings.Contains(ip, ":") {
		return fmt.Sprintf("http://[%s]:%d", ip, port)
	}
	return fmt.Sprintf("http://%s:%d", ip, port)
}

// OpenWebPanel launches the system's default browser at this machine's own
// loopback URL — the one link that always works regardless of bind mode or
// pairing, since the person pressing the keybinding is sitting at this
// computer.
func (a *App) OpenWebPanel() error {
	status := a.WebPanelStatus()
	if !status.Active {
		return fmt.Errorf("Web Panel isn't running — enable it with w first")
	}
	return openInBrowser(status.LocalURL)
}

func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
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
