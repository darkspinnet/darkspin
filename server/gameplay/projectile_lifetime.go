package gameplay

// Released shots belong to their tracked projectile, not to the NPC's next
// animation, volley shot, or phase. Session teardown and source death still
// retire them, and prelaunch callbacks separately require the original cast.
func (e campaignNPCProjectileSchedule) isReleasedFlightCurrent(
	current gameplayPeerSession, isFound bool,
) bool {
	if !isFound || current.generation != e.generation || current.isZoneTerminal() ||
		current.zone == nil || current.zone.NPCs() == nil ||
		current.campaignNPCProjectiles[e.projectileObjectID] != e.run {
		return false
	}
	source, isSourceFound := current.zone.NPCs().NPC(e.sourceObjectID)
	return isSourceFound && !source.IsDefeated && source.HitPoint > 0
}
