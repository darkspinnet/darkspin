package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

type arenaHitStatusKey struct {
	modifierID     uint32
	sourceObjectID uint32
}

const arenaBasicEnergyVulnerability sim.AbilityStatusKind = "basic_energy_vulnerability"
const arenaFireEnergyVulnerability sim.AbilityStatusKind = "fire_energy_vulnerability"
const arenaBasicWeakness sim.AbilityStatusKind = "basic_weakness"
const arenaBasicPummel sim.AbilityStatusKind = "basic_pummel"
const arenaBasicBurn sim.AbilityStatusKind = "basic_burn"
const arenaBasicHaste sim.AbilityStatusKind = "basic_haste"
const arenaBasicHealingReduction sim.AbilityStatusKind = "basic_healing_reduction"
const arenaChargeRoot sim.AbilityStatusKind = "root"
const arenaVoodooCurse sim.AbilityStatusKind = "voodoo_curse"
const arenaInfectionDisease sim.AbilityStatusKind = "infection_disease"

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
	rank                uint32
	stackCount          uint32
	movementScale       float32
	attackScale         float32
	physicalIncrease    float32
	damageBuff          float32
	energyIncrease      float32
	periodic            *arenaBasicBurnRun
	affliction          *arenaAfflictionRun
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
		if status.matches(e) && (status.kind == sim.AbilityStatusKindStun || status.kind == sim.AbilityStatusKindBanish) && at.Before(status.expiresAt) {
			return true
		}
	}
	return false
}

func (e gameplayPeerSession) isArenaBanished(at time.Time) bool {
	for _, status := range e.arenaHitStatuses {
		if status.matches(e) && status.kind == sim.AbilityStatusKindBanish && at.Before(status.expiresAt) {
			return true
		}
	}
	return false
}

