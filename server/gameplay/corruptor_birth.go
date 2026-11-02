package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// Birth is its own write/peer-publication boundary. Children are not scheduled
// from the enclosing phase producer, whose captured transport is already ready.
type corruptorBirthPublication struct {
	runtime     campaignNPCActionRuntime
	packet      raknet.Packet
	identity    gameplayProducerIdentity
	world       *zone.Zone
	plans       []zonenpc.SpawnPlan
	actionPlans []zonenpc.SpawnPlan
	packets     [][]byte
	timestamp   uint64
	startedAt   time.Time
}

func (e campaignNPCActionRuntime) publishCorruptorBirth(
	packet raknet.Packet, sessionKey string, generation uint64,
	plans []campaignCorruptorSpawnPlan, actionPlans []zonenpc.SpawnPlan,
	packets [][]byte, timestamp uint64,
) error {
	if len(plans) == 0 {
		return nil
	}
	e.registry.mutex.RLock()
	member, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && member.generation == generation && member.zone != nil &&
		member.transportGeneration == packet.TransportGeneration && !member.isZoneTerminal()
	identity := gameplayProducerIdentityFromSession(sessionKey, member, isFound)
	e.registry.mutex.RUnlock()
	if !isCurrent {
		return errors.New("corruptor birth carrier replaced")
	}
	plainPlans := make([]zonenpc.SpawnPlan, 0, len(plans))
	for _, plan := range plans {
		plainPlans = append(plainPlans, plan.Plan)
	}
	publication := &corruptorBirthPublication{
		runtime: e, packet: packet.Autonomous(), identity: identity, world: member.zone,
		plans: plainPlans, actionPlans: actionPlans, packets: clonePendingPackets(packets),
		timestamp: timestamp, startedAt: e.now(),
	}
	err := publication.schedule()
	if err != nil {
		return fmt.Errorf("corruptorBirthSchedule: %w", err)
	}
	return nil
}

func (e *corruptorBirthPublication) schedule() error {
	if e.packet.ScheduleGroupResult == nil {
		return errors.New("corruptor birth result scheduler unavailable")
	}
	cancel, err := e.packet.ScheduleGroupResult([]raknet.ScheduledPacketProducer{{
		Produce: e.produce, AfterCommit: e.commit,
	}}, e.fail)
	if err != nil {
		return fmt.Errorf("birthAdmission: %w", err)
	}
	if cancel == nil {
		return errors.New("corruptor birth cancellation unavailable")
	}
	return nil
}

func (e *corruptorBirthPublication) produce() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.identity.sessionKey]
	isCurrent := isFound && member.zone == e.world && !member.isZoneTerminal() &&
		gameplayProducerIdentityFromSession(e.identity.sessionKey, member, isFound) == e.identity
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, errors.New("corruptor birth world replaced")
	}
	return e.packets, nil
}

func (e *corruptorBirthPublication) commit() {
	// The source write is complete. Commit the exact birth bytes to each peer's
	// FIFO before admitting any child producer that could publish an action.
	e.runtime.registry.queuePeerPresentation(e.identity, e.packets)
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.identity.sessionKey]
	isCurrent := isFound && member.zone == e.world && !member.isZoneTerminal() &&
		gameplayProducerIdentityFromSession(e.identity.sessionKey, member, isFound) == e.identity
	if isCurrent {
		e.world.NPCs().CommitBirth(e.plans, zonenpc.ActionOwner{
			UserID: member.binding.UserID, PeerGeneration: member.generation,
		})
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		e.fail(errors.New("corruptor birth carrier changed after write"))
		return
	}
	packets, err := e.runtime.scheduleOwnedFirstActions(e.packet, e.identity.sessionKey,
		e.identity.zoneGeneration, e.actionPlans, e.timestamp, e.startedAt)
	if err != nil {
		e.world.NPCs().ReleaseBirthOwner(e.plans, zonenpc.ActionOwner{
			UserID: member.binding.UserID, PeerGeneration: member.generation,
		})
		e.runtime.logger.Printf("Corruptor committed birth first action admission failed: %v", err)
		return
	}
	if len(packets) != 0 {
		// Owned initial packets must be in the scheduled first deadline, never
		// a pending queue that a direct child action could overtake.
		e.runtime.logger.Printf("Corruptor owned first action returned unexpected initial packets count=%d", len(packets))
	}
}

func (e *corruptorBirthPublication) fail(cause error) {
	e.runtime.registry.mutex.Lock()
	if !e.world.IsActive() || e.runtime.registry.isClosed {
		e.runtime.registry.mutex.Unlock()
		return
	}
	isFound := false
	for sessionKey, candidate := range e.runtime.registry.sessions {
		identity := gameplayProducerIdentityFromSession(sessionKey, candidate, true)
		if candidate.zone != e.world || !candidate.stage.IsDungeon() ||
			candidate.isZoneTerminal() || candidate.isRejoinPending ||
			!candidate.dungeonSetup.IsCommitted() || identity == e.identity ||
			candidate.schedulePacket.ScheduleGroupResult == nil {
			continue
		}
		e.identity = identity
		e.packet = candidate.schedulePacket.Autonomous()
		isFound = true
		break
	}
	e.runtime.registry.mutex.Unlock()
	if !isFound {
		e.runtime.logger.Printf("Corruptor birth publication has no surviving carrier: %v", cause)
		return
	}
	err := e.schedule()
	if err != nil {
		e.runtime.logger.Printf("Corruptor birth carrier retry failed: %v (original: %v)", err, cause)
	}
}
