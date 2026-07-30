package remote

import (
	"bufio"
	"bytes"
	"net/netip"
	"testing"
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
