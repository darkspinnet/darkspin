package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone/npc"
)

const trapperStealthCooldown = 12 * time.Second
const trapperStealthDuration = 3 * time.Second
const trapperTechnologyStealth = uint8(1)

type trapperStealthStep struct {
	runtime    campaignDamageRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	epoch      uint64
	isExit     bool
}

func startTrapperStealth(
	runtime campaignDamageRuntime,
	packet raknet.Packet, sessionKey string, generation uint64,
) error {
	registry := runtime.registry
	if registry == nil || packet.ScheduleFunc == nil || sessionKey == "" || generation == 0 {
		return errors.New("trapper stealth schedule unavailable")
	}
	registry.mutex.Lock()
	peerSession, isFound := registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
		peerSession.binding.Creatures[peerSession.deployedCreatureIndex].PassiveAbility ==
			util.HashID("TrapperStealthModifier")
	if !isCurrent {
		registry.mutex.Unlock()
		return nil
	}
	peerSession.trapperStealthEpoch++
	peerSession.isTrapperStealthed = false
	epoch := peerSession.trapperStealthEpoch
	registry.sessions[sessionKey] = peerSession
	registry.mutex.Unlock()
	step := trapperStealthStep{
		runtime: runtime, packet: packet,
		sessionKey: sessionKey, generation: generation, epoch: epoch,
	}
	err := packet.ScheduleFunc(trapperStealthCooldown, step.produce)
	if err != nil {
		return fmt.Errorf("trapperStealthEntrySchedule: %w", err)
	}
	return nil
}

func (e trapperStealthStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil &&
		peerSession.trapperStealthEpoch == e.epoch &&
		peerSession.deployedHitPoint() > 0 &&
		peerSession.deployedCreatureIndex < uint32(len(peerSession.binding.Creatures)) &&
		peerSession.binding.Creatures[peerSession.deployedCreatureIndex].PassiveAbility ==
			util.HashID("TrapperStealthModifier")
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.isTrapperStealthed = !e.isExit
	objectID := peerSession.deployedObjectID
	userID := peerSession.binding.UserID
	campaignZone := peerSession.zone
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	acquired, err := campaignZone.SetHeroStealthed(
		userID, e.generation, objectID, !e.isExit,
	)
	if err != nil {
		return nil, fmt.Errorf("trapperStealthTargeting: %w", err)
	}
	packets, err := trapperStealthPackets(objectID, !e.isExit)
	if err != nil {
		return nil, fmt.Errorf("trapperStealthMarshal: %w", err)
	}
	actionPackets, err := e.scheduleAcquired(acquired)
	if err != nil {
		return nil, fmt.Errorf("trapperStealthTargetSchedule: %w", err)
	}
	packets = append(packets, actionPackets...)
	next := e
	next.isExit = !e.isExit
	delay := trapperStealthDuration
	if e.isExit {
		delay = trapperStealthCooldown
	}
	err = e.packet.ScheduleFunc(delay, next.produce)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("RakNet Trapper Stealth cycle stopped for %s: %v", e.sessionKey, err)
	}
	return packets, nil
}

func (e trapperStealthStep) scheduleAcquired(
	acquired []npc.Snapshot,
) ([][]byte, error) {
	plan := make([]npc.SpawnPlan, 0, len(acquired))
	for _, current := range acquired {
		plan = append(plan, current.Plan)
	}
	packets, err := e.runtime.npc.scheduleFirstActions(
		e.packet, e.sessionKey, e.generation, plan, e.packet.SourceTime,
	)
	if err != nil {
		return nil, fmt.Errorf("trapperStealthFirstAction: %w", err)
	}
	return packets, nil
}

func (e *gameplayPeerSession) breakTrapperStealth() ([][]byte, error) {
	if e == nil || !e.isTrapperStealthed {
		return nil, nil
	}
	e.isTrapperStealthed = false
	if e.zone != nil {
		_, err := e.zone.SetHeroStealthed(
			e.binding.UserID, e.generation, e.deployedObjectID, false,
		)
		if err != nil {
			return nil, fmt.Errorf("trapperStealthTargetBreak: %w", err)
		}
	}
	return trapperStealthPackets(e.deployedObjectID, false)
}

func breakTrapperStealthForDamage(
	registry *gameplaySessionRegistry, sessionKey string,
	generation uint64, sourceObjectID uint32,
) ([][]byte, error) {
	if registry == nil || sessionKey == "" || generation == 0 || sourceObjectID == 0 {
		return nil, nil
	}
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	peerSession, isFound := registry.sessions[sessionKey]
	if !isFound || peerSession.generation != generation ||
		peerSession.deployedObjectID != sourceObjectID {
		return nil, nil
	}
	packets, err := peerSession.breakTrapperStealth()
	if err != nil {
		return nil, fmt.Errorf("trapperStealthDamageBreak: %w", err)
	}
	registry.sessions[sessionKey] = peerSession
	return packets, nil
}

func (e *gameplayPeerSession) stopTrapperStealth() ([][]byte, error) {
	if e == nil {
		return nil, nil
	}
	e.trapperStealthEpoch++
	return e.breakTrapperStealth()
}

func trapperStealthPackets(objectID uint32, isStealthed bool) ([][]byte, error) {
	if objectID == 0 {
		return nil, errors.New("trapper stealth object unavailable")
	}
	stealth := uint8(0)
	effectName := "cyber_trapper_exit_stealth.ServerEventDef"
	if isStealthed {
		stealth = trapperTechnologyStealth
		effectName = "cyber_trapper_enter_stealth.ServerEventDef"
	}
	message := []raknet.ApplicationMessage{
		raknet.AgentBlackboardUpdateMessage{
			ObjectID: objectID, Stealth: stealth, IsTargetable: true,
		},
		raknet.ServerEventMessage{Asset: util.HashID(effectName), ObjectID: objectID},
	}
	packets := make([][]byte, 0, len(message))
	for index, current := range message {
		packet, err := raknet.MarshalApplication(current)
		if err != nil {
			return nil, fmt.Errorf("trapperStealthPacket[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
