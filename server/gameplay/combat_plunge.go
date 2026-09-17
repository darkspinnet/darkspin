package gameplay

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/zone"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

func campaignPlungeTargets(
	targets []zone.NPCTarget, center game.Vec3, radius float32,
) []zone.NPCTarget {
	if radius <= 0 {
		return nil
	}
	selected := make([]zone.NPCTarget, 0, len(targets))
	for _, target := range targets {
		if target.ObjectID == 0 || target.HitPoint <= 0 ||
			!zonegeometry.ContainsSphereStrict(target.Position, center, radius) {
			continue
		}
		selected = append(selected, target)
	}
	sort.Slice(selected, func(left int, right int) bool {
		return selected[left].ObjectID < selected[right].ObjectID
	})
	return selected
}

func (e campaignNPCAttackSchedule) hitVerdanthBasicPlunge() ([][]byte, error) {
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
		return e.fail("enemyPlungeSource", errors.New("unavailable"))
	}
	impactPacket, err := npcraknet.PositionedEffect(
		e.plan.Profile.ImpactEffectName, e.plan.TargetPosition,
	)
	if err != nil {
		runtime.registry.mutex.Unlock()
		return e.fail("enemyPlungeImpact", err)
	}
	targets := campaignPlungeTargets(
		peerSession.zone.LiveNPCTargets(), e.plan.TargetPosition,
		e.plan.Profile.Radius,
	)
	packets := [][]byte{impactPacket}
	statDelta := sporenet.PlayerStatDelta{}
	hitTimestamp := req.timestamp +
		uint64(e.plan.Profile.HitDelay/time.Millisecond)
	for _, target := range targets {
		plan, planErr := zonenpc.PlanAreaAttackWithProfile(
			source, target.ObjectID, target.Position, e.plan.Profile,
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
			return e.fail("enemyPlungeCommit", commitErr)
		}
		hitPackets, targetStatDelta, _, damageErr :=
			runtime.applyEnemyAreaAttackDamage(
				&peerSession, req.generation, plan, result, hitTimestamp,
			)
		if damageErr != nil {
			runtime.registry.mutex.Unlock()
			return e.fail("enemyPlungeDamage", damageErr)
		}
		packets = append(packets, hitPackets...)
		statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
	}
	binding := peerSession.binding
	runtime.registry.sessions[req.sessionKey] = peerSession
	runtime.registry.mutex.Unlock()
	err = runtime.stats.Record(context.Background(), binding, statDelta)
	if err != nil {
		runtime.logger.Printf(
			"RakNet campaign NPC plunge stats omitted object=%d: %v",
			req.objectID, err,
		)
	}
	return packets, nil
}
