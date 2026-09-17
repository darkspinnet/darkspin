package gameplay

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type fieldMedicBuffCapture struct {
	winners   []zoneeffect.Modifier
	originals []zoneeffect.Modifier
}

type fieldMedicHeroBuffRun struct {
	instanceID        uint32
	targetObjectID    uint32
	creatureIndex     uint32
	damageBuff        float32
	energyDamageBuff  float32
	attackSpeed       float32
	cooldownReduction float32
	movementSpeedBuff float32
	maximumHitPoint   float32
	isCompanion       bool
	cancel            raknet.CancelSchedule
}

func (e *fieldMedicHeroBuffRun) remove(peerSession *gameplayPeerSession) {
	if e == nil || peerSession == nil ||
		(!e.isCompanion &&
			e.creatureIndex >= uint32(len(peerSession.binding.Creatures))) {
		return
	}
	if e.isCompanion {
		if peerSession.zone == nil {
			return
		}
		companion, isFound :=
			peerSession.zone.Companion().Snapshot(e.targetObjectID)
		if isFound {
			_, _, _ = peerSession.zone.Companion().SetMaximumHitPoint(
				e.targetObjectID,
				max(float32(1), companion.MaximumHitPoint-e.maximumHitPoint),
			)
		}
		e.maximumHitPoint = 0
		return
	}
	profile := &peerSession.binding.Creatures[e.creatureIndex].DamageProfile
	profile.DamageBuff = max(float32(0), profile.DamageBuff-e.damageBuff)
	profile.EnergyDamageBuff = max(
		float32(0), profile.EnergyDamageBuff-e.energyDamageBuff,
	)
	timing := &peerSession.binding.Creatures[e.creatureIndex].TimingProfile
	timing.AttackSpeed = max(float32(0), timing.AttackSpeed-e.attackSpeed)
	timing.CooldownReduction = max(
		float32(0), timing.CooldownReduction-e.cooldownReduction,
	)
	creature := &peerSession.binding.Creatures[e.creatureIndex]
	creature.PassiveMovementIncrease = max(
		float32(0), creature.PassiveMovementIncrease-e.movementSpeedBuff,
	)
	peerSession.maximumHitPoints[e.creatureIndex] = max(
		float32(0), peerSession.maximumHitPoints[e.creatureIndex]-e.maximumHitPoint,
	)
	e.damageBuff = 0
	e.energyDamageBuff = 0
	e.attackSpeed = 0
	e.cooldownReduction = 0
	e.movementSpeedBuff = 0
	e.maximumHitPoint = 0
}

func (e *gameplayPeerSession) stopFieldMedicHeroBuffs(pool *modifierPool) {
	if e == nil {
		return
	}
	for instanceID, run := range e.fieldMedicHeroBuffs {
		if run.cancel != nil {
			run.cancel()
			run.cancel = nil
		}
		run.remove(e)
		if !run.isCompanion && e.squad != nil {
			maximumHitPoint := e.characterHitPointMaximum(run.creatureIndex)
			character, isCharacterFound := e.squad.Character(run.creatureIndex)
			if isCharacterFound && character.HitPoints > maximumHitPoint {
				_, _ = e.setCampaignCharacterHitPoints(
					run.creatureIndex, maximumHitPoint,
				)
			}
		}
		if e.zone != nil && e.zone.Effect() != nil {
			e.zone.Effect().Remove(instanceID)
		}
		if pool != nil {
			_ = pool.Release(instanceID)
		}
		delete(e.fieldMedicHeroBuffs, instanceID)
	}
}

