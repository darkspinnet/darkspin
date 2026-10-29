package zone

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type CryosGeyser struct {
	Targets         []NPCTarget
	IsQueryCaptured bool
	ObjectID        uint32
	MarkerID        uint32
	NounName        string
	Position        game.Vec3
	WarningAt       time.Time
	SpoutAt         time.Time
	EndsAt          time.Time
	Cycle           uint64
}

func (e *Zone) CaptureCryosGeyserTargets(objectID uint32, cycle uint64, targets []NPCTarget) []NPCTarget {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, isFound := e.cryosGeysers[objectID]
	if !isFound || state.Cycle != cycle {
		return nil
	}
	if !state.IsQueryCaptured {
		state.Targets = append([]NPCTarget(nil), targets...)
		state.IsQueryCaptured = true
		e.cryosGeysers[objectID] = state
	}
	return append([]NPCTarget(nil), state.Targets...)
}

// The zone owns the coroutine clock across party members and transports.
// Publication receipts remain connection-local; death is not destruction.
func (e *Zone) CryosGeysers(at time.Time) ([]CryosGeyser, error) {
	if e == nil || at.IsZero() {
		return nil, errors.New("geyser world clock unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != StateActive {
		return nil, nil
	}
	sources := e.cryosGeyserSourcesLocked()
	if e.cryosGeysers == nil {
		e.cryosGeysers = make(map[uint32]CryosGeyser)
	}
	for objectID := range e.cryosGeysers {
		if _, isFound := sources[objectID]; !isFound {
			delete(e.cryosGeysers, objectID)
		}
	}
	objectIDs := make([]uint32, 0, len(sources))
	for objectID := range sources {
		objectIDs = append(objectIDs, objectID)
	}
	sort.Slice(objectIDs, func(left, right int) bool { return objectIDs[left] < objectIDs[right] })
	states := make([]CryosGeyser, 0, len(sources))
	for _, objectID := range objectIDs {
		source := sources[objectID]
		state, isFound := e.cryosGeysers[objectID]
		start := at
		if isFound {
			start = state.EndsAt
		}
		for !isFound || !at.Before(state.EndsAt) {
			rest, err := sim.ScriptRandom(e.info.NPCRandom, 4, 7)
			if err != nil {
				return nil, fmt.Errorf("geyserRest: %w", err)
			}
			e.nextCryosGeyserCycle++
			if e.nextCryosGeyserCycle == 0 {
				e.nextCryosGeyserCycle++
			}
			state = CryosGeyser{ObjectID: objectID, MarkerID: source.MarkerID, NounName: source.NounName, Position: source.Position,
				WarningAt: start.Add(time.Duration(rest * float64(time.Second))), Cycle: e.nextCryosGeyserCycle}
			state.SpoutAt = state.WarningAt.Add(2 * time.Second)
			state.EndsAt = state.SpoutAt.Add(time.Second)
			start, isFound = state.EndsAt, true
		}
		e.cryosGeysers[objectID] = state
		states = append(states, state)
	}
	return states, nil
}

func (e *Zone) cryosGeyserSourcesLocked() map[uint32]CryosGeyser {
	sources := make(map[uint32]CryosGeyser)
	seenObjectIDs := make(map[uint32]bool)
	for _, source := range e.info.NPCs.Snapshots() {
		hook := game.CryosGeyserHook(source.Plan.NounName, source.Plan.NPCProfile)
		if hook == "" {
			continue
		}
		seenObjectIDs[source.Plan.ObjectID] = true
		if !source.IsPublished || (hook == "CryosGeyserDeath" && !source.IsDefeated) {
			continue
		}
		sources[source.Plan.ObjectID] = CryosGeyser{ObjectID: source.Plan.ObjectID, MarkerID: source.Plan.LocusID, NounName: source.Plan.NounName, Position: source.Plan.Position}
	}
	director := e.info.DirectorDefinition
	if !director.IsInitialLayoutSelected {
		return sources
	}
	for _, set := range director.MarkerSets {
		for _, candidate := range set.Definitions {
			marker, isSelected := director.SelectedMarkerDefinition(candidate.MarkerID)
			if !isSelected || !marker.IsVisible || seenObjectIDs[marker.MarkerID] {
				continue
			}
			profile := director.FixtureProfilesByNoun[strings.ToLower(marker.NounName)]
			if game.CryosGeyserHook(marker.NounName, profile) != "CryosGeyserPassive" {
				continue
			}
			sources[marker.MarkerID] = CryosGeyser{ObjectID: marker.MarkerID, MarkerID: marker.MarkerID, NounName: marker.NounName, Position: marker.Position}
		}
	}
	return sources
}

// Observation never advances a coroutine or consumes the simulator random.
func (e *Zone) CryosGeyserSnapshot() []CryosGeyser {
	e.mu.RLock()
	defer e.mu.RUnlock()
	states := make([]CryosGeyser, 0, len(e.cryosGeysers))
	sources := e.cryosGeyserSourcesLocked()
	for _, state := range e.cryosGeysers {
		if _, isFound := sources[state.ObjectID]; !isFound {
			continue
		}
		states = append(states, state)
	}
	sort.Slice(states, func(left, right int) bool { return states[left].ObjectID < states[right].ObjectID })
	return states
}

// Actual published fixture or selected scenery owns admission. Lua46 passes
// kObjIDNone to TakeDamage, so the resulting combat event has source zero.
func (e *Zone) ApplyCryosGeyserDamage(owner zonenpc.ActionOwner, sourceObjectID, targetObjectID uint32, damage float32, at time.Time) (NPCTargetDamage, bool, error) {
	if e == nil || owner.UserID == 0 || owner.PeerGeneration == 0 || sourceObjectID == 0 || targetObjectID == 0 ||
		damage <= 0 || math.IsNaN(float64(damage)) || math.IsInf(float64(damage), 0) {
		return NPCTargetDamage{}, false, errors.New("invalid geyser damage")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	member, isFound := e.members[owner.UserID]
	state, isActive := e.cryosGeysers[sourceObjectID]
	_, isSource := e.cryosGeyserSourcesLocked()[sourceObjectID]
	if e.state != StateActive || !isFound || member.PeerGeneration != owner.PeerGeneration || !isActive || !isSource || at.Before(state.SpoutAt) || !at.Before(state.EndsAt) {
		return NPCTargetDamage{}, false, nil
	}
	result, isApplied, err := e.applyNPCTargetDamage(owner, 0, targetObjectID, damage)
	if err != nil {
		return result, isApplied, fmt.Errorf("geyserTarget: %w", err)
	}
	return result, isApplied, nil
}
