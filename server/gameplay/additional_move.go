package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type additionalKitMove struct {
	step                          additionalKitStep
	profile                       zonenpc.ActionProfile
	destination                   game.Vec3
	intangibleEnd                 time.Time
	effects                       *channelEffects
	isBurrow                      bool
	wasNavigationCollisionEnabled bool
}

func (e additionalKitStep) startMove(profile zonenpc.ActionProfile, actor zonenpc.Snapshot, target game.Vec3) ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !isFound || !member.isCampaignNPCSourceGenerationActive(e.generation, e.objectID, actor.ActionGeneration) {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	isBurrow := profile.AbilityName == "Burrow"
	direction := actor.Plan.Position.Sub(target)
	length := direction.Length()
	if length == 0 {
		direction = actor.Facing
		length = 1
	}
	direction = direction.Scale(1 / length)
	destination := target.Add(direction.Scale(actor.Plan.ActorFootprintRadius() + 0.95))
	if !isBurrow {
		if length > 10 {
			// Native Flee first approaches a distant best target to safeDistance8.
			destination = target.Add(direction.Scale(8))
		} else {
			angle := float64(0)
			if member.zone.NPCRandom() != nil {
				angle = member.zone.NPCRandom().Float64()*2*math.Pi/3 - math.Pi/3
			}
			travel := float32(3)
			if member.zone.NPCRandom() != nil {
				travel += float32(member.zone.NPCRandom().Float64()) * 3
			}
			direction = game.Vec3{X: direction.X*float32(math.Cos(angle)) - direction.Y*float32(math.Sin(angle)), Y: direction.X*float32(math.Sin(angle)) + direction.Y*float32(math.Cos(angle))}
			destination = actor.Plan.Position.Add(direction.Scale(travel))
		}
	}
	projected, isProjected, err := zoneaction.NPCProjectPosition(member.zone.Navigation(), destination, actor.Plan.ActorFootprintRadius())
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("additionalLanding: %w", err)
	}
	if !isProjected {
		member.zone.NPCs().PayAdditionalKitCooldown(e.objectID, profile.AbilityName, e.timestamp+uint64(profile.Cooldown/time.Millisecond))
		e.runtime.registry.mutex.Unlock()
		e.timestamp += 250
		cancel, scheduleErr := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{Delay: 250 * time.Millisecond, Produce: e.next}})
		if scheduleErr != nil {
			return nil, fmt.Errorf("additionalLandingRetry: %w", scheduleErr)
		}
		if cancel == nil {
			return nil, fmt.Errorf("additionalLandingRetry: unavailable")
		}
		return nil, nil
	}
	move := &additionalKitMove{step: e, profile: profile, destination: projected, isBurrow: isBurrow, intangibleEnd: e.runtime.now().Add(10 * time.Second), wasNavigationCollisionEnabled: actor.IsNavigationCollisionEnabled}
	if member.additionalKitMoves == nil {
		member.additionalKitMoves = make(map[uint32]*additionalKitMove)
	}
	member.additionalKitMoves[e.objectID] = move
	if isBurrow {
		member.zone.NPCs().SetAdditionalKitHitAnimation(e.objectID, true)
	}
	member.zone.NPCs().PayAdditionalKitCooldown(e.objectID, profile.AbilityName, e.timestamp+uint64(profile.Cooldown/time.Millisecond))
	e.runtime.registry.sessions[e.sessionKey] = member
	e.runtime.registry.mutex.Unlock()
	if !isBurrow {
		return move.move()
	}
	hitAnimationPacket, hitAnimationErr := additionalHitAnimationPacket(e.objectID, true)
	if hitAnimationErr != nil {
		return move.fail("burrowHitAnimation", hitAnimationErr)
	}
	animation, err := npcraknet.AnimationState(e.objectID, "cast_burrow_in", e.timestamp)
	if err != nil {
		return move.fail("burrowAnimation", err)
	}
	effect, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{ObjectID: e.objectID, Asset: util.HashID("burrow_ground_in.ServerEventDef")})
	if err != nil {
		return move.fail("burrowEffect", err)
	}
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{Delay: 2329999924 * time.Nanosecond, Produce: move.move}})
	if err != nil {
		return move.fail("burrowWindup", err)
	}
	if cancel == nil {
		return move.fail("burrowWindup", fmt.Errorf("continuation unavailable"))
	}
	return [][]byte{hitAnimationPacket, animation, effect}, nil
}

