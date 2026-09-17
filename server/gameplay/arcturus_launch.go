package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignArcturusLaunch struct {
	runtime        campaignNPCActionRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	objectID       uint32
	timestamp      uint64
	state          *campaignArcturusState
	protectedUntil time.Time
}

type campaignArcturusLaunchStep struct {
	run   *campaignArcturusLaunch
	phase int
}

func (e campaignNPCActionRuntime) produceArcturusLaunch(packet raknet.Packet, sessionKey string,
	generation uint64, objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	current, isFound := e.registry.sessions[sessionKey]
	if !isFound || !current.isCampaignNPCSourceActive(generation, objectID) {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	state := current.campaignArcturusStates[objectID]
	if state == nil {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	if state.launch != nil {
		e.registry.mutex.Unlock()
		return nil, true, nil
	}
	if timestamp < state.nextLaunch {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	boss, isBossFound := current.zone.NPCs().NPC(objectID)
	if !isBossFound || boss.IsDefeated || current.zone.NPCs().StunRemaining(objectID, e.now()) > 0 {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	if packet.ScheduleFunc == nil || (packet.ScheduleGroupResult == nil && packet.ScheduleGroup == nil) {
		e.registry.mutex.Unlock()
		return nil, true, errors.New("arcturus launch scheduler unavailable")
	}
	run := &campaignArcturusLaunch{runtime: e, packet: packet.Autonomous(),
		sessionKey: sessionKey, generation: generation, objectID: objectID,
		timestamp: timestamp, state: state, protectedUntil: e.now().Add(9200 * time.Millisecond)}
	state.launch = run
	if state.turret != nil {
		turret, isTurretFound := current.zone.NPCs().NPC(state.turret.objectID)
		if !isTurretFound || turret.IsDefeated {
			// TurretCheck recreates a destroyed turret at a later launch.
			state.turret = nil
		}
	}
	e.registry.mutex.Unlock()
	animation, err := npcraknet.AnimationState(objectID, "ctd_boss_tc_attack1_launch", timestamp)
	if err != nil {
		run.abort()
		return nil, true, fmt.Errorf("arcturusLaunchAnimation: %w", err)
	}
	startPackets, err := npcraknet.MovementStop(objectID, boss.Plan.Position)
	if err != nil {
		run.abort()
		return nil, true, fmt.Errorf("arcturusLaunchStop: %w", err)
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, 17)
	for phase := 0; phase < 17; phase++ {
		delay := time.Second
		switch {
		case phase == 1:
			delay = 2 * time.Second
		case phase >= 2 && phase <= 13:
			delay = 1500*time.Millisecond + time.Duration(phase-2)*500*time.Millisecond
		case phase == 14:
			delay = 9000 * time.Millisecond
		case phase == 15:
			delay = 9200 * time.Millisecond
		case phase == 16:
			delay = 10280 * time.Millisecond
		}
		step := campaignArcturusLaunchStep{run: run, phase: phase}
		producers = append(producers, raknet.ScheduledPacketProducer{Delay: delay, Produce: step.produce})
	}
	sortScheduledPacketProducersByDelay(producers)
	cancel, err := run.packet.ScheduleProducers(producers)
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		run.abort()
		return nil, true, fmt.Errorf("arcturusLaunchSchedule: %w", err)
	}
	return append(startPackets, animation), true, nil
}

func (e *campaignArcturusLaunch) abort() {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	current, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && current.generation == e.generation && current.campaignArcturusStates[e.objectID] == e.state && e.state.launch == e {
		e.state.launch = nil
		current.zone.NPCs().ClearIntangible(e.objectID, e.protectedUntil)
	}
}

func (e campaignArcturusLaunchStep) produce() ([][]byte, error) {
	run := e.run
	run.runtime.registry.mutex.Lock()
	current, isFound := run.runtime.registry.sessions[run.sessionKey]
	if !isFound || current.generation != run.generation || current.isZoneTerminal() ||
		current.campaignArcturusStates[run.objectID] != run.state || run.state.launch != run {
		run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	boss, isBossFound := current.zone.NPCs().NPC(run.objectID)
	if !isBossFound {
		run.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	isAlive := !boss.IsDefeated && boss.HitPoint > 0
	packets := make([][]byte, 0)
	var err error
	switch {
	case e.phase == 0 && isAlive:
		err = current.zone.NPCs().ApplyIntangible(run.objectID, run.protectedUntil)
	case e.phase == 1 && isAlive:
		var packet []byte
		packet, err = raknet.MarshalApplication(raknet.ObjectUpdateMessage{ObjectID: run.objectID,
			IsVisible: false, PositionX: boss.Plan.Position.X, PositionY: boss.Plan.Position.Y, PositionZ: boss.Plan.Position.Z})
		packets = append(packets, packet)
	case e.phase >= 2 && e.phase <= 13 && isAlive:
		packets, err = run.dropMissile(&current, boss, run.timestamp+1500+uint64(e.phase-2)*500)
	case e.phase == 14:
		position := boss.Plan.Position
		if isAlive {
			position = run.state.center
			err = current.zone.NPCs().SetPosition(run.objectID, position)
		}
		if err == nil {
			var packet []byte
			packet, err = raknet.MarshalApplication(raknet.ObjectUpdateMessage{ObjectID: run.objectID,
				IsVisible: true, PositionX: position.X, PositionY: position.Y, PositionZ: position.Z})
			packets = append(packets, packet)
		}
		if err == nil && isAlive {
			var packet []byte
			packet, err = npcraknet.RestorePose(run.objectID, position, boss.Facing)
			packets = append(packets, packet)
			if err == nil {
				packet, err = npcraknet.AnimationState(run.objectID, "ctd_boss_tc_attack1_land", run.timestamp+9000)
				packets = append(packets, packet)
			}
		}
	case e.phase == 15 && isAlive:
		profile := zonenpc.ActionProfile{AbilityName: "CitadelBossLaunch", Family: zonenpc.ActionCone,
			MinimumDamage: 20, MaximumDamage: 30, Radius: 1 + boss.Plan.NPCProfile.FootprintRadius,
			// Native GUID 0x0139de24 has no recovered displacement contract.
			// Keep the fallback short and navigation-clipped; see notes/help.md.
			ForcedMovementDistance: 2, ForcedMovementSpeed: 8,
			DescriptorMask: 64, DamageSource: 0, DamageType: 0, IsDamageProfileKnown: true}
		packets, err = run.areaDamage(&current, boss, boss.Plan.Position, profile, run.timestamp+9200)
		current.zone.NPCs().ClearIntangible(run.objectID, run.protectedUntil)
	case e.phase == 16:
		run.state.launch = nil
		run.state.nextLaunch = run.timestamp + 30000
	}
	run.runtime.registry.sessions[run.sessionKey] = current
	run.runtime.registry.mutex.Unlock()
	if err != nil {
		// Keep the already-scheduled reveal and combat-resume steps alive even
		// if one missile or presentation update cannot be produced.
		run.runtime.logger.Printf("Arcturus launch phase omitted source=%d phase=%d: %v", run.objectID, e.phase, err)
		return packets, nil
	}
	if e.phase == 16 && isAlive {
		return run.runtime.produceEnemyCone(run.packet, run.sessionKey, run.generation, run.objectID, run.timestamp+10280)
	}
	return packets, nil
}

type campaignArcturusMissile struct {
	launch             *campaignArcturusLaunch
	zone               *zone.Zone
	startedAt          time.Time
	flightDelay        time.Duration
	direction          sim.Position
	projectile         *abilityraknet.ProjectileRun
	position           game.Vec3
	timestamp          uint64
	shadowObjectID     uint32
	projectileObjectID uint32
}

func (e *campaignArcturusLaunch) dropMissile(current *gameplayPeerSession, boss zonenpc.Snapshot, timestamp uint64) ([][]byte, error) {
	targets := make([]zone.NPCTarget, 0)
	for _, target := range current.zone.LiveNPCTargets() {
		if target.IsHero {
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	// Native Lua samples one of five player slots; empty slots scatter a
	// missile around the boss instead of making every drop target a hero.
	selected := int(current.zone.NPCRandom().Float64() * 5)
	target := targets[0]
	position := boss.Plan.Position
	if selected < len(targets) {
		target = targets[selected]
		position = target.Position
	} else {
		angle := current.zone.NPCRandom().Float64() * 2 * math.Pi
		distance := current.zone.NPCRandom().Float64() * 12
		position.X += float32(math.Cos(angle) * distance)
		position.Y += float32(math.Sin(angle) * distance)
	}
	projected, isProjected, err := zonenavigation.ProjectPosition(current.zone.Navigation(), position, 0.5)
	if err != nil {
		return nil, fmt.Errorf("missileGround: %w", err)
	}
	if isProjected {
		position = projected
	}
	if current.zone.Navigation() != nil && !isProjected {
		return nil, nil
	}
	objectID, err := current.reserveCampaignObjectID()
	if err != nil {
		return nil, fmt.Errorf("missileReserve: %w", err)
	}
	shadowObjectID, err := current.reserveCampaignObjectID()
	if err != nil {
		return nil, fmt.Errorf("missileShadowReserve: %w", err)
	}
	shadowPacket, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{
		ObjectID: shadowObjectID, Noun: util.HashID("RocketShadow.Noun"),
		PositionX: position.X, PositionY: position.Y, PositionZ: position.Z,
		Scale: 1, OwnerID: boss.Plan.ObjectID, IsCollisionEnabled: false,
	})
	if err != nil {
		return nil, fmt.Errorf("missileShadowCreate: %w", err)
	}
	shadowEffect, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: 1, ObjectID: shadowObjectID, IsForceAttached: true,
		Asset: util.HashID("citadel_boss_missile_shadow.ServerEventDef"),
	})
	if err != nil {
		return nil, fmt.Errorf("missileShadowEffect: %w", err)
	}
	origin := position
	origin.Z += 30
	ability := sim.AbilityDefinition{Name: "CitadelBossMissile", Kind: sim.AbilityKindProjectile,
		ProjectileNoun: "Ability_Fireball.Noun", TrailEffectName: "citadel_boss_missile.ServerEventDef",
		ImpactEffectName: "citadel_boss_missile_impact.ServerEventDef", Speed: 15, Distance: 31,
		Cooldown: time.Second, ReleaseDelay: 2100 * time.Millisecond, Range: 40, MinimumDamage: 12, MaximumDamage: 18}
	projectile, packets, err := abilityraknet.NewProjectileRun(abilityraknet.ProjectileInput{
		StartedAt: e.runtime.now(), Ability: ability, ActorObjectID: boss.Plan.ObjectID, TargetObjectID: target.ObjectID,
		ProjectileObjectID: objectID, ActorPosition: sim.Position(origin), TargetPosition: sim.Position(position),
		ImpactPosition: sim.Position(position), ActorFacing: sim.Position{Z: -1}, CollisionDelay: 2 * time.Second,
		IsDirectHit: true, IsCollisionExternallyDriven: true, Damage: 1, TargetHitPoint: target.HitPoint,
		SourceTime:             timestamp,
		IsActivationSuppressed: true, IsTurnSuppressed: true, IsCooldownSuppressed: true, IsReleaseSuppressed: true})
	if err != nil {
		return nil, fmt.Errorf("missileRun: %w", err)
	}
	shotPackets, err := projectile.Advance(context.Background(), 0)
	if err != nil {
		projectile.Stop()
		return nil, fmt.Errorf("missileLaunch: %w", err)
	}
	step := campaignArcturusMissile{launch: e, zone: current.zone, startedAt: e.runtime.now(), direction: sim.Position{Z: -1}, projectile: projectile, position: position, timestamp: timestamp,
		shadowObjectID: shadowObjectID, projectileObjectID: objectID}
	err = current.trackCampaignNPCProjectile(objectID, projectile)
	if err != nil {
		projectile.Stop()
		return nil, fmt.Errorf("missileTrack: %w", err)
	}
	err = e.packet.ScheduleFunc(50*time.Millisecond, step.poll)
	if err != nil {
		current.untrackCampaignNPCProjectile(objectID, projectile)
		projectile.Stop()
		return nil, fmt.Errorf("missileSchedule: %w", err)
	}
	packets = append(packets, shadowPacket, shadowEffect)
	return append(packets, shotPackets...), nil
}

func (e campaignArcturusMissile) impact() ([][]byte, error) {
	run := e.launch
	run.runtime.registry.mutex.Lock()
	defer run.runtime.registry.mutex.Unlock()
	current, isFound := run.runtime.registry.sessions[run.sessionKey]
	if !isFound || current.generation != run.generation || current.zone != e.zone {
		e.projectile.Stop()
		return nil, nil
	}
	if current.isZoneTerminal() || current.campaignNPCProjectiles[e.projectileObjectID] != e.projectile {
		e.projectile.Stop()
		return e.cleanupPackets()
	}
	current.untrackCampaignNPCProjectile(e.projectileObjectID, e.projectile)
	run.runtime.registry.sessions[run.sessionKey] = current
	// Build cleanup independently of the simulator result so an impact error
	// cannot strand either the falling object or its ground marker.
	cleanupPackets, err := e.cleanupPackets()
	if err != nil {
		e.projectile.Stop()
		return nil, fmt.Errorf("missileCleanup: %w", err)
	}
	packets, resolveErr := e.projectile.ResolveCollision(context.Background(), e.flightDelay, true, false, 0,
		0, false, sim.Position(e.position), e.direction)
	e.projectile.Stop()
	if resolveErr != nil {
		run.runtime.logger.Printf("Arcturus missile impact omitted projectile=%d: %v", e.projectileObjectID, resolveErr)
		return cleanupPackets, nil
	}
	// Successful resolution already deletes the projectile. Only the shadow
	// needs explicit removal in that case.
	packets = append(packets, cleanupPackets[:2]...)
	boss, isBossFound := current.zone.NPCs().NPC(run.objectID)
	if !isBossFound || boss.IsDefeated {
		return packets, nil
	}
	profile := zonenpc.ActionProfile{AbilityName: "CitadelBossMissile", Family: zonenpc.ActionCone,
		MinimumDamage: 12, MaximumDamage: 18, DamageCoefficient: 0.05, Radius: 4,
		// Native impulse strength is not a displacement. Use bounded server
		// knockback until the force/mass conversion is recovered.
		ForcedMovementDistance: 2, ForcedMovementSpeed: 8,
		ImpactEffectName: "citadel_boss_missile_hit.ServerEventDef",
		DescriptorMask:   9352, DamageSource: 1, DamageType: 0, IsDamageProfileKnown: true}
	hitPackets, err := run.areaDamage(&current, boss, e.position, profile, e.timestamp)
	run.runtime.registry.sessions[run.sessionKey] = current
	if err != nil {
		return packets, fmt.Errorf("missileDamage: %w", err)
	}
	return append(packets, hitPackets...), nil
}

func (e *campaignArcturusLaunch) areaDamage(current *gameplayPeerSession, boss zonenpc.Snapshot,
	position game.Vec3, profile zonenpc.ActionProfile, timestamp uint64,
) ([][]byte, error) {
	packets := make([][]byte, 0)
	statDelta := sporenet.PlayerStatDelta{}
	for _, target := range current.zone.LiveNPCTargets() {
		updated, err := e.runtime.pursuit.advanceTargetPoseAtLocked(*current, target.ObjectID, e.runtime.now())
		if err != nil {
			return nil, fmt.Errorf("arcturusAreaPose: %w", err)
		}
		*current = updated
	}
	for _, target := range current.zone.LiveNPCTargets() {
		if target.Position.Sub(position).Length() > profile.Radius+target.FootprintRadius {
			continue
		}
		plan, err := zonenpc.PlanAreaAttackWithProfile(boss, target.ObjectID, target.Position, profile)
		if err != nil {
			return nil, fmt.Errorf("arcturusAreaPlan: %w", err)
		}
		plan.SourcePosition = position
		result, err := zonenpc.CommitAttack(current.zone.NPCRandom(), plan, boss.Plan.NPCProfile.CriticalRating, e.runtime.program.Critical)
		if err != nil {
			return nil, fmt.Errorf("arcturusAreaRoll: %w", err)
		}
		hitPackets, targetStatDelta, isApplied, err := e.runtime.applyEnemyAreaAttackDamage(current, e.generation, plan, result, timestamp)
		if err != nil {
			return nil, fmt.Errorf("arcturusAreaHit: %w", err)
		}
		if isApplied {
			statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
		}
		packets = append(packets, hitPackets...)
		if !isApplied {
			continue
		}
		if profile.ImpactEffectName != "" {
			effectPacket, err := npcraknet.AttackImpact(plan)
			if err != nil {
				return packets, fmt.Errorf("arcturusHitEffect: %w", err)
			}
			packets = append(packets, effectPacket)
		}
		if profile.ForcedMovementDistance > 0 {
			liveTarget, isTargetFound := current.zone.NPCTarget(target.ObjectID)
			if !isTargetFound || liveTarget.HitPoint <= 0 {
				continue
			}
			movementPackets, err := e.runtime.applyEnemyForcedMovement(current, plan, liveTarget, timestamp)
			if err != nil {
				return packets, fmt.Errorf("arcturusKnockback: %w", err)
			}
			packets = append(packets, movementPackets...)
		}
	}
	if statDelta != (sporenet.PlayerStatDelta{}) {
		err := e.runtime.stats.Record(context.Background(), current.binding, statDelta)
		if err != nil {
			e.runtime.logger.Printf("Arcturus area statistics omitted: %v", err)
		}
	}
	return packets, nil
}

func (e campaignArcturusMissile) cleanupPackets() ([][]byte, error) {
	shadowStop, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		ObjectID: e.shadowObjectID, Slot: 1, IsRemovalRequested: true, IsHardStop: true,
	})
	if err != nil {
		return nil, fmt.Errorf("shadowStop: %w", err)
	}
	shadowDelete, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.shadowObjectID}})
	if err != nil {
		return nil, fmt.Errorf("shadowDelete: %w", err)
	}
	projectileDelete, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.projectileObjectID}})
	if err != nil {
		return nil, fmt.Errorf("projectileDelete: %w", err)
	}
	return [][]byte{shadowStop, shadowDelete, projectileDelete}, nil
}
