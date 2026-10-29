//go:build !scenario

package npc

type scenarioControlPlan struct{}

func (e Snapshot) IsScenarioAIEnabled() bool { return true }

func (e Snapshot) IsScenarioControlled() bool { return false }

func (e Snapshot) IsScenarioDefeated() bool { return false }

func markScenarioResurrection(actor *Snapshot) {}

func admitScenarioAction(actor Snapshot, controlID string) bool { return controlID == "" }

func markScenarioAction(actor *Snapshot, controlID string) {}

func markScenarioSelfDeath(actor *Snapshot, sourceObjectID uint32, targetObjectID uint32, isForcedDefeat bool) {
}

func isScenarioPursuitAllowed(actor Snapshot) bool { return true }
