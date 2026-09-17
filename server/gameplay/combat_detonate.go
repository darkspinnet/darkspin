package gameplay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignExploderScarabSchedule struct {
	request campaignNPCAttackRequest
	plan    zonenpc.AttackPlan
}

func (e campaignExploderScarabSchedule) fail(
	step string, err error,
) ([][]byte, error) {
	e.request.runtime.releaseAction(
		e.request.sessionKey, e.request.generation, e.request.objectID,
	)
	return nil, fmt.Errorf("%s: %w", step, err)
}

func (e campaignExploderScarabSchedule) explode() ([][]byte, error) {
	startPackets, err := e.request.runtime.startNPCAttack(e.request.sessionKey, e.request.generation, e.plan, e.request.timestamp)
	if err != nil {
		return e.fail("firebombStart", err)
	}
	_, scheduleErr := e.request.packet.ScheduleProducers(
		[]raknet.ScheduledPacketProducer{{
			Delay: e.plan.Profile.HitDelay, Produce: e.hit,
		}},
	)
	if scheduleErr != nil {
		e.request.runtime.releaseAction(
			e.request.sessionKey, e.request.generation, e.request.objectID,
		)
		e.request.runtime.logger.Printf(
			"RakNet campaign Nova Firebomb contact not scheduled object=%d: %v",
			e.request.objectID, scheduleErr,
		)
	}
	return startPackets, nil
}

