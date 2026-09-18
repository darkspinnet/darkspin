package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type thornBarkReflection struct {
	sessionKey     string
	generation     uint64
	binding        game.GameplayBinding
	packet         raknet.Packet
	sourceObjectID uint32
	result         zoneability.AreaResult
	transition     campaignDamageTransition
}

// Retain only connection-owned schedulers, never a request payload, response
// collector or command-scoped cancellation, for reactions on another peer.
func gameplaySchedulePacket(packet raknet.Packet) raknet.Packet {
	packet = packet.Autonomous()
	return raknet.Packet{
		Schedule: packet.Schedule, ScheduleFunc: packet.ScheduleFunc,
		ScheduleGroup: packet.ScheduleGroup, ScheduleGroupResult: packet.ScheduleGroupResult,
	}
}

func (e campaignNPCActionRuntime) applyEnemyAttackDamage(
	peerSession *gameplayPeerSession, generation uint64,
	plan zonenpc.AttackPlan, result zonenpc.AttackResult, timestamp uint64,
) ([][]byte, sporenet.PlayerStatDelta, thornBarkReflection, bool, error) {
	previousHitPoint := float32(0)
	if peerSession != nil {
		target, isFound := peerSession.campaignNPCTarget(generation, plan.TargetObjectID)
		if isFound && target.IsHero {
			previousHitPoint = target.HitPoint
		}
	}
	packets, statDelta, isApplied, err := e.applyEnemyDamage(
		peerSession, generation, plan, result, timestamp, false, false, false,
	)
	if err != nil {
		return nil, statDelta, thornBarkReflection{}, false, fmt.Errorf("attackDamage: %w", err)
	}
	if !isApplied || previousHitPoint <= 0 {
		return packets, statDelta, thornBarkReflection{}, isApplied, nil
	}
	target, isFound := peerSession.campaignNPCTarget(generation, plan.TargetObjectID)
	if !isFound || target.HitPoint <= 0 {
		return packets, statDelta, thornBarkReflection{}, true, nil
	}
	// Damage reactions may have changed the session or deployed a replacement.
	// Resolve it again and never let a replacement reflect the previous hit.
	targetSession, targetSessionKey := e.heroTargetSession(peerSession, target)
	if targetSession == nil {
		return packets, statDelta, thornBarkReflection{}, true, nil
	}
	acceptedDamage := max(float32(0), previousHitPoint-target.HitPoint)
	reflection, err := targetSession.commitThornBarkReflection(plan, acceptedDamage)
	if err != nil {
		return nil, sporenet.PlayerStatDelta{}, thornBarkReflection{}, false,
			fmt.Errorf("thornBarkCommit: %w", err)
	}
	if targetSessionKey != "" {
		reflection.sessionKey = targetSessionKey
		e.registry.sessions[targetSessionKey] = *targetSession
	}
	return packets, statDelta, reflection, true, nil
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
	sourcePosition := game.Vec3(e.playerPosition)
	damage, err := e.zone.NPCs().Hit(zonenpc.HitRequest{
		SourceObjectID: e.deployedObjectID,
		TargetObjectID: plan.SourceObjectID,
		Damage:         acceptedDamage * creature.DamageReflection,
		SourcePosition: &sourcePosition,
		Metadata:       zonenpc.DamageMetadata{DamageSource: 0},
	})
	if err != nil {
		return thornBarkReflection{}, fmt.Errorf("damage: %w", err)
	}
	transition, err := e.applyCampaignDamageTransition(damage)
	if err != nil {
		return thornBarkReflection{}, fmt.Errorf("transition: %w", err)
	}
	return thornBarkReflection{
		generation: e.generation, binding: e.binding, packet: e.schedulePacket,
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
	isRemote := reflection.sessionKey != ""
	if isRemote {
		sessionKey = reflection.sessionKey
		generation = reflection.generation
		binding = reflection.binding
		packet = reflection.packet
		packet.SourceTime = timestamp
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
	if isRemote {
		e.registry.mutex.Lock()
		targetSession, isFound := e.registry.sessions[sessionKey]
		if isFound && targetSession.generation == generation {
			targetSession.queuePackets(packets)
			e.registry.sessions[sessionKey] = targetSession
		}
		e.registry.mutex.Unlock()
		return nil, nil
	}
	return packets, nil
}
