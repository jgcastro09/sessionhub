package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// NetworkSettings intentionally contains only opt-in network transport
// preferences. The local LAN interface is always active for Remote Mode.
type NetworkSettings struct {
	TailscaleEnabled bool `json:"tailscale_enabled"`
}

func DefaultNetworkSettings() NetworkSettings { return NetworkSettings{TailscaleEnabled: true} }

func LoadNetworkSettings(path string) (NetworkSettings, error) {
	settings := DefaultNetworkSettings()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("read network settings: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, fmt.Errorf("decode network settings: %w", err)
	}
	return settings, nil
}

func SaveNetworkSettings(path string, settings NetworkSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode network settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create network settings directory: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write network settings: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("save network settings: %w", err)
	}
	return nil
}
