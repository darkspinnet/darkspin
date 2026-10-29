package gameplay

import (
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type nomadFrogPool struct {
	runtime           campaignNPCActionRuntime
	packet            raknet.Packet
	sessionKey        string
	generation        uint64
	world             *zone.Zone
	source            zonenpc.Snapshot
	initiatorObjectID uint32
	profile           zonenpc.ActionProfile
	tickDamage        game.DamageRange
	center            game.Vec3
	radius            float32
	timestamp         uint64
	tick              uint32
	cancel            raknet.CancelSchedule
	isRetired         bool
}

func (e campaignNPCActionRuntime) frogTossLanding(cast *nomadFrogCast, center game.Vec3, timestamp uint64) ([][]byte, error) {
	e.registry.mutex.RLock()
	member, isFound := e.registry.sessions[cast.sessionKey]
	if !isFound || member.generation != cast.generation || member.zone == nil || member.isZoneTerminal() {
		e.registry.mutex.RUnlock()
		return nil, nil
	}
	samples, sampleErr := e.registry.sampleNPCTargetsLocked(member.zone, member.zone.LiveNPCTargets(), e.now())
	if sampleErr != nil {
		e.registry.mutex.RUnlock()
		return nil, fmt.Errorf("frogLandingTargets: %w", sampleErr)
	}
	targets := campaignLobAreaTargets(samples, center, cast.child.Radius)
	current, isSourceFound := member.zone.NPCs().NPC(cast.plan.SourceObjectID)
	if isSourceFound {
		cast.source = current
	}
	e.registry.mutex.RUnlock()
	sort.SliceStable(targets, func(i, j int) bool {
		return targets[i].Position.Sub(center).LengthSquared() < targets[j].Position.Sub(center).LengthSquared()
	})
	for _, target := range targets {
		if !target.IsHero || !e.canApplyOperativeCage(cast.sessionKey, cast.generation, zonenpc.AttackPlan{SourceObjectID: cast.plan.SourceObjectID, TargetObjectID: target.ObjectID}) {
			continue
		}
		plan := cast.plan
		plan.Profile = cast.child
		plan.TargetObjectID, plan.TargetPosition = target.ObjectID, target.Position
		packets, err := e.applyCampaignNPCTimedModifier(cast.packet, cast.sessionKey, cast.generation, plan, timestamp)
		if err != nil {
			return nil, fmt.Errorf("frogLandingCage: %w", err)
		}
		poolPackets, poolErr := e.startNomadFrogPool(cast, target.Position, timestamp)
		if poolErr != nil {
			return nil, fmt.Errorf("frogLandingPool: %w", poolErr)
		}
		return append(packets, poolPackets...), nil
	}
	return nil, nil
}

func (e campaignNPCActionRuntime) startNomadFrogPool(cast *nomadFrogCast, center game.Vec3, timestamp uint64) ([][]byte, error) {
	profile := zonenpc.NomadSpecialFourPoisonProfile(cast.child.AbilityRank)
	tickDamage, err := zonenpc.ResolveModifierTickDamage(cast.source, profile)
	if err != nil {
		return nil, fmt.Errorf("frogPoolDamage: %w", err)
	}
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[cast.sessionKey]
	if !isFound || member.generation != cast.generation || member.zone == nil || member.isZoneTerminal() {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	objectID, err := member.reserveCampaignProjectileIDs(1, 1000)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("frogPoolReserve: %w", err)
	}
	source := cast.source
	source.Plan = source.Plan.Clone()
	source.Plan.ObjectID, source.Plan.NounName, source.Plan.Position = objectID, "NomadSpecialFourPoisonPool.Noun", center
	run := &nomadFrogPool{runtime: e, packet: cast.packet, sessionKey: cast.sessionKey, generation: cast.generation,
		world: member.zone, source: source, initiatorObjectID: cast.source.Plan.ObjectID,
		profile: profile, tickDamage: tickDamage, center: center, radius: cast.child.Radius, timestamp: timestamp}
	if member.nomadFrogPools == nil {
		member.nomadFrogPools = make(map[uint32]*nomadFrogPool)
	}
	member.nomadFrogPools[objectID] = run
	e.registry.sessions[cast.sessionKey] = member
	e.registry.mutex.Unlock()
	packet, err := raknet.MarshalApplication(raknet.ObjectCreateMessage{ObjectID: objectID, Noun: util.HashID(source.Plan.NounName),
		PositionX: center.X, PositionY: center.Y, PositionZ: center.Z, Scale: 1, IsCollisionEnabled: false})
	if err != nil {
		return nil, fmt.Errorf("frogPoolCreate: %w", err)
	}
	err = run.schedule()
	if err != nil {
		return nil, fmt.Errorf("frogPoolSchedule: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *nomadFrogPool) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && member.generation == e.generation && member.zone == e.world && !member.isZoneTerminal() && member.nomadFrogPools[e.source.Plan.ObjectID] == e
	var targets []zone.NPCTarget
	if isCurrent {
		samples, sampleErr := e.runtime.registry.sampleNPCTargetsLocked(member.zone, member.zone.LiveNPCTargets(), e.runtime.now())
		if sampleErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("frogPoolTargets: %w", sampleErr)
		}
		targets = campaignLobAreaTargets(samples, e.center, e.radius)
		e.tick++
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		err := e.retire()
		if err != nil {
			return nil, fmt.Errorf("frogPoolOwnerRetire: %w", err)
		}
		return nil, nil
	}
	var packets [][]byte
	for _, target := range targets {
		plan := zonenpc.AttackPlan{SourceObjectID: e.source.Plan.ObjectID, InitiatorObjectID: e.initiatorObjectID, TargetObjectID: target.ObjectID,
			SourcePosition: e.center, TargetPosition: target.Position, Profile: e.profile}
		modifierPackets, err := e.runtime.applyCampaignNPCDisease(nil, e.packet, e.sessionKey, e.generation, plan, e.timestamp+uint64(e.tick)*250, e.tickDamage)
		if err != nil {
			return nil, fmt.Errorf("frogPoolPoison: %w", err)
		}
		packets = append(packets, modifierPackets...)
	}
	if e.tick < 25 {
		err := e.schedule()
		if err != nil {
			return nil, fmt.Errorf("frogPoolContinue: %w", err)
		}
		return packets, nil
	}
	err := e.retire()
	if err != nil {
		return nil, fmt.Errorf("frogPoolExpire: %w", err)
	}
	return packets, nil
}

func (e *nomadFrogPool) retire() error {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	err := e.retireLocked()
	if err != nil {
		return fmt.Errorf("frogPoolRetire: %w", err)
	}
	return nil
}

func (e *nomadFrogPool) retireLocked() error {
	if e.isRetired {
		return nil
	}
	deleted, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.source.Plan.ObjectID}})
	if err != nil {
		return fmt.Errorf("frogPoolDelete: %w", err)
	}
	e.isRetired = true
	if e.cancel != nil {
		e.cancel()
	}
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && member.nomadFrogPools[e.source.Plan.ObjectID] == e {
		delete(member.nomadFrogPools, e.source.Plan.ObjectID)
		e.runtime.registry.sessions[e.sessionKey] = member
	}
	e.runtime.registry.queueChannelPresentationLocked(e.world, [][]byte{deleted})
	return nil
}

