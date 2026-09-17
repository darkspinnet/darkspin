package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCOozeGrowthRun struct {
	modifier *campaignNPCModifierRun
}

type campaignNPCOozeGrowthSchedule struct {
	runtime        campaignNPCActionRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	sourceObjectID uint32
	targetObjectID uint32
	timestamp      uint64
	profile        zonenpc.ActionProfile
}

func (e campaignNPCOozeGrowthSchedule) hit() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.isCampaignNPCSourceActive(e.generation, e.sourceObjectID) &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.campaignNPCOozeGrowths == nil {
		peerSession.campaignNPCOozeGrowths =
			make(map[uint32]*campaignNPCOozeGrowthRun)
	}
	run := peerSession.campaignNPCOozeGrowths[e.targetObjectID]
	isNew := run == nil
	if isNew {
		modifier, err := newCampaignNPCModifierRun(e.runtime.modifierPool)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("oozeGrowthModifier: %w", err)
		}
		run = &campaignNPCOozeGrowthRun{modifier: modifier}
		err = peerSession.trackCampaignNPCModifier(modifier)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			_, _ = modifier.release(e.runtime.modifierPool)
			return nil, fmt.Errorf("oozeGrowthTrack: %w", err)
		}
	}
	result, err := peerSession.zone.NPCs().ApplyOozeGrowth(
		e.sourceObjectID, e.targetObjectID,
	)
	if err != nil {
		if isNew {
			peerSession.untrackCampaignNPCModifier(run.modifier)
			_, _ = run.modifier.release(e.runtime.modifierPool)
		}
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	modifierPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: e.targetObjectID, ModifierGUID: util.HashID(e.profile.ModifierName),
		InstanceID: run.modifier.instanceID, DurationMilliseconds: 0,
		StackCount:        result.StackCount,
		StartMilliseconds: e.timestamp + uint64(e.profile.HitDelay/time.Millisecond),
		SourceID:          e.sourceObjectID,
	})
	if err != nil {
		if isNew {
			peerSession.untrackCampaignNPCModifier(run.modifier)
			_, _ = run.modifier.release(e.runtime.modifierPool)
		}
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("oozeGrowthMarshal: %w", err)
	}
	peerSession.campaignNPCOozeGrowths[e.targetObjectID] = run
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if isNew {
		run.modifier.create()
	}
	packets := [][]byte{modifierPacket}
	bodyScaleBonus := 0.1 * float32(result.StackCount)
	targetPlan := result.Target.Plan
	if targetPlan.IsCaptain || targetPlan.IsElite || targetPlan.IsBoss ||
		targetPlan.BossIdentity.HasModifier(zonenpc.EliteModifierName) {
		bodyScaleBonus += zonenpc.EliteBodyScaleBonus
	}
	bodyScalePacket, bodyScaleErr := raknet.MarshalApplication(
		raknet.AttributeDataUpdateMessage{
			ObjectID: e.targetObjectID,
			Value: map[uint8]float32{
				uint8(game.AttributeBodyScale): bodyScaleBonus,
			},
		},
	)
	if bodyScaleErr != nil {
		e.runtime.logger.Printf(
			"RakNet campaign ooze body scale omitted target=%d: %v",
			e.targetObjectID, bodyScaleErr,
		)
	} else {
		packets = append(packets, bodyScalePacket)
	}
	if result.HealedAmount > 0 {
		objectiveErr := peerSession.zone.RecordNPCHeal(
			peerSession.zone.Context(), e.sourceObjectID,
			e.targetObjectID, result.HealedAmount,
		)
		if objectiveErr != nil {
			e.runtime.logger.Printf(
				"RakNet campaign ooze heal objective omitted source=%d target=%d: %v",
				e.sourceObjectID, e.targetObjectID, objectiveErr,
			)
		}
		healPackets, healErr := npcraknet.HealDelta(
			e.sourceObjectID, result.Target, result.HealedAmount,
		)
		if healErr != nil {
			e.runtime.logger.Printf(
				"RakNet campaign ooze heal presentation omitted source=%d target=%d: %v",
				e.sourceObjectID, e.targetObjectID, healErr,
			)
			return packets, nil
		}
		packets = append(healPackets, packets...)
	}
	return packets, nil
}

func (e campaignNPCOozeGrowthSchedule) next() ([][]byte, error) {
	timestamp := e.timestamp + uint64(e.profile.Cooldown/time.Millisecond)
	packets, err := e.runtime.produceEnemyMelee(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID, timestamp,
	)
	if err != nil {
		e.runtime.releaseAction(e.sessionKey, e.generation, e.sourceObjectID)
		return nil, fmt.Errorf("oozeGrowthNext: %w", err)
	}
	return packets, nil
}

func oozeRetreatPosition(source game.Vec3, target game.Vec3) game.Vec3 {
	delta := source.Sub(target)
	length := delta.Length()
	if length <= 0 {
		return source
	}
	return source.Add(delta.Scale(3 / length))
}

func (r campaignNPCActionRuntime) produceVerdanthBasicOozeGrowth(
	packet raknet.Packet, sessionKey string, generation uint64,
	source zonenpc.Snapshot, timestamp uint64,
) ([][]byte, bool, error) {
	profile, isProfileFound := zonenpc.VerdanthBasicOozeGrowProfile(
		source.Plan.NounName,
	)
	if !isProfileFound {
		return nil, false, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.isCampaignNPCSourceActive(generation, source.Plan.ObjectID) &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, true, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().FirstOozeGrowthTarget(
		source.Plan.ObjectID, profile.Range,
	)
	if !isTargetFound {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	retreatPosition := oozeRetreatPosition(source.Plan.Position, target.Plan.Position)
	projectedPosition, _, err := zoneaction.NPCProjectPosition(
		peerSession.zone.Navigation(), retreatPosition,
		source.Plan.NPCProfile.FootprintRadius,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("oozeGrowthProject: %w", err)
	}
	err = peerSession.zone.NPCs().SetPosition(source.Plan.ObjectID, projectedPosition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("oozeGrowthPosition: %w", err)
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	castPackets, err := npcraknet.OozeGrowthCast(
		source.Plan.ObjectID, target.Plan.ObjectID, projectedPosition,
		profile, timestamp,
	)
	if err != nil {
		return nil, true, fmt.Errorf("oozeGrowthCast: %w", err)
	}
	schedule := campaignNPCOozeGrowthSchedule{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: source.Plan.ObjectID,
		targetObjectID: target.Plan.ObjectID, timestamp: timestamp,
		profile: profile,
	}
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: schedule.hit},
		{Delay: profile.Cooldown, Produce: schedule.next},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.releaseAction(sessionKey, generation, source.Plan.ObjectID)
		return nil, true, fmt.Errorf("oozeGrowthSchedule: %w", err)
	}
	return castPackets, true, nil
}

func (r campaignNPCActionRuntime) stopOozeGrowth(
	sessionKey string, generation uint64, objectID uint32,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation
	var run *campaignNPCOozeGrowthRun
	if isCurrent {
		run = peerSession.campaignNPCOozeGrowths[objectID]
	}
	if run != nil {
		delete(peerSession.campaignNPCOozeGrowths, objectID)
		peerSession.untrackCampaignNPCModifier(run.modifier)
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if run == nil {
		return nil, nil
	}
	isCreated, err := run.modifier.release(r.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("oozeGrowthRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
		TargetID: objectID, InstanceID: run.modifier.instanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("oozeGrowthDelete: %w", err)
	}
	return [][]byte{packet}, nil
}
