package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Each accepted recipient owns its modifier, independently of the caster's
// animation, deployed character and connection after the initial area query.
type sporogenesisHot struct {
	runtime          campaignAbilityCommandRuntime
	originZone       *zone.Zone
	sourceObjectID   uint32
	sourceGeneration uint64
	binding          game.GameplayBinding
	creature         game.GameplayCreature
	definition       sim.AbilityDefinition
	target           fieldMedicHealingTarget
	targetOwnerID    uint32
	targetNoun       uint32
	isNPC            bool
	instanceID       uint32
	startedAt        time.Time
	sourceTime       uint64
	tickIndex        uint32
	isHealingStarted bool
	isRetired        bool
	isPublished      bool
	cancel           func()
}

type sporogenesisHotStep struct {
	run   *sporogenesisHot
	index uint32
}

func (e heroHealingTicksSchedule) startSporogenesis(deadline time.Duration) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	owner, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(owner, isFound) || owner.deployedHitPoint() <= 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if e.runtime.registry.timer == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, errors.New("sporogenesis timer unavailable")
	}
	assetID := util.HashID(e.definition.HealEffectName)
	sourceID := e.sourceObjectID
	cloudPacket, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{
		Asset: &assetID, ObjectID: &sourceID,
	})
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("sporogenesisCloud: %w", err)
	}
	targets := e.sporogenesisTargetsLocked(owner)
	// The paid cast has reached its hit. Only its animation release remains
	// connection-owned; applied HoTs use the zone timer and target identity.
	owner.heroHealingTicks = nil
	e.runtime.registry.sessions[e.sessionKey] = owner
	e.runtime.registry.queueChannelPresentationLocked(owner.zone, [][]byte{cloudPacket})
	e.runtime.registry.mutex.Unlock()
	for _, target := range targets {
		impactPacket, impactErr := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset: util.HashID(target.definition.HitEffectName), ObjectID: target.target.objectID,
		})
		if impactErr != nil {
			e.runtime.logger.Printf("RakNet Sporogenesis impact failed target=%d: %v", target.target.objectID, impactErr)
			continue
		}
		e.runtime.registry.mutex.Lock()
		e.runtime.registry.queueChannelPresentationLocked(target.originZone, [][]byte{impactPacket})
		e.runtime.registry.mutex.Unlock()
		if !target.isNPC && !target.target.isCompanion {
			purged, purgeErr := e.runtime.purgeHeroDebuffs(target.target.sessionKey, target.target.generation, target.target.objectID)
			e.runtime.registry.mutex.Lock()
			e.runtime.registry.queueChannelRemovalsLocked(target.originZone, purged)
			e.runtime.registry.mutex.Unlock()
			if purgeErr != nil {
				e.runtime.logger.Printf("RakNet Sporogenesis cleanse failed target=%d: %v", target.target.objectID, purgeErr)
				continue
			}
		}
		target.sourceTime = e.packet.SourceTime + uint64(deadline/time.Millisecond)
		applyErr := target.apply()
		if applyErr != nil {
			e.runtime.logger.Printf("RakNet Sporogenesis modifier failed target=%d: %v", target.target.objectID, applyErr)
		}
	}
	return nil, nil
}

