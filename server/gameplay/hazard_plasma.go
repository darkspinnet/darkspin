package gameplay

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type plasmaBurnPresentation struct {
	registry  *gameplaySessionRegistry
	zone      *zone.Zone
	pool      *attachedEffectPool
	objectID  uint32
	slot      uint8
	packet    []byte
	isStopped bool
}

// All calls hold the registry lock, including modifier retirement on death.
func (e *plasmaBurnPresentation) stop() {
	if e.isStopped {
		return
	}
	e.isStopped = true
	e.pool.Release(e.objectID, e.slot)
	e.publishToAllies([][]byte{e.packet})
}

func (e *plasmaBurnPresentation) publishToAllies(packets [][]byte) {
	for sessionKey, member := range e.registry.sessions {
		if member.zone != e.zone || member.deployedObjectID == e.objectID {
			continue
		}
		member.queueCampaignPackets(packets)
		e.registry.sessions[sessionKey] = member
	}
}

// This passive belongs to zero-health scenery, not the live NPC roster. The
// director's selected marker sets retain its authored owner and world position.
func plasmaPoolContact(member *gameplayPeerSession, position game.Vec3) (game.CampaignDirectorMarker, bool) {
	if member == nil || member.zone == nil || member.deployedObjectID == 0 || member.deployedHitPoint() <= 0 {
		return game.CampaignDirectorMarker{}, false
	}
	director := member.zone.DirectorDefinition()
	if !director.IsInitialLayoutSelected {
		return game.CampaignDirectorMarker{}, false
	}
	for _, markerSet := range director.MarkerSets {
		for _, marker := range markerSet.Markers {
			if !strings.EqualFold(marker.NounName, "DEST_citadel_plasma_pool.Noun") || marker.MarkerID == 0 {
				continue
			}
			definition, isSelected := director.SelectedMarkerDefinition(marker.MarkerID)
			if !isSelected || game.SceneryHazardAbility(definition.NounName) != "CitadelPlasmaBurn" {
				continue
			}
			if position.Sub(marker.Position).Length() <= 5 {
				return marker, true
			}
		}
	}
	return game.CampaignDirectorMarker{}, false
}

func (e campaignNPCActionRuntime) applyPlasmaPoolContact(
	member *gameplayPeerSession, position game.Vec3, timestamp uint64, now time.Time,
) ([][]byte, sporenet.PlayerStatDelta, bool, error) {
	if member == nil {
		return nil, sporenet.PlayerStatDelta{}, false, nil
	}
	pool, isContact := plasmaPoolContact(member, position)
	packets := make([][]byte, 0)
	if member.plasmaBurn != nil && (!isContact || !member.plasmaBurn.isActive() ||
		member.plasmaBurn.record.TargetObjectID != member.deployedObjectID) {
		deletePackets, err := e.removePlasmaBurn(member)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, false, fmt.Errorf("plasmaRemove: %w", err)
		}
		packets = append(packets, deletePackets...)
	}
	if !isContact {
		return packets, sporenet.PlayerStatDelta{}, false, nil
	}
	if member.plasmaBurn == nil {
		createPackets, err := e.createPlasmaBurn(member, pool.MarkerID, timestamp)
		if err != nil {
			return nil, sporenet.PlayerStatDelta{}, false, fmt.Errorf("plasmaCreate: %w", err)
		}
		member.plasmaBurnReadyAt = now.Add(time.Second)
		return append(packets, createPackets...), sporenet.PlayerStatDelta{}, true, nil
	}
	if now.Before(member.plasmaBurnReadyAt) {
		return packets, sporenet.PlayerStatDelta{}, true, nil
	}
	damage, err := sim.SelectRankDamage(member.zone.NPCRandom(), sim.DamageRange{Minimum: 10, Maximum: 20})
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, true, fmt.Errorf("plasmaDamageSelect: %w", err)
	}
	plan := zonenpc.AttackPlan{
		SourceObjectID: pool.MarkerID, TargetObjectID: member.deployedObjectID,
		SourcePosition: pool.Position, TargetPosition: position,
		Profile: zonenpc.ActionProfile{
			Family: zonenpc.ActionRetainedArea, AbilityName: "CitadelPlasmaBurn",
			ModifierName: "CitadelPlasmaBurn", MinimumDamage: 10, MaximumDamage: 20,
			DescriptorMask:         1<<2 | 1<<14,
			IsRetainedVolumeDamage: true,
			DamageType:             3, DamageSource: 1, IsDamageProfileKnown: true,
		},
	}
	damagePackets, delta, isApplied, err := e.applyEnemyStatusDamage(
		member, member.generation, plan, zonenpc.AttackResult{Damage: damage}, timestamp,
	)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, true, fmt.Errorf("plasmaDamage: %w", err)
	}
	member.plasmaBurnReadyAt = now.Add(time.Second)
	if !isApplied {
		return append(packets, damagePackets...), sporenet.PlayerStatDelta{}, true, nil
	}
	return append(packets, damagePackets...), delta, true, nil
}

