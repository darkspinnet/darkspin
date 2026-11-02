package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

type agentChannelEffect struct {
	objectID uint32
	slot     uint8
	pool     *attachedEffectPool
}

func (e operativeCageSchedule) attachSupportEffects() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if len(e.state.effects) > 0 {
		return nil, nil
	}
	profile := e.state.plan.Profile
	objectIDs := []uint32{e.state.plan.SourceObjectID}
	names := []string{}
	switch profile.AbilityName {
	case "FireStream":
		names = []string{"nomad_lieu_el_3_firePet_flamethrower_effect.ServerEventDef"}
	case "ChannelLightningCage":
		names = []string{"nomad_lieu_el_3_lightningPet_beam_effect.ServerEventDef", "nomad_lieu_el_3_lightningPet_attack_muzzle_effect.ServerEventDef", "nomad_lieu_el_3_lightningPet_attack_hit_effect.ServerEventDef"}
		objectIDs = []uint32{e.state.plan.SourceObjectID, e.state.plan.SourceObjectID, e.state.plan.TargetObjectID}
	case "NocturnaSpecialHauntLockdown":
		names = []string{"shadow_agent_lockdown_web_cage_effect.ServerEventDef"}
		objectIDs = []uint32{e.state.plan.TargetObjectID}
	default:
		return nil, nil
	}
	var packets [][]byte
	for index, name := range names {
		slot, isAllocated := e.runtime.effectPool.Allocate(objectIDs[index])
		if !isAllocated {
			return nil, fmt.Errorf("agentEffectSlot: unavailable")
		}
		packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{Slot: slot + 1, IsForceAttached: true, Asset: util.HashID(name), ObjectID: objectIDs[index]})
		if err != nil {
			e.runtime.effectPool.Release(objectIDs[index], slot)
			return nil, fmt.Errorf("agentEffectAttach: %w", err)
		}
		e.state.effects = append(e.state.effects, agentChannelEffect{objectID: objectIDs[index], slot: slot, pool: e.runtime.effectPool})
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *operativeCageState) retireSupportEffects() ([][]byte, error) {
	var packets [][]byte
	for len(e.effects) > 0 {
		effect := e.effects[len(e.effects)-1]
		packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{Slot: effect.slot + 1, ObjectID: effect.objectID, IsRemovalRequested: true, IsHardStop: true})
		if err != nil {
			return packets, fmt.Errorf("agentEffectRemove: %w", err)
		}
		isReleased := effect.pool.Release(effect.objectID, effect.slot)
		e.effects = e.effects[:len(e.effects)-1]
		if isReleased {
			packets = append(packets, packet)
		}
	}
	return packets, nil
}
