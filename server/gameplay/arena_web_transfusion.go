package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The shared channel owns tick and cleanup timing. Arena replaces its NPC
// population with live opponents and commits their defense rolls in the match.
// The registry mutex is held on entry and released before returning.
func (e heroDrainSchedule) tickArenaWebTransfusionLocked(
	peerSession gameplayPeerSession, radius float32, deadline time.Duration,
) ([][]byte, error) {
	timestamp := e.packet.SourceTime + uint64(deadline/time.Millisecond)
	targets := e.runtime.registry.abilityTargetsLocked(
		e.runtime, peerSession, e.definition, timestamp,
	).(*arenaAbilityTargets)
	snapshots := make([]zonenpc.Snapshot, 0)
	for _, target := range targets.LiveSnapshots() {
		if zonegeometry.Distance(game.Vec3(peerSession.playerPosition), target.Plan.Position) <= radius {
			snapshots = append(snapshots, target)
		}
	}
	err := e.refreshWebTransfusionEffects(snapshots)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaWebEffects: %w", err)
	}
	peerSession = e.runtime.registry.sessions[e.sessionKey]
	damage, err := zoneability.ProjectDamage(
		e.creature, e.definition, e.definition.MinimumDamage, e.definition.MaximumDamage,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaWebProject: %w", err)
	}
	definition := e.definition
	definition.HitEffectName = ""
	random, err := e.runtime.registry.abilityRandomLocked(peerSession)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaWebRandom: %w", err)
	}
	results, err := zoneability.CommitArea(random, targets, zoneability.AreaPlan{
		SourceObjectID: e.sourceObjectID, AbilityID: util.HashID(definition.Name),
		Definition: definition, Damage: damage, Target: snapshots,
		Center: game.Vec3(peerSession.playerPosition),
	}, e.creature, peerSession.binding.Difficulty, e.runtime.program.Critical)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaWebCommit: %w", err)
	}
	damageAmount := float32(0)
	for _, result := range results {
		damageAmount += result.Damage.Damage
	}
	previousHitPoint := peerSession.deployedHitPoint()
	healingPackets, err := e.healWebTransfusion(&peerSession, damageAmount)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaWebHealing: %w", err)
	}
	healedAmount := peerSession.deployedHitPoint() - previousHitPoint
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	match := e.runtime.registry.arenaMatches[peerSession.binding.GameID]
	if match != nil && match.isCombatActive && healedAmount > 0 {
		match.result.recordHealing(peerSession.binding.UserID, peerSession.binding.UserID, healedAmount)
	}
	e.runtime.registry.queueArenaAbilityResourcesLocked(peerSession, healingPackets)
	e.runtime.registry.mutex.Unlock()
	targets.finish()
	packets, err := targets.packets(results)
	if err != nil {
		return nil, fmt.Errorf("arenaWebPackets: %w", err)
	}
	return append(packets, healingPackets...), nil
}
