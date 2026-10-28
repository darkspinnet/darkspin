package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

type arenaHitStatusKey struct {
	modifierID     uint32
	sourceObjectID uint32
}

// Accepted modifiers belong to the recipient. Build 103 PlasmaRandomModifier
// is Unique; Dazing Shot inherits CasterUnique from the speed template.
// Both deactivate on recipient death, not when their caster switches.
// All operations below run with the gameplay registry locked.
type arenaHitStatus struct {
	runtime             campaignAbilityCommandRuntime
	targetKey           string
	gameID              uint32
	objectID            uint32
	index               uint32
	noun                uint32
	generation          uint64
	transportGeneration uint64
	squad               *squad.Session
	sourceObjectID      uint32
	modifierID          uint32
	instanceID          uint32
	kind                sim.AbilityStatusKind
	movementScale       float32
	attackScale         float32
	duration            time.Duration
	expiresAt           time.Time
	revision            uint64
	cancel              func()
	isRetired           bool
	isCasterUnique      bool
}

func (e *arenaHitStatus) key() arenaHitStatusKey {
	key := arenaHitStatusKey{modifierID: e.modifierID}
	if e.isCasterUnique {
		key.sourceObjectID = e.sourceObjectID
	}
	return key
}

func (e *arenaHitStatus) matches(member gameplayPeerSession) bool {
	return !e.isRetired && member.binding.Mode == game.ModeArena && member.binding.GameID == e.gameID &&
		member.generation == e.generation && member.transportGeneration == e.transportGeneration &&
		member.squad == e.squad && member.deployedObjectID == e.objectID && member.deployedCreatureIndex == e.index &&
		e.index < uint32(len(member.binding.Creatures)) && member.binding.Creatures[e.index].Noun == e.noun
}

func (e gameplayPeerSession) isArenaHitStunActive(at time.Time) bool {
	for _, status := range e.arenaHitStatuses {
		if status.matches(e) && status.kind == sim.AbilityStatusKindStun && at.Before(status.expiresAt) {
			return true
		}
	}
	return false
}

func (e gameplayPeerSession) isArenaHitSilenceActive(at time.Time) bool {
	for _, status := range e.arenaHitStatuses {
		if status.matches(e) && status.kind == sim.AbilityStatusKindSilence && at.Before(status.expiresAt) {
			return true
		}
	}
	return false
}

func (e gameplayPeerSession) arenaSilenceAmount(at time.Time) float32 {
	amount := e.projectPassiveCreature(e.deployedCreatureIndex, at).PartAttribute[14]
	if at.Before(e.enemySilenceExpiresAt) {
		amount++
	}
	for _, status := range e.arenaHitStatuses {
		if status.matches(e) && status.kind == sim.AbilityStatusKindSilence && at.Before(status.expiresAt) {
			amount++
		}
	}
	return amount
}

func (e gameplayPeerSession) arenaHitSlowBuffs(at time.Time) (float32, float32) {
	var movement, attack float32
	for _, status := range e.arenaHitStatuses {
		if status.matches(e) && status.kind == sim.AbilityStatusKindSlow && at.Before(status.expiresAt) {
			movement += status.movementScale - 1
			attack += status.attackScale - 1
		}
	}
	return movement, attack
}

func (e campaignAbilityCommandRuntime) applyArenaTargetStatus(sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, plan zoneability.AreaPlan, targetObjectIDs []uint32,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	return e.applyArenaTargetStatusLocked(sessionKey, generation, sourceObjectID, timestamp, plan, targetObjectIDs)
}

