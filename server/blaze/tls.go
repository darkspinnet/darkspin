package blaze

import (
	"crypto/tls"
	"fmt"
)

// LoadTLSConfig loads an operator-managed certificate for Blaze. TLS 1.2 is
// the minimum supported version; the legacy C++ SSLv3/RC4 configuration is not
// enabled by this server.
func LoadTLSConfig(certificatePath, privateKeyPath string) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(certificatePath, privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("tlsKeypair: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
