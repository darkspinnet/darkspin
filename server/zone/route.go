package zone

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
)

// RouteMovement is the transport-neutral result of constraining movement
// against an authored zone route. The wire adapter decides how to present a
// gate contact to each connected member.
type RouteMovement struct {
	Current     game.Vec3
	GateContact zonehorde.GateContact
	IsAuthored  bool
}

func (e *Zone) ConstrainRoute(
	previous game.Vec3, current game.Vec3,
) (RouteMovement, error) {
	result := RouteMovement{Current: current}
	if e == nil {
		return result, errors.New("zone route unavailable")
	}
	e.mu.RLock()
	directorDefinition := e.info.DirectorDefinition
	director := e.info.Director
	horde := e.info.Horde
	e.mu.RUnlock()
	if director == nil {
		return result, nil
	}
	result.IsAuthored = true
	var err error
	if horde != nil {
		result.Current, result.GateContact, err = horde.ConstrainMovement(
			directorDefinition, previous, current,
		)
		if err != nil {
			return result, fmt.Errorf("routeGate: %w", err)
		}
		if result.GateContact.MarkerID != 0 {
			return result, nil
		}
	}
	return result, nil
}

func (e *Zone) AdvanceDirector(
	previous game.Vec3, current game.Vec3,
) ([]game.CampaignDirectorPublication, error) {
	if e == nil {
		return nil, errors.New("zone director unavailable")
	}
	e.mu.RLock()
	director := e.info.Director
	e.mu.RUnlock()
	if director == nil {
		return nil, nil
	}
	publications, err := director.Advance(previous, current)
	if err != nil {
		return nil, fmt.Errorf("routeDirector: %w", err)
	}
	return publications, nil
}

func (e *Zone) CanAcceptPublication(
	publication game.CampaignDirectorPublication,
) error {
	if e == nil {
		return errors.New("zone director unavailable")
	}
	e.mu.RLock()
	director := e.info.Director
	e.mu.RUnlock()
	if director == nil {
		return errors.New("zone director unavailable")
	}
	err := director.CanAccept(publication)
	if err != nil {
		return fmt.Errorf("publicationCheck: %w", err)
	}
	return nil
}

func (e *Zone) AcceptPublication(
	publication game.CampaignDirectorPublication,
) error {
	if e == nil {
		return errors.New("zone director unavailable")
	}
	e.mu.RLock()
	director := e.info.Director
	e.mu.RUnlock()
	if director == nil {
		return errors.New("zone director unavailable")
	}
	err := director.Accept(publication)
	if err != nil {
		return fmt.Errorf("publicationAccept: %w", err)
	}
	return nil
}

func (e *Zone) prepareNamedEvent(
	markerSetOrdinal int, sourceObjectID uint32, eventName string,
) (game.CampaignDirectorNamedEventPublication, error) {
	if e == nil {
		return game.CampaignDirectorNamedEventPublication{},
			errors.New("zone director unavailable")
	}
	e.mu.RLock()
	director := e.info.Director
	e.mu.RUnlock()
	if director == nil {
		return game.CampaignDirectorNamedEventPublication{},
			errors.New("zone director unavailable")
	}
	publication, err := director.PrepareNamedEvent(
		markerSetOrdinal, sourceObjectID, eventName,
	)
	if err != nil {
		return game.CampaignDirectorNamedEventPublication{},
			fmt.Errorf("namedEventPrepare: %w", err)
	}
	return publication, nil
}
