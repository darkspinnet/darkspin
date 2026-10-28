package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

const arenaSporogenesisHot sim.AbilityStatusKind = "sporogenesis_hot"

type arenaSporogenesisStep struct {
	schedule heroHealingTicksSchedule
	status   *arenaHitStatus
	index    uint32
}

// The campaign HoT belongs to each recipient after the 0.1-second area hit.
// Arena keeps that ownership on the recipient's status map, so a hero swap,
// defeat, or transport retirement cancels later healing and its modifier.
func (e heroHealingTicksSchedule) startArenaSporogenesis(deadline time.Duration) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	source, isFound := e.runtime.registry.sessions[e.sessionKey]
	match := e.runtime.registry.arenaMatches[e.binding.GameID]
	if !e.isCurrent(source, isFound) || source.binding.Mode != game.ModeArena ||
		!source.isArenaCombatActive || source.deployedHitPoint() <= 0 ||
		match == nil || !match.isCombatActive || match.random == nil || e.runtime.registry.timer == nil {
		return nil, nil
	}
	source.heroHealingTicks = nil
	e.runtime.registry.sessions[e.sessionKey] = source
	timestamp := e.packet.SourceTime + uint64(deadline/time.Millisecond)
	asset := util.HashID(e.definition.HealEffectName)
	objectID := e.sourceObjectID
	cloudPacket, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{
		Asset: &asset, ObjectID: &objectID,
	})
	if err != nil {
		return nil, fmt.Errorf("arenaSporoCloud: %w", err)
	}
	e.runtime.registry.queueArenaForcePulsePacketsLocked(source.binding.GameID, [][]byte{cloudPacket})
	center := game.Vec3(source.playerPosition)
	for key, member := range e.runtime.registry.sessions {
		if member.binding.Mode != game.ModeArena || member.binding.GameID != source.binding.GameID ||
			member.binding.Team != source.binding.Team || !member.isArenaCombatActive ||
			member.deployedHitPoint() <= 0 || member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) {
			continue
		}
		positionErr := member.advancePlayerPosition(e.runtime.now(), raknet.Vector3{})
		if positionErr != nil {
			if e.runtime.logger != nil {
				e.runtime.logger.Printf("Arena Sporogenesis position unavailable target=%d: %v", member.deployedObjectID, positionErr)
			}
			continue
		}
		e.runtime.registry.sessions[key] = member
		if game.Vec3(member.playerPosition).Sub(center).Length() > e.definition.Radius {
			continue
		}
		applyErr := e.applyArenaSporogenesisLocked(key, member, timestamp)
		if applyErr != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena Sporogenesis recipient failed target=%d: %v", member.deployedObjectID, applyErr)
		}
	}
	return nil, nil
}

func (e heroHealingTicksSchedule) applyArenaSporogenesisLocked(
	key string, member gameplayPeerSession, timestamp uint64,
) error {
	duration := time.Second + time.Duration(e.definition.NumberOfTicks-1)*e.definition.TickDuration
	status := &arenaHitStatus{
		runtime: e.runtime, targetKey: key, gameID: member.binding.GameID,
		objectID: member.deployedObjectID, index: member.deployedCreatureIndex,
		noun:       member.binding.Creatures[member.deployedCreatureIndex].Noun,
		generation: member.generation, transportGeneration: member.transportGeneration,
		squad: member.squad, sourceObjectID: e.sourceObjectID,
		modifierID: e.definition.RootModifierID, kind: arenaSporogenesisHot,
		isCasterUnique: true, rank: 1, stackCount: 1,
		duration: duration, expiresAt: e.runtime.now().Add(duration), revision: 1,
	}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return fmt.Errorf("arenaSporoAllocate: %w", err)
	}
	status.instanceID = instanceID
	createPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: status.objectID, ModifierGUID: status.modifierID,
		InstanceID: instanceID, SourceID: e.sourceObjectID, StackCount: 1,
		StartMilliseconds: timestamp, DurationMilliseconds: uint32(duration / time.Millisecond),
	})
	if err != nil {
		return status.discard("arenaSporoCreate", err)
	}
	if e.definition.HitEffectName == "" {
		return status.discard("arenaSporoImpactMissing", errors.New("hit effect unavailable"))
	}
	impactPacket, err := raknet.MarshalApplication(raknet.ServerEventMessage{
		Asset: util.HashID(e.definition.HitEffectName), ObjectID: status.objectID,
	})
	if err != nil {
		return status.discard("arenaSporoImpact", err)
	}
	expiry := arenaHitStatusExpiry{status: status, revision: status.revision}
	status.cancel, err = e.runtime.registry.timer.Schedule(duration, expiry.expire)
	if err == nil && status.cancel == nil {
		err = errors.New("HoT expiry cancellation unavailable")
	}
	if err != nil {
		return status.discard("arenaSporoExpiry", err)
	}
	tickCancels := make([]func(), 0, e.definition.NumberOfTicks-1)
	for index := uint32(1); index < e.definition.NumberOfTicks; index++ {
		step := arenaSporogenesisStep{schedule: e, status: status, index: index}
		cancel, scheduleErr := e.runtime.registry.timer.Schedule(time.Duration(index)*e.definition.TickDuration, step.execute)
		if scheduleErr == nil && cancel == nil {
			scheduleErr = errors.New("HoT tick cancellation unavailable")
		}
		if scheduleErr != nil {
			if cancel != nil {
				cancel()
			}
			for _, pendingCancel := range tickCancels {
				pendingCancel()
			}
			status.isRetired = true
			return status.discard("arenaSporoTickSchedule", scheduleErr)
		}
		tickCancels = append(tickCancels, cancel)
	}
	for _, old := range member.arenaHitStatuses {
		if old != nil && old.kind != arenaBasicHaste && old.kind != arenaSporogenesisHot {
			old.retire(&member)
		}
	}
	if member.arenaHitStatuses == nil {
		member.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
	}
	if previous := member.arenaHitStatuses[status.key()]; previous != nil {
		previous.retire(&member)
	}
	member.arenaHitStatuses[status.key()] = status
	e.runtime.registry.sessions[key] = member
	e.runtime.registry.queueArenaForcePulsePacketsLocked(member.binding.GameID, [][]byte{impactPacket, createPacket})
	return e.healArenaSporogenesisLocked(status)
}

