package zone

import (
	"errors"
	"strings"
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// BeginTripMineProjectile authorizes the authored fixture request at launch.
// This is not an NPC AI action: its passive issues independent abilities.
func (e *Zone) BeginTripMineProjectile(owner zonenpc.ActionOwner, sourceObjectID uint32, at time.Time) (*NPCProjectileAttack, bool, error) {
	if e == nil || owner.UserID == 0 || owner.PeerGeneration == 0 || sourceObjectID == 0 || at.IsZero() {
		return nil, false, errors.New("invalid trip mine launch")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	member, isMemberFound := e.members[owner.UserID]
	if e.state != StateActive || !isMemberFound || member.PeerGeneration != owner.PeerGeneration {
		return nil, false, nil
	}
	source, isFound := e.info.NPCs.NPC(sourceObjectID)
	if !isFound || !source.Plan.IsFixture || !strings.EqualFold(source.Plan.NounName, "DEST_prefab_tnx_object_tripmine.Noun") ||
		!source.IsPublished || source.IsDefeated || source.HitPoint <= 0 {
		return nil, false, nil
	}
	return &NPCProjectileAttack{zone: e, owner: owner, sourceObjectID: sourceObjectID}, true, nil
}
