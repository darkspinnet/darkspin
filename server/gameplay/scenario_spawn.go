//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type scenarioSpawnState struct {
	nextSpawnID   uint64
	spawnReceipts map[string]scenario.SpawnReceipt
	pendingSpawns []string
}

type scenarioSpawnProvider interface {
	ScenarioSpawnNPC(context.Context, scenario.SpawnRequest) (scenario.SpawnReceipt, error)
	ScenarioSpawnState(context.Context, string, int64, string) (scenario.SpawnReceipt, error)
}

func (e Lifecycle) ScenarioSpawnNPC(ctx context.Context, req scenario.SpawnRequest) (scenario.SpawnReceipt, error) {
	provider, isSupported := e.syncSnapshot.(scenarioSpawnProvider)
	if !isSupported {
		return scenario.SpawnReceipt{}, errors.New("focused spawn provider unavailable")
	}
	receipt, err := provider.ScenarioSpawnNPC(ctx, req)
	if err != nil {
		return receipt, fmt.Errorf("spawnProvider: %w", err)
	}
	return receipt, nil
}

func (e Lifecycle) ScenarioSpawnState(ctx context.Context, runID string, userID int64, requestID string) (scenario.SpawnReceipt, error) {
	provider, isSupported := e.syncSnapshot.(scenarioSpawnProvider)
	if !isSupported {
		return scenario.SpawnReceipt{}, errors.New("focused spawn provider unavailable")
	}
	receipt, err := provider.ScenarioSpawnState(ctx, runID, userID, requestID)
	if err != nil {
		return receipt, fmt.Errorf("spawnStateProvider: %w", err)
	}
	return receipt, nil
}

