package raknet103

import (
	"errors"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/zone/geometry"
)

func Distance(start raknet.Vector3, end raknet.Vector3) float32 {
	return geometry.Distance(game.Vec3(start), game.Vec3(end))
}

func Direction(start raknet.Vector3, end raknet.Vector3) raknet.Vector3 {
	return raknet.Vector3(geometry.Direction(game.Vec3(start), game.Vec3(end)))
}

func Forward(orientation raknet.Quaternion) (raknet.Vector3, error) {
	lengthSquared := orientation.X*orientation.X + orientation.Y*orientation.Y +
		orientation.Z*orientation.Z + orientation.W*orientation.W
	if math.IsNaN(float64(lengthSquared)) || math.IsInf(float64(lengthSquared), 0) ||
		lengthSquared <= 0 {
		return raknet.Vector3{}, errors.New("invalid orientation")
	}
	length := float32(math.Sqrt(float64(lengthSquared)))
	x := orientation.X / length
	y := orientation.Y / length
	z := orientation.Z / length
	w := orientation.W / length
	return raknet.Vector3{
		X: 2 * (x*y - w*z),
		Y: 1 - 2*(x*x+z*z),
		Z: 2 * (y*z + w*x),
	}, nil
}
