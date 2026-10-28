package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
)

// Called with the registry locked; it consumes one authored orb only after a
// valid live player hit has been planned. All impact packets use the match FIFO.
func (e plasmaWreathSchedule) fireArenaWreathLocked(source gameplayPeerSession,
	targetObjectID uint32, timestamp uint64,
) ([][]byte, error) {
	match := e.runtime.registry.arenaMatches[source.binding.GameID]
	if match == nil || !match.isCombatActive || match.random == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, source, e.definition, timestamp).(*arenaAbilityTargets)
	plan, err := zoneability.PlanProjected(targets, e.sourceObjectID, targetObjectID,
		game.Vec3(source.playerPosition), e.creature, e.definition, zoneability.PlasmaWreathRadius)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaWreathPlan: %w", err)
	}
	if plan.TargetObjectID == 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	target, isFound := targets.NPC(plan.TargetObjectID)
	if !isFound {
		e.runtime.registry.mutex.Unlock()
		return nil, errors.New("arena Wreath target unavailable")
	}
	err = zoneability.ValidateBasicCommit(match.random, targets, plan, e.creature,
		source.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaWreathValidate: %w", err)
	}
	orbPacket, isEmpty, err := e.run.ConsumeOrb()
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaWreathOrb: %w", err)
	}
	e.runtime.registry.queuePlasmaWreathRemovalsLocked(&source, e.run, [][]byte{orbPacket})
	e.runtime.registry.sessions[e.sessionKey] = source
	result, err := zoneability.CommitBasic(match.random, targets, plan, e.creature,
		source.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaWreathDamage: %w", err)
	}
	cleanupPackets := make([][]byte, 0)
	if isEmpty {
		source.plasmaWreathState.cancel()
		removals, stopErr := e.run.Stop()
		source.plasmaWreathRun = nil
		e.runtime.registry.queuePlasmaWreathRemovalsLocked(&source, e.run, removals)
		if stopErr != nil {
			e.runtime.registry.sessions[e.sessionKey] = source
			e.runtime.registry.mutex.Unlock()
			targets.finish()
			return nil, fmt.Errorf("arenaWreathStop: %w", stopErr)
		}
		attributePacket, attributeErr := e.runtime.registry.plasmaWreathAttributePacket(source, e.runtime.now())
		if attributeErr != nil {
			e.runtime.registry.sessions[e.sessionKey] = source
			e.runtime.registry.mutex.Unlock()
			targets.finish()
			return nil, fmt.Errorf("arenaWreathAttribute: %w", attributeErr)
		}
		cleanupPackets = append(cleanupPackets, attributePacket)
	}
	e.runtime.registry.sessions[e.sessionKey] = source
	e.runtime.registry.mutex.Unlock()
	hitPackets, err := targets.packets([]zoneability.AreaResult{{
		Snapshot: target, Damage: result.Damage, IsCritical: result.IsCritical,
		Definition: plan.Definition,
	}})
	if err != nil {
		targets.finish()
		return nil, fmt.Errorf("arenaWreathPackets: %w", err)
	}
	effectPackets, err := abilityraknet.PlasmaWreathHitPackets(e.sourceObjectID, plan.TargetObjectID)
	if err != nil {
		targets.finish()
		return nil, fmt.Errorf("arenaWreathEffects: %w", err)
	}
	packets := append(hitPackets, effectPackets...)
	packets = append(packets, cleanupPackets...)
	e.runtime.registry.mutex.Lock()
	e.runtime.registry.queueArenaForcePulsePacketsLocked(source.binding.GameID, packets)
	e.runtime.registry.mutex.Unlock()
	targets.finish()
	return nil, nil
}
