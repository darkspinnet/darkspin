package gameplay

// Called with the registry locked. Store the working owner before fan-out and
// reload it afterward so an outer command's later store cannot erase delivery.
// Account/squad updates stay on the explicit owner/eligible-party routes; only
// the filtered presentation enters the same-zone world stream.
func (e *gameplayPeerSession) publishCommittedPickupLocked(registry *gameplaySessionRegistry, ownerPackets [][]byte, partyPackets [][]byte) {
	if registry == nil {
		e.queueCampaignPackets(ownerPackets)
		return
	}
	ownerSessionKey := ""
	for sessionKey, member := range registry.sessions {
		if member.zone == e.zone && member.binding.UserID == e.binding.UserID &&
			member.generation == e.generation && member.transportGeneration == e.transportGeneration {
			ownerSessionKey = sessionKey
			registry.sessions[sessionKey] = *e
			break
		}
	}
	worldPackets := gameplayPeerPresentationPackets(ownerPackets)
	for sessionKey, member := range registry.sessions {
		if member.zone != e.zone {
			continue
		}
		packets := worldPackets
		if sessionKey == ownerSessionKey {
			packets = ownerPackets
		} else {
			if member.isRejoinPending || !member.isCampaignPresentationAvailable() {
				continue
			}
			if isActiveCoopPickupAlly(member, *e) {
				member.queueCampaignPackets(partyPackets)
			}
		}
		err := member.queueCampaignPresentation(packets)
		if err != nil && registry.logger != nil {
			registry.logger.Printf("RakNet committed pickup delivery retained user=%d: %v", member.binding.UserID, err)
		}
		registry.sessions[sessionKey] = member
	}
	if ownerSessionKey != "" {
		*e = registry.sessions[ownerSessionKey]
	} else {
		// A detached caller still retains its own accepted account correction.
		e.queueCampaignPackets(ownerPackets)
	}
}

func (e *gameplayPeerSession) queuePickupNoticeLocked(registry *gameplaySessionRegistry, packets [][]byte) {
	if registry == nil {
		e.queueCampaignPackets(packets)
		return
	}
	for sessionKey, member := range registry.sessions {
		if member.zone != e.zone || member.binding.UserID != e.binding.UserID ||
			member.generation != e.generation || member.transportGeneration != e.transportGeneration {
			continue
		}
		e.queueCampaignPackets(packets)
		registry.sessions[sessionKey] = *e
		return
	}
	e.queueCampaignPackets(packets)
}
