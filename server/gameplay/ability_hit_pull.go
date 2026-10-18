package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const binarySentinelActiveName = "BinarySentinelActive"
const binarySentinelPullSpeed = float32(25)
const binarySentinelPullOutro = 250 * time.Millisecond
const binarySentinelBossPullDuration = 1600 * time.Millisecond

type heroPullExpiry struct {
	runtime          campaignDamageRuntime
	sessionKey       string
	generation       uint64
	run              *campaignNPCModifierRun
	zone             *zone.Zone
	deletePacket     []byte
	isCommitted      bool
	isExpired        bool
	actionGeneration uint64
	positionRevision uint64
	landingTimestamp uint64
}

// BinarySentinelPullModifier waits for the jump, resets react_pulled, then
// waits for the outro. Fence this reset against later actions and displacement.
func (e *heroPullExpiry) land() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.isCommitted || e.isExpired || !isFound || peerSession.generation != e.generation ||
		peerSession.zone != e.zone || peerSession.campaignNPCModifiers[e.run.instanceID] != e.run {
		return nil, nil
	}
	target, isTargetFound := e.zone.NPCs().LiveNPC(e.run.record.TargetObjectID)
	if !isTargetFound || target.ActionGeneration != e.actionGeneration ||
		target.PositionRevision != e.positionRevision {
		return nil, nil
	}
	packet, err := npcraknet.ResetAnimation(target.Plan.ObjectID, e.landingTimestamp)
	if err != nil {
		return nil, fmt.Errorf("heroPullLanding: %w", err)
	}
	e.runtime.queueHeroPullPacketsLocked(e.sessionKey, &peerSession, [][]byte{packet})
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	return nil, nil
}

