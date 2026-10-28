package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
)

type arenaCombatPetKind uint8

const (
	arenaCombatPetBeast arenaCombatPetKind = iota + 1
	arenaCombatPetPlasma
	arenaCombatPetFire
)

// One Arena match owns companion actors independently of a campaign zone.
// Ability-specific runs may retain this actor, but the object and its AI
// cannot outlive its match, owner generation, or deployed hero.
type arenaCombatPetRun struct {
	runtime            campaignAbilityCommandRuntime
	packet             raknet.Packet
	world              *arenaAbilityWorld
	sessionKey         string
	gameID             uint32
	generation         uint64
	ownerObjectID      uint32
	objectID           uint32
	kind               arenaCombatPetKind
	startedAt          time.Time
	attackAbility      sim.AbilityDefinition
	petDamage          float32
	damageBuff         float32
	deletePacket       []byte
	modifierPacket     []byte
	modifierInstanceID uint32
	plasmaOwner        *plasmaSentinelActiveRun
	plasmaIndex        int
	attackCancel       raknet.CancelSchedule
	activeAttack       *abilityraknet.MeleeRun
	cycleCancel        raknet.CancelSchedule
	expireCancel       raknet.CancelSchedule
	lifetimeCancel     func()
	isPublished        bool
	isCorpseRetained   bool
}

func (e *arenaCombatPetRun) currentLocked() (gameplayPeerSession, zonecompanion.Actor, bool) {
	match := e.runtime.registry.arenaMatches[e.gameID]
	owner, isOwnerFound := e.runtime.registry.sessions[e.sessionKey]
	if e.runtime.registry.isClosed || match == nil || !match.isCombatActive ||
		match.abilityWorld != e.world || e.world.combatPets[e.objectID] != e ||
		!isOwnerFound || owner.generation != e.generation || owner.binding.Mode != game.ModeArena ||
		owner.binding.GameID != e.gameID || !owner.isArenaCombatActive ||
		owner.deployedObjectID != e.ownerObjectID ||
		(owner.deployedHitPoint() <= 0 && !(e.kind == arenaCombatPetBeast && owner.squad != nil && owner.squad.IsResurrectionPending())) {
		return gameplayPeerSession{}, zonecompanion.Actor{}, false
	}
	actor, isActorFound := e.world.companion.Snapshot(e.objectID)
	if !isActorFound || actor.HitPoint <= 0 || actor.UserID != owner.binding.UserID ||
		actor.PeerGeneration != e.generation || actor.OwnerObjectID != e.ownerObjectID {
		return gameplayPeerSession{}, zonecompanion.Actor{}, false
	}
	return owner, actor, true
}

func (e *arenaCombatPetRun) stopAttackLocked() {
	if e.activeAttack != nil {
		e.activeAttack.Stop()
		e.activeAttack = nil
	}
	if e.attackCancel != nil {
		e.attackCancel()
		e.attackCancel = nil
	}
	if e.cycleCancel != nil {
		e.cycleCancel()
		e.cycleCancel = nil
	}
	if e.expireCancel != nil {
		e.expireCancel()
		e.expireCancel = nil
	}
	if e.lifetimeCancel != nil {
		e.lifetimeCancel()
		e.lifetimeCancel = nil
	}
}

func (e *arenaCombatPetRun) retireLocked() {
	if e.world.combatPets[e.objectID] != e {
		return
	}
	delete(e.world.combatPets, e.objectID)
	e.stopAttackLocked()
	if !e.isCorpseRetained {
		e.world.companion.Remove(e.objectID)
	}
	if e.plasmaOwner != nil && e.plasmaIndex >= 0 && e.plasmaIndex < len(e.plasmaOwner.pets) &&
		e.plasmaOwner.pets[e.plasmaIndex].objectID == e.objectID {
		e.plasmaOwner.pets[e.plasmaIndex] = plasmaSentinelPetRun{}
		if !e.plasmaOwner.isShieldActive && !e.plasmaOwner.hasPets() {
			owner, isOwnerFound := e.runtime.registry.sessions[e.sessionKey]
			if isOwnerFound && owner.generation == e.generation && owner.plasmaSentinelActive == e.plasmaOwner {
				owner.plasmaSentinelActive = nil
				e.runtime.registry.sessions[e.sessionKey] = owner
			}
		}
	}
	if e.modifierInstanceID != 0 {
		err := e.runtime.modifierPool.Release(e.modifierInstanceID)
		if err != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena pet modifier release failed object=%d: %v", e.objectID, err)
		}
		e.modifierInstanceID = 0
	}
	if e.isPublished {
		packets := make([][]byte, 0, 2)
		if len(e.modifierPacket) != 0 {
			packets = append(packets, e.modifierPacket)
		}
		packets = append(packets, e.deletePacket)
		e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
	}
}

func (e *gameplaySessionRegistry) retireArenaCombatPetsLocked(gameID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	for _, run := range match.abilityWorld.combatPets {
		run.retireLocked()
	}
}

func (e *gameplaySessionRegistry) retireArenaCombatPetsForOwnerLocked(gameID uint32, objectID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	for _, run := range match.abilityWorld.combatPets {
		if run.ownerObjectID == objectID {
			owner, isOwnerFound := e.sessions[run.sessionKey]
			if run.kind == arenaCombatPetBeast && isOwnerFound && owner.generation == run.generation &&
				owner.squad != nil && owner.squad.IsResurrectionPending() {
				continue
			}
			run.retireLocked()
		}
	}
}

func (e *arenaCombatPetRun) expire() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	_, _, isCurrent := e.currentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.isPublished = false
	e.retireLocked()
	e.runtime.registry.mutex.Unlock()
	packets := make([][]byte, 0, 2)
	if len(e.modifierPacket) != 0 {
		packets = append(packets, e.modifierPacket)
	}
	return append(packets, e.deletePacket), nil
}

func (e *arenaCombatPetRun) fail(cause error) {
	e.runtime.registry.mutex.Lock()
	_, _, isCurrent := e.currentLocked()
	if isCurrent {
		e.retireLocked()
	}
	e.runtime.registry.mutex.Unlock()
	if cause != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena companion failed kind=%d game=%d object=%d: %v", e.kind, e.gameID, e.objectID, cause)
	}
}

func (e *arenaCombatPetRun) ensurePublished() error {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	_, _, isCurrent := e.currentLocked()
	if !isCurrent {
		return fmt.Errorf("companionPublish: actor unavailable")
	}
	e.isPublished = true
	return nil
}