func fieldMedicCapturedBuffs(
	peerSession gameplayPeerSession, center game.Vec3, radius float32,
) fieldMedicBuffCapture {
	selected := make(map[uint32]zoneeffect.Modifier)
	originals := make([]zoneeffect.Modifier, 0)
	for _, modifier := range peerSession.zone.Effect().Snapshot() {
		if modifier.Kind != zoneeffect.ModifierKindBuff ||
			!isFieldMedicBuffSupported(modifier.GUID) ||
			!fieldMedicEnemyInRange(
				peerSession, modifier.TargetObjectID, center, radius,
			) {
			continue
		}
		originals = append(originals, modifier)
		current, isFound := selected[modifier.GUID]
		if !isFound || current.Rank < modifier.Rank {
			selected[modifier.GUID] = modifier
		}
	}
	winners := make([]zoneeffect.Modifier, 0, len(selected))
	for _, current := range selected {
		winners = append(winners, current)
	}
	sort.Slice(winners, func(left, right int) bool {
		if winners[left].GUID == winners[right].GUID {
			return winners[left].InstanceID < winners[right].InstanceID
		}
		return winners[left].GUID < winners[right].GUID
	})
	return fieldMedicBuffCapture{winners: winners, originals: originals}
}

func fieldMedicEnemyInRange(
	peerSession gameplayPeerSession, objectID uint32,
	center game.Vec3, radius float32,
) bool {
	npc, isFound := peerSession.zone.NPCs().NPC(objectID)
	return isFound && npc.Faction == zonenpc.FactionNonPlayerAligned &&
		zonegeometry.Distance(center, npc.Plan.Position) <= radius
}

func isFieldMedicBuffSupported(guid uint32) bool {
	return guid == util.HashID(zonenpc.EnergyBuffModifierName) ||
		guid == util.HashID("NocturnaSpecialMunchModifier") ||
		guid == util.HashID("ZelemHasteBuff")
}

func (e fieldMedicActiveSchedule) removeEnemyBuffOriginalLocked(
	peerSession *gameplayPeerSession, modifier zoneeffect.Modifier,
) (bool, [][]byte, error) {
	if peerSession == nil || peerSession.zone == nil {
		return false, nil, nil
	}
	for sessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone != peerSession.zone {
			continue
		}
		run := candidate.campaignNPCEnergyBuffs[modifier.TargetObjectID]
		if run != nil && run.modifier.instanceID == modifier.InstanceID {
			if run.cancel != nil {
				run.cancel()
				run.cancel = nil
			}
			candidate.zone.NPCs().ClearEnergyBuff(run.targetID, run.expiresAt)
			delete(candidate.campaignNPCEnergyBuffs, run.targetID)
			candidate.zone.Effect().Remove(modifier.InstanceID)
			candidate.untrackCampaignNPCModifier(run.modifier)
			e.runtime.registry.sessions[sessionKey] = candidate
			return e.releaseEnemyBuffOriginal(run.modifier, modifier)
		}
		munch := candidate.campaignNPCMunches[modifier.TargetObjectID]
		if munch != nil && munch.modifier.instanceID == modifier.InstanceID {
			candidate.zone.NPCs().ClearMunch(
				modifier.TargetObjectID, munch.expiresAt,
			)
			delete(candidate.campaignNPCMunches, modifier.TargetObjectID)
			candidate.zone.Effect().Remove(modifier.InstanceID)
			candidate.untrackCampaignNPCModifier(munch.modifier)
			e.runtime.registry.sessions[sessionKey] = candidate
			return e.releaseEnemyBuffOriginal(munch.modifier, modifier)
		}
		haste := candidate.campaignNPCModifiers[modifier.InstanceID]
		if haste == nil || haste.record.GUID != util.HashID("ZelemHasteBuff") {
			continue
		}
		if haste.cancel != nil {
			haste.cancel()
			haste.cancel = nil
		}
		candidate.zone.Effect().Remove(modifier.InstanceID)
		candidate.untrackCampaignNPCModifier(haste)
		e.runtime.registry.sessions[sessionKey] = candidate
		return e.releaseEnemyBuffOriginal(haste, modifier)
	}
	return false, nil, nil
}

func (e fieldMedicActiveSchedule) releaseEnemyBuffOriginal(
	run *campaignNPCModifierRun, modifier zoneeffect.Modifier,
) (bool, [][]byte, error) {
	isCreated, err := run.release(e.runtime.modifierPool)
	if err != nil {
		return false, nil, fmt.Errorf("fieldMedicBuffRelease: %w", err)
	}
	if !isCreated {
		return true, nil, nil
	}
	packet, err := effectraknet.ModifierDelete(
		modifier.TargetObjectID, modifier.InstanceID,
	)
	if err != nil {
		return false, nil, fmt.Errorf("fieldMedicBuffDelete: %w", err)
	}
	return true, [][]byte{packet}, nil
}

