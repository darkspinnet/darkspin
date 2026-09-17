package gameplay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const shadowRavagerSupernaturalStealth = uint8(2)

func arborealMightMaximumStack(rank int32) uint32 {
	if rank%2 == 0 {
		return 2
	}
	return 5
}

func arborealMightDamagePerStack(rank int32) float32 {
	if rank%2 == 0 {
		return 0.20
	}
	return 0.10
}

type heroModifierRun struct {
	abilityID        uint32
	creatureIndex    uint32
	instanceID       uint32
	objectID         uint32
	stackCount       uint32
	damageBuff       float32
	autoCrit         float32
	isDamageImmune   bool
	isDebuffImmune   bool
	isSoulLink       bool
	isQuantumState   bool
	isArborealMight  bool
	isAbilityBreak   bool
	isShadowStealth  bool
	effectSlot       uint8
	isEffectAttached bool
	expiresAt        time.Time
	quantum          quantumStateRuntime
	cancel           raknet.CancelSchedule
}

func (e gameplayPeerSession) setShadowRavagerStealth(
	objectID uint32, isStealthed bool,
) ([]zonenpc.Snapshot, []byte, error) {
	if e.zone == nil || objectID == 0 {
		return nil, nil, errors.New("shadow stealth target unavailable")
	}
	acquired, err := e.zone.SetHeroStealthed(
		e.binding.UserID, e.generation, objectID, isStealthed,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("shadowStealthTargeting: %w", err)
	}
	stealth := uint8(0)
	if isStealthed {
		stealth = shadowRavagerSupernaturalStealth
	}
	packet, err := raknet.MarshalApplication(raknet.AgentBlackboardUpdateMessage{
		ObjectID: objectID, Stealth: stealth, IsTargetable: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("shadowStealthMarshal: %w", err)
	}
	return acquired, packet, nil
}

func scheduleShadowRavagerTargets(
	runtime campaignNPCActionRuntime, packet raknet.Packet,
	sessionKey string, generation uint64, acquired []zonenpc.Snapshot,
) ([][]byte, error) {
	plans := make([]zonenpc.SpawnPlan, 0, len(acquired))
	for _, target := range acquired {
		plans = append(plans, target.Plan)
	}
	packets, err := runtime.scheduleFirstActions(
		packet, sessionKey, generation, plans, packet.SourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("shadowStealthFirstAction: %w", err)
	}
	return packets, nil
}

func (e *heroModifierRun) releaseEffect(effectPool *attachedEffectPool) {
	if e == nil || !e.isEffectAttached || effectPool == nil {
		return
	}
	effectPool.Release(e.objectID, e.effectSlot)
	e.isEffectAttached = false
}

func (e *heroModifierRun) stopEffect(
	effectPool *attachedEffectPool,
) ([]byte, error) {
	if e == nil || !e.isEffectAttached || effectPool == nil {
		return nil, nil
	}
	packet, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{
		Slot: e.effectSlot + 1, IsRemovalRequested: true, IsHardStop: true,
		ObjectID: e.objectID,
	})
	if err != nil {
		return nil, fmt.Errorf("heroModifierEffectDelete: %w", err)
	}
	e.releaseEffect(effectPool)
	return packet, nil
}

func acceptedAbilityID(packets [][]byte) (uint32, bool) {
	for _, packet := range packets {
		if len(packet) < 9 || packet[0] != byte(raknet.ActionCommandResponse) ||
			raknet.ActionResponseType(packet[2]) != raknet.ActionResponseAccepted {
			continue
		}
		return binary.LittleEndian.Uint32(packet[5:9]), true
	}
	return 0, false
}

