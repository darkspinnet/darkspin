package ability

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
)

type Sequence struct {
	mu                     sync.RWMutex
	revision               uint64
	cooldownEnd            time.Time
	continueEnd            time.Time
	heldGeneration         uint64
	index                  int
	starterIndex           int
	isHeld                 bool
	isStarted              bool
	isFullSequenceComplete bool
}

type SequenceSnapshot struct {
	Revision               uint64
	CooldownEnd            time.Time
	ContinueEnd            time.Time
	HeldGeneration         uint64
	Index                  int
	StarterIndex           int
	IsHeld                 bool
	IsStarted              bool
	IsFullSequenceComplete bool
}

type Selection struct {
	AnimationIndex    int
	AnimationName     string
	ProjectileOffsets []sim.Position
	ProjectileAngles  []float32
	HitDelay          time.Duration
	HitDelays         []time.Duration
	ReleaseDelay      time.Duration
}

// SelectActivation projects a non-basic ability's single activation without
// mutating the basic combo sequence.
func SelectActivation(definition sim.AbilityDefinition) (Selection, error) {
	if definition.AnimationName == "" || definition.HitDelay < 0 ||
		definition.ReleaseDelay < 0 || definition.Cooldown < 0 {
		return Selection{}, errors.New("invalid active ability timing")
	}
	hitDelays := append([]time.Duration(nil), definition.HitDelays...)
	if len(hitDelays) == 0 {
		hitDelays = []time.Duration{definition.HitDelay}
	}
	selection := Selection{
		AnimationName: definition.AnimationName,
		HitDelay:      definition.HitDelay,
		HitDelays:     hitDelays,
		ReleaseDelay:  definition.ReleaseDelay,
	}
	selection, err := normalizeSelectionTimeline(selection)
	if err != nil {
		return Selection{}, fmt.Errorf("activeTimeline: %w", err)
	}
	return selection, nil
}

func normalizeSelectionTimeline(selection Selection) (Selection, error) {
	latestHitDelay := selection.HitDelay
	for _, hitDelay := range selection.HitDelays {
		if hitDelay < 0 {
			return Selection{}, errors.New("invalid ability hit timing")
		}
		latestHitDelay = max(latestHitDelay, hitDelay)
	}
	selection.ReleaseDelay = max(selection.ReleaseDelay, latestHitDelay)
	return selection, nil
}

func (s *Sequence) IsReady(now time.Time) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !now.Before(s.cooldownEnd)
}

func (s *Sequence) SetHeld(isHeld bool) uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heldGeneration++
	s.revision++
	s.isHeld = isHeld
	return s.heldGeneration
}

func (s *Sequence) ReleaseHeld() {
	s.SetHeld(false)
}

func (s *Sequence) IsHeldAt(generation uint64) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isHeld && s.heldGeneration == generation
}

func (s *Sequence) IsHeld() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isHeld
}

func (s *Sequence) HeldGeneration() uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.heldGeneration
}

func (s *Sequence) CooldownEnd() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cooldownEnd
}

func (s *Sequence) ContinueEnd() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.continueEnd
}

func (s *Sequence) Snapshot() SequenceSnapshot {
	if s == nil {
		return SequenceSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot()
}

func (s *Sequence) Revision() uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}

func (s *Sequence) snapshot() SequenceSnapshot {
	return SequenceSnapshot{
		Revision: s.revision, CooldownEnd: s.cooldownEnd, ContinueEnd: s.continueEnd,
		HeldGeneration: s.heldGeneration, Index: s.index, StarterIndex: s.starterIndex,
		IsHeld: s.isHeld, IsStarted: s.isStarted,
		IsFullSequenceComplete: s.isFullSequenceComplete,
	}
}

func (s *Sequence) Restore(snapshot SequenceSnapshot, expectedRevision uint64) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revision != expectedRevision {
		return false
	}
	s.cooldownEnd = snapshot.CooldownEnd
	s.continueEnd = snapshot.ContinueEnd
	s.heldGeneration = snapshot.HeldGeneration
	s.index = snapshot.Index
	s.starterIndex = snapshot.StarterIndex
	s.isHeld = snapshot.IsHeld
	s.isStarted = snapshot.IsStarted
	s.isFullSequenceComplete = snapshot.IsFullSequenceComplete
	s.revision++
	return true
}

func (s *Sequence) Accept(
	now time.Time, definition sim.AbilityDefinition,
) (Selection, error) {
	if s == nil {
		return Selection{}, errors.New("nil basic sequence")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sequenceLength := len(definition.AnimationNames)
	if sequenceLength == 0 || len(definition.HitDelays) != sequenceLength ||
		len(definition.ReleaseDelays) != sequenceLength || definition.Cooldown < 0 {
		return Selection{}, errors.New("invalid basic ability sequence")
	}
	if now.Before(s.cooldownEnd) {
		return Selection{}, fmt.Errorf("cooldown: %s", s.cooldownEnd.Sub(now))
	}
	if now.After(s.continueEnd) {
		if !s.isStarted || s.isFullSequenceComplete {
			s.starterIndex = 0
		} else {
			s.starterIndex = 1 - s.starterIndex
		}
		s.index = 0
		s.isStarted = true
		s.isFullSequenceComplete = false
	} else if s.isFullSequenceComplete {
		s.isFullSequenceComplete = false
	}
	sequenceIndex := s.index % sequenceLength
	if sequenceIndex < 2 && s.starterIndex == 1 {
		sequenceIndex = 1 - sequenceIndex
	}
	selection := Selection{
		AnimationIndex: sequenceIndex,
		AnimationName:  definition.AnimationNames[sequenceIndex],
		HitDelay:       definition.HitDelays[sequenceIndex],
		ReleaseDelay:   definition.ReleaseDelays[sequenceIndex],
	}
	if len(definition.AnimationProjectileOffsets) == sequenceLength {
		selection.ProjectileOffsets = append(
			[]sim.Position(nil),
			definition.AnimationProjectileOffsets[sequenceIndex]...,
		)
	}
	if len(definition.AnimationProjectileAngles) == sequenceLength {
		selection.ProjectileAngles = append(
			[]float32(nil),
			definition.AnimationProjectileAngles[sequenceIndex]...,
		)
	}
	if len(definition.AnimationHitDelays) == sequenceLength {
		selection.HitDelays = append(
			[]time.Duration(nil), definition.AnimationHitDelays[sequenceIndex]...,
		)
	}
	if len(selection.HitDelays) == 0 {
		selection.HitDelays = []time.Duration{selection.HitDelay}
	}
	selection, err := normalizeSelectionTimeline(selection)
	if err != nil {
		return Selection{}, fmt.Errorf("basicTimeline: %w", err)
	}
	s.index++
	if s.index == sequenceLength {
		s.index = 0
		s.starterIndex = 0
		s.isFullSequenceComplete = true
	}
	// Basic cooldowns begin when the input is accepted. HitDelay controls the
	// damage continuation and ReleaseDelay controls animation occupancy; neither
	// delays the start of the authored cooldown.
	s.cooldownEnd = now.Add(max(definition.Cooldown, selection.ReleaseDelay))
	s.continueEnd = s.cooldownEnd.Add(definition.Cooldown)
	s.revision++
	return selection, nil
}
