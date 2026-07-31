package remote

import (
	"testing"
	"time"
)

func TestDisablingTailscaleKeepsLANDiscovery(t *testing.T) {
	now := time.Now()
	d := &Discovery{tailscaleEnabled: true, peers: map[string]Device{
		"lan":  {ID: "lan", Name: "LAN", Network: "LAN", SeenAt: now},
		"tail": {ID: "tail", Name: "Tail", Network: "Tailscale", SeenAt: now},
	}}
	d.SetTailscaleEnabled(false)
	if d.TailscaleEnabled() {
		t.Fatal("tailscale should be disabled")
	}
	devices := d.Devices()
	if len(devices) != 1 || devices[0].ID != "lan" {
		t.Fatalf("LAN device must remain available: %#v", devices)
	}
}
