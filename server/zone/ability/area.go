package ability

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type AreaPlan struct {
	SourceObjectID uint32
	AbilityID      uint32
	Definition     sim.AbilityDefinition
	Damage         game.DamageRange
	Center         game.Vec3
	Facing         game.Vec3
	Target         []zonenpc.Snapshot
	DamageScale    map[uint32]float32
	TargetDamage   map[uint32]game.DamageRange
	// Single-target secondary hits can share damage selection and publication
	// without inheriting area mitigation from this executor.
	IsSingleTarget bool
}

type AreaResult struct {
	Snapshot   zonenpc.Snapshot
	Damage     zonenpc.DamageResult
	IsCritical bool
	Definition sim.AbilityDefinition
}

func projectAreaRadius(
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (sim.AbilityDefinition, error) {
	if !definition.IsAreaRadiusScaled {
		return definition, nil
	}
	radius, err := ProjectAreaRadius(creature, definition.Radius)
	if err != nil {
		return sim.AbilityDefinition{}, fmt.Errorf("areaRadius: %w", err)
	}
	definition.Radius = radius
	return definition, nil
}

func PlanShockwave(
	enemies *zonenpc.Session, sourceObjectID uint32, targetObjectID uint32,
	sourcePosition game.Vec3, targetPosition game.Vec3, creature game.GameplayCreature,
	definition sim.AbilityDefinition,
) (AreaPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 ||
		definition.Name == "" || definition.Kind != sim.AbilityKindMelee ||
		definition.Radius <= 0 || definition.AnimationName == "" ||
		definition.HitDelay < 0 || definition.ReleaseDelay < definition.HitDelay ||
		!isFinitePosition(sourcePosition) ||
		!isFinitePosition(targetPosition) {
		return AreaPlan{}, errors.New("invalid Shockwave ability")
	}
	directionX := targetPosition.X - sourcePosition.X
	directionY := targetPosition.Y - sourcePosition.Y
	directionZ := targetPosition.Z - sourcePosition.Z
	directionLength := float32(math.Sqrt(float64(
		directionX*directionX + directionY*directionY + directionZ*directionZ,
	)))
	if directionLength <= 0 {
		return AreaPlan{}, errors.New("Shockwave direction unavailable")
	}
	directionX /= directionLength
	directionY /= directionLength
	directionZ /= directionLength
	projected, err := ProjectTiming(creature, definition)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("shockwaveTiming: %w", err)
	}
	damage, err := ProjectDamage(
		creature, projected, projected.MinimumDamage, projected.MaximumDamage,
	)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("shockwaveDamage: %w", err)
	}
	const additionalRange = float32(4.5)
	eligible := make([]zonenpc.Snapshot, 0)
	primaryIndex := -1
	primaryDistance := projected.Radius + 1
	for _, enemy := range enemies.LiveSnapshots() {
		if enemy.TargetObjectID != sourceObjectID {
			continue
		}
		deltaX := enemy.Plan.Position.X - sourcePosition.X
		deltaY := enemy.Plan.Position.Y - sourcePosition.Y
		deltaZ := enemy.Plan.Position.Z - sourcePosition.Z
		distance := positionDistance(sourcePosition, enemy.Plan.Position)
		dot := deltaX*directionX + deltaY*directionY + deltaZ*directionZ
		if distance > projected.Radius || dot < 0 {
			continue
		}
		eligible = append(eligible, enemy)
		index := len(eligible) - 1
		isExplicit := targetObjectID != 0 && enemy.Plan.ObjectID == targetObjectID
		isSelectedExplicit := primaryIndex >= 0 && targetObjectID != 0 &&
			eligible[primaryIndex].Plan.ObjectID == targetObjectID
		if isExplicit || !isSelectedExplicit && (distance < primaryDistance ||
			distance == primaryDistance && (primaryIndex < 0 ||
				enemy.Plan.ObjectID < eligible[primaryIndex].Plan.ObjectID)) {
			primaryIndex = index
			primaryDistance = distance
		}
	}
	target := make([]zonenpc.Snapshot, 0, len(eligible))
	if primaryIndex >= 0 {
		target = append(target, eligible[primaryIndex])
	}
	for index, enemy := range eligible {
		if index == primaryIndex ||
			positionDistance(sourcePosition, enemy.Plan.Position) > additionalRange {
			continue
		}
		target = append(target, enemy)
	}
	return AreaPlan{
		SourceObjectID: sourceObjectID, AbilityID: util.HashID(projected.Name),
		Definition: projected, Damage: damage, Center: sourcePosition, Target: target,
	}, nil
}

