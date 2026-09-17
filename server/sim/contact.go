package sim

import (
	"errors"
	"fmt"
)

// SphereContactState retains one actor/trigger overlap pair across authoritative
// physics steps. The caller owns the cadence; movement commands alone are not
// treated as swept trigger reports.
type SphereContactState struct {
	IsOverlapping bool
}

// Advance evaluates one sphere-to-sphere contact step and maps the retained
// overlap transition to build-103 Lua trigger callback order.
func (s *SphereContactState) Advance(
	actorCenter Position, actorRadius float32, triggerCenter Position, triggerRadius float32,
) (LuaTriggerCallback, bool, error) {
	if s == nil {
		return "", false, errors.New("nil contact state")
	}
	if !isFinitePosition(actorCenter) || !isFinitePosition(triggerCenter) {
		return "", false, errors.New("non-finite contact position")
	}
	if actorRadius < 0 || triggerRadius <= 0 {
		return "", false, fmt.Errorf("contact radius: actor=%g trigger=%g", actorRadius, triggerRadius)
	}
	contactRadius := actorRadius + triggerRadius
	isOverlapping := isInsideSphere(actorCenter, triggerCenter, contactRadius)
	wasOverlapping := s.IsOverlapping
	s.IsOverlapping = isOverlapping
	switch {
	case !wasOverlapping && isOverlapping:
		return LuaTriggerEnter, true, nil
	case wasOverlapping && !isOverlapping:
		return LuaTriggerExit, true, nil
	case wasOverlapping && isOverlapping:
		return LuaTriggerStay, true, nil
	default:
		return "", false, nil
	}
}
