package security

import (
	"errors"
	"fmt"
	"sync"

	"github.com/darkspinnet/darkspin/server/game"
)

type Snapshot struct {
	ObjectID   [RouteCount]uint32
	RouteIndex int
	Presented  [RouteCount]bool
}

type Session struct {
	mutex                  sync.RWMutex
	objectID               [RouteCount]uint32
	routeIndex             int
	presented              [RouteCount]bool
	isPresentationReserved bool
	reservedRouteIndex     int
}

type MovementRequest struct {
	Previous         game.Vec3
	Current          game.Vec3
	Threats          []Threat
	IsTransferActive bool
}

type MovementDecision struct {
	RouteIndex     int
	ObjectID       uint32
	Teleport       Teleport
	IsActivation   bool
	IsDeactivation bool
	IsTeleport     bool
}

func NewSession(objectID [RouteCount]uint32) *Session {
	return &Session{objectID: objectID}
}

func (s *Session) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mutex.RLock()
	snapshot := Snapshot{
		ObjectID: s.objectID, RouteIndex: s.routeIndex,
		Presented: s.presented,
	}
	s.mutex.RUnlock()
	return snapshot
}

func ValidateSnapshot(snapshot Snapshot) error {
	isEmpty := snapshot.ObjectID == ([RouteCount]uint32{})
	if isEmpty {
		if snapshot.RouteIndex != 0 || snapshot.Presented != ([RouteCount]bool{}) {
			return errors.New("security snapshot empty state invalid")
		}
		return nil
	}
	objectIDs := make(map[uint32]struct{}, len(snapshot.ObjectID))
	for index, objectID := range snapshot.ObjectID {
		if objectID == 0 {
			return fmt.Errorf("security snapshot object[%d] unavailable", index)
		}
		if _, isDuplicate := objectIDs[objectID]; isDuplicate {
			return fmt.Errorf("security snapshot object[%d] duplicate", index)
		}
		objectIDs[objectID] = struct{}{}
	}
	if snapshot.RouteIndex < 0 || snapshot.RouteIndex > 4 {
		return errors.New("security snapshot route invalid")
	}
	return nil
}

