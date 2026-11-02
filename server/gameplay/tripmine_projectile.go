package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zone "github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type tripMineLaunch struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	source     zonenpc.Snapshot
	target     zone.NPCTarget
	damage     game.DamageRange
	timestamp  uint64
}

func tripMineMissileProfile(rank uint32) (zonenpc.ActionProfile, error) {
	profile := zonenpc.ActionProfile{Family: zonenpc.ActionProjectile,
		AbilityName: "TNXTripMineMissle", ProjectileNoun: "Ability_HomingProjectile.Noun",
		ProjectileSpeed: 75, ProjectileDistance: 50, HitDelay: 300 * time.Millisecond,
		ReleaseDelay: 450 * time.Millisecond,
		DamageType:   5, DamageSource: 1, DescriptorMask: 8320, IsDamageProfileKnown: true,
		TrailEffectName:  "cyber_missile_homing_projectile.ServerEventDef",
		ImpactEffectName: "cyber_missile_homing_hit.ServerEventDef",
		MissEffectName:   "ineffective_common_small.ServerEventDef",
	}
	switch rank {
	case 1:
		profile.MinimumDamage, profile.MaximumDamage = 20, 40
	case 2:
		profile.MinimumDamage, profile.MaximumDamage = 5, 7
	case 3:
		profile.MinimumDamage, profile.MaximumDamage = 10, 17
	default:
		return zonenpc.ActionProfile{}, errors.New("trip mine rank unavailable")
	}
	return profile, nil
}

func (e tripMineLaunch) commit() {
	err := scheduleNPCProducer(e.runtime.registry, e.packet, 300*time.Millisecond, e.produce)
	if err != nil {
		e.runtime.logger.Printf("RakNet Trip Mine missile omitted source=%d target=%d: %v", e.source.Plan.ObjectID, e.target.ObjectID, err)
	}
}

func (e tripMineLaunch) produce() ([][]byte, error) {
	r := e.runtime
	r.registry.mutex.Lock()
	schedule, immediatePackets, isAccepted, err := e.prepareLocked()
	if err == nil && isAccepted {
		// Zero-delay flight polling must not overtake its projectile birth.
		// The retained publication queues the birth before admitting drivers.
		r.registry.queueCampaignProjectilePublicationLocked(schedule.retirement, e.sessionKey, immediatePackets)
		r.registry.admitCampaignProjectilePublicationLocked(schedule.retirement, e.sessionKey)
	}
	r.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("minePrepare: %w", err)
	}
	if !isAccepted {
		return nil, nil
	}
	producers := make([]raknet.ScheduledPacketProducer, 0)
	for _, deadline := range schedule.run.Deadlines() {
		producers = append(producers, schedule.producer(deadline))
	}
	cancel, err := scheduleNPCProducers(r.registry, e.packet, producers)
	if err != nil || cancel == nil {
		r.projectile.retire(e.sessionKey, e.generation, schedule.projectileObjectID, schedule.run, schedule.retirement)
		if err != nil {
			return nil, fmt.Errorf("mineSchedule: %w", err)
		}
		return nil, errors.New("trip mine scheduler cancellation unavailable")
	}
	return nil, nil
}

