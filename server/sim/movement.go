package sim

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// LinearMovement advances constant-speed segments on one monotonic timeline.
// A navigation path retains its corners so observers never interpolate through
// an obstacle merely because the final goal is reachable.
type LinearMovement struct {
	position  Position
	goal      Position
	at        time.Duration
	speed     float32
	isMoving  bool
	waypoints []Position
}

// LinearMovementSnapshot is one immutable authoritative movement segment.
type LinearMovementSnapshot struct {
	Position Position
	Goal     Position
	Velocity Position
	At       time.Duration
	Speed    float32
	IsMoving bool
}

func NewLinearMovement(position Position, at time.Duration) (*LinearMovement, error) {
	if !isFinitePosition(position) {
		return nil, errors.New("non-finite movement position")
	}
	if at < 0 {
		return nil, errors.New("negative movement time")
	}
	return &LinearMovement{position: position, goal: position, at: at}, nil
}

func (m *LinearMovement) Position(at time.Duration) (Position, error) {
	if m == nil {
		return Position{}, errors.New("nil movement")
	}
	if at < m.at {
		return Position{}, errors.New("movement time regressed")
	}
	if !m.isMoving || at == m.at {
		m.at = at
		return m.position, nil
	}
	travel := m.speed * float32(at-m.at) / float32(time.Second)
	for m.isMoving {
		target := m.segmentGoal()
		delta := subtractPosition(target, m.position)
		distance := float32(math.Sqrt(float64(
			delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z,
		)))
		if distance > travel {
			m.position = addPosition(m.position, scalePosition(delta, travel/distance))
			break
		}
		m.position = target
		travel -= distance
		if len(m.waypoints) > 0 {
			m.waypoints = m.waypoints[1:]
		}
		m.isMoving = len(m.waypoints) > 0 || m.position != m.goal
	}
	m.at = at
	return m.position, nil
}

func (m *LinearMovement) SetGoal(at time.Duration, goal Position, speed float32) (Position, error) {
	if !isFinitePosition(goal) || math.IsNaN(float64(speed)) || math.IsInf(float64(speed), 0) || speed <= 0 {
		return Position{}, errors.New("invalid movement goal")
	}
	position, err := m.Position(at)
	if err != nil {
		return Position{}, fmt.Errorf("goalAdvance: %w", err)
	}
	m.goal = goal
	m.waypoints = nil
	m.speed = speed
	m.isMoving = position != goal
	return position, nil
}

// SetPath copies the remaining navigation corners, including the final goal.
func (e *LinearMovement) SetPath(at time.Duration, points []Position, speed float32) (Position, error) {
	if len(points) == 0 {
		return Position{}, errors.New("empty movement path")
	}
	for index, point := range points {
		if !isFinitePosition(point) {
			return Position{}, fmt.Errorf("pathPoint[%d]: non-finite", index)
		}
	}
	position, err := e.SetGoal(at, points[len(points)-1], speed)
	if err != nil {
		return Position{}, fmt.Errorf("pathGoal: %w", err)
	}
	e.waypoints = append([]Position(nil), points...)
	for len(e.waypoints) > 0 && e.waypoints[0] == position {
		e.waypoints = e.waypoints[1:]
	}
	e.isMoving = len(e.waypoints) > 0
	return position, nil
}

func (e *LinearMovement) segmentGoal() Position {
	if len(e.waypoints) > 0 {
		return e.waypoints[0]
	}
	return e.goal
}

// Clone keeps rollback snapshots independent of future path consumption.
func (e *LinearMovement) Clone() *LinearMovement {
	if e == nil {
		return nil
	}
	clone := *e
	clone.waypoints = append([]Position(nil), e.waypoints...)
	return &clone
}

func (m *LinearMovement) Stop(at time.Duration) (Position, error) {
	position, err := m.Position(at)
	if err != nil {
		return Position{}, fmt.Errorf("stopAdvance: %w", err)
	}
	m.goal = position
	m.waypoints = nil
	m.speed = 0
	m.isMoving = false
	return position, nil
}

func (m *LinearMovement) Teleport(at time.Duration, destination Position) error {
	if m == nil {
		return errors.New("nil movement")
	}
	if at < m.at {
		return errors.New("movement time regressed")
	}
	if !isFinitePosition(destination) {
		return errors.New("non-finite movement destination")
	}
	m.position = destination
	m.goal = destination
	m.waypoints = nil
	m.at = at
	m.speed = 0
	m.isMoving = false
	return nil
}

// Reconcile advances to at, then accepts a bounded observed pose without
// discarding the active movement goal. It lets a transport correct ordinary
// simulation/client drift without turning an action command into a teleport.
func (m *LinearMovement) Reconcile(
	at time.Duration, observed Position, maximumDistance float32,
) (Position, bool, error) {
	if !isFinitePosition(observed) || math.IsNaN(float64(maximumDistance)) ||
		math.IsInf(float64(maximumDistance), 0) || maximumDistance < 0 {
		return Position{}, false, errors.New("invalid movement reconciliation")
	}
	position, err := m.Position(at)
	if err != nil {
		return Position{}, false, fmt.Errorf("reconcileAdvance: %w", err)
	}
	delta := subtractPosition(observed, position)
	distanceSquared := delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z
	if distanceSquared > maximumDistance*maximumDistance {
		return position, false, nil
	}
	m.position = observed
	m.at = at
	if observed == m.goal {
		m.waypoints = nil
		m.speed = 0
		m.isMoving = false
	}
	return observed, true, nil
}

func (m *LinearMovement) IsMoving() bool {
	return m != nil && m.isMoving
}

func (m *LinearMovement) Snapshot() LinearMovementSnapshot {
	if m == nil {
		return LinearMovementSnapshot{}
	}
	return LinearMovementSnapshot{
		Position: m.position, Goal: m.goal, Velocity: m.Velocity(), At: m.at,
		Speed: m.speed, IsMoving: m.isMoving,
	}
}

func (m *LinearMovement) Velocity() Position {
	if m == nil || !m.isMoving {
		return Position{}
	}
	delta := subtractPosition(m.segmentGoal(), m.position)
	distance := float32(math.Sqrt(float64(
		delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z,
	)))
	if distance <= 0 {
		return Position{}
	}
	scale := m.speed / distance
	return scalePosition(delta, scale)
}
