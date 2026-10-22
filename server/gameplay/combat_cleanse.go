package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignMaserCleanseStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
	profile    zonenpc.ActionProfile
}

func (e campaignMaserCleanseStep) hit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.objectID,
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	if !isSourceFound {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	allies, err := peerSession.zone.NPCs().ActorsInSphere(zonenpc.SphereRequest{
		Center: source.Plan.Position, Radius: e.profile.Radius,
	})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("maserCleanseAllies: %w", err)
	}
	packets := make([][]byte, 0, len(allies)+1)
	novaPacket, err := npcraknet.PositionedEffect(
		e.profile.TrailEffectName, source.Plan.Position,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("maserCleanseNova: %w", err)
	}
	packets = append(packets, novaPacket)
	for _, ally := range allies {
		fixturePackets, fixtureErr := e.runtime.registry.retireFixtureBurnsLocked(peerSession.zone.NPCs(), ally.Plan.ObjectID, e.runtime.modifierPool)
		packets = append(packets, fixturePackets...)
		if fixtureErr != nil {
			e.runtime.registry.mutex.Unlock()
			return packets, fmt.Errorf("fixtureCleanse: %w", fixtureErr)
		}
		cloudPackets, cloudErr := e.runtime.registry.retireCloudPoisonLocked(peerSession.zone.NPCs(), ally.Plan.ObjectID, e.runtime.modifierPool)
		packets = append(packets, cloudPackets...)
		if cloudErr != nil {
			e.runtime.registry.mutex.Unlock()
			return packets, fmt.Errorf("cloudCleanse: %w", cloudErr)
		}
		isPurged := peerSession.zone.NPCs().PurgeDebuffs(ally.Plan.ObjectID)
		if !isPurged && len(cloudPackets) == 0 && len(fixturePackets) == 0 {
			continue
		}
		packets = append(packets, e.runtime.registry.retireTargetInfectionRootsLocked(peerSession.zone.NPCs(), ally.Plan.ObjectID)...)
		beastPacket, beastErr := e.runtime.registry.retireBeastVulnerabilityLocked(
			peerSession.zone.NPCs(), peerSession.zone.Effect(), ally.Plan.ObjectID, e.runtime.modifierPool,
		)
		if beastErr != nil {
			e.runtime.registry.mutex.Unlock()
			return packets, fmt.Errorf("beastCleanse: %w", beastErr)
		}
		if beastPacket != nil {
			packets = append(packets, beastPacket)
		}
		basicPackets, basicErr := e.runtime.registry.retireTargetBasicModifiersLocked(peerSession, ally.Plan.ObjectID, e.runtime.modifierPool, true)
		packets = append(packets, basicPackets...)
		if basicErr != nil {
			e.runtime.registry.mutex.Unlock()
			return packets, fmt.Errorf("basicCleanse: %w", basicErr)
		}
		hitPacket, hitErr := npcraknet.PositionedEffect(
			e.profile.ImpactEffectName, ally.Plan.Position,
		)
		if hitErr != nil {
			e.runtime.registry.mutex.Unlock()
			return packets, fmt.Errorf("maserCleanseHit: %w", hitErr)
		}
		packets = append(packets, hitPacket)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	return packets, nil
}

func (e campaignMaserCleanseStep) next() ([][]byte, error) {
	timestamp := e.timestamp + uint64(e.profile.Cooldown/time.Millisecond)
	return e.runtime.produceEnemyCone(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

func (r campaignNPCActionRuntime) produceScaldronBasicMaserCleanse(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	if sessionKey == "" || generation == 0 || objectID == 0 {
		return nil, false, errors.New("maser cleanse request invalid")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, objectID,
	)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.ScaldronBasicMaserCleanseProfile(
		source.Plan.NounName,
	)
	if !isSourceFound || !isProfileFound ||
		peerSession.campaignNPCMaserCleanseReadiness[objectID] > timestamp {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	profile = zonenpc.ApplyAffixTiming(profile, source.Plan)
	allies, err := peerSession.zone.NPCs().ActorsInSphere(zonenpc.SphereRequest{
		Center: source.Plan.Position, Radius: profile.Radius,
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("maserCleanseSelection: %w", err)
	}
	isNeeded := false
	now := r.now()
	for _, ally := range allies {
		if (peerSession.zone.NPCs().HasDebuff(ally.Plan.ObjectID, now) ||
			r.registry.hasCloudPoisonLocked(peerSession.zone.NPCs(), ally.Plan.ObjectID) ||
			r.registry.hasFixtureBurnLocked(peerSession.zone.NPCs(), ally.Plan.ObjectID)) &&
			zonegeometry.Distance(source.Plan.Position, ally.Plan.Position) <= profile.Radius {
			isNeeded = true
			break
		}
	}
	if !isNeeded {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	if peerSession.campaignNPCMaserCleanseReadiness == nil {
		peerSession.campaignNPCMaserCleanseReadiness = make(map[uint32]uint64)
	}
	peerSession.campaignNPCMaserCleanseReadiness[objectID] = timestamp +
		uint64(profile.Cooldown/time.Millisecond)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	plan := zonenpc.AttackPlan{
		ActionGeneration: source.ActionGeneration,
		SourceObjectID:   objectID, TargetObjectID: objectID,
		SourcePosition: source.Plan.Position, TargetPosition: source.Plan.Position,
		Profile: profile,
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, false, fmt.Errorf("maserCleanseStart: %w", err)
	}
	step := campaignMaserCleanseStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		profile: profile,
	}
	cancel, err := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: step.hit},
		{Delay: profile.Cooldown, Produce: step.next},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, false, fmt.Errorf("maserCleanseSchedule: %w", err)
	}
	return startPackets, true, nil
}