func (r campaignAbilityCommandRuntime) breakHeroModifierOnAcceptedAbility(
	packet raknet.Packet, sessionKey string, generation uint64, packets [][]byte,
) ([][]byte, error) {
	abilityID, isAccepted := acceptedAbilityID(packets)
	if !isAccepted {
		return nil, nil
	}

	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := peerSession.breakTrapperStealth()
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroModifierTrapperBreak: %w", err)
	}
	isHeroModifierBreak := peerSession.heroModifierRun != nil &&
		peerSession.heroModifierRun.isAbilityBreak &&
		peerSession.heroModifierRun.abilityID != abilityID
	if !isHeroModifierBreak {
		r.registry.sessions[sessionKey] = peerSession
		r.registry.mutex.Unlock()
		return packets, nil
	}
	run := peerSession.heroModifierRun
	modifierPacket, err := effectraknet.ModifierDelete(run.objectID, run.instanceID)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroModifierBreakDelete: %w", err)
	}
	run.Remove(&peerSession)
	acquired := []zonenpc.Snapshot(nil)
	stealthPacket := []byte(nil)
	if run.isShadowStealth {
		acquired, stealthPacket, err = peerSession.setShadowRavagerStealth(
			run.objectID, false,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroModifierBreakStealth: %w", err)
		}
	}
	peerSession.heroModifierRun = nil
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	run.Cancel()
	_ = r.modifierPool.Release(run.instanceID)
	r.logger.Printf(
		"RakNet hero modifier ended on accepted ability source=%d modifier=%d ability=%d",
		run.objectID, run.abilityID, abilityID,
	)
	targetPackets, err := scheduleShadowRavagerTargets(
		r.npc, packet, sessionKey, generation, acquired,
	)
	if err != nil {
		return nil, fmt.Errorf("heroModifierBreakTarget: %w", err)
	}
	packets = append(packets, modifierPacket)
	if stealthPacket != nil {
		packets = append(packets, stealthPacket)
	}
	packets = append(packets, targetPackets...)
	return packets, nil
}

func (e *heroModifierRun) Cancel() {
	if e == nil || e.cancel == nil {
		return
	}
	e.cancel()
	e.cancel = nil
}

func (e *heroModifierRun) IsDamageImmune() bool {
	return e != nil && e.isDamageImmune
}

func (e *heroModifierRun) IsDebuffImmune() bool {
	return e != nil && e.isDebuffImmune
}

func (e *heroModifierRun) Remove(peerSession *gameplayPeerSession) {
	if e == nil || peerSession == nil ||
		e.creatureIndex >= uint32(len(peerSession.binding.Creatures)) {
		return
	}
	profile := &peerSession.binding.Creatures[e.creatureIndex].DamageProfile
	profile.DamageBuff = max(float32(0), profile.DamageBuff-e.damageBuff)
	creature := &peerSession.binding.Creatures[e.creatureIndex]
	creature.AutoCrit = max(float32(0), creature.AutoCrit-e.autoCrit)
	e.damageBuff = 0
	e.autoCrit = 0
}

type heroModifierReleaseStep struct {
	runtime    campaignAbilityCommandRuntime
	sessionKey string
	generation uint64
	run        *heroModifierRun
	packet     []byte
}

func (e heroModifierReleaseStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.heroModifierRun == e.run
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return [][]byte{e.packet}, nil
}

type heroModifierExpiryStep struct {
	runtime    campaignAbilityCommandRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	run        *heroModifierRun
}

func (e heroModifierExpiryStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.heroModifierRun == e.run
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if e.run.isArborealMight && e.runtime.now().Before(e.run.expiresAt) {
		delay := e.run.expiresAt.Sub(e.runtime.now())
		e.runtime.registry.mutex.Unlock()
		if e.packet.ScheduleFunc == nil {
			return nil, fmt.Errorf("heroModifierRefreshSchedule: unavailable")
		}
		err := e.packet.ScheduleFunc(delay, e.produce)
		if err != nil {
			return nil, fmt.Errorf("heroModifierRefreshSchedule: %w", err)
		}
		return nil, nil
	}
	e.run.Remove(&peerSession)
	acquired := []zonenpc.Snapshot(nil)
	stealthPacket := []byte(nil)
	if e.run.isShadowStealth {
		var stealthErr error
		acquired, stealthPacket, stealthErr = peerSession.setShadowRavagerStealth(
			e.run.objectID, false,
		)
		if stealthErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroModifierExpiryStealth: %w", stealthErr)
		}
	}
	peerSession.heroModifierRun = nil
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	_ = e.runtime.modifierPool.Release(e.run.instanceID)
	packet, err := effectraknet.ModifierDelete(e.run.objectID, e.run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("heroModifierDelete: %w", err)
	}
	packets := [][]byte{packet}
	if stealthPacket != nil {
		packets = append(packets, stealthPacket)
	}
	targetPackets, err := scheduleShadowRavagerTargets(
		e.runtime.npc, e.packet, e.sessionKey, e.generation, acquired,
	)
	if err != nil {
		return nil, fmt.Errorf("heroModifierExpiryTarget: %w", err)
	}
	packets = append(packets, targetPackets...)
	effectPacket, err := e.run.stopEffect(e.runtime.effectPool)
	if err != nil {
		return nil, fmt.Errorf("heroModifierEffectStop: %w", err)
	}
	if effectPacket != nil {
		packets = append(packets, effectPacket)
	}
	if e.run.isArborealMight {
		attributePacket, marshalErr := raknet.MarshalApplication(
			raknet.AttributeDataUpdateMessage{
				ObjectID: e.run.objectID,
				Value:    map[uint8]float32{113: 0},
			},
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("heroModifierScaleReset: %w", marshalErr)
		}
		packets = append(packets, attributePacket)
	}
	return packets, nil
}

