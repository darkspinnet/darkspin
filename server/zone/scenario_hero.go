//go:build scenario

package zone

import (
	"errors"
	"fmt"
	"log"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// RetireScenarioHero removes a living fixture actor from combat without death
// progression. Its retained hero revision fences already prepared damage.
func (e *Zone) RetireScenarioHero(userID, peerGeneration uint64, objectID uint32, timestamp uint64) ([]zonenpc.Snapshot, []zonenpc.Snapshot, error) {
	if e == nil {
		return nil, nil, errors.New("nil campaign zone")
	}
	e.mu.Lock()
	if e.state != StateActive {
		e.mu.Unlock()
		return nil, nil, errors.New("campaign zone not active")
	}
	member, isFound := e.members[userID]
	if !isFound || member.PeerGeneration != peerGeneration || !member.IsConnected {
		e.mu.Unlock()
		return nil, nil, errors.New("campaign member unavailable")
	}
	err := e.info.Hero.RetireForScenario(userID, peerGeneration, objectID)
	if err != nil {
		e.mu.Unlock()
		return nil, nil, fmt.Errorf("retireActor: %w", err)
	}
	released, err := e.info.NPCs.ReleaseTarget(objectID)
	if err != nil {
		e.mu.Unlock()
		return nil, nil, fmt.Errorf("retireTarget: %w", err)
	}
	acquired, acquireErr := e.info.NPCs.AcquireTargets(e.livePlayerAlignedTargets(), true)
	returning := e.returnUnacquiredNPCs(released, acquired)
	timeline := e.info.Timeline
	e.mu.Unlock()
	for _, npc := range released {
		timeline.Cancel(zonenpc.FirstActionTimelineKey(npc.Plan.ObjectID))
		if !npc.IsActionStarted {
			continue
		}
		e.PublishNPCAction(zonenpc.ActionEvent{
			Kind: zonenpc.ActionEventCancel,
			Plan: zonenpc.FirstActionPlan{ObjectID: npc.Plan.ObjectID,
				ActionGeneration: npc.ActionGeneration, SourcePosition: npc.Plan.Position},
			Timestamp: timestamp,
		}, userID, peerGeneration)
	}
	e.publishNPCReturns(returning)
	if acquireErr != nil {
		// The old actor is already retired. Arrival retries target acquisition
		// after the replacement is committed, so keep the two-phase swap alive.
		log.Printf("Scenario hero NPC target refresh deferred: %v", acquireErr)
	}
	return acquired, released, nil
}
