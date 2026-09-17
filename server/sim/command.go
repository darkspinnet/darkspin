package sim

import "math"

// MovementCommand is a trusted world movement submitted after transport actor
// binding has succeeded. It carries no protocol-specific vector type.
type MovementCommand struct {
	ActorRole Role
	From      Position
	To        Position
}

// MarkerEnteredCommand is an internal scenario command produced by trusted
// world geometry. Clients never supply marker or level identity directly.
type MarkerEnteredCommand struct {
	LevelID   int64
	MarkerID  uint32
	ActorRole Role
}

// FirstAggroCommand is produced by trusted AI perception geometry after the
// transport actor and target have been resolved to stable simulation roles.
type FirstAggroCommand struct {
	ActorRole  Role
	TargetRole Role
}

// PerceptionTrigger describes one AI object's authoritative aggro perimeter.
type PerceptionTrigger struct {
	TargetRole Role
	Center     Position
	Radius     float32
}

// MarkerTrigger contains immutable authored geometry and execution authority.
type MarkerTrigger struct {
	LevelID           int64
	MarkerID          uint32
	Center            Position
	Radius            float32
	IsTriggerOnceOnly bool
	IsServerOnly      bool
}

// BoxMarkerTrigger contains immutable authored oriented-box geometry and
// execution authority. HalfExtent is expressed in the box's local axes.
type BoxMarkerTrigger struct {
	LevelID           int64
	MarkerID          uint32
	Center            Position
	HalfExtent        Position
	YawRadians        float32
	IsTriggerOnceOnly bool
	IsServerOnly      bool
}

// MarkerEntered evaluates a swept authoritative movement against one authored
// spherical marker. Scenario phase and once-only state remain director-owned.
func MarkerEntered(movement MovementCommand, trigger MarkerTrigger) (MarkerEnteredCommand, bool) {
	if movement.ActorRole == "" || trigger.LevelID <= 0 || trigger.MarkerID == 0 || trigger.Radius <= 0 {
		return MarkerEnteredCommand{}, false
	}
	if !isFinitePosition(movement.From) || !isFinitePosition(movement.To) || !isFinitePosition(trigger.Center) {
		return MarkerEnteredCommand{}, false
	}
	delta := Position{
		X: movement.To.X - movement.From.X,
		Y: movement.To.Y - movement.From.Y,
		Z: movement.To.Z - movement.From.Z,
	}
	lengthSquared := delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z
	if lengthSquared == 0 {
		if !isInsideSphere(movement.From, trigger.Center, trigger.Radius) {
			return MarkerEnteredCommand{}, false
		}
		return markerEnteredCommand(movement, trigger), true
	}
	toCenter := Position{
		X: trigger.Center.X - movement.From.X,
		Y: trigger.Center.Y - movement.From.Y,
		Z: trigger.Center.Z - movement.From.Z,
	}
	fraction := (toCenter.X*delta.X + toCenter.Y*delta.Y + toCenter.Z*delta.Z) / lengthSquared
	fraction = max(float32(0), min(float32(1), fraction))
	closest := Position{
		X: movement.From.X + delta.X*fraction,
		Y: movement.From.Y + delta.Y*fraction,
		Z: movement.From.Z + delta.Z*fraction,
	}
	if !isInsideSphere(closest, trigger.Center, trigger.Radius) {
		return MarkerEnteredCommand{}, false
	}
	return markerEnteredCommand(movement, trigger), true
}

