package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
)

// This is a publication cursor, not a passive instance or an expiry policy.
// It belongs to the retained member and is accessed under registry.mutex.
type overdrivePresentationState struct {
	activation          uint64
	generation          uint64
	transportGeneration uint64
	objectID            uint32
	deployedObjectID    uint32
	creatureIndex       uint32
	energy              float32
	startsAt            time.Time
	expiresAt           time.Time
	preparedAt          time.Time
	nextAt              time.Time
	isActive            bool
	isSet               bool
	isReconciled        bool
}

type overdrivePresentationSource struct {
	sessionKey string
	retained   *gameplayRetainedSession
	member     gameplayPeerSession
}

type overdrivePendingFrame struct {
	key                gameplayMemberKey
	world              *zone.Zone
	snapshot           overdrivePresentationState
	isAttributesNeeded bool
	attributes         overdriveAttributes
}

// These are the exact scalars encoded in this frame, not a meter revision.
// Independent effects and allied auras can change them without changing OD.
type overdriveAttributes struct {
	objectID          uint32
	energyDefense     float32
	criticalRating    float32
	attackSpeed       float32
	cooldownReduction float32
	isSet             bool
}

func (e *gameplaySessionRegistry) overdriveAttributesLocked(member gameplayPeerSession, at time.Time) overdriveAttributes {
	if member.deployedObjectID == 0 || member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) {
		return overdriveAttributes{}
	}
	creature := e.projectPassiveCreature(member, member.deployedCreatureIndex, at)
	return overdriveAttributes{
		objectID: member.deployedObjectID, energyDefense: creature.EnergyDefense,
		criticalRating: creature.CriticalRating, attackSpeed: creature.TimingProfile.AttackSpeed,
		cooldownReduction: creature.TimingProfile.CooldownReduction, isSet: true,
	}
}

func (e *gameplaySessionRegistry) areOverdriveAttributesCurrentLocked(attributes overdriveAttributes, member gameplayPeerSession, at time.Time) bool {
	return !attributes.isSet || attributes == e.overdriveAttributesLocked(member, at)
}

// Coalesce only this member's meter frames. Appending a new batch ID keeps an
// older in-flight transport commit from removing the replacement frame.
func (e *gameplayPeerSession) queueOverdriveFrame(frame overdrivePendingFrame, packets [][]byte) {
	batches := make([]pendingPeerPacketBatch, 0, len(e.pendingPacketBatches))
	for _, batch := range e.pendingPacketBatches {
		if batch.overdriveFrame != nil && batch.overdriveFrame.key == frame.key && batch.overdriveFrame.world == frame.world {
			frame.isAttributesNeeded = frame.isAttributesNeeded || batch.overdriveFrame.isAttributesNeeded
			continue
		}
		batches = append(batches, batch)
	}
	e.pendingPacketBatches = batches
	if len(packets) == 0 {
		// A stale transport receipt reserves a semantic repair. The next
		// locked drain must marshal it before returning any queued bytes.
		e.nextPendingPacketID++
		e.pendingPacketBatches = append(e.pendingPacketBatches, pendingPeerPacketBatch{
			id: e.nextPendingPacketID, isCampaignPresentation: true,
		})
	} else {
		e.queueCampaignPackets(packets)
	}
	lastIndex := len(e.pendingPacketBatches) - 1
	if lastIndex >= 0 && e.pendingPacketBatches[lastIndex].id == e.nextPendingPacketID {
		e.pendingPacketBatches[lastIndex].overdriveFrame = &frame
	}
}

func (e gameplayPeerSession) pendingOverdriveFrames(batchID uint64) []overdrivePendingFrame {
	frames := make([]overdrivePendingFrame, 0)
	for _, batch := range e.pendingPacketBatches {
		if batch.id > batchID {
			break
		}
		if batch.overdriveFrame != nil {
			frames = append(frames, *batch.overdriveFrame)
		}
	}
	return frames
}

// An older prepared response can finish after a newer response. Its receipt
// must leave a fresh repair queued even if the newer batch already committed.
func (e *gameplaySessionRegistry) repairOverdriveDeliveryLocked(recipient *gameplayPeerSession, frames []overdrivePendingFrame, at time.Time) {
	for _, frame := range frames {
		if recipient.zone != frame.world {
			continue
		}
		for _, source := range e.overdriveSourcesLocked(frame.world) {
			member := source.member
			if gameplaySessionMemberKey(member) != frame.key || member.generation != frame.snapshot.generation {
				continue
			}
			areAttributesCurrent := e.areOverdriveAttributesCurrentLocked(frame.attributes, member, at)
			if !frame.snapshot.matches(member, at) || !areAttributesCurrent {
				frame.isAttributesNeeded = frame.snapshot.needsAttributes(member, at) || !areAttributesCurrent
				recipient.queueOverdriveFrame(frame, nil)
			}
			break
		}
	}
}

