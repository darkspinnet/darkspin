//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/scenario"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type scenarioNPCControlQueue struct {
	nextNPCControlID         uint64
	npcControlRequests       map[string]scenario.NPCControlRequest
	npcControlReceipts       map[string]scenario.NPCControlReceipt
	pendingNPCControls       []string
	lastNPCControlIDsBySpawn map[string]string
	npcControlDeadlines      map[string]time.Time
}

func (e *gameplaySessionRegistry) controlledNPCPeerLocked(ctx context.Context, req scenario.ControlledNPCIdentity) (gameplayPeerSession, zonenpc.ScenarioControlState, error) {
	if req.RunID == "" || req.UserID <= 0 || req.GameID == 0 || req.PeerGeneration == 0 || req.TransportGeneration == 0 || req.ZoneGeneration == 0 || req.SpawnRequestID == "" || req.ObjectID == 0 {
		return gameplayPeerSession{}, zonenpc.ScenarioControlState{}, errors.New("NPC control requires a complete owned spawned-actor identity")
	}
	spawn, isFound := e.spawnReceipts[req.SpawnRequestID]
	if !isFound || spawn.RunID != req.RunID || spawn.UserID != req.UserID || spawn.GameID != req.GameID || spawn.PeerGeneration != req.PeerGeneration || spawn.TransportGeneration != req.TransportGeneration || spawn.ZoneGeneration != req.ZoneGeneration || spawn.ObjectID != req.ObjectID || spawn.IsAIEnabled || !spawn.IsPublished || spawn.Failure != "" {
		return gameplayPeerSession{}, zonenpc.ScenarioControlState{}, errors.New("NPC handle is not an owned published controlled spawn")
	}
	peer, observation, err := e.scenarioSessionLocked(ctx, req.UserID)
	if err != nil {
		return peer, zonenpc.ScenarioControlState{}, fmt.Errorf("npcPeer: %w", err)
	}
	if observation.Outcome != scenario.Passed || peer.binding.GameID != req.GameID || peer.generation != req.PeerGeneration || peer.transportGeneration != req.TransportGeneration || peer.zone == nil || !peer.stage.IsDungeon() || !peer.dungeonSetup.IsCommitted() || peer.zone.Snapshot().Generation != req.ZoneGeneration || peer.isZoneTerminal() {
		return peer, zonenpc.ScenarioControlState{}, errors.New("NPC handle game, peer, transport or zone is stale")
	}
	err = e.validateFocusedSpawnCohortLocked(peer, req.RunID)
	if err != nil {
		return peer, zonenpc.ScenarioControlState{}, fmt.Errorf("npcCohort: %w", err)
	}
	state, err := peer.zone.NPCs().ScenarioControlState(zonenpc.ScenarioControlRef{ObjectID: req.ObjectID, ControlID: req.SpawnRequestID})
	if err != nil {
		return peer, state, fmt.Errorf("npcActor: %w", err)
	}
	return peer, state, nil
}

func (e *gameplaySessionRegistry) ScenarioControlledNPCState(ctx context.Context, req scenario.ControlledNPCIdentity) (scenario.ControlledNPCState, error) {
	result := scenario.ControlledNPCState{Identity: req}
	err := e.lockScenarioSpawn(ctx)
	if err != nil {
		return result, fmt.Errorf("npcStateLock: %w", err)
	}
	defer e.mutex.Unlock()
	peer, actor, err := e.controlledNPCPeerLocked(ctx, req)
	if err != nil {
		return result, fmt.Errorf("npcStateActor: %w", err)
	}
	result.IsAvailable = true
	result.IsAIEnabled = actor.IsAIEnabled
	result.IsDead = actor.IsDefeated || actor.HitPoint <= 0
	result.HitPoint = actor.HitPoint
	result.Position = scenario.WorldPosition{X: actor.Position.X, Y: actor.Position.Y, Z: actor.Position.Z}
	result.ActionGeneration = actor.ActionGeneration
	result.SupportedAbilityNames = append([]string(nil), actor.SupportedAbilities...)
	requestID := e.lastNPCControlIDsBySpawn[req.SpawnRequestID]
	if requestID != "" {
		receipt := e.refreshNPCControlLocked(requestID, peer, actor)
		result.LastCommand = &receipt
	}
	return result, nil
}