func (e *additionalKitMove) move() ([][]byte, error) {
	runtime := e.step.runtime
	runtime.registry.mutex.Lock()
	member, isFound := runtime.registry.sessions[e.step.sessionKey]
	if !isFound || member.additionalKitMoves[e.step.objectID] != e || !member.isCampaignNPCSourceGenerationActive(e.step.generation, e.step.objectID, e.step.actionGeneration) {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	actor, isActorFound := member.zone.NPCs().NPC(e.step.objectID)
	if !isActorFound {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if !member.isCampaignNPCActionActiveAt(e.step.generation, e.step.objectID, runtime.now()) || member.zone.NPCs().SilenceRemaining(e.step.objectID, runtime.now()) > 0 {
		cleanupErr := member.retireAdditionalKitMovesLocked(e.step.objectID, e.step.actionGeneration)
		runtime.registry.sessions[e.step.sessionKey] = member
		runtime.registry.mutex.Unlock()
		if cleanupErr != nil {
			return nil, fmt.Errorf("additionalMoveInterrupt: %w", cleanupErr)
		}
		return e.step.resume(e.step.timestamp + 250)
	}
	if e.isBurrow {
		e.step.timestamp += 2330
	}
	profile := e.profile
	profile.Range = 0.95
	profile.MovementSpeed = actor.Plan.NPCProfile.BaseCombatSpeed
	if profile.MovementSpeed <= 0 {
		profile.MovementSpeed = 2.5
	}
	profile.MovementSpeed *= max(float32(0.05), 1+zonenpc.EffectiveMovementSpeedBuff(actor.Plan))
	var packets [][]byte
	if e.isBurrow {
		profile.Family = zonenpc.ActionCharge
		err := member.zone.NPCs().ApplyIntangible(e.step.objectID, e.intangibleEnd)
		if err != nil {
			runtime.registry.mutex.Unlock()
			return e.fail("burrowIntangible", err)
		}
		err = member.zone.NPCs().SetNavigationCollision(e.step.objectID, false)
		if err != nil {
			runtime.registry.mutex.Unlock()
			return e.fail("burrowCollision", err)
		}
		animation, animationErr := npcraknet.AnimationState(e.step.objectID, "burrow_tunnelling", e.step.timestamp)
		if animationErr != nil {
			runtime.registry.mutex.Unlock()
			return e.fail("burrowTunnelAnimation", animationErr)
		}
		packets = append(packets, animation)
		effects, effectErr := runtime.registry.allocateChannelEffectsLocked(member.zone, runtime.effectPool, []uint32{e.step.objectID})
		if effectErr != nil {
			runtime.registry.mutex.Unlock()
			return e.fail("burrowTunnelAllocate", effectErr)
		}
		e.effects = effects
		attachment := effects.Slots()[0]
		packet, marshalErr := raknet.MarshalApplication(raknet.AttachedEffectMessage{Slot: attachment.slot + 1, ObjectID: e.step.objectID, IsForceAttached: true, Asset: util.HashID("burrower_tunnelling.ServerEventDef")})
		if marshalErr != nil {
			effects.Retire()
			runtime.registry.mutex.Unlock()
			return e.fail("burrowTunnelMarshal", marshalErr)
		}
		effectErr = runtime.registry.publishChannelEffectsLocked(effects, [][]byte{packet})
		member = runtime.registry.sessions[e.step.sessionKey]
		if effectErr != nil {
			effects.Retire()
			runtime.registry.mutex.Unlock()
			return e.fail("burrowTunnelPublish", effectErr)
		}
		statePackets, stateErr := npcraknet.ChargeMovementState(e.step.objectID, zonenpc.EffectiveMovementSpeedBuff(actor.Plan), 0, true)
		if stateErr != nil {
			runtime.registry.mutex.Unlock()
			return e.fail("burrowCollisionMarshal", stateErr)
		}
		packets = append(packets, statePackets...)
	}
	runtime.registry.sessions[e.step.sessionKey] = member
	runtime.registry.mutex.Unlock()
	action := zonenpc.FirstActionPlan{ObjectID: e.step.objectID, TargetObjectID: actor.TargetObjectID, SourcePosition: actor.Plan.Position, TargetPosition: e.destination, Profile: profile, IsPursuitNeeded: true}
	movePackets, err := npcraknet.Pursuit(action)
	if err != nil {
		return e.fail("additionalMoveMarshal", err)
	}
	err = runtime.pursuit.schedule(e.step.packet, e.step.sessionKey, e.step.generation, e.step.objectID, e.step.timestamp, e.destination, profile, e.arrive)
	if err != nil {
		return e.fail("additionalPursuit", err)
	}
	cancel, err := scheduleNPCProducers(runtime.registry, e.step.packet, []raknet.ScheduledPacketProducer{{Delay: 5 * time.Second, Produce: e.timeout}})
	if err != nil {
		return e.fail("additionalMoveTimeout", err)
	}
	if cancel == nil {
		return e.fail("additionalMoveTimeout", fmt.Errorf("continuation unavailable"))
	}
	return append(packets, movePackets...), nil
}

func (e *additionalKitMove) timeout() ([][]byte, error) { return e.arrive(e.step.timestamp + 5000) }

func (e *additionalKitMove) arrive(timestamp uint64) ([][]byte, error) {
	runtime := e.step.runtime
	runtime.registry.mutex.Lock()
	member, isFound := runtime.registry.sessions[e.step.sessionKey]
	if !isFound || member.additionalKitMoves[e.step.objectID] != e {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets, err := e.restoreLocked(&member)
	delete(member.additionalKitMoves, e.step.objectID)
	runtime.registry.sessions[e.step.sessionKey] = member
	isCurrent := member.isCampaignNPCSourceGenerationActive(e.step.generation, e.step.objectID, e.step.actionGeneration)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("additionalArrivalRestore: %w", err)
	}
	if !isCurrent {
		runtime.registry.mutex.Unlock()
		return packets, nil
	}
	if !member.isCampaignNPCActionActiveAt(e.step.generation, e.step.objectID, runtime.now()) || member.zone.NPCs().SilenceRemaining(e.step.objectID, runtime.now()) > 0 {
		runtime.registry.mutex.Unlock()
		resumePackets, resumeErr := e.step.resume(timestamp)
		if resumeErr != nil {
			return packets, fmt.Errorf("additionalArrivalInterrupt: %w", resumeErr)
		}
		return append(packets, resumePackets...), nil
	}
	if e.isBurrow {
		profile := zonenpc.AdditionalBurrowNovaProfile()
		err = member.zone.NPCs().SetAdditionalKitProfile(e.step.objectID, profile)
		if err == nil {
			actor, isActorFound := member.zone.NPCs().NPC(e.step.objectID)
			if isActorFound {
				nova := &additionalKitMove{step: e.step, isBurrow: true, intangibleEnd: runtime.now().Add(1500 * time.Millisecond), wasNavigationCollisionEnabled: actor.IsNavigationCollisionEnabled}
				err = member.zone.NPCs().ApplyIntangible(e.step.objectID, nova.intangibleEnd)
				if err == nil {
					member.additionalKitMoves[e.step.objectID] = nova
					cancel, scheduleErr := scheduleNPCProducers(runtime.registry, e.step.packet, []raknet.ScheduledPacketProducer{{Delay: 1500 * time.Millisecond, Produce: nova.expireNova}})
					if scheduleErr != nil {
						err = fmt.Errorf("novaIntangibleSchedule: %w", scheduleErr)
					} else if cancel == nil {
						err = fmt.Errorf("novaIntangibleSchedule: unavailable")
					}
				}
			}
		}
		runtime.registry.sessions[e.step.sessionKey] = member
		runtime.registry.mutex.Unlock()
		if err != nil {
			return packets, fmt.Errorf("burrowNovaProfile: %w", err)
		}
		novaPackets, novaErr := runtime.produceEnemyCone(e.step.packet, e.step.sessionKey, e.step.generation, e.step.objectID, timestamp)
		if novaErr != nil {
			return packets, fmt.Errorf("burrowNova: %w", novaErr)
		}
		return append(packets, novaPackets...), nil
	}
	runtime.registry.mutex.Unlock()
	nextPackets, err := e.step.resume(timestamp)
	if err != nil {
		return packets, fmt.Errorf("fleeResume: %w", err)
	}
	return append(packets, nextPackets...), nil
}

func (e *additionalKitMove) expireNova() ([][]byte, error) {
	runtime := e.step.runtime
	runtime.registry.mutex.Lock()
	defer runtime.registry.mutex.Unlock()
	member, isFound := runtime.registry.sessions[e.step.sessionKey]
	if !isFound || member.additionalKitMoves[e.step.objectID] != e {
		return nil, nil
	}
	packets, err := e.restoreLocked(&member)
	if err != nil {
		return nil, fmt.Errorf("novaIntangibleRestore: %w", err)
	}
	delete(member.additionalKitMoves, e.step.objectID)
	runtime.registry.sessions[e.step.sessionKey] = member
	return packets, nil
}

func (e *additionalKitMove) restoreLocked(member *gameplayPeerSession) ([][]byte, error) {
	e.effects.Retire()
	if member.zone == nil || !e.isBurrow {
		return nil, nil
	}
	actor, isFound := member.zone.NPCs().NPC(e.step.objectID)
	if !isFound {
		return nil, nil
	}
	member.zone.NPCs().ClearIntangible(e.step.objectID, e.intangibleEnd)
	member.zone.NPCs().SetAdditionalKitHitAnimation(e.step.objectID, false)
	if actor.IsDefeated {
		return nil, nil
	}
	err := member.zone.NPCs().SetNavigationCollision(e.step.objectID, e.wasNavigationCollisionEnabled)
	if err != nil {
		return nil, fmt.Errorf("burrowRestoreCollision: %w", err)
	}
	packets, err := npcraknet.ChargeMovementState(e.step.objectID, zonenpc.EffectiveMovementSpeedBuff(actor.Plan), 0, !e.wasNavigationCollisionEnabled)
	if err != nil {
		return nil, fmt.Errorf("burrowRestoreMarshal: %w", err)
	}
	hitAnimationPacket, hitAnimationErr := additionalHitAnimationPacket(e.step.objectID, false)
	if hitAnimationErr != nil {
		return packets, fmt.Errorf("burrowHitAnimationRestore: %w", hitAnimationErr)
	}
	return append(packets, hitAnimationPacket), nil
}

func additionalHitAnimationPacket(objectID uint32, isDisabled bool) ([]byte, error) {
	flag := float32(0)
	if isDisabled {
		flag = 1
	}
	packet, err := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{ObjectID: objectID, Value: map[uint8]float32{110: flag}})
	if err != nil {
		return nil, fmt.Errorf("hitAnimationMarshal: %w", err)
	}
	return packet, nil
}

func (e *gameplayPeerSession) retireAdditionalKitMovesLocked(sourceID uint32, actionGeneration uint64) error {
	for objectID, state := range e.additionalKitMoves {
		if sourceID != 0 && sourceID != objectID || actionGeneration != 0 && actionGeneration != state.step.actionGeneration {
			continue
		}
		packets, err := state.restoreLocked(e)
		if err != nil {
			return fmt.Errorf("additionalMoveRetire: %w", err)
		}
		delete(e.additionalKitMoves, objectID)
		e.queueCampaignPackets(packets)
		for sessionKey, observer := range state.step.runtime.registry.sessions {
			if observer.zone != e.zone || observer.binding.UserID == e.binding.UserID && observer.generation == e.generation {
				continue
			}
			observer.queueCampaignPackets(packets)
			state.step.runtime.registry.sessions[sessionKey] = observer
		}
	}
	return nil
}

func (e *additionalKitMove) fail(step string, cause error) ([][]byte, error) {
	runtime := e.step.runtime
	runtime.registry.mutex.Lock()
	member, isFound := runtime.registry.sessions[e.step.sessionKey]
	var cleanupErr error
	if isFound && member.additionalKitMoves[e.step.objectID] == e {
		cleanupErr = member.retireAdditionalKitMovesLocked(e.step.objectID, e.step.actionGeneration)
		runtime.registry.sessions[e.step.sessionKey] = member
	}
	runtime.registry.mutex.Unlock()
	isReleased := runtime.releaseActionGeneration(e.step.sessionKey, e.step.generation, e.step.objectID, e.step.actionGeneration)
	if !isReleased {
		// A replaced generation already retired this producer's action lease.
	}
	return nil, fmt.Errorf("%s: %w", step, errors.Join(cause, cleanupErr))
}
