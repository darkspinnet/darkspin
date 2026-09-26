package action

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

type Motion struct {
	mu              sync.RWMutex
	movement        *sim.LinearMovement
	position        sim.Position
	startedAt       time.Time
	revision        uint64
	navigation      *navigation.Mesh
	footprintRadius float32
	projectionRange float32
}

type MotionSnapshot struct {
	movement        *sim.LinearMovement
	position        sim.Position
	startedAt       time.Time
	Revision        uint64
	projectionRange float32
}

func (e MotionSnapshot) Position() sim.Position {
	return e.position
}

func (e MotionSnapshot) StartedAt() time.Time {
	return e.startedAt
}

func (e MotionSnapshot) Movement() sim.LinearMovementSnapshot {
	if e.movement == nil {
		return sim.LinearMovementSnapshot{}
	}
	return e.movement.Snapshot()
}

func NewMotion(position sim.Position, startedAt time.Time) (*Motion, error) {
	movement, err := sim.NewLinearMovement(position, 0)
	if err != nil {
		return nil, fmt.Errorf("motionMovement: %w", err)
	}
	return &Motion{
		movement: movement, position: position, startedAt: startedAt,
		projectionRange: zonenavigation.ProjectionDistance,
	}, nil
}

func (m *Motion) Position() sim.Position {
	if m == nil {
		return sim.Position{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.position
}

// SamplePosition follows the retained path without consuming movement that
// command-driven encounter observers still need to process.
func (e *Motion) SamplePosition(now time.Time) (sim.Position, error) {
	e.mu.RLock()
	movement := e.movement.Clone()
	at := max(now.Sub(e.startedAt), movement.Snapshot().At)
	e.mu.RUnlock()
	position, err := movement.Position(at)
	if err != nil {
		return sim.Position{}, fmt.Errorf("motionSample: %w", err)
	}
	return position, nil
}

func (m *Motion) Revision() uint64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.revision
}

func (m *Motion) Velocity() sim.Position {
	if m == nil {
		return sim.Position{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.movement.Velocity()
}

func (m *Motion) Snapshot() MotionSnapshot {
	if m == nil {
		return MotionSnapshot{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot()
}

func (m *Motion) snapshot() MotionSnapshot {
	return MotionSnapshot{
		movement: m.movement.Clone(), position: m.position,
		startedAt: m.startedAt, Revision: m.revision,
		projectionRange: m.projectionRange,
	}
}

func (m *Motion) Restore(snapshot MotionSnapshot, expectedRevision uint64) (sim.Position, bool) {
	if m == nil {
		return sim.Position{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.revision != expectedRevision {
		return m.position, false
	}
	m.movement = snapshot.movement.Clone()
	m.position = snapshot.position
	m.startedAt = snapshot.startedAt
	m.projectionRange = snapshot.projectionRange
	m.revision++
	return m.position, true
}

func (m *Motion) Advance(
	now time.Time,
	reported sim.Position,
	goal sim.Position,
	isReported bool,
	isStop bool,
	correctionRange float32,
	speed float32,
) (sim.Position, sim.Position, error) {
	return m.advance(
		now, reported, goal, isReported, isStop, correctionRange, speed,
		zonenavigation.ProjectionDistance,
	)
}

// AdvancePursuit allows an NPC goal near the edge of authored navigation to
// use the same wider projection range as NPC pursuit while retaining a routed
// player path.
func (m *Motion) AdvancePursuit(
	now time.Time,
	reported sim.Position,
	goal sim.Position,
	isReported bool,
	isStop bool,
	correctionRange float32,
	speed float32,
) (sim.Position, sim.Position, error) {
	return m.advance(
		now, reported, goal, isReported, isStop, correctionRange, speed,
		pursuitProjectionDistance,
	)
}

func (m *Motion) advance(
	now time.Time,
	reported sim.Position,
	goal sim.Position,
	isReported bool,
	isStop bool,
	correctionRange float32,
	speed float32,
	projectionRange float32,
) (sim.Position, sim.Position, error) {
	if m == nil {
		return sim.Position{}, sim.Position{}, errors.New("nil motion")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := m.position
	elapsed, err := m.elapsed(now)
	if err != nil {
		return sim.Position{}, sim.Position{}, fmt.Errorf("advanceTime: %w", err)
	}
	movement := m.movement.Clone()
	position, err := movement.Position(elapsed)
	if err != nil {
		return sim.Position{}, sim.Position{}, fmt.Errorf("motionAdvance: %w", err)
	}
	if isReported {
		var isAccepted bool
		position, isAccepted, err = m.reconcilePosition(movement, elapsed, reported, correctionRange)
		if err != nil {
			return sim.Position{}, sim.Position{}, fmt.Errorf("motionReconcile: %w", err)
		}
		// Rejected observations retain the advanced authoritative position.
		if !isAccepted {
			position = movement.Snapshot().Position
		}
	}
	if isStop {
		position, err = movement.Stop(elapsed)
		if err != nil {
			return sim.Position{}, sim.Position{}, fmt.Errorf("motionStop: %w", err)
		}
	} else {
		position, err = m.setGoal(
			movement, elapsed, position, goal, speed, projectionRange,
		)
		if err != nil {
			return sim.Position{}, sim.Position{}, fmt.Errorf("motionGoal: %w", err)
		}
	}
	m.movement = movement
	m.position = position
	m.projectionRange = projectionRange
	m.revision++
	return previous, position, nil
}

func (m *Motion) AdvancePosition(
	now time.Time, reported sim.Position, isReported bool, correctionRange float32,
) (sim.Position, error) {
	if m == nil {
		return sim.Position{}, errors.New("nil motion")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	elapsed, err := m.elapsed(now)
	if err != nil {
		return sim.Position{}, fmt.Errorf("positionTime: %w", err)
	}
	movement := m.movement
	if isReported {
		movement = movement.Clone()
	}
	position, err := movement.Position(elapsed)
	if err != nil {
		return sim.Position{}, fmt.Errorf("motionPosition: %w", err)
	}
	if isReported {
		var isAccepted bool
		position, isAccepted, err = m.reconcilePosition(movement, elapsed, reported, correctionRange)
		if err != nil {
			return sim.Position{}, fmt.Errorf("motionReconcile: %w", err)
		}
		if isAccepted && movement.IsMoving() {
			snapshot := movement.Snapshot()
			projectionRange := m.projectionRange
			if projectionRange <= 0 {
				projectionRange = zonenavigation.ProjectionDistance
			}
			position, err = m.setGoal(
				movement, elapsed, position, snapshot.Goal, snapshot.Speed,
				projectionRange,
			)
			if err != nil {
				return sim.Position{}, fmt.Errorf("reconcilePath: %w", err)
			}
		}
	}
	m.movement = movement
	m.position = position
	m.revision++
	return position, nil
}

func (m *Motion) Stop(now time.Time) (sim.Position, error) {
	if m == nil {
		return sim.Position{}, errors.New("nil motion")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	elapsed, err := m.elapsed(now)
	if err != nil {
		return sim.Position{}, fmt.Errorf("stopTime: %w", err)
	}
	position, err := m.movement.Stop(elapsed)
	if err != nil {
		return sim.Position{}, fmt.Errorf("motionStop: %w", err)
	}
	m.position = position
	m.projectionRange = zonenavigation.ProjectionDistance
	m.revision++
	return position, nil
}

func (m *Motion) Teleport(now time.Time, destination sim.Position) error {
	if m == nil {
		return errors.New("nil motion")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	elapsed, err := m.elapsed(now)
	if err != nil {
		return fmt.Errorf("teleportTime: %w", err)
	}
	err = m.movement.Teleport(elapsed, destination)
	if err != nil {
		return fmt.Errorf("motionTeleport: %w", err)
	}
	m.position = destination
	m.projectionRange = zonenavigation.ProjectionDistance
	m.revision++
	return nil
}

func (m *Motion) elapsed(now time.Time) (time.Duration, error) {
	if m.movement == nil {
		return 0, errors.New("movement unavailable")
	}
	if now.Before(m.startedAt) {
		return 0, errors.New("movement clock regressed")
	}
	return now.Sub(m.startedAt), nil
}
