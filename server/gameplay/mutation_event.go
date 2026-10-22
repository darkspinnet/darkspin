package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const (
	mutationSourceFrame uint8 = iota + 1
	mutationBeamFrame
	mutationTransformFrame
	mutationPulseFrame
	mutationMovementFrame
)

type mutationVisualSnapshot struct {
	startedAt         time.Time
	targetID          uint32
	isActive          bool
	position          game.Vec3
	facing            game.Vec3
	waypoint          game.Vec3
	movementSpeed     float32
	movementSpeedBuff float32
}

type mutationPendingEvent struct {
	world               *zone.Zone
	event               zonenpc.MutationEvent
	snapshot            mutationVisualSnapshot
	group               uint8
	isPrepared          bool
	transportGeneration uint64
}

func (e *gameplayPeerSession) queueMutationEvent(frame mutationPendingEvent) {
	if frame.group == mutationPulseFrame {
		frame.transportGeneration = e.transportGeneration
	}
	batches := make([]pendingPeerPacketBatch, 0, len(e.pendingPacketBatches)+1)
	for _, batch := range e.pendingPacketBatches {
		pending := batch.mutationEvent
		if pending != nil && pending.world == frame.world && pending.group == frame.group && pending.event.ID.ObjectID == frame.event.ID.ObjectID {
			continue
		}
		batches = append(batches, batch)
	}
	e.nextPendingPacketID++
	e.pendingPacketBatches = append(batches, pendingPeerPacketBatch{
		id: e.nextPendingPacketID, mutationEvent: &frame, isCampaignPresentation: true,
	})
}

func (e *gameplaySessionRegistry) queueMutationEventLocked(world *zone.Zone, event zonenpc.MutationEvent, group uint8) {
	frame := mutationPendingEvent{world: world, event: event, group: group}
	for key, member := range e.sessions {
		if member.zone != world {
			continue
		}
		if group == mutationPulseFrame && (member.isRejoinPending || !member.dungeonSetup.IsCommitted() || !member.isCampaignPresentationAvailable()) {
			continue
		}
		member.queueMutationEvent(frame)
		e.sessions[key] = member
	}
	if group == mutationPulseFrame {
		return
	}
	for _, retained := range e.retainedSessions {
		if retained.peerSession.zone == world {
			retained.peerSession.queueMutationEvent(frame)
		}
	}
}

func mutationVisual(frame mutationPendingEvent, npc zonenpc.Snapshot, at time.Time) mutationVisualSnapshot {
	snapshot := mutationVisualSnapshot{}
	if frame.event.ID.Generation != npc.Mutation.Generation {
		return snapshot
	}
	switch frame.group {
	case mutationSourceFrame:
		snapshot.isActive = npc.IsPublished && !npc.IsDefeated && npc.HitPoint > 0 && npc.Mutation.IsPassiveActive
	case mutationBeamFrame:
		beam := npc.Mutation.Beam
		snapshot.isActive = npc.IsPublished && !npc.IsDefeated && npc.HitPoint > 0 && beam.IsActive && beam.IsChannelActive && at.Before(beam.ReleaseAt)
		snapshot.targetID = beam.TargetObjectID
		snapshot.startedAt = beam.StartedAt
	case mutationTransformFrame:
		transform := npc.Mutation.Transform
		snapshot.isActive = npc.IsPublished && !npc.IsDefeated && npc.HitPoint > 0 && transform.IsActive && at.Before(transform.CompleteAt)
		snapshot.startedAt = transform.CompleteAt.Add(-time.Second)
	case mutationMovementFrame:
		movement := npc.Mutation.Movement
		snapshot.position = npc.Plan.Position
		snapshot.facing = npc.Facing
		snapshot.waypoint = movement.Waypoint
		snapshot.movementSpeed = movement.MovementSpeed
		snapshot.movementSpeedBuff = movement.MovementSpeedBuff
		snapshot.isActive = npc.IsPublished && !npc.IsDefeated && npc.HitPoint > 0 && !npc.Mutation.Beam.IsActive && !movement.IsPaused && movement.IsInitialized && movement.MovementSpeed > 0 && npc.Plan.Position != movement.Waypoint
	}
	return snapshot
}

