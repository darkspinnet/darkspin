package gameplay

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type campaignPackCowerSchedule struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func (e campaignPackCowerSchedule) next() ([][]byte, error) {
	packets, err := e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.objectID, e.timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
		return nil, fmt.Errorf("packCowerNext: %w", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) producePackMeleeCower(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, objectID,
	)
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, false, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(objectID)
	isPackMelee := isSourceFound && campaignDifficultyNounFamily(
		source.Plan.NounName,
	) == "zelembasicpackmelee"
	if !isPackMelee || peerSession.zone.NPCs().HasLivingAlly(objectID, 20) {
		r.registry.mutex.RUnlock()
		return nil, false, nil
	}
	cowerDuration := 3 * time.Second
	if strings.Contains(strings.ToLower(source.Plan.NounName), "_2.noun") {
		cowerDuration = 2 * time.Second
	} else if strings.Contains(strings.ToLower(source.Plan.NounName), "_3.noun") {
		cowerDuration = time.Second
	}
	r.registry.mutex.RUnlock()
	profile := zonenpc.ActionProfile{
		Family: zonenpc.ActionMelee, AbilityName: "ZelemBasicPackMeleeCower",
		AnimationName: "zlm_minn_sp_01_cower", ReleaseDelay: cowerDuration,
		Cooldown: 24 * time.Second,
	}
	plan := zonenpc.AttackPlan{
		ActionGeneration: source.ActionGeneration,
		SourceObjectID:   objectID, TargetObjectID: objectID,
		SourcePosition: source.Plan.Position, TargetPosition: source.Plan.Position,
		Profile: profile,
	}
	packets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, true, fmt.Errorf("packCowerStart: %w", err)
	}
	schedule := campaignPackCowerSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
		timestamp: timestamp + uint64(profile.Cooldown/time.Millisecond),
	}
	_, err = packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: profile.Cooldown, Produce: schedule.next,
	}})
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, true, fmt.Errorf("packCowerSchedule: %w", err)
	}
	return packets, true, nil
}
