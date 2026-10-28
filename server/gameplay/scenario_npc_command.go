//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e gameplayPendingRuntime) consumeScenarioNPCControl(ctx context.Context, packet raknet.Packet, queuedPeer gameplayPeerSession, isEligible bool) ([][]byte, bool, error) {
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return nil, false, nil
	}
	err := e.registry.lockScenarioSpawn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("npcConsumeLock: %w", err)
	}
	requestID := ""
	for index, pendingID := range e.registry.pendingNPCControls {
		req := e.registry.npcControlRequests[pendingID]
		if req.Identity.UserID != int64(queuedPeer.binding.UserID) {
			continue
		}
		requestID = pendingID
		e.registry.pendingNPCControls = append(e.registry.pendingNPCControls[:index], e.registry.pendingNPCControls[index+1:]...)
		break
	}
	if requestID == "" {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	req := e.registry.npcControlRequests[requestID]
	peer, actor, err := e.registry.controlledNPCPeerLocked(ctx, req.Identity)
	if err == nil && !e.damage.npc.now().Before(e.registry.npcControlDeadlines[requestID]) {
		err = errors.New("NPC command expired before packet-owner admission; no action was applied")
	}
	if err == nil && (!isEligible || peer.generation != queuedPeer.generation || peer.transportGeneration != packet.TransportGeneration) {
		err = errors.New("NPC command packet owner changed before admission")
	}
	isCancellation := req.Operation == "retire" || req.Operation == "set_ai" && req.IsAIEnabled != nil && !*req.IsAIEnabled
	isCleanup := isCancellation
	if err == nil && (actor.IsDefeated || actor.HitPoint <= 0 || peer.deployedHitPoint() <= 0) && !isCleanup {
		err = errors.New("NPC command source or owned hero is dead")
	}
	if err == nil {
		err = validateScenarioNPCRequest(req, peer, actor)
	}
	if err != nil {
		e.registry.failNPCControlLocked(requestID, err)
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("npcConsumeValidate: %w", err)
	}
	ref := zonenpc.ScenarioControlRef{ObjectID: req.Identity.ObjectID, ControlID: req.Identity.SpawnRequestID}
	wasPublished := actor.IsPublished
	wasDefeated := actor.IsDefeated
	var action zonenpc.Snapshot
	var isStarted bool
	var isFirstAction bool
	switch req.Operation {
	case "set_ai":
		actor, err = peer.zone.NPCs().ScenarioSetAI(zonenpc.ScenarioAIRequest{Ref: ref, IsAIEnabled: *req.IsAIEnabled})
		if err == nil && *req.IsAIEnabled {
			var isAcquired bool
			action, isAcquired, err = peer.zone.NPCs().AcquireTarget(req.Identity.ObjectID, peer.deployedObjectID)
			if err == nil && !isAcquired && action.TargetObjectID != peer.deployedObjectID {
				err = errors.New("enabled NPC could not acquire the owned deployed hero")
			}
		}
	case "ability":
		action, isStarted, isFirstAction, err = peer.zone.NPCs().ScenarioStartAction(zonenpc.ScenarioActionRequest{
			Ref: ref, Owner: zonenpc.ActionOwner{UserID: peer.binding.UserID, PeerGeneration: peer.generation}, TargetObjectID: req.TargetObjectID,
		})
		if err == nil && !isStarted {
			err = errors.New("manual NPC ability admission was refused")
		}
		if err == nil {
			actor.ActionGeneration = action.ActionGeneration
		}
	case "move":
		// The first movement step runs through the owning scheduler, not here.
	case "restore":
		actor, err = peer.zone.NPCs().ScenarioRestoreHitPoint(ref)
	case "retire":
		actor, err = peer.zone.NPCs().ScenarioRetire(ref)
	default:
		err = errors.New("NPC command operation is unsupported")
	}
	if err != nil {
		e.registry.failNPCControlLocked(requestID, err)
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("npcConsumeApply: %w", err)
	}
	receipt := e.registry.npcControlReceipts[requestID]
	receipt.IsAccepted, receipt.State, receipt.ActionGeneration = true, "admitted", actor.ActionGeneration
	if req.Operation == "set_ai" || req.Operation == "restore" || req.Operation == "retire" {
		receipt.IsCompleted, receipt.IsSettled, receipt.State = true, true, "completed"
	}
	e.registry.npcControlReceipts[requestID] = receipt
	npcSession := peer.zone.NPCs()
	if req.Operation == "set_ai" && *req.IsAIEnabled {
		action, isStarted = npcSession.NPC(req.Identity.ObjectID)
		if !isStarted {
			err = errors.New("controlled NPC disappeared after enabling AI")
		}
	}
	e.registry.mutex.Unlock()
	if err != nil {
		e.registry.failNPCControl(requestID, err)
		return nil, true, fmt.Errorf("npcConsumeSnapshot: %w", err)
	}
	var packets [][]byte
	switch req.Operation {
	case "set_ai":
		if *req.IsAIEnabled {
			packets, err = e.damage.npc.scheduleFirstActions(packet, packet.Address.String(), peer.generation, []zonenpc.SpawnPlan{action.Plan}, packet.SourceTime)
		} else {
			packets, err = scenarioNPCStop(actor)
		}
	case "ability":
		// Run the admitted action through the ordinary first-action producer.
		// It selects the NPC family, pursues when needed, and schedules its
		// authored release and impact. AI-off fences autonomous continuation.
		step := campaignNPCFirstActionStep{runtime: e.damage.npc, packet: packet.Autonomous(),
			sessionKey: packet.Address.String(), generation: peer.generation,
			actionGeneration: action.ActionGeneration, objectID: req.Identity.ObjectID,
			timestamp: packet.SourceTime}
		packets, err = step.produce()
	case "move":
		step := scenarioNPCMoveStep{runtime: e, packet: packet.Autonomous(), requestID: requestID, req: req,
			controlGeneration: actor.ControlGeneration, startedAt: e.damage.npc.now(), lastAt: e.damage.npc.now()}
		err = step.schedule()
	case "restore":
		var healthPacket []byte
		healthPacket, err = raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
			ObjectID: actor.ObjectID, HitPoints: actor.HitPoint, IsHitPointChanged: true,
		})
		if err == nil {
			packets = [][]byte{healthPacket}
		}
	case "retire":
		if wasPublished && !wasDefeated {
			var deletePacket []byte
			deletePacket, err = raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{actor.ObjectID}})
			if err == nil {
				packets = [][]byte{deletePacket}
			}
		}
	}
	if err != nil {
		if req.Operation == "ability" {
			npcSession.ReleaseActionGeneration(req.Identity.ObjectID, action.ActionOwner, action.ActionGeneration)
		}
		e.registry.failNPCControl(requestID, err)
		return packets, true, fmt.Errorf("npcNormalAction: %w", err)
	}
	err = publishCampaignPeersAfterCommit(e.registry, packet, packets)
	if err != nil {
		e.registry.failNPCControl(requestID, err)
		return packets, true, fmt.Errorf("npcPeerPublication: %w", err)
	}
	commit := scenarioNPCResponseCommit{registry: e.registry, requestID: requestID}
	err = packet.AfterResponseCommit(commit.complete)
	if err != nil {
		e.registry.failNPCControl(requestID, err)
		return packets, true, fmt.Errorf("npcResponseCommit: %w", err)
	}
	if isFirstAction {
		// Manual admission may establish ordinary first-action state; it does
		// not grant an autonomous continuation or fabricate impact evidence.
		e.logger.Printf("Scenario manual NPC first action object=%d generation=%d", req.Identity.ObjectID, action.ActionGeneration)
	}
	return packets, true, nil
}

