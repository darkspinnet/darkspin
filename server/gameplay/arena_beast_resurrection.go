package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
	heroraknet "github.com/darkspinnet/darkspin/server/zone/hero/raknet103"
)

func (e *gameplayPeerSession) startArenaBeastResurrection(
	runtime campaignDamageRuntime, packet raknet.Packet, sessionKey string,
	world *arenaAbilityWorld, timestamp uint64,
) error {
	if runtime.npc.timer == nil || world == nil || world.companion == nil || e.beastPetObjectID == 0 {
		return errors.New("Arena Beast resurrection unavailable")
	}
	at := runtime.npc.now()
	run := &beastPetResurrectionRun{
		runtime: runtime.npc, damageRuntime: runtime, packet: packet,
		arenaWorld: world, arenaGameID: e.binding.GameID,
		userID: e.binding.UserID, sessionKey: sessionKey,
		generation: e.generation, ownerObjectID: e.deployedObjectID,
		petObjectID: e.beastPetObjectID, startedAt: at,
		sourceTime: timestamp, nextPollAt: at.Add(500 * time.Millisecond),
	}
	err := run.schedule(500 * time.Millisecond)
	if err != nil {
		return fmt.Errorf("arenaBeastPoll: %w", err)
	}
	e.beastPetResurrection = run
	return nil
}

func (e *beastPetResurrectionRun) arenaMemberLocked() (gameplayPeerSession, *arenaCombatPetRun, bool) {
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	match := e.runtime.registry.arenaMatches[e.arenaGameID]
	if !isFound || member.generation != e.generation || member.beastPetResurrection != e ||
		member.binding.Mode != game.ModeArena || member.binding.GameID != e.arenaGameID ||
		member.deployedObjectID != e.ownerObjectID || match == nil ||
		!match.isCombatActive || match.abilityWorld != e.arenaWorld {
		return gameplayPeerSession{}, nil, false
	}
	pet := e.arenaWorld.combatPets[e.petObjectID]
	if pet == nil || pet.kind != arenaCombatPetBeast || pet.ownerObjectID != e.ownerObjectID {
		return member, nil, true
	}
	return member, pet, true
}

func (e *beastPetResurrectionRun) executeArena(revision uint64) {
	e.runtime.registry.mutex.Lock()
	if e.revision != revision {
		e.runtime.registry.mutex.Unlock()
		return
	}
	member, pet, isCurrent := e.arenaMemberLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return
	}
	e.cancel = nil
	at := e.runtime.now()
	packets, isResume, err := e.advanceArena(&member, pet, at)
	if err != nil {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Savage Ally resurrection failed owner=%d: %v", e.ownerObjectID, err)
		}
		stopPackets, stopErr := e.stopArena(&member)
		if stopErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Savage Ally cleanup failed: %v", stopErr)
		}
		packets = append(packets, stopPackets...)
		member.beastPetResurrection = nil
		if pet != nil {
			pet.isPublished = false
			pet.retireLocked()
			packets = append(packets, pet.deletePacket)
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = member
	isTeamDefeated := member.isZoneGameOver()
	winnerTeam := uint32(3 - member.binding.Team)
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.arenaGameID, packets)
	e.runtime.registry.mutex.Unlock()
	if isTeamDefeated {
		ability := campaignAbilityCommandRuntime{registry: e.runtime.registry,
			modifierPool: e.runtime.modifierPool, effectPool: e.runtime.effectPool,
			logger: e.runtime.logger}
		ability.commitArenaVictory(e.arenaGameID, winnerTeam, e.sessionKey, e.generation)
	}
	if isResume && pet != nil {
		scheduleErr := pet.scheduleMeleePoll(time.Millisecond)
		if scheduleErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Savage Ally attack resume failed pet=%d: %v", pet.objectID, scheduleErr)
		}
	}
}

