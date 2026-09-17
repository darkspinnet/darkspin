package ability

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type BasicPlan struct {
	SourceObjectID uint32
	TargetObjectID uint32
	SourcePosition game.Vec3
	AbilityID      uint32
	Definition     sim.AbilityDefinition
	Damage         game.DamageRange
}

type BasicCommand struct {
	NPCs           *zonenpc.Session
	SourceObjectID uint32
	TargetObjectID uint32
	AbilityID      uint32
	SourcePosition game.Vec3
	MaximumRange   float32
	Definition     sim.AbilityDefinition
	Damage         game.DamageRange
}

type BasicResult struct {
	SelectedDamage float32
	Damage         zonenpc.DamageResult
	IsCritical     bool
}

type ProjectedBasicCommand struct {
	NPCs           *zonenpc.Session
	SourceObjectID uint32
	TargetObjectID uint32
	SourcePosition game.Vec3
	MaximumRange   float32
	Creature       game.GameplayCreature
	Definition     sim.AbilityDefinition
}

type TargetRangeError struct {
	Distance float32
	Maximum  float32
}

func (e TargetRangeError) Error() string {
	return fmt.Sprintf("targetRange: got %g, want <= %g", e.Distance, e.Maximum)
}

func PlanBasic(command BasicCommand) (BasicPlan, error) {
	if command.NPCs == nil {
		return BasicPlan{}, errors.New("nil enemy session")
	}
	if command.SourceObjectID == 0 || command.AbilityID == 0 {
		return BasicPlan{}, errors.New("invalid ability identity")
	}
	if command.Definition.Name == "" || command.Definition.Kind == "" ||
		command.Definition.Range <= 0 || command.MaximumRange <= 0 ||
		command.Definition.AnimationName == "" ||
		command.Definition.HitDelay < 0 || command.Definition.ReleaseDelay < 0 {
		return BasicPlan{}, errors.New("invalid ability definition")
	}
	if !isFinitePosition(command.SourcePosition) {
		return BasicPlan{}, errors.New("invalid source position")
	}
	if command.Damage.Minimum <= 0 || command.Damage.Maximum < command.Damage.Minimum {
		return BasicPlan{}, errors.New("invalid projected damage")
	}
	targetObjectID := command.TargetObjectID
	if targetObjectID == 0 {
		targetObjectID = NearestTarget(
			command.NPCs, command.SourceObjectID,
			command.SourcePosition, command.MaximumRange,
		)
	}
	plan := BasicPlan{
		SourceObjectID: command.SourceObjectID, TargetObjectID: targetObjectID,
		SourcePosition: command.SourcePosition,
		AbilityID:      command.AbilityID, Definition: command.Definition, Damage: command.Damage,
	}
	if targetObjectID == 0 {
		return plan, nil
	}
	enemy, isFound := command.NPCs.NPC(targetObjectID)
	if !isFound || enemy.IsDefeated || enemy.HitPoint <= 0 {
		return BasicPlan{}, fmt.Errorf("targetUnavailable: %d", targetObjectID)
	}
	distance := positionDistance(command.SourcePosition, enemy.Plan.Position)
	if distance > command.MaximumRange {
		return BasicPlan{}, TargetRangeError{
			Distance: distance, Maximum: command.MaximumRange,
		}
	}
	return plan, nil
}

func PlanProjectedBasic(command ProjectedBasicCommand) (BasicPlan, error) {
	if command.SourceObjectID == 0 || command.Creature.Noun == 0 {
		return BasicPlan{}, errors.New("invalid ability identity")
	}
	projected, err := ProjectTiming(command.Creature, command.Definition)
	if err != nil {
		return BasicPlan{}, fmt.Errorf("abilityTiming: %w", err)
	}
	damage, err := ProjectDamage(
		command.Creature, projected,
		projected.MinimumDamage, projected.MaximumDamage,
	)
	if err != nil {
		return BasicPlan{}, fmt.Errorf("abilityDamage: %w", err)
	}
	plan, err := PlanBasic(BasicCommand{
		NPCs:           command.NPCs,
		SourceObjectID: command.SourceObjectID,
		TargetObjectID: command.TargetObjectID,
		AbilityID:      util.HashID(projected.Name),
		SourcePosition: command.SourcePosition,
		MaximumRange:   command.MaximumRange,
		Definition:     projected,
		Damage:         damage,
	})
	if err != nil {
		return BasicPlan{}, fmt.Errorf("abilityPlan: %w", err)
	}
	return plan, nil
}

