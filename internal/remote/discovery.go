package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	discoveryPort     = 47685
	discoveryInterval = 3 * time.Second
	discoveryTTL      = 12 * time.Second
)

// Device is a SessionHub host found on the current LAN or tailnet. Address
// is deliberately the source address observed by the receiver, never a
// hostname supplied by the peer.
type Device struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Version string    `json:"version"`
	Address string    `json:"address"`
	Port    int       `json:"port"`
	Network string    `json:"network"`
	SeenAt  time.Time `json:"seen_at"`
	Online  bool      `json:"online"`
}

// ExecutorStatus is intentionally metadata only. Login profiles and secret
// environment values always remain on the controlled computer.
type ExecutorStatus struct {
	ExecutorID string `json:"executor_id"`
	LoginKnown bool   `json:"login_known"`
	Activated  bool   `json:"activated"`
	Live       bool   `json:"live"`
}

func (d Device) Endpoint() string { return net.JoinHostPort(d.Address, itoa(d.Port)) }

type announcement struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Port    int    `json:"port"`
}

// Discovery advertises this SessionHub and retains only recently observed
// peers. It uses LAN broadcast plus unicast probes to Tailscale peers; no
// Internet service, daemon, or system scheduler is involved.
type Discovery struct {
	conn   *net.UDPConn
	self   Device
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
	peers  map[string]Device
	wg     sync.WaitGroup
}

func StartDiscovery(parent context.Context, name, version, seed string, port int) (*Discovery, error) {
	if name == "" {
		name, _ = os.Hostname()
	}
	if name == "" {
		name = "SessionHub"
	}
	ctx, cancel := context.WithCancel(parent)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: discoveryPort})
	if err != nil {
		cancel()
		return nil, err
	}
	hash := sha256.Sum256([]byte(name + "\x00" + seed))
	d := &Discovery{
		conn: conn, ctx: ctx, cancel: cancel, peers: make(map[string]Device),
		self: Device{ID: hex.EncodeToString(hash[:8]), Name: name, Version: version, Port: port},
	}
	d.wg.Add(2)
	go d.receive()
	go d.announceLoop()
	return d, nil
}

func (d *Discovery) Self() Device { return d.self }

func (d *Discovery) Devices() []Device {
	now := time.Now()
	d.mu.Lock()
	for id, device := range d.peers {
		if now.Sub(device.SeenAt) > discoveryTTL {
			delete(d.peers, id)
		}
	}
	out := make([]Device, 0, len(d.peers))
	for _, device := range d.peers {
		device.Online = true
		out = append(out, device)
	}
	d.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Address < out[j].Address
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func (d *Discovery) receive() {
	defer d.wg.Done()
	buffer := make([]byte, 2048)
	for {
		_ = d.conn.SetReadDeadline(time.Now().Add(time.Second))
		n, source, err := d.conn.ReadFromUDP(buffer)
		if err != nil {
			if d.ctx.Err() != nil {
				return
			}
			continue
		}
		var message announcement
		if json.Unmarshal(buffer[:n], &message) != nil || message.Type != "sessionhub-discovery-v1" ||
			message.ID == "" || message.ID == d.self.ID || message.Port < 1 || message.Port > 65535 {
			continue
		}
		address, ok := netip.AddrFromSlice(source.IP)
		if !ok || !isTrustedRemoteAddress(address) {
			continue
		}
		network := "LAN"
		if IsTailscaleIP(address.Unmap()) {
			network = "Tailscale"
		}
		d.mu.Lock()
		d.peers[message.ID] = Device{ID: message.ID, Name: message.Name, Version: message.Version, Address: address.Unmap().String(), Port: message.Port, Network: network, SeenAt: time.Now(), Online: true}
		d.mu.Unlock()
	}
}

func (d *Discovery) announceLoop() {
	defer d.wg.Done()
	ticker := time.NewTicker(discoveryInterval)
	defer ticker.Stop()
	for {
		d.announce()
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Discovery) announce() {
	data, _ := json.Marshal(announcement{Type: "sessionhub-discovery-v1", ID: d.self.ID, Name: d.self.Name, Version: d.self.Version, Port: d.self.Port})
	for _, destination := range append(lanBroadcastAddresses(), tailscalePeerAddresses()...) {
		_, _ = d.conn.WriteToUDP(data, &net.UDPAddr{IP: net.ParseIP(destination), Port: discoveryPort})
	}
}

// SameVersion deliberately requires an exact release match. Remote control
// shares PTY and protocol semantics, so allowing only a major/minor match
// would make mixed releases look supported when they are not.
func SameVersion(left, right string) bool {
	return strings.TrimPrefix(strings.TrimSpace(left), "v") == strings.TrimPrefix(strings.TrimSpace(right), "v") && strings.TrimSpace(left) != "" && strings.TrimSpace(right) != ""
}

func (d *Discovery) Close() error {
	d.cancel()
	err := d.conn.Close()
	d.wg.Wait()
	return err
}

func lanBroadcastAddresses() []string {
	seen := map[string]bool{"255.255.255.255": true}
	out := []string{"255.255.255.255"}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		for _, raw := range interfaceAddrs(iface) {
			ip, network, err := net.ParseCIDR(raw)
			if err != nil || ip.To4() == nil || IsTailscaleIP(netip.MustParseAddr(ip.String())) {
				continue
			}
			broadcast := make(net.IP, len(ip.To4()))
			for i := range broadcast {
				broadcast[i] = ip.To4()[i] | ^network.Mask[i]
			}
			if value := broadcast.String(); !seen[value] {
				seen[value] = true
				out = append(out, value)
			}
		}
	}
	return out
}

func interfaceAddrs(iface net.Interface) []string {
	addresses, err := iface.Addrs()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, address.String())
	}
	return out
}

// tailscalePeerAddresses intentionally has no hard dependency on Tailscale.
// When the CLI is installed, discovery probes each online peer directly;
// otherwise LAN discovery remains fully functional.
func tailscalePeerAddresses() []string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil {
		return nil
	}
	var status struct {
		Peer map[string]struct {
			Online       bool     `json:"Online"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Peer"`
	}
	if json.Unmarshal(data, &status) != nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, peer := range status.Peer {
		if !peer.Online {
			continue
		}
		for _, raw := range peer.TailscaleIPs {
			address, err := netip.ParseAddr(raw)
			if err == nil && IsTailscaleIP(address.Unmap()) && !seen[address.String()] {
				seen[address.String()] = true
				out = append(out, address.String())
			}
		}
	}
	return out
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}