func (e campaignAbilityCommandRuntime) applyArenaTargetStatusLocked(sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, plan zoneability.AreaPlan, targetObjectIDs []uint32,
) ([][]byte, error) {
	definition := plan.Definition
	if definition.StatusKind != sim.AbilityStatusKindSlow && definition.StatusKind != sim.AbilityStatusKindStun &&
		definition.StatusKind != sim.AbilityStatusKindSilence {
		return nil, nil
	}
	if definition.StatusDuration <= 0 || definition.RootModifierID == 0 {
		return nil, nil
	}
	if e.registry.timer == nil {
		return nil, errors.New("arena status timer unavailable")
	}
	source, isFound := e.registry.sessions[sessionKey]
	match := e.registry.arenaMatches[source.binding.GameID]
	if e.registry.isClosed || !isFound || source.binding.Mode != game.ModeArena || source.generation != generation || source.deployedObjectID != sourceObjectID ||
		source.deployedHitPoint() <= 0 || !source.isArenaCombatActive || match == nil || !match.isCombatActive {
		return nil, nil
	}
	targets := arenaAbilityTargets{runtime: e, source: source}
	for _, objectID := range targetObjectIDs {
		key, member, isTargetFound := targets.member(objectID)
		if !isTargetFound || member.isHeroDebuffImmune(objectID) || member.isOverdriveActiveAt(e.now()) {
			continue
		}
		// Build-103 CanUseModifier chunks 453/549/53 reject their specific
		// immunity attributes only in IsChainGame. Arena is not that mode;
		// retain the independent universal debuff and Overdrive guards above.
		if definition.StatusKind == sim.AbilityStatusKindSilence {
			previous := member.arenaHitStatuses[arenaHitStatusKey{modifierID: definition.RootModifierID}]
			if previous != nil && previous.kind == sim.AbilityStatusKindSilence && previous.matches(member) && e.now().Before(previous.expiresAt) {
				err := previous.resetSilenceDuration(&member, timestamp)
				if err != nil {
					return nil, fmt.Errorf("arenaSilenceRefresh: %w", err)
				}
				e.registry.sessions[key] = member
				continue
			}
		}
		status := &arenaHitStatus{runtime: e, targetKey: key, gameID: source.binding.GameID,
			objectID: objectID, index: member.deployedCreatureIndex, generation: member.generation,
			transportGeneration: member.transportGeneration, squad: member.squad,
			noun:           member.binding.Creatures[member.deployedCreatureIndex].Noun,
			sourceObjectID: sourceObjectID,
			modifierID:     definition.RootModifierID, kind: definition.StatusKind,
			movementScale: definition.StatusMovementScale, attackScale: definition.StatusAttackScale,
			isCasterUnique: (definition.Name == "TCShieldedSentinelSupport" || definition.Name == "MissileTempestSupport") && definition.StatusKind == sim.AbilityStatusKindSlow,
			duration:       definition.StatusDuration, revision: 1,
			expiresAt: e.now().Add(definition.StatusDuration)}
		instanceID, err := e.modifierPool.Allocate()
		if err != nil {
			return nil, fmt.Errorf("arenaStatusAllocate: %w", err)
		}
		status.instanceID = instanceID
		createPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
			TargetID: objectID, ModifierGUID: status.modifierID, InstanceID: instanceID,
			DurationMilliseconds: uint32(definition.StatusDuration.Milliseconds()), StackCount: 1,
			StartMilliseconds: timestamp, SourceID: sourceObjectID})
		if err != nil {
			return nil, status.discard("arenaStatusCreate", err)
		}
		expiry := arenaHitStatusExpiry{status: status, revision: status.revision}
		status.cancel, err = e.registry.timer.Schedule(definition.StatusDuration, expiry.expire)
		if err == nil && status.cancel == nil {
			err = errors.New("status expiry cancellation unavailable")
		}
		if err != nil {
			return nil, status.discard("arenaStatusSchedule", err)
		}
		if member.arenaHitStatuses == nil {
			member.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
		}
		if previous := member.arenaHitStatuses[status.key()]; previous != nil {
			previous.retire(&member)
		}
		member.arenaHitStatuses[status.key()] = status
		packets := [][]byte{createPacket}
		if status.kind == sim.AbilityStatusKindStun {
			stopPackets, stopErr := stopEnemyControlledHeroMovement(&member, e.now())
			if stopErr != nil {
				e.registry.sessions[key] = member
				status.retire(&member)
				e.registry.sessions[key] = member
				return nil, fmt.Errorf("arenaStatusStop: %w", stopErr)
			}
			packets = append(packets, stopPackets...)
		}
		attributePackets, attributeErr := status.refresh(&member)
		if attributeErr != nil {
			status.retire(&member)
			e.registry.sessions[key] = member
			return nil, fmt.Errorf("arenaStatusAttribute: %w", attributeErr)
		}
		packets = append(packets, attributePackets...)
		status.queue(&member, packets)
		e.registry.sessions[key] = member
		if e.logger != nil {
			e.logger.Printf("Arena accepted-hit status game=%d source=%d target=%d modifier=%d instance=%d kind=%s duration=%s",
				status.gameID, sourceObjectID, objectID, status.modifierID, instanceID, status.kind, definition.StatusDuration)
		}
	}
	return nil, nil
}

func (e *arenaHitStatus) discard(step string, cause error) error {
	if e.cancel != nil {
		e.cancel()
	}
	err := e.runtime.modifierPool.Release(e.instanceID)
	return fmt.Errorf("%s: %w", step, errors.Join(cause, err))
}

func (e *arenaHitStatus) queue(member *gameplayPeerSession, packets [][]byte) {
	member.queuePackets(packets)
	for key, candidate := range e.runtime.registry.sessions {
		if key == e.targetKey || candidate.binding.Mode != game.ModeArena || candidate.binding.GameID != e.gameID || candidate.isRejoinPending {
			continue
		}
		candidate.queuePackets(packets)
		e.runtime.registry.sessions[key] = candidate
	}
}

