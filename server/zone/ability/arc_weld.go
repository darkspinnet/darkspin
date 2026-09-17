package ability

import (
	"errors"
	"fmt"
	"slices"
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

const (
	campaignArcWeldLeapRadius     = float32(7)
	campaignArcWeldMaximumChain   = 4
	campaignArcWeldDamageIncrease = float32(0.25)
	ArcWeldTimeBetweenChain       = 100 * time.Millisecond
)

var campaignArcWeldBeamEffectName = [...]string{
	"cyber_energy_arcweld_beam1.ServerEventDef",
	"cyber_energy_arcweld_beam2.ServerEventDef",
	"cyber_energy_arcweld_beam3.ServerEventDef",
	"cyber_energy_arcweld_beam4.ServerEventDef",
}

func PlanArcWeld(
	enemies *zonenpc.Session, sourceObjectID uint32, targetObjectID uint32,
	sourcePosition game.Vec3, creature game.GameplayCreature, definition sim.AbilityDefinition,
) (AreaPlan, error) {
	if enemies == nil || sourceObjectID == 0 || targetObjectID == 0 || creature.Noun == 0 ||
		definition.Name != "EnergySentinelActive" || definition.Kind != sim.AbilityKindMelee ||
		definition.Range <= 0 || definition.AnimationName == "" || definition.HitEffectName == "" ||
		definition.HitDelay < 0 || definition.ReleaseDelay < definition.HitDelay ||
		!isFinitePosition(sourcePosition) {
		return AreaPlan{}, errors.New("invalid Arc Weld ability")
	}
	projected, err := ProjectTiming(creature, definition)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("arcWeldTiming: %w", err)
	}
	damage, err := ProjectDamage(
		creature, projected, projected.MinimumDamage, projected.MaximumDamage,
	)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("arcWeldDamage: %w", err)
	}
	primary, isPrimaryFound := enemies.NPC(targetObjectID)
	admissionRange := game.AbilityAdmissionRange(
		projected.Range, creature.RangeIncrease, projected.DescriptorMask,
		projected.IsDescriptorFound,
	)
	primaryFootprintRadius := max(float32(0), primary.Plan.NPCProfile.FootprintRadius)
	if !isPrimaryFound || primary.IsDefeated || primary.HitPoint <= 0 ||
		primary.TargetObjectID != sourceObjectID ||
		positionDistance(sourcePosition, primary.Plan.Position) >
			admissionRange+primaryFootprintRadius {
		return AreaPlan{}, errors.New("Arc Weld target unavailable or out of range")
	}
	target := []zonenpc.Snapshot{primary}
	visited := map[uint32]bool{targetObjectID: true}
	currentPosition := primary.Plan.Position
	for range campaignArcWeldMaximumChain {
		candidate := make([]zonenpc.Snapshot, 0)
		for _, enemy := range enemies.LiveSnapshots() {
			if enemy.Plan.ObjectID == sourceObjectID || visited[enemy.Plan.ObjectID] ||
				enemy.TargetObjectID != sourceObjectID ||
				positionDistance(currentPosition, enemy.Plan.Position) > campaignArcWeldLeapRadius {
				continue
			}
			candidate = append(candidate, enemy)
		}
		if len(candidate) == 0 {
			break
		}
		slices.SortFunc(candidate, func(left zonenpc.Snapshot, right zonenpc.Snapshot) int {
			leftDistance := positionDistance(currentPosition, left.Plan.Position)
			rightDistance := positionDistance(currentPosition, right.Plan.Position)
			if leftDistance < rightDistance {
				return -1
			}
			if leftDistance > rightDistance {
				return 1
			}
			return int(left.Plan.ObjectID) - int(right.Plan.ObjectID)
		})
		next := candidate[0]
		target = append(target, next)
		visited[next.Plan.ObjectID] = true
		currentPosition = next.Plan.Position
	}
	return AreaPlan{
		SourceObjectID: sourceObjectID, AbilityID: util.HashID(projected.Name),
		Definition: projected, Damage: damage, Center: sourcePosition, Target: target,
	}, nil
}

func ArcWeldHitPlan(plan AreaPlan, index int) (AreaPlan, error) {
	if index < 0 || index >= len(plan.Target) || index > campaignArcWeldMaximumChain {
		return AreaPlan{}, errors.New("Arc Weld hit index unavailable")
	}
	hitPlan := plan
	hitPlan.Target = []zonenpc.Snapshot{plan.Target[index]}
	multiplier := 1 + campaignArcWeldDamageIncrease*float32(index)
	hitPlan.Damage.Minimum *= multiplier
	hitPlan.Damage.Maximum *= multiplier
	return hitPlan, nil
}

func ArcWeldBeamAsset(index int) uint32 {
	if index < 0 {
		return 0
	}
	index = min(index, len(campaignArcWeldBeamEffectName)-1)
	return util.HashID(campaignArcWeldBeamEffectName[index])
}
