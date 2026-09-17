package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (r campaignDamageRuntime) applyScaldronThornoReflection(
	sessionKey string, generation uint64, sourceObjectID uint32,
	timestamp uint64, result zoneability.AreaResult,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if r.registry == nil || r.npc.registry == nil {
		return nil, sporenet.PlayerStatDelta{},
			errors.New("Thorno reflection runtime unavailable")
	}
	if sourceObjectID == 0 || result.Damage.Damage <= 0 ||
		result.Damage.IsDamageImmune || result.Definition.DescriptorMask&1 == 0 {
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(result.Snapshot.Plan)
	if !isProfileFound || profile.PassiveMeleeDamageReflection <= 0 {
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, sourceObjectID,
	)
	if !isTargetFound || target.HitPoint <= 0 {
		r.registry.mutex.Unlock()
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	reflectionProfile := zonenpc.ActionProfile{
		AbilityName:      "ScaldronBasicThorno_ThornsPassive",
		DamageSource:     0,
		ImpactEffectName: "life_common_melee_hit.ServerEventDef",
	}
	plan := zonenpc.AttackPlan{
		SourceObjectID: result.Snapshot.Plan.ObjectID,
		TargetObjectID: sourceObjectID,
		SourcePosition: result.Snapshot.Plan.Position,
		TargetPosition: target.Position,
		Profile:        reflectionProfile,
	}
	reflectedDamage := result.Damage.Damage * profile.PassiveMeleeDamageReflection
	packets, statDelta, _, err := r.npc.applyEnemyReflectedDamage(
		&peerSession, generation, plan,
		zonenpc.AttackResult{Damage: reflectedDamage}, timestamp,
	)
	if err == nil {
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("thornoReflection: %w", err)
	}
	return packets, statDelta, nil
}
