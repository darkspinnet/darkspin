package raknet

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

var responseIDSequence atomic.Uint64

func newResponseCommitCollector() *responseCommitCollector {
	responseID := responseIDSequence.Add(1)
	if responseID == 0 {
		responseID = responseIDSequence.Add(1)
	}
	return &responseCommitCollector{responseID: responseID}
}

// ResponseID identifies one managed write-and-commit boundary, including all
// application frames in a datagram. It does not depend on tracing being enabled.
// Unmanaged packets have no shared response scope and return zero.
func (p Packet) ResponseID() uint64 {
	if p.response == nil || p.response.collector == nil {
		return 0
	}
	return p.response.collector.responseID
}

func isConnectedDataDatagram(packet []byte) bool {
	return len(packet) > 0 && packet[0]&0x80 != 0 &&
		packet[0] != 0xa0 && packet[0] != 0xc0
}

// This lock serializes application response selection through transport commit.
// It is separate from outboundMu: gameplay can cancel a scheduled producer that
// needs outboundMu, while ACKs, replacements and scheduled writes keep advancing.
// No caller may wait for this lock while holding outboundMu or the server mutex.
func lockPeerApplicationResponse(ctx context.Context, currentPeer *peer) error {
	if currentPeer == nil {
		return errors.New("application response peer unavailable")
	}
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("applicationContext: %w", err)
	}
	if currentPeer.applicationResponseMu.TryLock() {
		return nil
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("applicationWait: %w", ctx.Err())
		case <-ticker.C:
			if currentPeer.applicationResponseMu.TryLock() {
				return nil
			}
		}
	}
}
