package npc

import "time"

const recentDamageWindow = 10 * time.Second

type recentDamage struct {
	at               time.Time
	amount           float32
	isDamageOverTime bool
}

func (e *Snapshot) recordRecentDamage(now time.Time, amount float32, isDamageOverTime bool) {
	first := 0
	cutoff := now.Add(-recentDamageWindow)
	for first < len(e.recentDamages) && !e.recentDamages[first].at.After(cutoff) {
		first++
	}
	e.recentDamages = append(e.recentDamages[first:], recentDamage{at: now, amount: amount, isDamageOverTime: isDamageOverTime})
}

// RecentDamage returns health actually lost strictly after the ten-second cutoff.
// Shield absorption, immune hits and forced cleanup deaths do not contribute.
func (e *Session) RecentDamage(objectID uint32, now time.Time) float32 {
	return e.recentDamage(objectID, now, recentDamageWindow, true)
}

// IsOmicronRecentlyHit is build-103 RecentDamageCheck (chunk 775).
func (e *Session) IsOmicronRecentlyHit(objectID uint32, now time.Time) bool {
	return e.recentDamage(objectID, now, 3*time.Second, false) > 10
}

func (e *Session) recentDamage(objectID uint32, now time.Time, window time.Duration, isDamageOverTimeIncluded bool) float32 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	npc, isFound := e.npcs[objectID]
	if !isFound {
		return 0
	}
	cutoff := now.Add(-window)
	total := float32(0)
	for _, hit := range npc.recentDamages {
		if hit.at.After(cutoff) && !hit.at.After(now) && (isDamageOverTimeIncluded || !hit.isDamageOverTime) {
			total += hit.amount
		}
	}
	return total
}
