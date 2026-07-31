package config

import (
	"path/filepath"
	"testing"
)

func TestNetworkSettingsDefaultsAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "network.json")
	settings, err := LoadNetworkSettings(path)
	if err != nil || !settings.TailscaleEnabled {
		t.Fatalf("unexpected defaults: %#v, %v", settings, err)
	}
	if err := SaveNetworkSettings(path, NetworkSettings{TailscaleEnabled: false}); err != nil {
		t.Fatal(err)
	}
	settings, err = LoadNetworkSettings(path)
	if err != nil || settings.TailscaleEnabled {
		t.Fatalf("unexpected persisted settings: %#v, %v", settings, err)
	}
}