func (e heroHealingTicksSchedule) sporogenesisTargetsLocked(owner gameplayPeerSession) []*sporogenesisHot {
	runs := make([]*sporogenesisHot, 0)
	center := game.Vec3(owner.playerPosition)
	for sessionKey, member := range e.runtime.registry.sessions {
		if !isActivePartyRecipient(member, owner) || member.deployedHitPoint() <= 0 ||
			game.Vec3(member.playerPosition).Sub(center).Length() > e.definition.Radius {
			continue
		}
		runs = append(runs, e.newSporogenesisHot(fieldMedicHealingTarget{
			sessionKey: sessionKey, generation: member.generation, userID: member.binding.UserID,
			objectID: member.deployedObjectID, creatureIndex: member.deployedCreatureIndex,
			binding: member.binding, position: game.Vec3(member.playerPosition),
		}, false, owner.zone))
	}
	for _, actor := range owner.zone.Companion().Snapshots() {
		if !actor.IsTargetable || actor.HitPoint <= 0 || actor.IsIntangible ||
			actor.PositionAt(e.runtime.now()).Sub(center).Length() > e.definition.Radius {
			continue
		}
		for sessionKey, member := range e.runtime.registry.sessions {
			if !isPartyCompanionOwner(actor, member) || !isActivePartyRecipient(member, owner) {
				continue
			}
			run := e.newSporogenesisHot(fieldMedicHealingTarget{
				sessionKey: sessionKey, generation: member.generation, userID: member.binding.UserID,
				objectID: actor.ObjectID, binding: member.binding, position: actor.PositionAt(e.runtime.now()),
				isCompanion: true,
			}, false, owner.zone)
			run.targetOwnerID, run.targetNoun = actor.OwnerObjectID, actor.Noun
			runs = append(runs, run)
			break
		}
	}
	for _, actor := range owner.zone.NPCs().LiveSnapshots() {
		if actor.Faction != zonenpc.FactionPlayerAligned || !actor.IsPublished || actor.IsDefeated ||
			actor.HitPoint <= 0 || actor.Plan.Position.Sub(center).Length() > e.definition.Radius {
			continue
		}
		runs = append(runs, e.newSporogenesisHot(fieldMedicHealingTarget{
			objectID: actor.Plan.ObjectID, position: actor.Plan.Position,
		}, true, owner.zone))
	}
	return runs
}

func (e heroHealingTicksSchedule) newSporogenesisHot(target fieldMedicHealingTarget, isNPC bool, originZone *zone.Zone) *sporogenesisHot {
	return &sporogenesisHot{runtime: e.runtime, originZone: originZone, sourceObjectID: e.sourceObjectID,
		sourceGeneration: e.generation, binding: e.binding, creature: e.creature, definition: e.definition, target: target, isNPC: isNPC}
}

func (e *sporogenesisHot) apply() error {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	if e.originZone.Context().Err() != nil {
		return nil
	}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return fmt.Errorf("sporogenesisAllocate: %w", err)
	}
	e.instanceID = instanceID
	e.startedAt = e.runtime.now()
	duration := time.Second + time.Duration(e.definition.NumberOfTicks-1)*e.definition.TickDuration
	createPacket, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: e.target.objectID, ModifierGUID: e.definition.RootModifierID, InstanceID: instanceID,
		SourceID: e.sourceObjectID, StackCount: 1, StartMilliseconds: e.sourceTime,
		DurationMilliseconds: uint32(duration / time.Millisecond),
	})
	if err != nil {
		e.retireLocked()
		return fmt.Errorf("sporogenesisCreate: %w", err)
	}
	packets, err := e.purgeTargetLocked()
	e.runtime.registry.queueChannelRemovalsLocked(e.originZone, packets)
	if err != nil {
		e.retireLocked()
		return fmt.Errorf("sporogenesisPurge: %w", err)
	}
	err = e.originZone.Effect().Put(zoneeffect.Modifier{
		InstanceID: instanceID, GUID: e.definition.RootModifierID, SourceObjectID: e.sourceObjectID,
		TargetObjectID: e.target.objectID, InitiatorObject: e.sourceObjectID, Kind: zoneeffect.ModifierKindBuff,
		StackCount: 1, Duration: duration, Rank: 1,
	})
	if err != nil {
		e.retireLocked()
		return fmt.Errorf("sporogenesisInventory: %w", err)
	}
	if e.isRecipientHandoverLocked() {
		e.queuePresentationLocked([][]byte{createPacket})
		e.isPublished = true
		err = e.scheduleStepLocked(0, disconnectRetryDelay)
		if err != nil {
			e.retireLocked()
			return fmt.Errorf("sporogenesisHandover: %w", err)
		}
		return nil
	}
	healed, err := e.healLocked()
	if err != nil {
		e.retireLocked()
		return fmt.Errorf("sporogenesisFirstHeal: %w", err)
	}
	e.isHealingStarted = true
	// Initial creation and accepted health are durably queued before the next
	// tick can run, even if the requesting transport is slow or fails.
	e.queuePresentationLocked(append([][]byte{createPacket}, healed...))
	e.isPublished = true
	err = e.scheduleNextLocked()
	if err != nil {
		e.runtime.registry.queueChannelRemovalsLocked(e.originZone, e.retireLocked())
		return fmt.Errorf("sporogenesisSchedule: %w", err)
	}
	return nil
}

