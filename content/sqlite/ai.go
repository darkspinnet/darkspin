package sqlite

const aiDefinitionAssetType = uint32(0xeeeb0e31)
const aiPhaseAssetType = uint32(0x30728ce7)
const aiConditionAssetType = uint32(0x9f8087d5)

// These are imported authored data, not an implementation of AI scheduling.
// Nullable references distinguish absence from a present empty string.
type AIDefinition struct {
	Nodes                  []AINode
	DeathAbility           *string
	DeathCondition         *string
	FirstAggroAbility      *string
	FirstAggroAbility2     *string
	FirstAlertAbility      *string
	SubsequentAggroAbility *string
	PassiveAbility         *string
	CombatIdle2Condition   *string
	AggroType              uint32
	PreAggroIdle           string
	PreAggroIdle2          string
	CombatIdle             string
	PassiveIdle            string
	CombatIdle2            string
	TargetTooFar           string
	CombatIdleCooldown     uint32
	CombatIdle2Cooldown    uint32
	TargetTooFarCooldown   uint32
	IsFaceTarget           bool
	IsAlwaysRunAI          bool
	IsRandomizeCooldowns   bool
	UseSecondaryStart      float32
}

type AINode struct {
	Phase     *string
	Condition *string
	Outputs   []uint32
}

type AIPhase struct {
	Gambits     []AIGambit
	PhaseType   uint32
	IsStartNode bool
}

type AIGambit struct {
	Condition           *string
	ConditionProps      []AIProperty
	Ability             *string
	AbilityProps        []AIProperty
	IsRandomizeCooldown bool
}

type AICondition struct {
	Condition            *string
	Properties           []AIProperty
	IsActivateOnce       bool
	IsCheckOnSequenceEnd bool
	ActivateTime         float32
	CheckTimeInterval    float32
}

type AIProperty struct {
	Key     uint32
	Name    string
	Type    uint32
	Text    string
	Runtime AIPropertyRuntime
}

// Unsupported authored types retain the native numeric-zero result.
type AIPropertyRuntime struct {
	Kind     string
	IsTrue   bool
	Integer  int32
	Unsigned uint32
	Float    float32
	Text     string
}
