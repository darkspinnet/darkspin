package sim

import (
	"testing"
	"time"
)

func TestBurstBlockedCollisionRetiresWithoutCancellingNextShot(t *testing.T) {
	simulator := New("burst")
	scope := simulator.ActivateRole("actor")
	behavior, err := StartProjectileBurst(simulator, scope, ProjectileBurstInput{
		ActorRole: "actor", TargetRole: "target", ProjectileRoles: []Role{"first", "second"},
		AbilityName: "burst", AnimationName: "cast", ProjectileNoun: "projectile", ImpactEffectName: "impact",
		ActorFacing: Position{X: 1}, TargetPosition: Position{X: 10},
		ShotDelays: []time.Duration{0, time.Millisecond}, ReleaseDelay: time.Millisecond,
		ProjectileSpeed: 20, ProjectileDistance: 10, Damage: 5, TargetHitPoint: 100, IsTargetValid: true,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	err = simulator.AdvanceTo(time.Millisecond)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	eventCount := len(simulator.Events())
	err = behavior.ResolveCollision(0, true, true, 100, 0, false, Position{X: 10}, Position{X: 1})
	if err != nil {
		t.Fatalf("blocked collision: %v", err)
	}
	err = behavior.ResolveCollision(1, true, true, 100, 5, false, Position{X: 10}, Position{X: 1})
	if err != nil {
		t.Fatalf("following collision: %v", err)
	}
	damageCount, healthCount, impactCount, despawnCount := 0, 0, 0, 0
	for _, event := range simulator.Events()[eventCount:] {
		switch intent := event.Intent.(type) {
		case DamageIntent:
			damageCount++
			if intent.DeltaHealth != 5 {
				t.Fatal("blocked collision emitted damage")
			}
		case HitPointIntent:
			healthCount++
			if intent.HitPoints != 95 {
				t.Fatal("blocked collision changed health")
			}
		case ProjectileImpactIntent:
			impactCount++
		case DespawnIntent:
			despawnCount++
		}
	}
	if damageCount != 1 || healthCount != 1 || impactCount != 2 || despawnCount != 2 {
		t.Fatalf("collision trace damage=%d health=%d impact=%d despawn=%d", damageCount, healthCount, impactCount, despawnCount)
	}
	err = behavior.ResolveCollision(0, true, true, 100, 0, false, Position{X: 10}, Position{X: 1})
	if err == nil {
		t.Fatal("resolved blocked shot was accepted twice")
	}
}