func marshalMutationVisual(frame mutationPendingEvent, snapshot mutationVisualSnapshot, at time.Time, sourceTime uint64) ([][]byte, error) {
	objectID := frame.event.ID.ObjectID
	messages := make([]raknet.ApplicationMessage, 0, 2)
	switch frame.group {
	case mutationSourceFrame:
		for index, asset := range []string{"effect_environment_mutant_agent_smoke.ServerEventDef", "effect_environment_mutant_agent_smoke_model.ServerEventDef"} {
			messages = append(messages, raknet.AttachedEffectMessage{
				Slot: uint8(16 + index), ObjectID: objectID, Asset: util.HashID(asset),
				IsForceAttached: snapshot.isActive, IsRemovalRequested: !snapshot.isActive, IsHardStop: !snapshot.isActive,
			})
		}
	case mutationBeamFrame:
		for index, asset := range []string{"mutation_beam.ServerEventDef", "mutation_particles.ServerEventDef"} {
			messages = append(messages, raknet.AttachedEffectMessage{
				Slot: uint8(20 + index), ObjectID: objectID, SecondaryObjectID: snapshot.targetID,
				Asset: util.HashID(asset), IsForceAttached: snapshot.isActive,
				IsRemovalRequested: !snapshot.isActive, IsHardStop: !snapshot.isActive,
			})
		}
	case mutationTransformFrame:
		if !snapshot.isActive {
			return nil, nil
		}
		elapsed := max(time.Duration(0), at.Sub(snapshot.startedAt))
		messages = append(messages, raknet.SetAnimationStateMessage{
			ObjectID: objectID, State: util.HashID("npc_mutationagent_infected_grow"),
			Timestamp: sourceTime - uint64(elapsed/time.Millisecond), Scale: 1,
		})
	case mutationPulseFrame:
		messages = append(messages, raknet.DropPresentationMessage{
			Asset: util.HashID("mutant_agent_aoe_smoke.ServerEventDef"), Position: raknet.Vector3(frame.event.Position),
		})
	case mutationMovementFrame:
		position := raknet.Vector3(snapshot.position)
		goal := position
		flags := uint32(0x20)
		if snapshot.isActive {
			goal = raknet.Vector3(snapshot.waypoint)
			flags = 0x01
		}
		baseSpeed := snapshot.movementSpeed / max(float32(0.01), 1+snapshot.movementSpeedBuff)
		messages = append(messages,
			raknet.AttributeDataUpdateMessage{ObjectID: objectID, Value: map[uint8]float32{11: baseSpeed, 12: baseSpeed, 48: snapshot.movementSpeedBuff}},
			raknet.ObjectPositionUpdateMessage{ObjectID: objectID, PositionX: position.X, PositionY: position.Y, PositionZ: position.Z},
		)
		if snapshot.isActive {
			messages = append(messages, raknet.LocomotionUpdateContractMessage{
				ObjectID: objectID, Locomotion: raknet.LocomotionReflection{PartialGoalPosition: &goal},
			})
		}
		messages = append(messages, raknet.ObjectPlayerMoveMessage{ObjectID: objectID, GoalFlags: flags, GoalPosition: goal, Facing: raknet.Vector3(snapshot.facing)})
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("mutationVisual[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *gameplaySessionRegistry) refreshMutationEventsLocked(member *gameplayPeerSession, at time.Time, sourceTime uint64) error {
	batches := make([]pendingPeerPacketBatch, 0, len(member.pendingPacketBatches))
	replacements := make([]pendingPeerPacketBatch, 0)
	for _, batch := range member.pendingPacketBatches {
		pending := batch.mutationEvent
		if pending == nil {
			batches = append(batches, batch)
			continue
		}
		if pending.world != member.zone {
			continue
		}
		if pending.group == mutationPulseFrame && (pending.transportGeneration != member.transportGeneration || member.isRejoinPending) {
			continue
		}
		npc, isFound := pending.world.NPCs().NPC(pending.event.ID.ObjectID)
		if !isFound {
			continue
		}
		if pending.group == mutationPulseFrame && (!npc.IsPublished || !npc.Mutation.IsAuraActive) {
			continue
		}
		next := *pending
		if npc.Mutation.Generation != next.event.ID.Generation {
			if next.event.Kind != zonenpc.MutationEventBeamStop && next.event.Kind != zonenpc.MutationEventDeactivate {
				continue
			}
			// Retired-life cleanup describes the current slot representation;
			// never remove a newly active life's replacement effect.
			next.event.ID.Generation = npc.Mutation.Generation
		}
		next.snapshot = mutationVisual(next, npc, at)
		next.isPrepared = true
		packets, err := marshalMutationVisual(next, next.snapshot, at, sourceTime)
		if err != nil {
			return fmt.Errorf("mutationStage: %w", err)
		}
		if len(packets) == 0 {
			continue
		}
		batch.packets = packets
		batch.mutationEvent = &next
		if pending.isPrepared && (pending.snapshot != next.snapshot || pending.event.ID.Generation != next.event.ID.Generation) {
			replacements = append(replacements, batch)
			continue
		}
		batches = append(batches, batch)
	}
	for _, batch := range replacements {
		member.nextPendingPacketID++
		batch.id = member.nextPendingPacketID
		batches = append(batches, batch)
	}
	member.pendingPacketBatches = batches
	return nil
}

func (e gameplayPeerSession) pendingMutationEvents(batchID uint64) []mutationPendingEvent {
	frames := make([]mutationPendingEvent, 0)
	for _, batch := range e.pendingPacketBatches {
		if batch.id > batchID {
			break
		}
		if batch.mutationEvent != nil {
			frames = append(frames, *batch.mutationEvent)
		}
	}
	return frames
}

func (e *gameplaySessionRegistry) repairMutationEventsLocked(member *gameplayPeerSession, frames []mutationPendingEvent, at time.Time) {
	for _, frame := range frames {
		if member.zone == frame.world && frame.group == mutationTransformFrame && frame.snapshot.isActive {
			npc, isFound := frame.world.NPCs().NPC(frame.event.ID.ObjectID)
			if !isFound || npc.Mutation.Generation != frame.event.ID.Generation || !mutationVisual(frame, npc, at).isActive {
				e.requireMutationBaselineLocked(member, frame.event.ID.ObjectID, "stale growth pose")
			}
			continue
		}
		if member.zone != frame.world || (frame.group != mutationSourceFrame && frame.group != mutationBeamFrame && frame.group != mutationMovementFrame) {
			continue
		}
		npc, isFound := frame.world.NPCs().NPC(frame.event.ID.ObjectID)
		if !isFound {
			continue
		}
		isLifeChanged := npc.Mutation.Generation != frame.event.ID.Generation
		frame.event.ID.Generation = npc.Mutation.Generation
		if isLifeChanged || frame.snapshot != mutationVisual(frame, npc, at) {
			frame.isPrepared = false
			member.queueMutationEvent(frame)
		}
	}
}

func (e *gameplaySessionRegistry) mutationRejoinPackets(member gameplayPeerSession, at time.Time, sourceTime uint64) ([][]byte, []mutationPendingEvent, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	packets := make([][]byte, 0)
	frames := make([]mutationPendingEvent, 0)
	for _, npc := range member.zone.NPCs().LiveSnapshots() {
		if !npc.IsPublished {
			continue
		}
		// Active grow phases use the object-creation reflection in SpawnSnapshot;
		// live SetAnimationState does not establish bootstrap seek semantics.
		growthFrame := mutationPendingEvent{world: member.zone, group: mutationTransformFrame, event: zonenpc.MutationEvent{
			ID: zonenpc.MutationEventID{ObjectID: npc.Plan.ObjectID, Generation: npc.Mutation.Generation},
		}}
		growthFrame.snapshot = mutationVisual(growthFrame, npc, at)
		if growthFrame.snapshot.isActive {
			growthFrame.isPrepared = true
			frames = append(frames, growthFrame)
		}
		for _, group := range []uint8{mutationSourceFrame, mutationBeamFrame, mutationMovementFrame} {
			frame := mutationPendingEvent{world: member.zone, group: group, event: zonenpc.MutationEvent{
				ID: zonenpc.MutationEventID{ObjectID: npc.Plan.ObjectID, Generation: npc.Mutation.Generation},
			}}
			snapshot := mutationVisual(frame, npc, at)
			if !snapshot.isActive && (group != mutationMovementFrame || !npc.Mutation.IsPassiveActive) {
				continue
			}
			current, err := marshalMutationVisual(frame, snapshot, at, sourceTime)
			if err != nil {
				return nil, nil, fmt.Errorf("mutationReplay: %w", err)
			}
			packets = append(packets, current...)
			frame.snapshot = snapshot
			frame.isPrepared = true
			frames = append(frames, frame)
		}
	}
	return packets, frames, nil
}