func (e *sporogenesisHot) purgeTargetLocked() ([][]byte, error) {
	packets := make([][]byte, 0)
	if e.isNPC {
		fixturePackets, fixtureErr := e.runtime.registry.retireFixtureBurnsLocked(e.originZone.NPCs(), e.target.objectID, e.runtime.modifierPool)
		packets = append(packets, fixturePackets...)
		if fixtureErr != nil {
			return packets, fmt.Errorf("sporogenesisFixturePurge: %w", fixtureErr)
		}
		cloudPackets, cloudErr := e.runtime.registry.retireCloudPoisonLocked(e.originZone.NPCs(), e.target.objectID, e.runtime.modifierPool)
		packets = append(packets, cloudPackets...)
		if cloudErr != nil {
			return packets, fmt.Errorf("sporogenesisCloudPurge: %w", cloudErr)
		}
		e.originZone.NPCs().PurgeDebuffs(e.target.objectID)
		packets = append(packets, e.runtime.registry.retireTargetInfectionRootsLocked(e.originZone.NPCs(), e.target.objectID)...)
	}
	// Companion debuffs retain their native target ID in the owner's ordinary
	// tracked modifier namespace. Never clear another target's scalar status.
	for sessionKey, member := range e.runtime.registry.sessions {
		if member.zone != e.originZone {
			continue
		}
		if e.isNPC {
			basicPackets, basicErr := e.runtime.registry.retireTargetBasicModifiersLocked(member, e.target.objectID, e.runtime.modifierPool, true)
			if basicErr != nil {
				return packets, fmt.Errorf("sporogenesisBasicPurge: %w", basicErr)
			}
			packets = append(packets, basicPackets...)
			beastPacket, beastErr := e.runtime.registry.retireBeastVulnerabilityLocked(e.originZone.NPCs(), e.originZone.Effect(), e.target.objectID, e.runtime.modifierPool)
			if beastErr != nil {
				return packets, fmt.Errorf("sporogenesisBeastPurge: %w", beastErr)
			}
			if beastPacket != nil {
				packets = append(packets, beastPacket)
			}
		}
		for _, modifier := range e.originZone.Effect().Snapshot() {
			if modifier.TargetObjectID != e.target.objectID || modifier.Kind != zoneeffect.ModifierKindDebuff {
				continue
			}
			run := member.campaignNPCModifiers[modifier.InstanceID]
			if run == nil || run.fixtureBurn != nil {
				continue
			}
			deleted, deleteErr := purgeHeroSpecialDebuff(e.target.objectID, run, e.runtime.modifierPool)
			if deleteErr != nil {
				return packets, fmt.Errorf("sporogenesisDebuff: %w", deleteErr)
			}
			if run.cancel != nil {
				run.cancel()
				run.cancel = nil
			}
			member.untrackCampaignNPCModifier(run)
			e.originZone.Effect().Remove(modifier.InstanceID)
			if deleted != nil {
				packets = append(packets, deleted)
			}
		}
		e.runtime.registry.sessions[sessionKey] = member
	}
	return packets, nil
}

func (e sporogenesisHotStep) execute() {
	run := e.run
	run.runtime.registry.mutex.Lock()
	defer run.runtime.registry.mutex.Unlock()
	if run.isRetired || e.index < run.tickIndex || (e.index == run.tickIndex && run.isHealingStarted) {
		return
	}
	if run.isRecipientHandoverLocked() {
		err := run.scheduleStepLocked(e.index, disconnectRetryDelay)
		if err != nil {
			run.runtime.logger.Printf("RakNet Sporogenesis handover retry failed target=%d: %v", run.target.objectID, err)
			run.retireLocked()
		}
		return
	}
	run.tickIndex = e.index
	packets, err := run.healLocked()
	if err == nil {
		run.isHealingStarted = true
	}
	if err != nil {
		run.runtime.logger.Printf("RakNet Sporogenesis tick stopped target=%d: %v", run.target.objectID, err)
	}
	if err == nil && e.index+1 < run.definition.NumberOfTicks {
		err = run.scheduleNextLocked()
		if err != nil {
			run.runtime.logger.Printf("RakNet Sporogenesis continuation stopped target=%d: %v", run.target.objectID, err)
		}
	}
	run.queuePresentationLocked(packets)
	if err != nil || e.index+1 == run.definition.NumberOfTicks || run.originZone.Context().Err() != nil {
		run.retireLocked()
	}
}

