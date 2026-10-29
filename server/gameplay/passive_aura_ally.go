package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e gameplayPendingRuntime) pollNativeAllySilenceLocked(member *gameplayPeerSession, timestamp uint64) ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, actor := range member.zone.Companion().Snapshots() {
		if actor.UserID != member.binding.UserID || actor.PeerGeneration != member.generation || actor.HitPoint <= 0 {
			continue
		}
		childPackets, err := e.refreshNativeAllySilenceLocked(member, actor.ObjectID, actor.Position, false, timestamp)
		if err != nil {
			return packets, fmt.Errorf("companionSilence: %w", err)
		}
		packets = append(packets, childPackets...)
	}
	// Shared player-aligned NPCs have one deterministic live session owner.
	for _, candidate := range e.registry.sessions {
		if candidate.zone == member.zone && candidate.binding.UserID < member.binding.UserID && !candidate.isZoneTerminal() {
			return packets, nil
		}
	}
	for _, actor := range member.zone.NPCs().LiveSnapshots() {
		if !actor.IsPublished || actor.IsDefeated || actor.HitPoint <= 0 || actor.Faction != zonenpc.FactionPlayerAligned {
			continue
		}
		childPackets, err := e.refreshNativeAllySilenceLocked(member, actor.Plan.ObjectID, actor.Plan.Position, true, timestamp)
		if err != nil {
			return packets, fmt.Errorf("alignedNPCSilence: %w", err)
		}
		packets = append(packets, childPackets...)
	}
	return packets, nil
}

func (e gameplayPendingRuntime) refreshNativeAllySilenceLocked(member *gameplayPeerSession,
	targetObjectID uint32, position game.Vec3, isNPC bool, timestamp uint64,
) ([][]byte, error) {
	now := e.now()
	var current *campaignNPCModifierRun
	for _, run := range member.campaignNPCModifiers {
		if run.record.GUID == util.HashID(nativeSilenceChild) && run.record.TargetObjectID == targetObjectID {
			current = run
			break
		}
	}
	if current != nil && current.record.ExpiresAt.Sub(now) > 100*time.Millisecond {
		return nil, nil
	}
	var source zonenpc.Snapshot
	for _, candidate := range member.zone.NPCs().LiveSnapshots() {
		if candidate.IsPublished && !candidate.IsDefeated && candidate.HitPoint > 0 &&
			!candidate.Plan.IsIntroductionHidden && candidate.Faction == zonenpc.FactionNonPlayerAligned &&
			zonenpc.NativePassiveAura(candidate.Plan) == "CitadelSpecialTwo_SilenceAura" &&
			candidate.Plan.Position.Sub(position).Length() <= 2 {
			source = candidate
			break
		}
	}
	if source.Plan.ObjectID == 0 {
		return nil, nil
	}
	isNew := current == nil
	if isNew {
		var err error
		current, err = newCampaignNPCModifierRun(e.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("allySilenceAllocate: %w", err)
		}
	}
	record := zoneeffect.Modifier{InstanceID: current.instanceID, GUID: util.HashID(nativeSilenceChild),
		SourceObjectID: source.Plan.ObjectID, TargetObjectID: targetObjectID,
		InitiatorObject: source.Plan.ObjectID, Rank: uint32(zonenpc.EffectivePassiveRank(source.Plan)),
		Kind: zoneeffect.ModifierKindDebuff, Duration: 500 * time.Millisecond, ExpiresAt: now.Add(500 * time.Millisecond)}
	created, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: record.SourceObjectID, TargetObjectID: record.TargetObjectID,
		ModifierID: record.GUID, InstanceID: record.InstanceID, Duration: record.Duration, Timestamp: timestamp})
	if err == nil {
		if isNew {
			err = member.trackCampaignNPCModifier(current)
			if err == nil {
				err = member.zone.Effect().Put(record)
			}
		} else {
			err = member.zone.Effect().Update(record)
		}
	}
	if err != nil {
		if isNew {
			member.untrackCampaignNPCModifier(current)
			isCreated, releaseErr := current.release(e.modifierPool)
			_ = isCreated // No presentation has been returned.
			err = errors.Join(err, releaseErr)
		}
		return nil, fmt.Errorf("allySilenceCreate: %w", err)
	}
	current.record = record
	if isNew {
		current.create()
	}
	if isNPC {
		err = member.zone.NPCs().ApplySilence(targetObjectID, record.ExpiresAt)
	} else {
		err = member.zone.Companion().ApplySilence(targetObjectID, record.ExpiresAt)
	}
	if err != nil {
		member.untrackCampaignNPCModifier(current)
		member.zone.Effect().Remove(current.instanceID)
		isCreated, releaseErr := current.release(e.modifierPool)
		_ = isCreated // The vanished actor has no remaining presentation owner.
		return nil, fmt.Errorf("allySilenceApply: %w", errors.Join(err, releaseErr))
	}
	attribute, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: targetObjectID, Value: map[uint8]float32{14: 1}})
	if err != nil {
		return nil, fmt.Errorf("allySilenceAttribute: %w", err)
	}
	return [][]byte{created, attribute}, nil
}