type heroModifierScheduleFailure struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	run                 *heroModifierRun
	previousRun         *heroModifierRun
	previousManaPoint   float32
	previousDamageBuff  float32
	previousAutoCrit    float32
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	isInstanceAllocated bool
}

func (e heroModifierScheduleFailure) handle(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.heroModifierRun == e.run
	if isCurrent {
		profile := &peerSession.binding.Creatures[e.run.creatureIndex].DamageProfile
		profile.DamageBuff = e.previousDamageBuff
		peerSession.binding.Creatures[e.run.creatureIndex].AutoCrit = e.previousAutoCrit
		_ = peerSession.setCampaignCharacterManaPoints(
			e.run.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		peerSession.heroModifierRun = e.previousRun
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	isPreviousShadowStealth := e.previousRun != nil &&
		e.previousRun.isShadowStealth
	if e.run.isShadowStealth || isPreviousShadowStealth {
		acquired := []zonenpc.Snapshot(nil)
		stealthPacket := []byte(nil)
		var err error
		if e.run.isShadowStealth {
			acquired, stealthPacket, err = peerSession.setShadowRavagerStealth(
				e.run.objectID, false,
			)
		}
		if len(stealthPacket) != 0 && e.runtime.logger != nil {
			e.runtime.logger.Printf(
				"RakNet Shadow Cloak rollback suppressed one stale presentation for %s",
				e.sessionKey,
			)
		}
		if err != nil && e.runtime.logger != nil {
			e.runtime.logger.Printf(
				"RakNet Shadow Cloak targeting rollback skipped for %s: %v",
				e.sessionKey, err,
			)
		}
		if isPreviousShadowStealth {
			acquired, stealthPacket, err = peerSession.setShadowRavagerStealth(
				e.previousRun.objectID, true,
			)
			if len(stealthPacket) != 0 && e.runtime.logger != nil {
				e.runtime.logger.Printf(
					"RakNet previous Shadow Cloak rollback suppressed one stale presentation for %s",
					e.sessionKey,
				)
			}
			if err != nil && e.runtime.logger != nil {
				e.runtime.logger.Printf(
					"RakNet previous Shadow Cloak targeting restore skipped for %s: %v",
					e.sessionKey, err,
				)
			}
		}
		if err == nil {
			targetPackets, scheduleErr := scheduleShadowRavagerTargets(
				e.runtime.npc, e.packet, e.sessionKey, e.generation, acquired,
			)
			if len(targetPackets) != 0 && e.runtime.logger != nil {
				e.runtime.logger.Printf(
					"RakNet Shadow Cloak rollback suppressed %d immediate retarget presentations for %s",
					len(targetPackets), e.sessionKey,
				)
			}
			if scheduleErr != nil && e.runtime.logger != nil {
				e.runtime.logger.Printf(
					"RakNet Shadow Cloak rollback retarget skipped for %s: %v",
					e.sessionKey, scheduleErr,
				)
			}
		}
	}
	if e.isInstanceAllocated {
		_ = e.runtime.modifierPool.Release(e.run.instanceID)
	}
	e.run.releaseEffect(e.runtime.effectPool)
	e.runtime.logger.Printf(
		"RakNet hero modifier stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func (r campaignAbilityCommandRuntime) handleHeroSelfModifier(
	req campaignCharacterAbilityRequest, ability zonecontent.HeroAbility,
	cooldownKey zoneability.CooldownKey,
) ([][]byte, error) {
	definition := ability.Definition
	if ability.ID == 0 || definition.Kind != sim.AbilityKindModifier ||
		definition.RootModifierID == 0 || definition.Duration <= 0 {
		return req.reject("self modifier definition unavailable")
	}
	if req.packet.ScheduleGroup == nil &&
		req.packet.ScheduleGroupResult == nil {
		return req.reject("self modifier schedule unavailable")
	}

	sessionKey := req.packet.Address.String()
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isSessionAvailable := isFound &&
		peerSession.generation == req.commandSession.generation &&
		req.command.Common.ObjectID == peerSession.deployedObjectID &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
		peerSession.isAbilityReleaseReady(req.startTime)
	if !isSessionAvailable {
		r.registry.mutex.Unlock()
		return req.reject("self modifier session unavailable")
	}
	if !peerSession.abilityCooldownSession().IsReady(cooldownKey, req.startTime) {
		r.registry.mutex.Unlock()
		return req.reject("self modifier cooldown unavailable")
	}

	creatureIndex := peerSession.deployedCreatureIndex
	creature := peerSession.binding.Creatures[creatureIndex]
	abilityRank := req.command.Ability.Rank
	if abilityRank <= 0 {
		abilityRank = 1
	}
	definition, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroModifierTiming: %w", err)
	}
	manaCost, err := game.ResolveAbilityManaCost(
		definition.ManaCost, creature.DamageProfile.PrimaryAttribute,
		definition.ManaCoefficient, peerSession.isOverdriveActiveAt(req.startTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroModifierMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}

	previousRun := peerSession.heroModifierRun
	stackCount := uint32(1)
	instanceID := uint32(0)
	isInstanceAllocated := false
	isArborealAtMaximum := false
	if previousRun != nil && previousRun.abilityID == ability.ID &&
		previousRun.creatureIndex == creatureIndex {
		if definition.Name == "ArborealMight" {
			maximumStack := arborealMightMaximumStack(abilityRank)
			isArborealAtMaximum = previousRun.stackCount >= maximumStack
			stackCount = min(
				maximumStack, previousRun.stackCount+1,
			)
		}
		instanceID = previousRun.instanceID
	} else {
		instanceID, err = r.modifierPool.Allocate()
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroModifierAllocate: %w", err)
		}
		isInstanceAllocated = true
	}

	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	previousManaPoint := peerSession.deployedManaPoint()
	previousDamageBuff := creature.DamageProfile.DamageBuff
	previousAutoCrit := creature.AutoCrit
	damageBuff := float32(0)
	autoCrit := float32(0)
	if definition.Name == "ArborealMight" {
		damageBuff = float32(stackCount) *
			arborealMightDamagePerStack(abilityRank)
	}
	if definition.Name == "ShadowRavagerSupport" {
		autoCrit = 1
	}
	run := &heroModifierRun{
		abilityID: ability.ID, creatureIndex: creatureIndex,
		instanceID: instanceID, objectID: req.command.Common.ObjectID,
		stackCount: stackCount, damageBuff: damageBuff, autoCrit: autoCrit,
		isDamageImmune: definition.Name == "TechRandom2" ||
			definition.Name == "ShadowRavagerSupport",
		isDebuffImmune:  definition.Name == "TechRandom2",
		isSoulLink:      definition.Name == "CastSoulLink",
		isQuantumState:  definition.Name == "QuantumState",
		isArborealMight: definition.Name == "ArborealMight",
		isAbilityBreak:  definition.Name == "ShadowRavagerSupport",
		isShadowStealth: definition.Name == "ShadowRavagerSupport",
		expiresAt:       req.startTime.Add(definition.Duration),
	}
	if isArborealAtMaximum {
		run.expiresAt = previousRun.expiresAt
	}
	if run.isQuantumState {
		run.quantum = quantumStateRuntime{
			runtime: r.damage, packet: req.packet, sessionKey: sessionKey,
			generation: peerSession.generation, ownerObjectID: run.objectID,
			creatureIndex: creatureIndex, rank: abilityRank,
			activeAt: req.startTime.Add(definition.HitDelay),
		}
	}
	if (run.isSoulLink || run.isQuantumState || run.isArborealMight || definition.Name == "TechRandom2") &&
		definition.ActivationEffectName != "" {
		if r.effectPool == nil {
			if isInstanceAllocated {
				_ = r.modifierPool.Release(instanceID)
			}
			r.registry.mutex.Unlock()
			return req.reject("self modifier effect unavailable")
		}
		effectSlot, isEffectAllocated := r.effectPool.Allocate(run.objectID)
		if !isEffectAllocated {
			if isInstanceAllocated {
				_ = r.modifierPool.Release(instanceID)
			}
			r.registry.mutex.Unlock()
			return req.reject("self modifier effect unavailable")
		}
		run.effectSlot = effectSlot
		run.isEffectAttached = true
	}

	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp:    req.command.Common.Unknown[0],
		ResponseType: raknet.ActionResponseAccepted,
		ObjectID:     ability.ID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds: req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime +
			uint64(definition.HitDelay/time.Millisecond),
		SourceEndMilliseconds: req.packet.SourceTime +
			uint64(definition.ReleaseDelay/time.Millisecond),
	})
	if err != nil {
		run.releaseEffect(r.effectPool)
		if isInstanceAllocated {
			_ = r.modifierPool.Release(instanceID)
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroModifierAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], ability.ID,
		req.command.Ability.Index, req.packet.SourceTime,
		definition.HitDelay, definition.ReleaseDelay,
	)
	if err != nil {
		run.releaseEffect(r.effectPool)
		if isInstanceAllocated {
			_ = r.modifierPool.Release(instanceID)
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroModifierRelease: %w", err)
	}

	messages := []raknet.ApplicationMessage{
		raknet.SetAnimationStateMessage{
			ObjectID: run.objectID, State: util.HashID(definition.AnimationName),
			Timestamp: req.packet.SourceTime, Scale: 1,
		},
		raknet.CooldownUpdateMessage{
			ObjectID: run.objectID, AbilityKey: uint64(ability.ID),
			DurationMilliseconds:    definition.Cooldown.Milliseconds(),
			SourceStartMilliseconds: int64(req.packet.SourceTime),
		},
		raknet.CombatantDataDeltaMessage{
			ObjectID: run.objectID, ManaPoints: remainingManaPoint,
			IsManaPointChanged: true,
		},
	}
	if previousRun != nil && previousRun.isEffectAttached {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: previousRun.effectSlot + 1, IsRemovalRequested: true,
			IsHardStop: true, ObjectID: previousRun.objectID,
		})
	}
	if run.isEffectAttached {
		effectAssetName := definition.ActivationEffectName
		if run.isArborealMight {
			effectAssetName = "status_enraged.ServerEventDef"
		}
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: run.effectSlot + 1, IsForceAttached: true,
			Asset:    util.HashID(effectAssetName),
			ObjectID: run.objectID,
		})
	}
	if (!run.isEffectAttached || run.isArborealMight) && definition.ActivationEffectName != "" {
		messages = append(messages, raknet.ServerEventMessage{
			Asset: util.HashID(definition.ActivationEffectName), ObjectID: run.objectID,
		})
	}
	if run.isArborealMight && !isArborealAtMaximum {
		messages = append(messages, raknet.AttributeDataUpdateMessage{
			ObjectID: run.objectID,
			Value:    map[uint8]float32{113: float32(stackCount) * 0.06},
		})
	}
	modifierStart := req.packet.SourceTime
	if run.isQuantumState {
		modifierStart += uint64(definition.HitDelay / time.Millisecond)
	}
	if previousRun == nil || previousRun.abilityID != ability.ID ||
		previousRun.creatureIndex != creatureIndex {
		messages = append(messages, raknet.ModifierCreatedMessage{
			TargetID: run.objectID, ModifierGUID: definition.RootModifierID,
			InstanceID:           instanceID,
			DurationMilliseconds: uint32(definition.Duration.Milliseconds()),
			StackCount:           stackCount, StartMilliseconds: modifierStart,
			SourceID: run.objectID,
		})
	} else if !isArborealAtMaximum {
		messages = append(messages, raknet.ModifierUpdatedMessage{
			TargetID: run.objectID, InstanceID: instanceID,
			StartMilliseconds: int64(req.packet.SourceTime),
			StackCount:        stackCount,
		})
	}
	startPackets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, marshalErr := raknet.MarshalApplication(message)
		if marshalErr != nil {
			run.releaseEffect(r.effectPool)
			if isInstanceAllocated {
				_ = r.modifierPool.Release(instanceID)
			}
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroModifierMarshal[%d]: %w", index, marshalErr)
		}
		startPackets = append(startPackets, packet)
	}

	cooldownReservation, isCooldownReserved :=
		peerSession.abilityCooldownSession().Reserve(
			cooldownKey, req.startTime, definition.Cooldown,
		)
	if !isCooldownReserved {
		run.releaseEffect(r.effectPool)
		if isInstanceAllocated {
			_ = r.modifierPool.Release(instanceID)
		}
		r.registry.mutex.Unlock()
		return req.reject("self modifier cooldown unavailable")
	}
	releaseReservation, isReleaseReserved :=
		peerSession.abilityReleaseSession().Reserve(
			req.startTime, definition.ReleaseDelay,
		)
	if !isReleaseReserved {
		run.releaseEffect(r.effectPool)
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		if isInstanceAllocated {
			_ = r.modifierPool.Release(instanceID)
		}
		r.registry.mutex.Unlock()
		return req.reject("self modifier release unavailable")
	}
	err = peerSession.stopPlayerMovement(req.startTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		run.releaseEffect(r.effectPool)
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		if isInstanceAllocated {
			_ = r.modifierPool.Release(instanceID)
		}
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroModifierCommit: %w", err)
	}
	profile := &peerSession.binding.Creatures[creatureIndex].DamageProfile
	if previousRun != nil && previousRun.creatureIndex == creatureIndex {
		profile.DamageBuff = max(float32(0), profile.DamageBuff-previousRun.damageBuff)
		peerSession.binding.Creatures[creatureIndex].AutoCrit = max(
			float32(0),
			peerSession.binding.Creatures[creatureIndex].AutoCrit-previousRun.autoCrit,
		)
	}
	profile.DamageBuff += damageBuff
	peerSession.binding.Creatures[creatureIndex].AutoCrit += autoCrit
	peerSession.heroModifierRun = run
	generation := peerSession.generation
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	releaseStep := heroModifierReleaseStep{
		runtime: r, sessionKey: sessionKey, generation: generation,
		run: run, packet: releasePacket,
	}
	expiryStep := heroModifierExpiryStep{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, run: run,
	}
	expiryDelay := definition.Duration
	if isArborealAtMaximum {
		expiryDelay = max(time.Millisecond, run.expiresAt.Sub(req.startTime))
	}
	if run.isQuantumState {
		expiryDelay += definition.HitDelay
	}
	producers := []raknet.ScheduledPacketProducer{
		{Delay: definition.ReleaseDelay, Produce: releaseStep.produce},
		{Delay: expiryDelay, Produce: expiryStep.produce},
	}
	producers = r.registry.producerGuard.scheduledProducers(sessionKey, producers)
	rollback := heroModifierScheduleFailure{
		runtime: r, packet: req.packet,
		sessionKey: sessionKey, generation: generation,
		run: run, previousRun: previousRun,
		previousManaPoint:   previousManaPoint,
		previousDamageBuff:  previousDamageBuff,
		previousAutoCrit:    previousAutoCrit,
		cooldownReservation: cooldownReservation,
		releaseReservation:  releaseReservation,
		isInstanceAllocated: isInstanceAllocated,
	}
	var cancel raknet.CancelSchedule
	if req.packet.ScheduleGroupResult != nil {
		cancel, err = req.packet.ScheduleGroupResult(producers, rollback.handle)
	} else {
		cancel, err = req.packet.ScheduleGroup(producers)
	}
	if err != nil {
		rollback.handle(err)
		return nil, fmt.Errorf("heroModifierSchedule: %w", err)
	}
	run.cancel = cancel
	if run.isShadowStealth {
		acquired, stealthPacket, stealthErr := peerSession.setShadowRavagerStealth(
			run.objectID, true,
		)
		if stealthErr != nil {
			run.Cancel()
			rollback.handle(stealthErr)
			return nil, fmt.Errorf("heroModifierStealth: %w", stealthErr)
		}
		startPackets = append(startPackets, stealthPacket)
		targetPackets, targetErr := scheduleShadowRavagerTargets(
			r.npc, req.packet, sessionKey, generation, acquired,
		)
		if targetErr != nil {
			run.Cancel()
			rollback.handle(targetErr)
			return nil, fmt.Errorf("heroModifierStealthTarget: %w", targetErr)
		}
		startPackets = append(startPackets, targetPackets...)
	} else if previousRun != nil && previousRun.isShadowStealth {
		acquired, stealthPacket, stealthErr := peerSession.setShadowRavagerStealth(
			previousRun.objectID, false,
		)
		if stealthErr != nil {
			run.Cancel()
			rollback.handle(stealthErr)
			return nil, fmt.Errorf("heroModifierPreviousStealth: %w", stealthErr)
		}
		startPackets = append(startPackets, stealthPacket)
		targetPackets, targetErr := scheduleShadowRavagerTargets(
			r.npc, req.packet, sessionKey, generation, acquired,
		)
		if targetErr != nil {
			run.Cancel()
			rollback.handle(targetErr)
			return nil, fmt.Errorf("heroModifierPreviousTarget: %w", targetErr)
		}
		startPackets = append(startPackets, targetPackets...)
	}
	if previousRun != nil {
		previousRun.Cancel()
		previousRun.releaseEffect(r.effectPool)
	}
	if definition.Name == "TechRandom2" {
		purgePackets, purgeErr := r.purgeHeroDebuffs(
			sessionKey, generation, run.objectID,
		)
		if purgeErr != nil {
			r.logger.Printf(
				"RakNet Omni Shield debuff purge skipped for %s: %v",
				sessionKey, purgeErr,
			)
		} else {
			startPackets = append(startPackets, purgePackets...)
		}
	}
	r.logger.Printf(
		"RakNet hero modifier accepted ability=%s source=%d stack=%d duration=%s",
		definition.Name, run.objectID, stackCount, definition.Duration,
	)
	return append([][]byte{ackPacket}, startPackets...), nil
}

