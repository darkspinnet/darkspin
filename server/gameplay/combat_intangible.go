package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type campaignNPCIntangibleRun struct {
	modifier          *campaignNPCModifierRun
	objectID          uint32
	expiresAt         time.Time
	cancel            raknet.CancelSchedule
	timestamp         uint64
	destination       game.Vec3
	revealPacket      []byte
	effectPool        *attachedEffectPool
	effectSlot        uint8
	isEffectAllocated bool
	effectStopPacket  []byte
}

// Call while holding the registry lock, alongside removal of the burrow run.
func (e *campaignNPCIntangibleRun) releaseEffect() []byte {
	if !e.isEffectAllocated {
		return nil
	}
	e.isEffectAllocated = false
	if !e.effectPool.Release(e.objectID, e.effectSlot) {
		// Object cleanup already reclaimed this slot; do not stop a new owner.
		return nil
	}
	return e.effectStopPacket
}

type campaignNPCIntangibleExpiry struct {
	runtime    campaignNPCActionRuntime
	sessionKey string
	generation uint64
	run        *campaignNPCIntangibleRun
}

func (e campaignNPCIntangibleExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.campaignNPCIntangibles[e.run.objectID] == e.run
	var enemy zonenpc.Snapshot
	isEnemyFound := false
	var positionErr error
	packets := make([][]byte, 0, 4)
	if isCurrent {
		stopPacket := e.run.releaseEffect()
		if stopPacket != nil {
			packets = append(packets, stopPacket)
		}
		enemy, isEnemyFound = peerSession.zone.NPCs().NPC(e.run.objectID)
		if isEnemyFound && !enemy.IsDefeated && enemy.HitPoint > 0 {
			positionErr = peerSession.zone.NPCs().SetPosition(
				e.run.objectID, e.run.destination,
			)
			if positionErr == nil {
				enemy, isEnemyFound = peerSession.zone.NPCs().NPC(e.run.objectID)
			}
		}
		delete(peerSession.campaignNPCIntangibles, e.run.objectID)
		peerSession.zone.NPCs().ClearIntangible(e.run.objectID, e.run.expiresAt)
		peerSession.untrackCampaignNPCModifier(e.run.modifier)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	isCreated, err := e.run.modifier.release(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("intangibleRelease: %w", err)
	}
	if !isCreated {
		return packets, nil
	}
	if positionErr != nil {
		return nil, fmt.Errorf("intangiblePosition: %w", positionErr)
	}
	if !isEnemyFound {
		return packets, nil
	}
	if enemy.IsDefeated || enemy.HitPoint <= 0 {
		return packets, nil
	}
	arrivalPackets, err := npcraknet.BurrowArrival(
		e.run.objectID, enemy.Plan.Position, enemy.Facing, e.run.timestamp+1500,
	)
	if err != nil {
		return nil, fmt.Errorf("intangibleEmerge: %w", err)
	}
	return append(packets, arrivalPackets...), nil
}

func (r campaignNPCActionRuntime) prepareStagnantNovaIntangible(
	sessionKey string, generation uint64, plan zonenpc.AttackPlan,
	timestamp uint64,
) ([][]byte, *campaignNPCIntangibleRun, error) {
	profile := plan.Profile
	if sessionKey == "" || generation == 0 || plan.SourceObjectID == 0 ||
		profile.AbilityName != "StagnantNovaAbove" ||
		profile.EmergeDelay != 1500*time.Millisecond {
		return nil, nil, errors.New("stagnant intangible request invalid")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.isCampaignNPCSourceActive(generation, plan.SourceObjectID) &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil, nil
	}
	if peerSession.campaignNPCIntangibles == nil {
		peerSession.campaignNPCIntangibles = make(map[uint32]*campaignNPCIntangibleRun)
	}
	if peerSession.campaignNPCIntangibles[plan.SourceObjectID] != nil {
		r.registry.mutex.Unlock()
		return nil, nil, nil
	}
	modifier, err := newCampaignNPCModifierRun(r.modifierPool)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, nil, fmt.Errorf("intangibleRun: %w", err)
	}
	run := &campaignNPCIntangibleRun{
		modifier: modifier, objectID: plan.SourceObjectID,
		expiresAt: r.now().Add(profile.EmergeDelay), timestamp: timestamp,
		destination: plan.TargetPosition, effectPool: r.effectPool,
	}
	run.effectSlot, run.isEffectAllocated = r.effectPool.Allocate(run.objectID)
	var effectPacket []byte
	var effectErr error
	if run.isEffectAllocated {
		effectPacket, effectErr = npcraknet.BurrowEffect(run.objectID, run.effectSlot, false)
		if effectErr == nil {
			run.effectStopPacket, effectErr = npcraknet.BurrowEffect(run.objectID, run.effectSlot, true)
		}
	} else if r.logger != nil {
		// Presentation slot exhaustion must not cancel the working attack.
		r.logger.Printf("RakNet burrow trail omitted object=%d: attachment slots exhausted", run.objectID)
	}
	if effectErr != nil {
		run.releaseEffect()
		r.registry.mutex.Unlock()
		isReleased, releaseErr := modifier.release(r.modifierPool)
		if isReleased {
			return nil, nil, errors.New("burrow modifier unexpectedly published")
		}
		return nil, nil, fmt.Errorf("burrowEffect: %w", errors.Join(effectErr, releaseErr))
	}
	err = peerSession.zone.NPCs().ApplyIntangible(run.objectID, run.expiresAt)
	if err != nil {
		run.releaseEffect()
		r.registry.mutex.Unlock()
		_, releaseErr := modifier.release(r.modifierPool)
		return nil, nil, fmt.Errorf(
			"intangibleApply: %w", errors.Join(err, releaseErr),
		)
	}
	err = peerSession.trackCampaignNPCModifier(modifier)
	if err != nil {
		peerSession.zone.NPCs().ClearIntangible(run.objectID, run.expiresAt)
		run.releaseEffect()
		r.registry.mutex.Unlock()
		_, releaseErr := modifier.release(r.modifierPool)
		return nil, nil, fmt.Errorf(
			"intangibleTrack: %w", errors.Join(err, releaseErr),
		)
	}
	peerSession.campaignNPCIntangibles[run.objectID] = run
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	// The server-side intangible state rejects hits during travel. Publishing the
	// corresponding client modifier hides the entire Tunneler and suppresses its
	// authored burrow and emerge animation states.
	travelPackets, err := npcraknet.BurrowTravel(plan, timestamp)
	if err != nil {
		r.rollbackStagnantNovaIntangible(sessionKey, generation, run)
		return nil, nil, fmt.Errorf("intangibleTravel: %w", err)
	}
	run.revealPacket, err = raknet.MarshalApplication(raknet.ObjectUpdateMessage{
		ObjectID: plan.SourceObjectID, IsVisible: true,
		PositionX: plan.SourcePosition.X, PositionY: plan.SourcePosition.Y,
		PositionZ: plan.SourcePosition.Z,
	})
	if err != nil {
		r.rollbackStagnantNovaIntangible(sessionKey, generation, run)
		return nil, nil, fmt.Errorf("intangibleRecovery: %w", err)
	}
	if effectPacket != nil {
		travelPackets = append(travelPackets, effectPacket)
	}
	return travelPackets, run, nil
}

func (r campaignNPCActionRuntime) activateStagnantNovaIntangible(
	sessionKey string, generation uint64, run *campaignNPCIntangibleRun,
	cancel raknet.CancelSchedule,
) bool {
	if run == nil || cancel == nil {
		return false
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.campaignNPCIntangibles[run.objectID] == run
	if isCurrent {
		run.cancel = cancel
		run.modifier.create()
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	return isCurrent
}

func (r campaignNPCActionRuntime) rollbackStagnantNovaIntangible(
	sessionKey string, generation uint64, run *campaignNPCIntangibleRun,
) {
	if run == nil {
		return
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil &&
		peerSession.campaignNPCIntangibles[run.objectID] == run
	if isCurrent {
		run.releaseEffect()
		delete(peerSession.campaignNPCIntangibles, run.objectID)
		peerSession.zone.NPCs().ClearIntangible(run.objectID, run.expiresAt)
		peerSession.untrackCampaignNPCModifier(run.modifier)
		r.registry.sessions[sessionKey] = peerSession
	}
	r.registry.mutex.Unlock()
	if isCurrent {
		_, _ = run.modifier.release(r.modifierPool)
	}
}
