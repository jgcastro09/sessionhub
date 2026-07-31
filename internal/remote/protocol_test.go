package remote

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/terminal"
)

func TestFrameRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	want := Frame{ID: "1", Type: "terminal_input", InstanceID: "i", Sequence: 7, Payload: []byte(`{"data":"YQ=="}`)}
	if err := WriteFrame(&buffer, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(bufio.NewReader(&buffer))
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != want.Type || got.Sequence != want.Sequence || got.InstanceID != want.InstanceID {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestControllerConnectionIsExclusive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host, err := Listen(ctx, "127.0.0.1:0", remoteTestBackend{}, Device{Name: "target", Version: "0.3.30"})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	hostName, rawPort, err := net.SplitHostPort(host.Address())
	if err != nil {
		t.Fatal(err)
	}
	port, err := net.LookupPort("tcp", rawPort)
	if err != nil {
		t.Fatal(err)
	}
	device := Device{Name: "target", Address: hostName, Port: port, Network: "LAN"}
	first, err := ConnectController(context.Background(), device, "controller-one", "0.3.30")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	deadline := time.Now().Add(time.Second)
	for !host.Status().Active && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := host.Status(); !got.Active || got.Controller != "controller-one" {
		t.Fatalf("unexpected host status: %#v", got)
	}
	if _, err := ConnectController(context.Background(), device, "controller-two", "0.3.30"); err == nil {
		t.Fatal("second controller should be rejected")
	}
}

func TestControllerRejectsDifferentSessionHubVersion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host, err := Listen(ctx, "127.0.0.1:0", remoteTestBackend{}, Device{Name: "target", Version: "0.3.30"})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	name, rawPort, err := net.SplitHostPort(host.Address())
	if err != nil {
		t.Fatal(err)
	}
	port, err := net.LookupPort("tcp", rawPort)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConnectController(context.Background(), Device{Name: "target", Address: name, Port: port}, "controller", "0.3.29"); err == nil {
		t.Fatal("version mismatch should be rejected")
	}
}

func TestSameVersionRequiresExactRelease(t *testing.T) {
	if !SameVersion("v0.3.30", "0.3.30") {
		t.Fatal("same release should match")
	}
	if SameVersion("0.3.30", "0.3.29") {
		t.Fatal("different releases must not match")
	}
}

type remoteTestBackend struct{}

func (remoteTestBackend) RemoteSessions(context.Context) ([]domain.Session, error) { return nil, nil }
func (remoteTestBackend) RemoteExecutors(context.Context) ([]domain.ExecutorConfig, error) {
	return nil, nil
}
func (remoteTestBackend) RemoteStartTerminal(context.Context, string, string, int, int) (domain.Instance, error) {
	return domain.Instance{}, nil
}
func (remoteTestBackend) RemoteTerminal(string) (*terminal.Session, bool) { return nil, false }
func (remoteTestBackend) RemoteMetrics(context.Context, string) (domain.Metric, error) {
	return domain.Metric{}, nil
}
func (remoteTestBackend) RemoteLogs(context.Context, string, int) ([]domain.LogEntry, error) {
	return nil, nil
}
func (remoteTestBackend) RemoteCheckpoint(context.Context, string, string) (domain.Checkpoint, error) {
	return domain.Checkpoint{}, nil
}
func (remoteTestBackend) RemoteRunQueue(context.Context, string) error { return nil }
func (remoteTestBackend) RemoteDecideApproval(context.Context, string, string, string) (domain.Approval, error) {
	return domain.Approval{}, nil
}
func (remoteTestBackend) RemoteCancelWork(context.Context, string) error { return nil }

func TestTailscaleRanges(t *testing.T) {
	for _, value := range []string{"100.64.0.1", "100.127.255.254", "fd7a:115c:a1e0::1"} {
		if !IsTailscaleIP(netip.MustParseAddr(value)) {
			t.Fatalf("%s should be accepted", value)
		}
	}
	for _, value := range []string{"127.0.0.1", "192.168.1.1", "8.8.8.8", "::1"} {
		if IsTailscaleIP(netip.MustParseAddr(value)) {
			t.Fatalf("%s should be rejected", value)
		}
	}
}
