package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const nativeSilenceChild = "CitadelSpecialTwo_SilenceModifier"

// Build-103 chunks999/220 refresh a half-second hostile child every .4s.
// Each affected player's poll owns its child, independently of the source's
// transport. The finite child expires naturally after the source leaves/dies.
func (e gameplayPendingRuntime) pollNativeSilenceAuraLocked(member *gameplayPeerSession, timestamp uint64) ([][]byte, error) {
	now := e.now()
	packets := make([][]byte, 0)
	var current *campaignNPCModifierRun
	for _, run := range member.campaignNPCModifiers {
		if run.record.GUID != util.HashID(nativeSilenceChild) {
			continue
		}
		if now.Before(run.record.ExpiresAt) && nativeSilenceRecipientAlive(*member, run.record.TargetObjectID) {
			if run.record.TargetObjectID == member.deployedObjectID {
				current = run
			}
			continue
		}
		member.untrackCampaignNPCModifier(run)
		member.zone.Effect().Remove(run.instanceID)
		if run.record.TargetObjectID == member.deployedObjectID && member.enemySilenceExpiresAt == run.record.ExpiresAt {
			member.enemySilenceExpiresAt = time.Time{}
		}
		member.zone.Companion().ClearSilence(run.record.TargetObjectID, run.record.ExpiresAt)
		member.zone.NPCs().ClearSilence(run.record.TargetObjectID, run.record.ExpiresAt)
		isCreated, err := run.release(e.modifierPool)
		if err != nil {
			return packets, fmt.Errorf("auraSilenceRelease: %w", err)
		}
		if isCreated {
			deleted, deleteErr := effectraknet.ModifierDelete(run.record.TargetObjectID, run.instanceID)
			if deleteErr != nil {
				return packets, fmt.Errorf("auraSilenceDelete: %w", deleteErr)
			}
			packets = append(packets, deleted)
			if run.record.TargetObjectID == member.deployedObjectID {
				attribute, attributeErr := nativeSilenceAttribute(*member, now)
				if attributeErr != nil {
					return packets, fmt.Errorf("auraSilenceClear: %w", attributeErr)
				}
				packets = append(packets, attribute)
			} else {
				amount := float32(0)
				if member.zone.Companion().IsSilenced(run.record.TargetObjectID, now) ||
					member.zone.NPCs().SilenceRemaining(run.record.TargetObjectID, now) > 0 {
					amount = 1
				}
				attribute, attributeErr := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
					ObjectID: run.record.TargetObjectID, Value: map[uint8]float32{14: amount}})
				if attributeErr != nil {
					return packets, fmt.Errorf("auraAllySilenceClear: %w", attributeErr)
				}
				packets = append(packets, attribute)
			}
		}
	}
	if member.deployedObjectID == 0 || member.deployedHitPoint() <= 0 ||
		member.isHeroDebuffImmune(member.deployedObjectID) || member.isOverdriveActiveAt(now) {
		return packets, nil
	}
	target, isTargetFound := member.campaignNPCTarget(member.generation, member.deployedObjectID)
	if !isTargetFound {
		return packets, nil
	}
	var source zonenpc.Snapshot
	for _, candidate := range member.zone.NPCs().LiveSnapshots() {
		if !candidate.IsPublished || candidate.IsDefeated || candidate.HitPoint <= 0 ||
			candidate.Faction == zonenpc.FactionPlayerAligned || candidate.Plan.IsIntroductionHidden ||
			zonenpc.NativePassiveAura(candidate.Plan) != "CitadelSpecialTwo_SilenceAura" ||
			candidate.Plan.Position.Sub(target.Position).Length() > 2 {
			continue
		}
		source = candidate
		break
	}
	if source.Plan.ObjectID == 0 || current != nil && current.record.ExpiresAt.Sub(now) > 100*time.Millisecond {
		return packets, nil
	}
	isNew := current == nil
	if isNew {
		var err error
		current, err = newCampaignNPCModifierRun(e.modifierPool)
		if err != nil {
			return packets, fmt.Errorf("auraSilenceAllocate: %w", err)
		}
	}
	record := zoneeffect.Modifier{InstanceID: current.instanceID, GUID: util.HashID(nativeSilenceChild),
		SourceObjectID: source.Plan.ObjectID, TargetObjectID: member.deployedObjectID,
		InitiatorObject: source.Plan.ObjectID, Rank: uint32(zonenpc.EffectivePassiveRank(source.Plan)),
		Kind: zoneeffect.ModifierKindDebuff, Duration: 500 * time.Millisecond, ExpiresAt: now.Add(500 * time.Millisecond)}
	created, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: record.SourceObjectID, TargetObjectID: record.TargetObjectID,
		ModifierID: record.GUID, InstanceID: record.InstanceID, Duration: record.Duration, Timestamp: timestamp})
	if err != nil {
		if isNew {
			isCreated, releaseErr := current.release(e.modifierPool)
			_ = isCreated // A failed create has no published presentation.
			err = errors.Join(err, releaseErr)
		}
		return packets, fmt.Errorf("auraSilenceCreate: %w", err)
	}
	if isNew {
		err = member.trackCampaignNPCModifier(current)
		if err == nil {
			err = member.zone.Effect().Put(record)
		}
	} else {
		err = member.zone.Effect().Update(record)
	}
	if err != nil {
		if isNew {
			member.untrackCampaignNPCModifier(current)
			isCreated, releaseErr := current.release(e.modifierPool)
			_ = isCreated // The create packet has not been returned.
			err = errors.Join(err, releaseErr)
		}
		return packets, fmt.Errorf("auraSilenceInventory: %w", err)
	}
	current.record = record
	if isNew {
		current.create()
	}
	member.extendEnemySilence(record.TargetObjectID, record.ExpiresAt)
	interrupts, interruptErr := member.interruptNativeSilenceChannelsLocked(now, timestamp)
	if interruptErr != nil {
		return packets, fmt.Errorf("auraSilenceInterrupt: %w", interruptErr)
	}
	attribute, attributeErr := nativeSilenceAttribute(*member, now)
	if attributeErr != nil {
		return packets, fmt.Errorf("auraSilenceAttribute: %w", attributeErr)
	}
	packets = append(packets, created, attribute)
	return append(packets, interrupts...), nil
}