type fieldMedicHeroBuffExpiry struct {
	runtime          campaignAbilityCommandRuntime
	sourceSessionKey string
	targetSessionKey string
	generation       uint64
	run              *fieldMedicHeroBuffRun
}

func (e fieldMedicHeroBuffExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.targetSessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.fieldMedicHeroBuffs[e.run.instanceID] == e.run
	if isCurrent {
		e.run.remove(&peerSession)
		if !e.run.isCompanion && peerSession.squad != nil {
			maximumHitPoint :=
				peerSession.characterHitPointMaximum(e.run.creatureIndex)
			character, isCharacterFound :=
				peerSession.squad.Character(e.run.creatureIndex)
			if isCharacterFound && character.HitPoints > maximumHitPoint {
				_, _ = peerSession.setCampaignCharacterHitPoints(
					e.run.creatureIndex, maximumHitPoint,
				)
			}
		}
		delete(peerSession.fieldMedicHeroBuffs, e.run.instanceID)
		if peerSession.zone != nil && peerSession.zone.Effect() != nil {
			peerSession.zone.Effect().Remove(e.run.instanceID)
		}
		if !e.run.isCompanion {
			_ = peerSession.syncZoneHero()
		}
		if peerSession.zone != nil &&
			e.targetSessionKey != e.sourceSessionKey {
			if e.run.isCompanion {
				peerSession.zone.PublishCompanionResourceTo(
					peerSession.binding.UserID, peerSession.generation,
					e.run.targetObjectID,
				)
			} else {
				peerSession.zone.PublishHeroResourceTo(
					peerSession.binding.UserID, peerSession.generation,
				)
			}
		}
		e.runtime.registry.sessions[e.targetSessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	err := e.runtime.modifierPool.Release(e.run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicHeroBuffRelease: %w", err)
	}
	packet, err := effectraknet.ModifierDelete(
		e.run.targetObjectID, e.run.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicHeroBuffDelete: %w", err)
	}
	resourcePackets, err := fieldMedicTargetResourcePackets(peerSession, e.run)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicHeroBuffResource: %w", err)
	}
	return append([][]byte{packet}, resourcePackets...), nil
}

func fieldMedicTargetResourcePackets(
	peerSession gameplayPeerSession, run *fieldMedicHeroBuffRun,
) ([][]byte, error) {
	if run == nil {
		return nil, errors.New("field medic buff unavailable")
	}
	if !run.isCompanion {
		packet, err :=
			peerSession.marshalCampaignCharacterResource(run.creatureIndex)
		if err != nil {
			return nil, fmt.Errorf("fieldMedicHeroResource: %w", err)
		}
		return [][]byte{packet}, nil
	}
	if peerSession.zone == nil {
		return nil, errors.New("field medic companion zone unavailable")
	}
	companion, isFound :=
		peerSession.zone.Companion().Snapshot(run.targetObjectID)
	if !isFound {
		return nil, nil
	}
	healthPacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: companion.ObjectID, HitPoints: companion.HitPoint,
		IsHitPointChanged: true,
	})
	if err != nil {
		return nil, fmt.Errorf("fieldMedicCompanionHealth: %w", err)
	}
	attributePacket, err := raknet.MarshalApplication(
		raknet.AttributeDataUpdateMessage{
			ObjectID: companion.ObjectID,
			Value:    map[uint8]float32{4: companion.MaximumHitPoint},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("fieldMedicCompanionMaximum: %w", err)
	}
	return [][]byte{healthPacket, attributePacket}, nil
}

