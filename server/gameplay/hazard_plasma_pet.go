package gameplay

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type petPlasmaBurn struct {
	sourceObjectID uint32
	readyAt        time.Time
	stopPacket     []byte
	run            *campaignNPCModifierRun
	presentation   *petPlasmaPresentation
}

type petPlasmaPresentation struct {
	pool         *attachedEffectPool
	modifierPool *modifierPool
	burns        map[uint32]petPlasmaBurn
	modifiers    map[uint32]*campaignNPCModifierRun
	objectID     uint32
	slot         uint8
	run          *campaignNPCModifierRun
	isStopped    bool
}

func (e *petPlasmaPresentation) stop() {
	if e == nil || e.isStopped {
		return
	}
	e.isStopped = true
	e.pool.Release(e.objectID, e.slot)
	isCreated, err := e.run.release(e.modifierPool)
	if err != nil {
		log.Printf("Pet plasma modifier cleanup failed object=%d: %v", e.objectID, err)
	} else if isCreated {
		// Teardown can cancel a live modifier without an outgoing packet.
		// The companion object is retired by the same transition.
	}
	if err == nil && e.modifiers[e.run.instanceID] == e.run {
		delete(e.modifiers, e.run.instanceID)
	}
	if burn, isFound := e.burns[e.objectID]; isFound && burn.run == e.run {
		delete(e.burns, e.objectID)
	}
}

// The authored plasma aura selects any hostile combatant, including pets.
// Keep each companion's burn tied to its actual position and aura source.
func (e *gameplayPeerSession) pollPetPlasmaBurns(
	runtime campaignNPCActionRuntime, timestamp uint64, now time.Time,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	packets := make([][]byte, 0)
	stats := sporenet.PlayerStatDelta{}
	if e == nil || e.zone == nil || e.zone.Companion() == nil {
		return packets, stats, nil
	}
	if e.petPlasmaBurns == nil {
		e.petPlasmaBurns = make(map[uint32]petPlasmaBurn)
	}
	seenObjectIDs := make(map[uint32]bool)
	for _, pet := range e.zone.Companion().Snapshots() {
		if pet.UserID != e.binding.UserID || pet.PeerGeneration != e.generation ||
			!pet.IsCombatant || !pet.IsTargetable || pet.HitPoint <= 0 {
			continue
		}
		seenObjectIDs[pet.ObjectID] = true
		pool, isContact := plasmaPoolContact(e, pet.Position)
		burn, isBurning := e.petPlasmaBurns[pet.ObjectID]
		if isBurning && !burn.run.isActive() {
			effectPackets, err := e.endPetPlasmaBurn(runtime, pet.ObjectID, burn)
			if err != nil {
				return nil, stats, fmt.Errorf("petBurnStale: %w", err)
			}
			packets = append(packets, effectPackets...)
			isBurning = false
		}
		if isBurning && (!isContact || burn.sourceObjectID != pool.MarkerID) {
			effectPackets, err := e.endPetPlasmaBurn(runtime, pet.ObjectID, burn)
			if err != nil {
				return nil, stats, fmt.Errorf("petBurnExit: %w", err)
			}
			packets = append(packets, effectPackets...)
			isBurning = false
		}
		if !isContact {
			continue
		}
		if !isBurning {
			run, err := newCampaignNPCModifierRun(runtime.modifierPool)
			if err != nil {
				return nil, stats, fmt.Errorf("petBurnReserve: %w", err)
			}
			run.record = zoneeffect.Modifier{
				InstanceID: run.instanceID, GUID: util.HashID("CitadelPlasmaBurn"),
				SourceObjectID: pool.MarkerID, TargetObjectID: pet.ObjectID,
				Rank: 1, Kind: zoneeffect.ModifierKindDebuff, StackCount: 1,
			}
			startPacket, stopPackets, slot, err := preparePhantomChargeEffect(
				runtime.effectPool, pet.ObjectID, "status_burning.ServerEventDef",
			)
			if err != nil {
				isCreated, releaseErr := run.release(runtime.modifierPool)
				if releaseErr != nil || isCreated {
					return nil, stats, fmt.Errorf("petBurnRollback[%t]: %w", isCreated, errors.Join(err, releaseErr))
				}
				return nil, stats, fmt.Errorf("petBurnEffect: %w", err)
			}
			modifierPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
				TargetID: pet.ObjectID, ModifierGUID: run.record.GUID,
				InstanceID: run.instanceID, StackCount: 1, StartMilliseconds: timestamp,
				SourceID: pool.MarkerID,
			})
			if err == nil {
				err = e.trackCampaignNPCModifier(run)
			}
			if err != nil {
				runtime.effectPool.Release(pet.ObjectID, slot)
				isCreated, releaseErr := run.release(runtime.modifierPool)
				if releaseErr != nil || isCreated {
					return nil, stats, fmt.Errorf("petBurnRollback[%t]: %w", isCreated, errors.Join(err, releaseErr))
				}
				return nil, stats, fmt.Errorf("petBurnCreate: %w", err)
			}
			if !run.create() {
				e.untrackCampaignNPCModifier(run)
				runtime.effectPool.Release(pet.ObjectID, slot)
				isCreated, releaseErr := run.release(runtime.modifierPool)
				if releaseErr != nil || isCreated {
					return nil, stats, fmt.Errorf("petBurnCommitRollback[%t]: %w", isCreated, errors.Join(errors.New("reservation unavailable"), releaseErr))
				}
				return nil, stats, fmt.Errorf("petBurnCommit: reservation unavailable")
			}
			presentation := &petPlasmaPresentation{
				pool: runtime.effectPool, modifierPool: runtime.modifierPool,
				burns: e.petPlasmaBurns, modifiers: e.campaignNPCModifiers,
				objectID: pet.ObjectID, slot: slot, run: run,
			}
			run.cancel = presentation.stop
			burn = petPlasmaBurn{
				sourceObjectID: pool.MarkerID, readyAt: now.Add(time.Second),
				stopPacket: stopPackets[0], run: run, presentation: presentation,
			}
			e.petPlasmaBurns[pet.ObjectID] = burn
			startPackets := [][]byte{modifierPacket, startPacket}
			packets = append(packets, startPackets...)
			e.publishPetPlasmaEffect(runtime, startPackets)
			continue
		}
		if now.Before(burn.readyAt) {
			continue
		}
		damage, err := sim.SelectRankDamage(e.zone.NPCRandom(), sim.DamageRange{
			Minimum: 10, Maximum: 20,
		})
		if err != nil {
			return nil, stats, fmt.Errorf("petBurnSelect: %w", err)
		}
		plan := zonenpc.AttackPlan{
			SourceObjectID: pool.MarkerID, TargetObjectID: pet.ObjectID,
			SourcePosition: pool.Position, TargetPosition: pet.Position,
			Profile: zonenpc.ActionProfile{
				Family: zonenpc.ActionRetainedArea, AbilityName: "CitadelPlasmaBurn",
				ModifierName: "CitadelPlasmaBurn", MinimumDamage: 10, MaximumDamage: 20,
				DescriptorMask: 1<<2 | 1<<14, IsRetainedVolumeDamage: true,
				DamageType: 3, DamageSource: 1, IsDamageProfileKnown: true,
			},
		}
		hits, delta, isApplied, err := runtime.applyEnemyStatusDamage(
			e, e.generation, plan, zonenpc.AttackResult{Damage: damage}, timestamp,
		)
		if err != nil {
			return nil, stats, fmt.Errorf("petBurnDamage: %w", err)
		}
		packets = append(packets, hits...)
		if isApplied {
			stats.PVEDamageTaken += delta.PVEDamageTaken
		}
		if currentBurn, isCurrent := e.petPlasmaBurns[pet.ObjectID]; isCurrent && currentBurn.run == burn.run && burn.run.isActive() {
			burn.readyAt = now.Add(time.Second)
			e.petPlasmaBurns[pet.ObjectID] = burn
		}
	}
	for objectID, burn := range e.petPlasmaBurns {
		if seenObjectIDs[objectID] {
			continue
		}
		effectPackets, err := e.endPetPlasmaBurn(runtime, objectID, burn)
		if err != nil {
			return nil, stats, fmt.Errorf("petBurnRetire: %w", err)
		}
		packets = append(packets, effectPackets...)
	}
	return packets, stats, nil
}

