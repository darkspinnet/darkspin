package gameplay

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The targeted-area run supplies the authored spawn, pulse and removal times.
// Its Arena target adapter commits each pulse against current opposing heroes.
// The registry mutex is held on entry and released before returning.
func (e targetedAOESchedule) produceArenaFireSupportLocked(
	peerSession gameplayPeerSession, deadline time.Duration,
) ([][]byte, error) {
	outputPackets, pulses, err := e.run.Advance(context.Background(), deadline)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("arenaFireAdvance: %w", err)
	}
	finishedTargets := make([]*arenaAbilityTargets, 0, len(pulses))
	for _, pulse := range pulses {
		timestamp := e.packet.SourceTime + uint64(deadline/time.Millisecond)
		targets := e.runtime.registry.abilityTargetsLocked(
			e.runtime, peerSession, e.definition, timestamp,
		).(*arenaAbilityTargets)
		plan, planErr := zoneability.PlanTargetedAOEPulse(
			targets, e.sourceObjectID,
			game.Vec3{X: pulse.Position.X, Y: pulse.Position.Y, Z: pulse.Position.Z},
			e.creature, e.definition, pulse.DamageMinimum, pulse.DamageMaximum,
			pulse.DamageCoefficient,
		)
		if planErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaFirePlan: %w", planErr)
		}
		allTargets := append([]uint32(nil), fireSupportTargetIDs(plan.Target)...)
		if pulse.IsFirstTargetOnly && len(plan.Target) > 1 {
			plan.Target = plan.Target[:1]
		}
		random, randomErr := e.runtime.registry.abilityRandomLocked(peerSession)
		if randomErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaFireRandom: %w", randomErr)
		}
		results, commitErr := zoneability.CommitArea(
			random, targets, plan, e.creature, peerSession.binding.Difficulty,
			e.runtime.program.Critical,
		)
		if commitErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaFireCommit: %w", commitErr)
		}
		for _, targetObjectID := range allTargets {
			isDefeated := false
			for _, result := range results {
				if result.Damage.ObjectID == targetObjectID && result.Damage.IsDefeated {
					isDefeated = true
					break
				}
			}
			if isDefeated {
				continue
			}
			statusErr := targets.applyFireSupportReductionLocked(targetObjectID, timestamp)
			if statusErr != nil {
				e.runtime.registry.mutex.Unlock()
				return nil, fmt.Errorf("arenaFireReduction: %w", statusErr)
			}
		}
		impactPackets, packetErr := targets.packets(results)
		if packetErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("arenaFirePackets: %w", packetErr)
		}
		outputPackets = append(outputPackets, impactPackets...)
		finishedTargets = append(finishedTargets, targets)
	}
	if deadline == e.finalDeadline {
		current := e.runtime.registry.sessions[e.sessionKey]
		current.sphereAttack = nil
		current.sphereObjectID = 0
		e.run.ClearCancel()
		e.runtime.registry.sessions[e.sessionKey] = current
	}
	e.runtime.registry.mutex.Unlock()
	for _, targets := range finishedTargets {
		targets.finish()
	}
	if deadline == e.definition.ReleaseDelay && len(e.releaseResponse) != 0 {
		outputPackets = append(outputPackets, e.releaseResponse)
	}
	return outputPackets, nil
}

func fireSupportTargetIDs(targets []zonenpc.Snapshot) []uint32 {
	objectIDs := make([]uint32, 0, len(targets))
	for _, target := range targets {
		objectIDs = append(objectIDs, target.Plan.ObjectID)
	}
	return objectIDs
}

func (e *arenaAbilityTargets) applyFireSupportReductionLocked(objectID uint32, timestamp uint64) error {
	if e.runtime.registry.timer == nil {
		return fmt.Errorf("arenaFireTimer: timer unavailable")
	}
	key, target, isFound := e.member(objectID)
	if !isFound || target.isHeroDebuffImmune(objectID) || target.isOverdriveActiveAt(e.runtime.now()) {
		return nil
	}
	statusKey := arenaHitStatusKey{modifierID: util.HashID("FireTempestHealDebuff"),
		sourceObjectID: e.source.deployedObjectID}
	previous := target.arenaHitStatuses[statusKey]
	if previous != nil && previous.matches(target) && previous.kind == arenaBasicHealingReduction &&
		e.runtime.now().Before(previous.expiresAt) {
		err := previous.updateBasic(&target, timestamp, 1)
		if err != nil {
			return fmt.Errorf("arenaFireRefresh: %w", err)
		}
		e.runtime.registry.sessions[key] = target
		return nil
	}
	status, createPacket, err := e.prepareBasicStatusLocked(
		key, target, objectID, "FireTempestHealDebuff", arenaBasicHealingReduction,
		1, 1, e.definition.StatusDuration, timestamp,
	)
	if err != nil {
		return fmt.Errorf("arenaFireStatus: %w", err)
	}
	status.isCasterUnique = true
	if target.arenaHitStatuses == nil {
		target.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
	}
	if previous != nil {
		previous.retire(&target)
	}
	target.arenaHitStatuses[statusKey] = status
	attributePackets, err := status.refresh(&target)
	if err != nil {
		delete(target.arenaHitStatuses, statusKey)
		rollbackErr := status.discard("arenaFireRollback", err)
		e.runtime.registry.sessions[key] = target
		return fmt.Errorf("arenaFireAttributes: %w", rollbackErr)
	}
	status.queue(&target, append([][]byte{createPacket}, attributePackets...))
	e.runtime.registry.sessions[key] = target
	return nil
}
