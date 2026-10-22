package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

type fireRavagerCastKey struct {
	abilityID  uint32
	sourceTime uint64
}

type fireRavagerStunCast struct {
	startedAt time.Time
	isArmed   bool
	targets   map[uint32]bool
}

type fireRavagerPassiveState struct {
	useCount uint32
	isReady  bool
	casts    map[fireRavagerCastKey]*fireRavagerStunCast
}

type fireRavagerUseCommit struct {
	registry   *gameplaySessionRegistry
	sessionKey string
	generation uint64
	objectID   uint32
	key        fireRavagerCastKey
	startedAt  time.Time
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
	if state.casts == nil {
		state.casts = make(map[fireRavagerCastKey]*fireRavagerStunCast)
	}
	for key, cast := range state.casts {
		// Chunk555 AbilitiesCleanup drops activation identities after10seconds.
		if e.startedAt.Sub(cast.startedAt) > 10*time.Second {
			delete(state.casts, key)
		}
	}
	if state.casts[e.key] != nil {
		return
	}
	state.casts[e.key] = &fireRavagerStunCast{
		startedAt: e.startedAt, targets: make(map[uint32]bool),
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
	e.registry.sessions[e.sessionKey] = peerSession
}

func (e campaignAbilityCommandRuntime) recordFireRavagerUse(
	packet raknet.Packet, command raknet.ActionCommandData,
	peerSession gameplayPeerSession, packets [][]byte, startedAt time.Time,
) error {
	if command.Ability == nil || command.Ability.Index == 0 ||
		peerSession.deployedCreatureIndex >= uint32(len(peerSession.binding.Creatures)) ||
		peerSession.binding.Creatures[peerSession.deployedCreatureIndex].PassiveAbility != util.HashID("FireRavagerPassive") {
		return nil
	}
	abilityID, isAccepted := acceptedAbilityID(packets)
	if !isAccepted || abilityID == util.HashID("FireRavagerBasic") {
		return nil
	}
	commit := fireRavagerUseCommit{
		registry: e.registry, sessionKey: packet.Address.String(), generation: peerSession.generation,
		objectID: peerSession.deployedObjectID, key: fireRavagerCastKey{abilityID: abilityID, sourceTime: packet.SourceTime},
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
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation || peerSession.deployedObjectID != sourceObjectID ||
		peerSession.deployedHitPoint() <= 0 {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	state := &peerSession.fireRavagerPassive
	key := fireRavagerCastKey{abilityID: util.HashID(result.Definition.Name), sourceTime: packet.SourceTime}
	cast := state.casts[key]
	// Chunk555 HandleEvent PCs119–147 admits the damage activation itself,
	// independent of the definition that raised UsedAbility. Child modifiers
	// can deal the first qualifying damage with their own definition identity.
	if cast == nil {
		if !state.isReady {
			e.registry.mutex.Unlock()
			return nil, nil
		}
		if state.casts == nil {
			state.casts = make(map[fireRavagerCastKey]*fireRavagerStunCast)
		}
		cast = &fireRavagerStunCast{targets: make(map[uint32]bool)}
		state.casts[key] = cast
	}
	if cast.isArmed && e.npc.now().Sub(cast.startedAt) > 10*time.Second {
		e.registry.mutex.Unlock()
		return nil, nil
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
