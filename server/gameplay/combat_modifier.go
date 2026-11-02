package gameplay

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type campaignNPCTimedModifierExpiryStep struct {
	runtime        campaignNPCActionRuntime
	sessionKey     string
	generation     uint64
	targetObjectID uint32
	run            *campaignNPCModifierRun
}

func (e campaignNPCTimedModifierExpiryStep) produce() ([][]byte, error) {
	if e.run.frogCage != nil {
		e.runtime.registry.mutex.Lock()
		packets, err := e.runtime.registry.retireOperativeCageLocked(e.run, e.runtime.modifierPool)
		e.runtime.registry.mutex.Unlock()
		if err != nil {
			return nil, fmt.Errorf("frogCageExpiry: %w", err)
		}
		return packets, nil
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCModifiers[e.run.instanceID] == e.run
	if isCurrent {
		peerSession.untrackCampaignNPCModifier(e.run)
		if peerSession.zone != nil && peerSession.zone.Effect() != nil {
			peerSession.zone.Effect().Remove(e.run.instanceID)
		}
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		if e.run.cancel != nil {
			e.run.cancel()
		}
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	isCreated, err := e.run.release(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("enemyTimedModifierRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		e.targetObjectID, e.run.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("enemyTimedModifierDelete: %w", err)
	}
	packets := [][]byte{packet}
	if e.run.record.MovementSpeedBuff != 0 {
		speedPackets, speedErr := e.runtime.refreshScarabDazeMovement(e.sessionKey, e.generation, e.targetObjectID)
		if speedErr != nil {
			return nil, fmt.Errorf("dazeExpirySpeed: %w", speedErr)
		}
		packets = append(packets, speedPackets...)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) applyCampaignNPCTimedModifier(
	packet raknet.Packet,
	sessionKey string,
	generation uint64,
	plan zonenpc.AttackPlan,
	timestamp uint64,
) ([][]byte, error) {
	profile := plan.Profile
	if profile.MovementSpeedBuff < 0 {
		r.registry.mutex.RLock()
		sourceMember, isSourceFound := r.registry.sessions[sessionKey]
		if isSourceFound {
			target, isTargetFound := sourceMember.campaignNPCTarget(sourceMember.generation, plan.TargetObjectID)
			for targetKey, targetMember := range r.registry.sessions {
				if isTargetFound && targetMember.zone == sourceMember.zone && targetMember.binding.UserID == target.UserID &&
					targetMember.generation == target.PeerGeneration {
					sessionKey = targetKey
					generation = targetMember.generation
					break
				}
			}
		}
		r.registry.mutex.RUnlock()
	}
	if zonenpc.IsOperativeCage(profile.ModifierName) &&
		!r.canApplyOperativeCage(sessionKey, generation, plan) {
		return nil, nil
	}
	isIndefinite := zonenpc.IsOperativeCage(profile.ModifierName) && profile.ModifierDuration == 0
	if profile.ModifierDuration > 0 && r.now == nil {
		return nil, errors.New("enemy modifier clock unavailable")
	}
	if sessionKey == "" || generation == 0 || plan.SourceObjectID == 0 ||
		plan.TargetObjectID == 0 || profile.ModifierName == "" ||
		(profile.ModifierDuration < 0 || profile.ModifierDuration == 0 && !isIndefinite) {
		return nil, errors.New("enemy timed modifier request invalid")
	}
	isRoot := profile.ModifierName == "EntangleModifier" ||
		profile.ModifierName == "VerdanthBasicRootmobModifier"
	isStun := profile.ModifierName == "StalkerShock" || profile.ModifierName == "AcFreeze" ||
		profile.ModifierName == "CryosBossShock"
	if (profile.ModifierName == "SleepModifier" ||
		profile.ModifierName == "StalkerShock" ||
		profile.ModifierName == "CryosBossShock" ||
		profile.ModifierName == "AcFreeze" ||
		profile.ModifierName == "SilenceModifier" || profile.ModifierName == "CitadelMinionSuicide_Daze" || isRoot) && r.now == nil {
		return nil, errors.New("enemy modifier clock unavailable")
	}
	run, err := newCampaignNPCModifierRun(r.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("enemyTimedModifierRun: %w", err)
	}
	run.record = zoneeffect.Modifier{
		InstanceID: run.instanceID, GUID: profile.ModifierGUID(),
		SourceObjectID: plan.SourceObjectID, TargetObjectID: plan.TargetObjectID,
		Rank: uint32(max(int32(1), profile.AbilityRank)), Duration: profile.ModifierDuration, IsIndefinite: isIndefinite,
		Kind: zoneeffect.ModifierKindDebuff, InitiatorObject: plan.SourceObjectID,
		MovementSpeedBuff: profile.MovementSpeedBuff,
	}
	if !isIndefinite {
		run.record.ExpiresAt = r.now().Add(profile.ModifierDuration)
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil
	if isCurrent {
		_, isCurrent = peerSession.campaignNPCTarget(
			generation, plan.TargetObjectID,
		)
	}
	if isCurrent && r.isTargetStatusImmuneLocked(&peerSession, plan.TargetObjectID) {
		isCurrent = false
	}
	if isCurrent && profile.ModifierName == "CitadelMinionSuicide_Daze" && plan.TargetObjectID == peerSession.deployedObjectID && peerSession.isScarabDazed() {
		isCurrent = false
	}
	// Speed template453 is CasterUnique: repeated casts from one source do
	// not stack another contribution while that source's modifier is alive.
	if isCurrent && (profile.ModifierName == "Chilled" || profile.ModifierName == "SlowNovaModifier") {
		for _, existing := range peerSession.campaignNPCModifiers {
			if existing != nil && existing.isActive() && existing.record.GUID == profile.ModifierGUID() && existing.record.SourceObjectID == plan.SourceObjectID && existing.record.TargetObjectID == plan.TargetObjectID {
				isCurrent = false
				break
			}
		}
	}
	if isCurrent {
		err = peerSession.trackCampaignNPCModifier(run)
		if err == nil {
			r.registry.sessions[sessionKey] = peerSession
		}
	}
	r.registry.mutex.Unlock()
	if !isCurrent || err != nil {
		_, releaseErr := run.release(r.modifierPool)
		if err != nil {
			return nil, fmt.Errorf(
				"enemyTimedModifierTrack: %w", errors.Join(err, releaseErr),
			)
		}
		if releaseErr != nil {
			return nil, fmt.Errorf("enemyTimedModifierRelease: %w", releaseErr)
		}
		return nil, nil
	}
	createPacket, err := effectraknet.ModifierCreate(
		effectraknet.ModifierCreateRequest{
			SourceObjectID: plan.SourceObjectID,
			TargetObjectID: plan.TargetObjectID,
			ModifierID:     profile.ModifierGUID(),
			InstanceID:     run.instanceID,
			Duration:       profile.ModifierDuration, IsIndefinite: isIndefinite,
			Timestamp: timestamp, IsBound: isStun,
		},
	)
	if err != nil {
		r.rollbackCampaignNPCTimedModifier(sessionKey, generation, run)
		return nil, fmt.Errorf("enemyTimedModifierCreate: %w", err)
	}
	expiry := campaignNPCTimedModifierExpiryStep{
		runtime: r, sessionKey: sessionKey, generation: generation,
		targetObjectID: plan.TargetObjectID, run: run,
	}
	cancel := raknet.CancelSchedule(operativeIndefiniteCancel)
	var scheduleErr error
	if !isIndefinite {
		cancel, scheduleErr = scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
			Delay: profile.ModifierDuration, Produce: expiry.produce,
		}})

	}
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		r.rollbackCampaignNPCTimedModifier(sessionKey, generation, run)
		return nil, fmt.Errorf("enemyTimedModifierSchedule: %w", scheduleErr)
	}
	if !run.create() {
		cancel()
		return nil, nil
	}
	run.cancel = cancel
	err = peerSession.zone.Effect().Put(run.record)
	if err != nil {
		cancel()
		r.rollbackCampaignNPCTimedModifier(sessionKey, generation, run)
		return nil, fmt.Errorf("enemyTimedModifierInventory: %w", err)
	}
	descriptor := campaignNPCDebuffDescriptor(profile.ModifierName)
	if descriptor != 0 {
		objectiveErr := peerSession.zone.RecordModifierCreated(
			context.Background(), run.instanceID, plan.SourceObjectID,
			plan.TargetObjectID, descriptor,
		)
		if objectiveErr != nil && r.logger != nil {
			r.logger.Printf(
				"RakNet campaign modifier objective skipped source=%d target=%d: %v",
				plan.SourceObjectID, plan.TargetObjectID, objectiveErr,
			)
		}
	}
	packets := [][]byte{createPacket}
	if profile.ModifierName == "AcFreeze" {
		freezePackets, freezeErr := r.attachACFreeze(sessionKey, generation, run)
		if freezeErr != nil {
			cancel()
			r.rollbackCampaignNPCTimedModifier(sessionKey, generation, run)
			return nil, fmt.Errorf("acFreezeEffect: %w", freezeErr)
		}
		packets = append(packets, freezePackets...)
	}
	if profile.ModifierName == "MarkOfZelem" {
		visualPackets, visualErr := r.attachPolarisMark(sessionKey, generation, run, profile.TargetEffectName)
		if visualErr != nil {
			cancel()
			r.rollbackCampaignNPCTimedModifier(sessionKey, generation, run)
			return nil, fmt.Errorf("markReticle: %w", visualErr)
		}
		packets = append(packets, visualPackets...)
	}
	if zonenpc.IsOperativeCage(profile.ModifierName) {
		stopPackets, isBound, bindErr := r.bindOperativeCage(peerSession, run)
		if bindErr != nil || !isBound {
			cancel()
			r.rollbackCampaignNPCTimedModifier(sessionKey, generation, run)
			if bindErr != nil {
				return nil, fmt.Errorf("cageBind: %w", bindErr)
			}
			return nil, nil
		}
		packets = append(packets, stopPackets...)
	}
	if profile.ModifierName == "SleepModifier" {
		at := r.now()
		expiresAt := at.Add(profile.ModifierDuration)
		r.registry.mutex.Lock()
		for targetSessionKey, targetSession := range r.registry.sessions {
			if targetSession.zone != peerSession.zone ||
				!targetSession.extendEnemySleep(plan.TargetObjectID, expiresAt) {
				continue
			}
			stopPackets, stopErr := stopEnemyControlledHeroMovement(&targetSession, at)
			if stopErr != nil {
				r.logger.Printf("RakNet sleeping hero stop omitted target=%d: %v", plan.TargetObjectID, stopErr)
			} else {
				packets = append(packets, stopPackets...)
			}
			controlPackets, controlErr := targetSession.refreshEnemyImmobilizedAttributes(at)
			if controlErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemySleepAttribute: %w", controlErr)
			}
			packets = append(packets, controlPackets...)
			r.registry.sessions[targetSessionKey] = targetSession
		}
		r.registry.mutex.Unlock()
	}
	if isStun {
		at := r.now()
		expiresAt := at.Add(profile.ModifierDuration)
		r.registry.mutex.Lock()
		for targetSessionKey, targetSession := range r.registry.sessions {
			if targetSession.zone != peerSession.zone ||
				!targetSession.extendEnemyStun(plan.TargetObjectID, expiresAt) {
				continue
			}
			stopPackets, stopErr := stopEnemyControlledHeroMovement(&targetSession, at)
			if stopErr != nil {
				r.logger.Printf("RakNet stunned hero stop omitted target=%d: %v", plan.TargetObjectID, stopErr)
			} else {
				packets = append(packets, stopPackets...)
			}
			controlPackets, controlErr := targetSession.refreshEnemyImmobilizedAttributes(at)
			if controlErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyStunAttribute: %w", controlErr)
			}
			packets = append(packets, controlPackets...)
			r.registry.sessions[targetSessionKey] = targetSession
		}
		r.registry.mutex.Unlock()
	}
	if profile.ModifierName == "SilenceModifier" {
		expiresAt := r.now().Add(profile.ModifierDuration)
		r.registry.mutex.Lock()
		for targetSessionKey, targetSession := range r.registry.sessions {
			if targetSession.zone != peerSession.zone ||
				!targetSession.extendEnemySilence(plan.TargetObjectID, expiresAt) {
				continue
			}
			r.registry.sessions[targetSessionKey] = targetSession
		}
		r.registry.mutex.Unlock()
	}
	if isRoot {
		expiresAt := r.now().Add(profile.ModifierDuration)
		rootStopPackets := make([][]byte, 0, 1)
		r.registry.mutex.Lock()
		for targetSessionKey, targetSession := range r.registry.sessions {
			if targetSession.zone != peerSession.zone ||
				!targetSession.extendEnemyRoot(plan.TargetObjectID, expiresAt) {
				continue
			}
			// Stop queued approach callbacks as well as the current route; they
			// otherwise restart movement toward an attack or trap on their next tick.
			targetSession.campaignPlayerPursuitSession().Cancel()
			rootPosition := targetSession.playerPosition
			if targetSession.playerMotion != nil {
				stoppedPosition, stopErr := targetSession.playerMotion.Stop(r.now())
				if stopErr == nil {
					rootPosition = toRakNetPosition(stoppedPosition)
					targetSession.playerPosition = rootPosition
					syncErr := targetSession.syncZoneHeroPose()
					if syncErr != nil && r.logger != nil {
						r.logger.Printf(
							"RakNet rooted hero pose sync omitted target=%d: %v",
							plan.TargetObjectID, syncErr,
						)
					}
				} else if r.logger != nil {
					r.logger.Printf(
						"RakNet rooted hero movement stop omitted target=%d: %v",
						plan.TargetObjectID, stopErr,
					)
				}
			}
			targetSession.playerMovementGoal = rootPosition
			stopPackets, stopErr := marshalPlayerMovementCorrection(
				plan.TargetObjectID, rootPosition, raknet.Quaternion{W: 1},
			)
			if stopErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyRootStopMarshal: %w", stopErr)
			}
			rootStopPackets = append(rootStopPackets, stopPackets...)
			rootAttributes, rootAttributeErr := targetSession.refreshEnemyImmobilizedAttributes(r.now())
			if rootAttributeErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyRootAttribute: %w", rootAttributeErr)
			}
			rootStopPackets = append(rootStopPackets, rootAttributes...)
			r.registry.sessions[targetSessionKey] = targetSession
		}
		r.registry.mutex.Unlock()
		packets = append(packets, rootStopPackets...)
	}
	if profile.ModifierName == "EntangleModifier" {
		err = peerSession.zone.NPCs().FocusAllies(
			plan.SourceObjectID, plan.TargetObjectID, 20,
		)
		if err != nil {
			return nil, fmt.Errorf("enemyEntangleFocus: %w", err)
		}
	}
	if profile.MovementSpeedBuff != 0 {
		speedPackets, speedErr := r.refreshScarabDazeMovement(sessionKey, generation, plan.TargetObjectID)
		if speedErr != nil {
			return nil, fmt.Errorf("scarabDazeSpeed: %w", speedErr)
		}
		packets = append(packets, speedPackets...)
	}
	return packets, nil
}

func campaignNPCDebuffDescriptor(modifierName string) uint32 {
	normalized := strings.ToLower(strings.TrimSpace(modifierName))
	if strings.Contains(normalized, "root") || strings.Contains(normalized, "entangle") {
		return 1
	}
	if strings.Contains(normalized, "slow") || strings.Contains(normalized, "stagnant") || normalized == "chilled" {
		return 2
	}
	return 0
}

func (r campaignNPCActionRuntime) rollbackCampaignNPCTimedModifier(
	sessionKey string, generation uint64, run *campaignNPCModifierRun,
) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.campaignNPCModifiers[run.instanceID] == run
	if isCurrent {
		peerSession.untrackCampaignNPCModifier(run)
		if peerSession.zone != nil && peerSession.zone.Effect() != nil {
			peerSession.zone.Effect().Remove(run.instanceID)
		}
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if isCurrent {
		_, _ = run.release(r.modifierPool)
	}
}

// Indefinite cages retire through their source/target lifecycle, not a deadline.
func operativeIndefiniteCancel() {}
