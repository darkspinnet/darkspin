package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type arenaTurretStep struct {
	run      *arenaClaymoreRun
	deadline time.Duration
}

func (e *arenaTurretStep) produce() ([][]byte, error) {
	e.run.runtime.registry.mutex.Lock()
	source, isCurrent := e.run.isCurrentLocked()
	if !isCurrent || e.run.definition.Name != "TurretTrap" || !e.run.isPublished {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	timestamp := e.run.packet.SourceTime + uint64(e.deadline/time.Millisecond)
	targets := e.run.runtime.registry.abilityTargetsLocked(e.run.runtime, source, e.run.definition, timestamp).(*arenaAbilityTargets)
	target := zonenpc.Snapshot{}
	nearestDistance := float32(math.MaxFloat32)
	for _, current := range targets.LiveSnapshots() {
		if current.IsDefeated || current.HitPoint <= 0 || current.Plan.IsFixture {
			continue
		}
		distance := game.Vec3(e.run.position).Sub(current.Plan.Position).Length()
		maximumRange := e.run.definition.Range + max(float32(0), current.Plan.NPCProfile.FootprintRadius)
		if distance > maximumRange || distance > nearestDistance {
			continue
		}
		if distance == nearestDistance && target.Plan.ObjectID != 0 && current.Plan.ObjectID > target.Plan.ObjectID {
			continue
		}
		target = current
		nearestDistance = distance
	}
	if target.Plan.ObjectID == 0 {
		e.run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	presentationPackets, err := abilityraknet.TurretLaser(abilityraknet.TurretLaserRequest{
		SourceObjectID: e.run.objectID, TargetObjectID: target.Plan.ObjectID,
		SourceTime: timestamp,
	})
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaTurretLaser: %w", err)
	}
	damage, err := zoneability.ProjectDamage(e.run.creature, e.run.definition,
		e.run.definition.MinimumDamage, e.run.definition.MaximumDamage)
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaTurretDamage: %w", err)
	}
	random, err := e.run.runtime.registry.abilityRandomLocked(source)
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaTurretRandom: %w", err)
	}
	definition := e.run.definition
	definition.HitEffectName = ""
	results, err := zoneability.CommitArea(random, targets, zoneability.AreaPlan{
		SourceObjectID: e.run.objectID, AbilityID: util.HashID(definition.Name),
		Definition: definition, Damage: damage, Center: game.Vec3(e.run.position),
		Target: []zonenpc.Snapshot{target}, IsSingleTarget: true,
	}, e.run.creature, source.binding.Difficulty, e.run.runtime.program.Critical)
	if err != nil {
		e.run.runtime.registry.mutex.Unlock()
		targets.finish()
		return nil, fmt.Errorf("arenaTurretCommit: %w", err)
	}
	packets, err := targets.packets(results)
	e.run.runtime.registry.mutex.Unlock()
	targets.finish()
	if err != nil {
		return nil, fmt.Errorf("arenaTurretPackets: %w", err)
	}
	return append(presentationPackets, packets...), nil
}
