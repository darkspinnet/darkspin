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
const campaignScaldronCopterMoveTick = 200 * time.Millisecond

type campaignScaldronCopterRun struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	passiveTimestamp uint64
	strafeTimestamp  uint64
	isClockwise      bool
	circleRadius     float32
	circleExpiresAt  time.Time
	spawnPosition    game.Vec3
	idleDestination  game.Vec3
	idlePauseUntil   time.Time
	isIdleMoving     bool
	effectSlots      map[uint32]uint8
}

func (e *campaignScaldronCopterRun) releaseLinks() ([][]byte, error) {
	if e == nil || len(e.effectSlots) == 0 {
		return nil, nil
	}
	packets := make([][]byte, 0, len(e.effectSlots))
	for targetObjectID, effectSlot := range e.effectSlots {
		delete(e.effectSlots, targetObjectID)
		e.runtime.effectPool.Release(e.objectID, effectSlot)
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
		e.runtime.effectPool.Release(e.objectID, effectSlot)
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
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if isFound && member.generation == e.generation && member.campaignNPCCopterRuns[e.objectID] == e {
		packets, cleanupErr := e.releaseLinks()
		if cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
		member.queueCampaignPackets(packets)
		delete(member.campaignNPCCopterRuns, e.objectID)
		e.runtime.registry.sessions[e.sessionKey] = member
	}
	e.runtime.registry.mutex.Unlock()
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e *campaignScaldronCopterRun) schedule(
	delay time.Duration,
	producer func() ([][]byte, error),
) error {
	cancel, err := scheduleNPCProducers(e.runtime.registry, e.packet, []raknet.ScheduledPacketProducer{{
		Delay: delay, Produce: producer,
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
	speed float32,
	isClockwise bool,
) (game.Vec3, bool) {
	deltaX := source.X - target.X
	deltaY := source.Y - target.Y
	length := float32(math.Hypot(float64(deltaX), float64(deltaY)))
	if length <= 0 || distance <= 0 {
		return game.Vec3{}, false
	}
	directionX := deltaX / length
	directionY := deltaY / length
	// CircleTarget (chunk 75): project onto the 7-9 unit ring, add the
	// tangential step, then normalize back onto that ring. Never cross the
	// target's center as the old 90-degree destination shortcut did.
	lateral := speed * 0.25
	if length > distance {
		lateral = length / distance * 5
	}
	if isClockwise {
		lateral = -lateral
	}
	radialX := directionX*distance + directionY*lateral
	radialY := directionY*distance - directionX*lateral
	radialLength := float32(math.Hypot(float64(radialX), float64(radialY)))
	return game.Vec3{
		X: target.X + radialX/radialLength*distance,
		Y: target.Y + radialY/radialLength*distance,
		Z: target.Z,
	}, true
}

func campaignScaldronCopterHelpers(
	source zonenpc.Snapshot, snapshots []zonenpc.Snapshot,
	profile zonenpc.ActionProfile,
) []zonenpc.Snapshot {
	family := campaignDifficultyNounFamily(source.Plan.NounName)
	helpers := make([]zonenpc.Snapshot, 0, profile.MaximumTargetCount)
	helperCount := uint32(0)
	for _, candidate := range snapshots {
		if candidate.Plan.ObjectID == source.Plan.ObjectID || candidate.IsDefeated ||
			!candidate.IsPublished ||
			campaignDifficultyNounFamily(candidate.Plan.NounName) != family ||
			zonegeometry.Distance(source.Plan.Position, candidate.Plan.Position) > profile.Radius {
			continue
		}
		// The client counts eligible helpers before CompareIDs selects one
		// owner for each link. The reverse link must not deal a second hit.
		helperCount++
		if source.Plan.ObjectID > candidate.Plan.ObjectID {
			helpers = append(helpers, candidate)
		}
		if helperCount >= profile.MaximumTargetCount {
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
	isCurrent := isFound && e.isCurrent(peerSession)
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
		// TargetUtils queries the objects' positions, not the visual beam's
		// attachment offset. Raising this segment misses small footprints.
		start := source.Plan.Position
		end := helper.Plan.Position
		for _, target := range campaignScaldronCopterBeamTargets(targets, start, end) {
			plan, planErr := zonenpc.PlanAreaAttackWithProfile(source, target.ObjectID, target.Position, profile)
			if planErr != nil {
				e.runtime.registry.mutex.Unlock()
				return e.fail("copterBeamPlan", planErr)
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
	err = e.schedule(campaignScaldronCopterTick, e.passive)
	if err != nil {
		return e.fail("copterBeamNext", err)
	}
	return packets, nil
}

func (e *campaignScaldronCopterRun) strafe() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && e.isCurrent(peerSession) &&
		peerSession.zone.NPCRandom() != nil
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	npcSession := peerSession.zone.NPCs()
	now := e.runtime.now()
	if npcSession.StunRemaining(e.objectID, now) > 0 ||
		npcSession.SleepRemaining(e.objectID, now) > 0 || npcSession.FearRemaining(e.objectID, now) > 0 {
		e.runtime.registry.mutex.Unlock()
		err := e.schedule(campaignScaldronCopterMoveTick, e.strafe)
		if err != nil {
			return e.fail("copterDisabledNext", err)
		}
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	if isSourceFound && !source.IsActionStarted {
		packets, err := e.idleLocked(&peerSession, source)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		if err != nil {
			return e.fail("copterIdle", err)
		}
		err = e.schedule(campaignScaldronCopterMoveTick, e.strafe)
		if err != nil {
			return e.fail("copterIdleNext", err)
		}
		return packets, nil
	}
	target, isTargetFound := peerSession.campaignNPCTarget(
		e.generation, source.TargetObjectID,
	)
	profile, isProfileFound := zonenpc.ActionProfileForPlan(source.Plan)
	if !isSourceFound || !isTargetFound || !isProfileFound {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	if !e.runtime.now().Before(e.circleExpiresAt) {
		e.circleRadius = 7 + float32(peerSession.zone.NPCRandom().Float64())*2
		e.circleExpiresAt = e.runtime.now().Add(time.Duration(
			(1 + peerSession.zone.NPCRandom().Float64()) * float64(time.Second),
		))
	}
	destination, isDestinationFound := campaignScaldronCopterDestination(
		source.Plan.Position, target.Position, e.circleRadius, profile.MovementSpeed, e.isClockwise,
	)
	if isDestinationFound {
		var navigationErr error
		destination, isDestinationFound, navigationErr =
			zoneaction.NPCDirectMovementDestination(
				peerSession.zone.Navigation(), source.Plan.Position, destination,
				source.NavigationRadius(), source.Navigation,
			)
		if navigationErr != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("copterStrafeNavigation", navigationErr)
		}
	}
	if !isDestinationFound {
		e.isClockwise = !e.isClockwise
		destination = source.Plan.Position
	}
	step, err := peerSession.zone.NPCs().AdvancePursuit(
		peerSession.zone.Navigation(), e.objectID, destination, 0.1,
		profile.MovementSpeed, source.Plan.NPCProfile.FootprintRadius,
		campaignScaldronCopterMoveTick,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return e.fail("copterStrafeAdvance", err)
	}
	e.strafeTimestamp += uint64(campaignScaldronCopterMoveTick / time.Millisecond)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	var packets [][]byte
	if step.IsBlocked {
		packets, err = npcraknet.StopAtPose(e.objectID, step.Position, source.Facing)
	} else {
		packets, err = npcraknet.BoundedStrafe(
			e.objectID, step.Position, destination, target.ObjectID, target.Position, 0.1,
		)
	}
	if err != nil {
		return e.fail("copterStrafeRedirect", err)
	}
	err = e.schedule(campaignScaldronCopterMoveTick, e.strafe)
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
	isCurrent := isFound && peerSession.generation == generation && peerSession.zone != nil &&
		!peerSession.isZoneTerminal() && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(objectID)
	if !isSourceFound || source.IsDefeated || !source.IsPublished || source.Plan.IsIntroductionHidden {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	for _, other := range r.registry.sessions {
		if other.zone == peerSession.zone && other.campaignNPCCopterRuns[objectID] != nil {
			r.registry.mutex.Unlock()
			return nil, nil
		}
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
		effectSlots:   make(map[uint32]uint8),
		spawnPosition: source.Plan.Position,
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
