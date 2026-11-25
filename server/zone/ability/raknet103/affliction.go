package raknet103

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
)

// RetargetAffliction shares each sampled heading with the client. Letting the
// native mover independently track the target's root made it diverge from the
// curse sweep and descend below the owner's visible body.
func (e *ProjectileRun) RetargetAffliction(now time.Time, targetID uint32, target sim.Position, turnRate float32) ([][]byte, error) {
	if e == nil || now.IsZero() || turnRate <= 0 || math.IsNaN(float64(turnRate)) || math.IsInf(float64(turnRate), 0) {
		return nil, errors.New("invalid affliction steering")
	}
	for _, coordinate := range []float32{target.X, target.Y, target.Z} {
		if math.IsNaN(float64(coordinate)) || math.IsInf(float64(coordinate), 0) {
			return nil, errors.New("nonfinite affliction target")
		}
	}
	e.motionMutex.Lock()
	defer e.motionMutex.Unlock()
	if e.ability.Name != "AfflictionBolt" {
		return nil, errors.New("affliction projectile required")
	}
	if now.Before(e.launchAt) || now.Before(e.frozenUntil) || e.isFinished || e.isGravityDeflected ||
		e.behavior == nil || !e.behavior.IsProjectileActive() {
		return nil, nil
	}
	previousAt := e.afflictionTurnAt
	if previousAt.IsZero() {
		previousAt = e.launchAt
	}
	if previousAt.Before(e.frozenUntil) {
		previousAt = e.frozenUntil
	}
	if now.Before(previousAt) {
		return nil, errors.New("affliction clock regressed")
	}
	e.advanceMotionLocked(now)
	if e.motionRemaining <= 0 {
		return nil, nil
	}
	direction := e.direction
	if targetID != 0 {
		delta := sim.Position{X: target.X - e.motionPosition.X, Y: target.Y - e.motionPosition.Y, Z: target.Z - e.motionPosition.Z}
		direction = afflictionDirection(direction, delta, float64(turnRate)*now.Sub(previousAt).Seconds())
	}
	targetObjectID := uint32(0)
	initialDirection := raknet.Vector3(direction)
	lastUpdate := int32(1000)
	locomotionPacket, err := raknet.MarshalApplication(raknet.LocomotionUpdateContractMessage{
		ObjectID: e.objectID,
		Locomotion: raknet.LocomotionReflection{
			Projectile: &raknet.ProjectileParameter{
				Speed: e.speed * e.effectiveSpeedScaleLocked(), Range: e.motionRemaining,
				Direction: initialDirection, IsGroundCollisionIgnored: true, IsCreatureCollisionIgnored: true,
			},
			TargetObjectID: &targetObjectID, InitialDirection: &initialDirection, ReflectedLastUpdate: &lastUpdate,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("steeringMarshal: %w", err)
	}
	e.direction = direction
	e.targetObjectID = targetID
	e.targetPosition = target
	e.afflictionTurnAt = now
	// Steer the existing native mover. Rewriting its position at every sample
	// makes delivery jitter visible as repeated miniature teleports.
	return [][]byte{locomotionPacket}, nil
}

func afflictionDirection(direction, delta sim.Position, maximumAngle float64) sim.Position {
	length := math.Sqrt(float64(delta.X)*float64(delta.X) + float64(delta.Y)*float64(delta.Y) + float64(delta.Z)*float64(delta.Z))
	if length <= 0 || maximumAngle <= 0 {
		return direction
	}
	target := [3]float64{float64(delta.X) / length, float64(delta.Y) / length, float64(delta.Z) / length}
	current := [3]float64{float64(direction.X), float64(direction.Y), float64(direction.Z)}
	dot := max(-1.0, min(1.0, current[0]*target[0]+current[1]*target[1]+current[2]*target[2]))
	angle := math.Acos(dot)
	if angle <= maximumAngle {
		return sim.Position{X: float32(target[0]), Y: float32(target[1]), Z: float32(target[2])}
	}
	tangent := [3]float64{target[0] - dot*current[0], target[1] - dot*current[1], target[2] - dot*current[2]}
	tangentLength := math.Sqrt(tangent[0]*tangent[0] + tangent[1]*tangent[1] + tangent[2]*tangent[2])
	if tangentLength < 0.000001 {
		// An exactly opposite target needs a stable perpendicular turn axis.
		tangent = [3]float64{-current[1], current[0], 0}
		tangentLength = math.Hypot(tangent[0], tangent[1])
		if tangentLength < 0.000001 {
			tangent = [3]float64{1, 0, 0}
			tangentLength = 1
		}
	}
	cosine, sine := math.Cos(maximumAngle), math.Sin(maximumAngle)/tangentLength
	return sim.Position{
		X: float32(current[0]*cosine + tangent[0]*sine),
		Y: float32(current[1]*cosine + tangent[1]*sine),
		Z: float32(current[2]*cosine + tangent[2]*sine),
	}
}