func (r campaignAbilityCommandRuntime) applyFieldMedicSupportHealthBuff(
	packet raknet.Packet, sourceSessionKey string, sourceObjectID uint32,
	target fieldMedicHealingTarget,
	definition sim.AbilityDefinition,
) ([][]byte, error) {
	if definition.Name != "FieldMedicSupport" || sourceObjectID == 0 ||
		definition.RootModifierID == 0 || definition.StatusDuration <= 0 {
		return nil, nil
	}
	r.registry.mutex.Lock()
	defer r.registry.mutex.Unlock()
	peerSession, isFound := r.registry.sessions[target.sessionKey]
	if !isFound || peerSession.generation != target.generation ||
		peerSession.zone == nil || peerSession.zone.Effect() == nil ||
		(!target.isCompanion &&
			(target.creatureIndex >= uint32(len(peerSession.binding.Creatures)) ||
				peerSession.deployedCreatureIndex != target.creatureIndex ||
				peerSession.deployedObjectID != target.objectID)) {
		return nil, nil
	}

	packets := make([][]byte, 0, 3)
	for instanceID, previous := range peerSession.fieldMedicHeroBuffs {
		if previous == nil || previous.maximumHitPoint <= 0 ||
			previous.targetObjectID != target.objectID {
			continue
		}
		if previous.cancel != nil {
			previous.cancel()
			previous.cancel = nil
		}
		previous.remove(&peerSession)
		peerSession.zone.Effect().Remove(instanceID)
		delete(peerSession.fieldMedicHeroBuffs, instanceID)
		_ = r.modifierPool.Release(instanceID)
		deletePacket, err := effectraknet.ModifierDelete(
			previous.targetObjectID, instanceID,
		)
		if err != nil {
			return nil, fmt.Errorf("fieldMedicHealthReplace: %w", err)
		}
		packets = append(packets, deletePacket)
	}

	baseMaximumHitPoint := float32(0)
	if target.isCompanion {
		companion, isCompanionFound :=
			peerSession.zone.Companion().Snapshot(target.objectID)
		if !isCompanionFound || !companion.IsTargetable || companion.HitPoint <= 0 {
			return nil, nil
		}
		baseMaximumHitPoint = companion.MaximumHitPoint
	} else {
		baseMaximumHitPoint, _ =
			peerSession.characterResourceMaximum(target.creatureIndex)
	}
	if baseMaximumHitPoint <= 0 {
		baseMaximumHitPoint = campaignHeroResourceFallback
	}
	maximumHitPoint := baseMaximumHitPoint * 0.25
	instanceID, err := r.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("fieldMedicHealthAllocate: %w", err)
	}
	run := &fieldMedicHeroBuffRun{
		instanceID: instanceID, targetObjectID: target.objectID,
		creatureIndex: target.creatureIndex, maximumHitPoint: maximumHitPoint,
		isCompanion: target.isCompanion,
	}
	createPacket, err := effectraknet.ModifierCreate(
		effectraknet.ModifierCreateRequest{
			SourceObjectID: sourceObjectID, TargetObjectID: target.objectID,
			ModifierID: definition.RootModifierID, InstanceID: instanceID,
			Duration: definition.StatusDuration, StackCount: 1,
			Timestamp: packet.SourceTime,
		},
	)
	if err != nil {
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("fieldMedicHealthCreate: %w", err)
	}
	if target.isCompanion {
		_, _, err = peerSession.zone.Companion().SetMaximumHitPoint(
			target.objectID, baseMaximumHitPoint+maximumHitPoint,
		)
	} else {
		peerSession.maximumHitPoints[target.creatureIndex] += maximumHitPoint
	}
	if err != nil {
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("fieldMedicHealthCapacity: %w", err)
	}
	if peerSession.fieldMedicHeroBuffs == nil {
		peerSession.fieldMedicHeroBuffs = make(map[uint32]*fieldMedicHeroBuffRun)
	}
	peerSession.fieldMedicHeroBuffs[instanceID] = run
	err = peerSession.zone.Effect().Put(zoneeffect.Modifier{
		InstanceID: instanceID, GUID: definition.RootModifierID,
		SourceObjectID: sourceObjectID, TargetObjectID: target.objectID,
		Rank: 1, Duration: definition.StatusDuration,
		Kind: zoneeffect.ModifierKindBuff, StackCount: 1,
	})
	if err != nil {
		run.remove(&peerSession)
		delete(peerSession.fieldMedicHeroBuffs, instanceID)
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("fieldMedicHealthInventory: %w", err)
	}
	if !target.isCompanion {
		err = peerSession.syncZoneHero()
	}
	if err != nil {
		peerSession.zone.Effect().Remove(instanceID)
		run.remove(&peerSession)
		delete(peerSession.fieldMedicHeroBuffs, instanceID)
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("fieldMedicHealthSync: %w", err)
	}
	resourcePackets, err := fieldMedicTargetResourcePackets(peerSession, run)
	if err != nil {
		peerSession.zone.Effect().Remove(instanceID)
		run.remove(&peerSession)
		delete(peerSession.fieldMedicHeroBuffs, instanceID)
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("fieldMedicHealthResource: %w", err)
	}
	if target.sessionKey != sourceSessionKey {
		if target.isCompanion {
			peerSession.zone.PublishCompanionResourceTo(
				target.userID, target.generation, target.objectID,
			)
		} else {
			peerSession.zone.PublishHeroResourceTo(target.userID, target.generation)
		}
	}
	r.registry.sessions[target.sessionKey] = peerSession
	expiry := fieldMedicHeroBuffExpiry{
		runtime: r, sourceSessionKey: sourceSessionKey,
		targetSessionKey: target.sessionKey,
		generation:       target.generation, run: run,
	}
	cancel, scheduleErr := packet.ScheduleProducers(
		[]raknet.ScheduledPacketProducer{{
			Delay: definition.StatusDuration, Produce: expiry.produce,
		}},
	)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		peerSession.zone.Effect().Remove(instanceID)
		run.remove(&peerSession)
		delete(peerSession.fieldMedicHeroBuffs, instanceID)
		r.registry.sessions[target.sessionKey] = peerSession
		_ = r.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("fieldMedicHealthSchedule: %w", scheduleErr)
	}
	run.cancel = cancel
	packets = append(packets, createPacket)
	packets = append(packets, resourcePackets...)
	return packets, nil
}