func (e *gameplaySessionRegistry) ScenarioControlNPC(ctx context.Context, req scenario.NPCControlRequest) (scenario.NPCControlReceipt, error) {
	err := e.lockScenarioSpawn(ctx)
	if err != nil {
		return scenario.NPCControlReceipt{}, fmt.Errorf("npcQueueLock: %w", err)
	}
	defer e.mutex.Unlock()
	peer, actor, err := e.controlledNPCPeerLocked(ctx, req.Identity)
	if err != nil {
		return scenario.NPCControlReceipt{}, fmt.Errorf("npcQueueActor: %w", err)
	}
	isCancellation := req.Operation == "set_ai" && req.IsAIEnabled != nil && !*req.IsAIEnabled
	if actor.IsDefeated || actor.HitPoint <= 0 || peer.deployedHitPoint() <= 0 && !isCancellation {
		return scenario.NPCControlReceipt{}, errors.New("NPC mutation requires the spawned actor and owned hero alive")
	}
	err = validateScenarioNPCRequest(req, peer, actor)
	if err != nil {
		return scenario.NPCControlReceipt{}, fmt.Errorf("npcQueueRequest: %w", err)
	}
	previousID := e.lastNPCControlIDsBySpawn[req.Identity.SpawnRequestID]
	if previousID != "" {
		previous := e.refreshNPCControlLocked(previousID, peer, actor)
		if !previous.IsSettled && !isCancellation {
			return previous, errors.New("controlled NPC already has an unresolved command; do not replay")
		}
	}
	isReplacingQueued := isCancellation && previousID != "" && e.npcControlReceipts[previousID].State == "queued"
	if len(e.pendingNPCControls) >= 8 && !isReplacingQueued || len(e.npcControlReceipts) >= 128 && !isCancellation {
		return scenario.NPCControlReceipt{}, errors.New("NPC control receipt or pending-command limit reached")
	}
	supersededRequestID, supersededFailure := "", ""
	if previousID != "" && isCancellation {
		previous := e.npcControlReceipts[previousID]
		if !previous.IsSettled {
			previous.State, previous.IsSettled = "failed", true
			previous.Failure = "superseded by an explicit AI-off command; actor fence awaits packet-owner application"
			supersededRequestID, supersededFailure = previousID, previous.Failure
			e.npcControlReceipts[previousID] = previous
			for index, pendingID := range e.pendingNPCControls {
				if pendingID == previousID {
					e.pendingNPCControls = append(e.pendingNPCControls[:index], e.pendingNPCControls[index+1:]...)
					break
				}
			}
		}
	}
	if len(e.npcControlReceipts) >= 128 && !e.reclaimSettledNPCControlLocked() {
		return scenario.NPCControlReceipt{}, errors.New("NPC control ledger is full with no settled or owned superseded receipt to reclaim")
	}
	if e.npcControlReceipts == nil {
		e.npcControlReceipts = make(map[string]scenario.NPCControlReceipt)
		e.npcControlRequests = make(map[string]scenario.NPCControlRequest)
		e.lastNPCControlIDsBySpawn = make(map[string]string)
		e.npcControlDeadlines = make(map[string]time.Time)
	}
	if req.Destination != nil {
		destination := *req.Destination
		req.Destination = &destination
	}
	if req.IsAIEnabled != nil {
		isAIEnabled := *req.IsAIEnabled
		req.IsAIEnabled = &isAIEnabled
	}
	e.nextNPCControlID++
	receipt := scenario.NPCControlReceipt{RequestID: fmt.Sprintf("npc-%016x", e.nextNPCControlID), Identity: req.Identity, Operation: req.Operation, State: "queued",
		SupersededRequestID: supersededRequestID, SupersededFailure: supersededFailure}
	if req.Destination != nil {
		requestedDestination := *req.Destination
		receipt.RequestedDestination = &requestedDestination
	}
	e.npcControlReceipts[receipt.RequestID] = receipt
	e.npcControlRequests[receipt.RequestID] = req
	deadline, isDeadline := ctx.Deadline()
	if !isDeadline {
		deadline = time.Now().Add(30 * time.Second)
	}
	e.npcControlDeadlines[receipt.RequestID] = deadline
	e.pendingNPCControls = append(e.pendingNPCControls, receipt.RequestID)
	e.lastNPCControlIDsBySpawn[req.Identity.SpawnRequestID] = receipt.RequestID
	return receipt, nil
}