func (e *arenaHitStatus) refresh(member *gameplayPeerSession) ([][]byte, error) {
	if e.kind == sim.AbilityStatusKindSilence {
		packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: member.deployedObjectID, Value: map[uint8]float32{14: member.arenaSilenceAmount(e.runtime.now())},
		})
		if err != nil {
			return nil, fmt.Errorf("statusSilence: %w", err)
		}
		return [][]byte{packet}, nil
	}
	if e.kind == sim.AbilityStatusKindStun {
		packets, err := member.refreshEnemyImmobilizedAttributes(e.runtime.now())
		if err != nil {
			return nil, fmt.Errorf("statusControl: %w", err)
		}
		return packets, nil
	}
	if member.playerMotion != nil {
		increase := max(float32(-0.9), e.runtime.registry.passiveMovementIncrease(*member)+member.enemyMovementSpeedBuff())
		err := member.playerMotion.SetSpeed(e.runtime.now(), zonePlayerMoveSpeed*(1+increase))
		if err != nil {
			return nil, fmt.Errorf("statusMotion: %w", err)
		}
	}
	packet, err := member.marshalHeroSlowAttributes(member.deployedObjectID, member.enemyAttackSpeed(), member.enemyMovementSpeedBuff())
	if err != nil {
		return nil, fmt.Errorf("statusSlow: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *arenaHitStatus) retire(member *gameplayPeerSession) {
	if e.isRetired {
		return
	}
	isSameActor := e.matches(*member)
	e.isRetired = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if member.arenaHitStatuses[e.key()] == e {
		delete(member.arenaHitStatuses, e.key())
	}
	err := e.runtime.modifierPool.Release(e.instanceID)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("Arena status release failed instance=%d: %v", e.instanceID, err)
	}
	if !isSameActor {
		// An old callback may release its own instance, never present against
		// a replacement actor or a different transport/game.
		return
	}
	packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{TargetID: e.objectID, InstanceID: e.instanceID})
	if err != nil {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena status delete failed instance=%d: %v", e.instanceID, err)
		}
		return
	}
	packets := [][]byte{packet}
	attributePackets, attributeErr := e.refresh(member)
	if attributeErr != nil {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf("Arena status restoration failed instance=%d: %v", e.instanceID, attributeErr)
		}
	} else {
		packets = append(packets, attributePackets...)
	}
	e.queue(member, packets)
}

type arenaHitStatusExpiry struct {
	status   *arenaHitStatus
	revision uint64
}

func (e arenaHitStatusExpiry) expire() {
	status := e.status
	status.runtime.registry.mutex.Lock()
	defer status.runtime.registry.mutex.Unlock()
	if status.isRetired || status.revision != e.revision {
		return
	}
	member, isFound := status.runtime.registry.sessions[status.targetKey]
	if !isFound {
		status.cancel = nil
		// The connection has departed. There is no recipient actor to update,
		// but this owned instance still needs its single release.
		member = gameplayPeerSession{}
		status.retire(&member)
		return
	}
	if member.arenaHitStatuses[status.key()] != status {
		status.cancel = nil
		// A replacement session can reuse this endpoint without inheriting
		// its old status map. Release only the retired callback's instance.
		retiredMember := gameplayPeerSession{}
		status.retire(&retiredMember)
		return
	}
	status.cancel = nil
	status.retire(&member)
	status.runtime.registry.sessions[status.targetKey] = member
}

// Build 103 0x9E1390 dispatches Stacks into the existing modifier context;
// 0xA660A0 changes its start clock only. Keep creation duration/source/instance
// even when another caster requests the GUID with a different ability rank.
func (e *arenaHitStatus) resetSilenceDuration(member *gameplayPeerSession, timestamp uint64) error {
	update, err := raknet.MarshalApplication(raknet.ModifierUpdatedMessage{
		TargetID: e.objectID, InstanceID: e.instanceID, StartMilliseconds: int64(timestamp), StackCount: 1,
	})
	if err != nil {
		return fmt.Errorf("silenceUpdate: %w", err)
	}
	nextRevision := e.revision + 1
	expiry := arenaHitStatusExpiry{status: e, revision: nextRevision}
	cancel, err := e.runtime.registry.timer.Schedule(e.duration, expiry.expire)
	if err == nil && cancel == nil {
		err = errors.New("silence expiry cancellation unavailable")
	}
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return fmt.Errorf("silenceSchedule: %w", err)
	}
	previousCancel := e.cancel
	e.expiresAt = e.runtime.now().Add(e.duration)
	e.revision = nextRevision
	e.cancel = cancel
	e.queue(member, [][]byte{update})
	if previousCancel != nil {
		previousCancel()
	}
	return nil
}

func (e *gameplayPeerSession) retireArenaHitStatuses() {
	for _, run := range e.arenaHitPushes {
		run.retire(e)
	}
	e.arenaHitPushes = nil
	for _, status := range e.arenaHitStatuses {
		status.retire(e)
	}
	e.arenaHitStatuses = nil
}