func (e fieldMedicActiveSchedule) transferBuffsLocked(
	peerSession *gameplayPeerSession, center game.Vec3,
	buffs []zoneeffect.Modifier,
) ([][]byte, []uint32, error) {
	if peerSession == nil || peerSession.zone == nil || len(buffs) == 0 {
		return nil, nil, nil
	}
	packets := make([][]byte, 0)
	targetObjectIDs := make([]uint32, 0)
	for sessionKey, candidate := range e.runtime.registry.sessions {
		if candidate.zone != peerSession.zone ||
			!fieldMedicLivingHeroInRange(candidate, center, e.definition.Radius) {
			continue
		}
		isEffectAdded := false
		for _, modifier := range buffs {
			packet, isTransferred, err := e.transferBuffToHeroLocked(
				sessionKey, &candidate, modifier,
			)
			if err != nil {
				return nil, nil, fmt.Errorf(
					"fieldMedicHeroBuff[%s/%#x]: %w",
					sessionKey, modifier.GUID, err,
				)
			}
			if isTransferred {
				packets = append(packets, packet)
				isEffectAdded = true
				e.runtime.registry.sessions[sessionKey] = candidate
			}
		}
		if !isEffectAdded {
			continue
		}
		e.runtime.registry.sessions[sessionKey] = candidate
		targetObjectIDs = append(targetObjectIDs, candidate.deployedObjectID)
	}
	companionPackets, companionTargets, err := e.transferBuffsToCompanionsLocked(
		peerSession, center, buffs,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("fieldMedicCompanionBuff: %w", err)
	}
	packets = append(packets, companionPackets...)
	targetObjectIDs = append(targetObjectIDs, companionTargets...)
	return packets, targetObjectIDs, nil
}

