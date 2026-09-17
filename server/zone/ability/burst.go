package ability

import (
	"math"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
)

// ProjectileActor is the immutable source snapshot needed to begin a projectile.
type ProjectileActor struct {
	ObjectID uint32
	Noun     string
	Position raknet.Vector3
}

// ProjectileGeometry carries collision bounds without exposing encounter state.
type ProjectileGeometry struct {
	ProjectileHalfExtent sim.Position
	TargetMinimum        sim.Position
	TargetMaximum        sim.Position
}

// ProjectileAuthority supplies only live actor position and authored rank damage.
type ProjectileAuthority interface {
	EnemyPosition(uint32) (raknet.Vector3, bool)
	SelectRankDamage(sim.AbilityDefinition) (float32, error)
}

type ProjectileCriticalResolver func(string, float32) (sim.CriticalDamageResult, error)

type BurstActor = ProjectileActor
type BurstGeometry = ProjectileGeometry
type BurstAuthority = ProjectileAuthority
type BurstCriticalResolver = ProjectileCriticalResolver

func ProjectileDirection(from raknet.Vector3, to raknet.Vector3) raknet.Vector3 {
	vector := raknet.Vector3{
		X: to.X - from.X,
		Y: to.Y - from.Y,
		Z: to.Z - from.Z,
	}
	lengthSquared := vector.X*vector.X + vector.Y*vector.Y + vector.Z*vector.Z
	if lengthSquared == 0 {
		return raknet.Vector3{X: 1}
	}
	length := float32(math.Sqrt(float64(lengthSquared)))
	return raknet.Vector3{
		X: vector.X / length,
		Y: vector.Y / length,
		Z: vector.Z / length,
	}
}

func BurstDirection(from raknet.Vector3, to raknet.Vector3) raknet.Vector3 {
	return ProjectileDirection(from, to)
}
