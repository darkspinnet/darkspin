package chat

import "time"

const remoteBugReportCooldown = 2 * time.Minute

// Reserve before gathering gameplay context or queueing a ZIP. The cooldown
// is shared by remote players and covers concurrent requests and failed
// attempts, so repeated capture errors cannot overload the host either.
func (e *Service) reserveRemoteBugReport() bool {
	e.remoteBugReportMu.Lock()
	defer e.remoteBugReportMu.Unlock()
	now := e.now()
	if now.Before(e.nextRemoteBugReportAt) {
		return false
	}
	e.nextRemoteBugReportAt = now.Add(remoteBugReportCooldown)
	return true
}
