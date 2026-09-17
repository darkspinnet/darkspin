package developer

import (
	"errors"
	"math"
)

type EventKind uint8

const (
	EventReset EventKind = iota + 1
	EventGoto
	EventBossStart
	EventBossComplete
	EventSecurityNext
	EventRecap
)

type Position struct {
	X float32
	Y float32
	Z float32
}

type EventCommand struct {
	Name     string
	Position Position
}

type EventPlan struct {
	Kind     EventKind
	Position Position
}

func PlanEvent(command EventCommand) (EventPlan, error) {
	switch command.Name {
	case "reset":
		return EventPlan{Kind: EventReset}, nil
	case "goto":
		if !isFinite(command.Position.X) ||
			!isFinite(command.Position.Y) ||
			!isFinite(command.Position.Z) {
			return EventPlan{}, errors.New("invalid goto position")
		}
		return EventPlan{Kind: EventGoto, Position: command.Position}, nil
	case "boss-start":
		return EventPlan{Kind: EventBossStart}, nil
	case "boss-complete":
		return EventPlan{Kind: EventBossComplete}, nil
	case "security-next":
		return EventPlan{Kind: EventSecurityNext}, nil
	case "recap":
		return EventPlan{Kind: EventRecap}, nil
	default:
		return EventPlan{}, errors.New("event name unavailable")
	}
}

func isFinite(number float32) bool {
	return !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0)
}
