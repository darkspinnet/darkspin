package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

type fireRavagerStunCast struct {
	startedAt time.Time
	isArmed   bool
	targets   map[uint32]bool
}

type fireRavagerUse struct {
	startedAt time.Time
	abilityID uint32
}

type fireRavagerPassiveState struct {
	useCount uint32
	isReady  bool
	uses     map[uint64]fireRavagerUse
	casts    map[uint64]*fireRavagerStunCast
}

type fireRavagerUseCommit struct {
	registry     *gameplaySessionRegistry
	sessionKey   string
	generation   uint64
	objectID     uint32
	activationID uint64
	abilityID    uint32
	isBasic      bool
	startedAt    time.Time
}

type fireRavagerHitStep struct {
	runtime        campaignDamageRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	sourceObjectID uint32
	timestamp      uint64
	result         zoneability.AreaResult
}

func (e fireRavagerHitStep) produce() ([][]byte, error) {
	packets, err := e.runtime.commitFireRavagerStun(e.packet, e.sessionKey, e.generation, e.sourceObjectID, e.timestamp, e.result)
	if err != nil {
		return nil, fmt.Errorf("fireRavagerHit: %w", err)
	}
	return packets, nil
}

func (e fireRavagerUseCommit) commit() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	peerSession, isFound := e.registry.sessions[e.sessionKey]
	if !isFound || peerSession.generation != e.generation ||
		peerSession.deployedObjectID != e.objectID || peerSession.deployedHitPoint() <= 0 ||
		peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) ||
		peerSession.binding.Creatures[peerSession.deployedCreatureIndex].PassiveAbility != util.HashID("FireRavagerPassive") {
		return
	}
	state := &peerSession.fireRavagerPassive
	if state.uses == nil {
		state.uses = make(map[uint64]fireRavagerUse)
	}
	previousUse := state.uses[e.activationID]
	if !previousUse.startedAt.IsZero() {
		return
	}
	state.uses[e.activationID] = fireRavagerUse{startedAt: e.startedAt, abilityID: e.abilityID}
	if e.isBasic {
		e.registry.sessions[e.sessionKey] = peerSession
		return
	}
	if peerSession.isOverdriveActiveAt(e.startedAt) {
		state.isReady = true
	} else if !state.isReady {
		// Four nonbasic uses charge the passive. The following use sets the
		// ready flag (UsedAbility PCs35–69); readiness is consumed by damage.
		if state.useCount < 4 {
			state.useCount++
		} else {
			state.isReady = true
		}
	}
	// AbilitiesCleanup calls table.remove on the selected record, whose only
	// fields are timeActivated and targets. It does not remove the outer key.
	// Keep both snapshot metadata and target dedupe for this passive lifetime.
	e.registry.sessions[e.sessionKey] = peerSession
}

func (e campaignAbilityCommandRuntime) recordFireRavagerUse(
	packet raknet.Packet, command raknet.ActionCommandData,
	peerSession gameplayPeerSession, packets [][]byte, startedAt time.Time,
) error {
	if command.Ability == nil ||
		peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) ||
		peerSession.binding.Creatures[peerSession.deployedCreatureIndex].PassiveAbility != util.HashID("FireRavagerPassive") {
		return nil
	}
	abilityID, isAccepted := acceptedAbilityID(packets)
	if !isAccepted {
		return nil
	}
	commit := fireRavagerUseCommit{
		registry: e.registry, sessionKey: packet.Address.String(), generation: peerSession.generation,
		objectID: peerSession.deployedObjectID, activationID: e.damage.activationID,
		abilityID: abilityID, isBasic: command.Ability.Index == 0 || abilityID == util.HashID("FireRavagerBasic"),
		startedAt: startedAt,
	}
	err := packet.AfterResponseCommit(commit.commit)
	if err != nil {
		return fmt.Errorf("fireRavagerUseCommit: %w", err)
	}
	return nil
}

