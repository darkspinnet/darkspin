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
	return [][]byte{packet}, nil
}

func (r campaignNPCActionRuntime) applyCampaignNPCTimedModifier(
	packet raknet.Packet,
	sessionKey string,
	generation uint64,
	plan zonenpc.AttackPlan,
	timestamp uint64,
) ([][]byte, error) {
	profile := plan.Profile
	if sessionKey == "" || generation == 0 || plan.SourceObjectID == 0 ||
		plan.TargetObjectID == 0 || profile.ModifierName == "" ||
		profile.ModifierDuration <= 0 {
		return nil, errors.New("enemy timed modifier request invalid")
	}
	isRoot := profile.ModifierName == "EntangleModifier" ||
		profile.ModifierName == "VerdanthBasicRootmobModifier"
	if (profile.ModifierName == "SleepModifier" ||
		profile.ModifierName == "StalkerShock" ||
		profile.ModifierName == "CryosBossShock" ||
		profile.ModifierName == "SilenceModifier" || isRoot) && r.now == nil {
		return nil, errors.New("enemy modifier clock unavailable")
	}
	run, err := newCampaignNPCModifierRun(r.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("enemyTimedModifierRun: %w", err)
	}
	run.record = zoneeffect.Modifier{
		InstanceID: run.instanceID, GUID: profile.ModifierGUID(),
		SourceObjectID: plan.SourceObjectID, TargetObjectID: plan.TargetObjectID,
		Rank: 1, Duration: profile.ModifierDuration,
		Kind: zoneeffect.ModifierKindDebuff, InitiatorObject: plan.SourceObjectID,
		MovementSpeedBuff: profile.MovementSpeedBuff,
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
	if isCurrent && r.isTargetDebuffImmuneLocked(&peerSession, plan.TargetObjectID) {
		isCurrent = false
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
			Duration:       profile.ModifierDuration,
			Timestamp:      timestamp,
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
	cancel, scheduleErr := scheduleNPCProducers(r.registry, packet, []raknet.ScheduledPacketProducer{{
		Delay: profile.ModifierDuration, Produce: expiry.produce,
	}})
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
			r.registry.sessions[targetSessionKey] = targetSession
		}
		r.registry.mutex.Unlock()
	}
	if profile.ModifierName == "StalkerShock" ||
		profile.ModifierName == "CryosBossShock" {
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
			stopPackets, stopErr := marshalZonePlayerStop(
				plan.TargetObjectID, rootPosition,
			)
			if stopErr != nil {
				r.registry.mutex.Unlock()
				return nil, fmt.Errorf("enemyRootStopMarshal: %w", stopErr)
			}
			rootStopPackets = append(rootStopPackets, stopPackets...)
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
	return packets, nil
}

func campaignNPCDebuffDescriptor(modifierName string) uint32 {
	normalized := strings.ToLower(strings.TrimSpace(modifierName))
	if strings.Contains(normalized, "root") || strings.Contains(normalized, "entangle") {
		return 1
	}
	if strings.Contains(normalized, "slow") || strings.Contains(normalized, "stagnant") {
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
