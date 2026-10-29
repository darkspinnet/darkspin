package gameplay

// A managed response can contain several application frames and polls before
// its final transport commit. Select pending bytes once across that whole scope.
// Batches appended after selection wait for the next response; no queued bytes
// are removed here, so a failed write remains retryable under a fresh response ID.
// The caller holds registry.mutex, and managed peer responses are serialized.
func (e *gameplayPeerSession) pendingPacketsForResponse(responseID uint64) ([][]byte, uint64) {
	if e == nil || (responseID != 0 && e.pendingPacketResponseID == responseID) {
		return nil, 0
	}
	packets, batchID := e.pendingPackets()
	if responseID != 0 && len(packets) != 0 {
		e.pendingPacketResponseID = responseID
	}
	return packets, batchID
}
