package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
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
	runtime    campaignDamageRuntime
	sessionKey string
	generation uint64
	run        *campaignNPCModifierRun
	targetID   uint32
}

func (e heroPullExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCModifiers[e.run.instanceID] == e.run
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	peerSession.zone.Effect().Remove(e.run.instanceID)
	peerSession.untrackCampaignNPCModifier(e.run)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	isCreated, err := e.run.release(e.runtime.npc.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("heroPullRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	packet, err := effectraknet.ModifierDelete(e.targetID, e.run.instanceID)
	if err != nil {
		return nil, fmt.Errorf("heroPullDelete: %w", err)
	}
	return [][]byte{packet}, nil
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
	hero, isHeroFound := peerSession.zone.Hero().Snapshot(
		peerSession.binding.UserID, generation,
	)
	target, isTargetFound := peerSession.zone.NPCs().NPC(result.Damage.ObjectID)
	if !isHeroFound || hero.ObjectID != sourceObjectID || !isTargetFound ||
		target.IsDefeated || target.IsTurtleActive {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	modifierID := plan.Definition.RootModifierID
	duration := binarySentinelPullOutro
	movementPackets := make([][]byte, 0)
	if target.Plan.IsBoss {
		modifierID = util.HashID("PushPullBossModifier")
		duration = binarySentinelBossPullDuration
	} else if peerSession.zone.NPCs().RootRemaining(
		target.Plan.ObjectID, e.npc.now(),
	) <= 0 {
		delta := hero.Position.Sub(target.Plan.Position)
		distance := delta.Length()
		edgeDistance := distance - max(float32(0), hero.FootprintRadius) -
			max(float32(0), target.Plan.NPCProfile.FootprintRadius)
		if distance > 0 && edgeDistance > 0 {
			desired := target.Plan.Position.Add(delta.Scale(edgeDistance / distance))
			destination, isDestinationFound, err :=
				zoneaction.NPCDirectMovementDestination(
					peerSession.zone.Navigation(), target.Plan.Position, desired,
					max(target.Plan.NPCProfile.FootprintRadius, float32(0.25)),
				)
			if err != nil {
				e.registry.mutex.Unlock()
				return nil, fmt.Errorf("heroPullDestination: %w", err)
			}
			if isDestinationFound {
				attackPlan := zonenpc.AttackPlan{
					SourceObjectID: sourceObjectID,
					TargetObjectID: target.Plan.ObjectID,
					SourcePosition: hero.Position,
					TargetPosition: target.Plan.Position,
					Profile: zonenpc.ActionProfile{
						ForcedMovementSpeed:        binarySentinelPullSpeed,
						ForcedMovementDistance:     edgeDistance,
						ForcedMovementReactionName: "react_pulled",
					},
				}
				movementPackets, err = npcraknet.ForcedMovement(
					attackPlan, destination, timestamp,
				)
				if err != nil {
					e.registry.mutex.Unlock()
					return nil, fmt.Errorf("heroPullMarshal: %w", err)
				}
				err = peerSession.zone.NPCs().SetPosition(
					target.Plan.ObjectID, destination,
				)
				if err != nil {
					e.registry.mutex.Unlock()
					return nil, fmt.Errorf("heroPullMove: %w", err)
				}
				duration += time.Duration(
					float64(edgeDistance/binarySentinelPullSpeed) * float64(time.Second),
				)
			}
		}
	}
	run, err := newCampaignNPCModifierRun(e.npc.modifierPool)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroPullRun: %w", err)
	}
	run.record = zoneeffect.Modifier{
		InstanceID: run.instanceID, GUID: modifierID,
		SourceObjectID: sourceObjectID, TargetObjectID: target.Plan.ObjectID,
		Rank: 1, Duration: duration, Kind: zoneeffect.ModifierKindDebuff,
		InitiatorObject: sourceObjectID, StackCount: 1,
	}
	err = peerSession.trackCampaignNPCModifier(run)
	if err == nil {
		err = peerSession.zone.Effect().Put(run.record)
	}
	if err != nil {
		peerSession.untrackCampaignNPCModifier(run)
		_, releaseErr := run.release(e.npc.modifierPool)
		e.registry.mutex.Unlock()
		return nil, fmt.Errorf("heroPullTrack: %w", errors.Join(err, releaseErr))
	}
	e.registry.sessions[sessionKey] = peerSession
	e.registry.mutex.Unlock()

	createPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: sourceObjectID, TargetObjectID: target.Plan.ObjectID,
		ModifierID: modifierID, InstanceID: run.instanceID,
		StackCount: 1, Duration: duration, Timestamp: timestamp,
	})
	if err != nil {
		e.rollbackHeroPull(sessionKey, generation, run)
		return nil, fmt.Errorf("heroPullCreate: %w", err)
	}
	expiry := heroPullExpiry{
		runtime: e, sessionKey: sessionKey, generation: generation,
		run: run, targetID: target.Plan.ObjectID,
	}
	cancel, scheduleErr := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{{
		Delay: duration, Produce: expiry.produce,
	}})
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("nil cancellation")
	}
	if scheduleErr != nil {
		e.rollbackHeroPull(sessionKey, generation, run)
		return nil, fmt.Errorf("heroPullSchedule: %w", scheduleErr)
	}
	e.registry.mutex.Lock()
	latest, isLatestFound := e.registry.sessions[sessionKey]
	isLatest := isLatestFound && latest.generation == generation &&
		latest.campaignNPCModifiers[run.instanceID] == run
	if isLatest {
		run.cancel = cancel
		e.registry.sessions[sessionKey] = latest
	}
	e.registry.mutex.Unlock()
	if !isLatest {
		cancel()
		return nil, nil
	}
	run.create()
	packets := append([][]byte{createPacket}, movementPackets...)
	return packets, nil
}

func (e campaignDamageRuntime) rollbackHeroPull(
	sessionKey string, generation uint64, run *campaignNPCModifierRun,
) {
	e.registry.mutex.Lock()
	peerSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.campaignNPCModifiers[run.instanceID] == run
	if isCurrent {
		peerSession.zone.Effect().Remove(run.instanceID)
		peerSession.untrackCampaignNPCModifier(run)
		e.registry.sessions[sessionKey] = peerSession
	}
	e.registry.mutex.Unlock()
	if isCurrent {
		_, _ = run.release(e.npc.modifierPool)
	}
}
