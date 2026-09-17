package gameplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

const campaignScaldronCopterTick = 500 * time.Millisecond

type campaignScaldronCopterRun struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	passiveTimestamp uint64
	strafeTimestamp  uint64
	isClockwise      bool
	effectSlots      map[uint32]uint8
}

func (e *campaignScaldronCopterRun) releaseLinks() ([][]byte, error) {
	if e == nil || len(e.effectSlots) == 0 {
		return nil, nil
	}
	packets := make([][]byte, 0, len(e.effectSlots))
	for targetObjectID, effectSlot := range e.effectSlots {
		delete(e.effectSlots, targetObjectID)
		if !e.runtime.effectPool.Release(e.objectID, effectSlot) {
			continue
		}
		packet, err := npcraknet.CopterLinkEffect(
			e.objectID, 0, effectSlot, "", true,
		)
		if err != nil {
			return nil, fmt.Errorf("copterLinkRemove: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *campaignScaldronCopterRun) releaseLinkSlots() {
	if e == nil {
		return
	}
	for targetObjectID, effectSlot := range e.effectSlots {
		e.runtime.effectPool.Release(e.objectID, effectSlot)
		delete(e.effectSlots, targetObjectID)
	}
}

func (e *campaignScaldronCopterRun) syncLinks(
	helpers []zonenpc.Snapshot, effectName string,
) ([][]byte, error) {
	activeTargets := make(map[uint32]struct{}, len(helpers))
	for _, helper := range helpers {
		activeTargets[helper.Plan.ObjectID] = struct{}{}
	}
	packets := make([][]byte, 0)
	for targetObjectID, effectSlot := range e.effectSlots {
		if _, isActive := activeTargets[targetObjectID]; isActive {
			continue
		}
		delete(e.effectSlots, targetObjectID)
		if !e.runtime.effectPool.Release(e.objectID, effectSlot) {
			continue
		}
		packet, err := npcraknet.CopterLinkEffect(
			e.objectID, 0, effectSlot, "", true,
		)
		if err != nil {
			return nil, fmt.Errorf("copterLinkRemove: %w", err)
		}
		packets = append(packets, packet)
	}
	for _, helper := range helpers {
		targetObjectID := helper.Plan.ObjectID
		if _, isAttached := e.effectSlots[targetObjectID]; isAttached {
			continue
		}
		effectSlot, isAllocated := e.runtime.effectPool.Allocate(e.objectID)
		if !isAllocated {
			continue
		}
		e.effectSlots[targetObjectID] = effectSlot
		packet, err := npcraknet.CopterLinkEffect(
			e.objectID, targetObjectID, effectSlot, effectName, false,
		)
		if err != nil {
			delete(e.effectSlots, targetObjectID)
			e.runtime.effectPool.Release(e.objectID, effectSlot)
			return nil, fmt.Errorf("copterLinkCreate: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *campaignScaldronCopterRun) fail(
	step string, err error,
) ([][]byte, error) {
	e.runtime.releaseAction(e.sessionKey, e.generation, e.objectID)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e *campaignScaldronCopterRun) schedule(
	producer func() ([][]byte, error),
) error {
	cancel, err := e.packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: campaignScaldronCopterTick, Produce: producer,
	}})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		return fmt.Errorf("copterSchedule: %w", err)
	}
	return nil
}

func campaignScaldronCopterDestination(
	source game.Vec3, target game.Vec3, distance float32,
	isClockwise bool,
) (game.Vec3, bool) {
	deltaX := source.X - target.X
	deltaY := source.Y - target.Y
	length := float32(math.Hypot(float64(deltaX), float64(deltaY)))
	if length <= 0 || distance < 0 {
		return game.Vec3{}, false
	}
	directionX := deltaX / length
	directionY := deltaY / length
	if length < 8 {
		lateralX := directionY
		lateralY := -directionX
		if isClockwise {
			lateralX = -lateralX
			lateralY = -lateralY
		}
		return game.Vec3{
			X: target.X + lateralX*distance,
			Y: target.Y + lateralY*distance,
			Z: target.Z,
		}, true
	}
	return game.Vec3{
		X: target.X + directionX*distance,
		Y: target.Y + directionY*distance,
		Z: target.Z,
	}, true
}

func campaignScaldronCopterHelpers(
	source zonenpc.Snapshot, snapshots []zonenpc.Snapshot,
	profile zonenpc.ActionProfile,
) []zonenpc.Snapshot {
	family := campaignDifficultyNounFamily(source.Plan.NounName)
	helpers := make([]zonenpc.Snapshot, 0, profile.MaximumTargetCount)
	for _, candidate := range snapshots {
		if candidate.Plan.ObjectID == source.Plan.ObjectID || candidate.IsDefeated ||
			!candidate.IsPublished ||
			campaignDifficultyNounFamily(candidate.Plan.NounName) != family ||
			zonegeometry.Distance(source.Plan.Position, candidate.Plan.Position) > profile.Radius {
			continue
		}
		helpers = append(helpers, candidate)
		if uint32(len(helpers)) >= profile.MaximumTargetCount {
			break
		}
	}
	return helpers
}

func campaignScaldronCopterBeamTargets(
	targets []zone.NPCTarget, start game.Vec3, end game.Vec3,
) []zone.NPCTarget {
	result := make([]zone.NPCTarget, 0, len(targets))
	for _, target := range targets {
		if target.ObjectID == 0 || target.HitPoint <= 0 ||
			!zonegeometry.SegmentIntersectsSphere(
				start, end, target.Position, max(target.FootprintRadius, 0.25),
			) {
			continue
		}
		result = append(result, target)
	}
	return result
}

func (e *campaignScaldronCopterRun) passive() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCActionActiveAt(
		e.generation, e.objectID, e.runtime.now(),
	) && peerSession.campaignNPCCopterRuns[e.objectID] == e
	if !isCurrent {
		packets, err := e.releaseLinks()
		if isFound && peerSession.campaignNPCCopterRuns[e.objectID] == e {
			delete(peerSession.campaignNPCCopterRuns, e.objectID)
			e.runtime.registry.sessions[e.sessionKey] = peerSession
		}
		e.runtime.registry.mutex.Unlock()
		if err != nil {
			return e.fail("copterLinkStop", err)
		}
		return packets, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	profile, isProfileFound := zonenpc.ActionProfileForPlan(source.Plan)
	if !isSourceFound || !isProfileFound ||
		profile.AbilityName != "ScaldronBasicCopter_Passive" {
		packets, err := e.releaseLinks()
		delete(peerSession.campaignNPCCopterRuns, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		if err != nil {
			return e.fail("copterLinkStop", err)
		}
		return packets, nil
	}
	helpers := campaignScaldronCopterHelpers(
		source, peerSession.zone.NPCs().LiveSnapshots(), profile,
	)
	targets := peerSession.zone.LiveNPCTargets()
	packets, err := e.syncLinks(helpers, profile.TrailEffectName)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("copterLinkSync", err)
	}
	statDelta := sporenet.PlayerStatDelta{}
	for _, helper := range helpers {
		start := source.Plan.Position
		start.Z += profile.ProjectileOffset.Z
		end := helper.Plan.Position
		end.Z += profile.ProjectileOffset.Z
		for _, target := range campaignScaldronCopterBeamTargets(targets, start, end) {
			plan := zonenpc.AttackPlan{
				SourceObjectID: source.Plan.ObjectID,
				TargetObjectID: target.ObjectID,
				SourcePosition: start,
				TargetPosition: target.Position,
				Profile:        profile,
			}
			result, commitErr := zonenpc.CommitAttack(
				peerSession.zone.NPCRandom(), plan,
				source.Plan.NPCProfile.CriticalRating,
				e.runtime.program.Critical,
			)
			if commitErr != nil {
				e.runtime.registry.mutex.Unlock()
				return e.fail("copterBeamCommit", commitErr)
			}
			hitPackets, targetStatDelta, isApplied, damageErr :=
				e.runtime.applyEnemyAreaAttackDamage(
					&peerSession, e.generation, plan, result, e.passiveTimestamp,
				)
			if damageErr != nil {
				e.runtime.registry.mutex.Unlock()
				return e.fail("copterBeamDamage", damageErr)
			}
			packets = append(packets, hitPackets...)
			statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
			if isApplied {
				hitPacket, hitErr := effectraknet.Event(effectraknet.EventRequest{
					AssetID:  util.HashID(profile.ImpactEffectName),
					ObjectID: target.ObjectID,
					Position: target.Position,
				})
				if hitErr != nil {
					e.runtime.registry.mutex.Unlock()
					return e.fail("copterBeamHitEffect", hitErr)
				}
				packets = append(packets, hitPacket)
			}
		}
	}
	binding := peerSession.binding
	e.passiveTimestamp += uint64(campaignScaldronCopterTick / time.Millisecond)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	if statDelta.PVEDamageTaken > 0 {
		err := e.runtime.stats.Record(context.Background(), binding, statDelta)
		if err != nil {
			e.runtime.logger.Printf(
				"campaign Copter beam stat persistence omitted object=%d: %v",
				e.objectID, err,
			)
		}
	}
	err = e.schedule(e.passive)
	if err != nil {
		return e.fail("copterBeamNext", err)
	}
	return packets, nil
}

func (e *campaignScaldronCopterRun) strafe() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCActionActiveAt(
		e.generation, e.objectID, e.runtime.now(),
	) && peerSession.campaignNPCCopterRuns[e.objectID] == e &&
		peerSession.zone.NPCRandom() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		e.generation, source.TargetObjectID,
	)
	profile, isProfileFound := zonenpc.ActionProfileForPlan(source.Plan)
	if !isSourceFound || !isTargetFound || !isProfileFound {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	distance := profile.MinimumRange +
		float32(peerSession.zone.NPCRandom().Float64())*
			(profile.Range-profile.MinimumRange)
	if zonegeometry.Distance(source.Plan.Position, target.Position) < 8 {
		distance = float32(peerSession.zone.NPCRandom().Float64()) * 5
	}
	destination, isDestinationFound := campaignScaldronCopterDestination(
		source.Plan.Position, target.Position, distance, e.isClockwise,
	)
	if isDestinationFound {
		destination, isDestinationFound, _ =
			zoneaction.NPCDirectMovementDestination(
				peerSession.zone.Navigation(), source.Plan.Position, destination,
				source.Plan.NPCProfile.FootprintRadius,
			)
	}
	if !isDestinationFound {
		e.isClockwise = !e.isClockwise
		destination = target.Position
	}
	step, err := peerSession.zone.NPCs().AdvancePursuit(
		peerSession.zone.Navigation(), e.objectID, destination, 0.1,
		profile.MovementSpeed, source.Plan.NPCProfile.FootprintRadius,
		campaignScaldronCopterTick,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("copterStrafeAdvance", err)
	}
	e.strafeTimestamp += uint64(campaignScaldronCopterTick / time.Millisecond)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets, err := npcraknet.PursuitRedirect(
		e.objectID, step.Position, target.ObjectID, target.Position, 0.1,
	)
	if err != nil {
		return e.fail("copterStrafeRedirect", err)
	}
	err = e.schedule(e.strafe)
	if err != nil {
		return e.fail("copterStrafeNext", err)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) produceScaldronBasicCopter(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCActionActiveAt(
		generation, objectID, r.now(),
	)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	if peerSession.campaignNPCCopterRuns == nil {
		peerSession.campaignNPCCopterRuns = make(
			map[uint32]*campaignScaldronCopterRun,
		)
	}
	if peerSession.campaignNPCCopterRuns[objectID] != nil {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	run := &campaignScaldronCopterRun{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
		passiveTimestamp: timestamp, strafeTimestamp: timestamp,
		isClockwise: peerSession.zone.NPCRandom() != nil &&
			peerSession.zone.NPCRandom().Float64() < 0.5,
		effectSlots: make(map[uint32]uint8),
	}
	peerSession.campaignNPCCopterRuns[objectID] = run
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	passivePackets, err := run.passive()
	if err != nil {
		return nil, fmt.Errorf("copterPassiveStart: %w", err)
	}
	strafePackets, err := run.strafe()
	if err != nil {
		return nil, fmt.Errorf("copterStrafeStart: %w", err)
	}
	return append(passivePackets, strafePackets...), nil
}