func (e *sporogenesisHot) scheduleNextLocked() error {
	if e.tickIndex+1 >= e.definition.NumberOfTicks {
		return nil
	}
	return e.scheduleStepLocked(e.tickIndex+1, e.definition.TickDuration)
}

func (e *sporogenesisHot) scheduleStepLocked(index uint32, delay time.Duration) error {
	step := sporogenesisHotStep{run: e, index: index}
	cancel, err := e.runtime.registry.timer.Schedule(delay, step.execute)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return fmt.Errorf("hotSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("hot cancellation unavailable")
	}
	e.cancel = cancel
	return nil
}

func (e *sporogenesisHot) isRecipientHandoverLocked() bool {
	if e.isNPC || e.originZone.Context().Err() != nil {
		return false
	}
	lifetime := time.Second + time.Duration(e.definition.NumberOfTicks-1)*e.definition.TickDuration
	if !e.runtime.now().Before(e.startedAt.Add(lifetime)) {
		return false
	}
	sessionKey, member, retained, isFound := e.memberLocked()
	if isFound {
		// Only semantic membership is consulted here. The retry never mutates
		// a detached handover snapshot or advances its pending heal index.
		return false
	}
	// Bind the full lookup result explicitly; unavailable aggregates carry no
	// authority and must never be installed into the active registry.
	if sessionKey != "" || member.zone != nil || retained != nil {
		return false
	}
	for _, actor := range e.originZone.Snapshot().Members {
		if actor.UserID == e.target.userID && actor.PeerGeneration == e.target.generation {
			return true
		}
	}
	return false
}

