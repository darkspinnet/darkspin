package zone

import (
	"errors"
	"fmt"
	"math"
	"time"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// NPCProjectileAttack retains launch authorization after the NPC starts its
// next action. It can damage one living player-aligned object at collision.
type NPCProjectileAttack struct {
	zone           *Zone
	owner          zonenpc.ActionOwner
	sourceObjectID uint32
	isConsumed     bool
}

func (e *Zone) BeginNPCProjectile(
	owner zonenpc.ActionOwner, sourceObjectID uint32, actionGeneration uint64, at time.Time,
) (*NPCProjectileAttack, bool, error) {
	if e == nil || sourceObjectID == 0 || actionGeneration == 0 || owner.UserID == 0 || owner.PeerGeneration == 0 || at.IsZero() {
		return nil, false, errors.New("invalid projectile launch")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	member, isMemberFound := e.members[owner.UserID]
	if e.state != StateActive || !isMemberFound || member.PeerGeneration != owner.PeerGeneration {
		return nil, false, nil
	}
	source, isFound := e.info.NPCs.NPC(sourceObjectID)
	if !isFound || source.IsDefeated || source.HitPoint <= 0 || !source.IsActionStarted ||
		source.ActionOwner != owner || source.ActionGeneration != actionGeneration ||
		e.info.NPCs.StunRemaining(sourceObjectID, at) > 0 {
		return nil, false, nil
	}
	return &NPCProjectileAttack{zone: e, owner: owner, sourceObjectID: sourceObjectID}, true, nil
}

func (e *NPCProjectileAttack) Damage(targetObjectID uint32, amount float32) (NPCTargetDamage, bool, error) {
	if e == nil || e.zone == nil || targetObjectID == 0 || amount <= 0 ||
		math.IsNaN(float64(amount)) || math.IsInf(float64(amount), 0) {
		return NPCTargetDamage{}, false, errors.New("invalid projectile damage")
	}
	e.zone.mu.Lock()
	defer e.zone.mu.Unlock()
	member, isMemberFound := e.zone.members[e.owner.UserID]
	if e.isConsumed || e.zone.state != StateActive || !isMemberFound || member.PeerGeneration != e.owner.PeerGeneration {
		return NPCTargetDamage{}, false, nil
	}
	// Launch already authorized this projectile. The shooter dying must not
	// revoke an airborne shot; zone/member validity and single contact still apply.
	damage, isApplied, err := e.zone.applyNPCTargetDamage(e.owner, e.sourceObjectID, targetObjectID, amount)
	if err != nil {
		return NPCTargetDamage{}, false, fmt.Errorf("projectileCommit: %w", err)
	}
	e.isConsumed = isApplied
	return damage, isApplied, nil
}
