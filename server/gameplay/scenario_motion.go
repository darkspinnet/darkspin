//go:build scenario

package gameplay

import (
	"time"

	"github.com/darkspinnet/darkspin/server/scenario"
)

// The registry read lock pins these producers. Sampling a clone of retained
// movement does not advance command-driven movement or consume its observers.
func (e gameplayPeerSession) fillScenarioHeroMotion(hero *scenario.WorldHero, at time.Time) {
	if hero == nil || hero.Index != e.deployedCreatureIndex {
		return
	}
	hero.HostileMovementModifier = e.enemyMovementSpeedBuff()
	hero.IsStunned = e.isEnemyStunActive(at)
	hero.IsSilenced = e.isEnemySilenceActive(at)
	if run := e.missileFlakRun; run != nil && !run.isReleased && run.matches(e) {
		hero.FlakStackCount = run.stackCount
		hero.FlakAttackSpeed = run.attackSpeed
	}
	hero.HostileAttackModifier = e.enemyAttackSpeed()
	for _, objectID := range e.arenaSphereSlows {
		if objectID == e.deployedObjectID && objectID != 0 {
			hero.SphereMembershipCount++
		}
	}
	if e.playerMotion == nil {
		hero.MovementUnavailableReason = "movement producer is not initialized"
		return
	}
	position, err := e.playerMotion.SamplePosition(at)
	if err != nil {
		hero.MovementUnavailableReason = "movement sample: " + err.Error()
		return
	}
	hero.MovementPosition = scenario.WorldPosition{X: position.X, Y: position.Y, Z: position.Z}
	hero.MovementSpeed = e.playerMotion.Snapshot().Movement().Speed
	hero.IsMovementAvailable = true
}
