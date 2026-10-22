package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

// restartOverdriveEntryPassives follows native sub_9C34E0's removal and
// rank-three reapplication of the active hero's passive. Expiry is deliberately
// separate: this native dispatch has only been established for entry.
func (e gameplaySwitchRuntime) restartOverdriveEntryPassives(
	packet raknet.Packet, sessionKey string, generation uint64,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation || peerSession.deployedHitPoint() <= 0 ||
		peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) ||
		!peerSession.isOverdriveActiveAt(e.now()) {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	passiveID := peerSession.binding.Creatures[peerSession.deployedCreatureIndex].PassiveAbility
	packets := make([][]byte, 0)
	switch passiveID {
	case util.HashID("VoodooTempestPassive"):
		e.registry.voodooCharms.removeProvider(voodooActorIdentity(peerSession))
		e.registry.refreshVoodooCharmsLocked(e.now())
	case util.HashID("FieldMedicPassive"):
		objectID, err := peerSession.reserveCampaignProjectileIDs(1, 2000)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("overdriveDroneID: %w", err)
		}
		actorPackets, err := peerSession.spawnFieldMedicDrone(e.program, objectID)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("overdriveDroneSpawn: %w", err)
		}
		packets = append(packets, actorPackets...)
	case util.HashID("EnergySentinelPassive"):
		if peerSession.energySentinelPassive != nil {
			peerSession.energySentinelPassive.Stop()
			peerSession.energySentinelPassive = nil
		}
	case util.HashID("FireRavagerPassive"):
		peerSession.fireRavagerPassive = fireRavagerPassiveState{}
	case util.HashID("TCShieldedSentinelPassive"):
		stopPackets, err := peerSession.stopTCShield(peerSession.deployedCreatureIndex, e.effectPool)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("overdriveShieldStop: %w", err)
		}
		shieldPackets, err := peerSession.activateTCShield(
			peerSession.deployedCreatureIndex, e.now(), e.effectPool, e.registry,
		)
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("overdriveShieldStart: %w", err)
		}
		packets = append(packets, stopPackets...)
		packets = append(packets, shieldPackets...)
	case util.HashID("FireTempestPassive"):
		if peerSession.fireTempestPassive != nil {
			run := peerSession.fireTempestPassive
			removalPacket, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
				Slot: run.effectSlot + 1, IsRemovalRequested: true,
				IsHardStop: true, ObjectID: peerSession.deployedObjectID,
			})
			if err != nil {
				e.registry.mutex.Unlock()
				return nil, fmt.Errorf("overdriveFireRemove: %w", err)
			}
			packets = append(packets, removalPacket)
			run.Stop()
			peerSession.fireTempestPassive = nil
		}
	case util.HashID("TrapperStealthModifier"):
		stealthPackets, err := peerSession.breakTrapperStealth()
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("overdriveStealthStop: %w", err)
		}
		packets = append(packets, stealthPackets...)
	}
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()
	if passiveID == util.HashID("EnergySentinelPassive") {
		err := e.energyPassive.start(packet, sessionKey, generation)
		if err != nil {
			return nil, fmt.Errorf("overdriveEnergyPassive: %w", err)
		}
	}
	if passiveID == util.HashID("FireTempestPassive") {
		chargePacket, err := e.passive.start(packet, sessionKey, generation)
		if err != nil {
			return nil, fmt.Errorf("overdriveFireStart: %w", err)
		}
		if chargePacket != nil {
			packets = append(packets, chargePacket)
		}
	}
	if passiveID == util.HashID("TrapperStealthModifier") {
		err := startTrapperStealth(e.damage, packet, sessionKey, generation)
		if err != nil {
			return nil, fmt.Errorf("overdriveStealthStart: %w", err)
		}
	}
	return packets, nil
}
