package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sporenet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// applyAffixThorns delivers the TookDamage reflection from the affix after
// the triggering melee hit. The dedicated reflected-damage path permits a
// lethal melee hit to trigger Thorns and retains hero and companion defenses.
func (r campaignDamageRuntime) applyAffixThorns(
	sessionKey string, generation uint64, timestamp uint64,
	reflection *zonenpc.ThornsDamage,
) ([][]byte, sporenet.PlayerStatDelta, error) {
	if reflection == nil {
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	if r.registry == nil || r.npc.registry == nil {
		return nil, sporenet.PlayerStatDelta{}, errors.New("affix Thorns runtime unavailable")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(reflection.SourceObjectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, reflection.TargetObjectID,
	)
	if !isSourceFound || !isTargetFound || target.HitPoint <= 0 {
		r.registry.mutex.Unlock()
		return nil, sporenet.PlayerStatDelta{}, nil
	}
	profile := zonenpc.ActionProfile{
		AbilityName:          "Thorns_NPCAffixModifier",
		DamageType:           uint8(reflection.DamageType),
		DamageSource:         uint8(reflection.DamageSource),
		DescriptorMask:       reflection.DescriptorMask,
		IsDamageProfileKnown: true,
	}
	plan := zonenpc.AttackPlan{
		SourceObjectID: reflection.SourceObjectID,
		TargetObjectID: reflection.TargetObjectID,
		SourcePosition: source.Plan.Position,
		TargetPosition: target.Position,
		Profile:        profile,
	}
	packets, statDelta, isApplied, err := r.npc.applyEnemyReflectedDamage(
		&peerSession, generation, plan,
		zonenpc.AttackResult{Damage: reflection.Damage}, timestamp,
	)
	if err == nil {
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, fmt.Errorf("affixThornsApply: %w", err)
	}
	if !isApplied {
		// Dodges, immunity, and expired targets can still return feedback packets.
		return packets, statDelta, nil
	}
	return packets, statDelta, nil
}