func (e *beastPetResurrectionRun) advanceArena(member *gameplayPeerSession,
	pet *arenaCombatPetRun, at time.Time) ([][]byte, bool, error) {
	if pet == nil {
		packets, err := e.stopArena(member)
		member.beastPetResurrection = nil
		return packets, false, err
	}
	actor, isFound := e.arenaWorld.companion.Snapshot(e.petObjectID)
	if !isFound || actor.HitPoint <= 0 {
		packets, err := e.stopArena(member)
		member.beastPetResurrection = nil
		return packets, false, err
	}
	packets := make([][]byte, 0, 4)
	isResume := false
	if e.state.IsCasting && !e.state.IsHitApplied && !at.Before(e.state.HitAt) {
		healPackets, err := e.arenaHit(member, at)
		if err != nil {
			return nil, false, fmt.Errorf("arenaBeastReviveHit: %w", err)
		}
		packets = append(packets, healPackets...)
		e.state.IsHitApplied = true
	}
	if e.state.IsCasting && !at.Before(e.state.ReleaseAt) {
		e.state.IsCasting = false
		err := e.arenaWorld.companion.ControlAbility(e.petObjectID, at, time.Time{}, false, "")
		if err != nil {
			return nil, false, fmt.Errorf("arenaBeastReviveRelease: %w", err)
		}
		isResume = member.deployedHitPoint() > 0
		if !isResume && member.squad.IsResurrectionPending() {
			err = member.squad.CancelResurrection()
			if err != nil {
				return nil, false, fmt.Errorf("arenaBeastReviveCancel: %w", err)
			}
		}
	}
	if e.state.IsBurrowing && !at.Before(e.state.ReleaseAt) {
		stopPackets, err := member.stopBeastPet()
		if err != nil {
			return nil, false, fmt.Errorf("arenaBeastBurrowStop: %w", err)
		}
		return append(packets, stopPackets...), false, nil
	}
	if !at.Before(e.nextPollAt) {
		e.nextPollAt = at.Add(500 * time.Millisecond)
		isOverdrive := member.isBeastResurrectionOverdrive(at)
		isEligible := e.state.IsEligible(isOverdrive, member.isBeastNormalResurrectionAvailable())
		if member.deployedHitPoint() > 0 {
			signPackets, err := e.arenaSyncSign(isEligible, at)
			if err != nil {
				return nil, false, fmt.Errorf("arenaBeastSign: %w", err)
			}
			packets = append(packets, signPackets...)
		} else if !e.state.IsCasting && !e.state.IsBurrowing && !actor.IsAbilityControlled {
			if isEligible {
				castPackets, err := e.arenaBegin(member, at, isOverdrive)
				if err != nil {
					return nil, false, fmt.Errorf("arenaBeastBegin: %w", err)
				}
				packets = append(packets, castPackets...)
			} else {
				e.state.IsBurrowing = true
				e.state.ReleaseAt = at.Add(2200 * time.Millisecond)
				err := e.arenaWorld.companion.ControlAbility(e.petObjectID, at, e.state.ReleaseAt, true, "beastsentinel_pet_burrow")
				if err != nil {
					return nil, false, fmt.Errorf("arenaBeastBurrowControl: %w", err)
				}
				burrowPackets, err := e.animation("beastsentinel_pet_burrow", at)
				if err != nil {
					return nil, false, fmt.Errorf("arenaBeastBurrowPose: %w", err)
				}
				packets = append(packets, burrowPackets...)
			}
		}
		e.state.ObserveOverdrive(isOverdrive)
	}
	next := e.nextPollAt
	if e.state.IsCasting {
		next = minTime(next, e.state.ReleaseAt)
		if !e.state.IsHitApplied {
			next = minTime(next, e.state.HitAt)
		}
	}
	if e.state.IsBurrowing {
		next = minTime(next, e.state.ReleaseAt)
	}
	err := e.schedule(next.Sub(at))
	if err != nil {
		return nil, false, fmt.Errorf("arenaBeastNext: %w", err)
	}
	return packets, isResume, nil
}

func (e *beastPetResurrectionRun) arenaBegin(member *gameplayPeerSession,
	at time.Time, isOverdrive bool) ([][]byte, error) {
	if !e.state.Begin(at, isOverdrive) {
		return nil, nil
	}
	member.squad.ReserveResurrection()
	if !isOverdrive {
		member.squad.ConsumeBeastResurrection()
	}
	pet := e.arenaWorld.combatPets[e.petObjectID]
	if pet != nil {
		pet.stopAttackLocked()
	}
	err := e.arenaWorld.companion.ControlAbility(e.petObjectID, at, e.state.ReleaseAt, true, "beastsentinel_pet_res")
	if err != nil {
		return nil, fmt.Errorf("arenaBeastControl: %w", err)
	}
	return e.animation("beastsentinel_pet_res", at)
}

func (e *beastPetResurrectionRun) arenaHit(member *gameplayPeerSession, at time.Time) ([][]byte, error) {
	if member.deployedHitPoint() > 0 {
		return nil, nil
	}
	fraction := float32(0.25)
	if member.isBeastResurrectionOverdrive(at) {
		fraction = 0.5
	}
	hitPoint := member.characterHitPointMaximum(member.deployedCreatureIndex) * fraction
	previousSquad := member.squad
	nextSquad := *previousSquad
	err := nextSquad.ResurrectCharacter(member.deployedCreatureIndex, hitPoint)
	if err != nil {
		return nil, fmt.Errorf("arenaBeastHealth: %w", err)
	}
	resourcePacket, err := member.marshalCampaignCharacterResourceValues(member.deployedCreatureIndex,
		hitPoint, member.deployedManaPoint())
	if err != nil {
		return nil, fmt.Errorf("arenaBeastResource: %w", err)
	}
	worldPacket, err := heroraknet.HitPoint(e.ownerObjectID, hitPoint)
	if err != nil {
		return nil, fmt.Errorf("arenaBeastWorld: %w", err)
	}
	healPacket, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
		Flags: 0x0002, DeltaHealth: -hitPoint, TargetID: e.ownerObjectID,
		SourceID: e.petObjectID, IntegerHPChange: -int32(hitPoint)})
	if err != nil {
		return nil, fmt.Errorf("arenaBeastHeal: %w", err)
	}
	revivePacket, err := raknet.MarshalApplication(raknet.CombatEventMessage{
		Flags: 0x0100, TargetID: e.ownerObjectID, SourceID: e.petObjectID})
	if err != nil {
		return nil, fmt.Errorf("arenaBeastRevive: %w", err)
	}
	member.squad = &nextSquad
	member.isHeroSelectionPending = false
	member.isHeroSelectionScheduled = false
	member.heroSelectionReadyAt = time.Time{}
	member.heroInputLockedObjectID = 0
	member.heroInputLockedUntil = time.Time{}
	member.isPartyDefeatQueued = false
	return [][]byte{healPacket, revivePacket, resourcePacket, worldPacket}, nil
}