func PlanZetawattBeam(
	enemies *zonenpc.Session, sourceObjectID uint32, sourcePosition game.Vec3,
	targetPosition game.Vec3, creature game.GameplayCreature, definition sim.AbilityDefinition,
) (AreaPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 ||
		definition.Name == "" || definition.Range <= 0 || definition.AnimationName == "" ||
		definition.HitDelay < 0 || definition.ReleaseDelay < definition.HitDelay ||
		!isFinitePosition(sourcePosition) ||
		!isFinitePosition(targetPosition) {
		return AreaPlan{}, errors.New("invalid Zetawatt Beam ability")
	}
	directionX := targetPosition.X - sourcePosition.X
	directionY := targetPosition.Y - sourcePosition.Y
	directionZ := targetPosition.Z - sourcePosition.Z
	directionLength := float32(math.Sqrt(float64(
		directionX*directionX + directionY*directionY + directionZ*directionZ,
	)))
	admissionRange := game.AbilityAdmissionRange(
		definition.Range, creature.RangeIncrease, definition.DescriptorMask,
		definition.IsDescriptorFound,
	)
	if directionLength <= 0 || directionLength > admissionRange {
		return AreaPlan{}, errors.New("Zetawatt Beam direction unavailable or out of range")
	}
	directionX /= directionLength
	directionY /= directionLength
	directionZ /= directionLength
	projected, err := ProjectTiming(creature, definition)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("zetawattTiming: %w", err)
	}
	damage, err := ProjectDamage(
		creature, projected, projected.MinimumDamage, projected.MaximumDamage,
	)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("zetawattDamage: %w", err)
	}
	endpoint := game.Vec3{
		X: sourcePosition.X + directionX*projected.Range,
		Y: sourcePosition.Y + directionY*projected.Range,
		Z: sourcePosition.Z + directionZ*projected.Range,
	}
	target := make([]zonenpc.Snapshot, 0)
	for _, enemy := range enemies.LiveSnapshots() {
		if enemy.TargetObjectID != sourceObjectID {
			continue
		}
		deltaX := enemy.Plan.Position.X - sourcePosition.X
		deltaY := enemy.Plan.Position.Y - sourcePosition.Y
		deltaZ := enemy.Plan.Position.Z - sourcePosition.Z
		along := deltaX*directionX + deltaY*directionY + deltaZ*directionZ
		if along < 0 || along > projected.Range {
			continue
		}
		closest := game.Vec3{
			X: sourcePosition.X + directionX*along,
			Y: sourcePosition.Y + directionY*along,
			Z: sourcePosition.Z + directionZ*along,
		}
		halfExtent := enemy.Plan.NPCProfile.FootprintRadius
		if halfExtent <= 0 {
			halfExtent = 0.5
		}
		if positionDistance(closest, enemy.Plan.Position) > halfExtent {
			continue
		}
		target = append(target, enemy)
	}
	return AreaPlan{
		SourceObjectID: sourceObjectID, AbilityID: util.HashID(projected.Name),
		Definition: projected, Damage: damage, Center: endpoint,
		Facing: game.Vec3{X: directionX, Y: directionY, Z: directionZ}, Target: target,
	}, nil
}

