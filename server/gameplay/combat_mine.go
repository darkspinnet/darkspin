package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const (
	scaldronMineMinimumSpawnDistance = 3
	scaldronMineMaximumSpawnDistance = 12
	scaldronMineMinimumFlight        = 300 * time.Millisecond
	scaldronMineMaximumFlight        = 600 * time.Millisecond
	scaldronMineTriggerPoll          = 100 * time.Millisecond
	scaldronMineExplosionDuration    = time.Second
)

type campaignScaldronDeathMineRun struct {
	projectileObjectID uint32
	mineObjectID       uint32
	position           game.Vec3
}

type campaignScaldronDeathMineSchedule struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	timestamp  uint64
	source     zonenpc.Snapshot
	profile    zonenpc.ActionProfile
	binding    game.GameplayBinding
	toss       zoneability.TossPlan
	run        *campaignScaldronDeathMineRun
}

func (e campaignScaldronDeathMineSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCDeathMines[e.run.mineObjectID] == e.run
}

func (e campaignScaldronDeathMineSchedule) landing() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	landingPackets, err := abilityraknet.TossDirectLanding(
		e.toss, e.run.projectileObjectID, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("deathMineLanding: %w", err)
	}
	definition := sim.AbilityDefinition{SpawnNoun: e.profile.RetainedObjectNoun}
	spawnPacket, err := abilityraknet.TrapSpawnForTeam(
		e.run.mineObjectID, e.source.Plan.ObjectID,
		raknet.Vector3{X: e.run.position.X, Y: e.run.position.Y, Z: e.run.position.Z},
		definition, 0,
	)
	if err != nil {
		return nil, fmt.Errorf("deathMineSpawn: %w", err)
	}
	return append(landingPackets, spawnPacket), nil
}

func (e campaignScaldronDeathMineSchedule) trigger() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound) && peerSession.zone != nil
	isTriggered := false
	if isCurrent {
		for _, target := range peerSession.zone.LiveNPCTargets() {
			if target.HitPoint > 0 && zonegeometry.Distance(
				e.run.position, target.Position,
			) <= e.profile.MinimumRange+target.FootprintRadius {
				isTriggered = true
				break
			}
		}
	}
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent || !isTriggered {
		return nil, nil
	}
	return e.explode()
}

func (e campaignScaldronDeathMineSchedule) explode() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || peerSession.zone.NPCRandom() == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	delete(peerSession.campaignNPCDeathMines, e.run.mineObjectID)
	packets := make([][]byte, 0)
	statDelta := sporenet.PlayerStatDelta{}
	for _, target := range campaignLobAreaTargets(
		peerSession.zone.LiveNPCTargets(), e.run.position, e.profile.Radius,
	) {
		plan, planErr := zonenpc.PlanRetainedAreaAttackWithProfile(
			e.source, target.ObjectID, target.Position, e.profile,
		)
		if planErr != nil {
			continue
		}
		result, commitErr := zonenpc.CommitAttack(
			peerSession.zone.NPCRandom(), plan,
			e.source.Plan.NPCProfile.CriticalRating, e.runtime.program.Critical,
		)
		if commitErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("deathMineCommit: %w", commitErr)
		}
		hitPackets, targetStatDelta, _, damageErr :=
			e.runtime.applyEnemyStatusDamage(
				&peerSession, e.generation, plan, result, e.timestamp,
			)
		if damageErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("deathMineDamage: %w", damageErr)
		}
		packets = append(packets, hitPackets...)
		statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	explosionPacket, err := abilityraknet.TrapObjectEffect(
		e.profile.RetainedEffectName, e.run.mineObjectID, e.source.Plan.ObjectID,
	)
	if err != nil {
		return nil, fmt.Errorf("deathMineExplosion: %w", err)
	}
	err = e.runtime.stats.Record(context.Background(), e.binding, statDelta)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet Scaldron death mine stats omitted object=%d: %v",
			e.run.mineObjectID, err,
		)
	}
	_, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: scaldronMineExplosionDuration, Produce: e.cleanup,
	}})
	if scheduleErr != nil {
		cleanupPackets, cleanupErr := e.cleanup()
		if cleanupErr != nil {
			return nil, fmt.Errorf("deathMineCleanupFallback: %w", cleanupErr)
		}
		packets = append(packets, explosionPacket)
		return append(packets, cleanupPackets...), nil
	}
	return append(packets, explosionPacket), nil
}

