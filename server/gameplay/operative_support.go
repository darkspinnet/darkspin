package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonedifficulty "github.com/darkspinnet/darkspin/server/zone/difficulty"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

// Agent support belongs to the actual authored summoner, not the director pool.
// Chunk690 creates CyberOne's unattackable drone; chunk24 owns two pet slots.
type agentSupportState struct {
	runtime                campaignNPCActionRuntime
	packet                 raknet.Packet
	sessionKey             string
	generation             uint64
	ownerObjectID          uint32
	timestamp              uint64
	childObjectIDs         []uint32
	isDrone                bool
	summonSlot             int
	isSummoning            bool
	positionReadyTimestamp uint64
	passiveEffects         *channelEffects
}

func (e campaignNPCActionRuntime) startAgentSupport(packet raknet.Packet, sessionKey string, generation uint64, plans []zonenpc.SpawnPlan, timestamp uint64) ([][]byte, error) {
	var packets [][]byte
	for _, plan := range plans {
		family := campaignDifficultyNounFamily(plan.NounName)
		if family == "nomadspacetimeagent" {
			e.registry.mutex.Lock()
			member, isFound := e.registry.sessions[sessionKey]
			if isFound && member.generation == generation && member.zone != nil {
				if member.agentSupports == nil {
					member.agentSupports = make(map[uint32]*agentSupportState)
				}
				if member.agentSupports[plan.ObjectID] == nil {
					member.agentSupports[plan.ObjectID] = &agentSupportState{ownerObjectID: plan.ObjectID, generation: generation}
				}
				currentPackets, err := member.agentSupports[plan.ObjectID].attachPassiveEffects(e.registry, sessionKey, &member, e.effectPool, true)
				e.registry.sessions[sessionKey] = member
				e.registry.mutex.Unlock()
				if err != nil {
					return packets, fmt.Errorf("spacetimePassive: %w", err)
				}
				packets = append(packets, currentPackets...)
			} else {
				e.registry.mutex.Unlock()
			}
			continue
		}
		if family == "nocturnaspecialhaunt" {
			e.registry.mutex.Lock()
			member, isFound := e.registry.sessions[sessionKey]
			if isFound && member.generation == generation && member.zone != nil && member.agentSupports[plan.ObjectID] == nil {
				isInitialized, err := member.zone.NPCs().InitializeHauntStealth(plan.ObjectID)
				if err != nil {
					e.registry.mutex.Unlock()
					return nil, fmt.Errorf("hauntStartupStealth: %w", err)
				}
				statePacket, err := npcraknet.StealthState(plan.ObjectID, 2)
				if err != nil {
					e.registry.mutex.Unlock()
					return nil, fmt.Errorf("hauntStartupMarshal: %w", err)
				}
				if member.agentSupports == nil {
					member.agentSupports = make(map[uint32]*agentSupportState)
				}
				member.agentSupports[plan.ObjectID] = &agentSupportState{ownerObjectID: plan.ObjectID, generation: generation}
				currentPackets, passiveErr := member.agentSupports[plan.ObjectID].attachPassiveEffects(e.registry, sessionKey, &member, e.effectPool, false)
				e.registry.sessions[sessionKey] = member
				if passiveErr != nil {
					e.registry.mutex.Unlock()
					return packets, fmt.Errorf("hauntPassive: %w", passiveErr)
				}
				packets = append(packets, currentPackets...)
				if isInitialized {
					packets = append(packets, statePacket)
				}
			}
			e.registry.mutex.Unlock()
			continue
		}
		if family != "nomadcyberone" && family != "nomadpets" && family != "nomadspecialfour" {
			continue
		}
		e.registry.mutex.Lock()
		member, isFound := e.registry.sessions[sessionKey]
		if !isFound || member.generation != generation || member.zone == nil || member.agentSupports[plan.ObjectID] != nil {
			e.registry.mutex.Unlock()
			continue
		}
		state := &agentSupportState{runtime: e, packet: packet, sessionKey: sessionKey, generation: generation, ownerObjectID: plan.ObjectID, timestamp: timestamp, isDrone: family == "nomadcyberone"}
		if member.agentSupports == nil {
			member.agentSupports = make(map[uint32]*agentSupportState)
		}
		if family != "nomadspecialfour" {
			state.recoverChildrenLocked(member)
		}
		member.agentSupports[plan.ObjectID] = state
		currentPackets, passiveErr := state.attachPassiveEffects(e.registry, sessionKey, &member, e.effectPool, false)
		e.registry.sessions[sessionKey] = member
		e.registry.mutex.Unlock()
		if passiveErr != nil {
			return packets, fmt.Errorf("agentPassive: %w", passiveErr)
		}
		packets = append(packets, currentPackets...)
		if !state.isDrone {
			continue
		}
		supportPackets, err := state.tick()
		if err != nil {
			return packets, fmt.Errorf("agentSupportStart: %w", err)
		}
		packets = append(packets, supportPackets...)
	}
	return packets, nil
}

