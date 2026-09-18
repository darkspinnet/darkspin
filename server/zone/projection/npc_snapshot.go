package projection

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

// SeedNPCs gives a new subscriber the already-published population. Joining
// the shared zone after its first spawn must not leave a client with only
// future movement and damage events for objects it has never created.
func (e *Session) SeedNPCs(subscriber Subscriber, snapshots []zonenpc.Snapshot) {
	e.mu.Lock()
	defer e.mu.Unlock()
	objectIDs, isFound := e.npcObjectIDsBySubscriber[subscriber]
	if !isFound {
		return
	}
	for _, snapshot := range snapshots {
		if !snapshot.IsPublished || snapshot.IsDefeated || snapshot.Plan.IsFixture {
			continue
		}
		if _, isKnown := objectIDs[snapshot.Plan.ObjectID]; isKnown {
			continue
		}
		objectIDs[snapshot.Plan.ObjectID] = struct{}{}
		snapshot.Plan = snapshot.Plan.Clone()
		e.next++
		e.enqueueLocked(subscriber, Event{
			Sequence: e.next, Kind: EventNPCSnapshot, NPCSnapshot: snapshot,
		})
	}
}
