package gameplay

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	actionraknet "github.com/darkspinnet/darkspin/server/zone/action/raknet103"
	zonecompanion "github.com/darkspinnet/darkspin/server/zone/companion"
	companionraknet "github.com/darkspinnet/darkspin/server/zone/companion/raknet103"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type heroChargeModifier struct {
	targetObjectID uint32
	instanceID     uint32
	expiresAt      time.Time
}

type heroChargeRun struct {
	mutex        sync.Mutex
	npc          *zonenpc.Session
	companion    *zonecompanion.Session
	modifierPool *modifierPool
	assetName    string
	modifier     []heroChargeModifier
	petPursuit   zonecompanion.Pursuit
	cancel       raknet.CancelSchedule
	isCleaned    bool
}

func (e *heroChargeRun) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	cancel := e.cancel
	e.cancel = nil
	e.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	e.cleanup()
}

// Interrupt stops a charge which has not reached impact. Once modifiers have
// been published, their scheduled deletion remains authoritative even if the
// player starts another interruptible action.
func (e *heroChargeRun) Interrupt() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	isImpacted := len(e.modifier) != 0
	e.mutex.Unlock()
	if isImpacted {
		return
	}
	e.Stop()
}

func (e *heroChargeRun) addModifier(modifier heroChargeModifier) bool {
	if e == nil || modifier.targetObjectID == 0 || modifier.instanceID == 0 ||
		modifier.expiresAt.IsZero() {
		return false
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return false
	}
	e.modifier = append(e.modifier, modifier)
	return true
}

func (e *heroChargeRun) cleanup() []heroChargeModifier {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.isCleaned {
		return nil
	}
	e.isCleaned = true
	if e.petPursuit.ObjectID != 0 && e.companion != nil {
		e.companion.CancelPursuit(
			e.petPursuit.ObjectID, e.petPursuit.TargetObjectID,
			e.petPursuit.Position,
		)
		e.petPursuit = zonecompanion.Pursuit{}
	}
	modifier := e.modifier
	e.modifier = nil
	for _, current := range modifier {
		switch e.assetName {
		case "BeastCharge":
			e.npc.ClearStun(current.targetObjectID, current.expiresAt)
		case "EntanglingRush":
			e.npc.ClearRoot(current.targetObjectID, current.expiresAt)
		case "PhantomCharge":
			e.npc.ClearSilence(current.targetObjectID, current.expiresAt)
		}
		_ = e.modifierPool.Release(current.instanceID)
	}
	return modifier
}

type heroChargeSchedule struct {
	runtime             campaignAbilityCommandRuntime
	packet              raknet.Packet
	sessionKey          string
	generation          uint64
	sourceObjectID      uint32
	abilityID           uint32
	targetObjectID      uint32
	creatureIndex       uint32
	previousManaPoint   float32
	destination         raknet.Vector3
	targetPosition      raknet.Vector3
	startPosition       raknet.Vector3
	impactDelay         time.Duration
	petImpactDelay      time.Duration
	finishDelay         time.Duration
	creature            game.GameplayCreature
	definition          sim.AbilityDefinition
	damage              game.DamageRange
	petDamage           game.DamageRange
	petPursuit          zonecompanion.Pursuit
	binding             game.GameplayBinding
	run                 *heroChargeRun
	cooldownReservation zoneability.CooldownReservation
	releaseReservation  zoneaction.ReleaseReservation
	releasePacket       []byte
}

func (e heroChargeSchedule) petImpact() ([][]byte, error) {
	if e.petPursuit.ObjectID == 0 {
		return nil, nil
	}
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) || peerSession.zone == nil ||
		peerSession.zone.Companion() == nil || peerSession.zone.NPCs() == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	isReleased := e.petPursuit.TravelDuration == 0
	if !isReleased {
		isReleased = peerSession.zone.Companion().ReleasePursuit(
			e.petPursuit.ObjectID, e.petPursuit.TargetObjectID,
		)
	}
	if !isReleased {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	e.run.mutex.Lock()
	e.run.petPursuit = zonecompanion.Pursuit{}
	e.run.mutex.Unlock()
	pet, isPetFound := peerSession.zone.Companion().Snapshot(e.petPursuit.ObjectID)
	if !isPetFound || pet.HitPoint <= 0 {
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	results, transitions, modifierPackets, err := e.commitBeastPetPassThrough(
		&peerSession, pet,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("beastPetChargePassThrough: %w", err)
	}
	terminalResult, terminalTransition, isTerminalHit, err :=
		e.commitBeastPetTerminal(&peerSession, pet)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("beastPetChargeTerminal: %w", err)
	}
	if isTerminalHit {
		results = append(results, terminalResult)
		transitions = append(transitions, terminalTransition)
	}
	if activation, isActivationFound :=
		peerSession.sagePassiveActivations[pet.ObjectID]; isActivationFound {
		activation.Position = raknet.Vector3(pet.Position)
		activation.TargetObjectID = 0
		activation.Attack = nil
		peerSession.sagePassiveActivations[pet.ObjectID] = activation
	}
	binding := peerSession.binding
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	arrivalPackets, err := beastPetChargeArrivalPackets(pet)
	if err != nil {
		return nil, fmt.Errorf("beastPetChargeArrival: %w", err)
	}
	impact := campaignPointBlankImpact{
		assetName:  "beastSentinel_charge_hit.ServerEventDef",
		attackerID: pet.ObjectID, source: raknet.Vector3(pet.Position),
	}
	damagePackets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, pet.ObjectID,
		e.packet.SourceTime+uint64(e.petImpactDelay/time.Millisecond), binding,
		results, transitions, impact.marshal, true,
	)
	if err != nil {
		return nil, fmt.Errorf("beastPetChargePublish: %w", err)
	}
	resumePackets, err := e.resumeBeastPet()
	if err != nil {
		return nil, fmt.Errorf("beastPetChargeResume: %w", err)
	}
	packets := append(arrivalPackets, damagePackets...)
	packets = append(packets, modifierPackets...)
	return append(packets, resumePackets...), nil
}

