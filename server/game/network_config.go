package game

import (
	"fmt"
	"net/netip"
	"strings"
)

// NetworkHosts resolves listener defaults separately from client-visible IPv4
// endpoints. Build-103 endpoint fields carry an IPv4 integer, not a DNS name.
func (e *Config) NetworkHosts() (string, string, error) {
	advertisedHost := strings.TrimSpace(e.String(ConfigServerAdvertisedHost))
	advertisedAddress, err := netip.ParseAddr(advertisedHost)
	if err != nil {
		return "", "", fmt.Errorf("advertisedParse: %w", err)
	}
	if !advertisedAddress.Is4() || advertisedAddress.IsUnspecified() ||
		advertisedAddress.IsMulticast() || advertisedAddress == netip.MustParseAddr("255.255.255.255") {
		return "", "", fmt.Errorf("advertised host must be a unicast IPv4 address")
	}
	bindHost := strings.TrimSpace(e.String(ConfigServerBindHost))
	if bindHost == "" {
		bindHost = "127.0.0.1"
		if e.Bool(ConfigIsMultiplayerEnabled) ||
			e.Bool(ConfigServerIsDedicated) && !advertisedAddress.IsLoopback() {
			bindHost = "0.0.0.0"
		}
	}
	bindAddress, err := netip.ParseAddr(bindHost)
	if err != nil {
		return "", "", fmt.Errorf("bindParse: %w", err)
	}
	if !bindAddress.Is4() || bindAddress.IsMulticast() || bindAddress == netip.MustParseAddr("255.255.255.255") {
		return "", "", fmt.Errorf("bind host must be an IPv4 address or 0.0.0.0")
	}
	return bindAddress.String(), advertisedAddress.String(), nil
}