// Pending frames are semantic publications: a retained recipient must never
// replay an old active flag or old actor attributes after its fresh baseline.
// Both production queue drains invoke this while holding registry.mutex.
func (e *gameplaySessionRegistry) refreshOverdriveFramesLocked(recipient *gameplayPeerSession, at time.Time) error {
	batches := make([]pendingPeerPacketBatch, 0, len(recipient.pendingPacketBatches))
	replacements := make([]pendingPeerPacketBatch, 0)
	for _, batch := range recipient.pendingPacketBatches {
		pending := batch.overdriveFrame
		if pending == nil {
			batches = append(batches, batch)
			continue
		}
		var member gameplayPeerSession
		isFound := false
		for _, source := range e.overdriveSourcesLocked(pending.world) {
			if gameplaySessionMemberKey(source.member) == pending.key && source.member.generation == pending.snapshot.generation {
				member = source.member
				isFound = true
				break
			}
		}
		if !isFound || recipient.zone != pending.world {
			continue
		}
		isChanged := !pending.snapshot.matches(member, at) || !e.areOverdriveAttributesCurrentLocked(pending.attributes, member, at)
		isAttributesNeeded := pending.isAttributesNeeded || pending.snapshot.needsAttributes(member, at)
		packets, attributes, err := e.overdriveFrameLocked(member, at, isAttributesNeeded)
		if err != nil {
			return fmt.Errorf("overdrivePending: %w", err)
		}
		next := *pending
		next.snapshot = overdrivePresentationSnapshot(member, at)
		next.isAttributesNeeded = isAttributesNeeded
		next.attributes = attributes
		batch.overdriveFrame = &next
		batch.packets = packets
		if isChanged {
			replacements = append(replacements, batch)
			continue
		}
		batches = append(batches, batch)
	}
	for _, batch := range replacements {
		recipient.nextPendingPacketID++
		batch.id = recipient.nextPendingPacketID
		batches = append(batches, batch)
	}
	recipient.pendingPacketBatches = batches
	return nil
}

func overdrivePresentationSnapshot(member gameplayPeerSession, at time.Time) overdrivePresentationState {
	return overdrivePresentationState{
		activation: member.overdriveActivation, generation: member.generation,
		transportGeneration: member.transportGeneration,
		objectID:            member.overdriveObjectID, deployedObjectID: member.deployedObjectID,
		creatureIndex: member.deployedCreatureIndex, startsAt: member.overdriveStartsAt,
		energy:    member.overdriveEnergy,
		expiresAt: member.overdriveExpiresAt, preparedAt: at, nextAt: at.Add(500 * time.Millisecond),
		isActive: member.isOverdriveActiveAt(at), isSet: true,
	}
}

func (e overdrivePresentationState) matches(member gameplayPeerSession, at time.Time) bool {
	return e.isSet && e.activation == member.overdriveActivation && e.generation == member.generation &&
		e.transportGeneration == member.transportGeneration && e.objectID == member.overdriveObjectID &&
		e.energy == member.overdriveEnergy &&
		e.deployedObjectID == member.deployedObjectID && e.creatureIndex == member.deployedCreatureIndex &&
		e.startsAt.Equal(member.overdriveStartsAt) && e.expiresAt.Equal(member.overdriveExpiresAt) &&
		e.isActive == member.isOverdriveActiveAt(at)
}

func (e overdrivePresentationState) needsAttributes(member gameplayPeerSession, at time.Time) bool {
	return !e.isSet || e.activation != member.overdriveActivation || e.generation != member.generation ||
		e.transportGeneration != member.transportGeneration || e.deployedObjectID != member.deployedObjectID ||
		e.creatureIndex != member.deployedCreatureIndex || e.isActive != member.isOverdriveActiveAt(at)
}

func (e *gameplaySessionRegistry) overdriveSourcesLocked(world *zone.Zone) []overdrivePresentationSource {
	sources := make([]overdrivePresentationSource, 0)
	keys := make(map[gameplayMemberKey]bool)
	for sessionKey, member := range e.sessions {
		if member.zone != world || member.isZoneTerminal() {
			continue
		}
		keys[gameplaySessionMemberKey(member)] = true
		sources = append(sources, overdrivePresentationSource{sessionKey: sessionKey, member: member})
	}
	for key, retained := range e.retainedSessions {
		if keys[key] || retained.peerSession.zone != world || retained.peerSession.isZoneTerminal() {
			continue
		}
		sources = append(sources, overdrivePresentationSource{retained: retained, member: retained.peerSession})
	}
	return sources
}

func marshalOverdriveState(member gameplayPeerSession, at time.Time) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.LabsPlayerOverdriveStateMessage{
		Slot: uint8(member.binding.Slot), Energy: member.presentedOverdriveEnergy(at),
		IsActive: member.isOverdriveActiveAt(at),
	})
	if err != nil {
		return nil, fmt.Errorf("overdriveState: %w", err)
	}
	return packet, nil
}

