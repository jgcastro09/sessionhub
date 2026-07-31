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

func TestControllerReadsExecutorStatusFromHost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host, err := Listen(ctx, "127.0.0.1:0", remoteTestBackend{statuses: []ExecutorStatus{{ExecutorID: "codex", LoginKnown: true, Activated: true}}}, Device{Name: "target", Version: "0.3.31"})
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
	controller, err := ConnectController(context.Background(), Device{Name: "target", Address: name, Port: port}, "controller", "0.3.31")
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	statuses, err := controller.ExecutorStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || !statuses[0].Activated || statuses[0].ExecutorID != "codex" {
		t.Fatalf("unexpected remote executor status: %#v", statuses)
	}
}

func TestControllerOutlivesSetupTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host, err := Listen(ctx, "127.0.0.1:0", remoteTestBackend{}, Device{Name: "target", Version: "0.3.32"})
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
	setup, stopSetup := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer stopSetup()
	controller, err := ConnectController(setup, Device{Name: "target", Address: name, Port: port}, "controller", "0.3.32")
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	<-setup.Done()
	if _, err := controller.Projects(context.Background()); err != nil {
		t.Fatalf("controller should survive setup timeout: %v", err)
	}
}

func TestControllerNavigationMirrorsOnHostAndCanBeRevoked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host, err := Listen(ctx, "127.0.0.1:0", remoteTestBackend{}, Device{Name: "target", Version: "0.3.35"})
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
	controller, err := ConnectController(context.Background(), Device{Name: "target", Address: name, Port: port}, "controller", "0.3.35")
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	view := ViewState{Section: "Projects", ProjectID: "project-1", ExecutorID: "codex", TerminalFocused: true}
	if err := controller.Navigate(context.Background(), view); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for host.Status().View != view && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := host.Status().View; got != view {
		t.Fatalf("host view = %#v, want %#v", got, view)
	}
	if !host.Revoke() {
		t.Fatal("expected active controller to be revoked")
	}
	for controller.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if controller.Err() == nil {
		t.Fatal("controller should observe host revocation")
	}
	for host.Status().Active && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if host.Status().Active {
		t.Fatal("host should release controller after revocation")
	}
}

type remoteTestBackend struct{ statuses []ExecutorStatus }

func (remoteTestBackend) RemoteProjects(context.Context) ([]domain.Project, error) { return nil, nil }
func (remoteTestBackend) RemoteExecutors(context.Context) ([]domain.ExecutorConfig, error) {
	return nil, nil
}
func (b remoteTestBackend) RemoteExecutorStatuses(context.Context) ([]ExecutorStatus, error) {
	return b.statuses, nil
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
