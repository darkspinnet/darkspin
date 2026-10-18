package game

import "slices"

type AIPhaseType uint32

const (
	AIPhasePrioritizedList AIPhaseType = iota
	AIPhaseSequential
	AIPhaseRandom
)

// CampaignAIGraph retains authored graph order without assigning execution or
// transition behavior to the phase enum. Unresolved definitions retain their ID.
type CampaignAIGraph struct {
	PassiveAbility     *string
	PreAggroIdle       string
	PreAggroIdle2      string
	UseSecondaryStart  float32
	FirstAggroAbility  *string
	FirstAggroAbility2 *string
	DefinitionID       uint32
	IsResolved         bool
	Nodes              []CampaignAINode
}

type CampaignAINode struct {
	PhaseName     *string
	ConditionName *string
	PhaseID       uint32
	ConditionID   uint32
	Phase         *CampaignAIPhase
	Condition     *CampaignAICondition
	Outputs       []uint32
}

type CampaignAIPhase struct {
	PhaseType   AIPhaseType
	IsStartNode bool
	Gambits     []CampaignAIGambit
}

type CampaignAIGambit struct {
	Condition           *string
	Ability             *string
	ConditionProps      []CampaignAIProperty
	AbilityProps        []CampaignAIProperty
	IsRandomizeCooldown bool
}

type CampaignAICondition struct {
	Condition            *string
	Properties           []CampaignAIProperty
	IsActivateOnce       bool
	IsCheckOnSequenceEnd bool
	ActivateTime         float32
	CheckTimeInterval    float32
}

type CampaignAIProperty struct {
	Key     uint32
	Name    string
	Type    uint32
	Text    string
	Runtime CampaignAIPropertyRuntime
}

type CampaignAIPropertyRuntime struct {
	Kind     string
	IsTrue   bool
	Integer  int32
	Unsigned uint32
	Float    float32
	Text     string
}

func (e *CampaignAIGraph) Clone() *CampaignAIGraph {
	if e == nil {
		return nil
	}
	clone := *e
	clone.PassiveAbility = cloneAIReference(e.PassiveAbility)
	clone.FirstAggroAbility2 = cloneAIReference(e.FirstAggroAbility2)
	clone.FirstAggroAbility = cloneAIReference(e.FirstAggroAbility)
	clone.Nodes = slices.Clone(e.Nodes)
	for index := range clone.Nodes {
		node := &clone.Nodes[index]
		node.PhaseName = cloneAIReference(node.PhaseName)
		node.ConditionName = cloneAIReference(node.ConditionName)
		node.Outputs = slices.Clone(node.Outputs)
		if node.Phase != nil {
			phase := *node.Phase
			phase.Gambits = slices.Clone(phase.Gambits)
			for gambitIndex := range phase.Gambits {
				gambit := &phase.Gambits[gambitIndex]
				gambit.Condition = cloneAIReference(gambit.Condition)
				gambit.Ability = cloneAIReference(gambit.Ability)
				gambit.ConditionProps = slices.Clone(gambit.ConditionProps)
				gambit.AbilityProps = slices.Clone(gambit.AbilityProps)
			}
			node.Phase = &phase
		}
		if node.Condition != nil {
			condition := *node.Condition
			condition.Condition = cloneAIReference(condition.Condition)
			condition.Properties = slices.Clone(condition.Properties)
			node.Condition = &condition
		}
	}
	return &clone
}

func cloneAIReference(reference *string) *string {
	if reference == nil {
		return nil
	}
	name := *reference
	return &name
}
