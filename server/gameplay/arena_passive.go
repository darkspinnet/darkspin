package gameplay

import (
	"log"

	"github.com/darkspinnet/darkspin/server/game"
)

func (e *gameplayPeerSession) retireArenaKillPassive(creatureIndex uint32, objectID uint32) [][]byte {
	e.resetPassiveKill(creatureIndex)
	if e.soulRavagerOwnerObjectID != objectID {
		return nil
	}
	packets, err := e.clearSoulRavagerPresentation()
	if err != nil {
		log.Printf("Arena defeated soul presentation source=%d: %v", objectID, err)
	}
	return packets
}

// Call after the damage owner has saved its session copy. Delayed attacks
// retain their combat credit, but cannot grant a new hero the old hero's souls.
func (e *gameplaySessionRegistry) applyArenaKillPassiveLocked(source gameplayPeerSession, sourceObjectID uint32) {
	match := e.arenaMatches[source.binding.GameID]
	if source.binding.Mode != game.ModeArena || match == nil || !match.isCombatActive {
		return
	}
	for key, member := range e.sessions {
		if member.binding.GameID != source.binding.GameID || member.binding.UserID != source.binding.UserID ||
			member.binding.Mode != game.ModeArena || member.generation != source.generation ||
			member.transportGeneration != source.transportGeneration || member.squad != source.squad ||
			member.deployedObjectID != sourceObjectID || member.deployedHitPoint() <= 0 {
			continue
		}
		stack, isChanged := member.applyPassiveKill()
		if !isChanged {
			return
		}
		packets, err := member.syncSoulRavagerPresentation(e)
		e.sessions[key] = member
		if err != nil && e.logger != nil {
			e.logger.Printf("Arena kill passive presentation source=%d: %v", sourceObjectID, err)
		}
		for recipientKey, recipient := range e.sessions {
			if recipient.binding.Mode != game.ModeArena || recipient.binding.GameID != source.binding.GameID {
				continue
			}
			recipient.queuePackets(packets)
			e.sessions[recipientKey] = recipient
		}
		if e.logger != nil {
			e.logger.Printf("Arena kill passive source=%d stack=%d", sourceObjectID, stack)
		}
		return
	}
}
