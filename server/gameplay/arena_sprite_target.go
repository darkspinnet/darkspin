package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e arenaAbilityTargets) sprite(objectID uint32) (*arenaSpriteRun, zonecompanion.Actor, bool) {
	match := e.runtime.registry.arenaMatches[e.source.binding.GameID]
	if match == nil || !match.isCombatActive || match.abilityWorld == nil || match.abilityWorld.companion == nil {
		return nil, zonecompanion.Actor{}, false
	}
	run := match.abilityWorld.sprites[objectID]
	if run == nil || run.world != match.abilityWorld || run.gameID != e.source.binding.GameID ||
		run.objectID != objectID || !run.isPublished {
		return nil, zonecompanion.Actor{}, false
	}
	owner, isCurrent := run.isCurrentLocked()
	if !isCurrent || owner.binding.Team == e.source.binding.Team || owner.deployedObjectID != run.ownerObjectID {
		return nil, zonecompanion.Actor{}, false
	}
	actor, isFound := run.world.companion.Snapshot(objectID)
	if !isFound || actor.HitPoint <= 0 || !actor.IsTargetable ||
		actor.UserID != owner.binding.UserID || actor.PeerGeneration != owner.generation ||
		actor.OwnerObjectID != run.ownerObjectID {
		return nil, zonecompanion.Actor{}, false
	}
	for _, motion := range run.world.companion.MotionSnapshots(e.runtime.now()) {
		if motion.Actor.ObjectID == objectID {
			actor.Position = motion.Actor.Position
			break
		}
	}
	return run, actor, true
}

func (e arenaAbilityTargets) spriteSnapshot(objectID uint32) (zonenpc.Snapshot, bool) {
	_, actor, isFound := e.sprite(objectID)
	if !isFound {
		return zonenpc.Snapshot{}, false
	}
	return zonenpc.Snapshot{
		Plan: zonenpc.SpawnPlan{
			ObjectID: objectID, Position: actor.Position,
			NPCProfile: game.CampaignNPCProfile{FootprintRadius: actor.FootprintRadius, IsTargetable: true},
		},
		HitPoint: actor.HitPoint, Faction: zonenpc.FactionNonPlayerAligned, IsPublished: true,
	}, true
}

func (e *arenaAbilityTargets) hitSpriteLocked(run *arenaSpriteRun, actor zonecompanion.Actor, req zonenpc.HitRequest) (zonenpc.DamageResult, error) {
	previousHealth := actor.HitPoint
	damage := max(float32(0), min(req.Damage, previousHealth))
	hitPoint := previousHealth - damage
	healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: run.objectID, HitPoints: hitPoint, IsHitPointChanged: true,
	})
	if err != nil {
		return zonenpc.DamageResult{}, fmt.Errorf("arenaSpriteHealth: %w", err)
	}
	if damage > 0 {
		_, _, err = run.world.companion.SetHitPoint(run.objectID, hitPoint)
		if err != nil {
			return zonenpc.DamageResult{}, fmt.Errorf("arenaSpriteHit: %w", err)
		}
	}
	packets := [][]byte{healthPacket}
	if hitPoint <= 0 {
		run.world.companion.Remove(run.objectID)
		delete(run.world.sprites, run.objectID)
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		packets = append(packets, run.deletePacket)
	}
	e.hits[run.objectID] = arenaAbilityHit{sourceObjectID: req.SourceObjectID, packets: packets}
	return zonenpc.DamageResult{
		ObjectID: run.objectID, PreviousHealth: previousHealth, HitPoint: hitPoint,
		Damage: damage, ResolvedDamage: damage, IsDefeated: hitPoint <= 0,
	}, nil
}