func (e *gameplayPeerSession) endPetPlasmaBurn(
	runtime campaignNPCActionRuntime, objectID uint32, burn petPlasmaBurn,
) ([][]byte, error) {
	deletePacket, err := effectraknet.ModifierDelete(objectID, burn.run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("petBurnDelete: %w", err)
	}
	isCreated, err := burn.run.release(runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("petBurnRelease: %w", err)
	}
	e.untrackCampaignNPCModifier(burn.run)
	burn.presentation.stop()
	packets := [][]byte{burn.stopPacket}
	if isCreated {
		packets = append(packets, deletePacket)
	}
	e.publishPetPlasmaEffect(runtime, packets)
	return packets, nil
}

func (e *gameplayPeerSession) stopPetPlasmaBurns(
	runtime campaignNPCActionRuntime,
) ([][]byte, error) {
	packets := make([][]byte, 0)
	for objectID, burn := range e.petPlasmaBurns {
		stoppedPackets, err := e.endPetPlasmaBurn(runtime, objectID, burn)
		if err != nil {
			return packets, fmt.Errorf("petBurnStop[%d]: %w", objectID, err)
		}
		packets = append(packets, stoppedPackets...)
	}
	return packets, nil
}

func (e *gameplayPeerSession) cancelPetPlasmaBurns() {
	for objectID := range e.petPlasmaBurns {
		e.cancelPetPlasmaBurn(objectID)
	}
}

func (e *gameplayPeerSession) cancelPetPlasmaBurn(objectID uint32) {
	if e == nil {
		return
	}
	burn, isFound := e.petPlasmaBurns[objectID]
	if !isFound {
		return
	}
	burn.presentation.stop()
}

func (e *gameplayPeerSession) publishPetPlasmaEffect(
	runtime campaignNPCActionRuntime, packets [][]byte,
) {
	for sessionKey, member := range runtime.registry.sessions {
		if member.zone != e.zone || member.deployedObjectID == e.deployedObjectID {
			continue
		}
		member.queueCampaignPackets(packets)
		runtime.registry.sessions[sessionKey] = member
	}
}
