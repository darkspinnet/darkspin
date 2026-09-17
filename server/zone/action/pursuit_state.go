package action

import "sync"

type Pursuit struct {
	mu             sync.RWMutex
	generation     uint64
	sourceObjectID uint32
	targetObjectID uint32
	abilityIndex   uint32
	syncStamp      uint8
	stopDistance   float32
}

type PursuitSnapshot struct {
	Generation     uint64
	SourceObjectID uint32
	TargetObjectID uint32
	AbilityIndex   uint32
	SyncStamp      uint8
	StopDistance   float32
	IsActive       bool
}

func (p *Pursuit) Begin(
	sourceObjectID uint32, targetObjectID uint32, abilityIndex uint32, syncStamp uint8,
	stopDistance float32,
) uint64 {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.generation++
	p.sourceObjectID = sourceObjectID
	p.targetObjectID = targetObjectID
	p.abilityIndex = abilityIndex
	p.syncStamp = syncStamp
	p.stopDistance = stopDistance
	return p.generation
}

func (p *Pursuit) IsTarget(
	sourceObjectID uint32, targetObjectID uint32, abilityIndex uint32,
) bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return targetObjectID != 0 && p.isAbility(sourceObjectID, abilityIndex) &&
		p.targetObjectID == targetObjectID
}

func (p *Pursuit) IsAbility(sourceObjectID uint32, abilityIndex uint32) bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.isAbility(sourceObjectID, abilityIndex)
}

func (p *Pursuit) isAbility(sourceObjectID uint32, abilityIndex uint32) bool {
	return p.targetObjectID != 0 && p.sourceObjectID == sourceObjectID &&
		p.abilityIndex == abilityIndex
}

func (p *Pursuit) IsRetry(
	sourceObjectID uint32, targetObjectID uint32, abilityIndex uint32, _ uint8,
) bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.isAbility(sourceObjectID, abilityIndex) {
		return false
	}
	if targetObjectID == 0 {
		return true
	}
	return targetObjectID == p.targetObjectID
}

func (p *Pursuit) Cancel() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clear()
}

func (p *Pursuit) clear() {
	p.generation++
	p.sourceObjectID = 0
	p.targetObjectID = 0
	p.abilityIndex = 0
	p.syncStamp = 0
	p.stopDistance = 0
}

func (p *Pursuit) Expire(generation uint64) bool {
	if p == nil || generation == 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.generation != generation || p.targetObjectID == 0 {
		return false
	}
	p.clear()
	return true
}

func (p *Pursuit) Snapshot() PursuitSnapshot {
	if p == nil {
		return PursuitSnapshot{}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return PursuitSnapshot{
		Generation:     p.generation,
		SourceObjectID: p.sourceObjectID,
		TargetObjectID: p.targetObjectID,
		AbilityIndex:   p.abilityIndex,
		SyncStamp:      p.syncStamp,
		StopDistance:   p.stopDistance,
		IsActive:       p.targetObjectID != 0,
	}
}
