package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

type campaignArcturusTurret struct {
	owner          campaignArcturusSpawnStep
	zone           *zone.Zone
	objectID       uint32
	markerIDs      [2]uint32
	startedAt      time.Time
	nextHitAt      time.Time
	protectedUntil time.Time
	isProtected    bool
}

func (e campaignArcturusSpawnStep) startTurret(current *gameplayPeerSession, boss zonenpc.Snapshot) ([][]byte, error) {
	rank := zonenpc.ArcturusRank(boss.Plan.NounName)
	nounName := fmt.Sprintf("CitadelBossTurret_v%d.Noun", rank-1)
	profile, isFound := current.zone.DirectorDefinition().NPCProfilesByNoun[strings.ToLower(nounName)]
	if !isFound || !profile.IsKnown {
		return nil, errors.New("arcturus turret profile unavailable")
	}
	objectID, err := current.reserveCampaignObjectID()
	if err != nil {
		return nil, fmt.Errorf("turretReserve: %w", err)
	}
	run := &campaignArcturusTurret{owner: e, zone: current.zone, objectID: objectID, startedAt: e.runtime.now(), nextHitAt: e.runtime.now().Add(500 * time.Millisecond)}
	for index := range run.markerIDs {
		run.markerIDs[index], err = current.reserveCampaignObjectID()
		if err != nil {
			return nil, fmt.Errorf("turretMarkerReserve: %w", err)
		}
	}
	action := zonenpc.ActionProfile{Family: zonenpc.ActionCone, AbilityName: "TurretLaserSweep",
		Range: 40, Radius: 40, MinimumDamage: float32(rank * 3), MaximumDamage: float32(rank * 6),
		DescriptorMask: 1024, DamageSource: 1, DamageType: 0, IsDamageProfileKnown: true}
	plans := []zonenpc.SpawnPlan{{ObjectID: objectID, OwnerObjectID: boss.Plan.ObjectID,
		NounName: nounName, Position: e.state.center, NPCProfile: profile,
		ActionProfile: action, IsActionKnown: true, IsRewardSuppressed: true,
		LocusID: boss.Plan.LocusID, MarkerSetName: boss.Plan.MarkerSetName}}
	packets, err := npcraknet.TargetedSpawn(plans[0], boss.TargetObjectID)
	if err != nil {
		return nil, fmt.Errorf("turretMarshal: %w", err)
	}
	for _, markerID := range run.markerIDs {
		packet, marshalErr := raknet.MarshalApplication(raknet.ObjectCreateMessage{
			ObjectID: markerID, Noun: util.HashID("SweepingBeamMarker.Noun"), OwnerID: objectID,
			PositionX: e.state.center.X, PositionY: e.state.center.Y, PositionZ: e.state.center.Z,
			Scale: 1, IsCollisionEnabled: false})
		if marshalErr != nil {
			return nil, fmt.Errorf("turretMarkerCreate: %w", marshalErr)
		}
		packets = append(packets, packet)
	}
	effects := []raknet.AttachedEffectMessage{
		{ObjectID: run.markerIDs[0], SecondaryObjectID: run.markerIDs[1], Slot: 1, IsForceAttached: true, Asset: util.HashID("citadel_boss_turret_laser.ServerEventDef")},
		{ObjectID: run.markerIDs[0], Slot: 2, IsForceAttached: true, Asset: util.HashID("citadel_boss_turret_laser_beam_muzzle.ServerEventDef")},
		{ObjectID: run.markerIDs[1], Slot: 1, IsForceAttached: true, Asset: util.HashID("citadel_boss_turret_laser_beam_tip.ServerEventDef")},
	}
	for _, effect := range effects {
		packet, marshalErr := raknet.MarshalApplication(effect)
		if marshalErr != nil {
			return nil, fmt.Errorf("turretEffect: %w", marshalErr)
		}
		packets = append(packets, packet)
	}
	err = current.zone.NPCs().Add(plans, boss.TargetObjectID)
	if err != nil {
		return nil, fmt.Errorf("turretAdd: %w", err)
	}
	err = current.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{Plans: plans, TargetObjectID: boss.TargetObjectID}, current.binding.UserID, current.generation)
	if err != nil {
		rollbackErr := current.zone.NPCs().RollbackAdd(plans)
		return nil, fmt.Errorf("turretPublish: %w", errors.Join(err, rollbackErr))
	}
	snapshot, isStarted, isFirst, err := current.zone.NPCs().StartAction(objectID,
		zonenpc.ActionOwner{UserID: current.binding.UserID, PeerGeneration: current.generation}, boss.TargetObjectID)
	if err == nil && (!isStarted || !isFirst || !snapshot.IsActionStarted) {
		err = errors.New("turret action rejected")
	}
	if err == nil {
		run.protectedUntil = run.startedAt.Add(time.Second)
		err = current.zone.NPCs().ApplyIntangible(objectID, run.protectedUntil)
		run.isProtected = err == nil
	}
	if err == nil {
		err = scheduleNPCProducer(e.runtime.registry, e.packet, 100*time.Millisecond, run.tick)
	}
	if err != nil {
		despawnErr := current.zone.NPCs().Despawn([]uint32{objectID})
		cleanupPackets, cleanupErr := run.cleanup()
		return append(packets, cleanupPackets...), fmt.Errorf("turretStart: %w", errors.Join(err, despawnErr, cleanupErr))
	}
	e.state.turret = run
	return packets, nil
}

