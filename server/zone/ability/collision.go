package ability

import "github.com/darkspinnet/darkspin/server/sim"

type ProjectileCollisionGeometry struct {
	ProjectileHalfExtent sim.Position
	TargetMinimum        sim.Position
	TargetMaximum        sim.Position
}
