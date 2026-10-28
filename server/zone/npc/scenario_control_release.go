//go:build !scenario

package npc

type scenarioControlPlan struct{}

func (e Snapshot) IsScenarioAIEnabled() bool { return true }

func (e Snapshot) IsScenarioControlled() bool { return false }

func admitScenarioAction(actor Snapshot, controlID string) bool { return controlID == "" }

func markScenarioAction(actor *Snapshot, controlID string) {}

func markScenarioSelfDetonation(actor *Snapshot, sourceObjectID uint32, targetObjectID uint32, isForcedDefeat bool) {
}

func isScenarioPursuitAllowed(actor Snapshot) bool { return true }
