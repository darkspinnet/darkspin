package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
)

// AuraUtils IsFriend admits same-team combatants, including hero-owned pets.
// Their identities and death transitions belong to the pet, not its owner.
func voodooCompanionActors(member gameplayPeerSession, now time.Time) []voodooActor {
	if member.zone == nil || member.zone.Companion() == nil {
		return nil
	}
	actors := make([]voodooActor, 0)
	for _, companion := range member.zone.Companion().Snapshots() {
		if !isPartyCompanionOwner(companion, member) ||
			!companion.IsCombatant || !companion.IsTargetable {
			continue
		}
		key := voodooActorIdentity(member)
		key.objectID = companion.ObjectID
		actors = append(actors, voodooActor{
			key: key, position: companion.PositionAt(now), member: member,
			hitPoint: companion.HitPoint, deathRevision: companion.DeathRevision,
			isCompanion: true,
		})
	}
	return actors
}

// The child restores its receiving agent. A pet kill must never credit its
// owner's resource pool. Pet mana is not represented by the companion runtime;
// the authored health branch is independent of its mana branch.
// The caller holds the gameplay registry lock.
func (e campaignDamageRuntime) applyVoodooCompanionKillLocked(
	member gameplayPeerSession, sourceObjectID uint32,
) ([][]byte, error) {
	if member.zone == nil || member.zone.Companion() == nil {
		return nil, nil
	}
	companion, isFound := member.zone.Companion().Snapshot(sourceObjectID)
	if !isFound || !isPartyCompanionOwner(companion, member) ||
		!companion.IsCombatant || !companion.IsTargetable || companion.HitPoint <= 0 {
		return nil, nil
	}
	key := voodooActorIdentity(member)
	key.objectID = companion.ObjectID
	charm := e.registry.voodooCharmActorReturn(key)
	if charm.healthRatio <= 0 {
		return nil, nil
	}
	healing := companion.MaximumHitPoint * charm.healthRatio
	hitPoint := min(companion.MaximumHitPoint, companion.HitPoint+healing)
	if hitPoint <= companion.HitPoint {
		return nil, nil
	}
	integerChange := int32(hitPoint) - int32(companion.HitPoint)
	healingPacket, err := raknet.MarshalApplication(raknet.DamageCombatEventMessage{
		// Native HealDamage reports its requested heal before the HP cap;
		// the signed integer field separately reports the actual HP change.
		Flags: 0x0002, DeltaHealth: -healing,
		TargetID: companion.ObjectID, SourceID: 0, IntegerHPChange: -integerChange,
	})
	if err != nil {
		return nil, fmt.Errorf("charmHealing: %w", err)
	}
	resourcePacket, err := raknet.MarshalApplication(raknet.CombatantDataDeltaMessage{
		ObjectID: companion.ObjectID, HitPoints: hitPoint, IsHitPointChanged: true,
	})
	if err != nil {
		return nil, fmt.Errorf("charmResource: %w", err)
	}
	isRestored, err := member.zone.Companion().RestoreHitPoint(companion, hitPoint)
	if err != nil {
		return nil, fmt.Errorf("charmCommit: %w", err)
	}
	if !isRestored {
		return nil, nil
	}
	return [][]byte{healingPacket, resourcePacket}, nil
}
