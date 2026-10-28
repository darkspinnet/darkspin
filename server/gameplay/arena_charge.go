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
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Arena hero charges belong to their match rather than a campaign NPC zone.
type arenaHeroChargeRun struct {
	runtime        campaignAbilityCommandRuntime
	packet         raknet.Packet
	world          *arenaAbilityWorld
	sessionKey     string
	gameID         uint32
	generation     uint64
	objectID       uint32
	abilityID      uint32
	start          raknet.Vector3
	destination    raknet.Vector3
	targetPosition raknet.Vector3
	targetObjectID uint32
	creature       game.GameplayCreature
	definition     sim.AbilityDefinition
	damage         game.DamageRange
	impactDelay    time.Duration
	releaseDelay   time.Duration
	releasePacket  []byte
	removePacket   []byte
	effectSlot     uint8
	cancel         raknet.CancelSchedule
	isImpacted     bool
	startedAt      time.Time
}

func (e *arenaHeroChargeRun) currentLocked() (gameplayPeerSession, bool) {
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	match := e.runtime.registry.arenaMatches[e.gameID]
	isCurrent := isFound && !e.runtime.registry.isClosed && match != nil && match.isCombatActive &&
		match.abilityWorld == e.world && e.world.charges[e.objectID] == e &&
		source.generation == e.generation && source.binding.Mode == game.ModeArena &&
		source.binding.GameID == e.gameID && source.isArenaCombatActive &&
		source.deployedObjectID == e.objectID && source.deployedHitPoint() > 0
	return source, isCurrent
}

func (e *arenaHeroChargeRun) retireLocked() {
	if e.world.charges[e.objectID] != e {
		return
	}
	delete(e.world.charges, e.objectID)
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if len(e.removePacket) != 0 {
		e.runtime.effectPool.Release(e.objectID, e.effectSlot)
	}
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || source.generation != e.generation {
		return
	}
	packets := make([][]byte, 0, 4)
	if len(e.removePacket) != 0 {
		packets = append(packets, e.removePacket)
	}
	speedPacket, speedErr := heroChargeSpeedPacket(e.objectID, zonePlayerMoveSpeed)
	if speedErr == nil {
		packets = append(packets, speedPacket)
	}
	timestamp := e.packet.SourceTime + uint64(max(time.Duration(0), e.runtime.now().Sub(e.startedAt))/time.Millisecond)
	animationPacket, animationErr := abilityraknet.AnimationReset(e.objectID, timestamp)
	if animationErr == nil {
		packets = append(packets, animationPacket)
	}
	if !e.isImpacted {
		stopPackets, stopErr := marshalZonePlayerStop(e.objectID, source.playerPosition)
		if stopErr == nil {
			packets = append(packets, stopPackets...)
		}
	}
	e.runtime.registry.queueArenaForcePulsePacketsLocked(e.gameID, packets)
}

func (e *gameplaySessionRegistry) retireArenaChargesLocked(gameID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	for _, run := range match.abilityWorld.charges {
		run.retireLocked()
	}
}

func (e *gameplaySessionRegistry) retireArenaChargeForOwnerLocked(gameID uint32, objectID uint32) {
	match := e.arenaMatches[gameID]
	if match == nil || match.abilityWorld == nil {
		return
	}
	if run := match.abilityWorld.charges[objectID]; run != nil {
		run.retireLocked()
	}
}

