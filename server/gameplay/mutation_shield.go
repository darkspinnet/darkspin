package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type mutationShieldReceipt struct {
	Phase            uint8
	Index            uint32
	Result           tcShieldAbsorption
	IsGenerated      bool
	IsDepleted       bool
	At               time.Time
	removal          *channelEffects
	creationPacket   []byte
	creationObjectID uint32
	creationSlot     uint8
	creationIndex    int
}

// Shield authority and its fallible presentation advance independently. The
// subtraction occurs only in phase3, even when a timer or encoder must retry.
func (e *gameplayPeerSession) absorbMutationShield(damage float32, at time.Time, pool *attachedEffectPool, receipt *mutationShieldReceipt, registry *gameplaySessionRegistry) (tcShieldAbsorption, error) {
	if receipt.Phase == 0 {
		receipt.Result, receipt.At, receipt.Index = tcShieldAbsorption{Damage: damage}, at, e.deployedCreatureIndex
		index := receipt.Index
		if damage <= 0 || at.IsZero() || index >= uint32(len(e.binding.Creatures)) || index >= uint32(len(e.tcShieldAmount)) ||
			e.binding.Creatures[index].PassiveAbility != util.HashID("TCShieldedSentinelPassive") {
			receipt.Phase = 7
			return receipt.Result, nil
		}
		if e.tcShieldAmount[index] <= 0 {
			readyAt := e.tcShieldReadyAt[index]
			if readyAt.IsZero() {
				e.startTCShieldRecharge(index, at)
				receipt.Phase = 7
				return receipt.Result, nil
			}
			if at.Before(readyAt) {
				receipt.Phase = 7
				return receipt.Result, nil
			}
			base := tcShieldBaseStrength
			if e.isOverdriveActiveAt(at) {
				base = 30
			}
			strength := base + (e.binding.Creatures[index].EnergyDefense-tcShieldDefenseOffset)*tcShieldDefenseScale
			if strength <= 0 {
				e.tcShieldReadyAt[index] = at.Add(e.tcShieldRechargeDurationAt(at))
				receipt.Phase = 7
				return receipt.Result, nil
			}
			e.tcShieldAmount[index], e.tcShieldReadyAt[index] = strength, time.Time{}
			receipt.IsGenerated = true
		}
		receipt.Phase = 1
	}
	index := receipt.Index
	if receipt.Phase == 1 {
		if receipt.IsGenerated {
			packet, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{Asset: util.HashID("cyber_shield_generate_shield.ServerEventDef"), ObjectID: e.deployedObjectID})
			if err != nil {
				return receipt.Result, fmt.Errorf("shieldGenerate: %w", err)
			}
			receipt.Result.Packets = append(receipt.Result.Packets, packet)
		}
		receipt.Phase = 2
	}
	if receipt.Phase == 2 {
		if receipt.IsGenerated {
			packet, err := e.attachTCShield(index, pool)
			if err != nil {
				return receipt.Result, fmt.Errorf("shieldAttach: %w", err)
			}
			if packet != nil {
				receipt.creationPacket = packet
				receipt.creationObjectID = e.deployedObjectID
				receipt.creationSlot = e.tcShieldEffectSlot[index]
				receipt.creationIndex = len(receipt.Result.Packets)
			}
		}
		receipt.Phase = 3
	}
	if receipt.Phase == 3 {
		absorbed := min(receipt.Result.Damage, e.tcShieldAmount[index])
		e.tcShieldAmount[index] -= absorbed
		receipt.Result.Damage -= absorbed
		receipt.Result.Absorbed = absorbed
		receipt.IsDepleted = e.tcShieldAmount[index] <= 0
		if receipt.IsDepleted {
			e.tcShieldAmount[index] = 0
			e.tcShieldReadyAt[index] = receipt.At.Add(e.tcShieldRechargeDurationAt(receipt.At))
		}
		receipt.Phase = 4
	}
	if receipt.Phase == 4 {
		packet, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{Asset: util.HashID("status_shield_hit.ServerEventDef"), ObjectID: e.deployedObjectID})
		if err != nil {
			return receipt.Result, fmt.Errorf("shieldHit: %w", err)
		}
		receipt.Result.Packets = append(receipt.Result.Packets, packet)
		receipt.Phase = 5
	}
	if receipt.Phase == 5 {
		if receipt.IsDepleted && e.isTCShieldEffectAttached[index] {
			if pool == nil {
				return receipt.Result, errors.New("mutation shield removal pool unavailable")
			}
			slot := e.tcShieldEffectSlot[index]
			if pool != nil {
				err := registry.retainMutationShieldRemovalLocked(e, pool, slot, receipt)
				if err != nil {
					return receipt.Result, fmt.Errorf("shieldRemoval: %w", err)
				}
			}
			if e.tcShieldEffectSlot[index] == slot {
				e.tcShieldEffectSlot[index], e.isTCShieldEffectAttached[index] = 0, false
			}
		}
		receipt.Phase = 6
	}
	if receipt.Phase == 6 {
		if receipt.IsDepleted && pool != nil {
			err := e.scheduleTCShieldRecharge()
			if err != nil {
				return receipt.Result, fmt.Errorf("shieldSchedule: %w", err)
			}
		}
		receipt.Phase = 7
	}
	result := receipt.Result
	if !receipt.IsDepleted && len(receipt.creationPacket) > 0 && e.deployedObjectID == receipt.creationObjectID &&
		e.isTCShieldEffectAttached[index] && e.tcShieldEffectSlot[index] == receipt.creationSlot {
		result.Packets = append([][]byte(nil), receipt.Result.Packets[:receipt.creationIndex]...)
		result.Packets = append(result.Packets, receipt.creationPacket)
		result.Packets = append(result.Packets, receipt.Result.Packets[receipt.creationIndex:]...)
	}
	return result, nil
}