func PlanProjected(
	enemies *zonenpc.Session, sourceObjectID uint32, targetObjectID uint32,
	sourcePosition game.Vec3, creature game.GameplayCreature,
	definition sim.AbilityDefinition, maximumRange float32,
) (BasicPlan, error) {
	plan, err := PlanProjectedBasic(ProjectedBasicCommand{
		NPCs: enemies, SourceObjectID: sourceObjectID,
		TargetObjectID: targetObjectID, SourcePosition: sourcePosition,
		MaximumRange: maximumRange, Creature: creature, Definition: definition,
	})
	if err != nil {
		return BasicPlan{}, fmt.Errorf("basicPlan: %w", err)
	}
	return plan, nil
}

func NearestTarget(
	enemies *zonenpc.Session, sourceObjectID uint32,
	sourcePosition game.Vec3, maximumRange float32,
) uint32 {
	if enemies == nil || sourceObjectID == 0 || maximumRange <= 0 {
		return 0
	}
	selectedObjectID := uint32(0)
	selectedDistance := maximumRange
	for _, enemy := range enemies.LiveSnapshots() {
		if enemy.TargetObjectID != sourceObjectID {
			continue
		}
		distance := positionDistance(sourcePosition, enemy.Plan.Position)
		if distance > selectedDistance {
			continue
		}
		if distance == selectedDistance &&
			selectedObjectID != 0 && enemy.Plan.ObjectID > selectedObjectID {
			continue
		}
		selectedObjectID = enemy.Plan.ObjectID
		selectedDistance = distance
	}
	return selectedObjectID
}

// AdditionalMeleeTargets selects the nearest living enemies in the authored
// attack arc, excluding the primary target. The deterministic ordering keeps
// the same secondary victims authoritative for every connected client.
func AdditionalMeleeTargets(
	enemies *zonenpc.Session, primaryObjectID uint32,
	sourcePosition game.Vec3, targetPosition game.Vec3,
	maximumRange float32, angle float32, maximumCount uint32,
) ([]zonenpc.Snapshot, error) {
	if enemies == nil {
		return nil, errors.New("nil enemy session")
	}
	if primaryObjectID == 0 || maximumCount == 0 {
		return nil, nil
	}
	if !isFinitePosition(sourcePosition) || !isFinitePosition(targetPosition) ||
		invalidNumber(maximumRange) || maximumRange <= 0 ||
		invalidNumber(angle) || angle <= 0 || angle > 360 {
		return nil, errors.New("invalid melee arc")
	}
	forward := targetPosition.Sub(sourcePosition)
	forwardLength := forward.Length()
	if forwardLength <= 0 {
		return nil, errors.New("zero melee facing")
	}
	forward = forward.Scale(1 / forwardLength)
	halfAngleCosine := float32(math.Cos(float64(angle) * math.Pi / 360))
	type candidate struct {
		snapshot zonenpc.Snapshot
		distance float32
	}
	candidates := make([]candidate, 0)
	for _, enemy := range enemies.LiveSnapshots() {
		if enemy.Plan.ObjectID == primaryObjectID || enemy.IsDefeated ||
			!enemy.IsPublished ||
			enemy.HitPoint <= 0 || enemy.Faction != zonenpc.FactionNonPlayerAligned {
			continue
		}
		toEnemy := enemy.Plan.Position.Sub(sourcePosition)
		distance := toEnemy.Length()
		footprintRadius := max(float32(0), enemy.Plan.NPCProfile.FootprintRadius)
		if distance <= 0 || distance > maximumRange+footprintRadius {
			continue
		}
		direction := toEnemy.Scale(1 / distance)
		dot := forward.X*direction.X + forward.Y*direction.Y + forward.Z*direction.Z
		if dot < halfAngleCosine {
			continue
		}
		candidates = append(candidates, candidate{snapshot: enemy, distance: distance})
	}
	slices.SortFunc(candidates, func(first candidate, second candidate) int {
		if distanceOrder := cmp.Compare(first.distance, second.distance); distanceOrder != 0 {
			return distanceOrder
		}
		return cmp.Compare(first.snapshot.Plan.ObjectID, second.snapshot.Plan.ObjectID)
	})
	count := min(len(candidates), int(maximumCount))
	targets := make([]zonenpc.Snapshot, 0, count)
	for _, candidate := range candidates[:count] {
		targets = append(targets, candidate.snapshot)
	}
	return targets, nil
}

