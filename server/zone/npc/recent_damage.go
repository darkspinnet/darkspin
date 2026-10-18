package npc

import "time"

const recentDamageWindow = 10 * time.Second

type recentDamage struct {
	at     time.Time
	amount float32
}

func (e *Snapshot) recordRecentDamage(now time.Time, amount float32) {
	first := 0
	cutoff := now.Add(-recentDamageWindow)
	for first < len(e.recentDamages) && e.recentDamages[first].at.Before(cutoff) {
		first++
	}
	e.recentDamages = append(e.recentDamages[first:], recentDamage{at: now, amount: amount})
}

// RecentDamage returns health actually lost in the preceding ten seconds.
// Shield absorption, immune hits and forced cleanup deaths do not contribute.
func (e *Session) RecentDamage(objectID uint32, now time.Time) float32 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	npc, isFound := e.npcs[objectID]
	if !isFound {
		return 0
	}
	cutoff := now.Add(-recentDamageWindow)
	total := float32(0)
	for _, hit := range npc.recentDamages {
		if !hit.at.Before(cutoff) && !hit.at.After(now) {
			total += hit.amount
		}
	}
	return total
}
