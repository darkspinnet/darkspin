package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

// Ride the Lightning's wind-up, teleport, strike, and release belong to the
// Arena match. Its target can disappear before arrival without undoing travel.
type arenaRideRun struct {
	runtime        campaignAbilityCommandRuntime
	packet         raknet.Packet
	world          *arenaAbilityWorld
	sessionKey     string
	gameID         uint32
	generation     uint64
	objectID       uint32
	abilityID      uint32
	destination    raknet.Vector3
	strikeRange    float32
	creature       game.GameplayCreature
	definition     sim.AbilityDefinition
	plan           zoneability.BasicPlan
	arrivalPackets [][]byte
	releasePacket  []byte
	cancel         raknet.CancelSchedule
	startedAt      time.Time
}

func (e *arenaRideRun) currentLocked() (gameplayPeerSession, bool) {
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	match := e.runtime.registry.arenaMatches[e.gameID]
	isCurrent := isFound && !e.runtime.registry.isClosed && match != nil && match.isCombatActive &&
		match.abilityWorld == e.world && e.world.rides[e.objectID] == e &&
		source.generation == e.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == e.gameID && source.isArenaCombatActive && source.stage.IsDungeon() &&
		source.deployedObjectID == e.objectID && source.deployedHitPoint() > 0
	return source, isCurrent
}

func (e *arenaRideRun) retireLocked() {
	if e.world.rides[e.objectID] != e {
		return
	}
	delete(e.world.rides, e.objectID)
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || source.generation != e.generation || source.deployedObjectID != e.objectID ||
		source.deployedHitPoint() <= 0 {
		return
	}
	timestamp := e.packet.SourceTime + uint64(max(time.Duration(0), e.runtime.now().Sub(e.startedAt))/time.Millisecond)
	packet, err := abilityraknet.AnimationReset(e.objectID, timestamp)
	if err != nil {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Ride the Lightning animation cleanup failed owner=%d: %v", e.objectID, err)
		}
		return
	}
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, [][]byte{packet})
}

func (e *gameplaySessionRegistry) retireArenaRidesLocked(gameID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	for _, run := range match.abilityWorld.rides {
		run.retireLocked()
	}
}

func (e *gameplaySessionRegistry) retireArenaRideForOwnerLocked(gameID uint32, objectID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	if run := match.abilityWorld.rides[objectID]; run != nil {
		run.retireLocked()
	}
}

func (e *arenaRideRun) arrival() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	source, isCurrent := e.currentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	err := source.teleportPlayer(e.runtime.now(), e.destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaRideTeleport: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = source
	packets := clonePendingPackets(e.arrivalPackets)
	if e.plan.TargetObjectID == 0 {
		e.runtime.registry.mutex.Unlock()
		return packets, nil
	}
	timestamp := e.packet.SourceTime + uint64(e.definition.HitDelay/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.definition, timestamp).(*arenaAbilityTargets)
	target, isFound := targets.NPC(e.plan.TargetObjectID)
	if !isFound || target.Plan.Position.Sub(game.Vec3(e.destination)).Length() > e.strikeRange {
		e.runtime.registry.mutex.Unlock()
		return packets, nil
	}
	match := e.runtime.registry.arenaMatches[e.gameID]
	result, err := zoneability.CommitBasic(match.random, targets, e.plan, e.creature,
		source.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Ride the Lightning damage failed after teleport owner=%d: %v", e.objectID, err)
		}
		return packets, nil
	}
	hit := zoneability.AreaResult{Snapshot: target, Damage: result.Damage,
		IsCritical: result.IsCritical, Definition: e.plan.Definition}
	hitPackets, err := targets.packets([]zoneability.AreaResult{hit})
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Ride the Lightning hit presentation failed owner=%d: %v", e.objectID, err)
	}
	if !result.Damage.IsDefeated && !result.Damage.IsDamageImmune &&
		!result.Damage.IsShieldStarted && !result.Damage.IsTurtleStarted {
		shock := e.definition
		shock.StatusKind = sim.AbilityStatusKindStun
		shock.StatusDuration = rideLightningShockDuration
		shock.RootModifierID = util.HashID("ShockModifier")
		plan := zoneability.AreaPlan{SourceObjectID: e.objectID,
			AbilityID: e.abilityID, Definition: shock}
		_, statusErr := e.runtime.applyArenaTargetStatusLocked(e.sessionKey, e.generation,
			e.objectID, timestamp, plan, []uint32{e.plan.TargetObjectID})
		if statusErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Ride the Lightning shock failed target=%d: %v", e.plan.TargetObjectID, statusErr)
		}
	}
	if result.Damage.IsDefeated {
		if source.abilityCooldownSession().Reset(zoneability.HeroAbilityCooldown(e.abilityID)) {
			resetPacket, resetErr := abilityraknet.CooldownReset(e.objectID, e.abilityID)
			if resetErr == nil {
				packets = append(packets, resetPacket)
			} else if e.runtime.logger != nil {
				e.runtime.logger.Printf("Arena Ride the Lightning cooldown reset presentation failed owner=%d: %v", e.objectID, resetErr)
			}
		}
	}
	e.runtime.registry.mutex.Unlock()
	targets.finish()
	return append(packets, hitPackets...), nil
}

func (e *arenaRideRun) release() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	_, isCurrent := e.currentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	delete(e.world.rides, e.objectID)
	e.cancel = nil
	e.runtime.registry.mutex.Unlock()
	return [][]byte{e.releasePacket}, nil
}

func (e *arenaRideRun) fail(cause error) {
	e.runtime.registry.mutex.Lock()
	e.retireLocked()
	e.runtime.registry.mutex.Unlock()
	if cause != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena Ride the Lightning producer failed owner=%d: %v", e.objectID, cause)
	}
}