func (e *gameplaySessionRegistry) lockScenarioSpawn(ctx context.Context) error {
	if ctx == nil || e == nil {
		return errors.New("focused spawn requires context and live registry")
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := ctx.Err()
		if err != nil {
			return fmt.Errorf("spawnContext: %w", err)
		}
		if e.mutex.TryLock() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("spawnLock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (e *gameplaySessionRegistry) ScenarioSpawnNPC(ctx context.Context, req scenario.SpawnRequest) (scenario.SpawnReceipt, error) {
	if req.RunID == "" || req.UserID <= 0 || req.GameID == 0 || req.PeerGeneration == 0 || req.ZoneGeneration == 0 || !isScenarioSpawnNoun(req.NounName) {
		return scenario.SpawnReceipt{}, errors.New("spawn requires owned run/user/game/zone and an exact noun filename")
	}
	err := e.lockScenarioSpawn(ctx)
	if err != nil {
		return scenario.SpawnReceipt{}, fmt.Errorf("spawnAdmissionLock: %w", err)
	}
	defer e.mutex.Unlock()
	peerSession, observation, err := e.scenarioSessionLocked(ctx, req.UserID)
	if err != nil {
		return scenario.SpawnReceipt{}, fmt.Errorf("spawnSession: %w", err)
	}
	if observation.Outcome != scenario.Passed || peerSession.binding.GameID != req.GameID || peerSession.generation != req.PeerGeneration || peerSession.zone == nil || !peerSession.stage.IsDungeon() || !peerSession.dungeonSetup.IsCommitted() || peerSession.deployedObjectID == 0 || peerSession.isZoneTerminal() {
		return scenario.SpawnReceipt{}, errors.New("spawn requires the current committed live dungeon and deployed hero")
	}
	err = e.validateFocusedSpawnCohortLocked(peerSession, req.RunID)
	if err != nil {
		return scenario.SpawnReceipt{}, fmt.Errorf("spawnCohort: %w", err)
	}
	zoneState := peerSession.zone.Snapshot()
	if zoneState.Generation != req.ZoneGeneration {
		return scenario.SpawnReceipt{}, errors.New("spawn zone generation is stale")
	}
	director := peerSession.zone.DirectorDefinition()
	if !isScenarioSpawnNounKnown(director, req.NounName) {
		return scenario.SpawnReceipt{}, errors.New("spawn noun is absent from the installed mission content catalog")
	}
	profile, isProfile := developerNPCProfile(director, req.NounName)
	if !isProfile || profile.HitPoint <= 0 {
		return scenario.SpawnReceipt{}, errors.New("spawn noun has no supported combat profile in the installed mission catalog")
	}
	if len(e.spawnReceipts) >= 128 || len(e.pendingSpawns) >= 8 {
		return scenario.SpawnReceipt{}, errors.New("focused spawn receipt or pending-command limit reached")
	}
	e.nextSpawnID++
	isAIEnabled := req.IsAIEnabled == nil || *req.IsAIEnabled
	if !isAIEnabled {
		controlPlan := zonenpc.SpawnPlan{NounName: req.NounName, NPCProfile: profile}
		preparedPlan, prepareErr := zonenpc.PrepareScenarioControlledPlan(controlPlan, "admission")
		if prepareErr != nil {
			return scenario.SpawnReceipt{}, fmt.Errorf("spawnControlProfile: %w", prepareErr)
		}
		if preparedPlan.NounName == "" {
			return scenario.SpawnReceipt{}, errors.New("controlled spawn profile omitted its noun")
		}
	}
	receipt := scenario.SpawnReceipt{RequestID: fmt.Sprintf("spawn-%016x", e.nextSpawnID), RunID: req.RunID, UserID: req.UserID, State: "queued", GameID: req.GameID, PeerGeneration: peerSession.generation, TransportGeneration: peerSession.transportGeneration, ZoneGeneration: req.ZoneGeneration, NounName: req.NounName, IsAIEnabled: isAIEnabled}
	if e.spawnReceipts == nil {
		e.spawnReceipts = make(map[string]scenario.SpawnReceipt)
	}
	e.spawnReceipts[receipt.RequestID] = receipt
	e.pendingSpawns = append(e.pendingSpawns, receipt.RequestID)
	return receipt, nil
}

func (e *gameplaySessionRegistry) prepareScenarioSpawnPlanLocked(requestID string, plan zonenpc.SpawnPlan) (zonenpc.SpawnPlan, error) {
	receipt, isFound := e.spawnReceipts[requestID]
	if !isFound || receipt.IsAIEnabled {
		return plan, nil
	}
	prepared, err := zonenpc.PrepareScenarioControlledPlan(plan, requestID)
	if err != nil {
		return plan, fmt.Errorf("spawnControlledPlan: %w", err)
	}
	return prepared, nil
}

func isScenarioSpawnNoun(nounName string) bool {
	if len(nounName) < 6 || len(nounName) > 128 || !strings.HasSuffix(strings.ToLower(nounName), ".noun") {
		return false
	}
	for _, character := range nounName[:len(nounName)-5] {
		if character != '_' && !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}

func isScenarioSpawnNounKnown(director game.CampaignDirector, nounName string) bool {
	for name := range director.NPCProfilesByNoun {
		if strings.EqualFold(name, nounName) {
			return true
		}
	}
	for _, pool := range director.Pools {
		for _, entry := range pool.Entries {
			if strings.EqualFold(entry.NounName, nounName) {
				return true
			}
		}
	}
	for _, entry := range director.StandaloneBossEntries {
		if strings.EqualFold(entry.NounName, nounName) {
			return true
		}
	}
	return false
}

func (e *gameplaySessionRegistry) ScenarioSpawnState(ctx context.Context, runID string, userID int64, requestID string) (scenario.SpawnReceipt, error) {
	err := e.lockScenarioSpawn(ctx)
	if err != nil {
		return scenario.SpawnReceipt{}, fmt.Errorf("spawnReadLock: %w", err)
	}
	defer e.mutex.Unlock()
	receipt, isFound := e.spawnReceipts[requestID]
	if !isFound || receipt.RunID != runID || receipt.UserID != userID {
		return scenario.SpawnReceipt{}, errors.New("spawn receipt is not owned by this run and user")
	}
	if receipt.State == "queued" || receipt.State == "admitted" {
		isCurrent := false
		for _, peerSession := range e.sessions {
			if peerSession.binding.UserID != uint64(userID) || peerSession.binding.GameID != receipt.GameID || peerSession.generation != receipt.PeerGeneration || peerSession.zone == nil {
				continue
			}
			if peerSession.zone.Snapshot().Generation == receipt.ZoneGeneration {
				isCurrent = true
				break
			}
		}
		if !isCurrent {
			receipt.State = "failed"
			receipt.Failure = "focused spawn peer or zone ended before confirmed publication"
			e.spawnReceipts[requestID] = receipt
			for index, pendingID := range e.pendingSpawns {
				if pendingID == requestID {
					e.pendingSpawns = append(e.pendingSpawns[:index], e.pendingSpawns[index+1:]...)
					break
				}
			}
		}
	}
	return receipt, nil
}

func (e gameplayPendingRuntime) consumeScenarioSpawn(ctx context.Context, packet raknet.Packet, peerSession gameplayPeerSession, isEligible bool) ([][]byte, bool, error) {
	if packet.ScheduleGroup == nil && packet.ScheduleGroupResult == nil {
		return nil, false, nil
	}
	err := e.registry.lockScenarioSpawn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("spawnConsumeLock: %w", err)
	}
	var receipt scenario.SpawnReceipt
	for index, requestID := range e.registry.pendingSpawns {
		pending := e.registry.spawnReceipts[requestID]
		if pending.UserID != int64(peerSession.binding.UserID) {
			continue
		}
		receipt = pending
		e.registry.pendingSpawns = append(e.registry.pendingSpawns[:index], e.registry.pendingSpawns[index+1:]...)
		break
	}
	e.registry.mutex.Unlock()
	if receipt.RequestID == "" {
		return nil, false, nil
	}
	if !isEligible || peerSession.zone == nil || peerSession.generation != receipt.PeerGeneration || peerSession.transportGeneration != receipt.TransportGeneration || peerSession.binding.GameID != receipt.GameID || peerSession.zone.Snapshot().Generation != receipt.ZoneGeneration {
		err = errors.New("focused spawn binding changed before packet-owner admission")
		e.registry.recordScenarioSpawnFailure(receipt.RequestID, err)
		return nil, true, fmt.Errorf("spawnConsumeIdentity: %w", err)
	}
	packets, isHandled, err := e.spawnDeveloperNPCForRequest(packet, peerSession, game.PlayerEventCommand{Name: "spawn", NounName: receipt.NounName}, receipt.RequestID)
	if err != nil {
		e.registry.recordScenarioSpawnFailure(receipt.RequestID, err)
		return packets, isHandled, fmt.Errorf("spawnConsumeApply: %w", err)
	}
	return packets, isHandled, nil
}

func (e *gameplaySessionRegistry) recordScenarioSpawnAdmissionLocked(requestID string, peerSession gameplayPeerSession, plan zonenpc.SpawnPlan) {
	if requestID == "" {
		return
	}
	receipt, isFound := e.spawnReceipts[requestID]
	if !isFound {
		return
	}
	receipt.IsAdmitted = true
	receipt.ObjectID = plan.ObjectID
	receipt.State = "admitted"
	e.spawnReceipts[requestID] = receipt
}

func (e *gameplaySessionRegistry) validateScenarioSpawnLocked(requestID string, peerSession gameplayPeerSession) error {
	if requestID == "" {
		return nil
	}
	receipt, isFound := e.spawnReceipts[requestID]
	if !isFound || receipt.IsAdmitted || receipt.Failure != "" || peerSession.zone == nil || receipt.GameID != peerSession.binding.GameID || receipt.PeerGeneration != peerSession.generation || receipt.TransportGeneration != peerSession.transportGeneration || receipt.ZoneGeneration != peerSession.zone.Snapshot().Generation {
		return errors.New("focused spawn request is stale or already attempted")
	}
	err := e.validateFocusedSpawnCohortLocked(peerSession, receipt.RunID)
	if err != nil {
		return fmt.Errorf("spawnApplyCohort: %w", err)
	}
	return nil
}

func (e *gameplaySessionRegistry) recordScenarioSpawnFailure(requestID string, err error) {
	if requestID == "" || err == nil {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	receipt, isFound := e.spawnReceipts[requestID]
	if !isFound {
		return
	}
	receipt.State = "failed"
	receipt.Failure = err.Error()
	e.spawnReceipts[requestID] = receipt
}

type scenarioSpawnCommit struct {
	registry  *gameplaySessionRegistry
	requestID string
}

func (e *gameplaySessionRegistry) finishScenarioSpawnPublication(packet raknet.Packet, requestID string) error {
	if requestID == "" {
		return nil
	}
	commit := scenarioSpawnCommit{registry: e, requestID: requestID}
	err := packet.AfterResponseCommit(commit.complete)
	if err != nil {
		e.recordScenarioSpawnFailure(requestID, err)
		return fmt.Errorf("spawnCommitRegister: %w", err)
	}
	return nil
}

func (e scenarioSpawnCommit) complete() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	receipt, isFound := e.registry.spawnReceipts[e.requestID]
	if !isFound || !receipt.IsAdmitted {
		return
	}
	receipt.IsPublished = true
	if receipt.Failure == "" {
		receipt.State = "published"
	}
	e.registry.spawnReceipts[e.requestID] = receipt
}
