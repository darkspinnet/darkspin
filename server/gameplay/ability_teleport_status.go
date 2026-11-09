package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The generic modifier lifetime owns the exact attributes through expiry,
// dispel, recipient death, and session teardown.
type teleportControlState struct {
	npc        *zonenpc.Session
	objectID   uint32
	instanceID uint32
	runtime    campaignAbilityCommandRuntime
	zone       *zone.Zone
}

func (e *teleportControlState) clear() {
	if e == nil {
		return
	}
	e.npc.ClearTimeStopModifier(e.objectID, e.instanceID)
	reconcile := npcChronoReconciliation{runtime: e.runtime, key: chronoFreezeKey{zone: e.zone, objectID: e.objectID}}
	err := reconcile.schedule(0)
	if err != nil {
		reconcile.retry(err)
	}
}

func (e campaignAreaHitStep) rollbackTeleportControlLocked(member *gameplayPeerSession, run *campaignNPCModifierRun) error {
	if run.cancel != nil {
		run.cancel()
	}
	member.untrackCampaignNPCModifier(run)
	member.zone.Effect().Remove(run.instanceID)
	wasCreated, err := run.release(e.runtime.modifierPool)
	if err != nil {
		return fmt.Errorf("teleportRelease: %w", err)
	}
	if wasCreated {
		return errors.New("teleport rollback after publication")
	}
	return nil
}

// Build 103 declares Unique: a successful admission replaces the previous
// same-definition recipient instance with a newly allocated handle.
func (e campaignAreaHitStep) admitTeleportControlLocked(
	member *gameplayPeerSession, objectID uint32, at time.Time, duration time.Duration,
) ([][]byte, bool, error) {
	reaction, err := e.runtime.registry.prepareLaserControlReactionLocked(member, objectID)
	if err != nil {
		return nil, false, fmt.Errorf("teleportReaction: %w", err)
	}
	run, err := newCampaignNPCModifierRun(e.runtime.modifierPool)
	if err != nil {
		return nil, false, fmt.Errorf("teleportAllocate: %w", err)
	}
	run.teleportControl = &teleportControlState{npc: member.zone.NPCs(), objectID: objectID, instanceID: run.instanceID, runtime: e.runtime, zone: member.zone}
	run.record = zoneeffect.Modifier{InstanceID: run.instanceID, GUID: e.definition.RootModifierID,
		SourceObjectID: e.command.Common.ObjectID, TargetObjectID: objectID,
		Kind: zoneeffect.ModifierKindDebuff, Rank: uint32(max(int32(1), e.abilityRank)), Duration: duration, ExpiresAt: at.Add(duration)}
	packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: run.record.SourceObjectID, TargetObjectID: objectID,
		ModifierID: run.record.GUID, InstanceID: run.instanceID,
		Duration: duration, Timestamp: e.hitTimestamp,
	})
	if err != nil {
		return nil, false, fmt.Errorf("teleportCreate: %w", errors.Join(err, e.rollbackTeleportControlLocked(member, run)))
	}
	isAdmitted, err := run.teleportControl.npc.AdmitTimeStopModifier(objectID, run.instanceID, at, run.record.ExpiresAt, member.binding.Mode == game.ModeChain)
	if err != nil {
		return nil, false, fmt.Errorf("teleportAdmission: %w", errors.Join(err, e.rollbackTeleportControlLocked(member, run)))
	}
	if !isAdmitted {
		err = e.rollbackTeleportControlLocked(member, run)
		if err != nil {
			return nil, false, fmt.Errorf("teleportRejected: %w", err)
		}
		return nil, false, nil
	}
	err = member.zone.Effect().Put(run.record)
	if err != nil {
		return nil, false, fmt.Errorf("teleportInventory: %w", errors.Join(err, e.rollbackTeleportControlLocked(member, run)))
	}
	err = member.trackCampaignNPCModifier(run)
	if err != nil {
		return nil, false, fmt.Errorf("teleportTrack: %w", errors.Join(err, e.rollbackTeleportControlLocked(member, run)))
	}
	expiry := campaignNPCTimedModifierExpiryStep{runtime: e.runtime.npc, sessionKey: e.sessionKey,
		generation: e.generation, targetObjectID: objectID, run: run}
	run.cancel, err = scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{Delay: duration, Produce: expiry.produce}})
	if err != nil {
		return nil, false, fmt.Errorf("teleportSchedule: %w", errors.Join(err, e.rollbackTeleportControlLocked(member, run)))
	}
	replacements, err := e.runtime.prepareNativeUniqueReplacementsLocked(member, run)
	if err != nil {
		return nil, false, fmt.Errorf("teleportReplacement: %w", errors.Join(err, e.rollbackTeleportControlLocked(member, run)))
	}
	if !run.create() {
		return nil, false, fmt.Errorf("teleportPublish: %w", errors.Join(errors.New("modifier already retired"), e.rollbackTeleportControlLocked(member, run)))
	}
	deletes := e.runtime.commitNativeUniqueReplacementsLocked(e.sessionKey, member, replacements)
	reaction.commit()
	packets := append(deletes, reaction.packets...)
	return append(packets, packet), true, nil
}
