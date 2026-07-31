package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNetworkSettingsDefaultsAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "network.json")
	settings, err := LoadNetworkSettings(path)
	if err != nil || !settings.TailscaleEnabled || !settings.RemoteEnabled {
		t.Fatalf("unexpected defaults: %#v, %v", settings, err)
	}
	if err := SaveNetworkSettings(path, NetworkSettings{RemoteEnabled: false, TailscaleEnabled: false}); err != nil {
		t.Fatal(err)
	}
	settings, err = LoadNetworkSettings(path)
	if err != nil || settings.TailscaleEnabled || settings.RemoteEnabled {
		t.Fatalf("unexpected persisted settings: %#v, %v", settings, err)
	}
}

func TestNetworkSettingsKeepsRemoteModeEnabledForOlderFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "network.json")
	if err := os.WriteFile(path, []byte(`{"tailscale_enabled":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadNetworkSettings(path)
	if err != nil || !settings.RemoteEnabled || settings.TailscaleEnabled {
		t.Fatalf("unexpected legacy settings: %#v, %v", settings, err)
	}
}
