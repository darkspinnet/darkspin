package gameplay

import (
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
)

// Command orientation is optional in older input paths. Keep their existing
// identity fallback while retaining finite reported facing for pose baselines.
func normalizedHeroOrientation(orientation raknet.Quaternion) game.Quaternion {
	lengthSquared := float64(orientation.X)*float64(orientation.X) + float64(orientation.Y)*float64(orientation.Y) +
		float64(orientation.Z)*float64(orientation.Z) + float64(orientation.W)*float64(orientation.W)
	if lengthSquared <= 0 || math.IsNaN(lengthSquared) || math.IsInf(lengthSquared, 0) {
		return game.Quaternion{W: 1}
	}
	length := math.Sqrt(lengthSquared)
	return game.Quaternion{
		X: float32(float64(orientation.X) / length), Y: float32(float64(orientation.Y) / length),
		Z: float32(float64(orientation.Z) / length), W: float32(float64(orientation.W) / length),
	}
}
