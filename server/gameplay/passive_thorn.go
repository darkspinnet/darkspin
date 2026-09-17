package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type thornBarkReflection struct {
	sourceObjectID uint32
	result         zoneability.AreaResult
	transition     campaignDamageTransition
}

func (e thornBarkReflection) isEmpty() bool {
	return e.result.Damage.ObjectID == 0
}

func (e *gameplayPeerSession) commitThornBarkReflection(
	plan zonenpc.AttackPlan, acceptedDamage float32,
) (thornBarkReflection, error) {
	if e == nil || e.zone == nil || e.deployedCreatureIndex >=
		uint32(len(e.binding.Creatures)) || acceptedDamage <= 0 ||
		plan.Profile.DescriptorMask&1 == 0 ||
		plan.TargetObjectID != e.deployedObjectID {
		return thornBarkReflection{}, nil
	}
	creature := e.binding.Creatures[e.deployedCreatureIndex]
	if creature.PassiveAbility != util.HashID("ThornBarkModifier") ||
		creature.DamageReflection <= 0 {
		return thornBarkReflection{}, nil
	}
	target, isFound := e.zone.NPCs().NPC(plan.SourceObjectID)
	if !isFound || target.IsDefeated || target.HitPoint <= 0 {
		return thornBarkReflection{}, nil
	}
	damage, err := e.zone.NPCs().Damage(
		e.deployedObjectID, plan.SourceObjectID,
		acceptedDamage*creature.DamageReflection, 0,
	)
	if err != nil {
		return thornBarkReflection{}, fmt.Errorf("damage: %w", err)
	}
	transition, err := e.applyCampaignDamageTransition(damage)
	if err != nil {
		return thornBarkReflection{}, fmt.Errorf("transition: %w", err)
	}
	return thornBarkReflection{
		sourceObjectID: e.deployedObjectID,
		result:         zoneability.AreaResult{Snapshot: target, Damage: damage},
		transition:     transition,
	}, nil
}

func (e campaignNPCActionRuntime) publishThornBarkReflection(
	packet raknet.Packet, sessionKey string, generation uint64, timestamp uint64,
	binding game.GameplayBinding, reflection thornBarkReflection,
) ([][]byte, error) {
	if reflection.isEmpty() {
		return nil, nil
	}
	if e.registry == nil || e.projection.registry == nil || e.effectPool == nil {
		return nil, errors.New("thorn bark publication unavailable")
	}
	runtime := campaignDamageRuntime{
		registry: e.registry, npc: e, projection: e.projection,
		effectPool: e.effectPool, logger: e.logger,
	}
	packets, err := runtime.publishAreaResults(
		packet, sessionKey, generation, reflection.sourceObjectID,
		timestamp, binding, []zoneability.AreaResult{reflection.result},
		[]campaignDamageTransition{reflection.transition}, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("publish: %w", err)
	}
	return packets, nil
}