func (e *beastPetResurrectionRun) arenaSyncSign(isEligible bool, at time.Time) ([][]byte, error) {
	if !isEligible {
		return e.arenaRemoveSign()
	}
	if e.signInstanceID != 0 {
		return nil, nil
	}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("arenaBeastSignID: %w", err)
	}
	packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.petObjectID, TargetObjectID: e.ownerObjectID,
		ModifierID: util.HashID("BeastPetResSign"), InstanceID: instanceID,
		Timestamp: e.timestamp(at), IsIndefinite: true})
	if err != nil {
		releaseErr := e.runtime.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("arenaBeastSignCreate: %w", errors.Join(err, releaseErr))
	}
	e.signInstanceID = instanceID
	return [][]byte{packet}, nil
}

func (e *beastPetResurrectionRun) arenaRemoveSign() ([][]byte, error) {
	if e.signInstanceID == 0 {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(e.ownerObjectID, e.signInstanceID)
	if err != nil {
		return nil, fmt.Errorf("arenaBeastSignDelete: %w", err)
	}
	err = e.runtime.modifierPool.Release(e.signInstanceID)
	if err != nil {
		return nil, fmt.Errorf("arenaBeastSignRelease: %w", err)
	}
	e.signInstanceID = 0
	return [][]byte{packet}, nil
}

func (e *beastPetResurrectionRun) stopArena(member *gameplayPeerSession) ([][]byte, error) {
	e.revision++
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.resumeCancel != nil {
		e.resumeCancel()
		e.resumeCancel = nil
	}
	packets, err := e.arenaRemoveSign()
	pet, isFound := e.arenaWorld.companion.Snapshot(e.petObjectID)
	if isFound && pet.HitPoint > 0 && pet.IsAbilityControlled {
		controlErr := e.arenaWorld.companion.ControlAbility(e.petObjectID,
			e.runtime.now(), time.Time{}, false, "")
		err = errors.Join(err, controlErr)
	}
	if member.squad != nil && member.squad.IsResurrectionPending() {
		cancelErr := member.squad.CancelResurrection()
		err = errors.Join(err, cancelErr)
		if cancelErr == nil && member.deployedHitPoint() <= 0 && member.squad.LivingCount() > 0 {
			switchPackets, switchErr := e.arenaFailedRevivalSwitch(member)
			packets = append(packets, switchPackets...)
			err = errors.Join(err, switchErr)
		}
	}
	if err != nil {
		return packets, fmt.Errorf("arenaBeastStop: %w", err)
	}
	return packets, nil
}

func (e *beastPetResurrectionRun) arenaFailedRevivalSwitch(member *gameplayPeerSession) ([][]byte, error) {
	for index := uint32(0); index < squad.Size; index++ {
		if index == member.deployedCreatureIndex || index >= uint32(len(member.binding.Creatures)) {
			continue
		}
		character, isFound := member.squad.Character(index)
		if !isFound || !character.IsAvailable || character.HitPoints <= 0 {
			continue
		}
		targetObjectID := zonehero.ObjectID(member.binding.Slot, index)
		if targetObjectID == 0 {
			return nil, errors.New("Arena Beast reserve hero unavailable")
		}
		at := e.runtime.now()
		timestamp := e.timestamp(at)
		packets, err := marshalCampaignCharacterSwitch(
			uint8(member.binding.Slot), member.deployedObjectID, targetObjectID, index,
			member.binding.Creatures[member.deployedCreatureIndex], member.binding.Creatures[index],
			0, member.deployedManaPoint(), character.HitPoints, character.ManaPoints,
			member.playerPosition, raknet.Quaternion{W: 1}, timestamp,
			timestamp+uint64(standardCreatureSwapCooldown/time.Millisecond), false)
		if err != nil {
			return nil, fmt.Errorf("arenaBeastReserveSwitchPacket: %w", err)
		}
		member.squad.ResetDeployCooldown()
		err = member.deployZoneCharacter(index, at, standardCreatureSwapCooldown)
		if err != nil {
			return nil, fmt.Errorf("arenaBeastReserveDeploy: %w", err)
		}
		return packets, nil
	}
	return nil, errors.New("Arena Beast living reserve missing")
}
