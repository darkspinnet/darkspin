package gameplay

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const nomadDragMeteorTargetDelay = 300 * time.Millisecond

type campaignNPCMeteorRun struct {
	mutex    sync.RWMutex
	position game.Vec3
}

func (e *campaignNPCMeteorRun) positionSnapshot() game.Vec3 {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.position
}

func (e *campaignNPCMeteorRun) setPosition(position game.Vec3) {
	e.mutex.Lock()
	e.position = position
	e.mutex.Unlock()
}

func (e campaignNPCAttackSchedule) sampleNomadDragMeteor() ([][]byte, error) {
	if e.meteor == nil {
		return e.fail("enemyMeteorRun", errors.New("unavailable"))
	}
	runtime := e.request.runtime
	runtime.registry.mutex.RLock()
	peerSession, isFound := runtime.registry.sessions[e.request.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCAttackActiveAt(
		e.request.generation, e.request.objectID, e.plan.TargetObjectID,
		runtime.now(),
	)
	target, isTargetFound := peerSession.campaignNPCTarget(
		e.request.generation, e.plan.TargetObjectID,
	)
	runtime.registry.mutex.RUnlock()
	if !isCurrent || !isTargetFound {
		return nil, nil
	}
	e.meteor.setPosition(target.Position)
	return nil, nil
}

func (e campaignNPCAttackSchedule) hitNomadDragMeteor() ([][]byte, error) {
	req := e.request
	runtime := req.runtime
	runtime.registry.mutex.Lock()
	peerSession, isFound := runtime.registry.sessions[req.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCAttackActiveAt(
		req.generation, req.objectID, e.plan.TargetObjectID, runtime.now(),
	)
	if !isCurrent {
		runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(req.objectID)
	if !isSourceFound || source.IsDefeated || peerSession.zone.NPCRandom() == nil {
		runtime.registry.mutex.Unlock()
		return e.fail("enemyMeteorSource", errors.New("unavailable"))
	}
	center := e.plan.TargetPosition
	if e.meteor != nil {
		center = e.meteor.positionSnapshot()
	}
	targets := campaignLobAreaTargets(
		peerSession.zone.LiveNPCTargets(), center,
		e.plan.Profile.Radius,
	)
	packets := make([][]byte, 0)
	statDelta := sporenet.PlayerStatDelta{}
	hitTimestamp := req.timestamp +
		uint64(e.plan.Profile.HitDelay/time.Millisecond)
	for _, target := range targets {
		plan, err := zonenpc.PlanAreaAttackWithProfile(
			source, target.ObjectID, target.Position, e.plan.Profile,
		)
		if err != nil {
			continue
		}
		result, err := zonenpc.CommitAttack(
			peerSession.zone.NPCRandom(), plan,
			source.Plan.NPCProfile.CriticalRating, runtime.program.Critical,
		)
		if err != nil {
			runtime.registry.mutex.Unlock()
			return e.fail("enemyMeteorCommit", err)
		}
		hitPackets, targetStatDelta, _, err := runtime.applyEnemyAreaAttackDamage(
			&peerSession, req.generation, plan, result, hitTimestamp,
		)
		if err != nil {
			runtime.registry.mutex.Unlock()
			return e.fail("enemyMeteorDamage", err)
		}
		packets = append(packets, hitPackets...)
		statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
	}
	binding := peerSession.binding
	runtime.registry.sessions[req.sessionKey] = peerSession
	runtime.registry.mutex.Unlock()
	err := runtime.stats.Record(context.Background(), binding, statDelta)
	if err != nil {
		runtime.logger.Printf(
			"RakNet campaign NPC meteor stats omitted object=%d: %v",
			req.objectID, err,
		)
	}
	return packets, nil
}
