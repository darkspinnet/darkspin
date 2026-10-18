package gameplay

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
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
	mutex               sync.Mutex
	zone                *zone.Zone
	userID              uint64
	generation          uint64
	expiresAt           time.Time
	isRetired           bool
	isReleased          bool
	instanceID          uint32
	targetObjectID      uint32
	targetOwnerObjectID uint32
	creatureIndex       uint32
	damageBuff          float32
	energyDamageBuff    float32
	attackSpeed         float32
	cooldownReduction   float32
	movementSpeedBuff   float32
	maximumHitPoint     float32
	isCompanion         bool
	cancel              raknet.CancelSchedule
}

func (e *fieldMedicHeroBuffRun) remove(peerSession *gameplayPeerSession) error {
	if e == nil || peerSession == nil ||
		(!e.isCompanion &&
			e.creatureIndex >= uint32(len(peerSession.binding.Creatures))) {
		return nil
	}
	if e.isCompanion {
		if peerSession.zone == nil {
			return nil
		}
		companion, isFound :=
			peerSession.zone.Companion().Snapshot(e.targetObjectID)
		if isFound && companion.UserID == e.userID && companion.PeerGeneration == e.generation &&
			companion.OwnerObjectID == e.targetOwnerObjectID {
			previousCompanion, updatedCompanion, err := peerSession.zone.Companion().SetMaximumHitPoint(
				e.targetObjectID,
				max(float32(1), companion.MaximumHitPoint-e.maximumHitPoint),
			)
			if err != nil {
				return fmt.Errorf("companionCapacity: %w", err)
			}
			// The companion operation clamps current health to its new capacity.
			_ = previousCompanion
			_ = updatedCompanion
		}
		e.maximumHitPoint = 0
		return nil
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
	return nil
}

func (e *gameplayPeerSession) stopFieldMedicHeroBuffs(pool *modifierPool) ([][]byte, error) {
	if e == nil {
		return nil, nil
	}
	packets := make([][]byte, 0)
	var retirementErr error
	for _, run := range e.fieldMedicHeroBuffs {
		retiredPackets, err := run.retire(e, pool)
		packets = append(packets, retiredPackets...)
		if err != nil {
			retirementErr = errors.Join(retirementErr, err)
		}
	}
	if retirementErr != nil {
		return packets, fmt.Errorf("heroRetire: %w", retirementErr)
	}
	return packets, nil
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
	runtime campaignAbilityCommandRuntime
	run     *fieldMedicHeroBuffRun
}

func (e fieldMedicHeroBuffExpiry) execute() {
	packets, err := e.produce()
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet Field Medic buff expiry failed instance=%d: %v", e.run.instanceID, err)
	}
	if len(packets) != 0 && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet Field Medic expiry returned unqueued packets instance=%d", e.run.instanceID)
	}
	e.run.retryRetirement(e.runtime, e.execute)
}

func (e fieldMedicHeroBuffExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	for sessionKey, peerSession := range e.runtime.registry.sessions {
		if peerSession.zone != e.run.zone || peerSession.generation != e.run.generation ||
			peerSession.binding.UserID != e.run.userID ||
			peerSession.fieldMedicHeroBuffs[e.run.instanceID] != e.run {
			continue
		}
		packets, err := e.run.retire(&peerSession, e.runtime.modifierPool)
		e.runtime.registry.sessions[sessionKey] = peerSession
		e.runtime.registry.queueFieldMedicPacketsLocked(e.run.zone, packets)
		releaseErr := e.run.release(e.runtime.modifierPool)
		if releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
		if e.run.isRetired {
			delete(peerSession.fieldMedicHeroBuffs, e.run.instanceID)
		}
		if err != nil {
			return nil, fmt.Errorf("heroExpiry: %w", err)
		}
		return nil, nil
	}
	packets, err := e.run.retireMissingRecipient()
	e.runtime.registry.queueFieldMedicPacketsLocked(e.run.zone, packets)
	releaseErr := e.run.release(e.runtime.modifierPool)
	if err != nil || releaseErr != nil {
		return nil, fmt.Errorf("missingRecipient: %w", errors.Join(err, releaseErr))
	}
	return nil, nil
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
	if !isFound || companion.UserID != run.userID || companion.PeerGeneration != run.generation ||
		companion.OwnerObjectID != run.targetOwnerObjectID {
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
	sourceSession, isSourceFound := r.registry.sessions[sourceSessionKey]
	if !isFound || peerSession.generation != target.generation ||
		!isSourceFound || sourceSession.deployedObjectID != sourceObjectID ||
		!isActivePartyRecipient(peerSession, sourceSession) ||
		peerSession.zone == nil || peerSession.zone.Effect() == nil ||
		(!target.isCompanion &&
			(target.creatureIndex >= uint32(len(peerSession.binding.Creatures)) ||
				peerSession.deployedCreatureIndex != target.creatureIndex ||
				peerSession.deployedObjectID != target.objectID ||
				peerSession.deployedHitPoint() <= 0)) {
		return nil, nil
	}

	packets := make([][]byte, 0, 3)
	isHealthReplacement := false
	for instanceID, previous := range peerSession.fieldMedicHeroBuffs {
		if previous == nil || previous.maximumHitPoint <= 0 ||
			previous.targetObjectID != target.objectID {
			continue
		}
		retiredPackets, err := previous.retire(&peerSession, r.modifierPool)
		peerSession.queueCampaignPackets(retiredPackets)
		r.registry.sessions[target.sessionKey] = peerSession
		r.registry.queueFieldMedicPacketsLocked(peerSession.zone, retiredPackets, peerSession.binding.UserID)
		if err != nil {
			return nil, fmt.Errorf("fieldMedicHealthReplace: %w", err)
		}
		err = previous.release(r.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("healthReplaceRelease: %w", err)
		}
		delete(peerSession.fieldMedicHeroBuffs, instanceID)
		isHealthReplacement = true
	}

	baseMaximumHitPoint := float32(0)
	if target.isCompanion {
		companion, isCompanionFound :=
			peerSession.zone.Companion().Snapshot(target.objectID)
		if !isCompanionFound || !isPartyCompanionOwner(companion, peerSession) ||
			!companion.IsTargetable || companion.HitPoint <= 0 {
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
		zone: peerSession.zone, userID: peerSession.binding.UserID,
		generation: peerSession.generation, expiresAt: r.now().Add(definition.StatusDuration),
		instanceID: instanceID, targetObjectID: target.objectID,
		targetOwnerObjectID: peerSession.deployedObjectID,
		creatureIndex:       target.creatureIndex, maximumHitPoint: maximumHitPoint,
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
		releaseErr := r.modifierPool.Release(instanceID)
		err = errors.Join(err, releaseErr)
		return nil, fmt.Errorf("fieldMedicHealthCreate: %w", err)
	}
	if target.isCompanion {
		previousCompanion, updatedCompanion, capacityErr := peerSession.zone.Companion().SetMaximumHitPoint(
			target.objectID, baseMaximumHitPoint+maximumHitPoint,
		)
		err = capacityErr
		// Capacity application leaves the existing health unchanged.
		_ = previousCompanion
		_ = updatedCompanion
	} else {
		peerSession.maximumHitPoints[target.creatureIndex] += maximumHitPoint
	}
	if err != nil {
		releaseErr := r.modifierPool.Release(instanceID)
		err = errors.Join(err, releaseErr)
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
		rollbackErr := rollbackFieldMedicHeroBuff(&peerSession, run, r.modifierPool)
		err = errors.Join(err, rollbackErr)
		r.registry.sessions[target.sessionKey] = peerSession
		return nil, fmt.Errorf("fieldMedicHealthInventory: %w", err)
	}
	if !target.isCompanion {
		err = peerSession.syncZoneHero()
	}
	if err != nil {
		rollbackErr := rollbackFieldMedicHeroBuff(&peerSession, run, r.modifierPool)
		err = errors.Join(err, rollbackErr)
		r.registry.sessions[target.sessionKey] = peerSession
		return nil, fmt.Errorf("fieldMedicHealthSync: %w", err)
	}
	resourcePackets, err := fieldMedicTargetResourcePackets(peerSession, run)
	if err != nil {
		rollbackErr := rollbackFieldMedicHeroBuff(&peerSession, run, r.modifierPool)
		err = errors.Join(err, rollbackErr)
		r.registry.sessions[target.sessionKey] = peerSession
		return nil, fmt.Errorf("fieldMedicHealthResource: %w", err)
	}
	r.registry.sessions[target.sessionKey] = peerSession
	expiry := fieldMedicHeroBuffExpiry{runtime: r, run: run}
	cancel, scheduleErr := scheduleFieldMedicExpiry(r, run.expiresAt, expiry.execute)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		rollbackErr := rollbackFieldMedicHeroBuff(&peerSession, run, r.modifierPool)
		err = errors.Join(err, rollbackErr)
		r.registry.sessions[target.sessionKey] = peerSession
		return nil, fmt.Errorf("fieldMedicHealthSchedule: %w", errors.Join(scheduleErr, err))
	}
	run.cancel = cancel
	if target.sessionKey != sourceSessionKey {
		if target.isCompanion {
			peerSession.zone.PublishCompanionResourceTo(target.userID, target.generation, target.objectID)
		} else {
			peerSession.zone.PublishHeroResourceTo(target.userID, target.generation)
		}
	}
	packets = append(packets, createPacket)
	packets = append(packets, resourcePackets...)
	if isHealthReplacement {
		// Old retirement and the replacement share the same reliable pending
		// stream so an old resource snapshot cannot follow the new capacity.
		peerSession.queueCampaignPackets(packets)
		r.registry.sessions[target.sessionKey] = peerSession
		r.registry.queueFieldMedicPacketsLocked(peerSession.zone, packets, peerSession.binding.UserID)
		return nil, nil
	}
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
		if !fieldMedicLivingHeroInRange(candidate, *peerSession, center, e.definition.Radius) {
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
	peerSession, sourceSession gameplayPeerSession, center game.Vec3, radius float32,
) bool {
	return isActivePartyRecipient(peerSession, sourceSession) && peerSession.deployedObjectID != 0 &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
		peerSession.deployedHitPoint() > 0 &&
		zonegeometry.Distance(center, game.Vec3(peerSession.playerPosition)) <= radius
}

func (e fieldMedicActiveSchedule) transferBuffToHeroLocked(
	targetSessionKey string, peerSession *gameplayPeerSession,
	modifier zoneeffect.Modifier,
) ([]byte, bool, error) {
	sourceSession, isSourceFound := e.runtime.registry.sessions[e.sessionKey]
	if peerSession == nil || !isSourceFound ||
		!isActivePartyRecipient(*peerSession, sourceSession) ||
		peerSession.deployedHitPoint() <= 0 ||
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
		zone: peerSession.zone, userID: peerSession.binding.UserID,
		generation: peerSession.generation, expiresAt: e.runtime.now().Add(modifier.Duration),
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
		releaseErr := e.runtime.modifierPool.Release(instanceID)
		err = errors.Join(err, releaseErr)
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
		rollbackErr := rollbackFieldMedicHeroBuff(peerSession, run, e.runtime.modifierPool)
		err = errors.Join(err, rollbackErr)
		return nil, false, fmt.Errorf("fieldMedicHeroBuffInventory: %w", err)
	}
	expiry := fieldMedicHeroBuffExpiry{runtime: e.runtime, run: run}
	cancel, scheduleErr := scheduleFieldMedicExpiry(e.runtime, run.expiresAt, expiry.execute)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		rollbackErr := rollbackFieldMedicHeroBuff(peerSession, run, e.runtime.modifierPool)
		err = errors.Join(err, rollbackErr)
		return nil, false, fmt.Errorf("fieldMedicHeroBuffSchedule: %w", errors.Join(scheduleErr, err))
	}
	run.cancel = cancel
	return packet, true, nil
}