func (e *sporogenesisHot) healLocked() ([][]byte, error) {
	if e.originZone.Context().Err() != nil {
		return nil, errors.New("zone closed")
	}
	lifetime := time.Second + time.Duration(e.definition.NumberOfTicks-1)*e.definition.TickDuration
	if !e.runtime.now().Before(e.startedAt.Add(lifetime)) {
		return nil, errors.New("modifier expired")
	}
	isInstalled := false
	for _, modifier := range e.originZone.Effect().Snapshot() {
		if modifier.InstanceID == e.instanceID && modifier.TargetObjectID == e.target.objectID {
			isInstalled = true
			break
		}
	}
	if !isInstalled {
		return nil, errors.New("modifier removed")
	}
	// The five-argument HealDamage call uses the ordinary healing critical
	// pipeline, including a fresh range and critical roll for each recipient.
	projection := targetedAOESchedule{runtime: e.runtime, creature: e.creature, definition: e.definition, binding: e.binding}
	healingSession := gameplayPeerSession{}
	healingSession.zone = e.originZone
	critical, err := projection.criticalSupportHealingLocked(healingSession, sim.AreaPulseIntent{
		HealingMinimum: e.definition.MinimumHealingPerTick, HealingMaximum: e.definition.MaximumHealingPerTick,
	})
	if err != nil {
		return nil, fmt.Errorf("sporogenesisAmount: %w", err)
	}
	healing := critical.Damage
	hitPoint := float32(0)
	amount := float32(0)
	packets := make([][]byte, 0)
	if e.isNPC {
		actor, adjustedAmount, projectedHitPoint, previewErr := e.originZone.NPCs().PreviewHealing(e.target.objectID, healing, e.runtime.now())
		if previewErr != nil {
			return nil, fmt.Errorf("sporogenesisNPCPreview: %w", previewErr)
		}
		if adjustedAmount < 0 {
			return nil, errors.New("negative NPC healing")
		}
		hitPoint, amount = projectedHitPoint, projectedHitPoint-actor.HitPoint
		packets, err = e.healingPackets(actor.HitPoint, hitPoint, adjustedAmount, critical.IsCritical, false)
		if err != nil {
			return nil, fmt.Errorf("sporogenesisNPCFeedback: %w", err)
		}
		isRestored, restoreErr := e.originZone.NPCs().RestoreHealing(actor, hitPoint)
		if restoreErr != nil {
			return nil, fmt.Errorf("sporogenesisNPCCommit: %w", restoreErr)
		}
		if !isRestored {
			return nil, errors.New("NPC healing snapshot changed")
		}
	} else {
		sessionKey, member, retained, isFound := e.memberLocked()
		if !isFound {
			return nil, errors.New("recipient unavailable")
		}
		if e.target.isCompanion {
			actor, isActorFound := e.originZone.Companion().Snapshot(e.target.objectID)
			if !isActorFound || !actor.IsTargetable || actor.HitPoint <= 0 ||
				actor.UserID != e.target.userID || actor.PeerGeneration != e.target.generation ||
				actor.OwnerObjectID != e.targetOwnerID || actor.Noun != e.targetNoun {
				return nil, errors.New("companion unavailable")
			}
			hitPoint = min(actor.MaximumHitPoint, actor.HitPoint+healing)
			amount = hitPoint - actor.HitPoint
			packets, err = e.healingPackets(actor.HitPoint, hitPoint, healing, critical.IsCritical, false)
			if err != nil {
				return nil, fmt.Errorf("sporogenesisCompanionFeedback: %w", err)
			}
			isRestored, restoreErr := e.originZone.Companion().RestoreHitPoint(actor, hitPoint)
			if restoreErr != nil {
				return nil, fmt.Errorf("sporogenesisCompanionCommit: %w", restoreErr)
			}
			if !isRestored {
				return nil, errors.New("companion healing snapshot changed")
			}
			member.zone.PublishCompanionResourceTo(member.binding.UserID, member.generation, actor.ObjectID)
		} else {
			character, isCharacterFound := member.squad.Character(e.target.creatureIndex)
			if !isCharacterFound || !character.IsAvailable || character.HitPoints <= 0 {
				return nil, errors.New("hero unavailable")
			}
			maximumHitPoint, maximumManaPoint := member.characterResourceMaximum(e.target.creatureIndex)
			if maximumHitPoint <= 0 || maximumManaPoint < 0 {
				return nil, errors.New("hero maximum unavailable")
			}
			healing, err = game.ApplyTargetHealingReduction(healing, member.binding.Creatures[e.target.creatureIndex].HealingTargetProfile)
			if err != nil {
				return nil, fmt.Errorf("sporogenesisReduction: %w", err)
			}
			hitPoint = min(maximumHitPoint, character.HitPoints+healing)
			amount = hitPoint - character.HitPoints
			resourcePacket, resourceErr := member.marshalCampaignCharacterResourceAt(e.target.creatureIndex, hitPoint)
			if resourceErr != nil {
				return nil, fmt.Errorf("sporogenesisResource: %w", resourceErr)
			}
			packets, err = e.healingPackets(character.HitPoints, hitPoint, healing, critical.IsCritical, true)
			if err != nil {
				return nil, fmt.Errorf("sporogenesisHeroFeedback: %w", err)
			}
			isGameOver, setErr := member.setCampaignCharacterHitPoints(e.target.creatureIndex, hitPoint)
			if setErr != nil {
				return nil, fmt.Errorf("sporogenesisHero: %w", setErr)
			}
			if isGameOver {
				return nil, errors.New("hero healing ended squad")
			}
			member.queueCampaignPackets([][]byte{resourcePacket})
			member.zone.PublishHeroResourceTo(member.binding.UserID, member.generation)
		}
		member.queueStatDelta(sporenet.PlayerStatDelta{PVEHealingReceived: float64(amount)})
		if retained != nil {
			retained.peerSession = member
		} else {
			e.runtime.registry.sessions[sessionKey] = member
		}
	}
	for sessionKey, source := range e.runtime.registry.sessions {
		if source.zone == e.originZone && source.generation == e.sourceGeneration && source.binding.UserID == e.binding.UserID {
			source.queueStatDelta(sporenet.PlayerStatDelta{PVEHealing: float64(amount)})
			e.runtime.registry.sessions[sessionKey] = source
			break
		}
	}
	for _, retained := range e.runtime.registry.retainedSessions {
		source := &retained.peerSession
		if source.zone == e.originZone && source.generation == e.sourceGeneration && source.binding.UserID == e.binding.UserID {
			source.queueStatDelta(sporenet.PlayerStatDelta{PVEHealing: float64(amount)})
			break
		}
	}
	return packets, nil
}

