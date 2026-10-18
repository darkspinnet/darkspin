//go:build !scenario

package npc

type scenarioFixtureLedger struct{}

func (e *Session) recordScenarioFixtureAdmission(plan SpawnPlan) {}

func (e *Session) recordScenarioFixtureRollback(plan SpawnPlan) {}
