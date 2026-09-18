package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// Use the same queue for ordinary projection drains and delayed presentation.
// Transferring under the registry lock prevents a concurrent producer from
// copying an already in-flight spawn or replaying it after a movement update.
func (e gameplayProjectionRuntime) drainQueuedProjection(
	request raknet.Packet, sessionKey string,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound || peerSession.zone == nil || peerSession.isRejoinPending ||
		!peerSession.dungeonSetup.IsCommitted() ||
		len(peerSession.pendingPacketBatches) != 0 {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	err := peerSession.queueCampaignPresentation(nil)
	packets, batchID := peerSession.pendingPackets()
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("projectionQueue: %w", err)
	}
	if len(packets) == 0 {
		return nil, nil
	}
	delivery := campaignProjectionDelivery{
		registry: e.registry, sessionKey: sessionKey,
		generation:          peerSession.generation,
		transportGeneration: peerSession.transportGeneration, batchID: batchID,
	}
	err = request.AfterResponseCommit(delivery.commit)
	if err != nil {
		return nil, fmt.Errorf("projectionQueueCommit: %w", err)
	}
	return packets, nil
}

type campaignProjectionDelivery struct {
	registry            *gameplaySessionRegistry
	sessionKey          string
	generation          uint64
	transportGeneration uint64
	batchID             uint64
}

func (e campaignProjectionDelivery) commit() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peerSession, isFound := e.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation ||
		peerSession.transportGeneration != e.transportGeneration {
		return
	}
	peerSession.commitPendingPackets(e.batchID)
	e.registry.sessions[e.sessionKey] = peerSession
}

type campaignPeerPublication struct {
	registry *gameplaySessionRegistry
	identity gameplayProducerIdentity
	packets  [][]byte
}

func (e campaignPeerPublication) commit() {
	e.registry.queuePeerPresentation(e.identity, e.packets)
}

func publishCampaignPeersAfterCommit(
	registry *gameplaySessionRegistry, packet raknet.Packet, packets [][]byte,
) error {
	if len(packets) == 0 {
		return nil
	}
	publication := campaignPeerPublication{
		registry: registry, identity: registry.producerGuard.identity(packet.Address.String()),
		packets: clonePendingPackets(packets),
	}
	err := packet.AfterResponseCommit(publication.commit)
	if err != nil {
		return fmt.Errorf("peerCommit: %w", err)
	}
	return nil
}

// The registry lock protects this transfer from the semantic event queue to
// the transport-owned reliable queue. Pending packets remain until commit.
func (e *gameplayPeerSession) queueCampaignPresentation(packets [][]byte) error {
	events := e.zone.PeekProjection(e.binding.UserID, e.generation)
	queuedPackets := make([][]byte, 0, len(packets)+len(events))
	projectedPacketCounts := make(map[string]int)
	for _, event := range events {
		projectedPackets, err := marshalCampaignProjection(event)
		if err != nil {
			// Preserve the presentation for retry and request a state baseline
			// rather than silently dropping an unencodable prerequisite.
			e.queueCampaignPackets(packets)
			baselineErr := e.zone.RequireProjectionBaseline(e.binding.UserID, e.generation)
			if baselineErr != nil {
				return fmt.Errorf("presentationBaseline: %w", baselineErr)
			}
			return fmt.Errorf("presentationProjection: %w", err)
		}
		queuedPackets = append(queuedPackets, projectedPackets...)
		for _, projectedPacket := range projectedPackets {
			projectedPacketCounts[string(projectedPacket)]++
		}
	}
	for _, packet := range packets {
		key := string(packet)
		if projectedPacketCounts[key] > 0 {
			projectedPacketCounts[key]--
			continue
		}
		queuedPackets = append(queuedPackets, packet)
	}
	e.queueCampaignPackets(queuedPackets)
	if len(events) == 0 {
		return nil
	}
	isCommitted := e.zone.CommitProjection(
		e.binding.UserID, e.generation, events[len(events)-1].Sequence,
	)
	if !isCommitted {
		err := e.zone.RequireProjectionBaseline(e.binding.UserID, e.generation)
		if err != nil {
			return fmt.Errorf("presentationCursor: %w", err)
		}
	}
	return nil
}

func (e *gameplayPeerSession) queueCampaignPackets(packets [][]byte) {
	if len(packets) == 0 {
		return
	}
	e.queuePackets(packets)
	lastIndex := len(e.pendingPacketBatches) - 1
	if e.pendingPacketBatches[lastIndex].id == e.nextPendingPacketID {
		e.pendingPacketBatches[lastIndex].isCampaignPresentation = true
	}
}