func (e heroChargeSchedule) commitBeastPetPassThrough(
	peerSession *gameplayPeerSession, pet zonecompanion.Actor,
) ([]zoneability.AreaResult, []campaignDamageTransition, [][]byte, error) {
	targets := make([]zonenpc.Snapshot, 0)
	for _, npc := range peerSession.zone.NPCs().LiveSnapshots() {
		if npc.Faction != zonenpc.FactionNonPlayerAligned ||
			chargeSegmentDistance(
				game.Vec3(e.petPursuit.Position), game.Vec3(pet.Position),
				npc.Plan.Position,
			) > e.definition.Radius+npc.Plan.NPCProfile.FootprintRadius {
			continue
		}
		targets = append(targets, npc)
	}
	plan := zoneability.AreaPlan{
		SourceObjectID: pet.ObjectID, AbilityID: e.abilityID,
		Definition: e.definition, Damage: e.damage,
		Center: game.Vec3(pet.Position), Target: targets,
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.NPCRandom(), peerSession.zone.NPCs(), plan,
		e.creature, peerSession.binding.Difficulty, e.runtime.program.Critical,
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("damage: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for index, result := range results {
		transition, transitionErr := peerSession.applyCampaignDamageTransition(
			result.Damage,
		)
		if transitionErr != nil {
			return nil, nil, nil, fmt.Errorf("transition[%d]: %w", index, transitionErr)
		}
		transitions = append(transitions, transition)
	}
	modifierPackets, err := e.applyBeastPetChargeStatus(peerSession, pet, results)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("status: %w", err)
	}
	return results, transitions, modifierPackets, nil
}

