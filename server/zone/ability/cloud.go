package ability

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// CloudPoisonPlan contains the zone-owned state needed to resolve one poison
// cloud. Projectile and effect presentation remain transport concerns.
type CloudPoisonPlan struct {
	SourceObjectID uint32
	AbilityID      uint32
	Definition     sim.AbilityDefinition
	Destination    sim.Position
	Damage         game.DamageRange
}

type CloudLobPlan struct {
	SourceObjectID uint32
	TargetObjectID uint32
	AbilityID      uint32
	Definition     sim.AbilityDefinition
	Destination    sim.Position
	LaunchPosition [2]sim.Position
	Lob            [2]sim.CrystalLob
	Damage         game.DamageRange
}

func (e CloudLobPlan) PoisonPlan() CloudPoisonPlan {
	return CloudPoisonPlan{
		SourceObjectID: e.SourceObjectID,
		AbilityID:      e.AbilityID,
		Definition:     e.Definition,
		Destination:    e.Destination,
		Damage:         e.Damage,
	}
}

func PlanCloudLob(
	enemies *zonenpc.Session, sourceObjectID uint32, targetObjectID uint32,
	sourcePosition game.Vec3, creature game.GameplayCreature,
	definition sim.AbilityDefinition, simulationTime time.Duration,
) (CloudLobPlan, error) {
	if enemies == nil || sourceObjectID == 0 || creature.Noun == 0 ||
		simulationTime < 0 || !isFinitePosition(sourcePosition) {
		return CloudLobPlan{}, errors.New("invalid cloud lob identity")
	}
	cloud := definition.CloudLob
	if definition.Name != "Sprout" ||
		definition.Kind != sim.AbilityKindCloudLob ||
		definition.Range <= 0 || cloud.ProjectileNoun == "" ||
		cloud.CloudNoun == "" || cloud.ProjectileEffectName == "" ||
		cloud.ImpactEffectName == "" || cloud.CloudEffectName == "" ||
		cloud.CloudHitEffectName == "" || cloud.CloudRadius <= 0 ||
		cloud.CloudDuration <= 0 || cloud.TickDuration <= 0 ||
		cloud.TickCount == 0 {
		return CloudLobPlan{}, errors.New("invalid cloud lob definition")
	}
	admissionRange := game.AbilityAdmissionRange(
		definition.Range, creature.RangeIncrease, definition.DescriptorMask,
		definition.IsDescriptorFound,
	)
	if targetObjectID == 0 {
		targetObjectID = NearestTarget(
			enemies, sourceObjectID, sourcePosition, admissionRange,
		)
	}
	target, isFound := enemies.NPC(targetObjectID)
	if !isFound || target.IsDefeated || target.HitPoint <= 0 ||
		target.TargetObjectID != sourceObjectID {
		return CloudLobPlan{},
			fmt.Errorf("targetUnavailable: %d", targetObjectID)
	}
	distance := Distance(sourcePosition, target.Plan.Position)
	if distance > admissionRange {
		return CloudLobPlan{},
			fmt.Errorf("targetRange: got %g, want <= %g", distance, admissionRange)
	}
	projected, err := ProjectTiming(creature, definition)
	if err != nil {
		return CloudLobPlan{}, fmt.Errorf("cloudLobTiming: %w", err)
	}
	if projected.IsAreaRadiusScaled {
		projected.CloudLob.CloudRadius, err = ProjectAreaRadius(
			creature, projected.CloudLob.CloudRadius,
		)
		if err != nil {
			return CloudLobPlan{}, fmt.Errorf("cloudLobRadius: %w", err)
		}
	}
	cast, err := sim.ResolveCloudLobCast(projected, distance)
	if err != nil {
		return CloudLobPlan{}, fmt.Errorf("cloudLobCast: %w", err)
	}
	minimumDamage := projected.MinimumDamage
	maximumDamage := projected.MaximumDamage
	if minimumDamage == 0 && maximumDamage == 0 {
		minimumDamage = creature.MinimumWeaponDamage
		maximumDamage = creature.MaximumWeaponDamage
	}
	damageDefinition := projected
	damageDefinition.DamageType = projected.CloudLob.PoisonDamageType
	damageDefinition.DamageSource = projected.CloudLob.PoisonDamageSource
	damageDefinition.IsDamageTypeFound = true
	damageDefinition.IsDamageSourceFound = true
	damage, err := ProjectDamage(
		creature, damageDefinition, minimumDamage, maximumDamage,
	)
	if err != nil {
		return CloudLobPlan{}, fmt.Errorf("cloudLobDamage: %w", err)
	}
	if damage.Minimum <= 0 || damage.Maximum < damage.Minimum {
		return CloudLobPlan{}, errors.New("invalid cloud lob damage")
	}
	destination := sim.Position{
		X: target.Plan.Position.X,
		Y: target.Plan.Position.Y,
		Z: target.Plan.Position.Z,
	}
	offset := [2]sim.Position{
		projected.CloudLob.LeftOffset,
		projected.CloudLob.RightOffset,
	}
	var launchPosition [2]sim.Position
	var lob [2]sim.CrystalLob
	for index := range offset {
		launchPosition[index], err = TossLaunchPosition(
			sourcePosition, target.Plan.Position, offset[index],
		)
		if err != nil {
			return CloudLobPlan{},
				fmt.Errorf("cloudLobLaunch[%d]: %w", index, err)
		}
		lob[index], err = sim.BuildTossLob(
			simulationTime+projected.HitDelay,
			launchPosition[index], destination,
			cast.Height, cast.Duration, 0, 0, 0, true, false,
		)
		if err != nil {
			return CloudLobPlan{},
				fmt.Errorf("cloudLobTrajectory[%d]: %w", index, err)
		}
	}
	return CloudLobPlan{
		SourceObjectID: sourceObjectID,
		TargetObjectID: targetObjectID,
		AbilityID:      util.HashID(projected.Name),
		Definition:     projected,
		Destination:    destination,
		LaunchPosition: launchPosition,
		Lob:            lob,
		Damage:         damage,
	}, nil
}