func nativeSilenceRecipientAlive(member gameplayPeerSession, objectID uint32) bool {
	if objectID == member.deployedObjectID {
		return member.deployedHitPoint() > 0
	}
	companion, isCompanionFound := member.zone.Companion().Snapshot(objectID)
	if isCompanionFound {
		return companion.UserID == member.binding.UserID && companion.PeerGeneration == member.generation && companion.HitPoint > 0
	}
	npc, isNPCFound := member.zone.NPCs().NPC(objectID)
	return isNPCFound && npc.IsPublished && !npc.IsDefeated && npc.HitPoint > 0 && npc.Faction == zonenpc.FactionPlayerAligned
}

func nativeSilenceAttribute(member gameplayPeerSession, now time.Time) ([]byte, error) {
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: member.deployedObjectID, Value: map[uint8]float32{14: member.arenaSilenceAmount(now)}})
	if err != nil {
		return nil, fmt.Errorf("silenceMarshal: %w", err)
	}
	return packet, nil
}

func (e *gameplayPeerSession) interruptNativeSilenceChannelsLocked(now time.Time, timestamp uint64) ([][]byte, error) {
	packets := make([][]byte, 0)
	isChannel := e.heroDrain != nil || e.heroChannelArea != nil || e.rocketBarrage != nil ||
		e.heroHealingTicks != nil && e.heroHealingTicks.isChanneled
	if !isChannel {
		return nil, nil
	}
	e.retireChannelsLocked(timestamp)
	e.heroDrain = nil
	e.heroChannelArea = nil
	e.interruptRocketBarrage(now)
	if e.heroHealingTicks != nil && e.heroHealingTicks.isChanneled {
		healing := e.heroHealingTicks
		interruptionPackets, err := healing.interruptionPacketsAt(timestamp)
		if err != nil {
			return nil, fmt.Errorf("silenceHealInterrupt: %w", err)
		}
		healing.Stop()
		e.heroHealingTicks = nil
		packets = append(packets, interruptionPackets...)
	}
	e.resetAbilityRelease()
	return packets, nil
}
