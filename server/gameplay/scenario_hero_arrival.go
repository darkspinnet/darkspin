//go:build scenario

package gameplay

import (
	"fmt"
	"log"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Take the ordinary member lock without blocking a world poll. Each starter
// takes the registry lock itself, so retain the fixture identity across calls.
func (e gameplayPendingRuntime) pollScenarioHeroArrival(packet raknet.Packet) ([][]byte, error) {
	if packet.Address == nil {
		return nil, nil
	}
	sessionKey := packet.Address.String()
	e.registry.mutex.RLock()
	observed, isObserved := e.registry.sessions[sessionKey]
	e.registry.mutex.RUnlock()
	if !isObserved || !observed.scenarioAbility.isArrivalPending {
		return nil, nil
	}
	memberKey := gameplaySessionMemberKey(observed)
	memberMutex := e.registry.memberMutex(memberKey)
	if !memberMutex.TryLock() {
		return nil, nil
	}
	defer memberMutex.Unlock()
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || gameplaySessionMemberKey(member) != memberKey || !member.scenarioAbility.isArrivalPending || member.zone == nil ||
		member.binding.Mode == game.ModeArena || member.isRejoinPending || member.isZoneTerminal() ||
		!member.dungeonSetup.IsCommitted() || member.scenarioAbility.retirement != nil || member.deployedHitPoint() <= 0 ||
		member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) ||
		(packet.TransportGeneration != 0 && packet.TransportGeneration != member.transportGeneration) {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	original := member
	plans := append([]zonenpc.SpawnPlan(nil), member.scenarioAbility.npcPlans...)
	member.scenarioAbility.isArrivalPending = false
	member.scenarioAbility.npcPlans = nil
	packets, droneErr := member.syncFieldMedicDrones(e.action.switcher.program, e.now())
	e.registry.sessions[sessionKey] = member
	acquired, targetErr := member.zone.RefreshNPCTargets()
	planIndexes := make(map[uint32]int, len(plans))
	for index, plan := range plans {
		planIndexes[plan.ObjectID] = index
	}
	for _, npc := range acquired {
		index, isExisting := planIndexes[npc.Plan.ObjectID]
		if isExisting {
			plans[index] = npc.Plan
			continue
		}
		planIndexes[npc.Plan.ObjectID] = len(plans)
		plans = append(plans, npc.Plan)
	}
	e.registry.mutex.Unlock()
	e.logScenarioHeroArrivalError("fieldMedic", droneErr)
	e.logScenarioHeroArrivalError("targets", targetErr)
	if !e.isScenarioHeroArrivalCurrent(sessionKey, original) {
		return nil, nil
	}
	sagePackets, sageErr := e.setup.startSagePassive(packet, sessionKey, original.generation, e.now())
	packets = append(packets, sagePackets...)
	e.logScenarioHeroArrivalError("sage", sageErr)
	if !e.isScenarioHeroArrivalCurrent(sessionKey, original) {
		return nil, nil
	}
	firePacket, fireErr := e.action.switcher.passive.start(packet, sessionKey, original.generation)
	if firePacket != nil {
		packets = append(packets, firePacket)
	}
	e.logScenarioHeroArrivalError("fire", fireErr)
	if !e.isScenarioHeroArrivalCurrent(sessionKey, original) {
		return nil, nil
	}
	energyErr := e.action.switcher.energyPassive.start(packet, sessionKey, original.generation)
	e.logScenarioHeroArrivalError("energy", energyErr)
	if !e.isScenarioHeroArrivalCurrent(sessionKey, original) {
		return nil, nil
	}
	trapperErr := startTrapperStealth(e.action.switcher.damage, packet, sessionKey, original.generation)
	e.logScenarioHeroArrivalError("trapper", trapperErr)
	if !e.isScenarioHeroArrivalCurrent(sessionKey, original) {
		return nil, nil
	}
	lightspeedPackets, lightspeedErr := startLightspeedPassive(e.action.switcher.damage, packet, sessionKey, original.generation)
	packets = append(packets, lightspeedPackets...)
	e.logScenarioHeroArrivalError("lightspeed", lightspeedErr)
	if !e.isScenarioHeroArrivalCurrent(sessionKey, original) {
		return nil, nil
	}
	soulPackets, soulErr := startSoulRavagerPresentation(e.action.switcher.damage, sessionKey, original.generation)
	packets = append(packets, soulPackets...)
	e.logScenarioHeroArrivalError("soul", soulErr)
	if !e.isScenarioHeroArrivalCurrent(sessionKey, original) {
		return nil, nil
	}
	npcPackets, npcErr := e.action.switcher.npc.scheduleFirstActions(packet, sessionKey, original.generation, plans, packet.SourceTime)
	packets = append(packets, npcPackets...)
	e.logScenarioHeroArrivalError("npc", npcErr)
	companionPackets, companionErr := e.action.switcher.damage.startCompanionAttacks(packet, sessionKey, original.generation, packet.SourceTime)
	packets = append(packets, companionPackets...)
	e.logScenarioHeroArrivalError("companions", companionErr)
	if !e.isScenarioHeroArrivalCurrent(sessionKey, original) {
		return nil, nil
	}
	err := publishCampaignPeersAfterCommit(e.registry, packet, packets)
	if err != nil {
		return nil, fmt.Errorf("fixtureArrivalPublish: %w", err)
	}
	return packets, nil
}

func (e gameplayPendingRuntime) isScenarioHeroArrivalCurrent(sessionKey string, original gameplayPeerSession) bool {
	e.registry.mutex.RLock()
	defer e.registry.mutex.RUnlock()
	member, isFound := e.registry.sessions[sessionKey]
	return isFound && member.binding.UserID == original.binding.UserID && member.binding.GameID == original.binding.GameID &&
		member.zone == original.zone && member.generation == original.generation &&
		member.transportGeneration == original.transportGeneration && member.squad == original.squad &&
		member.deployedObjectID == original.deployedObjectID && member.deployedCreatureIndex == original.deployedCreatureIndex &&
		member.deployedCreatureIndex < uint32(len(member.binding.Creatures)) &&
		member.binding.Creatures[member.deployedCreatureIndex].Noun == original.binding.Creatures[original.deployedCreatureIndex].Noun &&
		member.scenarioAbility.retirement == nil && !member.isRejoinPending && !member.isZoneTerminal() && member.deployedHitPoint() > 0
}

func (e gameplayPendingRuntime) logScenarioHeroArrivalError(step string, err error) {
	if err == nil {
		return
	}
	if e.logger != nil {
		e.logger.Printf("Scenario hero arrival %s omitted: %v", step, err)
		return
	}
	log.Printf("Scenario hero arrival %s omitted: %v", step, err)
}