func (e tripMineLaunch) prepareLocked() (campaignNPCProjectileSchedule, [][]byte, bool, error) {
	r := e.runtime
	member, isFound := r.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone == nil || member.isZoneTerminal() {
		return campaignNPCProjectileSchedule{}, nil, false, nil
	}
	source, isFound := member.zone.NPCs().NPC(e.source.Plan.ObjectID)
	if !isFound || source.IsDefeated || source.HitPoint <= 0 || !source.IsPublished {
		return campaignNPCProjectileSchedule{}, nil, false, nil
	}
	target, isFound := member.zone.NPCTarget(e.target.ObjectID)
	if !isFound {
		// The template checks object validity, not IsAlive. Keep firing the
		// three accepted requests at a dead body; deleted targets use the
		// explicit RequestAbility fallback coordinates from chunk112.
		target = e.target
		target.HitPoint = 0
		hero, isHeroFound := member.zone.Hero().SnapshotByObjectID(target.ObjectID)
		companion, isCompanionFound := member.zone.Companion().Snapshot(target.ObjectID)
		switch {
		case isHeroFound:
			target.Position = hero.Position
		case isCompanionFound:
			target.Position = companion.Position
		default:
			target.Position = e.source.Plan.Position.Add(e.source.Facing)
		}
	}
	now := r.now()
	targets, err := r.registry.sampleNPCTargetsLocked(member.zone, []zone.NPCTarget{target}, now)
	if err != nil {
		return campaignNPCProjectileSchedule{}, nil, false, fmt.Errorf("mineAimPose: %w", err)
	}
	target = targets[0]
	profile, err := tripMineMissileProfile(uint32(e.source.Plan.NPCProfile.NPCRank))
	if err != nil {
		return campaignNPCProjectileSchedule{}, nil, false, fmt.Errorf("mineProfile: %w", err)
	}
	plan := zonenpc.AttackPlan{SourceObjectID: e.source.Plan.ObjectID, TargetObjectID: target.ObjectID,
		SourcePosition: e.source.Plan.Position, TargetPosition: target.Position, Profile: profile, Damage: e.damage}
	result, err := zonenpc.CommitAttack(member.zone.NPCRandom(), plan, e.source.EffectiveCriticalRating(), r.program.Critical)
	if err != nil {
		return campaignNPCProjectileSchedule{}, nil, false, fmt.Errorf("mineDamage: %w", err)
	}
	geometry, err := r.program.ProjectileGeometry(0)
	if err != nil {
		return campaignNPCProjectileSchedule{}, nil, false, fmt.Errorf("mineGeometry: %w", err)
	}
	projectilePhysics, isProjectilePhysicsFound := r.program.NounPhysics[profile.ProjectileNoun]
	if !isProjectilePhysicsFound || projectilePhysics.ProjectileQueryHalfExtent.X <= 0 ||
		projectilePhysics.ProjectileQueryHalfExtent.Y <= 0 || projectilePhysics.ProjectileQueryHalfExtent.Z <= 0 {
		return campaignNPCProjectileSchedule{}, nil, false, errors.New("trip mine projectile physics unavailable")
	}
	geometry.ProjectileHalfExtent = projectilePhysics.ProjectileQueryHalfExtent
	// Template38 refreshes the source center and target position AFTER its
	// .3s wait. RequestAbility's outer coordinates are only fallback aim.
	physics, isPhysicsFound := r.program.NounPhysics[source.Plan.NounName]
	if !isPhysicsFound {
		return campaignNPCProjectileSchedule{}, nil, false, errors.New("trip mine source physics unavailable")
	}
	position := source.Plan.Position.Add(game.Vec3(physics.BoundMinimum).Add(game.Vec3(physics.BoundMaximum)).Scale(0.5))
	position = position.Add(source.Facing.Scale(source.Plan.NPCProfile.FootprintRadius))
	aim := target.Position
	direction := aim.Sub(position)
	length := direction.Length()
	if length <= 0 {
		return campaignNPCProjectileSchedule{}, nil, false, errors.New("trip mine direction unavailable")
	}
	direction = direction.Scale(1 / length)
	ability := sim.AbilityDefinition{Name: profile.AbilityName, Kind: sim.AbilityKindProjectile,
		ProjectileNoun: profile.ProjectileNoun, TrailEffectName: profile.TrailEffectName,
		ImpactEffectName: profile.ImpactEffectName, MissEffectName: profile.MissEffectName,
		Speed: 75, Distance: 50, ReleaseDelay: profile.ReleaseDelay - profile.HitDelay}
	flight, err := newCampaignNPCProjectileFlight(profile, ability, position, sim.Position(direction), geometry.ProjectileHalfExtent, now)
	if err != nil {
		return campaignNPCProjectileSchedule{}, nil, false, fmt.Errorf("mineFlight: %w", err)
	}
	attack, isAccepted, err := member.zone.BeginTripMineProjectile(zonenpc.ActionOwner{
		UserID: member.binding.UserID, PeerGeneration: e.generation,
	}, source.Plan.ObjectID, now)
	if err != nil {
		return campaignNPCProjectileSchedule{}, nil, false, fmt.Errorf("mineAuthorize: %w", err)
	}
	if !isAccepted {
		return campaignNPCProjectileSchedule{}, nil, false, nil
	}
	flight.attack = attack
	objectID, err := member.reserveCampaignProjectileIDs(1, 1000)
	if err != nil {
		return campaignNPCProjectileSchedule{}, nil, false, fmt.Errorf("mineObjectID: %w", err)
	}
	run, packets, err := abilityraknet.NewProjectileRun(abilityraknet.ProjectileInput{
		StartedAt: now, Ability: ability, ActorObjectID: source.Plan.ObjectID, TargetObjectID: target.ObjectID,
		ProjectileObjectID: objectID, ActorPosition: sim.Position(position), TargetPosition: sim.Position(aim),
		ActorFacing: sim.Position(direction), ImpactPosition: sim.Position(aim),
		CollisionDelay: flight.collision.Duration(), IsDirectHit: true, IsCollisionExternallyDriven: true,
		IsCollisionSampled: true, Damage: result.Damage, IsCritical: result.IsCritical,
		TargetHitPoint: target.HitPoint, TargetManaPoint: target.ManaPoint, SourceTime: e.timestamp + 300,
		IsActivationSuppressed: true, IsTurnSuppressed: true, IsCooldownSuppressed: true, IsReleaseSuppressed: true,
	})
	if err != nil {
		return campaignNPCProjectileSchedule{}, nil, false, fmt.Errorf("mineRun: %w", err)
	}
	err = member.trackCampaignNPCProjectile(objectID, run)
	if err != nil {
		run.Stop()
		return campaignNPCProjectileSchedule{}, nil, false, fmt.Errorf("mineTrack: %w", err)
	}
	r.registry.sessions[e.sessionKey] = member
	if r.logger != nil {
		r.logger.Printf("RakNet Trip Mine missile launched source=%d target=%d projectile=%d rank=%d position=%v direction=%v speed=75 distance=50", source.Plan.ObjectID, target.ObjectID, objectID, source.Plan.NPCProfile.NPCRank, position, direction)
	}
	schedule := campaignNPCProjectileSchedule{runtime: r, packet: e.packet, sessionKey: e.sessionKey,
		generation: e.generation, sourceObjectID: source.Plan.ObjectID, projectileObjectID: objectID,
		timestamp: e.timestamp + 300, source: e.source, target: target, plan: plan, result: result, geometry: geometry,
		facing: raknet.Vector3(direction), binding: member.binding, run: run, flight: flight,
		impactDeadline: ability.HitDelay + flight.collision.Duration(), lastDeadline: run.LastDeadline(),
		retirement: &campaignProjectileRetirement{originalZone: member.zone, generation: e.generation, objectID: objectID, run: run}}
	return schedule, packets, true, nil
}
