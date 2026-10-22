package npc

import (
	"fmt"
	"time"
)

const DrainerDeathDelay = 930000007 * time.Nanosecond

// IsDrainerDeath matches the installed condition/ability pair, rather than a
// name heuristic. Chunk 745 admits the death callback only at five mana.
func IsDrainerDeath(source Snapshot) bool {
	graph := source.Plan.NPCProfile.AIGraph
	return graph != nil && graph.DeathAbility != nil && *graph.DeathAbility == "DrainerDeathDetonate" &&
		graph.DeathCondition != nil && *graph.DeathCondition == "manaOverload" && source.ManaPoint >= 5
}

// CaptureDrainerDeath captures chunk 621's snapshot before its fixed wait.
func CaptureDrainerDeath(source Snapshot, at time.Time) (AttackPlan, error) {
	profile := ActionProfile{
		AbilityName: "DrainerDeathDetonate", MinimumDamage: 6, MaximumDamage: 12,
		DamageType: 4, DamageSource: 1, IsDamageProfileKnown: true, Radius: 6,
	}
	damage, err := resolveAttackDamageAt(source, profile, at)
	if err != nil {
		return AttackPlan{}, fmt.Errorf("drainerSnapshot: %w", err)
	}
	return AttackPlan{SourceObjectID: source.Plan.ObjectID, SourcePosition: source.Plan.Position,
		ActionGeneration: source.ActionGeneration, Profile: profile, Damage: damage}, nil
}

// FinishDrainerDeath cannot delete a revived or replaced actor. Resurrection
// advances ActionGeneration, even when the same world object ID is reused.
func (e *Session) FinishDrainerDeath(source Snapshot) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	current, isFound := e.npcs[source.Plan.ObjectID]
	if !isFound || !current.IsPublished || !current.IsDefeated || current.HitPoint > 0 ||
		current.ActionGeneration != source.ActionGeneration || current.Plan.NounName != source.Plan.NounName {
		return false
	}
	current.IsPublished = false
	e.npcs[source.Plan.ObjectID] = current
	return true
}