func (e *gameplaySessionRegistry) failNPCControlLocked(requestID string, err error) {
	receipt, isFound := e.npcControlReceipts[requestID]
	if !isFound || err == nil {
		return
	}
	receipt.State, receipt.Failure, receipt.IsSettled = "failed", err.Error(), true
	receipt.IsCompleted = false
	e.npcControlReceipts[requestID] = receipt
}

func (e *gameplaySessionRegistry) failNPCControl(requestID string, err error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.failNPCControlLocked(requestID, err)
}

type scenarioNPCResponseCommit struct {
	registry  *gameplaySessionRegistry
	requestID string
}

func (e scenarioNPCResponseCommit) complete() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	receipt, isFound := e.registry.npcControlReceipts[e.requestID]
	if !isFound {
		return
	}
	receipt.IsResponseCommitted = true
	e.registry.npcControlReceipts[e.requestID] = receipt
	if receipt.Operation == "retire" {
		spawn := e.registry.spawnReceipts[receipt.Identity.SpawnRequestID]
		spawn.State = "retired"
		e.registry.spawnReceipts[receipt.Identity.SpawnRequestID] = spawn
	}
}

// Move permits at most ten seconds of actor-footprint mesh progression. A
// blocked, crowd-controlled, superseded or stale actor produces a partial error.
type scenarioNPCMoveStep struct {
	runtime           gameplayPendingRuntime
	packet            raknet.Packet
	requestID         string
	req               scenario.NPCControlRequest
	controlGeneration uint64
	startedAt         time.Time
	lastAt            time.Time
}
