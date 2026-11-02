package gameplay

import (
	"errors"
	"math"
)

// Psistorm's cloud uses while counter < bound, starting at zero and
// incrementing before damage. A fractional bound admits its final iteration;
// unlike an authored numeric for-loop, its positive iteration count rounds up.
func psistormTickCount(count uint32, increase float32) (uint32, error) {
	if count == 0 || math.IsNaN(float64(increase)) || math.IsInf(float64(increase), 0) {
		return 0, errors.New("invalid Psistorm duration count")
	}
	bound := float64(count) * (1 + float64(max(increase, 0)))
	iterations := math.Ceil(bound)
	if math.IsInf(iterations, 0) || iterations > float64(^uint32(0)) {
		return 0, errors.New("Psistorm duration count overflow")
	}
	return uint32(iterations), nil
}