func (e heroChargeSchedule) applyBeastPetChargeStatus(
	peerSession *gameplayPeerSession, pet zonecompanion.Actor,
	results []zoneability.AreaResult,
) ([][]byte, error) {
	targets := e.statusTargets(peerSession.zone.NPCs(), results)
	packets := make([][]byte, 0, len(targets))
	expiresAt := e.runtime.now().Add(e.definition.StatusDuration)
	for _, npc := range targets {
		err := e.applyStatus(peerSession.zone.NPCs(), npc.Plan.ObjectID, expiresAt)
		if err != nil {
			return nil, fmt.Errorf("apply[%d]: %w", npc.Plan.ObjectID, err)
		}
		instanceID, err := e.runtime.modifierPool.Allocate()
		if err != nil {
			e.clearStatus(peerSession.zone.NPCs(), npc.Plan.ObjectID, expiresAt)
			return nil, fmt.Errorf("allocate[%d]: %w", npc.Plan.ObjectID, err)
		}
		modifier := heroChargeModifier{
			targetObjectID: npc.Plan.ObjectID, instanceID: instanceID,
			expiresAt: expiresAt,
		}
		if !e.run.addModifier(modifier) {
			e.clearStatus(peerSession.zone.NPCs(), npc.Plan.ObjectID, expiresAt)
			_ = e.runtime.modifierPool.Release(instanceID)
			continue
		}
		packet, err := effectraknet.ModifierCreate(
			effectraknet.ModifierCreateRequest{
				SourceObjectID: pet.ObjectID, TargetObjectID: npc.Plan.ObjectID,
				ModifierID: e.definition.RootModifierID, InstanceID: instanceID,
				Duration: e.definition.StatusDuration,
				Timestamp: e.packet.SourceTime +
					uint64(e.petImpactDelay/time.Millisecond),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("marshal[%d]: %w", npc.Plan.ObjectID, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e heroChargeSchedule) commitBeastPetTerminal(
	peerSession *gameplayPeerSession, pet zonecompanion.Actor,
) (zoneability.AreaResult, campaignDamageTransition, bool, error) {
	target, isTargetFound := peerSession.zone.NPCs().NPC(e.petPursuit.TargetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
		return zoneability.AreaResult{}, campaignDamageTransition{}, false, nil
	}
	selectedDamage, err := sim.SelectRankDamage(
		peerSession.zone.NPCRandom(),
		sim.DamageRange{Minimum: e.petDamage.Minimum, Maximum: e.petDamage.Maximum},
	)
	if err != nil {
		return zoneability.AreaResult{}, campaignDamageTransition{}, false,
			fmt.Errorf("damage: %w", err)
	}
	selectedDamage *= 1 + pet.DamageBuff
	critical, err := sim.ResolveCriticalDamage(
		peerSession.zone.NPCRandom(), selectedDamage, peerSession.binding.Difficulty,
		sim.CriticalProfile{
			Rating: e.creature.CriticalRating, AutoCrit: e.creature.AutoCrit,
			DamageIncrease: e.creature.CriticalDamageIncrease,
		},
		e.runtime.program.Critical,
	)
	if err != nil {
		return zoneability.AreaResult{}, campaignDamageTransition{}, false,
			fmt.Errorf("critical: %w", err)
	}
	damage, err := peerSession.zone.NPCs().Damage(
		pet.ObjectID, target.Plan.ObjectID, critical.Damage,
		uint32(physicalDamageSource),
	)
	if err != nil {
		return zoneability.AreaResult{}, campaignDamageTransition{}, false,
			fmt.Errorf("commit: %w", err)
	}
	transition, err := peerSession.applyCampaignDamageTransition(damage)
	if err != nil {
		return zoneability.AreaResult{}, campaignDamageTransition{}, false,
			fmt.Errorf("transition: %w", err)
	}
	result := zoneability.AreaResult{
		Snapshot: target, Damage: damage, IsCritical: critical.IsCritical,
		Definition: e.definition,
	}
	return result, transition, true, nil
}

func (e heroChargeSchedule) resumeBeastPet() ([][]byte, error) {
	return e.runtime.damage.startBeastPetAttack(
		e.packet, e.sessionKey, e.generation,
		e.packet.SourceTime+uint64(e.petImpactDelay/time.Millisecond),
	)
}

func beastPetChargeArrivalPackets(pet zonecompanion.Actor) ([][]byte, error) {
	if pet.ObjectID == 0 || pet.HitPoint <= 0 {
		return nil, nil
	}
	positionPacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: pet.ObjectID, PositionX: pet.Position.X,
		PositionY: pet.Position.Y, PositionZ: pet.Position.Z, IsVisible: true,
	})
	if err != nil {
		return nil, fmt.Errorf("positionMarshal: %w", err)
	}
	speedPacket, err := heroChargeSpeedPacket(
		pet.ObjectID, zonecompanion.CompatibilityMovementSpeed,
	)
	if err != nil {
		return nil, fmt.Errorf("speedRestore: %w", err)
	}
	return [][]byte{positionPacket, speedPacket}, nil
}

func (e heroChargeSchedule) isCurrent(
	peerSession gameplayPeerSession, isFound bool,
) bool {
	return isFound && peerSession.generation == e.generation &&
		peerSession.heroCharge == e.run
}

func (e heroChargeSchedule) impact() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCurrent(peerSession, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	err := peerSession.teleportPlayer(e.runtime.now(), e.destination)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargePosition: %w", err)
	}
	if e.definition.Name == "BioRandom2" {
		return e.impactRoar(&peerSession)
	}
	target := e.damageTargets(peerSession.zone.NPCs())
	plan := zoneability.AreaPlan{
		SourceObjectID: e.sourceObjectID,
		AbilityID:      e.abilityID,
		Definition:     e.definition,
		Damage:         e.damage,
		Center:         game.Vec3(e.destination),
		Target:         target,
	}
	results, err := zoneability.CommitArea(
		peerSession.zone.Population().Random(), peerSession.zone.NPCs(), plan,
		e.creature, peerSession.binding.Difficulty, e.runtime.program.Critical,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargeDamage: %w", err)
	}
	transitions := make([]campaignDamageTransition, 0, len(results))
	for index, result := range results {
		transition, transitionErr := peerSession.applyCampaignDamageTransition(result.Damage)
		if transitionErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroChargeTransition[%d]: %w", index, transitionErr)
		}
		transitions = append(transitions, transition)
	}
	statusTargets := e.statusTargets(peerSession.zone.NPCs(), results)
	modifierPackets := make([][]byte, 0, len(statusTargets))
	expiresAt := e.runtime.now().Add(e.definition.StatusDuration)
	for _, npc := range statusTargets {
		applyErr := e.applyStatus(peerSession.zone.NPCs(), npc.Plan.ObjectID, expiresAt)
		if applyErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroChargeStatus[%d]: %w", npc.Plan.ObjectID, applyErr)
		}
		instanceID, allocateErr := e.runtime.modifierPool.Allocate()
		if allocateErr != nil {
			e.clearStatus(peerSession.zone.NPCs(), npc.Plan.ObjectID, expiresAt)
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroChargeModifier[%d]: %w", npc.Plan.ObjectID, allocateErr)
		}
		modifier := heroChargeModifier{
			targetObjectID: npc.Plan.ObjectID, instanceID: instanceID,
			expiresAt: expiresAt,
		}
		if !e.run.addModifier(modifier) {
			e.clearStatus(peerSession.zone.NPCs(), npc.Plan.ObjectID, expiresAt)
			_ = e.runtime.modifierPool.Release(instanceID)
			continue
		}
		modifierPacket, marshalErr := effectraknet.ModifierCreate(
			effectraknet.ModifierCreateRequest{
				SourceObjectID: e.sourceObjectID, TargetObjectID: npc.Plan.ObjectID,
				ModifierID: e.definition.RootModifierID, InstanceID: instanceID,
				Duration:  e.definition.StatusDuration,
				Timestamp: e.packet.SourceTime + uint64(e.impactDelay/time.Millisecond),
			},
		)
		if marshalErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroChargeModifierMarshal[%d]: %w", npc.Plan.ObjectID, marshalErr)
		}
		modifierPackets = append(modifierPackets, modifierPacket)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()

	positionPacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: e.sourceObjectID, PositionX: e.destination.X,
		PositionY: e.destination.Y, PositionZ: e.destination.Z, IsVisible: true,
	})
	if err != nil {
		return nil, fmt.Errorf("heroChargePositionMarshal: %w", err)
	}
	stopPackets, err := marshalZonePlayerStop(e.sourceObjectID, e.destination)
	if err != nil {
		return nil, fmt.Errorf("heroChargeStopMarshal: %w", err)
	}
	speedPacket, err := heroChargeSpeedPacket(e.sourceObjectID, zonePlayerMoveSpeed)
	if err != nil {
		return nil, fmt.Errorf("heroChargeSpeedRestore: %w", err)
	}
	var effect func(zoneability.AreaResult) ([]byte, error)
	if e.definition.HitEffectName != "" {
		impact := campaignPointBlankImpact{
			assetName: e.definition.HitEffectName, attackerID: e.sourceObjectID,
			source: e.destination,
		}
		effect = impact.marshal
	}
	damagePackets, err := e.runtime.damage.publishAreaResults(
		e.packet, e.sessionKey, e.generation, e.sourceObjectID,
		e.packet.SourceTime+uint64(e.impactDelay/time.Millisecond),
		e.binding, results, transitions, effect, true,
	)
	if err != nil {
		return nil, fmt.Errorf("heroChargePublish: %w", err)
	}
	packets := [][]byte{positionPacket}
	packets = append(packets, stopPackets...)
	packets = append(packets, speedPacket)
	if e.definition.Name == "EntanglingRush" {
		// Contact ends the running loop; ReleaseDelay belongs to the ending
		// animation, not an extra second of charging at a stationary target.
		animationPacket, animationErr := abilityraknet.Animation(
			e.sourceObjectID, e.definition.OutAnimationName,
			e.packet.SourceTime+uint64(e.impactDelay/time.Millisecond),
		)
		if animationErr != nil {
			return nil, fmt.Errorf("rushContactAnimation: %w", animationErr)
		}
		effectPacket, effectErr := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset:    util.HashID(e.definition.MuzzleEffectName),
			Position: e.targetPosition,
		})
		if effectErr != nil {
			return nil, fmt.Errorf("rushContactEffect: %w", effectErr)
		}
		packets = append(packets, animationPacket, effectPacket)
	}
	packets = append(packets, damagePackets...)
	packets = append(packets, modifierPackets...)
	if e.definition.Name == "BeastCharge" {
		timestamp := e.packet.SourceTime + uint64(e.impactDelay/time.Millisecond)
		for _, npc := range statusTargets {
			reactionPacket, reactionErr := abilityraknet.Animation(
				npc.Plan.ObjectID, "react_knockup", timestamp,
			)
			if reactionErr != nil {
				return nil, fmt.Errorf(
					"heroChargeKnockup[%d]: %w", npc.Plan.ObjectID, reactionErr,
				)
			}
			packets = append(packets, reactionPacket)
		}
	}
	return packets, nil
}

