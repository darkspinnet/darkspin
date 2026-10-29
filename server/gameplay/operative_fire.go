package gameplay

import (
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type firePetArrival struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	objectID   uint32
}

func (e firePetArrival) resume(timestamp uint64) ([][]byte, error) {
	packets, err := e.runtime.produceEnemyMelee(e.packet, e.sessionKey, e.generation, e.objectID, timestamp)
	if err != nil {
		return nil, fmt.Errorf("fireResume: %w", err)
	}
	return packets, nil
}

// The authored positioning gambit precedes FireStream and uses its own cooldown.
func (e campaignNPCActionRuntime) produceFirePet(packet raknet.Packet, sessionKey string, generation uint64, objectID uint32, timestamp uint64) ([][]byte, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || !member.isCampaignNPCSourceActive(generation, objectID) {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(objectID)
	target, isTargetFound := member.campaignNPCTarget(generation, enemy.TargetObjectID)
	if !isEnemyFound || !isTargetFound {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	// The authored fire condition prefers a LightningCage target, then any hostile.
	for _, candidate := range member.zone.LiveNPCTargets() {
		if candidate.HitPoint > 0 && hasAgentLightningCage(member, candidate.ObjectID) {
			target = candidate
			break
		}
	}
	if target.ObjectID != enemy.TargetObjectID {
		replaced, isReplaced, err := member.zone.NPCs().Retarget(objectID, zonenpc.Target{ObjectID: target.ObjectID, Position: target.Position, FootprintRadius: target.ActorFootprintRadius, Faction: zonenpc.FactionPlayerAligned, Owner: zonenpc.ActionOwner{UserID: target.UserID, PeerGeneration: target.PeerGeneration}, IsAlive: true})
		if err != nil {
			e.registry.mutex.Unlock()
			return nil, fmt.Errorf("fireRetarget: %w", err)
		}
		if isReplaced {
			enemy = replaced
		}
	}
	profile := zonenpc.ApplyAffixTiming(zonenpc.AgentChildProfile(enemy.Plan.NounName, zonenpc.EffectiveNPCRank(enemy.Plan)), enemy.Plan)
	e.registry.mutex.Unlock()
	positionPackets, isPositionHandled, positionErr := e.positionFirePet(packet, sessionKey, generation, enemy, target, timestamp)
	if positionErr != nil {
		return nil, fmt.Errorf("firePosition: %w", positionErr)
	}
	if isPositionHandled {
		return positionPackets, nil
	}
	plan, err := zonenpc.PlanAttackWithProfile(enemy, target.ObjectID, target.Position, profile, target.ActorFootprintRadius)
	if err == nil {
		return e.produceOperativeCage(packet, sessionKey, generation, plan, timestamp)
	}
	destination := target.Position
	action, err := campaignNPCActionWithProfile(enemy.Plan, target.ObjectID, destination, profile, target.ActorFootprintRadius)
	if err != nil {
		return nil, fmt.Errorf("firePositionPlan: %w", err)
	}
	packets, err := npcraknet.Pursuit(action)
	if err != nil {
		return nil, fmt.Errorf("firePositionMarshal: %w", err)
	}
	resume := firePetArrival{runtime: e, packet: packet, sessionKey: sessionKey, generation: generation, objectID: objectID}
	err = e.pursuit.schedule(packet, sessionKey, generation, objectID, timestamp, action.TargetPosition, profile, resume.resume)
	if err != nil {
		return nil, fmt.Errorf("firePositionMove: %w", err)
	}
	return packets, nil
}

func (e operativeCageSchedule) tickFire(producer *operativeCageProducer) ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isCurrent := e.currentLocked()
	isContinuing := isCurrent && !e.state.isComplete && member.isCampaignNPCActionActiveAt(e.generation, e.state.plan.SourceObjectID, e.runtime.now())
	if !isContinuing {
		e.runtime.registry.mutex.RUnlock()
		return e.finish(producer)
	}
	enemy, isEnemyFound := member.zone.NPCs().NPC(e.state.plan.SourceObjectID)
	target, isTargetFound := member.campaignNPCTarget(e.generation, e.state.plan.TargetObjectID)
	e.runtime.registry.mutex.RUnlock()
	if !isContinuing || !isEnemyFound || !isTargetFound {
		return e.finish(producer)
	}
	arc, err := zoneability.NewMeleeArc(enemy.Plan.Position, enemy.Facing, 3+enemy.Plan.ActorFootprintRadius(), 90)
	if err != nil {
		return e.fail("fireArc", err, nil)
	}
	if !arc.Contains(target.Position, target.ActorFootprintRadius) {
		return e.finish(producer)
	}
	packets, err := e.damage()
	if err != nil {
		return e.fail("firePulse", err, packets)
	}
	animation, err := npcraknet.AnimationState(e.state.plan.SourceObjectID, e.state.plan.Profile.LoopAnimationName, e.timestamp+uint64((e.state.plan.Profile.HitDelay+e.state.elapsed)/time.Millisecond))
	if err != nil {
		return e.fail("fireLoop", err, packets)
	}
	effectPackets, effectErr := e.attachSupportEffects()
	if effectErr != nil {
		return e.fail("fireEffect", effectErr, packets)
	}
	packets = append(packets, effectPackets...)
	packets = append(packets, animation)
	e.runtime.registry.mutex.Lock()
	e.state.elapsed += time.Second
	e.runtime.registry.mutex.Unlock()
	producer.nextStep = operativeCageTick
	producer.nextDelay = time.Second
	return packets, nil
}

func (e campaignNPCActionRuntime) positionFirePet(packet raknet.Packet, sessionKey string, generation uint64, enemy zonenpc.Snapshot, target zone.NPCTarget, timestamp uint64) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.generation != generation || member.zone == nil || !hasAgentLightningCage(member, target.ObjectID) {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	state := member.agentSupports[enemy.Plan.ObjectID]
	if state == nil {
		state = &agentSupportState{}
		if member.agentSupports == nil {
			member.agentSupports = make(map[uint32]*agentSupportState)
		}
		member.agentSupports[enemy.Plan.ObjectID] = state
		e.registry.sessions[sessionKey] = member
	}
	if state.positionReadyTimestamp > timestamp {
		e.registry.mutex.Unlock()
		return nil, false, nil
	}
	profile := zonenpc.ActionProfile{AbilityName: "PositionFirePet", Cooldown: 12 * time.Second}
	profile = zonenpc.ApplyAffixTiming(profile, enemy.Plan)
	state.positionReadyTimestamp = timestamp + uint64(profile.Cooldown/time.Millisecond)
	e.registry.mutex.Unlock()
	var lightning zonenpc.Snapshot
	isLightningFound := false
	for _, modifier := range member.zone.Effect().Snapshot() {
		if modifier.TargetObjectID != target.ObjectID || modifier.GUID != zonenpc.AgentChildProfile("NomadLightningPet.Noun", 1).ModifierGUID() {
			continue
		}
		lightning, isLightningFound = member.zone.NPCs().NPC(modifier.SourceObjectID)
		break
	}
	if !isLightningFound || lightning.IsDefeated {
		return nil, false, nil
	}
	lightningAngle := math.Atan2(float64(lightning.Plan.Position.Y-target.Position.Y), float64(lightning.Plan.Position.X-target.Position.X))
	fireAngle := math.Atan2(float64(enemy.Plan.Position.Y-target.Position.Y), float64(enemy.Plan.Position.X-target.Position.X))
	if math.Abs(math.Remainder(lightningAngle-fireAngle, 2*math.Pi)) > math.Pi/4 {
		return nil, false, nil
	}
	// Lua alternates clockwise/counterclockwise 45-degree probes. Bound the
	// reachability search to one turn so blocked geometry always retains a caller.
	for _, angleOffset := range []float64{-45, 45, -90, 90, -135, 135, 180} {
		angle := fireAngle + angleOffset*math.Pi/180
		if math.Abs(math.Remainder(lightningAngle-angle, 2*math.Pi)) <= math.Pi/4 {
			continue
		}
		desired := target.Position.Add(game.Vec3{X: float32(math.Cos(angle)) * 3, Y: float32(math.Sin(angle)) * 3})
		destination, isReachable, err := navigationClippedMovementDestination(member.zone.Navigation(), enemy.Plan.Position, desired, enemy.NavigationRadius(), enemy.Navigation)
		if err != nil {
			return nil, true, fmt.Errorf("firePositionReachable: %w", err)
		}
		if !isReachable || destination.Sub(desired).Length() > 0.1 {
			continue
		}
		movementProfile := zonenpc.AgentChildProfile(enemy.Plan.NounName, zonenpc.EffectiveNPCRank(enemy.Plan))
		movementProfile.Range = 0.5
		movementProfile.AbilityName = "PositionFirePet"
		action := zonenpc.FirstActionPlan{ObjectID: enemy.Plan.ObjectID, TargetObjectID: target.ObjectID, SourcePosition: enemy.Plan.Position, TargetPosition: destination, Profile: movementProfile, IsPursuitNeeded: true}
		packets, err := npcraknet.Pursuit(action)
		if err != nil {
			return nil, true, fmt.Errorf("firePositionPresentation: %w", err)
		}
		resume := firePetArrival{runtime: e, packet: packet, sessionKey: sessionKey, generation: generation, objectID: enemy.Plan.ObjectID}
		err = e.pursuit.schedule(packet, sessionKey, generation, enemy.Plan.ObjectID, timestamp, destination, movementProfile, resume.resume)
		if err != nil {
			return nil, true, fmt.Errorf("firePositionSchedule: %w", err)
		}
		return packets, true, nil
	}
	return nil, false, nil
}
