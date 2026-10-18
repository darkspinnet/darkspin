package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
)

// The registry lock owns this exact cast's debit and phase. Ordinary power
// changes do not invalidate its additive refund; a replacement squad does.
type channelCast struct {
	revision            uint64
	zone                *zone.Zone
	squad               *squad.Session
	generation          uint64
	userID              uint64
	creatureIndex       uint32
	sourceObjectID      uint32
	manaDebit           float32
	cooldown            *zoneability.CooldownSession
	release             *zoneaction.ReleaseSession
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	isActivated         bool
	isResolved          bool
}

func (e *gameplaySessionRegistry) reserveChannelCastLocked(peerSession gameplayPeerSession, sourceObjectID uint32, cooldown zoneability.CooldownReservation, release zoneaction.ReleaseReservation) *channelCast {
	e.nextChannelCastRevision++
	return &channelCast{
		revision: e.nextChannelCastRevision, zone: peerSession.zone,
		squad: peerSession.squad, generation: peerSession.generation,
		userID: peerSession.binding.UserID, creatureIndex: peerSession.deployedCreatureIndex,
		sourceObjectID: sourceObjectID,
		cooldown:       peerSession.abilityCooldownSession(), release: peerSession.abilityReleaseSession(),
		cooldownReservation: cooldown, releaseReservation: release,
	}
}

func (e *channelCast) debitLocked(peerSession *gameplayPeerSession, manaCost float32) error {
	beforeManaPoint := peerSession.deployedManaPoint()
	err := peerSession.setDeployedManaPoints(beforeManaPoint - manaCost)
	// Checkpoint persistence can fail after Squad and Zone accepted the debit.
	// Retain only the actual mutation, including that partial-error branch.
	e.manaDebit = max(float32(0), beforeManaPoint-peerSession.deployedManaPoint())
	if err != nil {
		return fmt.Errorf("channelDebit: %w", err)
	}
	return nil
}

func (e *channelCast) activateLocked(revision uint64) {
	if e == nil || e.revision != revision || e.isResolved {
		return
	}
	e.isActivated = true
}

func (e *channelCast) failLocked(peerSession *gameplayPeerSession, isFound bool, revision uint64) ([]byte, error) {
	if e == nil || e.revision != revision || e.isResolved {
		return nil, nil
	}
	// Mark once before resource/checkpoint I/O: that operation can mutate the
	// squad and still return an error, so retrying must not refund twice.
	e.isResolved = true
	if !isFound || peerSession.zone != e.zone || peerSession.squad != e.squad ||
		peerSession.generation != e.generation || peerSession.binding.UserID != e.userID {
		return nil, nil
	}
	e.release.Rollback(e.releaseReservation)
	if e.isActivated {
		return nil, nil
	}
	e.cooldown.Rollback(e.cooldownReservation)
	if e.manaDebit == 0 {
		return nil, nil
	}
	character, isCharacterFound := peerSession.squad.Character(e.creatureIndex)
	if !isCharacterFound || !character.IsAvailable {
		return nil, fmt.Errorf("channelRefund: character unavailable")
	}
	manaPoint := min(peerSession.characterManaPointMaximum(e.creatureIndex), character.ManaPoints+e.manaDebit)
	packet, err := abilityraknet.Mana(e.sourceObjectID, manaPoint)
	if err != nil {
		return nil, fmt.Errorf("channelRefundEncode: %w", err)
	}
	err = peerSession.setCampaignCharacterManaPoints(e.creatureIndex, manaPoint)
	if err != nil {
		updatedCharacter, isUpdatedFound := peerSession.squad.Character(e.creatureIndex)
		if isUpdatedFound && updatedCharacter.ManaPoints == manaPoint {
			return packet, fmt.Errorf("channelRefundCheckpoint: %w", err)
		}
		return nil, fmt.Errorf("channelRefundApply: %w", err)
	}
	return packet, nil
}

type channelDrainAdmission struct {
	run      *heroDrainRun
	revision uint64
}

func (e channelDrainAdmission) commit() {
	e.run.registry.mutex.Lock()
	defer e.run.registry.mutex.Unlock()
	// The response was sent even if cancellation already retired visuals.
	e.run.cast.activateLocked(e.revision)
}