func (e heroChargeSchedule) impactRoar(
	peerSession *gameplayPeerSession,
) ([][]byte, error) {
	if peerSession == nil || peerSession.zone == nil ||
		e.definition.RootModifierID == 0 ||
		e.definition.SecondaryModifierID == 0 ||
		e.definition.TickDuration <= 0 || e.definition.StatusDuration <= 0 {
		e.runtime.registry.mutex.Unlock()
		return nil, errors.New("hero roar runtime unavailable")
	}
	now := e.runtime.now()
	acquired, err := peerSession.zone.TauntHero(
		peerSession.binding.UserID, peerSession.generation,
		e.sourceObjectID, e.definition.Radius,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroRoarTaunt: %w", err)
	}
	modifierPackets := make([][]byte, 0, len(acquired)+1)
	for _, npc := range acquired {
		instanceID, allocateErr := e.runtime.modifierPool.Allocate()
		if allocateErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroRoarTauntModifier: %w", allocateErr)
		}
		modifier := heroChargeModifier{
			targetObjectID: npc.Plan.ObjectID, instanceID: instanceID,
			expiresAt: now.Add(e.definition.TickDuration),
		}
		if !e.run.addModifier(modifier) {
			_ = e.runtime.modifierPool.Release(instanceID)
			continue
		}
		packet, marshalErr := effectraknet.ModifierCreate(
			effectraknet.ModifierCreateRequest{
				SourceObjectID: e.sourceObjectID,
				TargetObjectID: npc.Plan.ObjectID,
				ModifierID:     e.definition.RootModifierID,
				InstanceID:     instanceID,
				Duration:       e.definition.TickDuration,
				Timestamp: e.packet.SourceTime +
					uint64(e.impactDelay/time.Millisecond),
			},
		)
		if marshalErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroRoarTauntMarshal: %w", marshalErr)
		}
		modifierPackets = append(modifierPackets, packet)
	}
	buffInstanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroRoarBuffModifier: %w", err)
	}
	buff := heroChargeModifier{
		targetObjectID: e.sourceObjectID, instanceID: buffInstanceID,
		expiresAt: now.Add(e.definition.StatusDuration),
	}
	if !e.run.addModifier(buff) {
		_ = e.runtime.modifierPool.Release(buffInstanceID)
		e.runtime.registry.mutex.Unlock()
		return nil, errors.New("hero roar buff stopped")
	}
	buffPacket, err := effectraknet.ModifierCreate(
		effectraknet.ModifierCreateRequest{
			SourceObjectID: e.sourceObjectID, TargetObjectID: e.sourceObjectID,
			ModifierID: e.definition.SecondaryModifierID,
			InstanceID: buffInstanceID, Duration: e.definition.StatusDuration,
			Timestamp: e.packet.SourceTime + uint64(e.impactDelay/time.Millisecond),
		},
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroRoarBuffMarshal: %w", err)
	}
	modifierPackets = append(modifierPackets, buffPacket)
	peerSession.roarReductionObjectID = e.sourceObjectID
	peerSession.roarReductionExpiresAt = now.Add(e.definition.StatusDuration)
	e.runtime.registry.sessions[e.sessionKey] = *peerSession
	e.runtime.registry.mutex.Unlock()

	positionPacket, err := raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: e.sourceObjectID, PositionX: e.destination.X,
		PositionY: e.destination.Y, PositionZ: e.destination.Z, IsVisible: true,
	})
	if err != nil {
		return nil, fmt.Errorf("heroRoarPositionMarshal: %w", err)
	}
	speedPacket, err := heroChargeSpeedPacket(e.sourceObjectID, zonePlayerMoveSpeed)
	if err != nil {
		return nil, fmt.Errorf("heroRoarSpeedRestore: %w", err)
	}
	plan := make([]zonenpc.SpawnPlan, 0, len(acquired))
	for _, npc := range acquired {
		plan = append(plan, npc.Plan)
	}
	actionPackets, err := e.runtime.npc.scheduleFirstActions(
		e.packet, e.sessionKey, e.generation, plan,
		e.packet.SourceTime+uint64(e.impactDelay/time.Millisecond),
	)
	if err != nil {
		return nil, fmt.Errorf("heroRoarTauntSchedule: %w", err)
	}
	packets := [][]byte{positionPacket, speedPacket}
	if e.definition.ActivationEffectName != "" {
		// Roar's onFinishCharge emits a world effect at the resulting root;
		// attaching it to the caster changes its transform and scale.
		effectPacket, effectErr := raknet.MarshalApplication(raknet.DropPresentationMessage{
			Asset: util.HashID(e.definition.ActivationEffectName), Position: e.destination,
		})
		if effectErr != nil {
			return nil, fmt.Errorf("roarEffect: %w", effectErr)
		}
		packets = append(packets, effectPacket)
	}
	packets = append(packets, modifierPackets...)
	return append(packets, actionPackets...), nil
}

