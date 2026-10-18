package gameplay

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/squad"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
)

type capsuleRecipientRecovery struct {
	sessionKey      string
	previousSession gameplayPeerSession
	nextSession     gameplayPeerSession
	packets         [][]byte
}

// prepareCapsuleRecovery scans authored slot order and stops at the first
// fallen slot, even when its recovery is zero. Active HP belongs to the live
// object; inactive HP belongs to squad storage. Work on a copied squad so a
// rejected party operation cannot leave a partial recovery behind.
func (e gameplayPeerSession) prepareCapsuleRecovery(
	fraction float32, activeHitPoint float32, activeMaximumHitPoint float32,
) (gameplayPeerSession, [][]byte, error) {
	if e.squad == nil || math.IsNaN(float64(fraction)) || math.IsInf(float64(fraction), 0) {
		return e, nil, errors.New("capsule recovery input invalid")
	}
	for index := uint32(0); index < squad.Size; index++ {
		character, isFound := e.squad.Character(index)
		if !isFound || !character.IsAvailable {
			continue
		}
		oldHitPoint := character.HitPoints
		if index == e.deployedCreatureIndex {
			oldHitPoint = activeHitPoint
		}
		if oldHitPoint > 0 {
			continue
		}
		maximumHitPoint := e.characterHitPointMaximum(index)
		if index == e.deployedCreatureIndex {
			maximumHitPoint = activeMaximumHitPoint
		}
		if maximumHitPoint <= 0 || math.IsNaN(float64(maximumHitPoint)) ||
			math.IsInf(float64(maximumHitPoint), 0) {
			return e, nil, fmt.Errorf("capsuleMaximum[%d]: invalid", index)
		}
		// Keep the native float32 multiply/add boundaries before clamping.
		recovery := float32(maximumHitPoint * fraction)
		hitPoint := min(maximumHitPoint, max(float32(0), float32(oldHitPoint+recovery)))
		packet, err := e.marshalCampaignCharacterResourceValues(index, hitPoint, character.ManaPoints)
		if err != nil {
			return e, nil, fmt.Errorf("capsuleResource[%d]: %w", index, err)
		}
		packets := [][]byte{packet}
		if index == e.deployedCreatureIndex {
			worldPacket, worldErr := heroraknet.HitPoint(e.deployedObjectID, hitPoint)
			if worldErr != nil {
				return e, nil, fmt.Errorf("capsuleWorld: %w", worldErr)
			}
			packets = append(packets, worldPacket)
		}
		next := e
		nextSquad := *e.squad
		next.squad = &nextSquad
		err = next.squad.ResurrectCharacter(index, hitPoint)
		if err != nil {
			return e, nil, fmt.Errorf("capsuleHealth: %w", err)
		}
		if index == e.deployedCreatureIndex && hitPoint > 0 {
			// A restored live object no longer waits for death-driven selection.
			next.isHeroSelectionPending = false
			next.isHeroSelectionScheduled = false
			next.heroSelectionReadyAt = time.Time{}
			next.heroInputLockedObjectID = 0
			next.heroInputLockedUntil = time.Time{}
			next.isPartyDefeatQueued = false
		}
		return next, packets, nil
	}
	return e, nil, nil
}

// The caller holds registry.mutex throughout preparation, checkpoint updates,
// and capsule commit. Each recipient contributes at most one fallen slot.
func (e resurrectionPickupStep) recoverPartyLocked(
	source gameplayPeerSession,
) ([]capsuleRecipientRecovery, error) {
	sessionKeys := make([]string, 0, len(e.runtime.registry.sessions))
	for sessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone != source.zone || candidate.binding.GameID != source.binding.GameID ||
			candidate.binding.Mode != source.binding.Mode || candidate.squad == nil ||
			candidate.lastPlayerStatus.Status&0x40 != 0 || !candidate.stage.IsDungeon() ||
			candidate.isRejoinPending || candidate.isZoneTerminal() {
			continue
		}
		sessionKeys = append(sessionKeys, sessionKey)
	}
	sort.Slice(sessionKeys, func(left int, right int) bool {
		return e.runtime.registry.sessions[sessionKeys[left]].binding.Slot <
			e.runtime.registry.sessions[sessionKeys[right]].binding.Slot
	})
	recoveries := make([]capsuleRecipientRecovery, 0, len(sessionKeys))
	baseFraction := source.zone.DirectorDefinition().PickupTuning.ResurrectionHealthFraction
	for _, sessionKey := range sessionKeys {
		candidate := e.runtime.registry.sessions[sessionKey]
		actor, isActorFound := source.zone.Hero().Snapshot(candidate.binding.UserID, candidate.generation)
		if !isActorFound || actor.ObjectID != candidate.deployedObjectID ||
			actor.CreatureIndex != candidate.deployedCreatureIndex ||
			candidate.deployedCreatureIndex >= uint32(len(candidate.binding.Creatures)) {
			continue
		}
		// PartAttribute is the current hero attribute projection: catalyst deltas
		// are already applied by setCrystalInventory. Do not add them twice or
		// read the picker's projection for another recipient.
		orbEffectiveness := candidate.binding.Creatures[candidate.deployedCreatureIndex].PartAttribute[campaignOrbEffectAttribute]
		fraction := float32(baseFraction * float32(1+orbEffectiveness))
		next, packets, err := candidate.prepareCapsuleRecovery(fraction, actor.HitPoint, actor.MaximumHitPoint)
		if err != nil {
			return nil, fmt.Errorf("capsulePrepare[%d]: %w", candidate.binding.UserID, err)
		}
		if len(packets) == 0 {
			continue
		}
		recoveries = append(recoveries, capsuleRecipientRecovery{
			sessionKey: sessionKey, previousSession: candidate, nextSession: next, packets: packets,
		})
	}
	for index, recovery := range recoveries {
		next := recovery.nextSession
		err := next.syncZoneHero()
		if err == nil {
			err = next.syncZoneSquadCheckpoint()
		}
		if err != nil {
			rollbackErr := e.rollbackPartyLocked(recoveries[:index+1])
			return nil, fmt.Errorf("capsuleSync[%d]: %w", next.binding.UserID, errors.Join(err, rollbackErr))
		}
		e.runtime.registry.sessions[recovery.sessionKey] = next
	}
	return recoveries, nil
}

func (e resurrectionPickupStep) rollbackPartyLocked(recoveries []capsuleRecipientRecovery) error {
	var rollbackErrors []error
	for index := len(recoveries) - 1; index >= 0; index-- {
		recovery := recoveries[index]
		previous := recovery.previousSession
		heroErr := previous.syncZoneHero()
		if heroErr != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("capsuleHeroRollback[%d]: %w", previous.binding.UserID, heroErr))
		}
		checkpointErr := previous.syncZoneSquadCheckpoint()
		if checkpointErr != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("capsuleSquadRollback[%d]: %w", previous.binding.UserID, checkpointErr))
		}
		e.runtime.registry.sessions[recovery.sessionKey] = previous
	}
	rollbackErr := errors.Join(rollbackErrors...)
	if rollbackErr != nil {
		return fmt.Errorf("capsuleRollback: %w", rollbackErr)
	}
	return nil
}
