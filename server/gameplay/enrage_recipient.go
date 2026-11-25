package gameplay

import (
	"errors"
	"fmt"
	"log"

	"github.com/darkspinnet/darkspin/server/game"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
)

type enrageRecipient struct {
	registry   *gameplaySessionRegistry
	userID     uint64
	generation uint64
}

type sharedEnrageState struct {
	source           *gameplayPeerSession
	recipient        *enrageRecipient
	isRegistryLocked bool
}

type enrageMember struct {
	state      sharedEnrageState
	session    gameplayPeerSession
	sessionKey string
	retained   *gameplayRetainedSession
}

func campaignEnrageAdapter(source *gameplayPeerSession, isRegistryLocked bool) abilityraknet.EnrageState {
	if source.enrageRecipient == nil {
		return campaignEnrageState{session: source}
	}
	return sharedEnrageState{source: source, recipient: source.enrageRecipient, isRegistryLocked: isRegistryLocked}
}

// Resolve the live or retained recipient on every operation; endpoint changes
// must not redirect a buff, and a replacement generation must not inherit it.
func (e sharedEnrageState) open() (enrageMember, bool) {
	registry := e.recipient.registry
	if !e.isRegistryLocked {
		registry.mutex.Lock()
	}
	for sessionKey, member := range registry.sessions {
		if member.binding.UserID == e.recipient.userID && member.generation == e.recipient.generation && member.zone == e.source.zone {
			return enrageMember{state: e, session: member, sessionKey: sessionKey}, true
		}
	}
	key := gameplayMemberKey{gameID: e.source.binding.GameID, userID: e.recipient.userID}
	retained := registry.retainedSessions[key]
	if retained != nil && retained.peerSession.generation == e.recipient.generation && retained.peerSession.zone == e.source.zone {
		return enrageMember{state: e, session: retained.peerSession, retained: retained}, true
	}
	if !e.isRegistryLocked {
		registry.mutex.Unlock()
	}
	return enrageMember{}, false
}

func (e *enrageMember) close() {
	if e.retained != nil {
		e.retained.peerSession = e.session
	} else {
		e.state.recipient.registry.sessions[e.sessionKey] = e.session
	}
	if !e.state.isRegistryLocked {
		e.state.recipient.registry.mutex.Unlock()
	}
}

func (e sharedEnrageState) EnrageTarget(index, objectID uint32, isCompanion bool) (abilityraknet.EnrageTarget, bool) {
	member, isFound := e.open()
	if !isFound {
		return abilityraknet.EnrageTarget{}, false
	}
	defer member.close()
	return (campaignEnrageState{session: &member.session}).EnrageTarget(index, objectID, isCompanion)
}

func (e sharedEnrageState) ApplyEnrage(index, objectID uint32, isCompanion bool, damageBonus, hitPoint float32) error {
	member, isFound := e.open()
	if !isFound {
		return errors.New("Enrage recipient unavailable")
	}
	defer member.close()
	err := (campaignEnrageState{session: &member.session}).ApplyEnrage(index, objectID, isCompanion, damageBonus, hitPoint)
	if err != nil {
		return fmt.Errorf("recipientApply: %w", err)
	}
	return nil
}

func (e sharedEnrageState) RemoveEnrage(index, objectID uint32, isCompanion bool, damageBonus float32) error {
	member, isFound := e.open()
	if !isFound {
		return nil
	}
	defer member.close()
	err := (campaignEnrageState{session: &member.session}).RemoveEnrage(index, objectID, isCompanion, damageBonus)
	if err != nil {
		return fmt.Errorf("recipientRemove: %w", err)
	}
	return nil
}

func (e sharedEnrageState) ApplyEnrageModifier(index, objectID uint32, isCompanion bool, instanceID, previousID uint32, damageBonus, hitPoint float32) error {
	member, isFound := e.open()
	if !isFound {
		return errors.New("Enrage recipient unavailable")
	}
	defer member.close()
	err := (campaignEnrageState{session: &member.session}).ApplyEnrageModifier(index, objectID, isCompanion, instanceID, previousID, damageBonus, hitPoint)
	if err != nil {
		return fmt.Errorf("recipientModifier: %w", err)
	}
	return nil
}