func (r campaignAbilityCommandRuntime) purgeHeroDebuffs(
	sessionKey string, generation uint64, objectID uint32,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation ||
		peerSession.deployedObjectID != objectID || peerSession.zone == nil ||
		peerSession.zone.Effect() == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for _, modifier := range peerSession.zone.Effect().Snapshot() {
		if modifier.TargetObjectID != objectID ||
			modifier.Kind != zoneeffect.ModifierKindDebuff {
			continue
		}
		run := peerSession.campaignNPCModifiers[modifier.InstanceID]
		if run == nil {
			continue
		}
		packet, err := effectraknet.ModifierDelete(
			modifier.TargetObjectID, modifier.InstanceID,
		)
		if err != nil {
			return nil, fmt.Errorf("heroDebuffDelete: %w", err)
		}
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		clearFieldMedicHeroStatus(&peerSession, modifier)
		peerSession.zone.Effect().Remove(modifier.InstanceID)
		peerSession.untrackCampaignNPCModifier(run)
		_, err = run.release(r.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("heroDebuffRelease: %w", err)
		}
		packets = append(packets, packet)
	}
	for key, run := range peerSession.campaignNPCPoisons {
		if key.targetObjectID != objectID || run == nil {
			continue
		}
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		if run.disease != nil {
			run.disease.expire(objectID)
		}
		delete(peerSession.campaignNPCPoisons, key)
		peerSession.untrackCampaignNPCModifier(run.modifier)
		packet, err := purgeHeroSpecialDebuff(
			objectID, run.modifier, r.modifierPool,
		)
		if err != nil {
			return nil, fmt.Errorf("heroPoisonPurge: %w", err)
		}
		if packet != nil {
			packets = append(packets, packet)
		}
	}
	if run := peerSession.campaignNPCSilences[objectID]; run != nil {
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		delete(peerSession.campaignNPCSilences, objectID)
		peerSession.enemySilenceExpiresAt = time.Time{}
		peerSession.untrackCampaignNPCModifier(run.modifier)
		packet, err := purgeHeroSpecialDebuff(
			objectID, run.modifier, r.modifierPool,
		)
		if err != nil {
			return nil, fmt.Errorf("heroSilencePurge: %w", err)
		}
		if packet != nil {
			packets = append(packets, packet)
		}
	}
	if run := peerSession.campaignNPCPhysicalVulnerabilities[objectID]; run != nil {
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		delete(peerSession.campaignNPCPhysicalVulnerabilities, objectID)
		packet, err := purgeHeroSpecialDebuff(
			objectID, run.modifier, r.modifierPool,
		)
		if err != nil {
			return nil, fmt.Errorf("heroPhysicalVulnerabilityPurge: %w", err)
		}
		if packet != nil {
			packets = append(packets, packet)
		}
	}
	if run := peerSession.campaignNPCEnergyVulnerabilities[objectID]; run != nil {
		delete(peerSession.campaignNPCEnergyVulnerabilities, objectID)
		packet, err := purgeHeroSpecialDebuff(
			objectID, run.modifier, r.modifierPool,
		)
		if err != nil {
			return nil, fmt.Errorf("heroEnergyVulnerabilityPurge: %w", err)
		}
		if packet != nil {
			packets = append(packets, packet)
		}
	}
	if run := peerSession.campaignNPCFears[objectID]; run != nil {
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		delete(peerSession.campaignNPCFears, objectID)
		peerSession.enemyFearExpiresAt = time.Time{}
		peerSession.enemyFearTargetObjectID = 0
		packet, err := purgeHeroSpecialDebuff(
			objectID, run.modifier, r.modifierPool,
		)
		if err != nil {
			return nil, fmt.Errorf("heroFearPurge: %w", err)
		}
		if packet != nil {
			packets = append(packets, packet)
		}
	}
	r.registry.sessions[sessionKey] = peerSession
	return packets, nil
}

