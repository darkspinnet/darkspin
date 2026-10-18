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

type campaignFixtureBurnStep struct {
	blast              campaignFixtureBlastStep
	run                *campaignNPCModifierRun
	targetID           uint32
	profile            zonenpc.ActionProfile
	remainingTickCount uint32
	isPermanent        bool
}

func (e campaignFixtureBlastStep) startBurn(targetID uint32, profile zonenpc.ActionProfile) ([][]byte, error) {
	r := e.runtime
	r.registry.mutex.Lock()
	member, isFound := r.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation || member.isZoneTerminal() {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if profile.ModifierName == "LifePlagueSpread" &&
		e.timestamp < member.campaignNPCDiseaseImmunityEnds[targetID] {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := member.zone.NPCs().LiveNPC(targetID)
	if !isTargetFound || !target.IsPublished {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	modifierID := util.HashID(profile.ModifierName)
	for _, current := range member.campaignNPCModifiers {
		if current != nil && current.isActive() && current.record.TargetObjectID == targetID && current.record.GUID == modifierID {
			r.registry.mutex.Unlock()
			return nil, nil
		}
	}
	run, err := newCampaignNPCModifierRun(r.npc.modifierPool)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("fixtureBurnAllocate: %w", err)
	}
	run.record = zoneeffect.Modifier{
		InstanceID: run.instanceID, GUID: modifierID,
		SourceObjectID: e.source.Plan.ObjectID, TargetObjectID: targetID,
		Rank: 1, Duration: profile.ModifierDuration, Kind: zoneeffect.ModifierKindDebuff,
		InitiatorObject: e.source.Plan.ObjectID,
	}
	err = member.trackCampaignNPCModifier(run)
	if err == nil {
		r.registry.sessions[e.sessionKey] = member
	}
	r.registry.mutex.Unlock()
	if err != nil {
		isCreated, releaseErr := run.release(r.npc.modifierPool)
		if isCreated && r.logger != nil {
			r.logger.Printf("fixture burn unexpectedly created before tracking target=%d", targetID)
		}
		return nil, fmt.Errorf("fixtureBurnTrack: %w", errors.Join(err, releaseErr))
	}
	// CitadelPlasmaBurn explicitly authors duration=0 (until removal). The
	// finite-duration codec rejects zero, so preserve the native wire contract here.
	create, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: targetID, SourceID: e.source.Plan.ObjectID,
		ModifierGUID: modifierID, InstanceID: run.instanceID, StackCount: 1,
		DurationMilliseconds: uint32(profile.ModifierDuration.Milliseconds()), StartMilliseconds: e.timestamp,
	})
	if err != nil {
		r.npc.rollbackCampaignNPCTimedModifier(e.sessionKey, e.generation, run)
		return nil, fmt.Errorf("fixtureBurnCreate: %w", err)
	}
	isCreated := run.create()
	if !isCreated {
		r.npc.rollbackCampaignNPCTimedModifier(e.sessionKey, e.generation, run)
		return nil, nil
	}
	step := campaignFixtureBurnStep{
		blast: e, run: run, targetID: targetID, profile: profile,
		remainingTickCount: uint32(profile.ModifierDuration / time.Second),
		isPermanent:        profile.ModifierDuration == 0,
	}
	err = step.schedule()
	if err != nil {
		r.npc.rollbackCampaignNPCTimedModifier(e.sessionKey, e.generation, run)
		return nil, fmt.Errorf("fixtureBurnStart: %w", err)
	}
	return [][]byte{create}, nil
}

func (e campaignFixtureBurnStep) schedule() error {
	cancel, err := scheduleNPCProducers(e.blast.runtime.registry, e.blast.packet,
		[]raknet.ScheduledPacketProducer{{Delay: time.Second, Produce: e.produce}})
	if err != nil {
		return fmt.Errorf("fixtureBurnSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("fixture burn cancellation unavailable")
	}
	e.run.cancel = cancel
	return nil
}

func (e campaignFixtureBurnStep) stop() ([][]byte, error) {
	r := e.blast.runtime
	r.registry.mutex.Lock()
	member, isFound := r.registry.sessions[e.blast.sessionKey]
	if isFound && member.generation == e.blast.generation && member.campaignNPCModifiers[e.run.instanceID] == e.run {
		member.untrackCampaignNPCModifier(e.run)
		if e.profile.ModifierName == "LifePlagueSpread" && e.remainingTickCount == 0 {
			if member.campaignNPCDiseaseImmunityEnds == nil {
				member.campaignNPCDiseaseImmunityEnds = make(map[uint32]uint64)
			}
			member.campaignNPCDiseaseImmunityEnds[e.targetID] =
				e.blast.timestamp + uint64(campaignNPCDiseaseImmunityPeriod/time.Millisecond)
		}
		r.registry.sessions[e.blast.sessionKey] = member
	}
	r.registry.mutex.Unlock()
	isCreated, err := e.run.release(r.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("fixtureBurnRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(e.targetID, e.run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("fixtureBurnDelete: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e campaignFixtureBurnStep) produce() ([][]byte, error) {
	if !e.run.isActive() {
		return nil, nil
	}
	profile := e.profile
	profile.MinimumDamage = profile.ModifierMinimumTickDamage
	profile.MaximumDamage = profile.ModifierMaximumTickDamage
	profile.DamageCoefficient = profile.ModifierTickDamageCoefficient
	profile.DamageType = 3
	profile.DamageSource = 1
	profile.DescriptorMask = 1 << 2
	if profile.IsModifierDamageProfileKnown {
		profile.DamageType = profile.ModifierDamageType
		profile.DamageSource = profile.ModifierDamageSource
		profile.DescriptorMask = profile.ModifierDescriptorMask
	}
	e.blast.timestamp += 1000
	packets, isSurviving, err := e.blast.damage(e.targetID, profile, true)
	if err != nil {
		deletePackets, deleteErr := e.stop()
		return append(packets, deletePackets...), fmt.Errorf("fixtureBurnTick: %w", errors.Join(err, deleteErr))
	}
	if !e.isPermanent && e.remainingTickCount > 0 {
		e.remainingTickCount--
	}
	if !isSurviving || (!e.isPermanent && e.remainingTickCount == 0) {
		deletePackets, deleteErr := e.stop()
		if deleteErr != nil {
			return packets, fmt.Errorf("fixtureBurnFinish: %w", deleteErr)
		}
		return append(packets, deletePackets...), nil
	}
	err = e.schedule()
	if err != nil {
		deletePackets, deleteErr := e.stop()
		return append(packets, deletePackets...), fmt.Errorf("fixtureBurnContinue: %w", errors.Join(err, deleteErr))
	}
	return packets, nil
}
