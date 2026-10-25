//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const scenarioNPCMoveInterval = 100 * time.Millisecond

func scenarioNPCStop(actor zonenpc.ScenarioControlState) ([][]byte, error) {
	packets, err := npcraknet.MovementStop(actor.ObjectID, actor.Position)
	if err != nil {
		return nil, fmt.Errorf("npcStopMarshal: %w", err)
	}
	return packets, nil
}

func (e scenarioNPCMoveStep) schedule() error {
	publication := &scenarioNPCMovePublication{step: e}
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{Delay: scenarioNPCMoveInterval, Produce: publication.produce, AfterCommit: publication.complete}})
	if err != nil {
		return fmt.Errorf("npcMoveSchedule: %w", err)
	}
	if cancel == nil {
		return errors.New("controlled NPC move scheduler returned no cancellation")
	}
	return nil
}

// Each scheduled producer owns its own result. A delayed earlier commit must
// never complete a later step merely because the shared receipt says arrived.
type scenarioNPCMovePublication struct {
	step           scenarioNPCMoveStep
	isFinalArrival bool
}

func (publication *scenarioNPCMovePublication) produce() ([][]byte, error) {
	e := publication.step
	registry := e.runtime.registry
	registry.mutex.Lock()
	receipt, isFound := registry.npcControlReceipts[e.requestID]
	if !isFound || receipt.IsSettled {
		registry.mutex.Unlock()
		return nil, nil
	}
	current, isCurrent := registry.sessions[e.packet.Address.String()]
	if !isCurrent || current.zone == nil {
		err := errors.New("controlled NPC move owner ended")
		registry.failNPCControlLocked(e.requestID, err)
		registry.mutex.Unlock()
		return nil, fmt.Errorf("npcMoveOwner: %w", err)
	}
	ctx, cancel := context.WithTimeout(current.zone.Context(), 500*time.Millisecond)
	defer cancel()
	peer, actor, err := registry.controlledNPCPeerLocked(ctx, e.req.Identity)
	if err != nil {
		registry.failNPCControlLocked(e.requestID, err)
		registry.mutex.Unlock()
		return nil, fmt.Errorf("npcMoveActor: %w", err)
	}
	now := e.runtime.damage.npc.now()
	if now.Sub(e.startedAt) >= 10*time.Second || !now.Before(registry.npcControlDeadlines[e.requestID]) {
		err = errors.New("controlled NPC move exceeded its ten-second progression bound")
		registry.failNPCControlLocked(e.requestID, err)
		registry.mutex.Unlock()
		return scenarioNPCStop(actor)
	}
	elapsed := min(now.Sub(e.lastAt), 250*time.Millisecond)
	if elapsed <= 0 {
		err = errors.New("controlled NPC move clock did not advance")
		registry.failNPCControlLocked(e.requestID, err)
		registry.mutex.Unlock()
		return scenarioNPCStop(actor)
	}
	destination := e.req.Destination
	progress, err := peer.zone.NPCs().ScenarioMove(zonenpc.ScenarioMoveRequest{
		Ref:               zonenpc.ScenarioControlRef{ObjectID: e.req.Identity.ObjectID, ControlID: e.req.Identity.SpawnRequestID},
		ControlGeneration: e.controlGeneration, Destination: game.Vec3{X: destination.X, Y: destination.Y, Z: destination.Z}, Elapsed: elapsed, At: now,
	})
	if progress.IsProjectedDestinationAvailable {
		projected := progress.ProjectedDestination
		receipt.ProjectedDestination = &scenario.WorldPosition{X: projected.X, Y: projected.Y, Z: projected.Z}
		registry.npcControlReceipts[e.requestID] = receipt
	}
	if err == nil && progress.Step.IsBlocked {
		err = errors.New("controlled NPC move was blocked by the actor mesh")
	}
	if err != nil {
		registry.failNPCControlLocked(e.requestID, err)
		registry.mutex.Unlock()
		// The command receipt carries the semantic failure. A successful
		// stop remains a normal committed producer so peer fanout occurs.
		return scenarioNPCStop(actor)
	}
	if progress.IsComplete {
		receipt.State = "arrived"
		registry.npcControlReceipts[e.requestID] = receipt
	}
	registry.mutex.Unlock()
	var packets [][]byte
	if progress.IsComplete {
		packets, err = scenarioNPCStop(progress.State)
	} else {
		if !progress.Step.IsWaypointKnown {
			err = errors.New("controlled NPC move omitted its mesh waypoint")
		} else {
			packets, err = npcraknet.PursuitWaypoint(e.req.Identity.ObjectID, progress.Step.Waypoint)
		}
	}
	if err != nil {
		registry.failNPCControl(e.requestID, err)
		return packets, fmt.Errorf("npcMoveMarshal: %w", err)
	}
	positionPacket, err := npcraknet.Position(e.req.Identity.ObjectID, progress.State.Position)
	if err != nil {
		registry.failNPCControl(e.requestID, err)
		return packets, fmt.Errorf("npcMovePosition: %w", err)
	}
	packets = append(packets, positionPacket)
	if !progress.IsComplete {
		e.lastAt = now
		err = e.schedule()
		if err != nil {
			registry.failNPCControl(e.requestID, err)
			stopPackets, stopErr := scenarioNPCStop(progress.State)
			if stopErr != nil {
				return packets, fmt.Errorf("npcMoveStop: %w", errors.Join(err, stopErr))
			}
			return append(packets, stopPackets...), nil
		}
	}
	publication.isFinalArrival = progress.IsComplete
	return packets, nil
}

func (publication *scenarioNPCMovePublication) complete() {
	if !publication.isFinalArrival {
		return
	}
	e := publication.step
	registry := e.runtime.registry
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	receipt, isFound := registry.npcControlReceipts[e.requestID]
	if !isFound || receipt.RequestID != e.requestID || receipt.Identity != e.req.Identity || receipt.Operation != "move" || receipt.IsSettled || receipt.State != "arrived" {
		return
	}
	receipt.IsCompleted, receipt.IsSettled, receipt.IsResponseCommitted, receipt.State = true, true, true, "completed"
	registry.npcControlReceipts[e.requestID] = receipt
}
