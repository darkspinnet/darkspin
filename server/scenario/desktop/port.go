//go:build scenario

package desktop

import "fmt"

const CanonicalServerPort uint16 = 42127

// ValidatePort keeps the ordinary client's canonical endpoint. Development
// scenario runs cannot use a custom port or borrow an occupied server.
func ValidatePort(port uint16) error {
	if port != CanonicalServerPort {
		return fmt.Errorf("scenario requires canonical loopback server port %d; custom port %d is unsupported", CanonicalServerPort, port)
	}
	return nil
}
