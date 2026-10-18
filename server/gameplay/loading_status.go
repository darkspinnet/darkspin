package gameplay

// A full LabsPlayer update contains all three character reflections, which
// trigger native asset loading. Subsequent loading progress must use sparse
// status fields rather than replay those character records to every peer.
type campaignInitialPlayerDelivery struct {
	registry            *gameplaySessionRegistry
	sessionKey          string
	generation          uint64
	transportGeneration uint64
}

func (e campaignInitialPlayerDelivery) commit() {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	member, isFound := e.registry.sessions[e.sessionKey]
	if !isFound || member.generation != e.generation ||
		member.transportGeneration != e.transportGeneration {
		return
	}
	member.isCampaignPlayerInitialized = true
	e.registry.sessions[e.sessionKey] = member
}
