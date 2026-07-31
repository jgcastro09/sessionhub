package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WebBindMode controls which networks the web panel's HTTP listener accepts
// requests from.
type WebBindMode string

const (
	// WebBindTailscale accepts only requests arriving from a Tailscale
	// address — no pairing needed, since the tailnet is already the trust
	// boundary internal/remote relies on for its own protocol.
	WebBindTailscale WebBindMode = "tailscale"
	// WebBindLocal accepts LAN requests once paired with the short code
	// shown in the TUI.
	WebBindLocal WebBindMode = "local"
	// WebBindBoth accepts both: Tailscale addresses with no pairing, LAN
	// addresses once paired.
	WebBindBoth WebBindMode = "both"
)

// WebPanelSettings controls the optional HTTP web panel. A missing field
// keeps the backwards-compatible, disabled default when loading an older
// web.json file.
type WebPanelSettings struct {
	Enabled  bool        `json:"enabled"`
	BindMode WebBindMode `json:"bind_mode"`
	// Port is the TCP port to bind. Zero means "pick an ephemeral port",
	// matching how internal/remote's automatic mode binds ":0".
	Port int `json:"port"`
}

func DefaultWebPanelSettings() WebPanelSettings {
	return WebPanelSettings{Enabled: false, BindMode: WebBindTailscale, Port: 0}
}

func LoadWebPanelSettings(path string) (WebPanelSettings, error) {
	settings := DefaultWebPanelSettings()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("read web panel settings: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, fmt.Errorf("decode web panel settings: %w", err)
	}
	return settings, nil
}

func SaveWebPanelSettings(path string, settings WebPanelSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode web panel settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create web panel settings directory: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write web panel settings: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("save web panel settings: %w", err)
	}
	return nil
}
