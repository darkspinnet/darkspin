package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

type heroTerrifyFleeArrival struct {
	runtime        campaignAbilityCommandRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	targetObjectID uint32
	sourceObjectID uint32
}

func (e heroTerrifyFleeArrival) produce(
	timestamp uint64,
) ([][]byte, error) {
	return e.runtime.startTerrifyFlee(
		e.packet, e.sessionKey, e.generation, e.targetObjectID,
		e.sourceObjectID, timestamp,
	)
}

func (r campaignAbilityCommandRuntime) startTerrifyFlee(
	packet raknet.Packet, sessionKey string, generation uint64,
	targetObjectID uint32, sourceObjectID uint32, timestamp uint64,
) ([][]byte, error) {
	if r.now == nil || r.registry == nil {
		return nil, errors.New("terrify flee unavailable")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	npcSession := peerSession.zone.NPCs()
	if npcSession.FearRemaining(targetObjectID, r.now()) <= 0 {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	target, isTargetFound := npcSession.NPC(targetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	profile, isProfileFound := zonenpc.ActionProfileForNoun(target.Plan.NounName)
	if !isProfileFound || peerSession.zone.NPCRandom() == nil {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	owner := zonenpc.ActionOwner{
		UserID: peerSession.binding.UserID, PeerGeneration: generation,
	}
	if target.TargetObjectID == 0 {
		acquired, _, err := npcSession.AcquireTarget(targetObjectID, sourceObjectID)
		if err != nil {
			r.registry.mutex.Unlock()
			return nil, fmt.Errorf("terrifyTarget: %w", err)
		}
		target = acquired
	}
	if target.IsActionStarted {
		npcSession.ResetAction(targetObjectID)
	}
	started, isStarted, _, err := npcSession.StartAction(
		targetObjectID, owner, target.TargetObjectID,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("terrifyAction: %w", err)
	}
	if !isStarted {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	target = started
	profile.AbilityName = "Flee"
	if profile.MovementSpeed <= 0 {
		profile.MovementSpeed = 5
	}
	profile.Range = max(float32(0.5), target.Plan.NPCProfile.FootprintRadius*0.5)
	sourcePosition := game.Vec3(peerSession.playerPosition)
	destination, isDestinationFound, err := campaignChronoStrikerFleeDestination(
		target.Plan.Position, sourcePosition,
		peerSession.zone.NPCRandom().Float64,
		func(candidate game.Vec3) (game.Vec3, bool, error) {
			return zoneaction.NPCDirectMovementDestination(
				peerSession.zone.Navigation(), target.Plan.Position, candidate,
				target.Plan.NPCProfile.FootprintRadius,
			)
		},
	)
	if err != nil {
		npcSession.ReleaseAction(targetObjectID, owner)
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("terrifyDestination: %w", err)
	}
	if !isDestinationFound {
		npcSession.ReleaseAction(targetObjectID, owner)
		r.registry.mutex.Unlock()
		return nil, nil
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	action := zonenpc.FirstActionPlan{
		ObjectID: targetObjectID, TargetObjectID: target.TargetObjectID,
		SourcePosition: target.Plan.Position, TargetPosition: destination,
		Profile: profile, IsPursuitNeeded: true,
	}
	packets, err := npcraknet.Pursuit(action)
	if err != nil {
		npcSession.ReleaseAction(targetObjectID, owner)
		return nil, fmt.Errorf("terrifyPursuit: %w", err)
	}
	arrival := heroTerrifyFleeArrival{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, targetObjectID: targetObjectID,
		sourceObjectID: sourceObjectID,
	}
	err = r.npc.pursuit.schedule(
		packet, sessionKey, generation, targetObjectID, timestamp,
		destination, profile, arrival.produce,
	)
	if err != nil {
		npcSession.ReleaseAction(targetObjectID, owner)
		return nil, fmt.Errorf("terrifySchedule: %w", err)
	}
	return packets, nil
}

type heroNPCFearExpiryStep struct {
	runtime        campaignAbilityCommandRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	targetObjectID uint32
	timestamp      uint64
	expiresAt      time.Time
	run            *campaignNPCModifierRun
}

func (e heroNPCFearExpiryStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCModifiers[e.run.instanceID] == e.run
	if isCurrent {
		peerSession.untrackCampaignNPCModifier(e.run)
		if peerSession.zone != nil && peerSession.zone.NPCs() != nil {
			peerSession.zone.NPCs().ClearFear(e.targetObjectID, e.expiresAt)
		}
		e.runtime.registry.sessions[e.sessionKey] = peerSession
	}
	e.runtime.registry.mutex.Unlock()
	if !isCurrent {
		return nil, nil
	}
	isCreated, err := e.run.release(e.runtime.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("heroFearRelease: %w", err)
	}
	if !isCreated {
		return nil, nil
	}
	deletePacket, err := effectraknet.ModifierDelete(
		e.targetObjectID, e.run.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("heroFearDelete: %w", err)
	}
	restartPackets, err := e.runtime.npc.restartAfterTerrify(
		e.packet, e.sessionKey, e.generation, e.targetObjectID, e.timestamp,
	)
	if err != nil {
		if e.runtime.logger != nil {
			e.runtime.logger.Printf(
				"RakNet enemy action restart after Terrified omitted target=%d: %v",
				e.targetObjectID, err,
			)
		}
		return [][]byte{deletePacket}, nil
	}
	return append([][]byte{deletePacket}, restartPackets...), nil
}

func (r campaignDamageRuntime) stopHeroNPCFear(
	sessionKey string, generation uint64, targetObjectID uint32,
) ([][]byte, error) {
	if r.registry == nil || r.npc.modifierPool == nil || sessionKey == "" ||
		generation == 0 || targetObjectID == 0 {
		return nil, errors.New("hero fear cleanup invalid")
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	runs := make([]*campaignNPCModifierRun, 0)
	projectileDeletes := make([]heroProjectileStatusDelete, 0)
	for candidateKey, candidate := range r.registry.sessions {
		if candidate.zone == nil || candidate.zone != peerSession.zone {
			continue
		}
		for _, run := range candidate.campaignNPCModifiers {
			if run == nil || run.fearTargetObjectID != targetObjectID {
				continue
			}
			if run.cancel != nil {
				run.cancel()
				run.cancel = nil
			}
			if candidate.zone.NPCs() != nil {
				candidate.zone.NPCs().ClearFear(targetObjectID, run.fearExpiresAt)
			}
			if candidate.zone.Effect() != nil {
				candidate.zone.Effect().Remove(run.instanceID)
			}
			candidate.untrackCampaignNPCModifier(run)
			runs = append(runs, run)
		}
		for _, projectileRun := range candidate.heroProjectileRuns {
			deleted, releaseErr := projectileRun.RemoveFearTarget(targetObjectID)
			if releaseErr != nil && r.logger != nil {
				r.logger.Printf(
					"RakNet Terrified corpse modifier release incomplete target=%d instance=%d: %v",
					targetObjectID, deleted.instanceID, releaseErr,
				)
			}
			if deleted.instanceID != 0 {
				projectileDeletes = append(projectileDeletes, deleted)
			}
		}
		r.registry.sessions[candidateKey] = candidate
	}
	r.registry.mutex.Unlock()
	packets := make([][]byte, 0, len(runs)+len(projectileDeletes))
	for index, run := range runs {
		isCreated, err := run.release(r.npc.modifierPool)
		if err != nil {
			return nil, fmt.Errorf("heroFearCleanupRelease[%d]: %w", index, err)
		}
		if !isCreated {
			continue
		}
		deletePacket, err := effectraknet.ModifierDelete(
			targetObjectID, run.instanceID,
		)
		if err != nil {
			return nil, fmt.Errorf("heroFearCleanupDelete[%d]: %w", index, err)
		}
		packets = append(packets, deletePacket)
	}
	for index, deleted := range projectileDeletes {
		deletePacket, err := effectraknet.ModifierDelete(
			deleted.targetID, deleted.instanceID,
		)
		if err != nil {
			return nil, fmt.Errorf("projectileFearCleanupDelete[%d]: %w", index, err)
		}
		packets = append(packets, deletePacket)
	}
	return packets, nil
}

func (r campaignNPCActionRuntime) restartAfterTerrify(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, error) {
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		!peerSession.isZoneTerminal() && peerSession.zone != nil &&
		peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	npcSession := peerSession.zone.NPCs()
	npc, isNPCFound := npcSession.NPC(objectID)
	if !isNPCFound || npc.IsDefeated || npc.HitPoint <= 0 ||
		npc.TargetObjectID == 0 {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	npcSession.ResetAction(objectID)
	owner := zonenpc.ActionOwner{
		UserID: peerSession.binding.UserID, PeerGeneration: generation,
	}
	started, isStarted, _, err := npcSession.StartAction(
		objectID, owner, npc.TargetObjectID,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, fmt.Errorf("terrifyRestartAction: %w", err)
	}
	if !isStarted {
		r.registry.mutex.Unlock()
		return nil, nil
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	step := campaignNPCFirstActionStep{
		runtime: r, packet: packet.Autonomous(), sessionKey: sessionKey,
		generation: generation, actionGeneration: started.ActionGeneration,
		objectID: objectID, timestamp: timestamp,
	}
	packets, err := step.produce()
	if err != nil {
		return nil, fmt.Errorf("terrifyRestartProduce: %w", err)
	}
	return packets, nil
}

func (r campaignAbilityCommandRuntime) applyHeroNPCFear(
	packet raknet.Packet, sessionKey string, generation uint64,
	targetObjectID uint32, sourceObjectID uint32, duration time.Duration,
	timestamp uint64,
) ([][]byte, error) {
	if r.now == nil || r.registry == nil || r.modifierPool == nil ||
		sessionKey == "" || generation == 0 || targetObjectID == 0 ||
		sourceObjectID == 0 || duration <= 0 {
		return nil, errors.New("hero fear request invalid")
	}
	run, err := newCampaignNPCModifierRun(r.modifierPool)
	if err != nil {
		return nil, fmt.Errorf("heroFearRun: %w", err)
	}
	expiresAt := r.now().Add(duration)
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.generation == generation &&
		peerSession.zone != nil && peerSession.zone.NPCs() != nil
	if !isCurrent {
		r.registry.mutex.Unlock()
		_, releaseErr := run.release(r.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("heroFearStaleRelease: %w", releaseErr)
		}
		return nil, nil
	}
	target, isTargetFound := peerSession.zone.NPCs().NPC(targetObjectID)
	if !isTargetFound || target.IsDefeated || target.HitPoint <= 0 {
		r.registry.mutex.Unlock()
		_, releaseErr := run.release(r.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("heroFearTargetRelease: %w", releaseErr)
		}
		return nil, nil
	}
	err = peerSession.zone.NPCs().ApplyFear(targetObjectID, expiresAt)
	if err != nil {
		r.registry.mutex.Unlock()
		_, releaseErr := run.release(r.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("heroFearApplyRelease: %w", releaseErr)
		}
		return nil, fmt.Errorf("heroFearApply: %w", err)
	}
	if peerSession.zone.NPCs().FearRemaining(targetObjectID, r.now()) <= 0 {
		r.registry.mutex.Unlock()
		_, releaseErr := run.release(r.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("heroFearBlockedRelease: %w", releaseErr)
		}
		return nil, nil
	}
	run.record = zoneeffect.Modifier{
		InstanceID: run.instanceID, GUID: util.HashID("DeathsEmbraceDebuff"),
		SourceObjectID: sourceObjectID, TargetObjectID: targetObjectID,
		Rank: 1, Duration: duration, Kind: zoneeffect.ModifierKindDebuff,
	}
	run.fearTargetObjectID = targetObjectID
	run.fearExpiresAt = expiresAt
	err = peerSession.trackCampaignNPCModifier(run)
	if err != nil {
		peerSession.zone.NPCs().ClearFear(targetObjectID, expiresAt)
		r.registry.mutex.Unlock()
		_, releaseErr := run.release(r.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("heroFearTrackRelease: %w", releaseErr)
		}
		return nil, fmt.Errorf("heroFearTrack: %w", err)
	}
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()

	modifierPacket, err := effectraknet.ModifierCreate(
		effectraknet.ModifierCreateRequest{
			SourceObjectID: sourceObjectID, TargetObjectID: targetObjectID,
			ModifierID: run.record.GUID, InstanceID: run.instanceID,
			Duration: duration, Timestamp: timestamp,
		},
	)
	if err != nil {
		r.registry.mutex.Lock()
		latest, isLatestFound := r.registry.sessions[sessionKey]
		if isLatestFound && latest.generation == generation &&
			latest.campaignNPCModifiers[run.instanceID] == run {
			latest.untrackCampaignNPCModifier(run)
			latest.zone.NPCs().ClearFear(targetObjectID, expiresAt)
			r.registry.sessions[sessionKey] = latest
		}
		r.registry.mutex.Unlock()
		_, releaseErr := run.release(r.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("heroFearCreateRelease: %w", releaseErr)
		}
		return nil, fmt.Errorf("heroFearCreate: %w", err)
	}
	expiry := heroNPCFearExpiryStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, targetObjectID: targetObjectID,
		timestamp: timestamp + uint64(duration/time.Millisecond),
		expiresAt: expiresAt, run: run,
	}
	producers := r.registry.producerGuard.scheduledProducers(
		sessionKey, []raknet.ScheduledPacketProducer{{
			Delay: duration, Produce: expiry.produce,
		}},
	)
	cancel, scheduleErr := packet.ScheduleProducers(producers)
	if scheduleErr == nil && cancel == nil {
		scheduleErr = errors.New("hero fear cancellation unavailable")
	}
	if scheduleErr != nil {
		r.registry.mutex.Lock()
		latest, isLatestFound := r.registry.sessions[sessionKey]
		if isLatestFound && latest.generation == generation &&
			latest.campaignNPCModifiers[run.instanceID] == run {
			latest.untrackCampaignNPCModifier(run)
			latest.zone.NPCs().ClearFear(targetObjectID, expiresAt)
			r.registry.sessions[sessionKey] = latest
		}
		r.registry.mutex.Unlock()
		_, releaseErr := run.release(r.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("heroFearScheduleRelease: %w", releaseErr)
		}
		return nil, fmt.Errorf("heroFearSchedule: %w", scheduleErr)
	}
	r.registry.mutex.Lock()
	latest, isLatestFound := r.registry.sessions[sessionKey]
	isLatest := isLatestFound && latest.generation == generation &&
		latest.campaignNPCModifiers[run.instanceID] == run
	if isLatest {
		run.cancel = cancel
	}
	r.registry.mutex.Unlock()
	if !isLatest {
		cancel()
		_, releaseErr := run.release(r.modifierPool)
		if releaseErr != nil {
			return nil, fmt.Errorf("heroFearStaleScheduleRelease: %w", releaseErr)
		}
		return nil, nil
	}
	run.create()
	packets := [][]byte{modifierPacket}
	fleePackets, err := r.startTerrifyFlee(
		packet, sessionKey, generation, targetObjectID, sourceObjectID, timestamp,
	)
	if err != nil {
		if r.logger != nil {
			r.logger.Printf(
				"RakNet Death's Embrace fear movement skipped target=%d: %v",
				targetObjectID, err,
			)
		}
		return packets, nil
	}
	return append(packets, fleePackets...), nil
}