func CommitBasic(
	random *sim.SimulatorRandom, enemies *zonenpc.Session, plan BasicPlan,
	creature game.GameplayCreature, difficulty uint32, tuning sim.CriticalTuning,
) (BasicResult, error) {
	err := ValidateBasicCommit(random, enemies, plan, creature, difficulty, tuning)
	if err != nil {
		return BasicResult{}, fmt.Errorf("commitValidate: %w", err)
	}
	selectedDamage, err := sim.SelectRankDamage(random, sim.DamageRange{
		Minimum: plan.Damage.Minimum, Maximum: plan.Damage.Maximum,
	})
	if err != nil {
		return BasicResult{}, fmt.Errorf("commitSelect: %w", err)
	}
	if creature.PassiveBehindDamage > 0 {
		target, isTargetFound := enemies.NPC(plan.TargetObjectID)
		if isTargetFound && isRearDirectHit(target, plan.SourcePosition) {
			selectedDamage *= 1 + creature.PassiveBehindDamage
		}
	}
	critical, err := sim.ResolveCriticalDamage(
		random, selectedDamage, difficulty,
		sim.CriticalProfile{
			Rating: creature.CriticalRating, AutoCrit: creature.AutoCrit,
			DamageIncrease: creature.CriticalDamageIncrease,
		},
		tuning,
	)
	if err != nil {
		return BasicResult{}, fmt.Errorf("commitCritical: %w", err)
	}
	damage, err := enemies.Hit(zonenpc.HitRequest{
		SourceObjectID: plan.SourceObjectID, TargetObjectID: plan.TargetObjectID, Damage: critical.Damage,
		SourcePosition: &plan.SourcePosition, Metadata: NPCDamageMetadata(plan.Definition),
	})
	if err != nil {
		return BasicResult{}, fmt.Errorf("commitDamage: %w", err)
	}
	return BasicResult{
		SelectedDamage: selectedDamage, Damage: damage, IsCritical: critical.IsCritical,
	}, nil
}

func isRearDirectHit(target zonenpc.Snapshot, sourcePosition game.Vec3) bool {
	if target.Facing.Length() <= 0 {
		return false
	}
	toSource := sourcePosition.Sub(target.Plan.Position)
	length := toSource.Length()
	if length <= 0 {
		return false
	}
	toSource = toSource.Scale(1 / length)
	return target.Facing.X*toSource.X+
		target.Facing.Y*toSource.Y+
		target.Facing.Z*toSource.Z < 0
}

func ValidateBasicCommit(
	random *sim.SimulatorRandom, enemies *zonenpc.Session, plan BasicPlan,
	creature game.GameplayCreature, difficulty uint32, tuning sim.CriticalTuning,
) error {
	if random == nil {
		return errors.New("nil simulator random")
	}
	if enemies == nil {
		return errors.New("nil enemy session")
	}
	if plan.SourceObjectID == 0 || plan.TargetObjectID == 0 || plan.AbilityID == 0 {
		return errors.New("invalid ability plan")
	}
	minimum := float64(plan.Damage.Minimum)
	maximum := float64(plan.Damage.Maximum)
	if math.IsNaN(minimum) || math.IsNaN(maximum) || math.IsInf(minimum, 0) ||
		math.IsInf(maximum, 0) || minimum <= 0 || minimum > maximum ||
		math.Trunc(minimum) != minimum || math.Trunc(maximum) != maximum ||
		maximum-minimum+1 > math.MaxUint32 {
		return errors.New("invalid damage range")
	}
	if invalidNumber(tuning.DamageBonus) || tuning.DamageBonus <= 0 {
		return errors.New("invalid critical damage bonus")
	}
	if invalidNumber(creature.CriticalRating) || creature.CriticalRating < 0 ||
		invalidNumber(creature.AutoCrit) || invalidNumber(creature.CriticalDamageIncrease) {
		return errors.New("invalid critical profile")
	}
	if creature.AutoCrit <= 0 {
		if difficulty == 0 || difficulty > uint32(len(tuning.RatingConversions)) {
			return errors.New("invalid critical difficulty")
		}
		conversion := tuning.RatingConversions[difficulty-1]
		if invalidNumber(conversion) || conversion <= 0 {
			return errors.New("invalid critical conversion")
		}
	}
	enemy, isFound := enemies.NPC(plan.TargetObjectID)
	if !isFound || enemy.IsDefeated || enemy.HitPoint <= 0 {
		return fmt.Errorf("targetUnavailable: %d", plan.TargetObjectID)
	}
	return nil
}

func positionDistance(first game.Vec3, second game.Vec3) float32 {
	deltaX := float64(first.X - second.X)
	deltaY := float64(first.Y - second.Y)
	deltaZ := float64(first.Z - second.Z)
	return float32(math.Sqrt(deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ))
}

func isFinitePosition(position game.Vec3) bool {
	return !invalidNumber(position.X) &&
		!invalidNumber(position.Y) &&
		!invalidNumber(position.Z)
}

func invalidNumber(number float32) bool {
	return math.IsNaN(float64(number)) || math.IsInf(float64(number), 0)
}