// The caller holds the registry lock; allied projections must use this same
// captured time and owner, rather than an old meter producer's hero slot.
func (e *gameplaySessionRegistry) overdriveFrameLocked(member gameplayPeerSession, at time.Time, isAttributesNeeded bool) ([][]byte, overdriveAttributes, error) {
	statePacket, err := marshalOverdriveState(member, at)
	if err != nil {
		return nil, overdriveAttributes{}, fmt.Errorf("overdriveFrame: %w", err)
	}
	packets := [][]byte{statePacket}
	if !isAttributesNeeded || member.deployedObjectID == 0 || member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) {
		return packets, overdriveAttributes{}, nil
	}
	attributes := e.overdriveAttributesLocked(member, at)
	attributePacket, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: attributes.objectID,
		Value: map[uint8]float32{9: attributes.energyDefense, 10: attributes.criticalRating,
			23: attributes.attackSpeed, 24: attributes.cooldownReduction},
	})
	if err != nil {
		return nil, overdriveAttributes{}, fmt.Errorf("overdriveAttributes: %w", err)
	}
	return append(packets, attributePacket), attributes, nil
}

// Store the source cursor before queueing, so its own recipient copy cannot
// overwrite a newly queued FIFO batch. Terminal packets survive detachment.
func (e *gameplaySessionRegistry) publishOverdriveFrameLocked(source overdrivePresentationSource, frame overdrivePresentationState, packets [][]byte, attributes overdriveAttributes) {
	member := source.member
	pending := overdrivePendingFrame{
		key: gameplaySessionMemberKey(member), world: member.zone, snapshot: frame,
		isAttributesNeeded: member.overdrivePresentation.needsAttributes(member, frame.preparedAt),
		attributes:         attributes,
	}
	member.overdrivePresentation = frame
	if source.retained != nil {
		source.retained.peerSession = member
	} else {
		e.sessions[source.sessionKey] = member
	}
	for sessionKey, recipient := range e.sessions {
		if recipient.zone != member.zone || recipient.isZoneTerminal() {
			continue
		}
		recipient.queueOverdriveFrame(pending, packets)
		e.sessions[sessionKey] = recipient
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.zone == member.zone && !retained.peerSession.isZoneTerminal() {
			retained.peerSession.queueOverdriveFrame(pending, packets)
		}
	}
}

// The source already committed these entry packets through its original
// producer. Fanout shares the activation check and lock with meter publication.
func (e *gameplaySessionRegistry) queueOverdriveEntryLocked(sessionKey string, source gameplayPeerSession, packets [][]byte) {
	presentations := gameplayPeerPresentationPackets(packets)
	for recipientKey, recipient := range e.sessions {
		if recipientKey == sessionKey || recipient.zone != source.zone || recipient.isZoneTerminal() {
			continue
		}
		recipient.queueCampaignPackets(presentations)
		e.sessions[recipientKey] = recipient
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.zone == source.zone && !retained.peerSession.isZoneTerminal() {
			retained.peerSession.queueCampaignPackets(presentations)
		}
	}
}

// A surviving peer also reconciles disconnected owners. Handoff never invokes
// entry helpers or changes duration, resources, persistent passives, or rank.
func (e campaignOverdriveRuntime) reconcilePresentation(packet raknet.Packet) error {
	if e.registry == nil || packet.Address == nil {
		return nil
	}
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	driver, isFound := e.registry.sessions[packet.Address.String()]
	if !isFound || driver.transportGeneration != packet.TransportGeneration || driver.isRejoinPending || driver.zone == nil {
		return nil
	}
	at := e.now()
	for _, source := range e.registry.overdriveSourcesLocked(driver.zone) {
		// Earlier sources may have queued packets to this same member. Keep
		// that current aggregate instead of restoring the enumeration copy.
		if source.retained != nil {
			source.member = source.retained.peerSession
		} else {
			source.member = e.registry.sessions[source.sessionKey]
		}
		member := source.member
		if member.overdriveActivation == 0 {
			continue
		}
		previous := member.overdrivePresentation
		isCurrent := previous.matches(member, at)
		if !previous.isReconciled && isCurrent && at.Before(previous.preparedAt.Add(time.Second)) {
			continue
		}
		if previous.isReconciled && isCurrent && (!previous.isActive || at.Before(previous.nextAt)) {
			continue
		}
		// Allow the original delayed entry its normal publication boundary.
		// A fallback repairs state only and does not invent a passive restart.
		if (!previous.isSet || previous.activation != member.overdriveActivation) && at.Before(member.overdriveStartsAt.Add(time.Second)) {
			continue
		}
		packets, attributes, err := e.registry.overdriveFrameLocked(member, at, previous.needsAttributes(member, at))
		if err != nil {
			return fmt.Errorf("overdriveReconcile: %w", err)
		}
		frame := overdrivePresentationSnapshot(member, at)
		frame.isReconciled = true
		e.registry.publishOverdriveFrameLocked(source, frame, packets, attributes)
	}
	return nil
}

func (e *gameplaySessionRegistry) overdriveRejoinPackets(member gameplayPeerSession, at time.Time) ([][]byte, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	packets := make([][]byte, 0)
	for _, source := range e.overdriveSourcesLocked(member.zone) {
		packet, err := marshalOverdriveState(source.member, at)
		if err != nil {
			return nil, fmt.Errorf("overdriveReplay: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