func purgeHeroSpecialDebuff(
	objectID uint32, run *campaignNPCModifierRun, pool *modifierPool,
) ([]byte, error) {
	if objectID == 0 || run == nil || pool == nil {
		return nil, nil
	}
	isCreated, err := run.release(pool)
	if err != nil {
		return nil, fmt.Errorf("specialDebuffRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(objectID, run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("specialDebuffDelete: %w", err)
	}
	return packet, nil
}

func (r campaignDamageRuntime) refreshArborealMightOnDeath(
	sessionKey string, generation uint64, position game.Vec3, timestamp uint64,
) ([][]byte, error) {
	now := r.npc.now()
	r.registry.mutex.Lock()
	source, isFound := r.registry.sessions[sessionKey]
	if !isFound || source.generation != generation || source.zone == nil {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	type refresh struct {
		objectID   uint32
		instanceID uint32
		stackCount uint32
	}
	refreshes := make([]refresh, 0, len(r.registry.sessions))
	for key, candidate := range r.registry.sessions {
		run := candidate.heroModifierRun
		if candidate.zone != source.zone || candidate.deployedHitPoint() <= 0 ||
			run == nil || !run.isArborealMight ||
			game.Vec3(candidate.playerPosition).Sub(position).Length() >= 20 {
			continue
		}
		run.expiresAt = now.Add(15 * time.Second)
		candidate.heroModifierRun = run
		r.registry.sessions[key] = candidate
		refreshes = append(refreshes, refresh{
			objectID: run.objectID, instanceID: run.instanceID,
			stackCount: run.stackCount,
		})
	}
	r.registry.mutex.Unlock()

	packets := make([][]byte, 0, len(refreshes))
	for index, refreshed := range refreshes {
		packet, err := raknet.MarshalApplication(raknet.ModifierUpdatedMessage{
			TargetID: refreshed.objectID, InstanceID: refreshed.instanceID,
			StartMilliseconds: int64(timestamp), StackCount: refreshed.stackCount,
		})
		if err != nil {
			return nil, fmt.Errorf("arborealRefresh[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
