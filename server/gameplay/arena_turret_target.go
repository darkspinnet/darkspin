package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e arenaAbilityTargets) turret(objectID uint32) (*arenaClaymoreRun, bool) {
	match := e.runtime.registry.arenaMatches[e.source.binding.GameID]
	if match == nil || !match.isCombatActive || match.abilityWorld == nil {
		return nil, false
	}
	run := match.abilityWorld.claymores[objectID]
	if run == nil || run.definition.Name != "TurretTrap" || !run.isPublished || run.hitPoint <= 0 ||
		run.world != match.abilityWorld || run.gameID != e.source.binding.GameID ||
		run.objectID != objectID || run.footprint <= 0 {
		return nil, false
	}
	owner, isCurrent := run.isCurrentLocked()
	if !isCurrent || owner.binding.Team == e.source.binding.Team ||
		owner.deployedObjectID != run.ownerObjectID || owner.deployedHitPoint() <= 0 {
		return nil, false
	}
	return run, true
}

func (e arenaAbilityTargets) turretSnapshot(objectID uint32) (zonenpc.Snapshot, bool) {
	run, isFound := e.turret(objectID)
	if !isFound {
		return zonenpc.Snapshot{}, false
	}
	return zonenpc.Snapshot{
		Plan: zonenpc.SpawnPlan{
			ObjectID: objectID, Position: game.Vec3(run.position),
			NPCProfile: game.CampaignNPCProfile{FootprintRadius: run.footprint, IsTargetable: true},
		},
		HitPoint: run.hitPoint, Faction: zonenpc.FactionNonPlayerAligned, IsPublished: true,
	}, true
}

func (e *arenaAbilityTargets) hitTurretLocked(run *arenaClaymoreRun, req zonenpc.HitRequest) (zonenpc.DamageResult, error) {
	previousHealth := run.hitPoint
	damage := max(float32(0), min(req.Damage, previousHealth))
	remainingHealth := previousHealth - damage
	healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: run.objectID, HitPoints: remainingHealth, IsHitPointChanged: true,
	})
	if err != nil {
		return zonenpc.DamageResult{}, fmt.Errorf("arenaTurretHealth: %w", err)
	}
	packets := [][]byte{healthPacket}
	if remainingHealth <= 0 {
		brokenPacket, brokenErr := abilityraknet.TrapObjectEffect(run.definition.HitEffectName, run.objectID, run.ownerObjectID)
		if brokenErr != nil {
			return zonenpc.DamageResult{}, fmt.Errorf("arenaTurretBroken: %w", brokenErr)
		}
		packets = append(packets, brokenPacket, run.deletePacket)
	}
	run.hitPoint = remainingHealth
	if remainingHealth <= 0 {
		delete(run.world.claymores, run.objectID)
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		if run.triggerCancel != nil {
			run.triggerCancel()
			run.triggerCancel = nil
		}
	}
	e.hits[run.objectID] = arenaAbilityHit{sourceObjectID: req.SourceObjectID, packets: packets}
	return zonenpc.DamageResult{
		ObjectID: run.objectID, PreviousHealth: previousHealth, HitPoint: remainingHealth,
		Damage: damage, ResolvedDamage: damage, IsDefeated: remainingHealth <= 0,
	}, nil
}
