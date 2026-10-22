package ability

import (
	"errors"
	"math"
	"sort"
	"sync"
	"time"
)

// ScaleRemaining follows the native cooldown operation's millisecond delta.
// Release admission is independent and remains the floor for the current cast.
func (e *CooldownSession) ScaleRemaining(at time.Time, scale float32, releaseEnds ...time.Time) ([]CooldownSnapshot, error) {
	if e == nil || at.IsZero() || math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) || scale < 0 {
		return nil, errors.New("cooldown scale invalid")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	snapshots := make([]CooldownSnapshot, 0)
	releaseRemaining := time.Duration(0)
	for _, releaseEnd := range releaseEnds {
		releaseRemaining = max(releaseRemaining, releaseEnd.Sub(at))
	}
	for key, current := range e.entries {
		remaining := current.end.Sub(at)
		if remaining <= releaseRemaining {
			continue
		}
		delta := time.Duration(int64(float32(scale-1)*float32(remaining/time.Millisecond))) * time.Millisecond
		if delta == 0 {
			continue
		}
		current.end = current.end.Add(delta)
		current.revision++
		e.entries[key] = current
		isHeroAbility := key&heroAbilityCooldownMask != 0
		abilityID := uint32(0)
		if isHeroAbility {
			abilityID = uint32(key & ^heroAbilityCooldownMask)
		}
		snapshots = append(snapshots, CooldownSnapshot{Key: key, AbilityID: abilityID, End: current.end, Revision: current.revision, IsHeroAbility: isHeroAbility})
	}
	sort.Slice(snapshots, func(left int, right int) bool { return snapshots[left].Key < snapshots[right].Key })
	return snapshots, nil
}

type CooldownKey uint64

const heroAbilityCooldownMask CooldownKey = 1 << 32

const CooldownProjectileBasic CooldownKey = 1

// HeroAbilityCooldown gives every authored ability identity an independent
// cooldown key without expanding a hard-coded enum for every hero kit.
func HeroAbilityCooldown(abilityID uint32) CooldownKey {
	if abilityID == 0 {
		return 0
	}
	return heroAbilityCooldownMask | CooldownKey(abilityID)
}

type cooldownEntry struct {
	end      time.Time
	revision uint64
}

type CooldownReservation struct {
	key             CooldownKey
	previousEnd     time.Time
	currentEnd      time.Time
	currentRevision uint64
}

// CooldownSession owns per-participant ability cooldown admission. A
// reservation can be rolled back only while it is still the latest mutation,
// preventing an old schedule failure from clearing a newer accepted action.
type CooldownSession struct {
	mutex   sync.RWMutex
	entries map[CooldownKey]cooldownEntry
}

type CooldownSnapshot struct {
	Key           CooldownKey
	AbilityID     uint32
	End           time.Time
	Revision      uint64
	IsHeroAbility bool
}

func NewCooldownSession() *CooldownSession {
	return &CooldownSession{entries: make(map[CooldownKey]cooldownEntry)}
}

func (s *CooldownSession) IsReady(key CooldownKey, now time.Time) bool {
	if s == nil || key == 0 {
		return false
	}
	s.mutex.RLock()
	current := s.entries[key]
	s.mutex.RUnlock()
	return !now.Before(current.end)
}

func (s *CooldownSession) Reserve(
	key CooldownKey, now time.Time, duration time.Duration,
) (CooldownReservation, bool) {
	if s == nil || key == 0 || now.IsZero() || duration < 0 {
		return CooldownReservation{}, false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	current := s.entries[key]
	if now.Before(current.end) {
		return CooldownReservation{}, false
	}
	next := cooldownEntry{
		end: now.Add(duration), revision: current.revision + 1,
	}
	s.entries[key] = next
	return CooldownReservation{
		key: key, previousEnd: current.end,
		currentEnd: next.end, currentRevision: next.revision,
	}, true
}

func (s *CooldownSession) Rollback(reservation CooldownReservation) bool {
	if s == nil || reservation.key == 0 ||
		reservation.currentRevision == 0 {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	current := s.entries[reservation.key]
	if current.revision != reservation.currentRevision ||
		!current.end.Equal(reservation.currentEnd) {
		return false
	}
	s.entries[reservation.key] = cooldownEntry{
		end: reservation.previousEnd, revision: current.revision + 1,
	}
	return true
}

func (s *CooldownSession) Reset(key CooldownKey) bool {
	if s == nil || key == 0 {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	current := s.entries[key]
	isChanged := !current.end.IsZero()
	current.end = time.Time{}
	current.revision++
	s.entries[key] = current
	return isChanged
}

// ResetAll clears every named and content-derived cooldown while invalidating
// outstanding reservations. It is used only by explicit recovery flows.
func (s *CooldownSession) ResetAll() bool {
	if s == nil {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	isChanged := false
	for key, current := range s.entries {
		isChanged = isChanged || !current.end.IsZero()
		current.end = time.Time{}
		current.revision++
		s.entries[key] = current
	}
	return isChanged
}

func (s *CooldownSession) Snapshots() []CooldownSnapshot {
	if s == nil {
		return nil
	}
	s.mutex.RLock()
	snapshots := make([]CooldownSnapshot, 0, len(s.entries))
	for key, entry := range s.entries {
		isHeroAbility := key&heroAbilityCooldownMask != 0
		abilityID := uint32(0)
		if isHeroAbility {
			abilityID = uint32(key & ^heroAbilityCooldownMask)
		}
		snapshots = append(snapshots, CooldownSnapshot{
			Key: key, AbilityID: abilityID, End: entry.end,
			Revision: entry.revision, IsHeroAbility: isHeroAbility,
		})
	}
	s.mutex.RUnlock()
	sort.Slice(snapshots, func(left int, right int) bool {
		return snapshots[left].Key < snapshots[right].Key
	})
	return snapshots
}
