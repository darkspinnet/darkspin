package npc

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

const maximumCorpseConsumerCount = 2

type ConsumeCorpseResult struct {
	Source        Snapshot
	HealedAmount  float32
	ModifierStack uint32
	ExpiresAt     time.Time
	IsFadeStarted bool
}

func (s *Session) ConsumableCorpseCandidates(
	sourceObjectID uint32, maximumRange float32,
) []Snapshot {
	if s == nil || sourceObjectID == 0 || maximumRange <= 0 ||
		math.IsNaN(float64(maximumRange)) || math.IsInf(float64(maximumRange), 0) {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, isFound := s.npcs[sourceObjectID]
	if !isFound || source.IsDefeated || !source.IsPublished {
		return nil
	}
	candidate := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		corpse := s.npcs[objectID]
		if objectID == sourceObjectID || corpse.Faction != source.Faction ||
			!corpse.IsDefeated || !corpse.IsPublished || corpse.Plan.IsFixture ||
			corpse.status.isCorpseFading ||
			corpse.status.corpseConsumerCount >= maximumCorpseConsumerCount ||
			zonegeometry.Distance(source.Plan.Position, corpse.Plan.Position) > maximumRange {
			continue
		}
		candidate = append(candidate, corpse)
	}
	slices.SortFunc(candidate, func(left Snapshot, right Snapshot) int {
		leftDistance := zonegeometry.Distance(source.Plan.Position, left.Plan.Position)
		rightDistance := zonegeometry.Distance(source.Plan.Position, right.Plan.Position)
		if leftDistance < rightDistance {
			return -1
		}
		if leftDistance > rightDistance {
			return 1
		}
		return cmp.Compare(left.Plan.ObjectID, right.Plan.ObjectID)
	})
	return candidate
}

func (s *Session) ClaimCorpse(sourceObjectID uint32, corpseObjectID uint32) error {
	if s == nil || sourceObjectID == 0 || corpseObjectID == 0 ||
		sourceObjectID == corpseObjectID {
		return errors.New("invalid corpse claim")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	source, isSourceFound := s.npcs[sourceObjectID]
	corpse, isCorpseFound := s.npcs[corpseObjectID]
	if !isSourceFound || source.IsDefeated || !source.IsPublished {
		return fmt.Errorf("corpseClaimSource: %d", sourceObjectID)
	}
	if !isCorpseFound || !corpse.IsDefeated || !corpse.IsPublished ||
		corpse.Plan.IsFixture || corpse.Faction != source.Faction ||
		corpse.status.isCorpseFading ||
		corpse.status.corpseConsumerCount >= maximumCorpseConsumerCount {
		return fmt.Errorf("corpseClaimTarget: %d", corpseObjectID)
	}
	corpse.status.corpseConsumerCount++
	s.npcs[corpseObjectID] = corpse
	return nil
}

func (s *Session) ReleaseCorpseClaim(corpseObjectID uint32) {
	if s == nil || corpseObjectID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	corpse, isFound := s.npcs[corpseObjectID]
	if !isFound || corpse.status.corpseConsumerCount == 0 {
		return
	}
	corpse.status.corpseConsumerCount--
	s.npcs[corpseObjectID] = corpse
}

func (s *Session) ClaimedCorpse(corpseObjectID uint32) (Snapshot, bool) {
	if s == nil || corpseObjectID == 0 {
		return Snapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	corpse, isFound := s.npcs[corpseObjectID]
	return corpse, isFound && corpse.IsDefeated && !corpse.Plan.IsFixture &&
		corpse.status.corpseConsumerCount > 0
}

func (s *Session) IsCorpseFading(corpseObjectID uint32) bool {
	if s == nil || corpseObjectID == 0 {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	corpse, isFound := s.npcs[corpseObjectID]
	return isFound && corpse.status.isCorpseFading
}

func (s *Session) StartCorpseFadePublication(corpseObjectID uint32) bool {
	if s == nil || corpseObjectID == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	corpse, isFound := s.npcs[corpseObjectID]
	if !isFound || !corpse.status.isCorpseFading ||
		corpse.status.isCorpseFadePublished {
		return false
	}
	corpse.status.isCorpseFadePublished = true
	s.npcs[corpseObjectID] = corpse
	return true
}

func (s *Session) ConsumeCorpse(
	sourceObjectID uint32, corpseObjectID uint32, healing float32,
	damageBuff float32, maximumStack uint32, duration time.Duration, at time.Time,
) (ConsumeCorpseResult, error) {
	if s == nil || sourceObjectID == 0 || corpseObjectID == 0 ||
		healing <= 0 || damageBuff <= 0 || maximumStack == 0 || duration <= 0 ||
		at.IsZero() {
		return ConsumeCorpseResult{}, errors.New("invalid corpse consumption")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	source, isSourceFound := s.npcs[sourceObjectID]
	corpse, isCorpseFound := s.npcs[corpseObjectID]
	if !isCorpseFound || !corpse.IsDefeated || corpse.Plan.IsFixture ||
		corpse.status.corpseConsumerCount == 0 {
		return ConsumeCorpseResult{}, fmt.Errorf("corpseConsumeTarget: %d", corpseObjectID)
	}
	corpse.status.corpseConsumerCount--
	if !isSourceFound || source.IsDefeated || !source.IsPublished ||
		source.HitPoint <= 0 || corpse.Faction != source.Faction {
		s.npcs[corpseObjectID] = corpse
		return ConsumeCorpseResult{}, fmt.Errorf("corpseConsumeSource: %d", sourceObjectID)
	}
	maximumHitPoint := source.Plan.NPCProfile.HitPoint
	if maximumHitPoint <= 0 {
		s.npcs[corpseObjectID] = corpse
		return ConsumeCorpseResult{}, fmt.Errorf("corpseConsumeMaximum: %d", sourceObjectID)
	}
	previousHitPoint := source.HitPoint
	if at.Before(source.status.healingReductionEnd) {
		healing *= 1 - source.status.healingReduction
	}
	source.HitPoint = min(maximumHitPoint, source.HitPoint+healing)
	if !at.Before(source.status.munchExpiresAt) {
		source.status.munchStack = 0
	}
	source.status.munchStack = min(maximumStack, source.status.munchStack+1)
	source.status.munchDamageIncrease = damageBuff * float32(source.status.munchStack)
	source.status.munchExpiresAt = at.Add(duration)
	isFadeStarted := !corpse.status.isCorpseFading
	corpse.status.isCorpseFading = true
	corpse.IsPublished = false
	s.npcs[sourceObjectID] = source
	s.npcs[corpseObjectID] = corpse
	return ConsumeCorpseResult{
		Source: source, HealedAmount: source.HitPoint - previousHitPoint,
		ModifierStack: source.status.munchStack,
		ExpiresAt:     source.status.munchExpiresAt, IsFadeStarted: isFadeStarted,
	}, nil
}

func (s *Session) ClearMunch(objectID uint32, expiresAt time.Time) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.munchExpiresAt != expiresAt {
		return
	}
	npc.status.munchExpiresAt = time.Time{}
	npc.status.munchDamageIncrease = 0
	npc.status.munchStack = 0
	s.npcs[objectID] = npc
}