func (e gameplayPeerSession) isArenaHitRootActive(at time.Time) bool {
	for _, status := range e.arenaHitStatuses {
		if status.matches(e) && status.kind == arenaChargeRoot && at.Before(status.expiresAt) {
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
	amount += float32(e.fixtureSilenceCount())
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

func (e gameplayPeerSession) arenaBasicEnergyIncrease(at time.Time) float32 {
	status := e.arenaHitStatuses[arenaHitStatusKey{modifierID: util.HashID("Modifier_LightningTempest_Basic")}]
	if status == nil || !status.matches(e) || status.kind != arenaBasicEnergyVulnerability || !at.Before(status.expiresAt) {
		return 0
	}
	return 0.1 * float32(status.stackCount)
}

func (e gameplayPeerSession) arenaFireEnergyIncrease(at time.Time) float32 {
	status := e.arenaHitStatuses[arenaHitStatusKey{modifierID: util.HashID("FireRavagerEnergyVulnerabilityModifier")}]
	if status == nil || !status.matches(e) || status.kind != arenaFireEnergyVulnerability || !at.Before(status.expiresAt) {
		return 0
	}
	return 0.5 * float32(status.stackCount)
}

func (e gameplayPeerSession) arenaBasicWeakness(at time.Time) float32 {
	status := e.arenaHitStatuses[arenaHitStatusKey{modifierID: util.HashID("WeaknessDebuff")}]
	if status == nil || !status.matches(e) || status.kind != arenaBasicWeakness || !at.Before(status.expiresAt) {
		return 0
	}
	if status.rank >= 2 {
		return 0.15
	}
	return 0.25
}

func (e gameplayPeerSession) arenaBasicHasteIncrease(at time.Time) float32 {
	status := e.arenaHitStatuses[arenaHitStatusKey{modifierID: util.HashID("LightSpeedHaste")}]
	if status == nil || !status.matches(e) || status.kind != arenaBasicHaste || !at.Before(status.expiresAt) {
		return 0
	}
	return 0.08 * float32(status.stackCount)
}

func (e gameplayPeerSession) arenaPhysicalVulnerability(at time.Time) float32 {
	curse := e.arenaHitStatuses[arenaHitStatusKey{modifierID: util.HashID("VoodooTempestWeaken")}]
	curseIncrease := float32(0)
	if curse != nil && curse.matches(e) && curse.kind == arenaVoodooCurse && at.Before(curse.expiresAt) {
		curseIncrease = curse.physicalIncrease
	}
	status := e.arenaHitStatuses[arenaHitStatusKey{modifierID: util.HashID("ShadowRavagerPhysicalVulnerabilityModifier")}]
	if status == nil || !status.matches(e) || status.kind != sim.AbilityStatusKindPhysicalVulnerability || !at.Before(status.expiresAt) {
		return curseIncrease
	}
	return status.physicalIncrease + curseIncrease
}

func (e gameplayPeerSession) arenaVoodooEnergyIncrease(at time.Time) float32 {
	status := e.arenaHitStatuses[arenaHitStatusKey{modifierID: util.HashID("VoodooTempestWeaken")}]
	if status == nil || !status.matches(e) || status.kind != arenaVoodooCurse || !at.Before(status.expiresAt) {
		return 0
	}
	return status.energyIncrease
}

func (e gameplayPeerSession) arenaVoodooDamageBuff(at time.Time) float32 {
	status := e.arenaHitStatuses[arenaHitStatusKey{modifierID: util.HashID("VoodooTempestWeaken")}]
	if status == nil || !status.matches(e) || status.kind != arenaVoodooCurse || !at.Before(status.expiresAt) {
		return 0
	}
	return status.damageBuff
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
		definition.StatusKind != sim.AbilityStatusKindSilence && definition.StatusKind != sim.AbilityStatusKindBanish &&
		definition.StatusKind != sim.AbilityStatusKindPhysicalVulnerability &&
		(definition.StatusKind != sim.AbilityStatusKindTaunt ||
			(definition.Name != "TechRandom3" && definition.Name != "TCShieldedSentinelActive" && definition.Name != "BioRandom2")) &&
		(definition.StatusKind != arenaChargeRoot ||
			(definition.Name != "EntanglingRush" && definition.Name != supportHealerName && definition.Name != "RootingPlague")) &&
		(definition.StatusKind != sim.AbilityStatusKindCurse || definition.Name != "VoodooTempestSupport") {
		return nil, nil
	}
	duration := definition.StatusDuration
	if definition.Name == "BioRandom2" {
		duration = definition.TickDuration
	}
	if definition.Name == "VoodooTempestSupport" {
		duration = definition.Duration
	}
	if definition.Name == "SpacetimeRandom" {
		duration = definition.Duration
	}
	if duration <= 0 || definition.RootModifierID == 0 {
		return nil, nil
	}
	if definition.StatusKind == sim.AbilityStatusKindPhysicalVulnerability && definition.Name != "ShadowRavagerActive" {
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
		if definition.StatusKind == sim.AbilityStatusKindBanish && member.isArenaBanished(e.now()) {
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
			physicalIncrease: definition.StatusPhysicalIncrease,
			damageBuff:       definition.StatusDamageBuff, energyIncrease: definition.StatusEnergyIncrease,
			isCasterUnique: (definition.Name == "TCShieldedSentinelSupport" || definition.Name == "MissileTempestSupport") && definition.StatusKind == sim.AbilityStatusKindSlow,
			duration:       duration, revision: 1,
			expiresAt: e.now().Add(duration)}
		if definition.Name == "VoodooTempestSupport" {
			status.kind = arenaVoodooCurse
		}
		instanceID, err := e.modifierPool.Allocate()
		if err != nil {
			return nil, fmt.Errorf("arenaStatusAllocate: %w", err)
		}
		status.instanceID = instanceID
		createPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
			TargetID: objectID, ModifierGUID: status.modifierID, InstanceID: instanceID,
			DurationMilliseconds: uint32(duration.Milliseconds()), StackCount: 1,
			StartMilliseconds: timestamp, NPCAffixAssetID: 0})
		if err != nil {
			return nil, status.discard("arenaStatusCreate", err)
		}
		expiry := arenaHitStatusExpiry{status: status, revision: status.revision}
		status.cancel, err = e.registry.timer.Schedule(duration, expiry.expire)
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
		if status.kind == sim.AbilityStatusKindStun || status.kind == sim.AbilityStatusKindBanish || status.kind == arenaChargeRoot {
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
				status.gameID, sourceObjectID, objectID, status.modifierID, instanceID, status.kind, duration)
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
	if e.kind == arenaBasicEnergyVulnerability || e.kind == arenaFireEnergyVulnerability || e.kind == arenaBasicWeakness ||
		e.kind == arenaBasicPummel || e.kind == arenaBasicBurn || e.kind == sim.AbilityStatusKindPhysicalVulnerability ||
		e.kind == sim.AbilityStatusKindTaunt || e.kind == arenaVoodooCurse || e.kind == arenaPoisonNova ||
		e.kind == arenaHeroFear ||
		(e.kind == sim.AbilityStatusKindCurse && e.modifierID == util.HashID("AfflictionCurse")) ||
		e.kind == arenaSporogenesisHot || e.kind == arenaInfectionDisease {
		return nil, nil
	}
	if e.kind == arenaBasicHaste {
		packet, err := member.marshalHeroSlowAttributes(member.deployedObjectID, member.enemyAttackSpeed(), member.enemyMovementSpeedBuff())
		if err != nil {
			return nil, fmt.Errorf("hasteAttributes: %w", err)
		}
		if member.playerMotion != nil {
			increase := max(float32(-0.9), e.runtime.registry.passiveMovementIncrease(*member)+member.enemyMovementSpeedBuff())
			err = member.playerMotion.SetSpeed(e.runtime.now(), zonePlayerMoveSpeed*(1+increase))
			if err != nil {
				return nil, fmt.Errorf("hasteMotion: %w", err)
			}
		}
		return [][]byte{packet}, nil
	}
	if e.kind == arenaBasicHealingReduction {
		profile := member.healingTargetProfile(member.deployedCreatureIndex)
		packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: member.deployedObjectID, Value: map[uint8]float32{30: profile.HealingReduction},
		})
		if err != nil {
			return nil, fmt.Errorf("healingReductionAttribute: %w", err)
		}
		return [][]byte{packet}, nil
	}
	if e.kind == sim.AbilityStatusKindSilence {
		packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: member.deployedObjectID, Value: map[uint8]float32{14: member.arenaSilenceAmount(e.runtime.now())},
		})
		if err != nil {
			return nil, fmt.Errorf("statusSilence: %w", err)
		}
		return [][]byte{packet}, nil
	}
	if e.kind == sim.AbilityStatusKindStun || e.kind == sim.AbilityStatusKindBanish || e.kind == sim.AbilityStatusKindSleep || e.kind == arenaChargeRoot {
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
