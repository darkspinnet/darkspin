package gameplay

import (
	"fmt"
	"log"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// stopCampaignTreeOfLife cancels the run and retires its object exactly once.
// Keep the identity on marshal failure so cleanup can be retried. A retained
// object still needs removal even if its schedule has already been cleared.
func (e *gameplayPeerSession) stopCampaignTreeOfLife() ([][]byte, error) {
	if e == nil {
		return nil, nil
	}
	if e.treeOfLifeRun != nil {
		e.treeOfLifeRun.Stop()
	}
	if e.zone != nil && e.zone.TreeOfLife() != nil {
		e.zone.TreeOfLife().Retire(e.treeOfLifeObjectID)
	}
	var packets [][]byte
	if e.treeOfLifeObjectID != 0 {
		packet, err := raknet.MarshalApplication(raknet.ObjectDeleteMessage{
			ObjectID: []uint32{e.treeOfLifeObjectID},
		})
		if err != nil {
			return nil, fmt.Errorf("treeDeleteMarshal: %w", err)
		}
		packets = [][]byte{packet}
	}
	e.treeOfLifeRun = nil
	e.treeOfLifeObjectID = 0
	return packets, nil
}

// retireCampaignTreeOfLifeLocked runs before membership or session ownership
// changes. Queue against the original zone while the registry is locked, so
// departure cannot cancel an ally's reliable delivery or redirect it elsewhere.
// The caller retains or removes the updated caster session after this operation.
func (e *gameplaySessionRegistry) retireCampaignTreeOfLifeLocked(caster *gameplayPeerSession) {
	packets, err := caster.stopCampaignTreeOfLife()
	if err != nil {
		log.Printf("RakNet Tree of Life retirement failed game=%d user=%d generation=%d: %v",
			caster.binding.GameID, caster.binding.UserID, caster.generation, err)
		return
	}
	if len(packets) == 0 || caster.zone == nil {
		return
	}
	e.queueCampaignTreeRemovalLocked(caster, packets)
}

func (e *gameplaySessionRegistry) queueCampaignTreeRemovalLocked(caster *gameplayPeerSession, packets [][]byte) {
	caster.queueCampaignPackets(packets)
	for sessionKey, candidate := range e.sessions {
		if candidate.zone != caster.zone || candidate.binding.GameID != caster.binding.GameID ||
			(candidate.binding.UserID == caster.binding.UserID && candidate.generation == caster.generation) {
			continue
		}
		candidate.queueCampaignPackets(packets)
		e.sessions[sessionKey] = candidate
	}
}