func fieldMedicLivingHeroInRange(
	peerSession gameplayPeerSession, center game.Vec3, radius float32,
) bool {
	return peerSession.zone != nil && peerSession.deployedObjectID != 0 &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
		peerSession.deployedHitPoint() > 0 &&
		zonegeometry.Distance(center, game.Vec3(peerSession.playerPosition)) <= radius
}

func (e fieldMedicActiveSchedule) transferBuffToHeroLocked(
	targetSessionKey string, peerSession *gameplayPeerSession,
	modifier zoneeffect.Modifier,
) ([]byte, bool, error) {
	if peerSession == nil || peerSession.zone == nil ||
		peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) ||
		(modifier.DamageBuff <= 0 && modifier.EnergyDamageBuff <= 0 &&
			modifier.AttackSpeed <= 0 && modifier.CooldownReduction <= 0 &&
			modifier.MovementSpeedBuff <= 0) {
		return nil, false, nil
	}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return nil, false, fmt.Errorf("fieldMedicHeroBuffAllocate: %w", err)
	}
	run := &fieldMedicHeroBuffRun{
		instanceID: instanceID, targetObjectID: peerSession.deployedObjectID,
		creatureIndex:     peerSession.deployedCreatureIndex,
		damageBuff:        modifier.DamageBuff,
		energyDamageBuff:  modifier.EnergyDamageBuff,
		attackSpeed:       modifier.AttackSpeed,
		cooldownReduction: modifier.CooldownReduction,
		movementSpeedBuff: modifier.MovementSpeedBuff,
	}
	packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.sourceObjectID, TargetObjectID: run.targetObjectID,
		ModifierID: modifier.GUID, InstanceID: instanceID,
		Duration: modifier.Duration, StackCount: modifier.StackCount,
		Timestamp: e.packet.SourceTime +
			uint64(e.definition.HitDelay/time.Millisecond),
	})
	if err != nil {
		_ = e.runtime.modifierPool.Release(instanceID)
		return nil, false, fmt.Errorf("fieldMedicHeroBuffCreate: %w", err)
	}
	if peerSession.fieldMedicHeroBuffs == nil {
		peerSession.fieldMedicHeroBuffs = make(map[uint32]*fieldMedicHeroBuffRun)
	}
	profile := &peerSession.binding.Creatures[run.creatureIndex].DamageProfile
	profile.DamageBuff += run.damageBuff
	profile.EnergyDamageBuff += run.energyDamageBuff
	timing := &peerSession.binding.Creatures[run.creatureIndex].TimingProfile
	timing.AttackSpeed += run.attackSpeed
	timing.CooldownReduction += run.cooldownReduction
	peerSession.binding.Creatures[run.creatureIndex].PassiveMovementIncrease +=
		run.movementSpeedBuff
	peerSession.fieldMedicHeroBuffs[instanceID] = run
	record := modifier
	record.InstanceID = instanceID
	record.SourceObjectID = e.sourceObjectID
	record.TargetObjectID = run.targetObjectID
	record.Kind = zoneeffect.ModifierKindBuff
	err = peerSession.zone.Effect().Put(record)
	if err != nil {
		run.remove(peerSession)
		delete(peerSession.fieldMedicHeroBuffs, instanceID)
		_ = e.runtime.modifierPool.Release(instanceID)
		return nil, false, fmt.Errorf("fieldMedicHeroBuffInventory: %w", err)
	}
	expiry := fieldMedicHeroBuffExpiry{
		runtime: e.runtime, sourceSessionKey: e.sessionKey,
		targetSessionKey: targetSessionKey,
		generation:       peerSession.generation, run: run,
	}
	cancel, scheduleErr := e.packet.ScheduleProducers(
		[]raknet.ScheduledPacketProducer{{
			Delay: modifier.Duration, Produce: expiry.produce,
		}},
	)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		peerSession.zone.Effect().Remove(instanceID)
		run.remove(peerSession)
		delete(peerSession.fieldMedicHeroBuffs, instanceID)
		_ = e.runtime.modifierPool.Release(instanceID)
		return nil, false, fmt.Errorf("fieldMedicHeroBuffSchedule: %w", scheduleErr)
	}
	run.cancel = cancel
	return packet, true, nil
}