func PlanArea(
	enemies *zonenpc.Session, sourceObjectID uint32, sourcePosition game.Vec3,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (AreaPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 {
		return AreaPlan{}, errors.New("invalid area ability identity")
	}
	if definition.Name == "" ||
		definition.Kind != sim.AbilityKindPointBlank &&
			definition.Kind != sim.AbilityKindTimedArea ||
		definition.Radius <= 0 || definition.AnimationName == "" || definition.HitDelay < 0 ||
		definition.ReleaseDelay < definition.HitDelay {
		return AreaPlan{}, errors.New("invalid area ability definition")
	}
	projected, err := ProjectTiming(creature, definition)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("areaTiming: %w", err)
	}
	projected, err = projectAreaRadius(creature, projected)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("areaProjection: %w", err)
	}
	damage, err := ProjectDamage(
		creature, projected, projected.MinimumDamage, projected.MaximumDamage,
	)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("areaDamage: %w", err)
	}
	target := make([]zonenpc.Snapshot, 0)
	for _, enemy := range enemies.LiveSnapshots() {
		footprintRadius := max(float32(0), enemy.Plan.NPCProfile.FootprintRadius)
		if enemy.Faction != zonenpc.FactionNonPlayerAligned ||
			positionDistance(sourcePosition, enemy.Plan.Position) >
				projected.Radius+footprintRadius {
			continue
		}
		target = append(target, enemy)
	}
	return AreaPlan{
		SourceObjectID: sourceObjectID, AbilityID: util.HashID(projected.Name),
		Definition: projected, Damage: damage, Center: sourcePosition, Target: target,
	}, nil
}

func PlanCursorArea(
	enemies *zonenpc.Session, sourceObjectID uint32, targetObjectID uint32,
	sourcePosition game.Vec3, center game.Vec3, creature game.GameplayCreature,
	definition sim.AbilityDefinition,
) (AreaPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 {
		return AreaPlan{}, errors.New("invalid cursor area identity")
	}
	isCursorKind := definition.Kind == sim.AbilityKindCursorArea ||
		definition.Kind == sim.AbilityKindTeleportArea
	if definition.Name == "" || !isCursorKind ||
		!definition.IsAlwaysUseCursorPosition || definition.Range <= 0 || definition.Radius <= 0 ||
		definition.AnimationName == "" || definition.HitEffectName == "" ||
		definition.HitDelay < 0 || definition.ReleaseDelay < definition.HitDelay {
		return AreaPlan{}, errors.New("invalid cursor area definition")
	}
	if !isFinitePosition(sourcePosition) || !isFinitePosition(center) {
		return AreaPlan{}, errors.New("invalid cursor area position")
	}
	admissionRange := game.AbilityAdmissionRange(
		definition.Range, creature.RangeIncrease, definition.DescriptorMask,
		definition.IsDescriptorFound,
	)
	if positionDistance(sourcePosition, center) > admissionRange {
		return AreaPlan{}, errors.New("cursor area center out of range")
	}
	projected, err := ProjectTiming(creature, definition)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("cursorAreaTiming: %w", err)
	}
	projected, err = projectAreaRadius(creature, projected)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("cursorAreaProjection: %w", err)
	}
	damage, err := ProjectDamage(
		creature, projected, projected.MinimumDamage, projected.MaximumDamage,
	)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("cursorAreaDamage: %w", err)
	}
	target := cursorAreaTargets(
		enemies, sourceObjectID, targetObjectID, center, projected.Radius,
	)
	targetDamage := make(map[uint32]game.DamageRange)
	if projected.SecondaryMinimumDamage > 0 && projected.SecondaryMaximumDamage > 0 {
		secondaryDefinition := projected
		secondaryDefinition.DamageCoefficient = projected.SecondaryDamageCoefficient
		secondaryDamage, secondaryErr := ProjectDamage(
			creature, secondaryDefinition, projected.SecondaryMinimumDamage,
			projected.SecondaryMaximumDamage,
		)
		if secondaryErr != nil {
			return AreaPlan{}, fmt.Errorf("cursorSecondaryDamage: %w", secondaryErr)
		}
		for _, npc := range target {
			if npc.Plan.ObjectID != targetObjectID {
				targetDamage[npc.Plan.ObjectID] = secondaryDamage
			}
		}
	}
	return AreaPlan{
		SourceObjectID: sourceObjectID, AbilityID: util.HashID(projected.Name),
		Definition: projected, Damage: damage, Center: center, Target: target,
		TargetDamage: targetDamage,
	}, nil
}

