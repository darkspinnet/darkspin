package quest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/zone/objective"
)

const CrystalNoun = "loot_questCrystal.Noun"
const PickupAbility = "PickUpQuestCrystal"
const PickupRange = float32(2)
const CastTime = 300000011 * time.Nanosecond
const ReleaseTime = time.Second

// State retains accepted emissions, including collected identities. A replayed
// objective outbox entry must never recreate a previously collected crystal.
type State struct {
	Request       objective.DropRequest
	ObjectID      uint32
	Destination   sim.Position
	FlightElapsed time.Duration
	IsCollected   bool
	IsPublished   bool
}

type Crystal struct {
	State
	Flight sim.DropFlight
}

type Claim struct {
	ObjectID   uint32
	Generation uint64
}

type Progress interface {
	RecordQuestCrystalFound(context.Context) error
}

type Session struct {
	mu         sync.Mutex
	crystals   map[uint64]Crystal
	claims     map[uint32]Claim
	generation uint64
}

func NewSession() *Session {
	return &Session{crystals: make(map[uint64]Crystal), claims: make(map[uint32]Claim)}
}

func (e *Session) Request(id uint64) (Crystal, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	crystal, isFound := e.crystals[id]
	return crystal, isFound
}

func (e *Session) Crystal(objectID uint32, at time.Time) (Crystal, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, crystal := range e.crystals {
		if crystal.ObjectID == objectID && !crystal.IsCollected {
			crystal.Flight = crystal.Flight.At(at)
			crystal.FlightElapsed = min(500*time.Millisecond, max(time.Duration(0), at.Sub(crystal.Flight.StartedAt)))
			return crystal, true
		}
	}
	return Crystal{}, false
}

func (e *Session) Admit(req objective.DropRequest, objectID uint32, destination sim.Position, at time.Time, timestamp uint64) (Crystal, error) {
	if objectID == 0 || at.IsZero() {
		return Crystal{}, errors.New("quest crystal identity unavailable")
	}
	lob, err := sim.BuildDropLob(time.Duration(timestamp)*time.Millisecond, req.SourcePosition, destination)
	if err != nil {
		return Crystal{}, fmt.Errorf("questLob: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if crystal, isFound := e.crystals[req.ID]; isFound {
		return crystal, nil
	}
	crystal := Crystal{State: State{Request: req, ObjectID: objectID, Destination: destination},
		Flight: sim.DropFlight{Source: req.SourcePosition, Destination: destination, Lob: lob,
			StartedAt: at, Position: req.SourcePosition, MovementType: 4}}
	e.crystals[req.ID] = crystal
	return crystal, nil
}

func (e *Session) Claim(objectID uint32) (Claim, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, isClaimed := e.claims[objectID]; isClaimed {
		return Claim{}, false
	}
	for _, crystal := range e.crystals {
		if crystal.ObjectID != objectID || crystal.IsCollected {
			continue
		}
		e.generation++
		if e.generation == 0 {
			e.generation = 1
		}
		claim := Claim{ObjectID: objectID, Generation: e.generation}
		e.claims[objectID] = claim
		return claim, true
	}
	return Claim{}, false
}

func (e *Session) Release(claim Claim) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.claims[claim.ObjectID] == claim {
		delete(e.claims, claim.ObjectID)
	}
}

func (e *Session) Publish(requestID uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	crystal, isFound := e.crystals[requestID]
	if isFound {
		crystal.IsPublished = true
		e.crystals[requestID] = crystal
	}
}

func (e *Session) Collect(ctx context.Context, claim Claim, progress Progress) (bool, error) {
	if ctx == nil || progress == nil || claim.Generation == 0 {
		return false, errors.New("quest collection context unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.claims[claim.ObjectID] != claim {
		return false, nil
	}
	for id, crystal := range e.crystals {
		if crystal.ObjectID != claim.ObjectID || crystal.IsCollected {
			continue
		}
		err := progress.RecordQuestCrystalFound(ctx)
		if err != nil {
			return false, fmt.Errorf("questProgress: %w", err)
		}
		crystal.IsCollected = true
		e.crystals[id] = crystal
		delete(e.claims, claim.ObjectID)
		return true, nil
	}
	return false, nil
}

func (e *Session) Snapshots(at time.Time) []State {
	e.mu.Lock()
	defer e.mu.Unlock()
	states := make([]State, 0, len(e.crystals))
	for _, crystal := range e.crystals {
		state := crystal.State
		state.FlightElapsed = min(500*time.Millisecond, max(time.Duration(0), at.Sub(crystal.Flight.StartedAt)))
		states = append(states, state)
	}
	sort.Slice(states, func(left, right int) bool { return states[left].Request.ID < states[right].Request.ID })
	return states
}

func Validate(states []State) error {
	requests := make(map[uint64]bool)
	objects := make(map[uint32]bool)
	for _, state := range states {
		if state.Request.ID == 0 || state.Request.ObjectiveID != objective.LootCrystalsID ||
			state.ObjectID == 0 || requests[state.Request.ID] || objects[state.ObjectID] ||
			state.FlightElapsed < 0 || state.FlightElapsed > 500*time.Millisecond {
			return errors.New("quest crystal checkpoint invalid")
		}
		lob, err := sim.BuildDropLob(0, state.Request.SourcePosition, state.Destination)
		if err != nil {
			return fmt.Errorf("questPosition: %w", err)
		}
		if lob.Duration <= 0 {
			return errors.New("quest crystal flight unavailable")
		}
		requests[state.Request.ID] = true
		objects[state.ObjectID] = true
	}
	return nil
}

func (e *Session) Restore(states []State, at time.Time) error {
	err := Validate(states)
	if err != nil {
		return fmt.Errorf("questValidate: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.crystals) != 0 {
		return errors.New("quest crystal restore requires empty session")
	}
	for _, state := range states {
		lob, lobErr := sim.BuildDropLob(0, state.Request.SourcePosition, state.Destination)
		if lobErr != nil {
			return fmt.Errorf("questRestoreLob: %w", lobErr)
		}
		flight := sim.DropFlight{Source: state.Request.SourcePosition, Destination: state.Destination,
			Lob: lob, StartedAt: at.Add(-state.FlightElapsed), Position: state.Request.SourcePosition, MovementType: 4}
		e.crystals[state.Request.ID] = Crystal{State: state, Flight: flight.At(at)}
	}
	return nil
}
