package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

// A campaign result is personal even though encounter completion is shared.
// Keep presenting the completed zone to each member until that member enters
// the result flow, then stop connection-local gameplay producers for them.
func (e gameplayPeerSession) isCampaignPresentationAvailable() bool {
	if e.zone == nil {
		return false
	}
	if e.binding.Mode == game.ModeChain {
		return e.chainResult == nil
	}
	return e.zone.Boss() == nil || !e.zone.Boss().IsBeamOutCommitted()
}

// Use the same queue for ordinary projection drains and delayed presentation.
// Transferring under the registry lock prevents a concurrent producer from
// copying an already in-flight spawn or replaying it after a movement update.
func (e gameplayProjectionRuntime) drainQueuedProjection(
	request raknet.Packet, sessionKey string,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	e.registry.commitChannelRemovalsLocked()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound || peerSession.zone == nil || peerSession.isRejoinPending ||
		!peerSession.dungeonSetup.IsCommitted() ||
		(request.TraceID != 0 && peerSession.pendingPacketResponseTraceID == request.TraceID) {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.queueCampaignPackets(e.registry.takePlasmaWreathRestorationLocked(&peerSession))
	err := peerSession.queueCampaignPresentation(nil)
	if err == nil {
		err = e.registry.refreshOverdriveFramesLocked(&peerSession, time.Now())
	}
	if err == nil {
		err = e.registry.refreshQuestFramesLocked(&peerSession, time.Now(), request.SourceTime)
	}
	if err == nil {
		err = e.registry.refreshMutationElitesLocked(&peerSession)
	}
	if err == nil {
		err = e.registry.refreshMutationEventsLocked(&peerSession, time.Now(), request.SourceTime)
	}
	if err == nil {
		err = e.registry.refreshMutationFeedbackLocked(&peerSession)
	}
	if err != nil {
		e.registry.sessions[sessionKey] = peerSession
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("projectionPrepare: %w", err)
	}
	packets, batchID := peerSession.pendingPackets()
	overdriveFrames := peerSession.pendingOverdriveFrames(batchID)
	questFrames := peerSession.pendingQuestFrames(batchID)
	mutationElites := peerSession.pendingMutationElites(batchID)
	mutationEvents := peerSession.pendingMutationEvents(batchID)
	mutationFeedbacks := peerSession.pendingMutationFeedbacks(batchID)
	if len(packets) != 0 {
		// Pending batches are retained until transport commit. Only a batch
		// already included by this request should suppress the outer drain;
		// the existence of queued peer updates is a reason to send them.
		peerSession.pendingPacketResponseTraceID = request.TraceID
	}
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()
	if len(packets) == 0 {
		return nil, nil
	}
	delivery := campaignProjectionDelivery{
		registry: e.registry, sessionKey: sessionKey,
		generation:          peerSession.generation,
		transportGeneration: peerSession.transportGeneration, batchID: batchID,
		overdriveFrames:   overdriveFrames,
		questFrames:       questFrames,
		mutationElites:    mutationElites,
		mutationEvents:    mutationEvents,
		mutationFeedbacks: mutationFeedbacks,
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
	overdriveFrames     []overdrivePendingFrame
	questFrames         []questPendingFrame
	mutationElites      []mutationEliteFrame
	mutationEvents      []mutationPendingEvent
	mutationFeedbacks   []mutationDamageFeedback
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
	e.registry.repairOverdriveDeliveryLocked(&peerSession, e.overdriveFrames, time.Now())
	e.registry.repairQuestDeliveryLocked(&peerSession, e.questFrames, time.Now())
	e.registry.repairMutationDeliveryLocked(&peerSession, e.mutationElites)
	e.registry.repairMutationEventsLocked(&peerSession, e.mutationEvents, time.Now())
	e.registry.repairMutationFeedbackLocked(&peerSession, e.mutationFeedbacks)
	e.registry.sessions[e.sessionKey] = peerSession
	e.registry.commitPlasmaWreathPublicationsLocked()
	e.registry.commitOmniShieldRemovalsLocked()
	e.registry.commitChannelRemovalsLocked()
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
		var liveObjectID uint32
		isSuppressed := false
		switch event.Kind {
		case zoneprojection.EventNPCAction:
			liveObjectID = event.Action.Plan.ObjectID
		case zoneprojection.EventNPCSnapshot:
			liveObjectID = event.NPCSnapshot.Plan.ObjectID
		case zoneprojection.EventNPCTarget:
			liveObjectID = event.NPCTarget.Plan.ObjectID
		}
		if liveObjectID != 0 {
			// Native 5393D0 installs movement on any retained object, including
			// a corpse. A delayed action must not restart pursuit or overwrite
			// its death animation; a delayed join snapshot must not recreate it.
			enemy, isFound := e.zone.NPCs().NPC(liveObjectID)
			isSuppressed = !isFound || enemy.IsDefeated
		}
		if event.Kind == zoneprojection.EventNPCDamage && !event.Damage.IsDamageImmune {
			// Damage publication can finish after a later projectile has killed
			// the target. Native 5399D0 applies combatant snapshots without a
			// revision check: replaying the earlier hit's health revives its
			// client state. Keep the hit feedback, but publish current health.
			enemy, isFound := e.zone.NPCs().NPC(event.Damage.TargetObjectID)
			if !isFound {
				continue
			}
			event.Damage.HitPoint = enemy.HitPoint
			if enemy.IsDefeated {
				event.Damage.HitPoint = 0
			}
		}
		projectedPackets, err := e.marshalCampaignPresentation(event, time.Now())
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
		if !isSuppressed {
			queuedPackets = append(queuedPackets, projectedPackets...)
		}
		// Count suppressed packets too, so an encoded copy supplied by the
		// producer cannot reintroduce the stale event below.
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
	for _, event := range events {
		if event.Kind == zoneprojection.EventHeroLeave && event.HeroLeave.IsMemberRemoved {
			e.knownPlayerMask &^= uint32(1) << event.HeroLeave.PlayerSlot
		}
	}
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

func (e *gameplayPeerSession) marshalCampaignPresentation(event zoneprojection.Event, now time.Time) ([][]byte, error) {
	if event.Kind == zoneprojection.EventNPCAction && event.Action.Kind == zonenpc.ActionEventAggro {
		snapshot, isFound := e.zone.NPCs().NPC(event.Action.Plan.ObjectID)
		if !isFound || snapshot.IsDefeated {
			return nil, nil
		}
		packets, err := npcraknet.FirstAggroCurrent(event.Action.Plan, event.Action.Timestamp, snapshot, now)
		if err != nil {
			return nil, fmt.Errorf("presentationAggro: %w", err)
		}
		return packets, nil
	}
	packets, err := marshalCampaignProjection(event)
	if err != nil {
		return nil, fmt.Errorf("presentationEvent: %w", err)
	}
	return packets, nil
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
