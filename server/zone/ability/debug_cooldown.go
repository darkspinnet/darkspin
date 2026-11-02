//go:build scenario

package ability

import "time"

func (e *CooldownSession) ScenarioRemaining(key CooldownKey, now time.Time) (time.Duration, bool) {
	if e == nil || !e.mutex.TryRLock() {
		return 0, false
	}
	defer e.mutex.RUnlock()
	return max(time.Duration(0), e.entries[key].end.Sub(now)), true
}
