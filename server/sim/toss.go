package sim

import (
	"errors"
	"fmt"
	"math"
	"time"
)

type TossCast struct {
	AnimationName     string
	Height            float32
	Speed             float32
	BounceRestitution float32
	Offset            Position
	IsClose           bool
}

// ResolveTossCast applies the shared template's strict near-range branch and
// component-wise close-offset overrides without losing authored absence.
func ResolveTossCast(definition AbilityDefinition, targetDistance float32) (TossCast, error) {
	if definition.Kind != AbilityKindToss || definition.Toss.NearAnimationName == "" ||
		definition.Toss.FarAnimationName == "" || definition.Toss.CloseRange <= 0 ||
		definition.Toss.Height < 0 ||
		(definition.Toss.Speed <= 0 && definition.Toss.FlightTime <= 0) || targetDistance < 0 ||
		math.IsNaN(float64(targetDistance)) || math.IsInf(float64(targetDistance), 0) {
		return TossCast{}, errors.New("invalid toss definition")
	}
	cast := TossCast{
		AnimationName: definition.Toss.FarAnimationName,
		Height:        definition.Toss.Height, Speed: definition.Toss.Speed,
		BounceRestitution: definition.Toss.BounceRestitution,
		Offset:            definition.Toss.Offset,
	}
	if targetDistance >= definition.Toss.CloseRange {
		// Template 630 interpolates the flight operands between closeRange
		// and range (or bounceRange); it does not jump straight to the far arc.
		farRange := definition.Range
		if definition.Toss.BounceRange > 0 {
			farRange = definition.Toss.BounceRange
		}
		if farRange <= definition.Toss.CloseRange {
			return TossCast{}, errors.New("invalid toss interpolation range")
		}
		portion := (targetDistance - definition.Toss.CloseRange) /
			(farRange - definition.Toss.CloseRange)
		if definition.Toss.CloseHeight > 0 {
			cast.Height = definition.Toss.CloseHeight +
				(definition.Toss.Height-definition.Toss.CloseHeight)*portion
		}
		if definition.Toss.CloseSpeed > 0 {
			cast.Speed = definition.Toss.CloseSpeed +
				(definition.Toss.Speed-definition.Toss.CloseSpeed)*portion
		}
		if definition.Toss.CloseBounceRestitution > 0 {
			cast.BounceRestitution = definition.Toss.CloseBounceRestitution +
				(definition.Toss.BounceRestitution-definition.Toss.CloseBounceRestitution)*portion
		}
		return cast, nil
	}
	cast.IsClose = true
	cast.AnimationName = definition.Toss.NearAnimationName
	if definition.Toss.CloseHeight > 0 {
		cast.Height = definition.Toss.CloseHeight
	}
	if definition.Toss.CloseSpeed > 0 {
		cast.Speed = definition.Toss.CloseSpeed
	}
	if definition.Toss.CloseBounceRestitution > 0 {
		cast.BounceRestitution = definition.Toss.CloseBounceRestitution
	}
	if definition.Toss.IsCloseOffsetXFound {
		cast.Offset.X = definition.Toss.CloseOffset.X
	}
	if definition.Toss.IsCloseOffsetYFound {
		cast.Offset.Y = definition.Toss.CloseOffset.Y
	}
	if definition.Toss.IsCloseOffsetZFound {
		cast.Offset.Z = definition.Toss.CloseOffset.Z
	}
	return cast, nil
}

// BuildTossLob resolves the authored toss alternatives into the exact
// trajectory operands reflected by build 103's lob locomotion component.
func BuildTossLob(
	startTime time.Duration, start Position, destination Position,
	height float32, flightTime time.Duration, speed float32,
	bounceCount uint32, bounceRestitution float32,
	isGroundCollisionOnly bool, isStopBounceOnCreature bool,
) (CrystalLob, error) {
	if startTime < 0 || height < 0 || bounceRestitution < 0 ||
		invalidTossPosition(start) || invalidTossPosition(destination) {
		return CrystalLob{}, errors.New("invalid toss lob")
	}
	deltaX := float64(destination.X - start.X)
	deltaY := float64(destination.Y - start.Y)
	deltaZ := float64(destination.Z - start.Z)
	planeDistance := math.Hypot(deltaX, deltaY)
	if planeDistance <= 0 {
		return CrystalLob{}, errors.New("zero toss plane distance")
	}
	duration := flightTime
	if duration <= 0 {
		if speed <= 0 || math.IsNaN(float64(speed)) || math.IsInf(float64(speed), 0) {
			return CrystalLob{}, errors.New("invalid toss speed")
		}
		duration = time.Duration(planeDistance / float64(speed) * float64(time.Second))
	}
	if duration <= 0 {
		return CrystalLob{}, errors.New("invalid toss duration")
	}
	var upLinearParameter float64
	var upQuadraticParameter float64
	if height == 0 && deltaZ < 0 {
		upQuadraticParameter = deltaZ / (planeDistance * planeDistance)
	} else {
		apexHeight := float64(height) + math.Max(deltaZ, 0)
		apexDistance := planeDistance / 2
		if deltaZ != 0 {
			discriminant := apexHeight*apexHeight - deltaZ*apexHeight
			if discriminant < 0 {
				return CrystalLob{}, errors.New("invalid toss apex discriminant")
			}
			apexDistance = (apexHeight - math.Sqrt(discriminant)) / deltaZ * planeDistance
		}
		if apexDistance <= 0 || math.IsNaN(apexDistance) || math.IsInf(apexDistance, 0) {
			return CrystalLob{}, errors.New("invalid toss apex distance")
		}
		upLinearParameter = 2 * apexHeight / apexDistance
		upQuadraticParameter = -apexHeight / (apexDistance * apexDistance)
	}
	durationSecond := duration.Seconds()
	trajectory := CrystalLob{
		StartTime: startTime, Duration: duration, Height: height,
		PlaneDirection: Position{
			X: float32(deltaX / planeDistance), Y: float32(deltaY / planeDistance),
		},
		PlaneDirectionVelocity: float32(planeDistance / durationSecond),
		UpLinearParameter:      float32(upLinearParameter),
		UpQuadraticParameter:   float32(upQuadraticParameter),
		BounceCount:            bounceCount,
		BounceRestitution:      bounceRestitution,
		IsGroundCollisionOnly:  isGroundCollisionOnly,
		IsStopBounceOnCreature: isStopBounceOnCreature,
	}
	if invalidTossLob(trajectory) {
		return CrystalLob{}, fmt.Errorf("invalid toss trajectory: %#v", trajectory)
	}
	return trajectory, nil
}

func invalidTossPosition(position Position) bool {
	for _, coordinate := range []float32{position.X, position.Y, position.Z} {
		if math.IsNaN(float64(coordinate)) || math.IsInf(float64(coordinate), 0) {
			return true
		}
	}
	return false
}

func invalidTossLob(lob CrystalLob) bool {
	for _, operand := range []float32{
		lob.Height, lob.PlaneDirection.X, lob.PlaneDirection.Y,
		lob.PlaneDirectionVelocity, lob.UpLinearParameter, lob.UpQuadraticParameter,
	} {
		if math.IsNaN(float64(operand)) || math.IsInf(float64(operand), 0) {
			return true
		}
	}
	return false
}