func (e *heroPullExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	e.isExpired = true
	if !e.isCommitted {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone == e.zone &&
		peerSession.campaignNPCModifiers[e.run.instanceID] == e.run
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.zone.Effect().Remove(e.run.instanceID)
	peerSession.untrackCampaignNPCModifier(e.run)
	e.runtime.queueHeroPullPacketsLocked(e.sessionKey, &peerSession, [][]byte{e.deletePacket})
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	isCreated, err := e.run.release(e.runtime.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("heroPullRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	return nil, nil
}

func (e campaignDamageRuntime) applyAcceptedHitPulls(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, plan zoneability.AreaPlan,
	results []zoneability.AreaResult,
) ([][]byte, error) {
	if plan.Definition.Name != binarySentinelActiveName ||
		plan.Definition.RootModifierID == 0 {
		return nil, nil
	}
	packets := make([][]byte, 0)
	for _, result := range results {
		if result.Damage.IsDamageImmune || result.Damage.IsDefeated ||
			result.Damage.IsTurtleStarted {
			continue
		}
		targetPackets, err := e.applyHeroPull(
			packet, sessionKey, generation, sourceObjectID, timestamp,
			plan, result,
		)
		if err != nil {
			return packets, fmt.Errorf("acceptedHitPull[%d]: %w", result.Damage.ObjectID, err)
		}
		packets = append(packets, targetPackets...)
	}
	return packets, nil
}

func (e campaignDamageRuntime) applyHeroPull(
	packet raknet.Packet, sessionKey string, generation uint64,
	sourceObjectID uint32, timestamp uint64, plan zoneability.AreaPlan,
	result zoneability.AreaResult,
) ([][]byte, error) {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.zone.Hero() != nil && peerSession.zone.Effect() != nil
	if !isCurrent {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	hero, isHeroFound := peerSession.zone.Hero().Snapshot(peerSession.binding.UserID, generation)
	target, isTargetFound := peerSession.zone.NPCs().NPC(result.Damage.ObjectID)
	if !isHeroFound || hero.ObjectID != sourceObjectID || !isTargetFound ||
		target.IsDefeated || target.IsTurtleActive {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	originalZone := peerSession.zone
	modifierID := plan.Definition.RootModifierID
	duration := binarySentinelPullOutro
	movementPackets := make([][]byte, 0)
	destination := game.Vec3{}
	isMoving := false
	isRooted := originalZone.NPCs().RootRemaining(target.Plan.ObjectID, e.npc.now()) > 0
	if target.Plan.IsBoss {
		modifierID = util.HashID("PushPullBossModifier")
		duration = binarySentinelBossPullDuration
	} else if !isRooted {
		delta := hero.Position.Sub(target.Plan.Position)
		distance := delta.Length()
		edgeDistance := distance - max(float32(0), hero.FootprintRadius) -
			max(float32(0), target.Plan.ActorFootprintRadius())
		if distance > 0 && edgeDistance > 0 {
			desired := target.Plan.Position.Add(delta.Scale(edgeDistance / distance))
			clippedDestination, isDestinationFound, destinationErr := navigationClippedMovementDestination(
				originalZone.Navigation(), target.Plan.Position, desired,
				max(target.NavigationRadius(), float32(0.25)), target.Navigation,
			)
			if destinationErr != nil {
				e.registry.mutex.Unlock()
				return nil, fmt.Errorf("heroPullDestination: %w", destinationErr)
			}
			if isDestinationFound {
				destination = clippedDestination
				movementDistance := destination.Sub(target.Plan.Position).Length()
				isMoving = movementDistance > 0
				if isMoving {
					attackPlan := zonenpc.AttackPlan{
						SourceObjectID: sourceObjectID, TargetObjectID: target.Plan.ObjectID,
						SourcePosition: hero.Position, TargetPosition: target.Plan.Position,
						Profile: zonenpc.ActionProfile{
							ForcedMovementSpeed:        binarySentinelPullSpeed,
							ForcedMovementDistance:     movementDistance,
							ForcedMovementReactionName: "react_pulled",
						},
					}
					var movementErr error
					movementPackets, movementErr = npcraknet.ForcedJump(
						attackPlan, destination, timestamp, [3]float32{},
					)
					if movementErr != nil {
						e.registry.mutex.Unlock()
						return nil, fmt.Errorf("heroPullMarshal: %w", movementErr)
					}
					duration += time.Duration(float64(movementDistance/binarySentinelPullSpeed) * float64(time.Second))
				}
			}
		}
	}
	e.registry.mutex.Unlock()

	run, err := newCampaignNPCModifierRun(e.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("heroPullRun: %w", err)
	}
	run.record = zoneeffect.Modifier{
		InstanceID: run.instanceID, GUID: modifierID,
		SourceObjectID: sourceObjectID, TargetObjectID: target.Plan.ObjectID,
		Rank: 1, Duration: duration, Kind: zoneeffect.ModifierKindDebuff,
		InitiatorObject: sourceObjectID, StackCount: 1,
	}
	createPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: sourceObjectID, TargetObjectID: target.Plan.ObjectID,
		ModifierID: modifierID, InstanceID: run.instanceID,
		StackCount: 1, Duration: duration, Timestamp: timestamp,
	})
	if err != nil {
		e.rejectHeroPull(run, nil)
		return nil, fmt.Errorf("heroPullCreate: %w", err)
	}
	deletePacket, err := effectraknet.ModifierDelete(target.Plan.ObjectID, run.instanceID)
	if err != nil {
		e.rejectHeroPull(run, nil)
		return nil, fmt.Errorf("heroPullDelete: %w", err)
	}
	expiry := &heroPullExpiry{
		runtime: e, sessionKey: sessionKey, generation: generation,
		run: run, zone: originalZone, deletePacket: deletePacket,
	}
	producers := make([]raknet.ScheduledPacketProducer, 0, 2)
	if isMoving {
		landingDelay := duration - binarySentinelPullOutro
		expiry.landingTimestamp = timestamp + uint64(landingDelay/time.Millisecond)
		producers = append(producers, raknet.ScheduledPacketProducer{Delay: landingDelay, Produce: expiry.land})
	}
	producers = append(producers, raknet.ScheduledPacketProducer{Delay: duration, Produce: expiry.produce})
	cancel, scheduleErr := packet.ScheduleProducers(producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		e.rejectHeroPull(run, cancel)
		return nil, fmt.Errorf("heroPullSchedule: %w", scheduleErr)
	}

	e.registry.mutex.Lock()
	latest, isLatestFound := e.registry.sessions[sessionKey]
	isLatest := isLatestFound && latest.generation == generation && latest.zone == originalZone && !expiry.isExpired
	if isLatest {
		latestHero, isLatestHeroFound := originalZone.Hero().Snapshot(latest.binding.UserID, generation)
		latestTarget, isLatestTargetFound := originalZone.NPCs().NPC(target.Plan.ObjectID)
		isLatest = isLatestHeroFound && latestHero.ObjectID == sourceObjectID && latestHero.Position == hero.Position &&
			isLatestTargetFound && !latestTarget.IsDefeated && !latestTarget.IsTurtleActive &&
			latestTarget.ActionGeneration == target.ActionGeneration && latestTarget.ActionOwner == target.ActionOwner &&
			latestTarget.PositionRevision == target.PositionRevision &&
			latestTarget.Plan.IsEqual(target.Plan) &&
			(originalZone.NPCs().RootRemaining(target.Plan.ObjectID, e.npc.now()) > 0) == isRooted
	}
	if !isLatest {
		e.registry.mutex.Unlock()
		e.rejectHeroPull(run, cancel)
		return nil, nil
	}
	interruption := campaignNPCForcedMovementInterruption{}
	if isMoving {
		interruption, err = e.registry.prepareCampaignNPCForcedMovementLocked(&latest, target.Plan.ObjectID)
		if err != nil {
			e.registry.mutex.Unlock()
			e.rejectHeroPull(run, cancel)
			return nil, fmt.Errorf("heroPullInterruptPrepare: %w", err)
		}
	}
	err = latest.trackCampaignNPCModifier(run)
	if err != nil {
		e.registry.mutex.Unlock()
		e.rejectHeroPull(run, cancel)
		return nil, fmt.Errorf("heroPullTrack: %w", err)
	}
	err = originalZone.Effect().Put(run.record)
	if err != nil {
		latest.untrackCampaignNPCModifier(run)
		e.registry.sessions[sessionKey] = latest
		e.registry.mutex.Unlock()
		e.rejectHeroPull(run, cancel)
		return nil, fmt.Errorf("heroPullStore: %w", err)
	}
	if isMoving {
		err = originalZone.NPCs().SetPosition(target.Plan.ObjectID, destination)
		if err != nil {
			originalZone.Effect().Remove(run.instanceID)
			latest.untrackCampaignNPCModifier(run)
			e.registry.sessions[sessionKey] = latest
			e.registry.mutex.Unlock()
			e.rejectHeroPull(run, cancel)
			return nil, fmt.Errorf("heroPullMove: %w", err)
		}
		err = originalZone.NPCs().ApplyStun(target.Plan.ObjectID, e.npc.now().Add(duration))
		if err != nil {
			originalZone.Effect().Remove(run.instanceID)
			latest.untrackCampaignNPCModifier(run)
			e.registry.sessions[sessionKey] = latest
			e.registry.mutex.Unlock()
			e.rejectHeroPull(run, cancel)
			return nil, fmt.Errorf("heroPullMotionHold: %w", err)
		}
	}
	if isMoving {
		e.registry.commitCampaignNPCForcedMovementLocked(sessionKey, &latest, interruption)
		movedTarget, isMovedTargetFound := originalZone.NPCs().NPC(target.Plan.ObjectID)
		if isMovedTargetFound {
			expiry.actionGeneration = movedTarget.ActionGeneration
			expiry.positionRevision = movedTarget.PositionRevision
		}
	}
	run.cancel = cancel
	isCreated := run.create()
	if !isCreated {
		// The uncommitted run is exclusively owned by this admission.
		if e.logger != nil {
			e.logger.Printf("RakNet hero pull modifier unexpectedly unavailable object=%d instance=%d", target.Plan.ObjectID, run.instanceID)
		}
	}
	expiry.isCommitted = true
	packets := append(interruption.packets, createPacket)
	packets = append(packets, movementPackets...)
	e.queueHeroPullPacketsLocked(sessionKey, &latest, packets)
	e.registry.sessions[sessionKey] = latest
	e.registry.mutex.Unlock()
	return nil, nil
}

// Admission owns the run until commit. Rejection never restores an old actor
// position and always retires both the armed timer and modifier reservation.
func (e campaignDamageRuntime) rejectHeroPull(run *campaignNPCModifierRun, cancel raknet.CancelSchedule) {
	if cancel != nil {
		cancel()
	}
	isCreated, err := run.release(e.npc.modifierPool)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("RakNet hero pull reservation release failed instance=%d: %v", run.instanceID, err)
		}
		return
	}
	if isCreated && e.logger != nil {
		e.logger.Printf("RakNet hero pull rejected an unexpectedly committed modifier instance=%d", run.instanceID)
	}
}

// Queue the accepted publication for the caster as well as every original-zone
// peer. A later target or scheduled producer error cannot discard this batch.
func (e campaignDamageRuntime) queueHeroPullPacketsLocked(sessionKey string, caster *gameplayPeerSession, packets [][]byte) {
	caster.queueCampaignPackets(packets)
	for candidateSessionKey, candidate := range e.registry.sessions {
		if candidateSessionKey == sessionKey || candidate.zone != caster.zone {
			continue
		}
		candidate.queueCampaignPackets(packets)
		e.registry.sessions[candidateSessionKey] = candidate
	}
}