func PlanCone(
	enemies *zonenpc.Session, sourceObjectID uint32, sourcePosition game.Vec3,
	targetPosition game.Vec3, creature game.GameplayCreature,
	definition sim.AbilityDefinition,
) (AreaPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 {
		return AreaPlan{}, errors.New("invalid cone ability identity")
	}
	if definition.Name == "" || definition.Kind != sim.AbilityKindCone ||
		definition.Radius <= 0 || definition.Angle <= 0 || definition.Angle > 360 ||
		definition.AnimationName == "" || definition.HitDelay < 0 ||
		definition.ReleaseDelay < definition.HitDelay {
		return AreaPlan{}, errors.New("invalid cone ability definition")
	}
	if !isFinitePosition(sourcePosition) || !isFinitePosition(targetPosition) {
		return AreaPlan{}, errors.New("invalid cone ability position")
	}
	directionX := targetPosition.X - sourcePosition.X
	directionY := targetPosition.Y - sourcePosition.Y
	directionZ := targetPosition.Z - sourcePosition.Z
	directionLength := float32(math.Sqrt(float64(
		directionX*directionX + directionY*directionY + directionZ*directionZ,
	)))
	if directionLength <= 0 {
		return AreaPlan{}, errors.New("cone direction unavailable")
	}
	directionX /= directionLength
	directionY /= directionLength
	directionZ /= directionLength
	projected, err := ProjectTiming(creature, definition)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("coneTiming: %w", err)
	}
	projected, err = projectAreaRadius(creature, projected)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("coneProjection: %w", err)
	}
	damage, err := ProjectDamage(
		creature, projected, projected.MinimumDamage, projected.MaximumDamage,
	)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("coneDamage: %w", err)
	}
	minimumScale := projected.MinimumDamagePercent
	if minimumScale <= 0 || minimumScale > 1 {
		minimumScale = 1
	}
	minimumDot := float32(math.Cos(float64(projected.Angle) * math.Pi / 360))
	target := make([]zonenpc.Snapshot, 0)
	damageScale := make(map[uint32]float32)
	for _, enemy := range enemies.LiveSnapshots() {
		if enemy.Faction != zonenpc.FactionNonPlayerAligned {
			continue
		}
		deltaX := enemy.Plan.Position.X - sourcePosition.X
		deltaY := enemy.Plan.Position.Y - sourcePosition.Y
		deltaZ := enemy.Plan.Position.Z - sourcePosition.Z
		distance := float32(math.Sqrt(float64(
			deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ,
		)))
		if distance <= 0 || distance > projected.Radius {
			continue
		}
		dot := (deltaX*directionX + deltaY*directionY + deltaZ*directionZ) / distance
		if dot < minimumDot {
			continue
		}
		target = append(target, enemy)
		distanceRatio := distance / projected.Radius
		targetScale := 1 - (1-minimumScale)*distanceRatio
		if projected.IsFixedDamageScale {
			targetScale = minimumScale
		}
		if projected.LargeTargetDamageMultiplier > 0 &&
			(enemy.Plan.IsCaptain || enemy.Plan.IsElite || enemy.Plan.IsBoss) {
			targetScale *= projected.LargeTargetDamageMultiplier
		}
		damageScale[enemy.Plan.ObjectID] = targetScale
	}
	return AreaPlan{
		SourceObjectID: sourceObjectID, AbilityID: util.HashID(projected.Name),
		Definition: projected, Damage: damage, Center: sourcePosition,
		Facing: game.Vec3{X: directionX, Y: directionY, Z: directionZ},
		Target: target, DamageScale: damageScale,
	}, nil
}

func cursorAreaTargets(
	enemies *zonenpc.Session, sourceObjectID uint32, targetObjectID uint32,
	center game.Vec3, radius float32,
) []zonenpc.Snapshot {
	target := make([]zonenpc.Snapshot, 0)
	if targetObjectID != 0 {
		enemy, isFound := enemies.NPC(targetObjectID)
		if isFound && !enemy.IsDefeated && enemy.HitPoint > 0 &&
			enemy.Faction == zonenpc.FactionNonPlayerAligned &&
			positionDistance(center, enemy.Plan.Position) <= radius {
			target = append(target, enemy)
		}
	}
	for _, enemy := range enemies.LiveSnapshots() {
		if enemy.Plan.ObjectID == targetObjectID ||
			enemy.Faction != zonenpc.FactionNonPlayerAligned ||
			positionDistance(center, enemy.Plan.Position) > radius {
			continue
		}
		target = append(target, enemy)
	}
	return target
}

