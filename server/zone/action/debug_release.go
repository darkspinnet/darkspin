//go:build scenario

package action

import "time"

func (e *ReleaseSession) ScenarioRemaining(now time.Time) (time.Duration, bool) {
	if e == nil || !e.mutex.TryRLock() {
		return 0, false
	}
	defer e.mutex.RUnlock()
	return max(time.Duration(0), e.end.Sub(now)), true
}
