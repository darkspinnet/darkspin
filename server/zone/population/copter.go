package population

import (
	"errors"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
)

func isCopterPlan(plan zonenpc.SpawnPlan) bool {
	profile, isFound := zonenpc.ActionProfileForNoun(plan.NounName)
	return isFound && profile.AbilityName == "ScaldronBasicCopter_Passive"
}

// Requested encounter policy: a selected Hover Bot needs a nearby partner.
// The client supports linked pairs but does not provide a recovered minimum
// population count. Preserve the selected species/rank and the encounter locus.
func pairCopterSpawns(plans []zonenpc.SpawnPlan, nextObjectID uint32) ([]zonenpc.SpawnPlan, uint32, error) {
	initialCount := len(plans)
	for index := 0; index < initialCount; index++ {
		source := plans[index]
		if !isCopterPlan(source) {
			continue
		}
		isPaired := false
		for _, candidate := range plans {
			if candidate.ObjectID != source.ObjectID && isCopterPlan(candidate) &&
				candidate.LocusID == source.LocusID &&
				candidate.Position.Sub(source.Position).Length() <= 15 {
				isPaired = true
				break
			}
		}
		if isPaired {
			continue
		}
		if nextObjectID >= zoneobject.ProjectileIDStart {
			return nil, nextObjectID, errors.New("copter partner object ID exhausted")
		}
		partner := source.Clone()
		partner.ObjectID = nextObjectID
		// A separate placement avoids a coincident, zero-length laser. Normal
		// spawn admission still projects the actor footprint onto navigation.
		partner.Position.X += max(float32(2), source.ActorFootprintRadius()*2+0.1)
		plans = append(plans, partner)
		nextObjectID++
	}
	return plans, nextObjectID, nil
}
