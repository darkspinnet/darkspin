package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Chunk724 installs a living3.5-radius hostile trigger. Each connection owns
// its hero/companions, so two co-op polls cannot admit duplicate poison runs.
func (e gameplayPendingRuntime) pollMushroomAuras(packet raknet.Packet) ([][]byte, error) {
	e.registry.mutex.RLock()
	key := packet.Address.String()
	member, isFound := e.registry.sessions[key]
	if !isFound || member.transportGeneration != packet.TransportGeneration || member.zone == nil || member.isRejoinPending || !member.stage.IsDungeon() || !member.dungeonSetup.IsCommitted() || member.isZoneTerminal() {
		e.registry.mutex.RUnlock()
		return nil, nil
	}
	plans := make([]zonenpc.AttackPlan, 0)
	targets, sampleErr := e.registry.sampleNPCTargetsLocked(member.zone, member.zone.LiveNPCTargets(), e.damage.npc.now())
	if sampleErr != nil {
		e.registry.mutex.RUnlock()
		return nil, fmt.Errorf("mushroomTargets: %w", sampleErr)
	}
	for _, source := range member.zone.NPCs().Snapshots() {
		if !zonenpc.IsCryosFungus(source.Plan) || !source.IsPublished || source.IsDefeated || source.HitPoint <= 0 {
			continue
		}
		for _, target := range targets {
			if target.UserID != member.binding.UserID || target.PeerGeneration != member.generation || source.Plan.Position.Sub(target.Position).Length() > 3.5 {
				continue
			}
			plans = append(plans, zonenpc.AttackPlan{SourceObjectID: source.Plan.ObjectID, TargetObjectID: target.ObjectID, SourcePosition: source.Plan.Position, TargetPosition: target.Position, Profile: zonenpc.MushroomPoisonProfile()})
		}
	}
	e.registry.mutex.RUnlock()
	var packets [][]byte
	for _, plan := range plans {
		currentPackets, err := e.damage.npc.applyCampaignNPCDisease(nil, packet, key, member.generation, plan, packet.SourceTime)
		if err != nil {
			return packets, fmt.Errorf("mushroomPoison: %w", err)
		}
		packets = append(packets, currentPackets...)
	}
	err := publishCampaignPeersAfterCommit(e.registry, packet, packets)
	if err != nil {
		return packets, fmt.Errorf("mushroomPublish: %w", err)
	}
	return packets, nil
}

func isCadencedNPCPoison(name string) bool {
	return name == "CryosBossAddFire" || name == "MushroomCryosPoison"
}

// Native1039E1390/9E1320 only dispatch StackModifier; they do not increment.
// Chunk472 handles that event with ResetDuration alone, preserving count1 and
// the original DOT thread/snapshot even when another fungus refreshes it.
func (e campaignNPCActionRuntime) refreshMushroomPoisonLocked(packet raknet.Packet, sessionKey string, generation uint64, run *campaignNPCPoisonRun, timestamp uint64) ([][]byte, error) {
	created, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{SourceObjectID: run.plan.SourceObjectID, TargetObjectID: run.plan.TargetObjectID, ModifierID: run.plan.Profile.ModifierGUID(), InstanceID: run.modifier.instanceID, StackCount: 1, Duration: run.plan.Profile.ModifierDuration, Timestamp: timestamp})
	if err != nil {
		return nil, fmt.Errorf("mushroomRefreshMarshal: %w", err)
	}
	err = e.scheduleStackingBurnExpiryLocked(packet, sessionKey, generation, run, timestamp)
	if err != nil {
		return nil, fmt.Errorf("mushroomRefreshExpiry: %w", err)
	}
	return [][]byte{created}, nil
}

func (e campaignNPCActionRuntime) attachMushroomPoisonLocked(member gameplayPeerSession, run *campaignNPCModifierRun) error {
	effects, err := e.registry.allocateChannelEffectsLocked(member.zone, e.effectPool, []uint32{run.record.TargetObjectID})
	if err != nil {
		return fmt.Errorf("mushroomEffectAllocate: %w", err)
	}
	slot := effects.Slots()[0]
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{ObjectID: run.record.TargetObjectID, Slot: slot.slot + 1, IsForceAttached: true, Asset: util.HashID("status_poisoned.ServerEventDef")})
	if err != nil {
		effects.Retire()
		return fmt.Errorf("mushroomEffectMarshal: %w", err)
	}
	run.mushroomEffects = effects
	err = e.registry.publishChannelEffectsLocked(effects, [][]byte{packet})
	if err != nil {
		effects.Retire()
		return fmt.Errorf("mushroomEffectPublish: %w", err)
	}
	return nil
}

// Chunk567 notifies the complete ServerEventDef, then immediately marks the
// original object for deletion. There is no retained dead aura or cloud timer.
func withMushroomDeath(snapshot zonenpc.Snapshot, definition zoneNPCDeathDefinition) zoneNPCDeathDefinition {
	if !zonenpc.IsCryosFungus(snapshot.Plan) {
		return definition
	}
	definition.isImmediateDelete = true
	definition.isDeathAnimationSuppressed = true
	definition.ordinaryDeathAnimation = ""
	definition.corpseFadeDelay = 0
	definition.graphicsState = 0
	definition.explosionEffectName = "hazard_poisonStalk_explosion.ServerEventDef"
	definition.deleteDelay = 0
	return definition
}
