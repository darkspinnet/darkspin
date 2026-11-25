// Package hostconfig owns the local server's durable configuration.
package hostconfig

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

type Settings struct {
	Port                uint16
	BindHost            string
	AdvertisedHost      string
	IsVersionLocked     bool
	IsSingleplayerOnly  bool
	IsChatStdoutEnabled bool
	JWTSecret           string
	JWTIssuer           string
	JWTAudience         string
	ContentPath         string
}

func Validate(req Settings) error {
	if req.Port == 0 || req.Port == 65535 {
		return errors.New("server port must be between 1 and 65534")
	}
	for _, host := range []string{req.BindHost, req.AdvertisedHost} {
		if host == "" {
			continue
		}
		address, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("hostParse: %w", err)
		}
		if !address.Is4() || !address.IsLoopback() {
			return errors.New("local server addresses must be loopback IPv4 addresses")
		}
	}
	if len(req.JWTSecret) < 32 || strings.TrimSpace(req.JWTIssuer) == "" || strings.TrimSpace(req.JWTAudience) == "" {
		return errors.New("local authentication settings are incomplete")
	}
	return nil
}