// BoxMarkerEntered evaluates swept authoritative movement against one authored
// oriented box expanded by the actor footprint. Phase and once-only state stay
// director-owned.
func BoxMarkerEntered(
	movement MovementCommand, trigger BoxMarkerTrigger, actorRadius float32,
) (MarkerEnteredCommand, bool) {
	if movement.ActorRole == "" || trigger.LevelID <= 0 || trigger.MarkerID == 0 || actorRadius < 0 {
		return MarkerEnteredCommand{}, false
	}
	if !isFinitePosition(movement.From) || !isFinitePosition(movement.To) ||
		!isFinitePosition(trigger.Center) || !isFinitePosition(trigger.HalfExtent) ||
		math.IsNaN(float64(trigger.YawRadians)) || math.IsInf(float64(trigger.YawRadians), 0) ||
		trigger.HalfExtent.X <= 0 || trigger.HalfExtent.Y <= 0 || trigger.HalfExtent.Z <= 0 {
		return MarkerEnteredCommand{}, false
	}
	start := boxLocalPosition(movement.From, trigger)
	end := boxLocalPosition(movement.To, trigger)
	extent := Position{
		X: trigger.HalfExtent.X + actorRadius,
		Y: trigger.HalfExtent.Y + actorRadius,
		Z: trigger.HalfExtent.Z + actorRadius,
	}
	minimumFraction := float32(0)
	maximumFraction := float32(1)
	for _, axis := range [][3]float32{
		{start.X, end.X, extent.X},
		{start.Y, end.Y, extent.Y},
		{start.Z, end.Z, extent.Z},
	} {
		delta := axis[1] - axis[0]
		if math.Abs(float64(delta)) < 0.000001 {
			if math.Abs(float64(axis[0])) > float64(axis[2]) {
				return MarkerEnteredCommand{}, false
			}
			continue
		}
		firstFraction := (-axis[2] - axis[0]) / delta
		secondFraction := (axis[2] - axis[0]) / delta
		if firstFraction > secondFraction {
			firstFraction, secondFraction = secondFraction, firstFraction
		}
		minimumFraction = max(minimumFraction, firstFraction)
		maximumFraction = min(maximumFraction, secondFraction)
		if minimumFraction > maximumFraction {
			return MarkerEnteredCommand{}, false
		}
	}
	return MarkerEnteredCommand{
		LevelID: trigger.LevelID, MarkerID: trigger.MarkerID, ActorRole: movement.ActorRole,
	}, true
}

// FirstAggroEntered converts swept movement through an AI perception sphere
// into a protocol-neutral command. Phase policy and one-shot state remain
// director-owned.
func FirstAggroEntered(movement MovementCommand, trigger PerceptionTrigger) (FirstAggroCommand, bool) {
	if movement.ActorRole == "" || trigger.TargetRole == "" || trigger.Radius <= 0 {
		return FirstAggroCommand{}, false
	}
	marker := MarkerTrigger{
		LevelID: 1, MarkerID: 1, Center: trigger.Center, Radius: trigger.Radius,
	}
	_, isEntered := MarkerEntered(movement, marker)
	if !isEntered {
		return FirstAggroCommand{}, false
	}
	return FirstAggroCommand{ActorRole: movement.ActorRole, TargetRole: trigger.TargetRole}, true
}

func markerEnteredCommand(movement MovementCommand, trigger MarkerTrigger) MarkerEnteredCommand {
	return MarkerEnteredCommand{
		LevelID: trigger.LevelID, MarkerID: trigger.MarkerID, ActorRole: movement.ActorRole,
	}
}

func boxLocalPosition(position Position, trigger BoxMarkerTrigger) Position {
	cosYaw := float32(math.Cos(float64(trigger.YawRadians)))
	sinYaw := float32(math.Sin(float64(trigger.YawRadians)))
	x := position.X - trigger.Center.X
	y := position.Y - trigger.Center.Y
	return Position{
		X: x*cosYaw + y*sinYaw,
		Y: -x*sinYaw + y*cosYaw,
		Z: position.Z - trigger.Center.Z,
	}
}

func isInsideSphere(position Position, center Position, radius float32) bool {
	x := position.X - center.X
	y := position.Y - center.Y
	z := position.Z - center.Z
	return x*x+y*y+z*z <= radius*radius
}

func isFinitePosition(position Position) bool {
	return !math.IsNaN(float64(position.X)) && !math.IsInf(float64(position.X), 0) &&
		!math.IsNaN(float64(position.Y)) && !math.IsInf(float64(position.Y), 0) &&
		!math.IsNaN(float64(position.Z)) && !math.IsInf(float64(position.Z), 0)
}