func PlanTargetedAOEPulse(
	enemies *zonenpc.Session, sourceObjectID uint32, center game.Vec3,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	minimum float32, maximum float32, coefficient float32,
) (AreaPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 ||
		definition.Name == "" || definition.Kind != sim.AbilityKindTargetedAOE ||
		definition.Radius <= 0 || !isFinitePosition(center) {
		return AreaPlan{}, errors.New("invalid targeted AOE pulse")
	}
	projected := definition
	projected.DamageCoefficient = coefficient
	damage, err := ProjectDamage(
		creature, projected, minimum, maximum,
	)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("targetedAOEDamage: %w", err)
	}
	return AreaPlan{
		SourceObjectID: sourceObjectID, AbilityID: util.HashID(projected.Name),
		Definition: projected, Damage: damage, Center: center,
		Target: cursorAreaTargets(enemies, sourceObjectID, 0, center, projected.Radius),
	}, nil
}

func PlanPointBlankBasic(
	enemies *zonenpc.Session, sourceObjectID uint32,
	sourcePosition game.Vec3, creature game.GameplayCreature, definition sim.AbilityDefinition,
) (AreaPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 {
		return AreaPlan{}, errors.New("invalid point blank identity")
	}
	if definition.Name == "" || definition.Kind != sim.AbilityKindPointBlank ||
		definition.IsTargeted || definition.Range <= 0 || definition.Radius <= 0 ||
		definition.BonusDamageMultiplier <= 0 || definition.AnimationName == "" ||
		definition.ImpactEffectName == "" || definition.HitDelay < 0 ||
		definition.ReleaseDelay < definition.HitDelay {
		return AreaPlan{}, errors.New("invalid point blank definition")
	}
	if !isFinitePosition(sourcePosition) {
		return AreaPlan{}, errors.New("invalid point blank position")
	}
	return ResolvePointBlankBasicHit(
		enemies, sourceObjectID, sourcePosition, creature, definition,
	)
}

func ResolvePointBlankBasicHit(
	enemies *zonenpc.Session, sourceObjectID uint32, sourcePosition game.Vec3,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (AreaPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 ||
		definition.Kind != sim.AbilityKindPointBlank || definition.Radius <= 0 ||
		definition.BonusDamageMultiplier <= 0 {
		return AreaPlan{}, errors.New("invalid point blank hit")
	}
	projected, err := ProjectTiming(creature, definition)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("pointBlankTiming: %w", err)
	}
	projected, err = projectAreaRadius(creature, projected)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("pointBlankProjection: %w", err)
	}
	target := make([]zonenpc.Snapshot, 0)
	for _, enemy := range enemies.LiveSnapshots() {
		if enemy.Faction != zonenpc.FactionNonPlayerAligned ||
			positionDistance(sourcePosition, enemy.Plan.Position) > projected.Radius {
			continue
		}
		target = append(target, enemy)
	}
	if len(target) == 0 {
		return AreaPlan{
			SourceObjectID: sourceObjectID, AbilityID: util.HashID(projected.Name),
			Definition: projected, Center: sourcePosition,
		}, nil
	}
	targetDivisor := float32(len(target))
	minimum := projected.MinimumDamage*projected.BonusDamageMultiplier +
		projected.MinimumDamage/targetDivisor
	maximum := projected.MaximumDamage*projected.BonusDamageMultiplier +
		projected.MaximumDamage/targetDivisor
	projected.DamageCoefficient /= targetDivisor
	damage, err := ProjectDamage(
		creature, projected, minimum, maximum,
	)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("pointBlankDamage: %w", err)
	}
	return AreaPlan{
		SourceObjectID: sourceObjectID, AbilityID: util.HashID(projected.Name),
		Definition: projected, Damage: damage, Center: sourcePosition, Target: target,
	}, nil
}

