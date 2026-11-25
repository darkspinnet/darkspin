package udp

import (
	"errors"
	"fmt"
	"net"
)

// RetireGameplayTransport removes only the captured generation. Endpoint-owned
// worker queues span generations and must not be cancelled by an old snapshot.
// The gameplay admission boundary rejects any new session for a banned account.
func (e *SharedServer) RetireGameplayTransport(address string, generation uint64) (bool, error) {
	if generation == 0 {
		return false, errors.New("invalid gameplay transport generation")
	}
	remote, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return false, fmt.Errorf("transportAddress: %w", err)
	}
	if remote.IP == nil || remote.Port <= 0 || e.raknet == nil {
		return false, errors.New("invalid gameplay transport endpoint")
	}
	isRetired := e.raknet.RetirePeerGeneration(remote, generation)
	return isRetired, nil
}