func (e arenaSporogenesisStep) execute() {
	e.schedule.runtime.registry.mutex.Lock()
	defer e.schedule.runtime.registry.mutex.Unlock()
	if e.status.isRetired {
		return
	}
	err := e.schedule.healArenaSporogenesisLocked(e.status)
	if err != nil && e.schedule.runtime.logger != nil {
		e.schedule.runtime.logger.Printf("Arena Sporogenesis tick failed target=%d tick=%d: %v", e.status.objectID, e.index, err)
	}
}

func (e heroHealingTicksSchedule) healArenaSporogenesisLocked(status *arenaHitStatus) error {
	member, isFound := e.runtime.registry.sessions[status.targetKey]
	match := e.runtime.registry.arenaMatches[status.gameID]
	if !isFound || !status.matches(member) || status.isRetired ||
		member.arenaHitStatuses[status.key()] != status || member.deployedHitPoint() <= 0 ||
		match == nil || !match.isCombatActive || match.random == nil {
		return nil
	}
	healing := float32(e.definition.MinimumHealingPerTick)
	if e.definition.MaximumHealingPerTick != e.definition.MinimumHealingPerTick {
		healing += (e.definition.MaximumHealingPerTick - healing) * float32(match.random.Float64())
	}
	projected, err := zoneability.ProjectHealing(e.creature, e.definition, healing)
	if err != nil {
		return fmt.Errorf("arenaSporoProject: %w", err)
	}
	isCritical, err := sim.RollCritical(match.random, e.creature.CriticalRating,
		e.creature.AutoCrit, e.binding.Difficulty, e.runtime.program.Critical.RatingConversions)
	if err != nil {
		return fmt.Errorf("arenaSporoCritical: %w", err)
	}
	if isCritical {
		projected *= e.runtime.program.Critical.DamageBonus
	}
	healing, err = game.ApplyTargetHealingReduction(projected, member.healingTargetProfile(status.index))
	if err != nil {
		return fmt.Errorf("arenaSporoReduction: %w", err)
	}
	before := member.deployedHitPoint()
	maximum, maximumMana := member.characterResourceMaximum(status.index)
	if maximum <= 0 || maximumMana < 0 {
		return errors.New("Arena Sporogenesis hero maximum unavailable")
	}
	hitPoint := min(maximum, before+healing)
	amount := hitPoint - before
	packets, err := supportHealerHealingPackets(e.sourceObjectID, status.objectID, before, hitPoint, healing, isCritical)
	if err != nil {
		return fmt.Errorf("arenaSporoFeedback: %w", err)
	}
	resourcePacket, err := member.marshalCampaignCharacterResourceAt(status.index, hitPoint)
	if err != nil {
		return fmt.Errorf("arenaSporoResource: %w", err)
	}
	if amount > 0 {
		isGameOver, commitErr := member.setCampaignCharacterHitPoints(status.index, hitPoint)
		if commitErr != nil {
			return fmt.Errorf("arenaSporoCommit: %w", commitErr)
		}
		if isGameOver {
			return errors.New("Arena Sporogenesis ended squad")
		}
		match.result.recordHealing(e.binding.UserID, member.binding.UserID, amount)
	}
	e.runtime.registry.sessions[status.targetKey] = member
	packets = append(packets, resourcePacket)
	e.runtime.registry.queueArenaForcePulsePacketsLocked(status.gameID, packets)
	return nil
}
