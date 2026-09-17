package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e campaignNPCActionRuntime) produceNashiraCombatPanic(
	packet raknet.Packet, sessionKey string, generation uint64, objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	e.registry.mutex.RLock()
	session, isFound := e.registry.sessions[sessionKey]
	isNashira := false
	if isFound && session.isCampaignNPCSourceActive(generation, objectID) {
		source, isSourceFound := session.zone.NPCs().NPC(objectID)
		isNashira = isSourceFound && zonenpc.IsNashiraNoun(source.Plan.NounName)
	}
	e.registry.mutex.RUnlock()
	if !isNashira {
		return nil, false, nil
	}
	packets, isHandled, err := e.produceNashiraPanic(packet, sessionKey, generation, objectID, timestamp)
	if err != nil {
		return packets, isHandled, fmt.Errorf("combatPanic: %w", err)
	}
	return packets, isHandled, nil
}

type campaignNashiraFearStep struct {
	step    campaignNashiraPanicStep
	profile zonenpc.ActionProfile
}

func (e campaignNashiraFearStep) produce() ([][]byte, error) {
	runtime := e.step.runtime
	runtime.registry.mutex.RLock()
	session, isFound := runtime.registry.sessions[e.step.sessionKey]
	if !isFound || !session.isCampaignNPCSourceActive(e.step.generation, e.step.objectID) {
		runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	source, isSourceFound := session.zone.NPCs().NPC(e.step.objectID)
	if !isSourceFound || source.ActionGeneration != e.step.actionGeneration {
		runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	targets := append([]zone.NPCTarget(nil), session.zone.LiveNPCTargets()...)
	runtime.registry.mutex.RUnlock()
	packets := make([][]byte, 0)
	for _, target := range targets {
		if target.HitPoint <= 0 || !isCampaignLeapTarget(source.Plan.Position,
			target.Position, target.FootprintRadius, e.profile.Radius) {
			continue
		}
		fearPackets, err := runtime.applyCampaignNPCFear(e.step.packet,
			e.step.sessionKey, e.step.generation, source, target, e.profile, e.step.timestamp)
		if err != nil {
			return packets, fmt.Errorf("panicFear: %w", err)
		}
		packets = append(packets, fearPackets...)
	}
	return packets, nil
}