func (e sharedEnrageState) RemoveEnrageModifier(index, objectID uint32, isCompanion bool, instanceID uint32, damageBonus float32) error {
	member, isFound := e.open()
	if !isFound {
		return nil
	}
	defer member.close()
	err := (campaignEnrageState{session: &member.session}).RemoveEnrageModifier(index, objectID, isCompanion, instanceID, damageBonus)
	if err != nil {
		return fmt.Errorf("recipientModifierRemove: %w", err)
	}
	return nil
}

func (e campaignAbilityCommandRuntime) enrageRecipientLocked(source gameplayPeerSession, objectID uint32) (gameplayPeerSession, uint32, bool, *enrageRecipient, bool) {
	for _, member := range e.registry.sessions {
		if member.zone != source.zone || member.squad == nil || member.isZoneTerminal() ||
			member.zone == nil && member.binding.UserID != source.binding.UserID ||
			!isPassiveAuraRecipient(member, source) {
			continue
		}
		index := member.deployedCreatureIndex
		isCompanion := false
		isTargetFound := false
		for candidateIndex := uint32(0); candidateIndex < uint32(len(member.binding.Creatures)); candidateIndex++ {
			if zonehero.ObjectID(member.binding.Slot, candidateIndex) != objectID {
				continue
			}
			character, isCharacterFound := member.squad.Character(candidateIndex)
			isTargetFound = isCharacterFound && character.IsAvailable && character.HitPoints > 0
			index = candidateIndex
			break
		}
		if !isTargetFound && member.zone != nil && member.zone.Companion() != nil {
			actor, isActorFound := member.zone.Companion().Snapshot(objectID)
			isCompanion = isActorFound && actor.UserID == member.binding.UserID && actor.PeerGeneration == member.generation && actor.HitPoint > 0
			isTargetFound = isCompanion
		}
		if !isTargetFound || index >= uint32(len(member.binding.Creatures)) {
			continue
		}
		var recipient *enrageRecipient
		if member.binding.UserID != source.binding.UserID {
			recipient = &enrageRecipient{registry: e.registry, userID: member.binding.UserID, generation: member.generation}
		}
		return member, index, isCompanion, recipient, true
	}
	return gameplayPeerSession{}, 0, false, nil, false
}

// Build-103 CastEnrage uses the instant-target template's ten-unit friendly
// search around the stored point, then falls back to the caster.
func (e campaignAbilityCommandRuntime) nearestEnrageTargetLocked(source gameplayPeerSession, position game.Vec3) uint32 {
	if source.zone == nil || !zonegeometry.IsFinite(position) {
		return source.deployedObjectID
	}
	selectedID := source.deployedObjectID
	minimumDistance := float32(10)
	for _, member := range e.registry.sessions {
		if member.zone != source.zone || member.isZoneTerminal() || !isPassiveAuraRecipient(member, source) {
			continue
		}
		if member.deployedHitPoint() > 0 {
			distance := game.Vec3(member.playerPosition).Sub(position).Length()
			if distance < minimumDistance || distance == minimumDistance && member.deployedObjectID < selectedID {
				selectedID, minimumDistance = member.deployedObjectID, distance
			}
		}
		for _, actor := range member.zone.Companion().Snapshots() {
			if actor.UserID != member.binding.UserID || actor.PeerGeneration != member.generation || actor.HitPoint <= 0 {
				continue
			}
			distance := actor.Position.Sub(position).Length()
			if distance < minimumDistance || distance == minimumDistance && actor.ObjectID < selectedID {
				selectedID, minimumDistance = actor.ObjectID, distance
			}
		}
	}
	return selectedID
}

func (e *gameplayPeerSession) retireSharedEnrage(isRegistryLocked bool) {
	if e.enrageRecipient == nil || e.enrageRun == nil {
		return
	}
	registry := e.enrageRecipient.registry
	packets, err := e.enrageRun.Expire(campaignEnrageAdapter(e, isRegistryLocked))
	if err != nil {
		e.enrageRun.ReleaseEffect()
		log.Printf("RakNet shared Enrage cleanup user=%d: %v", e.binding.UserID, err)
	}
	e.enrageRecipient = nil
	if !isRegistryLocked {
		registry.mutex.Lock()
	}
	for sessionKey, member := range registry.sessions {
		if member.zone != e.zone {
			continue
		}
		if member.binding.UserID == e.binding.UserID {
			e.queueCampaignPackets(packets)
			continue
		}
		member.queueCampaignPackets(packets)
		registry.sessions[sessionKey] = member
	}
	if !isRegistryLocked {
		registry.mutex.Unlock()
	}
}
