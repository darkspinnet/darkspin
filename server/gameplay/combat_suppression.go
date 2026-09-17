package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const campaignSuppressionTick = 500 * time.Millisecond

type campaignSuppressionRun struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
	timestamp  uint64
}

func isCampaignSuppressionFamily(nounName string) bool {
	family := campaignDifficultyNounFamily(nounName)
	return family == "citadelspecialtwo" ||
		family == "citadelspecialtwo_captain"
}

func (e *campaignSuppressionRun) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && peerSession.campaignNPCSuppressionRuns[e.objectID] == e {
		delete(peerSession.campaignNPCSuppressionRuns, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e *campaignSuppressionRun) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.objectID,
	) && peerSession.campaignNPCSuppressionRuns[e.objectID] == e
	if !isCurrent {
		if isFound && peerSession.campaignNPCSuppressionRuns[e.objectID] == e {
			delete(peerSession.campaignNPCSuppressionRuns, e.objectID)
			e.runtime.registry.sessions[e.sessionKey] = peerSession
		}
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	if !isSourceFound || !isCampaignSuppressionFamily(source.Plan.NounName) {
		delete(peerSession.campaignNPCSuppressionRuns, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	profile := zonenpc.ActionProfile{
		ModifierName: "SilenceModifier", ModifierDuration: time.Second,
	}
	plans := make([]zonenpc.AttackPlan, 0)
	for _, target := range peerSession.zone.LiveNPCTargets() {
		if zonegeometry.Distance(source.Plan.Position, target.Position) > 10 {
			continue
		}
		plans = append(plans, zonenpc.AttackPlan{
			SourceObjectID: e.objectID, TargetObjectID: target.ObjectID,
			SourcePosition: source.Plan.Position, TargetPosition: target.Position,
			Profile: profile,
		})
	}
	e.runtime.registry.mutex.Unlock()
	packets := make([][]byte, 0, len(plans))
	for _, plan := range plans {
		modifierPackets, err := e.runtime.applyCampaignNPCSilence(
			e.packet, e.sessionKey, e.generation, plan, e.timestamp,
		)
		if err != nil {
			return e.fail("suppressionApply", err)
		}
		packets = append(packets, modifierPackets...)
	}
	e.timestamp += uint64(campaignSuppressionTick / time.Millisecond)
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: campaignSuppressionTick, Produce: e.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return e.fail("suppressionSchedule", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) startCampaignSuppressionAura(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) error {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, objectID,
	)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(objectID)
	if !isSourceFound || !isCampaignSuppressionFamily(source.Plan.NounName) {
		r.registry.mutex.Unlock()
		return nil
	}
	if peerSession.campaignNPCSuppressionRuns == nil {
		peerSession.campaignNPCSuppressionRuns = make(
			map[uint32]*campaignSuppressionRun,
		)
	}
	if peerSession.campaignNPCSuppressionRuns[objectID] != nil {
		r.registry.mutex.Unlock()
		return nil
	}
	run := &campaignSuppressionRun{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
	}
	peerSession.campaignNPCSuppressionRuns[objectID] = run
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: time.Millisecond, Produce: run.produce,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err == nil {
		return nil
	}
	r.registry.mutex.Lock()
	latest, isLatestFound := r.registry.sessions[sessionKey]
	if isLatestFound && latest.campaignNPCSuppressionRuns[objectID] == run {
		delete(latest.campaignNPCSuppressionRuns, objectID)
		r.registry.sessions[sessionKey] = latest
	}
	r.registry.mutex.Unlock()
	return fmt.Errorf("suppressionStartSchedule: %w", err)
}
