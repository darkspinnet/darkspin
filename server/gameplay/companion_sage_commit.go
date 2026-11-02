package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
)

type sagePassivePublicationCommit struct {
	runtime    gameplaySwitchRuntime
	sessionKey string
	generation uint64
	poll       *sagePassivePoll
}

func (e sagePassivePublicationCommit) commit() {
	e.runtime.commitSagePassive(e.sessionKey, e.generation, e.poll)
}

func (e gameplaySwitchRuntime) armSageAfterResponse(packet raknet.Packet, sessionKey string, generation uint64, poll *sagePassivePoll) error {
	commit := sagePassivePublicationCommit{runtime: e, sessionKey: sessionKey, generation: generation, poll: poll}
	err := packet.AfterResponseCommit(commit.commit)
	if err != nil {
		return fmt.Errorf("sageResponseCommit: %w", err)
	}
	return nil
}

// Scheduled switch/death-selection packets have their own commit boundary.
// Queue the owner's published hero presentation to allies before arming the
// pet world clock. These producers fence their captured identity in produce.
func (e *gameplaySwitchArrivalStep) commit() {
	if len(e.publishedPackets) == 0 {
		return
	}
	recipientCount := e.runtime.registry.queuePeerPresentation(e.identity, e.publishedPackets)
	e.runtime.logger.Printf("RakNet character arrival committed owner=%d peers=%d", e.targetObjectID, recipientCount)
	e.runtime.commitSagePassive(e.sessionKey, e.generation, e.sagePoll)
}

func (e *gameplayDeathSelectionStep) commit() {
	if len(e.publishedPackets) == 0 {
		return
	}
	recipientCount := e.runtime.registry.queuePeerPresentation(e.identity, e.publishedPackets)
	e.runtime.logger.Printf("RakNet death selection committed remote=%s peers=%d", e.sessionKey, recipientCount)
	e.runtime.commitSagePassive(e.sessionKey, e.generation, e.sagePoll)
}
