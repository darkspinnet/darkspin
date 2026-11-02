package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zone "github.com/darkspinnet/darkspin/server/zone"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

func silenceFieldCount(world *zone.Zone, objectID uint32) int {
	if world == nil || objectID == 0 {
		return 0
	}
	count := 0
	for _, modifier := range world.Effect().Snapshot() {
		if modifier.GUID == util.HashID(silenceFieldModifier) && modifier.TargetObjectID == objectID &&
			isSilenceFieldSource(world, modifier.SourceObjectID) {
			count++
		}
	}
	return count
}

func (e gameplayPeerSession) fixtureSilenceCount() int {
	return silenceFieldCount(e.zone, e.deployedObjectID)
}

// The caller holds registry.mutex. Include other silence owners when one
// source exits; removing this lease must not restore a still-silenced actor.
func silenceFieldAttribute(world *zone.Zone, objectID uint32, registry *gameplaySessionRegistry, now time.Time) ([]byte, error) {
	amount := float32(silenceFieldCount(world, objectID))
	for _, member := range registry.sessions {
		if member.zone == world && member.deployedObjectID == objectID {
			amount = member.arenaSilenceAmount(now)
			break
		}
	}
	if world.Companion().SilenceRemaining(objectID, now) > 0 {
		amount++
	}
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{ObjectID: objectID,
		Value: map[uint8]float32{14: amount}})
	if err != nil {
		return nil, fmt.Errorf("fieldSilenceAttribute: %w", err)
	}
	return packet, nil
}

// Retire recipient leases before a member/transport is discarded. The source
// trigger itself stays zone-owned for surviving players and subsequent joins.
func (e *gameplaySessionRegistry) retireSilenceFieldRecipientsLocked(member *gameplayPeerSession, pool *modifierPool) error {
	if member == nil || member.zone == nil {
		return nil
	}
	runtime := gameplayPendingRuntime{registry: e, modifierPool: pool, now: e.now}
	packets := make([][]byte, 0)
	for _, contact := range member.zone.SilenceFieldContacts() {
		isRecipient := contact.TargetObjectID == member.deployedObjectID
		hero, isHeroFound := member.zone.Hero().SnapshotByObjectID(contact.TargetObjectID)
		if isHeroFound && hero.UserID == member.binding.UserID && hero.PeerGeneration == member.generation {
			isRecipient = true
		}
		companion, isCompanionFound := member.zone.Companion().Snapshot(contact.TargetObjectID)
		if isCompanionFound && companion.UserID == member.binding.UserID && companion.PeerGeneration == member.generation {
			isRecipient = true
		}
		if !isRecipient {
			continue
		}
		deleted, err := runtime.retireSilenceFieldModifierLocked(member.zone, contact)
		if err != nil {
			return fmt.Errorf("fieldRecipientRetire: %w", err)
		}
		packets = append(packets, deleted...)
		member.zone.RemoveSilenceFieldContact(contact)
	}
	for key, peer := range e.sessions {
		if peer.zone == member.zone && (peer.binding.UserID != member.binding.UserID || peer.generation != member.generation) {
			peer.queuePackets(packets)
			e.sessions[key] = peer
		}
	}
	member.queuePackets(packets)
	return nil
}

func (e *gameplaySessionRegistry) silenceFieldPresentation(member gameplayPeerSession, timestamp uint64) ([][]byte, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	if member.zone == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for _, modifier := range member.zone.Effect().Snapshot() {
		if modifier.GUID != util.HashID(silenceFieldModifier) || !isSilenceFieldSource(member.zone, modifier.SourceObjectID) {
			continue
		}
		hero, isHeroFound := member.zone.Hero().SnapshotByObjectID(modifier.TargetObjectID)
		companion, isCompanionFound := member.zone.Companion().Snapshot(modifier.TargetObjectID)
		if (!isHeroFound || hero.HitPoint <= 0) && (!isCompanionFound || companion.HitPoint <= 0) {
			continue
		}
		created, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
			SourceObjectID: modifier.SourceObjectID, TargetObjectID: modifier.TargetObjectID,
			ModifierID: modifier.GUID, InstanceID: modifier.InstanceID, IsIndefinite: true, Timestamp: timestamp})
		if err != nil {
			return nil, fmt.Errorf("fieldReplayCreate: %w", err)
		}
		attribute, err := silenceFieldAttribute(member.zone, modifier.TargetObjectID, e, e.now())
		if err != nil {
			return nil, fmt.Errorf("fieldReplayAttribute: %w", err)
		}
		packets = append(packets, created, attribute)
	}
	return packets, nil
}