// The caller holds registry.mutex. The channel delivery owner is reused for
// this exact depleted shield slot; it is never an active channel or replay.
func (e *gameplaySessionRegistry) retainMutationShieldRemovalLocked(member *gameplayPeerSession, pool *attachedEffectPool, slot uint8, receipt *mutationShieldReceipt) error {
	if member == nil {
		return errors.New("mutation shield removal member unavailable")
	}
	packet, err := npcraknet.ShieldEffectAsset(member.deployedObjectID, slot, "status_shielded.ServerEventDef", true)
	if err != nil {
		return fmt.Errorf("shieldRemoveEncode: %w", err)
	}
	lease, err := e.adoptMutationRemovalLocked(member, pool, member.deployedObjectID, slot, receipt.removal, [][]byte{packet})
	if err != nil {
		return fmt.Errorf("shieldRemoveAdopt: %w", err)
	}
	receipt.removal = lease
	return nil
}

// Already-encoded shield or forced-movement deletion enters the existing
// transport-owned removal FIFO. Reusing lease makes retry admission idempotent.
func (e *gameplaySessionRegistry) adoptMutationRemovalLocked(member *gameplayPeerSession, pool *attachedEffectPool, objectID uint32, slot uint8, lease *channelEffects, packets [][]byte) (*channelEffects, error) {
	if e == nil || member == nil || member.zone == nil || pool == nil {
		return lease, errors.New("mutation removal runtime unavailable")
	}
	if objectID == 0 || len(packets) == 0 {
		return lease, errors.New("mutation removal identity unavailable")
	}
	if lease == nil {
		isHeld := pool.HoldRemoval(objectID, slot)
		if !isHeld {
			return nil, errors.New("mutation removal hold unavailable")
		}
		lease = &channelEffects{world: member.zone, pool: pool,
			slots:       []channelEffectSlot{{objectID: objectID, slot: slot}},
			isPublished: true, isRetired: true}
		removal := &channelRemoval{effects: lease, packets: clonePendingPackets(packets)}
		for _, participant := range member.zone.Snapshot().Members {
			removal.deliveries = append(removal.deliveries, &channelDelivery{
				userID: participant.UserID, generation: participant.PeerGeneration})
		}
		e.channelRemovals = append(e.channelRemovals, removal)
	} else if lease.world != member.zone || lease.pool != pool || len(lease.slots) != 1 ||
		lease.slots[0].objectID != objectID || lease.slots[0].slot != slot {
		return lease, errors.New("mutation removal lease replaced")
	}
	member.channelRegistry = e
	memberKey := ""
	for key, current := range e.sessions {
		if current.zone == member.zone && current.binding.UserID == member.binding.UserID &&
			current.generation == member.generation && current.transportGeneration == member.transportGeneration {
			e.sessions[key] = *member
			memberKey = key
			break
		}
	}
	e.commitChannelRemovalsLocked()
	if memberKey != "" {
		*member = e.sessions[memberKey]
	}
	return lease, nil
}