// Restore applies only committed route and presentation state. An in-flight
// activation or transfer is intentionally discarded and may be retried from
// the restored client baseline.
func (e *Session) Restore(snapshot Snapshot) error {
	if e == nil {
		return errors.New("nil security session")
	}
	err := ValidateSnapshot(snapshot)
	if err != nil {
		return fmt.Errorf("securitySnapshot: %w", err)
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.objectID != snapshot.ObjectID {
		return errors.New("security snapshot object mismatch")
	}
	e.routeIndex = snapshot.RouteIndex
	e.presented = snapshot.Presented
	e.isPresentationReserved = false
	e.reservedRouteIndex = 0
	return nil
}

func (s *Session) Current() (int, uint32, bool) {
	snapshot := s.Snapshot()
	routeIndex := forwardRoute(snapshot.RouteIndex)
	if routeIndex < 0 || routeIndex >= len(snapshot.ObjectID) {
		return routeIndex, 0, false
	}
	objectID := snapshot.ObjectID[routeIndex]
	return routeIndex, objectID, objectID != 0
}

func (e *Session) ObserveMovement(
	req MovementRequest,
) (MovementDecision, error) {
	if e == nil {
		return MovementDecision{}, errors.New("nil security session")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	for _, routeIndex := range activeRoutes(e.routeIndex) {
		if routeIndex < 0 || routeIndex >= len(e.objectID) ||
			e.objectID[routeIndex] == 0 {
			continue
		}
		decision := MovementDecision{
			RouteIndex: routeIndex, ObjectID: e.objectID[routeIndex],
		}
		if e.presented[routeIndex] {
			teleport, isFound := Route(routeIndex)
			if isFound && HasThreat(teleport, req.Threats) {
				e.presented[routeIndex] = false
				decision.Teleport = teleport
				decision.IsDeactivation = true
				return decision, nil
			}
			if req.IsTransferActive {
				continue
			}
			teleport, isTeleport, err := PlanTeleport(
				req.Previous, req.Current, req.Threats, routeIndex,
			)
			if err != nil {
				return MovementDecision{}, fmt.Errorf("securityTeleport: %w", err)
			}
			if isTeleport {
				decision.Teleport = teleport
				decision.IsTeleport = true
				return decision, nil
			}
			continue
		}
		if e.isPresentationReserved {
			continue
		}
		teleport, isActivation, err := PlanActivation(
			req.Previous, req.Current, req.Threats, routeIndex,
		)
		if err != nil {
			return MovementDecision{}, fmt.Errorf("securityActivation: %w", err)
		}
		if !isActivation {
			continue
		}
		_, isTeleport, err := PlanTeleport(
			req.Previous, req.Current, req.Threats, routeIndex,
		)
		if err != nil {
			return MovementDecision{}, fmt.Errorf("securityContact: %w", err)
		}
		e.isPresentationReserved = true
		e.reservedRouteIndex = routeIndex
		decision.Teleport = teleport
		decision.IsActivation = true
		decision.IsTeleport = isTeleport
		return decision, nil
	}
	return MovementDecision{}, nil
}

// ReserveClearedPresentation activates the current teleporter as soon as its
// local threat cluster is defeated. Movement proximity is intentionally not
// part of floor-clear presentation.
func (e *Session) ReserveClearedPresentation(
	threats []Threat,
) (MovementDecision, error) {
	if e == nil {
		return MovementDecision{}, errors.New("nil security session")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	routeIndex := forwardRoute(e.routeIndex)
	if routeIndex < 0 || routeIndex >= len(e.objectID) ||
		e.objectID[routeIndex] == 0 || e.presented[routeIndex] ||
		e.isPresentationReserved {
		return MovementDecision{}, nil
	}
	teleport, isFound := Route(routeIndex)
	if !isFound || HasThreat(teleport, threats) {
		return MovementDecision{}, nil
	}
	e.isPresentationReserved = true
	e.reservedRouteIndex = routeIndex
	return MovementDecision{
		RouteIndex: routeIndex, ObjectID: e.objectID[routeIndex],
		Teleport: teleport, IsActivation: true,
	}, nil
}

func (e *Session) CommitPresentation(routeIndex int) error {
	if e == nil {
		return errors.New("nil security session")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if !e.isPresentationReserved ||
		e.reservedRouteIndex != routeIndex ||
		!isActiveRoute(e.routeIndex, routeIndex) {
		return errors.New("security presentation reservation unavailable")
	}
	e.presented[routeIndex] = true
	e.isPresentationReserved = false
	e.reservedRouteIndex = 0
	return nil
}

func (e *Session) CancelPresentation(routeIndex int) {
	if e == nil {
		return
	}
	e.mutex.Lock()
	if e.isPresentationReserved && e.reservedRouteIndex == routeIndex {
		e.isPresentationReserved = false
		e.reservedRouteIndex = 0
	}
	e.mutex.Unlock()
}

func (e *Session) RollbackDeactivation(routeIndex int) {
	if e == nil {
		return
	}
	e.mutex.Lock()
	if isActiveRoute(e.routeIndex, routeIndex) && routeIndex >= 0 &&
		routeIndex < len(e.objectID) && e.objectID[routeIndex] != 0 {
		e.presented[routeIndex] = true
	}
	e.mutex.Unlock()
}

func (s *Session) Advance(routeIndex int) error {
	if s == nil {
		return errors.New("nil security session")
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if !isActiveRoute(s.routeIndex, routeIndex) || routeIndex < 0 ||
		routeIndex >= len(s.objectID) || s.objectID[routeIndex] == 0 {
		return errors.New("security advance route unavailable")
	}
	s.routeIndex = destinationArea(routeIndex)
	return nil
}

func (s *Session) Complete(routeIndex int) error {
	if s == nil {
		return errors.New("nil security session")
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if !isActiveRoute(s.routeIndex, routeIndex) || routeIndex < 0 ||
		routeIndex >= len(s.objectID) || s.objectID[routeIndex] == 0 {
		return errors.New("security completion route unavailable")
	}
	s.routeIndex = destinationArea(routeIndex)
	s.presented[routeIndex] = true
	return nil
}

func activeRoutes(areaIndex int) []int {
	switch areaIndex {
	case 0:
		return []int{0}
	case 1:
		return []int{1, 2}
	case 2:
		return []int{3, 4}
	case 3:
		return []int{5, 6}
	default:
		return nil
	}
}

func forwardRoute(areaIndex int) int {
	switch areaIndex {
	case 0:
		return 0
	case 1:
		return 2
	case 2:
		return 4
	case 3:
		return 6
	default:
		return -1
	}
}

func isActiveRoute(areaIndex int, routeIndex int) bool {
	for _, activeRouteIndex := range activeRoutes(areaIndex) {
		if activeRouteIndex == routeIndex {
			return true
		}
	}
	return false
}

func destinationArea(routeIndex int) int {
	switch routeIndex {
	case 0, 3:
		return 1
	case 1:
		return 0
	case 2, 5:
		return 2
	case 4:
		return 3
	case 6:
		return 4
	default:
		return -1
	}
}
