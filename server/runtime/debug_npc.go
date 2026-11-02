//go:build scenario

package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/scenario"
)

type debugNPCControlProvider interface {
	ScenarioControlledNPCState(context.Context, scenario.ControlledNPCIdentity) (scenario.ControlledNPCState, error)
	ScenarioControlNPC(context.Context, scenario.NPCControlRequest) (scenario.NPCControlReceipt, error)
	ScenarioNPCControlState(context.Context, scenario.ControlledNPCIdentity, string) (scenario.NPCControlReceipt, error)
}

func (e *Server) DebugControlledNPCState(ctx context.Context, req scenario.ControlledNPCIdentity) (scenario.ControlledNPCState, error) {
	if e == nil || ctx == nil {
		return scenario.ControlledNPCState{}, errors.New("controlled NPC service unavailable")
	}
	provider, isSupported := e.bugContext.(debugNPCControlProvider)
	if !isSupported {
		return scenario.ControlledNPCState{}, errors.New("controlled NPC provider unavailable")
	}
	state, err := provider.ScenarioControlledNPCState(ctx, req)
	if err != nil {
		return state, fmt.Errorf("npcObserve: %w", err)
	}
	return state, nil
}

func (e *Server) DebugControlNPC(ctx context.Context, req scenario.NPCControlRequest) (scenario.NPCControlReceipt, error) {
	if e == nil || ctx == nil {
		return scenario.NPCControlReceipt{}, errors.New("NPC control service unavailable")
	}
	provider, isSupported := e.bugContext.(debugNPCControlProvider)
	if !isSupported {
		return scenario.NPCControlReceipt{}, errors.New("NPC control provider unavailable")
	}
	receipt, err := provider.ScenarioControlNPC(ctx, req)
	if err != nil {
		return receipt, fmt.Errorf("npcControl: %w", err)
	}
	return receipt, nil
}

func (e *Server) DebugNPCControlState(ctx context.Context, req scenario.ControlledNPCIdentity, requestID string) (scenario.NPCControlReceipt, error) {
	if e == nil || ctx == nil {
		return scenario.NPCControlReceipt{}, errors.New("NPC control receipt service unavailable")
	}
	provider, isSupported := e.bugContext.(debugNPCControlProvider)
	if !isSupported {
		return scenario.NPCControlReceipt{}, errors.New("NPC control receipt provider unavailable")
	}
	receipt, err := provider.ScenarioNPCControlState(ctx, req, requestID)
	if err != nil {
		return receipt, fmt.Errorf("npcReceipt: %w", err)
	}
	return receipt, nil
}