func (e campaignNPCActionRuntime) createPlasmaBurn(member *gameplayPeerSession, sourceObjectID uint32, timestamp uint64) ([][]byte, error) {
	run, err := newCampaignNPCModifierRun(e.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("plasmaReserve: %w", err)
	}
	run.record = zoneeffect.Modifier{
		InstanceID: run.instanceID, GUID: util.HashID("CitadelPlasmaBurn"),
		SourceObjectID: sourceObjectID, TargetObjectID: member.deployedObjectID,
		Rank: 1, Kind: zoneeffect.ModifierKindDebuff, StackCount: 1,
	}
	startEffectPacket, stopEffectPackets, slot, err := preparePhantomChargeEffect(
		e.effectPool, member.deployedObjectID, "status_burning.ServerEventDef",
	)
	if err != nil {
		isCreated, releaseErr := run.release(e.modifierPool)
		if releaseErr != nil || isCreated {
			return nil, fmt.Errorf("plasmaEffectRollback: effect=%v release=%v created=%t", err, releaseErr, isCreated)
		}
		return nil, fmt.Errorf("plasmaEffect: %w", err)
	}
	presentation := &plasmaBurnPresentation{
		registry: e.registry, zone: member.zone, pool: e.effectPool,
		objectID: member.deployedObjectID, slot: slot, packet: stopEffectPackets[0],
	}
	// The client modifier has duration zero: the aura owns its lifetime and
	// deletes it on exit. Do not invent a timed burn after leaving the pool.
	packet, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: member.deployedObjectID, ModifierGUID: run.record.GUID,
		InstanceID: run.instanceID, StackCount: 1, StartMilliseconds: timestamp,
		SourceID: sourceObjectID,
	})
	if err == nil {
		err = member.trackCampaignNPCModifier(run)
	}
	if err != nil {
		e.effectPool.Release(member.deployedObjectID, slot)
		isCreated, releaseErr := run.release(e.modifierPool)
		if releaseErr != nil || isCreated {
			return nil, fmt.Errorf("plasmaRollback: create=%v release=%v created=%t", err, releaseErr, isCreated)
		}
		return nil, fmt.Errorf("plasmaPublish: %w", err)
	}
	if !run.create() {
		return nil, fmt.Errorf("plasmaCommit: modifier reservation unavailable")
	}
	run.cancel = presentation.stop
	member.plasmaBurn = run
	member.plasmaBurnEffect = presentation
	presentation.publishToAllies([][]byte{packet, startEffectPacket})
	return [][]byte{packet, startEffectPacket}, nil
}

func (e campaignNPCActionRuntime) removePlasmaBurn(member *gameplayPeerSession) ([][]byte, error) {
	run := member.plasmaBurn
	packet, err := effectraknet.ModifierDelete(run.record.TargetObjectID, run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("plasmaDelete: %w", err)
	}
	isCreated, err := run.release(e.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("plasmaRelease: %w", err)
	}
	member.untrackCampaignNPCModifier(run)
	packets := make([][]byte, 0, 2)
	if member.plasmaBurnEffect != nil {
		packets = append(packets, member.plasmaBurnEffect.packet)
		if isCreated {
			member.plasmaBurnEffect.publishToAllies([][]byte{packet})
		}
	}
	if run.cancel != nil {
		run.cancel()
	}
	member.plasmaBurn = nil
	member.plasmaBurnEffect = nil
	member.plasmaBurnReadyAt = time.Time{}
	if !isCreated {
		return packets, nil
	}
	return append(packets, packet), nil
}
