//go:build scenario

package gameplay

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type scenarioNPCControlProvider interface {
	ScenarioControlledNPCState(context.Context, scenario.ControlledNPCIdentity) (scenario.ControlledNPCState, error)
	ScenarioControlNPC(context.Context, scenario.NPCControlRequest) (scenario.NPCControlReceipt, error)
	ScenarioNPCControlState(context.Context, scenario.ControlledNPCIdentity, string) (scenario.NPCControlReceipt, error)
}

func (e Lifecycle) ScenarioControlledNPCState(ctx context.Context, req scenario.ControlledNPCIdentity) (scenario.ControlledNPCState, error) {
	provider, isSupported := e.syncSnapshot.(scenarioNPCControlProvider)
	if !isSupported {
		return scenario.ControlledNPCState{}, errors.New("controlled NPC provider unavailable")
	}
	state, err := provider.ScenarioControlledNPCState(ctx, req)
	if err != nil {
		return state, fmt.Errorf("npcStateProvider: %w", err)
	}
	return state, nil
}

func (e Lifecycle) ScenarioControlNPC(ctx context.Context, req scenario.NPCControlRequest) (scenario.NPCControlReceipt, error) {
	provider, isSupported := e.syncSnapshot.(scenarioNPCControlProvider)
	if !isSupported {
		return scenario.NPCControlReceipt{}, errors.New("NPC control provider unavailable")
	}
	receipt, err := provider.ScenarioControlNPC(ctx, req)
	if err != nil {
		return receipt, fmt.Errorf("npcCommandProvider: %w", err)
	}
	return receipt, nil
}

func (e Lifecycle) ScenarioNPCControlState(ctx context.Context, req scenario.ControlledNPCIdentity, requestID string) (scenario.NPCControlReceipt, error) {
	provider, isSupported := e.syncSnapshot.(scenarioNPCControlProvider)
	if !isSupported {
		return scenario.NPCControlReceipt{}, errors.New("NPC control receipt provider unavailable")
	}
	receipt, err := provider.ScenarioNPCControlState(ctx, req, requestID)
	if err != nil {
		return receipt, fmt.Errorf("npcReceiptProvider: %w", err)
	}
	return receipt, nil
}