// Recovery remains bounded: only settled history can be reclaimed. If AI-off
// just superseded its own unresolved command, its outcome is copied into the
// new receipt before that old record becomes eligible for reclamation.
func (e *gameplaySessionRegistry) reclaimSettledNPCControlLocked() bool {
	oldestID := ""
	for requestID, receipt := range e.npcControlReceipts {
		if receipt.IsSettled && (oldestID == "" || requestID < oldestID) {
			oldestID = requestID
		}
	}
	if oldestID == "" {
		return false
	}
	receipt := e.npcControlReceipts[oldestID]
	delete(e.npcControlReceipts, oldestID)
	delete(e.npcControlRequests, oldestID)
	delete(e.npcControlDeadlines, oldestID)
	if e.lastNPCControlIDsBySpawn[receipt.Identity.SpawnRequestID] == oldestID {
		delete(e.lastNPCControlIDsBySpawn, receipt.Identity.SpawnRequestID)
	}
	for index, pendingID := range e.pendingNPCControls {
		if pendingID == oldestID {
			e.pendingNPCControls = append(e.pendingNPCControls[:index], e.pendingNPCControls[index+1:]...)
			break
		}
	}
	return true
}

func validateScenarioNPCRequest(req scenario.NPCControlRequest, peer gameplayPeerSession, actor zonenpc.ScenarioControlState) error {
	switch req.Operation {
	case "set_ai":
		if req.IsAIEnabled == nil || req.Destination != nil || req.AbilityName != "" || req.TargetObjectID != 0 {
			return errors.New("set_ai requires only an explicit AI flag")
		}
		if *req.IsAIEnabled && time.Now().Before(actor.NextActionAt) {
			return errors.New("enabling NPC AI before its preserved action cooldown expires is unsupported; no mutation was admitted")
		}
	case "move":
		if req.Destination == nil || req.IsAIEnabled != nil || req.AbilityName != "" || req.TargetObjectID != 0 || actor.IsAIEnabled {
			return errors.New("manual move requires AI off and only a destination")
		}
		point := game.Vec3{X: req.Destination.X, Y: req.Destination.Y, Z: req.Destination.Z}
		if !zonegeometry.IsFinite(point) || point.Sub(actor.Position).Length() > 20 {
			return errors.New("manual move destination must be finite and within twenty units")
		}
	case "ability":
		if req.IsAIEnabled != nil || req.Destination != nil || req.AbilityName == "" || req.TargetObjectID != peer.deployedObjectID || actor.IsAIEnabled {
			return errors.New("manual ability requires AI off, an exact supported name and the owned deployed hero target")
		}
		isSupported := false
		for _, name := range actor.SupportedAbilities {
			if req.AbilityName == name {
				isSupported = true
				break
			}
		}
		if !isSupported {
			return errors.New("manual ability name is not supported by this spawned actor")
		}
	case "restore":
		if req.IsAIEnabled != nil || req.Destination != nil || req.AbilityName != "" || req.TargetObjectID != 0 ||
			actor.IsAIEnabled || actor.IsActionStarted {
			return errors.New("NPC restore requires an idle AI-off actor and no other command fields")
		}
	default:
		return errors.New("NPC control operation is unsupported")
	}
	return nil
}

func (e *gameplaySessionRegistry) ScenarioNPCControlState(ctx context.Context, req scenario.ControlledNPCIdentity, requestID string) (scenario.NPCControlReceipt, error) {
	err := e.lockScenarioSpawn(ctx)
	if err != nil {
		return scenario.NPCControlReceipt{}, fmt.Errorf("npcReceiptLock: %w", err)
	}
	defer e.mutex.Unlock()
	receipt, isFound := e.npcControlReceipts[requestID]
	if !isFound || receipt.Identity != req {
		return scenario.NPCControlReceipt{}, errors.New("NPC control receipt does not belong to this exact actor handle")
	}
	peer, actor, err := e.controlledNPCPeerLocked(ctx, req)
	if err != nil {
		return receipt, fmt.Errorf("npcReceiptActor: %w", err)
	}
	return e.refreshNPCControlLocked(requestID, peer, actor), nil
}

func (e *gameplaySessionRegistry) refreshNPCControlLocked(requestID string, peer gameplayPeerSession, actor zonenpc.ScenarioControlState) scenario.NPCControlReceipt {
	receipt := e.npcControlReceipts[requestID]
	if receipt.IsSettled || !receipt.IsAccepted {
		return receipt
	}
	if actor.IsDefeated || actor.HitPoint <= 0 {
		receipt.State, receipt.Failure, receipt.IsSettled = "failed", "controlled actor died before command settlement", true
	} else if receipt.ActionGeneration != actor.ActionGeneration {
		receipt.State, receipt.Failure, receipt.IsSettled = "failed", "controlled actor action generation changed; outcome is not correlated", true
	} else if receipt.Operation == "ability" && !actor.IsActionStarted {
		// Normal action release proves settlement only. Impact and damage
		// remain independently observable; cancellation must not imply a hit.
		receipt.State, receipt.IsSettled = "settled", true
	}
	e.npcControlReceipts[requestID] = receipt
	return receipt
}