func CommitArea(
	random *sim.SimulatorRandom, enemies *zonenpc.Session, plan AreaPlan,
	creature game.GameplayCreature, difficulty uint32, tuning sim.CriticalTuning,
) ([]AreaResult, error) {
	if random == nil || enemies == nil || plan.SourceObjectID == 0 || plan.AbilityID == 0 {
		return nil, errors.New("invalid area ability commit")
	}
	if plan.IsSingleTarget && len(plan.Target) > 1 {
		return nil, errors.New("single target hit has multiple targets")
	}
	results := make([]AreaResult, 0, len(plan.Target))
	for _, target := range plan.Target {
		live, isFound := enemies.NPC(target.Plan.ObjectID)
		if !isFound || live.IsDefeated || !live.IsPublished ||
			live.Faction != zonenpc.FactionNonPlayerAligned {
			continue
		}
		damageScale := float32(1)
		targetDamage := plan.Damage
		if projectedDamage, isProjected := plan.TargetDamage[target.Plan.ObjectID]; isProjected {
			targetDamage = projectedDamage
		}
		if scaledDamage, isScaled := plan.DamageScale[target.Plan.ObjectID]; isScaled {
			damageScale = scaledDamage
		}
		minimumDamage := float32(math.Floor(
			float64(targetDamage.Minimum * damageScale),
		))
		maximumDamage := float32(math.Ceil(
			float64(targetDamage.Maximum * damageScale),
		))
		selectedDamage, err := sim.SelectRankDamage(random, sim.DamageRange{
			Minimum: minimumDamage,
			Maximum: maximumDamage,
		})
		if err != nil {
			return nil, fmt.Errorf("areaSelect[%d]: %w", target.Plan.ObjectID, err)
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
			return nil, fmt.Errorf("areaCritical[%d]: %w", target.Plan.ObjectID, err)
		}
		damage, err := enemies.Hit(zonenpc.HitRequest{
			SourceObjectID: plan.SourceObjectID, TargetObjectID: target.Plan.ObjectID, Damage: critical.Damage,
			IsArea: !plan.IsSingleTarget, SourcePosition: &plan.Center, Metadata: NPCDamageMetadata(plan.Definition),
		})
		if err != nil {
			return nil, fmt.Errorf("areaApply[%d]: %w", target.Plan.ObjectID, err)
		}
		results = append(results, AreaResult{
			Snapshot: live, Damage: damage, IsCritical: critical.IsCritical,
			Definition: plan.Definition,
		})
	}
	return results, nil
}

func PlanTrapArea(
	enemies *zonenpc.Session, sourceObjectID uint32, center game.Vec3,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (AreaPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 ||
		definition.Name == "" || definition.Kind != sim.AbilityKindTrap ||
		definition.Radius <= 0 || !isFinitePosition(center) {
		return AreaPlan{}, errors.New("invalid trap area")
	}
	damage, err := ProjectDamage(
		creature, definition, definition.MinimumDamage, definition.MaximumDamage,
	)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("trapDamage: %w", err)
	}
	target := make([]zonenpc.Snapshot, 0)
	for _, npc := range enemies.LiveSnapshots() {
		if npc.Faction != zonenpc.FactionNonPlayerAligned ||
			positionDistance(center, npc.Plan.Position) > definition.Radius {
			continue
		}
		target = append(target, npc)
	}
	return AreaPlan{
		SourceObjectID: sourceObjectID, AbilityID: util.HashID(definition.Name),
		Definition: definition, Damage: damage, Center: center, Target: target,
	}, nil
}

func PlanFixedArea(
	enemies *zonenpc.Session, sourceObjectID uint32, center game.Vec3,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
) (AreaPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 ||
		definition.Name == "" || definition.Kind != sim.AbilityKindAuraArea ||
		definition.Radius <= 0 || !isFinitePosition(center) {
		return AreaPlan{}, errors.New("invalid fixed area")
	}
	damage, err := ProjectDamage(
		creature, definition, definition.MinimumDamage, definition.MaximumDamage,
	)
	if err != nil {
		return AreaPlan{}, fmt.Errorf("fixedAreaDamage: %w", err)
	}
	target := make([]zonenpc.Snapshot, 0)
	for _, npc := range enemies.LiveSnapshots() {
		if npc.Faction != zonenpc.FactionNonPlayerAligned ||
			positionDistance(center, npc.Plan.Position) > definition.Radius {
			continue
		}
		target = append(target, npc)
	}
	return AreaPlan{
		SourceObjectID: sourceObjectID, AbilityID: util.HashID(definition.Name),
		Definition: definition, Damage: damage, Center: center, Target: target,
	}, nil
}
