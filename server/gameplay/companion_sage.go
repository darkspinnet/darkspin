package gameplay

import "github.com/darkspinnet/darkspin/server/util"

// sageCompanionObjectIDs selects the current deployment's live Dendrones before
// reserving combat state in the shared zone. The registry lock must be held.
func (e *gameplayPeerSession) sageCompanionObjectIDs(nounName string) []uint32 {
	if e == nil || e.sagePassive == nil || e.zone == nil ||
		e.zone.Companion() == nil || e.deployedObjectID == 0 || nounName == "" {
		return nil
	}
	noun := util.HashID(nounName)
	objectIDs := make([]uint32, 0, len(e.sagePassiveActivations))
	// Companion snapshots are sorted by object ID, so reservation order is stable.
	for _, actor := range e.zone.Companion().Snapshots() {
		if actor.UserID != e.binding.UserID || actor.PeerGeneration != e.generation ||
			actor.OwnerObjectID != e.deployedObjectID || actor.Noun != noun ||
			!actor.IsCombatant || actor.HitPoint <= 0 {
			continue
		}
		activation, isFound := e.sagePassiveActivations[actor.ObjectID]
		if !isFound || activation.ObjectID != actor.ObjectID ||
			activation.OwnerObjectID != e.deployedObjectID {
			continue
		}
		objectIDs = append(objectIDs, actor.ObjectID)
	}
	return objectIDs
}
