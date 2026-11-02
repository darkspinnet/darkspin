package gameplay

import "log"

// Full recovery cancels the expiry job. Queue the same modifier removal now
// so the client cannot retain a permanent Ghostform tooltip after recovery.
func (e *gameplayPeerSession) retireGhostForm(modifierInstancePool *modifierPool) {
	if e == nil || e.ghostFormRun == nil {
		return
	}
	run := e.ghostFormRun
	e.ghostFormRun = nil
	run.Cancel()
	packets, err := run.CleanupPackets()
	if err != nil {
		log.Printf("RakNet Ghost Form recovery cleanup failed user=%d instance=%d: %v",
			e.binding.UserID, run.InstanceID(), err)
	}
	e.queueCampaignPackets(packets)
	if modifierInstancePool == nil {
		return
	}
	err = modifierInstancePool.Release(run.InstanceID())
	if err != nil {
		log.Printf("RakNet Ghost Form recovery modifier release failed user=%d instance=%d: %v",
			e.binding.UserID, run.InstanceID(), err)
	}
}
