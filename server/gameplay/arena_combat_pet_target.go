package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e arenaAbilityTargets) combatPet(objectID uint32) (*arenaCombatPetRun, zonecompanion.Actor, bool) {
	match := e.runtime.registry.arenaMatches[e.source.binding.GameID]
	if match == nil || !match.isCombatActive || match.abilityWorld == nil || match.abilityWorld.companion == nil {
		return nil, zonecompanion.Actor{}, false
	}
	run := match.abilityWorld.combatPets[objectID]
	if run == nil || !run.isPublished || run.world != match.abilityWorld ||
		run.gameID != e.source.binding.GameID || run.objectID != objectID {
		return nil, zonecompanion.Actor{}, false
	}
	owner, actor, isCurrent := run.currentLocked()
	if !isCurrent || owner.binding.Team == e.source.binding.Team || !actor.IsTargetable {
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

func (e arenaAbilityTargets) combatPetSnapshot(objectID uint32) (zonenpc.Snapshot, bool) {
	_, actor, isFound := e.combatPet(objectID)
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

func (e *arenaAbilityTargets) hitCombatPetLocked(run *arenaCombatPetRun, actor zonecompanion.Actor, req zonenpc.HitRequest) (zonenpc.DamageResult, error) {
	previousHealth := actor.HitPoint
	damage := max(float32(0), min(req.Damage, previousHealth))
	hitPoint := previousHealth - damage
	healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: run.objectID, HitPoints: hitPoint, IsHitPointChanged: true,
	})
	if err != nil {
		return zonenpc.DamageResult{}, fmt.Errorf("arenaPetHealth: %w", err)
	}
	deadActor := zonecompanion.Actor{}
	if damage > 0 {
		_, deadActor, err = run.world.companion.SetHitPoint(run.objectID, hitPoint)
		if err != nil {
			return zonenpc.DamageResult{}, fmt.Errorf("arenaPetHit: %w", err)
		}
	}
	packets := [][]byte{healthPacket}
	if hitPoint <= 0 {
		if _, isKnown := zonecompanion.DeathProfileForNoun(actor.Noun); isKnown {
			deathPackets, deathErr := run.beginDeathLocked(deadActor, e.timestamp)
			if deathErr == nil {
				packets = deathPackets
			} else if e.runtime.logger != nil {
				e.runtime.logger.Printf("Arena pet death fallback object=%d: %v", run.objectID, deathErr)
			}
		}
		if !run.isCorpseRetained {
			if len(run.modifierPacket) != 0 {
				packets = append(packets, run.modifierPacket)
			}
			run.isPublished = false
			run.retireLocked()
			packets = append(packets, run.deletePacket)
		}
	}
	e.hits[run.objectID] = arenaAbilityHit{sourceObjectID: req.SourceObjectID, packets: packets}
	return zonenpc.DamageResult{
		ObjectID: run.objectID, PreviousHealth: previousHealth, HitPoint: hitPoint,
		Damage: damage, ResolvedDamage: damage, IsDefeated: hitPoint <= 0,
	}, nil
}
