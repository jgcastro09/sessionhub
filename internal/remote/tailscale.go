package remote

import (
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
)

var (
	tailscaleV4 = netip.MustParsePrefix("100.64.0.0/10")
	tailscaleV6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

func IsTailscaleIP(address netip.Addr) bool {
	return tailscaleV4.Contains(address) || tailscaleV6.Contains(address)
}

// NetworkAddresses describes the interfaces Remote Mode can use. LAN is
// always available; Tailscale is simply an optional additional route.
type NetworkAddresses struct {
	LocalIPs     []string
	TailscaleIPs []string
}

func LocalNetworkAddresses() NetworkAddresses {
	assigned, err := localAddresses()
	if err != nil {
		return NetworkAddresses{}
	}
	var result NetworkAddresses
	for address := range assigned {
		switch {
		case IsTailscaleIP(address):
			result.TailscaleIPs = append(result.TailscaleIPs, address.String())
		case address.IsPrivate() && !address.IsLoopback():
			result.LocalIPs = append(result.LocalIPs, address.String())
		}
	}
	sort.Strings(result.LocalIPs)
	sort.Strings(result.TailscaleIPs)
	return result
}

func ValidateListenAddress(address string) (netip.AddrPort, error) {
	value, err := netip.ParseAddrPort(address)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("remote listen address must be IP:port: %w", err)
	}
	if value.Port() == 0 {
		return netip.AddrPort{}, fmt.Errorf("remote listen port cannot be zero")
	}
	if !isTrustedRemoteAddress(value.Addr()) {
		return netip.AddrPort{}, fmt.Errorf("remote host must bind a local-network or Tailscale address, got %s", value.Addr())
	}
	assigned, err := localAddresses()
	if err != nil {
		return netip.AddrPort{}, err
	}
	if !assigned[value.Addr()] {
		return netip.AddrPort{}, fmt.Errorf("remote address %s is not assigned to this host", value.Addr())
	}
	return value, nil
}

func ValidateRemoteAddress(address string) (netip.AddrPort, error) {
	value, err := netip.ParseAddrPort(address)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("remote address must be IP:port: %w", err)
	}
	if !isTrustedRemoteAddress(value.Addr()) {
		return netip.AddrPort{}, fmt.Errorf("remote client may connect only to a local-network or Tailscale address")
	}
	return value, nil
}

func isTrustedRemoteAddress(address netip.Addr) bool {
	address = address.Unmap()
	return address.IsLoopback() || address.IsPrivate() || IsTailscaleIP(address)
}

func localAddresses() (map[netip.Addr]bool, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	out := make(map[netip.Addr]bool)
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			text := address.String()
			if index := strings.LastIndexByte(text, '/'); index >= 0 {
				text = text[:index]
			}
			if zone := strings.LastIndexByte(text, '%'); zone >= 0 {
				text = text[:zone]
			}
			if parsed, err := netip.ParseAddr(text); err == nil {
				out[parsed.Unmap()] = true
			}
		}
	}
	return out, nil
}
