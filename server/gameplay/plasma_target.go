package gameplay

import (
	"fmt"
	"github.com/darkspinnet/darkspin/server/game"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e plasmaWreathSchedule) isWreathTargetLocked(peerSession gameplayPeerSession, objectID uint32, isReactive bool) bool {
	if peerSession.binding.Mode != game.ModeArena {
		return isPlasmaWreathTarget(peerSession, objectID, isReactive)
	}
	if isReactive != isPlasmaWreathReactive(peerSession.plasmaWreathState.rank) {
		return false
	}
	targets := e.runtime.registry.abilityTargetsLocked(e.runtime, peerSession, e.definition, e.sourceTime).(*arenaAbilityTargets)
	_, victim, isFound := targets.member(objectID)
	if !isFound {
		return false
	}
	_, isFound = targets.NPC(objectID)
	if !isFound {
		return false
	}
	_, victim, isFound = targets.member(objectID)
	if !isFound {
		return false
	}
	distance := game.Vec3(victim.playerPosition).Sub(game.Vec3(peerSession.playerPosition)).Length()
	if isReactive {
		return distance < zoneability.PlasmaWreathRadius
	}
	return distance <= zoneability.PlasmaWreathRadius
}

func isPlasmaWreathReactive(rank int32) bool {
	return rank == 5 || rank == 6
}

func isPlasmaWreathTarget(peerSession gameplayPeerSession, objectID uint32, isReactive bool) bool {
	if peerSession.zone == nil || peerSession.zone.NPCs() == nil ||
		isReactive != isPlasmaWreathReactive(peerSession.plasmaWreathState.rank) {
		return false
	}
	target, isFound := peerSession.zone.NPCs().NPC(objectID)
	if !isFound || !target.IsPublished || target.IsDefeated || target.HitPoint <= 0 ||
		peerSession.zone.DirectorDefinition().NounTypeForAsset(target.Plan.NounName) != game.NounTypeCreature {
		return false
	}
	distance := target.Plan.Position.Sub(game.Vec3(peerSession.playerPosition)).Length()
	if isReactive {
		// The authored TookDamage callback checks type and strict distance,
		// without the proactive callback's hostile-target predicate.
		return distance < zoneability.PlasmaWreathRadius
	}
	return target.Faction == zonenpc.FactionNonPlayerAligned && distance <= zoneability.PlasmaWreathRadius
}

func (e plasmaWreathSchedule) produceTick() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || peerSession.deployedObjectID != e.sourceObjectID ||
		isPlasmaWreathReactive(peerSession.plasmaWreathState.rank) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	tickCount := e.run.AdvanceTick()
	targetObjectID := uint32(0)
	// Lua uses the unsorted query and ipairs, then permits only the first
	// successful orb. Preserve authoritative object order, not nearest range.
	var snapshots []zonenpc.Snapshot
	if peerSession.binding.Mode == game.ModeArena {
		targets := e.runtime.registry.abilityTargetsLocked(e.runtime, peerSession, e.definition, e.sourceTime).(*arenaAbilityTargets)
		snapshots = targets.LiveSnapshots()
	} else {
		snapshots = peerSession.zone.NPCs().LiveSnapshots()
	}
	for _, target := range snapshots {
		if !e.isWreathTargetLocked(peerSession, target.Plan.ObjectID, false) {
			continue
		}
		targetObjectID = target.Plan.ObjectID
		break
	}
	e.runtime.registry.mutex.Unlock()
	if targetObjectID == 0 {
		return nil, nil
	}
	timestamp := e.sourceTime + (tickCount-1)*uint64(zoneability.PlasmaWreathCheckInterval.Milliseconds())
	packets, err := e.fireTarget(targetObjectID, timestamp, false)
	if err != nil {
		return nil, fmt.Errorf("wreathScan: %w", err)
	}
	return packets, nil
}