func ResolveCloudLobLanding(
	enemies *zonenpc.Session, plan CloudLobPlan,
) (AreaPlan, error) {
	if enemies == nil || plan.SourceObjectID == 0 || plan.AbilityID == 0 ||
		plan.Definition.Kind != sim.AbilityKindCloudLob ||
		plan.Definition.CloudLob.CloudRadius <= 0 ||
		IsInvalidNumber(plan.Damage.Minimum) || plan.Damage.Minimum <= 0 ||
		IsInvalidNumber(plan.Damage.Maximum) ||
		plan.Damage.Maximum < plan.Damage.Minimum {
		return AreaPlan{}, errors.New("invalid cloud lob landing")
	}
	target := make([]zonenpc.Snapshot, 0)
	center := game.Vec3{
		X: plan.Destination.X,
		Y: plan.Destination.Y,
		Z: plan.Destination.Z,
	}
	for _, enemy := range enemies.LiveSnapshots() {
		if enemy.TargetObjectID != plan.SourceObjectID ||
			enemy.IsDefeated || enemy.HitPoint <= 0 ||
			Distance(center, enemy.Plan.Position) >
				plan.Definition.CloudLob.CloudRadius {
			continue
		}
		target = append(target, enemy)
	}
	return AreaPlan{
		SourceObjectID: plan.SourceObjectID,
		AbilityID:      plan.AbilityID,
		Definition:     plan.Definition,
		Damage:         plan.Damage,
		Center:         center,
		Target:         target,
	}, nil
}

type cloudPoisonState struct {
	tailTicksByObjectID map[uint32]uint32
}

type cloudPoisonRegion struct {
	center game.Vec3
	radius float32
}

// CloudPoisonRuntime owns overlapping cloud regions and their shared tick
// cadence for one zone member.
type CloudPoisonRuntime struct {
	state                 cloudPoisonState
	regionsByRunID        map[uint32]cloudPoisonRegion
	nextTickMilliseconds  uint64
	isNextTickInitialized bool
}

type CloudPoisonTick struct {
	Plan            AreaPlan
	EnteredObjectID []uint32
}

func (e *CloudPoisonRuntime) AddRegion(
	runID uint32, plan CloudPoisonPlan, timestampMilliseconds uint64,
) error {
	if e == nil || runID == 0 ||
		plan.Definition.Kind != sim.AbilityKindCloudLob ||
		plan.Definition.CloudLob.CloudRadius <= 0 ||
		plan.Definition.CloudLob.TickDuration <= 0 {
		return errors.New("invalid cloud poison region")
	}
	if e.regionsByRunID == nil {
		e.regionsByRunID = make(map[uint32]cloudPoisonRegion)
	}
	if _, isDuplicate := e.regionsByRunID[runID]; isDuplicate {
		return fmt.Errorf("cloud poison region duplicate: %d", runID)
	}
	e.regionsByRunID[runID] = cloudPoisonRegion{
		center: game.Vec3{
			X: plan.Destination.X,
			Y: plan.Destination.Y,
			Z: plan.Destination.Z,
		},
		radius: plan.Definition.CloudLob.CloudRadius,
	}
	if e.isNextTickInitialized {
		return nil
	}
	tickMilliseconds := uint64(
		plan.Definition.CloudLob.TickDuration / time.Millisecond,
	)
	if tickMilliseconds == 0 ||
		timestampMilliseconds > ^uint64(0)-tickMilliseconds {
		delete(e.regionsByRunID, runID)
		return errors.New("invalid cloud poison tick time")
	}
	e.nextTickMilliseconds = timestampMilliseconds + tickMilliseconds
	e.isNextTickInitialized = true
	return nil
}