func (e *agentSupportState) tick() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.zone == nil || member.agentSupports[e.ownerObjectID] != e || member.isZoneTerminal() {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	owner, isOwnerFound := member.zone.NPCs().NPC(e.ownerObjectID)
	if !isOwnerFound || owner.IsDefeated || owner.HitPoint <= 0 {
		passiveErr := member.retireAgentPassiveEffectsLocked(e.ownerObjectID)
		if passiveErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("agentDeathPassive: %w", passiveErr)
		}
		delete(member.agentSupports, e.ownerObjectID)
		e.runtime.registry.sessions[e.sessionKey] = member
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	var packets [][]byte
	var spawnedPlans []zonenpc.SpawnPlan
	nounNames := []string{"NomadLightningPet.Noun", "NomadFirePet.Noun"}
	if e.isDrone {
		nounNames = []string{"NomadCyberOne_Droid.Noun"}
	}
	for slotIndex, nounName := range nounNames {
		if !e.isDrone && slotIndex != e.summonSlot {
			continue
		}
		if slotIndex < len(e.childObjectIDs) && e.childObjectIDs[slotIndex] != 0 {
			child, isChildFound := member.zone.NPCs().NPC(e.childObjectIDs[slotIndex])
			if isChildFound && !child.IsDefeated && child.HitPoint > 0 {
				continue
			}
			// CyberOne's unique passive creates one drone, not a replacement loop.
			if e.isDrone {
				continue
			}
		}
		npcProfile, err := zonedifficulty.ProjectOwnedSummon(member.zone.DirectorDefinition(), nounName)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("agentChildProfile: %w", err)
		}
		if e.isDrone {
			npcProfile.IsTargetable = false
		}

		objectID, err := member.reserveCampaignObjectID()
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("agentChildReserve: %w", err)
		}
		position := owner.Plan.Position
		if !e.isDrone {
			position = position.Add(owner.Facing.Scale(owner.Plan.ActorFootprintRadius() + 4))
		}
		projected, isProjected, projectionErr := zoneaction.NPCProjectPosition(member.zone.Navigation(), position, max(npcProfile.FootprintRadius, float32(0.1)))
		if projectionErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("agentChildPosition: %w", projectionErr)
		}
		if isProjected {
			position = projected
		}
		actionProfile := zonenpc.AgentChildProfile(nounName, npcProfile.NPCRank)
		plan := zonenpc.SpawnPlan{ObjectID: objectID, OwnerObjectID: e.ownerObjectID, NounName: nounName, Position: position, LocusID: owner.Plan.LocusID, MarkerSetName: owner.Plan.MarkerSetName, IsEncounterAuxiliary: true, IsOwnedDeathPersistent: !e.isDrone && zonenpc.EffectivePassiveRank(owner.Plan) >= 2, IsLootSuppressed: true, IsExperienceSuppressed: true, NPCProfile: npcProfile, ActionProfile: actionProfile, IsActionKnown: true}
		targetObjectID := owner.TargetObjectID
		if e.isDrone {
			// This passive child heals GetOwnerID; it has no hostile action.
			targetObjectID = 0
		}
		if e.isDrone {
			err = member.zone.NPCs().AddDormant([]zonenpc.SpawnPlan{plan})
		} else {
			err = member.zone.NPCs().Add([]zonenpc.SpawnPlan{plan}, targetObjectID)
		}
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("agentChildAdd: %w", err)
		}
		if !e.isDrone {
			mirrored, mirrorErr := member.zone.NPCs().AttachAttributeMirror(objectID, e.ownerObjectID, member.zone.DirectorDefinition().AttributeMirrorTuning)
			if mirrorErr != nil {
				rollbackErr := member.zone.NPCs().RollbackAdd([]zonenpc.SpawnPlan{plan})
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("agentChildMirror: %w", errors.Join(mirrorErr, rollbackErr))
			}
			plan = mirrored.Plan
		}
		var childPackets [][]byte
		var marshalErr error
		if e.isDrone {
			childPackets, marshalErr = npcraknet.DormantSpawns([]zonenpc.SpawnPlan{plan})
		} else {
			childPackets, marshalErr = npcraknet.TargetedSpawn(plan, targetObjectID)
		}
		if marshalErr != nil {
			rollbackErr := member.zone.NPCs().RollbackAdd([]zonenpc.SpawnPlan{plan})
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("agentChildMarshal: %w", errors.Join(marshalErr, rollbackErr))
		}
		err = member.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{Plans: []zonenpc.SpawnPlan{plan}, TargetObjectID: targetObjectID, IsDormant: e.isDrone}, member.binding.UserID, member.generation)
		if err != nil {
			rollbackErr := member.zone.NPCs().RollbackAdd([]zonenpc.SpawnPlan{plan})
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("agentChildPublish: %w", errors.Join(err, rollbackErr))
		}
		for len(e.childObjectIDs) <= slotIndex {
			e.childObjectIDs = append(e.childObjectIDs, 0)
		}
		e.childObjectIDs[slotIndex] = objectID
		packets = append(packets, childPackets...)
		if !e.isDrone {
			spawnAnimationNames := [...]string{"nomad_lieu_el_3_lightningpet_spawn", "nomad_lieu_el_3_firepet_spawn"}
			spawnEffectNames := [...]string{"nomad_lieu_el_3_lightningPet_appear.ServerEventDef", "nomad_lieu_el_3_firePet_appear.ServerEventDef"}
			animationPacket, animationErr := npcraknet.AnimationState(objectID, spawnAnimationNames[slotIndex], e.timestamp)
			if animationErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("agentChildAnimation: %w", animationErr)
			}
			effectPacket, effectErr := raknet.MarshalApplication(raknet.ObjectEffectMessage{Asset: util.HashID(spawnEffectNames[slotIndex]), ObjectID: objectID, AttackerID: e.ownerObjectID})
			if effectErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("agentChildEffect: %w", effectErr)
			}
			packets = append(packets, animationPacket, effectPacket)
		}
		if !e.isDrone {
			spawnedPlans = append(spawnedPlans, plan)
		}
	}
	if e.isDrone && len(e.childObjectIDs) > 0 && owner.HitPoint < owner.Plan.NPCProfile.HitPoint {
		child, isChildFound := member.zone.NPCs().NPC(e.childObjectIDs[0])
		if isChildFound && !child.IsDefeated && child.Plan.OwnerObjectID == e.ownerObjectID {
			healed, amount, err := member.zone.NPCs().Heal(e.ownerObjectID, 3)
			if err != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("agentDroneHeal: %w", err)
			}
			if amount > 0 {
				healPackets, err := npcraknet.HealDelta(e.ownerObjectID, healed, amount)
				if err != nil {
					e.runtime.registry.mutex.Unlock()
					return nil, fmt.Errorf("agentDroneHealMarshal: %w", err)
				}
				packets = append(packets, healPackets...)
				effectPacket, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{Asset: util.HashID("ctd_lieu_tc_3_droidHealShot_hit.ServerEventDef"), ObjectID: e.ownerObjectID, AttackerID: child.Plan.ObjectID})
				if err != nil {
					e.runtime.registry.mutex.Unlock()
					return nil, fmt.Errorf("agentDroneHealEffect: %w", err)
				}
				packets = append(packets, effectPacket)
				sourcePacket, sourceErr := raknet.MarshalApplication(raknet.ObjectEffectMessage{Asset: util.HashID("ctd_lieu_tc_3_droidHealShot.ServerEventDef"), ObjectID: child.Plan.ObjectID, AttackerID: e.ownerObjectID})
				if sourceErr != nil {
					e.runtime.registry.mutex.Unlock()
					return nil, fmt.Errorf("agentDroneHealSource: %w", sourceErr)
				}
				packets = append(packets, sourcePacket)
			}
		}
	}
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	if len(spawnedPlans) > 0 {
		actionPackets, err := e.runtime.scheduleFirstActions(e.packet, e.sessionKey, e.generation, spawnedPlans, e.timestamp)
		if err != nil {
			return nil, fmt.Errorf("agentChildActions: %w", err)
		}
		packets = append(packets, actionPackets...)
	}
	if !e.isDrone {
		return packets, nil
	}
	e.timestamp += 1000
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{Delay: time.Second, Produce: e.tick}})
	if err == nil && cancel == nil {
		err = errors.New("agent support continuation unavailable")
	}
	if err != nil {
		return nil, fmt.Errorf("agentSupportSchedule: %w", err)
	}
	return packets, nil
}

func (e *agentSupportState) recoverChildrenLocked(member gameplayPeerSession) {
	names := []string{"nomadlightningpet", "nomadfirepet"}
	if e.isDrone {
		names = []string{"nomadcyberone_droid"}
	}
	e.childObjectIDs = make([]uint32, len(names))
	for _, child := range member.zone.NPCs().Snapshots() {
		if child.Plan.OwnerObjectID != e.ownerObjectID {
			continue
		}
		family := campaignDifficultyNounFamily(child.Plan.NounName)
		for index, name := range names {
			if family != name {
				continue
			}
			previousID := e.childObjectIDs[index]
			if previousID == 0 {
				e.childObjectIDs[index] = child.Plan.ObjectID
				continue
			}
			previous, isPreviousFound := member.zone.NPCs().NPC(previousID)
			if !isPreviousFound || (previous.IsDefeated && !child.IsDefeated) {
				e.childObjectIDs[index] = child.Plan.ObjectID
			}
		}
	}
}