func (e campaignExploderScarabSchedule) hit() ([][]byte, error) {
	req := e.request
	runtime := req.runtime
	runtime.registry.mutex.Lock()
	peerSession, isFound := runtime.registry.sessions[req.sessionKey]
	isCurrent := isFound && peerSession.generation == req.generation &&
		peerSession.isCampaignNPCSourceActive(req.generation, req.objectID)
	if !isCurrent {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(req.objectID)
	if !isSourceFound || source.IsDefeated || peerSession.zone.NPCRandom() == nil {
		runtime.registry.mutex.Unlock()
		return e.fail(
			"exploderScarabSource", errors.New("source unavailable"),
		)
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(source.Plan)
	if !isProfileFound || profile.Family != zonenpc.ActionDetonate {
		runtime.registry.mutex.Unlock()
		e.request.runtime.releaseAction(
			e.request.sessionKey, e.request.generation, e.request.objectID,
		)
		return nil, nil
	}
	isFirebomb := profile.AbilityName == "ScaldronBasicMonk_Firebomb"
	if !isFirebomb {
		target, isTargetFound := peerSession.campaignNPCTarget(
			req.generation, source.TargetObjectID,
		)
		if !isTargetFound || zonegeometry.Distance(
			source.Plan.Position, target.Position,
		) >= profile.Range+source.Plan.NPCProfile.FootprintRadius+
			target.FootprintRadius {
			runtime.registry.mutex.Unlock()
			e.request.runtime.releaseAction(
				e.request.sessionKey, e.request.generation, e.request.objectID,
			)
			return nil, nil
		}
	}
	packets := make([][]byte, 0, 1)
	if profile.ImpactEffectName != "" {
		impactPacket, impactErr := npcraknet.PositionedEffect(
			profile.ImpactEffectName, source.Plan.Position,
		)
		if impactErr != nil {
			runtime.logger.Printf(
				"RakNet exploder impact presentation omitted object=%d: %v",
				req.objectID, impactErr,
			)
		} else {
			packets = append(packets, impactPacket)
		}
	}
	targets := campaignPlungeTargets(
		peerSession.zone.LiveNPCTargets(), source.Plan.Position, profile.Radius,
	)
	modifierPlans := make([]zonenpc.AttackPlan, 0, len(targets))
	statDelta := sporenet.PlayerStatDelta{}
	hitTimestamp := req.timestamp + uint64(profile.HitDelay/time.Millisecond)
	for _, currentTarget := range targets {
		if isFirebomb {
			modifierPlans = append(modifierPlans, zonenpc.AttackPlan{
				SourceObjectID: source.Plan.ObjectID,
				TargetObjectID: currentTarget.ObjectID,
				SourcePosition: source.Plan.Position,
				TargetPosition: currentTarget.Position,
				Profile:        profile,
			})
			continue
		}
		plan, planErr := zonenpc.PlanAreaAttackWithProfile(
			source, currentTarget.ObjectID, currentTarget.Position, profile,
		)
		if planErr != nil {
			continue
		}
		result, commitErr := zonenpc.CommitAttack(
			peerSession.zone.NPCRandom(), plan,
			source.Plan.NPCProfile.CriticalRating, runtime.program.Critical,
		)
		if commitErr != nil {
			runtime.registry.mutex.Unlock()
			return e.fail("exploderScarabCommit", commitErr)
		}
		hitPackets, targetStatDelta, isApplied, damageErr :=
			runtime.applyEnemyAreaAttackDamage(
				&peerSession, req.generation, plan, result, hitTimestamp,
			)
		if damageErr != nil {
			runtime.registry.mutex.Unlock()
			return e.fail("exploderScarabDamage", damageErr)
		}
		packets = append(packets, hitPackets...)
		statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
		if isApplied {
			modifierPlans = append(modifierPlans, plan)
		}
	}
	damageResult, err := peerSession.zone.NPCs().Defeat(
		req.objectID, req.objectID,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return e.fail("exploderScarabSelfDamage", err)
	}
	transition, err := peerSession.applyCampaignNPCSelfDamageTransition(
		damageResult,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return e.fail("exploderScarabTransition", err)
	}
	binding := peerSession.binding
	runtime.registry.sessions[req.sessionKey] = peerSession
	runtime.registry.mutex.Unlock()
	err = runtime.stats.Record(context.Background(), binding, statDelta)
	if err != nil {
		runtime.logger.Printf(
			"campaign exploder stat persistence omitted object=%d: %v",
			req.objectID, err,
		)
	}
	for index, plan := range modifierPlans {
		var modifierPackets [][]byte
		var modifierErr error
		if isFirebomb {
			modifierPackets, modifierErr = runtime.applyCampaignNPCPoison(
				req.packet, req.sessionKey, req.generation, plan, hitTimestamp,
			)
		} else {
			modifierPackets, modifierErr = runtime.applyCampaignNPCTimedModifier(
				req.packet, req.sessionKey, req.generation, plan, hitTimestamp,
			)
		}
		if modifierErr != nil {
			runtime.logger.Printf(
				"RakNet exploder modifier presentation omitted object=%d modifier=%d: %v",
				req.objectID, index, modifierErr,
			)
			continue
		}
		packets = append(packets, modifierPackets...)
	}
	deathPackets, err := runtime.publishNPCSelfDeath(
		req.packet, req.sessionKey, req.generation, source, damageResult,
		transition, hitTimestamp,
	)
	if err != nil {
		return e.fail("exploderScarabDeath", err)
	}
	return append(packets, deathPackets...), nil
}

func (r campaignNPCActionRuntime) produceExploderScarab(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	request := campaignNPCAttackRequest{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID, timestamp: timestamp,
		kind: campaignNPCAttackExploderScarab,
	}
	isDeferred, err := r.pursuit.deferAction(
		packet, sessionKey, generation, objectID, timestamp, request.resume,
	)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("exploderScarabStun: %w", err)
	}
	if isDeferred {
		return nil, nil
	}
	r.registry.mutex.RLock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound &&
		peerSession.isCampaignNPCSourceActive(generation, objectID) &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.RUnlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(objectID)
	target, isTargetFound := peerSession.campaignNPCTarget(
		generation, source.TargetObjectID,
	)
	r.registry.mutex.RUnlock()
	if !isSourceFound || source.IsDefeated || !isTargetFound {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	profile, isProfileFound := zonenpc.ActionProfileForPlan(source.Plan)
	if !isProfileFound || profile.Family != zonenpc.ActionDetonate {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, nil
	}
	plan, err := zonenpc.PlanAttackWithProfile(
		source, target.ObjectID, target.Position, profile,
		target.FootprintRadius,
	)
	if err != nil {
		action, actionErr := campaignNPCActionWithProfile(
			source.Plan, target.ObjectID, target.Position, profile,
			target.FootprintRadius,
		)
		if actionErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, fmt.Errorf("exploderScarabPursuitAction: %w", actionErr)
		}
		if !action.IsPursuitNeeded {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, nil
		}
		pursuitPackets, marshalErr := npcraknet.Pursuit(action)
		if marshalErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, fmt.Errorf("exploderScarabPursuitMarshal: %w", marshalErr)
		}
		scheduleErr := r.pursuit.schedule(
			packet, sessionKey, generation, objectID, timestamp,
			action.TargetPosition, action.Profile, request.resume,
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			r.logger.Printf(
				"RakNet campaign Exploder Scarab pursuit not scheduled object=%d: %v",
				objectID, scheduleErr,
			)
		}
		return pursuitPackets, nil
	}
	if profile.AbilityName == "ScaldronBasicMonk_Firebomb" {
		alertPacket, alertErr := npcraknet.PositionedEffect(
			profile.TargetEffectName, source.Plan.Position,
		)
		if alertErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			return nil, fmt.Errorf("firebombAlert: %w", alertErr)
		}
		request.timestamp = timestamp + uint64(profile.EmergeDelay/time.Millisecond)
		plan.Profile = profile
		schedule := campaignExploderScarabSchedule{request: request, plan: plan}
		_, scheduleErr := packet.ScheduleProducers(
			[]raknet.ScheduledPacketProducer{{
				Delay: profile.EmergeDelay, Produce: schedule.explode,
			}},
		)
		if scheduleErr != nil {
			r.releaseAction(sessionKey, generation, objectID)
			r.logger.Printf(
				"RakNet campaign Nova Firebomb alert not scheduled object=%d: %v",
				objectID, scheduleErr,
			)
		}
		return [][]byte{alertPacket}, nil
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		r.releaseAction(sessionKey, generation, objectID)
		return nil, fmt.Errorf("exploderScarabStart: %w", err)
	}
	schedule := campaignExploderScarabSchedule{request: request, plan: plan}
	_, scheduleErr := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: profile.HitDelay, Produce: schedule.hit,
	}})
	if scheduleErr != nil {
		r.releaseAction(sessionKey, generation, objectID)
		r.logger.Printf(
			"RakNet campaign Exploder Scarab detonation not scheduled object=%d: %v",
			objectID, scheduleErr,
		)
	}
	return startPackets, nil
}
