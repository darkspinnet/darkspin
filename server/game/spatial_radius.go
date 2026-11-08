package game

import (
	"errors"
	"math"
)

// SpatialRadius follows build 103's 9D8220 object-tree sphere. Unlike the
// navigation footprint, this radius includes height above the object's origin.
func (e NavigationFootprint) SpatialRadius(scale float32) (float32, error) {
	if !isNavigationScalar(scale) || scale <= 0 || e.SizeClass > 17 {
		return 0, errors.New("invalid spatial bounds scale or class")
	}
	var x, y, z float64
	if e.SizeClass != 0 {
		if !isNavigationScalar(e.Extents.X) || !isNavigationScalar(e.Extents.Z) ||
			e.Extents.X <= 0 || e.Extents.Z < 0 {
			return 0, errors.New("spatial extents unavailable")
		}
		x = float64(e.Extents.X) * 0.5
		y = x
		z = float64(e.Extents.Z)
	} else {
		if !isNavigationScalar(e.Minimum.X) || !isNavigationScalar(e.Minimum.Y) ||
			!isNavigationScalar(e.Minimum.Z) || !isNavigationScalar(e.Maximum.X) ||
			!isNavigationScalar(e.Maximum.Y) || !isNavigationScalar(e.Maximum.Z) {
			return 0, errors.New("invalid spatial bounds")
		}
		x = max(math.Abs(float64(e.Minimum.X)), math.Abs(float64(e.Maximum.X)))
		y = max(math.Abs(float64(e.Minimum.Y)), math.Abs(float64(e.Maximum.Y)))
		z = max(math.Abs(float64(e.Minimum.Z)), math.Abs(float64(e.Maximum.Z)))
	}
	radius := float32(math.Sqrt(x*x+y*y+z*z) * float64(scale))
	if !isNavigationScalar(radius) {
		return 0, errors.New("spatial radius overflow")
	}
	return radius, nil
}