func (e *arenaHeroChargeRun) slide() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	_, isCurrent := e.currentLocked()
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	packet, err := actionraknet.ChargeSlide(e.objectID, game.Vec3(e.start), game.Vec3(e.destination),
		zonePlayerMoveSpeed*e.definition.MovementSpeedMultiplier)
	if err != nil {
		return nil, fmt.Errorf("arenaSlide: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *arenaHeroChargeRun) rushDamageTargets(targets *arenaAbilityTargets) []zonenpc.Snapshot {
	if e.targetObjectID == 0 {
		return nil
	}
	target, isFound := targets.NPC(e.targetObjectID)
	if !isFound || game.Vec3(e.targetPosition).Sub(target.Plan.Position).Length() >
		e.definition.Radius+target.Plan.ActorFootprintRadius() {
		return nil
	}
	return []zonenpc.Snapshot{target}
}

func (e *arenaHeroChargeRun) rushRootTargets(targets *arenaAbilityTargets) []uint32 {
	targetIDs := make([]uint32, 0)
	for _, target := range targets.LiveSnapshots() {
		if game.Vec3(e.targetPosition).Sub(target.Plan.Position).Length() <= e.definition.Radius {
			targetIDs = append(targetIDs, target.Plan.ObjectID)
		}
	}
	return targetIDs
}

func (e *arenaHeroChargeRun) impact() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	source, isCurrent := e.currentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	positionPacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: e.objectID, PositionX: e.destination.X, PositionY: e.destination.Y,
		PositionZ: e.destination.Z, IsVisible: true,
	})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargePosition: %w", err)
	}
	stopPackets, err := marshalZonePlayerStop(e.objectID, e.destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeStop: %w", err)
	}
	speedPacket, err := heroChargeSpeedPacket(e.objectID, zonePlayerMoveSpeed)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeSpeed: %w", err)
	}
	movePackets := append([][]byte{positionPacket}, stopPackets...)
	movePackets = append(movePackets, speedPacket)
	if e.definition.Name == "EntanglingRush" {
		contactPacket, contactErr := abilityraknet.Animation(e.objectID, e.definition.OutAnimationName,
			e.packet.SourceTime+uint64(e.impactDelay/time.Millisecond))
		if contactErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaRushContact: %w", contactErr)
		}
		effectPacket, effectErr := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset: util.HashID(e.definition.MuzzleEffectName), Position: e.targetPosition,
		})
		if effectErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaRushEffect: %w", effectErr)
		}
		movePackets = append(movePackets, contactPacket, effectPacket)
	}
	err = source.teleportPlayer(e.runtime.now(), e.destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeTeleport: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = source
	e.isImpacted = true
	timestamp := e.packet.SourceTime + uint64(e.impactDelay/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.definition, timestamp).(*arenaAbilityTargets)
	if e.definition.Name == "BioRandom2" {
		roarPackets, roarErr := e.impactRoarLocked(&source, targets, timestamp)
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		if roarErr != nil {
			return movePackets, fmt.Errorf("arenaRoarImpact: %w", roarErr)
		}
		return append(movePackets, roarPackets...), nil
	}
	candidates := make([]zonenpc.Snapshot, 0)
	if e.definition.Name == "EntanglingRush" {
		candidates = e.rushDamageTargets(targets)
	} else {
		for _, candidate := range targets.LiveSnapshots() {
			if chargeSegmentDistance(game.Vec3(e.start), game.Vec3(e.destination), candidate.Plan.Position) <=
				e.definition.Radius+candidate.Plan.ActorFootprintRadius() {
				candidates = append(candidates, candidate)
			}
		}
	}
	plan := zoneability.AreaPlan{SourceObjectID: e.objectID, AbilityID: e.abilityID,
		Definition: e.definition, Damage: e.damage, Center: game.Vec3(e.destination), Target: candidates}
	results, err := zoneability.CommitArea(e.runtime.registry.arenaMatches[e.gameID].random, targets, plan,
		e.creature, source.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Phantom Charge damage failed after movement owner=%d: %v", e.objectID, err)
		}
		return movePackets, nil
	}
	targetIDs := make([]uint32, 0, len(results))
	if e.definition.Name == "EntanglingRush" {
		targetIDs = e.rushRootTargets(targets)
	} else {
		for _, result := range results {
			if !result.Damage.IsDamageImmune && !result.Damage.IsDefeated {
				targetIDs = append(targetIDs, result.Damage.ObjectID)
			}
		}
	}
	_, err = e.runtime.applyArenaTargetStatusLocked(e.sessionKey, e.generation, e.objectID, timestamp, plan, targetIDs)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena hero charge status failed owner=%d: %v", e.objectID, err)
	}
	var effect func(zoneability.AreaResult) ([]byte, error)
	if e.definition.HitEffectName != "" {
		impact := campaignPointBlankImpact{assetName: e.definition.HitEffectName,
			attackerID: e.objectID, source: e.destination}
		effect = impact.marshal
	}
	hitPackets, err := targets.packetsWithEffects(results, effect, true)
	if err != nil {
		fallbackPackets, fallbackErr := targets.packets(results)
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena hero charge impact effect failed owner=%d: %v", e.objectID, err)
		}
		if fallbackErr == nil {
			return append(movePackets, fallbackPackets...), nil
		}
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena hero charge damage presentation failed owner=%d: %v", e.objectID, fallbackErr)
		}
		return movePackets, nil
	}
	if e.definition.Name == "BeastCharge" {
		for _, objectID := range targetIDs {
			reactionPacket, reactionErr := abilityraknet.Animation(objectID, "react_knockup", timestamp)
			if reactionErr != nil {
				if e.runtime.logger != nil {
					e.runtime.logger.Printf("Arena Beast Charge reaction failed target=%d: %v", objectID, reactionErr)
				}
				continue
			}
			hitPackets = append(hitPackets, reactionPacket)
		}
	}
	e.runtime.registry.mutex.Unlock()
	targets.finish()
	return append(movePackets, hitPackets...), nil
}

func (e *arenaHeroChargeRun) release() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	_, isCurrent := e.currentLocked()
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	timestamp := e.packet.SourceTime + uint64(e.releaseDelay/time.Millisecond)
	var animationPacket []byte
	var err error
	if e.definition.OutAnimationName != "" && e.definition.Name != "EntanglingRush" {
		animationPacket, err = abilityraknet.Animation(e.objectID, e.definition.OutAnimationName, timestamp)
	} else {
		animationPacket, err = abilityraknet.AnimationReset(e.objectID, timestamp)
	}
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaChargeAnimationReset: %w", err)
	}
	delete(e.world.charges, e.objectID)
	e.cancel = nil
	if len(e.removePacket) != 0 {
		e.runtime.effectPool.Release(e.objectID, e.effectSlot)
	}
	e.runtime.registry.mutex.Unlock()
	packets := make([][]byte, 0, 3)
	if len(e.removePacket) != 0 {
		packets = append(packets, e.removePacket)
	}
	return append(packets, animationPacket, e.releasePacket), nil
}

func (e *arenaHeroChargeRun) fail(cause error) {
	e.runtime.registry.mutex.Lock()
	e.retireLocked()
	e.runtime.registry.mutex.Unlock()
	if cause != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena hero charge producer failed owner=%d: %v", e.objectID, cause)
	}
}