func (e campaignScaldronDeathMineSchedule) cleanup() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	effectPacket, err := abilityraknet.TrapRemoveEffect(e.run.mineObjectID)
	if err != nil {
		return nil, fmt.Errorf("deathMineEffectRemove: %w", err)
	}
	deletePacket, err := abilityraknet.TrapDelete(e.run.mineObjectID)
	if err != nil {
		return nil, fmt.Errorf("deathMineDelete: %w", err)
	}
	return [][]byte{effectPacket, deletePacket}, nil
}

func (e campaignScaldronDeathMineSchedule) timeout() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	delete(peerSession.campaignNPCDeathMines, e.run.mineObjectID)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	effectPacket, err := abilityraknet.TrapRemoveEffect(e.run.mineObjectID)
	if err != nil {
		return nil, fmt.Errorf("deathMineTimeoutEffect: %w", err)
	}
	deletePacket, err := abilityraknet.TrapDelete(e.run.mineObjectID)
	if err != nil {
		return nil, fmt.Errorf("deathMineTimeout: %w", err)
	}
	return [][]byte{effectPacket, deletePacket}, nil
}

func (r campaignNPCActionRuntime) spawnScaldronDeathMines(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	if packet.ScheduleProducers == nil {
		return nil, false, errors.New("death mine scheduler unavailable")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.ScaldronBasicMinesDeathProfile(
		source.Plan.NounName,
	)
	if !isSourceFound || !source.IsDefeated || !isProfileFound ||
		peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	objectCount := profile.ProjectileShotCount * 2
	firstObjectID, err := peerSession.reserveCampaignProjectileIDs(objectCount, 1000)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("deathMineObjectID: %w", err)
	}
	if peerSession.campaignNPCDeathMines == nil {
		peerSession.campaignNPCDeathMines =
			make(map[uint32]*campaignScaldronDeathMineRun)
	}
	schedules := make([]campaignScaldronDeathMineSchedule, 0, profile.ProjectileShotCount)
	for mineIndex := uint32(0); mineIndex < profile.ProjectileShotCount; mineIndex++ {
		distance := scaldronMineMinimumSpawnDistance +
			peerSession.zone.NPCRandom().Float64()*
				(scaldronMineMaximumSpawnDistance-scaldronMineMinimumSpawnDistance)
		destination, isDestinationFound, destinationErr :=
			zonenavigation.RandomTeleportDestination(
				peerSession.zone.Navigation(), peerSession.zone.NPCRandom(),
				zonenavigation.RandomTeleportRequest{
					SourcePosition:  source.Plan.Position,
					FootprintRadius: max(source.Plan.NPCProfile.FootprintRadius, 0.25),
					MinimumDistance: scaldronMineMinimumSpawnDistance,
					NormalDistance:  float32(distance),
					MaximumDistance: scaldronMineMaximumSpawnDistance,
				},
			)
		if destinationErr != nil {
			r.registry.mutex.Unlock()
			return nil, false, fmt.Errorf("deathMineDestination: %w", destinationErr)
		}
		if !isDestinationFound {
			continue
		}
		flightDuration := scaldronMineMinimumFlight + time.Duration(
			peerSession.zone.NPCRandom().Float64()*float64(
				scaldronMineMaximumFlight-scaldronMineMinimumFlight,
			),
		)
		launchPosition := sim.Position{
			X: source.Plan.Position.X, Y: source.Plan.Position.Y,
			Z: source.Plan.Position.Z,
		}
		landingPosition := sim.Position{X: destination.X, Y: destination.Y, Z: destination.Z}
		lob, lobErr := sim.BuildTossLob(
			0, launchPosition, landingPosition, profile.ProjectileHeight,
			flightDuration, 0, 0, 0, true, false,
		)
		if lobErr != nil {
			continue
		}
		projectileObjectID := firstObjectID + mineIndex*2
		mineObjectID := projectileObjectID + 1
		run := &campaignScaldronDeathMineRun{
			projectileObjectID: projectileObjectID,
			mineObjectID:       mineObjectID, position: destination,
		}
		peerSession.campaignNPCDeathMines[mineObjectID] = run
		schedules = append(schedules, campaignScaldronDeathMineSchedule{
			runtime: r, packet: packet, sessionKey: sessionKey,
			generation: generation, timestamp: timestamp,
			source: source, profile: profile, binding: peerSession.binding,
			toss: zoneability.TossPlan{
				SourceObjectID: source.Plan.ObjectID,
				AbilityID:      util.HashID(profile.AbilityName),
				Definition: sim.AbilityDefinition{
					Name: profile.AbilityName, Kind: sim.AbilityKindToss,
					SpawnNoun: profile.RetainedObjectNoun,
					Toss: sim.TossAbilityDefinition{
						Behavior:             sim.TossAbilityBehaviorTrapper,
						ProjectileNoun:       profile.ProjectileNoun,
						ProjectileEffectName: profile.TrailEffectName,
						ImpactEffectName:     profile.ProjectileExitEffectName,
						Radius:               profile.Radius,
					},
				},
				LaunchPosition: launchPosition, Destination: landingPosition,
				Lob: lob,
			},
			run: run,
		})
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	if len(schedules) == 0 {
		return nil, false, nil
	}
	packets := make([][]byte, 0, len(schedules)*2)
	producers := make([]raknet.ScheduledPacketProducer, 0)
	for _, schedule := range schedules {
		launchPackets, launchErr := abilityraknet.TossLaunchForTeam(
			schedule.toss, schedule.run.projectileObjectID, timestamp, 0,
		)
		if launchErr != nil {
			r.clearScaldronDeathMineSchedules(sessionKey, generation, schedules)
			return nil, false, fmt.Errorf("deathMineLaunch: %w", launchErr)
		}
		packets = append(packets, launchPackets...)
		flightDuration := schedule.toss.Lob.Duration
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: flightDuration, Produce: schedule.landing,
		})
		activeStart := flightDuration + profile.HitDelay
		for delay := activeStart; delay < activeStart+profile.ModifierDuration; delay += scaldronMineTriggerPoll {
			triggerSchedule := schedule
			triggerSchedule.timestamp = timestamp + uint64(delay/time.Millisecond)
			producers = append(producers, raknet.ScheduledPacketProducer{
				Delay: delay, Produce: triggerSchedule.trigger,
			})
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay:   activeStart + profile.ModifierDuration,
			Produce: schedule.timeout,
		})
	}
	_, scheduleErr := scheduleNPCProducers(r.registry, packet, producers)
	if scheduleErr != nil {
		r.clearScaldronDeathMineSchedules(sessionKey, generation, schedules)
		return nil, false, fmt.Errorf("deathMineSchedule: %w", scheduleErr)
	}
	return packets, true, nil
}

func (r campaignNPCActionRuntime) clearScaldronDeathMineSchedules(
	sessionKey string, generation uint64,
	schedules []campaignScaldronDeathMineSchedule,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if isFound && peerSession.generation == generation {
		for _, schedule := range schedules {
			if peerSession.campaignNPCDeathMines[schedule.run.mineObjectID] ==
				schedule.run {
				delete(peerSession.campaignNPCDeathMines, schedule.run.mineObjectID)
			}
		}
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
}
