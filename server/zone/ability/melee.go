package ability

import (
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

type MeleeActor struct {
	ObjectID uint32
	Noun     string
	Position game.Vec3
}

type MeleeCandidate struct {
	Actor             MeleeActor
	Ability           sim.AbilityDefinition
	ActorPosition     game.Vec3
	AttackerFootprint float32
	TargetFootprint   float32
}

type MeleeAuthority interface {
	HasEnemy(uint32) bool
	EnemyPosition(uint32) (game.Vec3, bool)
	SelectRankDamage(sim.AbilityDefinition) (float32, error)
}
