package gameplay

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

// Retire while the original zone and its connected recipients remain owned by
// the registry. The detached copy's later runtime cleanup then has no work.
func (e *gameplaySessionRegistry) retireHeroInfectionsLocked(caster *gameplayPeerSession) {
	packets := caster.takeHeroInfections()
	if len(packets) == 0 || caster.zone == nil {
		return
	}
	caster.queueCampaignPackets(packets)
	for sessionKey, candidate := range e.sessions {
		if candidate.zone != caster.zone ||
			(candidate.binding.UserID == caster.binding.UserID && candidate.generation == caster.generation) {
			continue
		}
		candidate.queueCampaignPackets(packets)
		e.sessions[sessionKey] = candidate
	}
}

// BioEntangle owns its deletion independently from the disease. Purge and
// death clear exactly the tracked instance before an expiry producer can run.
func (e *gameplaySessionRegistry) retireTargetInfectionRootsLocked(npc *zonenpc.Session, objectID uint32) [][]byte {
	entries := make([]heroInfectionEntry, 0)
	for _, member := range e.sessions {
		if member.zone == nil || member.zone.NPCs() != npc {
			continue
		}
		for run := range member.heroInfections {
			entry, isRemoved := run.expireRoot(objectID)
			if isRemoved {
				entries = append(entries, entry)
			}
		}
	}
	return infectionDeletionPackets(entries)
}

type heroInfectionRootStep struct {
	schedule   heroInfectionSchedule
	objectID   uint32
	instanceID uint32
}

func (e heroInfectionRootStep) produce() ([][]byte, error) {
	registry := e.schedule.runtime.registry
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	member, isFound := registry.sessions[e.schedule.sessionKey]
	if !e.schedule.isCurrent(member, isFound) || !e.schedule.run.isAccepted {
		return nil, nil
	}
	entry, isRemoved := e.schedule.run.expireRootInstance(e.objectID, e.instanceID)
	if !isRemoved {
		return nil, nil
	}
	return infectionDeletionPackets([]heroInfectionEntry{entry}), nil
}