func (e *campaignArcturusTurret) tick() ([][]byte, error) {
	owner := e.owner
	owner.runtime.registry.mutex.Lock()
	current, isFound := owner.runtime.registry.sessions[owner.sessionKey]
	if !isFound || current.generation != owner.generation ||
		current.zone != e.zone {
		owner.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	boss, isBossFound := current.zone.NPCs().NPC(owner.objectID)
	turret, isTurretFound := current.zone.NPCs().NPC(e.objectID)
	if current.isZoneTerminal() || !isBossFound || boss.IsDefeated || !isTurretFound || turret.IsDefeated || owner.state.turret != e {
		err := current.zone.NPCs().Despawn([]uint32{e.objectID})
		owner.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("turretDespawn: %w", err)
		}
		return e.cleanup()
	}
	now := owner.runtime.now()
	isAirborne := owner.state.launch != nil && now.Before(owner.state.launch.protectedUntil) &&
		!now.Before(owner.state.launch.protectedUntil.Add(-8200*time.Millisecond))
	var err error
	if !isAirborne {
		e.protectedUntil = now.Add(time.Second)
		err = current.zone.NPCs().ApplyIntangible(e.objectID, e.protectedUntil)
		e.isProtected = err == nil
	} else if e.isProtected {
		current.zone.NPCs().ClearIntangible(e.objectID, e.protectedUntil)
		e.isProtected = false
	}
	// The native orbit rate is unrecovered. Server-owned markers keep the
	// conservative 24-second orbit identical for presentation and hit checks.
	angle := now.Sub(e.startedAt).Seconds() * 2 * math.Pi / 24
	start := owner.state.center
	start.X += float32(math.Cos(angle))
	start.Y += float32(math.Sin(angle))
	end := owner.state.center
	end.X += 40 * float32(math.Cos(angle))
	end.Y += 40 * float32(math.Sin(angle))
	clipped, isClipped, clipErr := navigationClippedMovementDestination(current.zone.Navigation(), start, end, 0.25)
	if clipErr != nil {
		err = fmt.Errorf("turretBeamClip: %w", clipErr)
	} else if isClipped {
		end = clipped
	} else if current.zone.Navigation() != nil {
		// Never damage through unavailable geometry.
		end = start
	}
	packets := make([][]byte, 0)
	if err == nil {
		for index, position := range []game.Vec3{start, end} {
			packet, moveErr := npcraknet.RestorePose(e.markerIDs[index], position, game.Vec3{Y: 1})
			if moveErr != nil {
				err = fmt.Errorf("turretMarkerMove: %w", moveErr)
				break
			}
			packets = append(packets, packet)
		}
	}
	if err == nil && !now.Before(e.nextHitAt) {
		e.nextHitAt = now.Add(500 * time.Millisecond)
		for _, target := range current.zone.LiveNPCTargets() {
			updated, poseErr := owner.runtime.pursuit.advanceTargetPoseAtLocked(current, target.ObjectID, now)
			if poseErr != nil {
				err = fmt.Errorf("turretTargetPose: %w", poseErr)
				break
			}
			current = updated
		}
		if err == nil {
			for _, target := range current.zone.LiveNPCTargets() {
				if !isCampaignLineTarget(start, end, target) {
					continue
				}
				plan, planErr := zonenpc.PlanAreaAttackWithProfile(turret, target.ObjectID, target.Position, turret.Plan.ActionProfile)
				if planErr != nil {
					err = fmt.Errorf("turretHitPlan: %w", planErr)
					break
				}
				result, rollErr := zonenpc.CommitAttack(current.zone.NPCRandom(), plan, turret.Plan.NPCProfile.CriticalRating, owner.runtime.program.Critical)
				if rollErr != nil {
					err = fmt.Errorf("turretHitRoll: %w", rollErr)
					break
				}
				hits, stats, isApplied, hitErr := owner.runtime.applyEnemyAreaAttackDamage(&current, owner.generation, plan, result, owner.timestamp+uint64(now.Sub(e.startedAt)/time.Millisecond))
				if hitErr != nil {
					err = fmt.Errorf("turretHit: %w", hitErr)
					break
				}
				packets = append(packets, hits...)
				if isApplied {
					effect, effectErr := npcraknet.PositionedEffect("citadel_boss_turret_laser_beam_hit.ServerEventDef", target.Position)
					if effectErr != nil {
						err = fmt.Errorf("turretHitEffect: %w", effectErr)
						break
					}
					packets = append(packets, effect)
					statErr := owner.runtime.stats.Record(context.Background(), current.binding, stats)
					if statErr != nil {
						owner.runtime.logger.Printf("Arcturus turret statistics omitted: %v", statErr)
					}
				}
			}
		}
	}
	owner.runtime.registry.sessions[owner.sessionKey] = current
	owner.runtime.registry.mutex.Unlock()
	if err != nil {
		owner.runtime.logger.Printf("Arcturus turret tick omitted: %v", err)
	}
	err = scheduleNPCProducer(owner.runtime.registry, owner.packet, 100*time.Millisecond, e.tick)
	if err != nil {
		owner.runtime.logger.Printf("Arcturus turret stopped after scheduler failure: %v", err)
		owner.runtime.registry.mutex.Lock()
		latest, isLatestFound := owner.runtime.registry.sessions[owner.sessionKey]
		if isLatestFound && latest.generation == owner.generation && latest.zone == e.zone {
			despawnErr := latest.zone.NPCs().Despawn([]uint32{e.objectID})
			if despawnErr != nil {
				owner.runtime.logger.Printf("Arcturus turret retirement failed: %v", despawnErr)
			}
		}
		owner.runtime.registry.mutex.Unlock()
		cleanupPackets, cleanupErr := e.cleanup()
		if cleanupErr != nil {
			return packets, fmt.Errorf("turretCleanup: %w", cleanupErr)
		}
		return append(packets, cleanupPackets...), nil
	}
	return packets, nil
}

func (e *campaignArcturusTurret) cleanup() ([][]byte, error) {
	packets := make([][]byte, 0)
	for index, objectID := range e.markerIDs {
		maximumSlot := uint8(1)
		if index == 0 {
			maximumSlot = 2
		}
		for slot := uint8(1); slot <= maximumSlot; slot++ {
			packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
				ObjectID: objectID, Slot: slot, IsRemovalRequested: true, IsHardStop: true})
			if err != nil {
				return nil, fmt.Errorf("turretEffectStop: %w", err)
			}
			packets = append(packets, packet)
		}
	}
	for _, objectID := range []uint32{e.markerIDs[0], e.markerIDs[1], e.objectID} {
		packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{objectID}})
		if err != nil {
			return nil, fmt.Errorf("turretDelete: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
