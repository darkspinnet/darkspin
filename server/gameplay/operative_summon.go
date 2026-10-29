package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type agentPetSummonStep struct {
	state            *agentSupportState
	actionGeneration uint64
	slotIndex        int
	timestamp        uint64
	nextDelay        time.Duration
}

func (e campaignNPCActionRuntime) produceAgentPetAction(packet raknet.Packet, sessionKey string, generation uint64, objectID uint32, timestamp uint64) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || !member.isCampaignNPCSourceActive(generation, objectID) {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(objectID)
	if !isEnemyFound {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	family := campaignDifficultyNounFamily(enemy.Plan.NounName)
	if family == "nomadfirepet" {
		e.registry.mutex.Unlock()
		packets, err := e.produceFirePet(packet, sessionKey, generation, objectID, timestamp)
		if err != nil {
			return nil, true, fmt.Errorf("firePetAction: %w", err)
		}
		return packets, true, nil
	}
	if family == "nomadlightningpet" {
		nearestDistance := float32(1e20)
		selectedObjectID := uint32(0)
		for _, target := range member.zone.LiveNPCTargets() {
			distance := enemy.Plan.Position.Sub(target.Position).Length()
			if target.IsHero && target.HitPoint > 0 && distance < nearestDistance {
				selectedObjectID = target.ObjectID
				nearestDistance = distance
			}
		}
		if selectedObjectID != 0 && selectedObjectID != enemy.TargetObjectID {
			target, isTargetFound := member.campaignNPCTarget(generation, selectedObjectID)
			if isTargetFound {
				replaced, isReplaced, retargetErr := member.zone.NPCs().Retarget(objectID, zonenpc.Target{ObjectID: target.ObjectID, Position: target.Position, FootprintRadius: target.ActorFootprintRadius, Faction: zonenpc.FactionPlayerAligned, Owner: zonenpc.ActionOwner{UserID: target.UserID, PeerGeneration: target.PeerGeneration}, IsAlive: true})
				if retargetErr != nil {
					e.registry.mutex.Unlock()
					return nil, true, fmt.Errorf("lightningPetRetarget: %w", retargetErr)
				}
				if isReplaced {
					enemy = replaced
				}
			}
		}
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	if family != "nomadpets" {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	state := member.agentSupports[objectID]
	if state == nil {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	if state.isSummoning {
		e.registry.mutex.Unlock()
		return nil, true, nil
	}
	slotIndex := -1
	for index := 0; index < 2; index++ {
		if index >= len(state.childObjectIDs) {
			slotIndex = index
			break
		}
		child, isChildFound := member.zone.NPCs().NPC(state.childObjectIDs[index])
		if !isChildFound || child.IsDefeated || child.HitPoint <= 0 {
			slotIndex = index
			break
		}
	}
	if slotIndex < 0 {
		// The melee condition excludes targets carrying LightningCage.
		selectedObjectID := uint32(0)
		for _, target := range member.zone.LiveNPCTargets() {
			if target.HitPoint > 0 && !hasAgentLightningCage(member, target.ObjectID) {
				selectedObjectID = target.ObjectID
				break
			}
		}
		if selectedObjectID != 0 && selectedObjectID != enemy.TargetObjectID {
			selected, isTargetFound := member.campaignNPCTarget(generation, selectedObjectID)
			if isTargetFound {
				replaced, isReplaced, retargetErr := member.zone.NPCs().Retarget(objectID, zonenpc.Target{ObjectID: selected.ObjectID, Position: selected.Position, FootprintRadius: selected.ActorFootprintRadius, Faction: zonenpc.FactionPlayerAligned, Owner: zonenpc.ActionOwner{UserID: selected.UserID, PeerGeneration: selected.PeerGeneration}, IsAlive: true})
				if retargetErr != nil {
					e.registry.mutex.Unlock()
					return nil, true, fmt.Errorf("petMeleeRetarget: %w", retargetErr)
				}
				if isReplaced {
					enemy = replaced
				}
			}
		}
		e.registry.mutex.Unlock()
		if selectedObjectID == 0 {
			step := agentPetSummonStep{state: state, actionGeneration: enemy.ActionGeneration, timestamp: timestamp, nextDelay: time.Second}
			cancel, err := scheduleNPCProducers(e.registry, packet, []raknet.ScheduledPacketProducer{{Delay: time.Second, Produce: step.next}})
			if err == nil && cancel == nil {
				err = errors.New("pet idle continuation unavailable")
			}
			if err != nil {
				return nil, true, fmt.Errorf("petIdleSchedule: %w", err)
			}
			return nil, true, nil
		}
		return nil, false, nil
	}
	state.isSummoning = true
	state.summonSlot = slotIndex
	state.timestamp = timestamp + 1133
	e.registry.mutex.Unlock()
	step := agentPetSummonStep{state: state, actionGeneration: enemy.ActionGeneration, slotIndex: slotIndex, timestamp: timestamp, nextDelay: 2266666889 * time.Nanosecond}
	animationNames := [...]string{"nomad_lieu_el_3_lightningpet_summon", "nomad_lieu_el_3_firepet_summon"}
	animation, err := npcraknet.AnimationState(objectID, animationNames[slotIndex], timestamp)
	if err != nil {
		return step.failAdmission("petSummonAnimation", err)
	}

	cancel, err := scheduleNPCProducers(e.registry, packet, []raknet.ScheduledPacketProducer{{Delay: 1133333325 * time.Nanosecond, Produce: step.hit}, {Delay: 2266666889 * time.Nanosecond, Produce: step.next}})
	if err == nil && cancel == nil {
		err = errors.New("pet summon continuation unavailable")
	}
	if err != nil {
		e.registry.mutex.Lock()
		state.isSummoning = false
		e.registry.mutex.Unlock()
		e.releaseActionGeneration(sessionKey, generation, objectID, enemy.ActionGeneration)
		return nil, true, fmt.Errorf("petSummonSchedule: %w", err)
	}
	return [][]byte{animation}, true, nil
}

func (e agentPetSummonStep) hit() ([][]byte, error) {
	e.state.runtime.registry.mutex.RLock()
	member, isFound := e.state.runtime.registry.sessions[e.state.sessionKey]
	isCurrent := isFound && member.agentSupports[e.state.ownerObjectID] == e.state && member.isCampaignNPCSourceGenerationActive(e.state.generation, e.state.ownerObjectID, e.actionGeneration)
	e.state.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.state.tick()
	if err != nil {
		return e.fail("petSummonHit", err)
	}
	return packets, nil
}

func (e agentPetSummonStep) next() ([][]byte, error) {
	e.state.runtime.registry.mutex.Lock()
	member, isFound := e.state.runtime.registry.sessions[e.state.sessionKey]
	isCurrent := isFound && member.agentSupports[e.state.ownerObjectID] == e.state && member.isCampaignNPCSourceGenerationActive(e.state.generation, e.state.ownerObjectID, e.actionGeneration)
	e.state.isSummoning = false
	e.state.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	packets, err := e.state.runtime.produceEnemyMelee(e.state.packet, e.state.sessionKey, e.state.generation, e.state.ownerObjectID, e.timestamp+uint64(e.nextDelay/time.Millisecond))
	if err != nil {
		return e.fail("petSummonNext", err)
	}
	return packets, nil
}

func hasAgentLightningCage(member gameplayPeerSession, objectID uint32) bool {
	for _, modifier := range member.zone.Effect().Snapshot() {
		if modifier.TargetObjectID == objectID && modifier.GUID == util.HashID("LightningCage") {
			return true
		}
	}
	return false
}

func (e agentPetSummonStep) failAdmission(step string, cause error) ([][]byte, bool, error) {
	packets, err := e.fail(step, cause)
	if err != nil {
		return packets, true, fmt.Errorf("petSummonAdmission: %w", err)
	}
	return packets, true, nil
}

func (e agentPetSummonStep) fail(step string, cause error) ([][]byte, error) {
	e.state.runtime.registry.mutex.Lock()
	e.state.isSummoning = false
	e.state.runtime.registry.mutex.Unlock()
	e.state.runtime.releaseActionGeneration(e.state.sessionKey, e.state.generation, e.state.ownerObjectID, e.actionGeneration)
	return nil, fmt.Errorf("%s: %w", step, cause)
}
