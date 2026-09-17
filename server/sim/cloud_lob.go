package sim

import (
	"errors"
	"math"
	"time"
)

const cloudLobCloseMinimumDistance = float32(5)
const cloudLobCloseMaximumDistance = float32(8)
const cloudLobCloseDuration = 300 * time.Millisecond
const cloudLobMaximumFlightMultiplier = float32(1.8)

type CloudLobCast struct {
	Duration time.Duration
	Height   float32
}

// ResolveCloudLobCast preserves Sprout's authored two-stage flight shaping.
// Nominal flight scales from the base duration to 1.8x at maximum range; the
// close band blends from a flat 0.3-second ground lob at distance five to the
// nominal duration and height at distance eight.
func ResolveCloudLobCast(definition AbilityDefinition, targetDistance float32) (CloudLobCast, error) {
	if definition.Kind != AbilityKindCloudLob || definition.Range <= 0 ||
		definition.CloudLob.FlightTime <= 0 || definition.CloudLob.Height <= 0 ||
		targetDistance < 0 || targetDistance > definition.Range ||
		math.IsNaN(float64(targetDistance)) || math.IsInf(float64(targetDistance), 0) {
		return CloudLobCast{}, errors.New("invalid cloud lob definition")
	}
	rangeRatio := targetDistance / definition.Range
	nominalMultiplier := 1 + (cloudLobMaximumFlightMultiplier-1)*rangeRatio
	nominalDuration := time.Duration(float32(definition.CloudLob.FlightTime) * nominalMultiplier)
	if targetDistance >= cloudLobCloseMaximumDistance {
		return CloudLobCast{Duration: nominalDuration, Height: definition.CloudLob.Height}, nil
	}
	if targetDistance <= cloudLobCloseMinimumDistance {
		return CloudLobCast{Duration: cloudLobCloseDuration}, nil
	}
	closeRatio := (targetDistance - cloudLobCloseMinimumDistance) /
		(cloudLobCloseMaximumDistance - cloudLobCloseMinimumDistance)
	durationDelta := nominalDuration - cloudLobCloseDuration
	return CloudLobCast{
		Duration: cloudLobCloseDuration + time.Duration(float32(durationDelta)*closeRatio),
		Height:   definition.CloudLob.Height * closeRatio,
	}, nil
}
