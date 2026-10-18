package npc

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
)

// GraphState belongs to one actor. It does not infer phase execution order or
// traverse outputs; both indices are retained verbatim across checkpoint restore.
type GraphState struct {
	CurrentNodeIndex   int32
	CurrentGambitIndex int32
	IsInitialized      bool
	Diagnostic         string
}

type GraphContext struct {
	Phase     *game.CampaignAIPhase
	Gambit    *game.CampaignAIGambit
	Condition *game.CampaignAICondition
}

func initialGraphState(graph *game.CampaignAIGraph) GraphState {
	state := GraphState{CurrentNodeIndex: -1, CurrentGambitIndex: -1}
	if graph == nil {
		return state
	}
	if !graph.IsResolved {
		state.Diagnostic = "AI definition unavailable"
		return state
	}
	if len(graph.Nodes) == 0 {
		state.Diagnostic = "empty AI graph"
		return state
	}
	state.IsInitialized = true
	state.CurrentNodeIndex = 0
	for index, node := range graph.Nodes {
		if node.PhaseID != 0 && node.Phase != nil && node.Phase.IsStartNode {
			state.CurrentNodeIndex = int32(index)
			break
		}
	}
	context, err := state.Resolve(graph)
	if err != nil {
		state.Diagnostic = err.Error()
	}
	// Resolution diagnoses references and enum data without selecting a gambit.
	if context.Phase != nil && context.Phase.PhaseType > game.AIPhaseRandom {
		state.Diagnostic = "AI phase type invalid"
	}
	return state
}

// Resolve follows the native phase-reference precedence, including gambit -1.
func (e GraphState) Resolve(graph *game.CampaignAIGraph) (GraphContext, error) {
	if graph == nil || !graph.IsResolved {
		return GraphContext{}, errors.New("AI definition unavailable")
	}
	if !e.IsInitialized || e.CurrentNodeIndex < 0 || int(e.CurrentNodeIndex) >= len(graph.Nodes) {
		return GraphContext{}, errors.New("AI node unavailable")
	}
	node := graph.Nodes[e.CurrentNodeIndex]
	if node.PhaseID != 0 {
		if node.Phase == nil {
			return GraphContext{}, fmt.Errorf("AI phase %#x unavailable", node.PhaseID)
		}
		if e.CurrentGambitIndex == -1 {
			return GraphContext{Phase: node.Phase}, nil
		}
		if e.CurrentGambitIndex < 0 || int(e.CurrentGambitIndex) >= len(node.Phase.Gambits) {
			return GraphContext{}, errors.New("AI gambit unavailable")
		}
		return GraphContext{Phase: node.Phase, Gambit: &node.Phase.Gambits[e.CurrentGambitIndex]}, nil
	}
	if e.CurrentGambitIndex != -1 || node.ConditionID == 0 || node.Condition == nil {
		return GraphContext{}, errors.New("AI condition unavailable")
	}
	return GraphContext{Condition: node.Condition}, nil
}

func (e GraphState) Validate(graph *game.CampaignAIGraph) error {
	if graph == nil || !graph.IsResolved || len(graph.Nodes) == 0 {
		if e.IsInitialized || e.CurrentNodeIndex != -1 || e.CurrentGambitIndex != -1 {
			return errors.New("inactive AI graph cursor invalid")
		}
		return nil
	}
	if !e.IsInitialized || e.CurrentNodeIndex < 0 || int(e.CurrentNodeIndex) >= len(graph.Nodes) ||
		e.CurrentGambitIndex < -1 {
		return errors.New("AI graph cursor invalid")
	}
	if e.CurrentGambitIndex == -1 {
		return nil
	}
	context, err := e.Resolve(graph)
	if err != nil {
		return fmt.Errorf("graphCursor: %w", err)
	}
	if context.Gambit == nil {
		return errors.New("AI gambit cursor invalid")
	}
	return nil
}