func (e *CloudPoisonRuntime) RemoveRegion(runID uint32) {
	if e == nil || e.regionsByRunID == nil {
		return
	}
	delete(e.regionsByRunID, runID)
}

func (e *CloudPoisonRuntime) ResolveLanding(
	enemies *zonenpc.Session, plan CloudPoisonPlan,
) (CloudPoisonTick, error) {
	return e.resolve(enemies, plan, true)
}

func (e *CloudPoisonRuntime) ResolveTick(
	enemies *zonenpc.Session, plan CloudPoisonPlan,
	timestampMilliseconds uint64,
) (CloudPoisonTick, bool, error) {
	if e == nil || !e.isNextTickInitialized ||
		timestampMilliseconds < e.nextTickMilliseconds {
		return CloudPoisonTick{}, false, nil
	}
	tickMilliseconds := uint64(
		plan.Definition.CloudLob.TickDuration / time.Millisecond,
	)
	if tickMilliseconds == 0 ||
		timestampMilliseconds > ^uint64(0)-tickMilliseconds {
		return CloudPoisonTick{}, false,
			errors.New("invalid shared cloud poison tick time")
	}
	e.nextTickMilliseconds = timestampMilliseconds + tickMilliseconds
	tick, err := e.resolve(enemies, plan, false)
	if err != nil {
		return CloudPoisonTick{}, false,
			fmt.Errorf("cloudPoisonResolve: %w", err)
	}
	return tick, true, nil
}

func (e *CloudPoisonRuntime) resolve(
	enemies *zonenpc.Session, plan CloudPoisonPlan, isLanding bool,
) (CloudPoisonTick, error) {
	if e == nil || enemies == nil ||
		plan.Definition.Kind != sim.AbilityKindCloudLob ||
		plan.Definition.CloudLob.TickCount == 0 {
		return CloudPoisonTick{}, errors.New("invalid shared cloud poison tick")
	}
	if e.state.tailTicksByObjectID == nil {
		e.state.tailTicksByObjectID = make(map[uint32]uint32)
	}
	target := make([]zonenpc.Snapshot, 0)
	enteredObjectID := make([]uint32, 0)
	for _, enemy := range enemies.LiveSnapshots() {
		if enemy.TargetObjectID != plan.SourceObjectID ||
			enemy.IsDefeated || enemy.HitPoint <= 0 {
			delete(e.state.tailTicksByObjectID, enemy.Plan.ObjectID)
			continue
		}
		_, isTracked := e.state.tailTicksByObjectID[enemy.Plan.ObjectID]
		isInside := false
		for _, region := range e.regionsByRunID {
			if Distance(region.center, enemy.Plan.Position) <= region.radius {
				isInside = true
				break
			}
		}
		if isLanding {
			if !isInside || isTracked {
				continue
			}
			e.state.tailTicksByObjectID[enemy.Plan.ObjectID] = 0
			enteredObjectID = append(enteredObjectID, enemy.Plan.ObjectID)
			target = append(target, enemy)
			continue
		}
		if isInside {
			if !isTracked {
				enteredObjectID = append(
					enteredObjectID, enemy.Plan.ObjectID,
				)
			}
			e.state.tailTicksByObjectID[enemy.Plan.ObjectID] = 0
			target = append(target, enemy)
			continue
		}
		if !isTracked {
			continue
		}
		tailTick := e.state.tailTicksByObjectID[enemy.Plan.ObjectID]
		if tailTick >= plan.Definition.CloudLob.TickCount {
			delete(e.state.tailTicksByObjectID, enemy.Plan.ObjectID)
			continue
		}
		target = append(target, enemy)
		tailTick++
		if tailTick >= plan.Definition.CloudLob.TickCount {
			delete(e.state.tailTicksByObjectID, enemy.Plan.ObjectID)
		} else {
			e.state.tailTicksByObjectID[enemy.Plan.ObjectID] = tailTick
		}
	}
	return CloudPoisonTick{
		Plan: AreaPlan{
			SourceObjectID: plan.SourceObjectID,
			AbilityID:      plan.AbilityID,
			Definition:     plan.Definition,
			Damage:         plan.Damage,
			Center: game.Vec3{
				X: plan.Destination.X,
				Y: plan.Destination.Y,
				Z: plan.Destination.Z,
			},
			Target: target,
		},
		EnteredObjectID: enteredObjectID,
	}, nil
}
