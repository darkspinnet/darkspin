package contentsqlite

import (
	"context"
	"fmt"
	"slices"
	"strings"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/util"
)

func campaignAIGraphs(ctx context.Context, store levelDirectorStore) (map[uint32]*game.CampaignAIGraph, error) {
	assets, err := store.AIAssets(ctx)
	if err != nil {
		return nil, fmt.Errorf("graphAssets: %w", err)
	}
	phasesByID := make(map[uint32]*game.CampaignAIPhase)
	conditionsByID := make(map[uint32]*game.CampaignAICondition)
	definitionsByID := make(map[uint32]contentsqlite.AIDefinition)
	for _, asset := range assets {
		switch asset.AssetType {
		case 0xeeeb0e31: // AIDefinition
			definition, readErr := store.AIDefinition(ctx, asset.InstanceID)
			if readErr != nil {
				return nil, fmt.Errorf("graphDefinition[%#x]: %w", asset.InstanceID, readErr)
			}
			definitionsByID[asset.InstanceID] = definition
		case 0x30728ce7: // Phase
			phase, readErr := store.AIPhase(ctx, asset.InstanceID)
			if readErr != nil {
				return nil, fmt.Errorf("graphPhase[%#x]: %w", asset.InstanceID, readErr)
			}
			if phase.PhaseType > uint32(game.AIPhaseRandom) {
				return nil, fmt.Errorf("graphPhaseType[%#x]: %d", asset.InstanceID, phase.PhaseType)
			}
			mapped := &game.CampaignAIPhase{
				PhaseType: game.AIPhaseType(phase.PhaseType), IsStartNode: phase.IsStartNode,
				Gambits: make([]game.CampaignAIGambit, 0, len(phase.Gambits)),
			}
			for _, gambit := range phase.Gambits {
				mapped.Gambits = append(mapped.Gambits, game.CampaignAIGambit{
					Condition: gambit.Condition, Ability: gambit.Ability,
					ConditionProps:      campaignAIProperties(gambit.ConditionProps),
					AbilityProps:        campaignAIProperties(gambit.AbilityProps),
					IsRandomizeCooldown: gambit.IsRandomizeCooldown,
				})
			}
			phasesByID[asset.InstanceID] = mapped
		case 0x9f8087d5: // Condition
			condition, readErr := store.AICondition(ctx, asset.InstanceID)
			if readErr != nil {
				return nil, fmt.Errorf("graphCondition[%#x]: %w", asset.InstanceID, readErr)
			}
			conditionsByID[asset.InstanceID] = &game.CampaignAICondition{
				Condition: condition.Condition, Properties: campaignAIProperties(condition.Properties),
				IsActivateOnce: condition.IsActivateOnce, IsCheckOnSequenceEnd: condition.IsCheckOnSequenceEnd,
				ActivateTime: condition.ActivateTime, CheckTimeInterval: condition.CheckTimeInterval,
			}
		}
	}
	graphsByID := make(map[uint32]*game.CampaignAIGraph, len(definitionsByID))
	for definitionID, definition := range definitionsByID {
		graph := &game.CampaignAIGraph{
			PassiveAbility:     definition.PassiveAbility,
			PreAggroIdle:       definition.PreAggroIdle,
			PreAggroIdle2:      definition.PreAggroIdle2,
			UseSecondaryStart:  definition.UseSecondaryStart,
			FirstAggroAbility:  definition.FirstAggroAbility,
			FirstAggroAbility2: definition.FirstAggroAbility2,
			DefinitionID:       definitionID, IsResolved: true,
			Nodes: make([]game.CampaignAINode, 0, len(definition.Nodes)),
		}
		for _, node := range definition.Nodes {
			phaseID := campaignAIReferenceID(node.Phase)
			conditionID := campaignAIReferenceID(node.Condition)
			graph.Nodes = append(graph.Nodes, game.CampaignAINode{
				PhaseName: node.Phase, ConditionName: node.Condition,
				PhaseID: phaseID, ConditionID: conditionID,
				Phase: phasesByID[phaseID], Condition: conditionsByID[conditionID],
				Outputs: slices.Clone(node.Outputs),
			})
		}
		graphsByID[definitionID] = graph
	}
	return graphsByID, nil
}

func campaignAIGraph(definitionID uint32, graphsByID map[uint32]*game.CampaignAIGraph) *game.CampaignAIGraph {
	if definitionID == 0 {
		return nil
	}
	graph, isFound := graphsByID[definitionID]
	if isFound {
		return graph
	}
	return &game.CampaignAIGraph{DefinitionID: definitionID}
}

func campaignAIReferenceID(reference *string) uint32 {
	if reference == nil {
		return 0
	}
	name := *reference
	separator := strings.LastIndexByte(name, '.')
	if separator >= 0 {
		name = name[:separator]
	}
	return util.HashID(name)
}

func campaignAIProperties(properties []contentsqlite.AIProperty) []game.CampaignAIProperty {
	mappedProperties := make([]game.CampaignAIProperty, 0, len(properties))
	for _, property := range properties {
		mappedProperties = append(mappedProperties, game.CampaignAIProperty{
			Key: property.Key, Name: property.Name, Type: property.Type, Text: property.Text,
			Runtime: game.CampaignAIPropertyRuntime{
				Kind: property.Runtime.Kind, IsTrue: property.Runtime.IsTrue,
				Integer: property.Runtime.Integer, Unsigned: property.Runtime.Unsigned,
				Float: property.Runtime.Float, Text: property.Runtime.Text,
			},
		})
	}
	return mappedProperties
}