func (e campaignDamageRuntime) applyFireRavagerStun(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, result zoneability.AreaResult,
) ([][]byte, error) {
	if result.Definition.Name == "" || result.Definition.Name == "FireRavagerBasic" ||
		result.ActivationDefinitionID == util.HashID("FireRavagerBasic") ||
		result.Definition.DescriptorMask&((1<<1)|(1<<18)) != 0 ||
		result.Damage.ResolvedDamage <= 0 || result.Damage.IsDefeated {
		return nil, nil
	}
	e.registry.mutex.RLock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isKrel := isFound && peerSession.generation == generation && peerSession.deployedObjectID == sourceObjectID &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
		peerSession.binding.Creatures[peerSession.deployedCreatureIndex].PassiveAbility == util.HashID("FireRavagerPassive")
	e.registry.mutex.RUnlock()
	if !isKrel {
		return nil, nil
	}
	// Immediate area handlers can commit damage before returning their accepted
	// response. A production producer waits for response publication, after the
	// UsedAbility commit has admitted the activation identity.
	step := fireRavagerHitStep{
		runtime: e, packet: packet.Autonomous(), sessionKey: sessionKey, generation: generation,
		sourceObjectID: sourceObjectID, timestamp: timestamp, result: result,
	}
	if step.packet.ScheduleFunc == nil {
		return nil, errors.New("fire ravager hit scheduler unavailable")
	}
	err := step.packet.ScheduleFunc(0, step.produce)
	if err != nil {
		return nil, fmt.Errorf("fireRavagerHitSchedule: %w", err)
	}
	return nil, nil
}

func (e campaignDamageRuntime) commitFireRavagerStun(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, result zoneability.AreaResult,
) ([][]byte, error) {
	if result.ActivationDefinitionID == util.HashID("FireRavagerBasic") {
		return nil, nil
	}
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation || peerSession.deployedObjectID != sourceObjectID ||
		peerSession.deployedHitPoint() <= 0 {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	state := &peerSession.fireRavagerPassive
	activationID := result.ActivationID
	if activationID == 0 {
		activationID = e.activationID
	}
	if activationID == 0 {
		// An unowned damage callback cannot fabricate an activation identity.
		e.registry.mutex.Unlock()
		return nil, nil
	}
	if use, isUsed := state.uses[activationID]; isUsed && use.abilityID == util.HashID("FireRavagerBasic") {
		// A cloned basic snapshot retains its original definition identity even
		// when another callback (including Expunge) selects the damage amount.
		e.registry.mutex.Unlock()
		return nil, nil
	}
	cast := state.casts[activationID]
	// Chunk555 HandleEvent PCs119–147 admits the damage activation itself,
	// independent of the definition that raised UsedAbility. Child modifiers
	// can deal the first qualifying damage with their own definition identity.
	if cast == nil {
		if !state.isReady {
			e.registry.mutex.Unlock()
			return nil, nil
		}
		if state.casts == nil {
			state.casts = make(map[uint64]*fireRavagerStunCast)
		}
		cast = &fireRavagerStunCast{targets: make(map[uint32]bool)}
		state.casts[activationID] = cast
	}
	if state.isReady && !cast.isArmed {
		// Lua timeActivated starts when damage arms the activation, so a
		// delayed first hit retains the same window as an immediate hit.
		cast.startedAt = e.npc.now()
		cast.isArmed = true
		state.isReady = false
		state.useCount = 0
	}
	if !cast.isArmed || cast.targets[result.Damage.ObjectID] {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	cast.targets[result.Damage.ObjectID] = true
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()
	packets, err := e.publishNPCStuns(packet, sessionKey, generation, sourceObjectID, timestamp,
		[]zoneability.AreaResult{result}, util.HashID("FireRavagerStunModifier"), time.Second)
	if err != nil {
		return nil, fmt.Errorf("fireRavagerStun: %w", err)
	}
	return packets, nil
}