func (e *sporogenesisHot) healingPackets(before, hitPoint, healing float32, isCritical, isHero bool) ([][]byte, error) {
	// Non-player HealDamage accepts a capped zero gain without an event. A
	// positive requested player heal still reports at full health.
	if healing <= 0 || (!isHero && hitPoint <= before) {
		return nil, nil
	}
	packets, err := supportHealerHealingPackets(e.sourceObjectID, e.target.objectID, before, hitPoint, healing, isCritical)
	if err != nil {
		return nil, fmt.Errorf("sporogenesisHealPacket: %w", err)
	}
	return packets, nil
}

func (e *sporogenesisHot) memberLocked() (string, gameplayPeerSession, *gameplayRetainedSession, bool) {
	for sessionKey, member := range e.runtime.registry.sessions {
		if !e.isTargetMember(member) {
			continue
		}
		return sessionKey, member, nil, true
	}
	for _, retained := range e.runtime.registry.retainedSessions {
		if e.isTargetMember(retained.peerSession) {
			return "", retained.peerSession, retained, true
		}
	}
	return "", gameplayPeerSession{}, nil, false
}

func (e *sporogenesisHot) isTargetMember(member gameplayPeerSession) bool {
	if member.zone != e.originZone || member.generation != e.target.generation || member.binding.UserID != e.target.userID || member.squad == nil || member.isZoneTerminal() {
		return false
	}
	return e.target.isCompanion || (e.target.creatureIndex < uint32(len(member.binding.Creatures)) &&
		zonehero.ObjectID(member.binding.Slot, e.target.creatureIndex) == e.target.objectID)
}

func (e *sporogenesisHot) queuePresentationLocked(packets [][]byte) {
	e.runtime.registry.queueChannelPresentationLocked(e.originZone, packets)
	// The world-owned HoT continues while endpoints hand off. Preserve the
	// same ordered create/heal/delete history in the retained member queue.
	for sessionKey, member := range e.runtime.registry.sessions {
		if member.zone == e.originZone && !member.isZoneTerminal() &&
			(member.isRejoinPending || !member.isCampaignPresentationAvailable()) {
			member.queueCampaignPackets(packets)
			e.runtime.registry.sessions[sessionKey] = member
		}
	}
	e.queueRetainedLocked(packets)
}

func (e *sporogenesisHot) queueRetainedLocked(packets [][]byte) {
	for _, retained := range e.runtime.registry.retainedSessions {
		member := &retained.peerSession
		if member.zone == e.originZone && !member.isZoneTerminal() {
			member.queueCampaignPackets(packets)
		}
	}
}

func (e *sporogenesisHot) retireLocked() [][]byte {
	if e.isRetired {
		return nil
	}
	e.isRetired = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.instanceID == 0 {
		return nil
	}
	if e.isPublished {
		packet, marshalErr := raknet.MarshalApplication(raknet.ModifierDeletedMessage{TargetID: e.target.objectID, InstanceID: e.instanceID})
		if marshalErr != nil {
			e.runtime.logger.Printf("RakNet Sporogenesis modifier deletion failed instance=%d: %v", e.instanceID, marshalErr)
		} else {
			// Queue exact-instance retirement before releasing its handle.
			e.runtime.registry.queueChannelRemovalsLocked(e.originZone, [][]byte{packet})
			e.queueRetainedLocked([][]byte{packet})
		}
	}
	e.originZone.Effect().Remove(e.instanceID)
	err := e.runtime.modifierPool.Release(e.instanceID)
	if err != nil {
		e.runtime.logger.Printf("RakNet Sporogenesis modifier release failed instance=%d: %v", e.instanceID, err)
	}
	return nil
}