func (e heroChargeSchedule) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	packets := make([][]byte, 0, 2)
	if e.definition.Name == "EntanglingRush" {
		animationPacket, err := abilityraknet.AnimationReset(
			e.sourceObjectID,
			e.packet.SourceTime+uint64(e.finishDelay/time.Millisecond),
		)
		if err != nil {
			return nil, fmt.Errorf("rushReleaseAnimation: %w", err)
		}
		packets = append(packets, animationPacket)
	}
	if e.definition.OutAnimationName != "" && e.definition.Name != "EntanglingRush" {
		animationPacket, err := abilityraknet.Animation(
			e.sourceObjectID, e.definition.OutAnimationName,
			e.packet.SourceTime+uint64(e.finishDelay/time.Millisecond),
		)
		if err != nil {
			return nil, fmt.Errorf("heroChargeRelax: %w", err)
		}
		packets = append(packets, animationPacket)
	}
	packets = append(packets, e.releasePacket)
	return packets, nil
}

func (e heroChargeSchedule) finish() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.heroCharge == e.run {
		peerSession.heroCharge = nil
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	modifier := e.run.cleanup()
	packets := make([][]byte, 0, len(modifier))
	for _, current := range modifier {
		packet, err := effectraknet.ModifierDelete(
			current.targetObjectID, current.instanceID,
		)
		if err != nil {
			return nil, fmt.Errorf("heroChargeModifierDelete: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e heroChargeSchedule) fail(scheduleErr error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.isCurrent(peerSession, isFound)
	if isCurrent {
		peerSession.heroCharge = nil
		_ = peerSession.setCampaignCharacterManaPoints(
			e.creatureIndex, e.previousManaPoint,
		)
		peerSession.abilityCooldownSession().Rollback(e.cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(e.releaseReservation)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return
	}
	e.run.Stop()
	e.runtime.logger.Printf(
		"RakNet hero charge stopped after schedule failure for %s: %v",
		e.sessionKey, scheduleErr,
	)
}

func (e heroChargeSchedule) damageTargets(npc *zonenpc.Session) []zonenpc.Snapshot {
	if e.definition.Name == "EntanglingRush" {
		target, isFound := npc.NPC(e.targetObjectID)
		if !isFound || target.IsDefeated || target.HitPoint <= 0 {
			return nil
		}
		return []zonenpc.Snapshot{target}
	}
	target := make([]zonenpc.Snapshot, 0)
	for _, current := range npc.LiveSnapshots() {
		if current.Faction != zonenpc.FactionNonPlayerAligned ||
			chargeSegmentDistance(
				game.Vec3(e.startPosition), game.Vec3(e.destination),
				current.Plan.Position,
			) > e.definition.Radius+current.Plan.NPCProfile.FootprintRadius {
			continue
		}
		target = append(target, current)
	}
	return target
}

func (e heroChargeSchedule) statusTargets(
	npc *zonenpc.Session, results []zoneability.AreaResult,
) []zonenpc.Snapshot {
	if e.definition.Name != "EntanglingRush" {
		target := make([]zonenpc.Snapshot, 0, len(results))
		for _, result := range results {
			if !result.Damage.IsDamageImmune && !result.Damage.IsDefeated {
				target = append(target, result.Snapshot)
			}
		}
		return target
	}
	target := make([]zonenpc.Snapshot, 0)
	for _, current := range npc.LiveSnapshots() {
		if current.Faction == zonenpc.FactionNonPlayerAligned &&
			game.Vec3(e.targetPosition).Sub(current.Plan.Position).Length() <= e.definition.Radius {
			target = append(target, current)
		}
	}
	return target
}

func (e heroChargeSchedule) applyStatus(
	npc *zonenpc.Session, targetObjectID uint32, expiresAt time.Time,
) error {
	switch e.definition.Name {
	case "BeastCharge":
		return npc.ApplyStun(targetObjectID, expiresAt)
	case "EntanglingRush":
		return npc.ApplyRoot(targetObjectID, expiresAt)
	case "PhantomCharge":
		return npc.ApplySilence(targetObjectID, expiresAt)
	default:
		return errors.New("hero charge status unavailable")
	}
}

func (e heroChargeSchedule) clearStatus(
	npc *zonenpc.Session, targetObjectID uint32, expiresAt time.Time,
) {
	switch e.definition.Name {
	case "BeastCharge":
		npc.ClearStun(targetObjectID, expiresAt)
	case "EntanglingRush":
		npc.ClearRoot(targetObjectID, expiresAt)
	case "PhantomCharge":
		npc.ClearSilence(targetObjectID, expiresAt)
	}
}

func prepareBeastPetCharge(
	peerSession *gameplayPeerSession, target zonenpc.Snapshot,
) (zonecompanion.Pursuit, game.DamageRange, [][]byte, error) {
	if peerSession == nil || peerSession.beastPetObjectID == 0 ||
		peerSession.zone == nil || peerSession.zone.Companion() == nil {
		return zonecompanion.Pursuit{}, game.DamageRange{}, nil, nil
	}
	companion := peerSession.zone.Companion()
	pet, isPetFound := companion.Snapshot(peerSession.beastPetObjectID)
	if !isPetFound || pet.HitPoint <= 0 || !pet.IsCombatant {
		return zonecompanion.Pursuit{}, game.DamageRange{}, nil, nil
	}
	centerRange := float32(0.2) + pet.FootprintRadius +
		max(float32(0), target.Plan.NPCProfile.FootprintRadius)
	distance := pet.Position.Sub(target.Plan.Position).Length()
	if distance > 125 {
		return zonecompanion.Pursuit{}, game.DamageRange{}, nil, nil
	}
	if activation, isActivationFound :=
		peerSession.sagePassiveActivations[pet.ObjectID]; isActivationFound {
		if activation.Attack != nil {
			activation.Attack.Stop()
		}
		activation.Attack = nil
		activation.TargetObjectID = 0
		peerSession.sagePassiveActivations[pet.ObjectID] = activation
	}
	if pet.TargetObjectID != 0 {
		companion.ReleaseAttack(pet.ObjectID, pet.TargetObjectID)
	}
	if pet.PursuitObjectID != 0 {
		companion.CancelPursuit(
			pet.ObjectID, pet.PursuitObjectID, pet.Position,
		)
	}
	pet, _ = companion.Snapshot(pet.ObjectID)
	pursuit := zonecompanion.Pursuit{
		ObjectID: pet.ObjectID, TargetObjectID: target.Plan.ObjectID,
		Position: pet.Position, TargetPosition: target.Plan.Position,
		DesiredStopDistance: centerRange,
	}
	packets := make([][]byte, 0, 3)
	if distance > centerRange {
		reserved, isReserved, err := companion.ReserveActorPursuit(
			pet.ObjectID, []zonenpc.Snapshot{target}, 0.2, 125,
			zonePlayerMoveSpeed*3,
		)
		if err != nil {
			return zonecompanion.Pursuit{}, game.DamageRange{}, nil,
				fmt.Errorf("beastPetChargeReserve: %w", err)
		}
		if !isReserved {
			return zonecompanion.Pursuit{}, game.DamageRange{}, nil, nil
		}
		pursuit = reserved
		movePackets, err := companionraknet.Pursuit(pursuit)
		if err != nil {
			companion.CancelPursuit(
				pursuit.ObjectID, pursuit.TargetObjectID, pursuit.Position,
			)
			return zonecompanion.Pursuit{}, game.DamageRange{}, nil,
				fmt.Errorf("beastPetChargeMove: %w", err)
		}
		speedPacket, err := heroChargeSpeedPacket(
			pet.ObjectID, zonePlayerMoveSpeed*3,
		)
		if err != nil {
			companion.CancelPursuit(
				pursuit.ObjectID, pursuit.TargetObjectID, pursuit.Position,
			)
			return zonecompanion.Pursuit{}, game.DamageRange{}, nil,
				fmt.Errorf("beastPetChargeSpeed: %w", err)
		}
		packets = append(packets, speedPacket)
		packets = append(packets, movePackets...)
	}
	damage, err := game.ResolveAbilityDamageRange(
		game.AbilityDamage{Minimum: 8, Maximum: 12, Coefficient: 0.05},
		game.DamageProfile{
			PrimaryAttribute:        peerSession.beastPetDamage,
			IsPrimaryAttributeFound: true,
		},
	)
	if err != nil {
		if pursuit.TravelDuration > 0 {
			companion.CancelPursuit(
				pursuit.ObjectID, pursuit.TargetObjectID, pursuit.Position,
			)
		}
		return zonecompanion.Pursuit{}, game.DamageRange{}, nil,
			fmt.Errorf("beastPetChargeProjection: %w", err)
	}
	if peerSession.beastPetDamageIncrease > 0 {
		damage.Minimum *= 1 + peerSession.beastPetDamageIncrease
		damage.Maximum *= 1 + peerSession.beastPetDamageIncrease
	}
	return pursuit, damage, packets, nil
}

func (r campaignAbilityCommandRuntime) handleHeroCharge(
	req campaignCharacterAbilityRequest, peerSession gameplayPeerSession,
	creature game.GameplayCreature, definition sim.AbilityDefinition,
	activeAbilityID uint32, targetObjectID uint32, sessionKey string,
	abilityStartTime time.Time,
) ([][]byte, error) {
	if activeAbilityID == 0 || definition.Kind != sim.AbilityKindCharge ||
		definition.Range <= 0 || definition.Radius <= 0 ||
		definition.MovementSpeedMultiplier <= 0 || definition.AnimationName == "" ||
		definition.RootModifierID == 0 || definition.StatusDuration <= 0 ||
		(definition.Name != "BioRandom2" &&
			(definition.MinimumDamage <= 0 ||
				definition.MaximumDamage < definition.MinimumDamage)) ||
		peerSession.heroCharge != nil {
		r.registry.mutex.Unlock()
		return req.reject("charge definition or runtime unavailable")
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(targetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 ||
		target.Faction != zonenpc.FactionNonPlayerAligned {
		r.registry.mutex.Unlock()
		return req.reject("charge target unavailable")
	}
	projected, err := zoneability.ProjectTiming(creature, definition)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargeTiming: %w", err)
	}
	damage := game.DamageRange{}
	if projected.Name != "BioRandom2" {
		damage, err = zoneability.ProjectDamage(
			creature, projected, projected.MinimumDamage, projected.MaximumDamage,
		)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroChargeDamage: %w", err)
		}
	}
	startPosition := peerSession.playerPosition
	targetPosition := raknet.Vector3(target.Plan.Position)
	distance := chargeDistance(startPosition, targetPosition)
	admissionRange := heroAbilityAdmissionRange(creature, projected)
	if distance > admissionRange {
		r.registry.mutex.Unlock()
		return req.reject("charge target out of range")
	}
	stopDistance := projected.Distance
	if projected.Name == "BeastCharge" {
		stopDistance = peerSession.deployedCampaignFootprintRadius() +
			max(float32(0.5), target.Plan.NPCProfile.FootprintRadius) + 0.2
	}
	destination := chargeDestination(startPosition, targetPosition, stopDistance)
	if projected.Name == "PhantomCharge" {
		destination = chargeThroughDestination(startPosition, targetPosition, projected.Radius)
	}
	travelDelay := time.Duration(
		float64(chargeDistance(startPosition, destination)) /
			float64(zonePlayerMoveSpeed*projected.MovementSpeedMultiplier) *
			float64(time.Second),
	)
	impactDelay := projected.HitDelay + max(travelDelay, 50*time.Millisecond)
	finishDelay := impactDelay + projected.ReleaseDelay
	cleanupDelay := impactDelay + projected.StatusDuration
	cleanupDelay = max(cleanupDelay, finishDelay)
	manaCost, err := game.ResolveAbilityManaCost(
		projected.ManaCost, creature.DamageProfile.PrimaryAttribute, projected.ManaCoefficient,
		peerSession.isOverdriveActiveAt(abilityStartTime),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargeMana: %w", err)
	}
	if peerSession.deployedManaPoint() < manaCost {
		r.registry.mutex.Unlock()
		return req.reject("power unavailable")
	}
	cooldown, err := zoneability.ProjectCooldown(creature, projected)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargeCooldown: %w", err)
	}
	ackPacket, err := abilityraknet.Acknowledge(abilityraknet.AcknowledgeRequest{
		SyncStamp: req.command.Common.Unknown[0], ResponseType: raknet.ActionResponseAccepted,
		ObjectID: activeAbilityID, AbilityIndex: req.command.Ability.Index,
		SourceStartMilliseconds:  req.packet.SourceTime,
		SourceCommitMilliseconds: req.packet.SourceTime + uint64(projected.HitDelay/time.Millisecond),
		SourceEndMilliseconds:    req.packet.SourceTime + uint64(finishDelay/time.Millisecond),
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargeAck: %w", err)
	}
	releasePacket, err := abilityraknet.ReleaseResponse(
		req.command.Common.Unknown[0], activeAbilityID, req.command.Ability.Index,
		req.packet.SourceTime, projected.HitDelay, finishDelay,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargeRelease: %w", err)
	}
	cooldownPacket, err := abilityraknet.Cooldown(abilityraknet.CooldownRequest{
		ObjectID: req.command.Common.ObjectID, AbilityID: activeAbilityID,
		Duration: cooldown, StartTime: req.packet.SourceTime,
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargeCooldownMarshal: %w", err)
	}
	remainingManaPoint := peerSession.deployedManaPoint() - manaCost
	manaPacket, err := abilityraknet.Mana(req.command.Common.ObjectID, remainingManaPoint)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargeManaMarshal: %w", err)
	}
	animationPacket, err := abilityraknet.Animation(
		req.command.Common.ObjectID, projected.AnimationName, req.packet.SourceTime,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargeAnimation: %w", err)
	}
	movePackets, err := actionraknet.ChargeMove(
		req.command.Common.ObjectID, game.Vec3(startPosition),
		game.Vec3(destination), targetObjectID, target.Plan.Position,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargeMove: %w", err)
	}
	speedPacket, err := heroChargeSpeedPacket(
		req.command.Common.ObjectID,
		zonePlayerMoveSpeed*projected.MovementSpeedMultiplier,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargeSpeed: %w", err)
	}
	previousManaPoint := peerSession.deployedManaPoint()
	cooldownReservation, isCooldownReserved := peerSession.abilityCooldownSession().Reserve(
		zoneability.HeroAbilityCooldown(activeAbilityID), abilityStartTime, cooldown,
	)
	if !isCooldownReserved {
		r.registry.mutex.Unlock()
		return req.reject("charge cooldown unavailable")
	}
	releaseReservation, isReleaseReserved := peerSession.abilityReleaseSession().Reserve(
		abilityStartTime, finishDelay,
	)
	if !isReleaseReserved {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		r.registry.mutex.Unlock()
		return req.reject("charge release unavailable")
	}
	err = peerSession.stopPlayerMovement(abilityStartTime)
	if err == nil {
		err = peerSession.setDeployedManaPoints(remainingManaPoint)
	}
	if err != nil {
		peerSession.abilityCooldownSession().Rollback(cooldownReservation)
		peerSession.abilityReleaseSession().Rollback(releaseReservation)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroChargeCommit: %w", err)
	}
	petPursuit := zonecompanion.Pursuit{}
	petDamage := game.DamageRange{}
	petPackets := make([][]byte, 0)
	if projected.Name == "BeastCharge" {
		petPursuit, petDamage, petPackets, err = prepareBeastPetCharge(
			&peerSession, target,
		)
		if err != nil {
			_ = peerSession.setDeployedManaPoints(previousManaPoint)
			peerSession.abilityCooldownSession().Rollback(cooldownReservation)
			peerSession.abilityReleaseSession().Rollback(releaseReservation)
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("heroChargePet: %w", err)
		}
	}
	petImpactDelay := time.Duration(0)
	if petPursuit.ObjectID != 0 {
		petImpactDelay = 300*time.Millisecond + petPursuit.TravelDuration
		cleanupDelay = max(
			cleanupDelay, petImpactDelay+projected.StatusDuration,
		)
	}
	run := &heroChargeRun{
		npc: peerSession.zone.NPCs(), companion: peerSession.zone.Companion(),
		modifierPool: r.modifierPool, assetName: projected.Name,
		petPursuit: petPursuit,
	}
	peerSession.heroCharge = run
	generation := peerSession.generation
	creatureIndex := peerSession.deployedCreatureIndex
	binding := peerSession.binding
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	schedule := heroChargeSchedule{
		runtime: r, packet: req.packet, sessionKey: sessionKey,
		generation: generation, sourceObjectID: req.command.Common.ObjectID,
		abilityID:      activeAbilityID,
		targetObjectID: targetObjectID, creatureIndex: creatureIndex,
		previousManaPoint: previousManaPoint, destination: destination,
		startPosition: startPosition, impactDelay: impactDelay,
		targetPosition: targetPosition,
		petImpactDelay: petImpactDelay, finishDelay: finishDelay,
		creature: creature, definition: projected, damage: damage,
		petDamage: petDamage, petPursuit: petPursuit, binding: binding,
		run: run, cooldownReservation: cooldownReservation,
		releaseReservation: releaseReservation, releasePacket: releasePacket,
	}
	producer := []raknet.ScheduledPacketProducer{
		{Delay: impactDelay, Produce: schedule.impact},
		{Delay: finishDelay, Produce: schedule.release},
	}
	if petPursuit.ObjectID != 0 {
		producer = append(producer, raknet.ScheduledPacketProducer{
			Delay: petImpactDelay, Produce: schedule.petImpact,
		})
	}
	producer = append(producer, raknet.ScheduledPacketProducer{
		Delay: cleanupDelay, Produce: schedule.finish,
	})
	sortScheduledPacketProducersByDelay(producer)
	producers := r.registry.producerGuard.scheduledProducers(sessionKey, producer)
	var cancel raknet.CancelSchedule
	if req.packet.ScheduleGroupResult != nil {
		cancel, err = req.packet.ScheduleGroupResult(producers, schedule.fail)
	} else if req.packet.ScheduleGroup != nil {
		cancel, err = req.packet.ScheduleGroup(producers)
	} else {
		err = errors.New("schedule unavailable")
	}
	if err != nil {
		schedule.fail(err)
		return nil, fmt.Errorf("heroChargeSchedule: %w", err)
	}
	run.cancel = cancel
	packets := [][]byte{ackPacket, cooldownPacket, manaPacket, animationPacket, speedPacket}
	packets = append(packets, movePackets...)
	packets = append(packets, petPackets...)
	if projected.Name != "BioRandom2" && projected.Name != "EntanglingRush" &&
		(projected.ActivationEffectName != "" || projected.MuzzleEffectName != "") {
		assetName := projected.ActivationEffectName
		if assetName == "" {
			assetName = projected.MuzzleEffectName
		}
		effectPacket, marshalErr := raknet.MarshalApplication(raknet.ServerEventMessage{
			Asset: util.HashID(assetName), ObjectID: req.command.Common.ObjectID,
			Position: startPosition,
		})
		if marshalErr != nil {
			schedule.fail(marshalErr)
			return nil, fmt.Errorf("heroChargeEffect: %w", marshalErr)
		}
		packets = append(packets, effectPacket)
	}
	return packets, nil
}

func heroChargeSpeedPacket(objectID uint32, speed float32) ([]byte, error) {
	if objectID == 0 || speed <= 0 || math.IsNaN(float64(speed)) ||
		math.IsInf(float64(speed), 0) {
		return nil, errors.New("hero charge speed invalid")
	}
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
		ObjectID: objectID, Value: map[uint8]float32{11: speed, 12: speed},
	})
	if err != nil {
		return nil, fmt.Errorf("heroChargeSpeedMarshal: %w", err)
	}
	return packet, nil
}

func chargeDestination(
	start raknet.Vector3, target raknet.Vector3, stopDistance float32,
) raknet.Vector3 {
	distance := chargeDistance(start, target)
	if distance <= stopDistance || distance == 0 {
		return start
	}
	ratio := stopDistance / distance
	return raknet.Vector3{
		X: target.X + (start.X-target.X)*ratio,
		Y: target.Y + (start.Y-target.Y)*ratio,
		Z: target.Z + (start.Z-target.Z)*ratio,
	}
}

func chargeThroughDestination(
	start raknet.Vector3, target raknet.Vector3, distanceAfterTarget float32,
) raknet.Vector3 {
	distance := chargeDistance(start, target)
	if distance == 0 || distanceAfterTarget <= 0 {
		return target
	}
	ratio := distanceAfterTarget / distance
	return raknet.Vector3{
		X: target.X + (target.X-start.X)*ratio,
		Y: target.Y + (target.Y-start.Y)*ratio,
		Z: target.Z + (target.Z-start.Z)*ratio,
	}
}

func chargeDistance(left raknet.Vector3, right raknet.Vector3) float32 {
	deltaX := left.X - right.X
	deltaY := left.Y - right.Y
	deltaZ := left.Z - right.Z
	return float32(math.Sqrt(float64(deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ)))
}

func chargeSegmentDistance(start game.Vec3, end game.Vec3, point game.Vec3) float32 {
	segment := end.Sub(start)
	lengthSquared := segment.X*segment.X + segment.Y*segment.Y + segment.Z*segment.Z
	if lengthSquared == 0 {
		return point.Sub(start).Length()
	}
	toPoint := point.Sub(start)
	ratio := (toPoint.X*segment.X + toPoint.Y*segment.Y + toPoint.Z*segment.Z) /
		lengthSquared
	ratio = min(max(ratio, 0), 1)
	closest := game.Vec3{
		X: start.X + segment.X*ratio,
		Y: start.Y + segment.Y*ratio,
		Z: start.Z + segment.Z*ratio,
	}
	return point.Sub(closest).Length()
}