func (e *nomadFrogPool) schedule() error {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone != e.world || member.nomadFrogPools[e.source.Plan.ObjectID] != e {
		err := e.retireLocked()
		if err != nil {
			return fmt.Errorf("frogPoolScheduleRetire: %w", err)
		}
		return nil
	}
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{Delay: 250 * time.Millisecond, Produce: e.produce}})
	if err != nil {
		return fmt.Errorf("frogPoolContinueSchedule: %w", err)
	}
	e.cancel = cancel
	return nil
}

// Called before the owning transport/session is replaced. No action-generation
// cleanup calls this: a released/dead caster leaves its authored pool alive.
func (e *gameplaySessionRegistry) retireNomadFrogPoolsLocked(member *gameplayPeerSession) error {
	objectIDs := make([]uint32, 0, len(member.nomadFrogPools))
	for objectID := range member.nomadFrogPools {
		objectIDs = append(objectIDs, objectID)
	}
	if len(objectIDs) == 0 {
		return nil
	}
	deleted, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: objectIDs})
	if err != nil {
		return fmt.Errorf("frogPoolsDelete: %w", err)
	}
	for _, pool := range member.nomadFrogPools {
		pool.isRetired = true
		if pool.cancel != nil {
			pool.cancel()
		}
	}
	clear(member.nomadFrogPools)
	e.queueChannelPresentationLocked(member.zone, [][]byte{deleted})
	return nil
}
