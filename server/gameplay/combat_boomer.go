package gameplay

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignBoomerDeathSchedule struct {
	runtime    campaignNPCActionRuntime
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e campaignBoomerDeathSchedule) detonate() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.zone.NPCRandom() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	profile, isProfileFound := zonenpc.BoomerDeathDetonationProfile(
		source.Plan.NounName,
	)
	if !isSourceFound || !source.IsDefeated || !isProfileFound {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets := make([][]byte, 0)
	statDelta := sporenet.PlayerStatDelta{}
	for _, target := range campaignLobAreaTargets(
		peerSession.zone.LiveNPCTargets(), source.Plan.Position, profile.Radius,
	) {
		plan, err := zonenpc.PlanRetainedAreaAttackWithProfile(
			source, target.ObjectID, target.Position, profile,
		)
		if err != nil {
			continue
		}
		result, err := zonenpc.CommitAttack(
			peerSession.zone.NPCRandom(), plan,
			source.Plan.NPCProfile.CriticalRating, e.runtime.program.Critical,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("boomerDetonationCommit: %w", err)
		}
		hitPackets, targetStatDelta, _, err := e.runtime.applyEnemyStatusDamage(
			&peerSession, e.generation, plan, result, e.timestamp,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("boomerDetonationDamage: %w", err)
		}
		packets = append(packets, hitPackets...)
		statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
	}
	binding := peerSession.binding
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	effectPacket, err := npcraknet.PositionedEffect(
		profile.ImpactEffectName, source.Plan.Position,
	)
	if err != nil {
		return nil, fmt.Errorf("boomerDetonationEffect: %w", err)
	}
	packets = append(packets, effectPacket)
	err = e.runtime.stats.Record(context.Background(), binding, statDelta)
	if err != nil {
		e.runtime.logger.Printf(
			"RakNet Lightning Juggernaut death detonation stats omitted object=%d: %v",
			e.objectID, err,
		)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) scheduleBoomerDeathDetonation(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) (bool, error) {
	if packet.ScheduleProducers == nil {
		return false, errors.New("death detonation scheduler unavailable")
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return false, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.BoomerDeathDetonationProfile(
		source.Plan.NounName,
	)
	r.registry.mutex.RUnlock()
	if !isSourceFound || !source.IsDefeated || !isProfileFound {
		return false, nil
	}
	schedule := campaignBoomerDeathSchedule{
		runtime: r, sessionKey: sessionKey, generation: generation,
		objectID:  objectID,
		timestamp: timestamp + uint64(profile.HitDelay.Milliseconds()),
	}
	cancel, err := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay: profile.HitDelay, Produce: schedule.detonate,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return false, fmt.Errorf("boomerDetonationSchedule: %w", err)
	}
	return true, nil
}
