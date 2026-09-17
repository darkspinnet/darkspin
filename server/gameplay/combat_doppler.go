package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

const campaignDopplerFakeLifetime = 10 * time.Second

func campaignDopplerStrafeCandidate(
	position game.Vec3, distance float32, angle float64,
) game.Vec3 {
	return game.Vec3{
		X: position.X + float32(math.Cos(angle))*distance,
		Y: position.Y + float32(math.Sin(angle))*distance,
		Z: position.Z,
	}
}

type campaignDopplerCloneStep struct {
	runtime      campaignNPCActionRuntime
	packet       raknet.Packet
	sessionKey   string
	generation   uint64
	objectID     uint32
	fakeObjectID uint32
	timestamp    uint64
	profile      zonenpc.ActionProfile
}

func (e campaignDopplerCloneStep) spawn() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		e.generation, e.objectID,
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	source, isSourceFound := peerSession.zone.NPCs().NPC(e.objectID)
	if !isSourceFound || source.IsDefeated {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	random := peerSession.zone.NPCRandom()
	if random == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, errors.New("doppler clone random unavailable")
	}
	strafeDistance := e.profile.MinimumRange +
		float32(random.Float64())*(e.profile.Range-e.profile.MinimumRange)
	strafeAngle := random.Float64() * 2 * math.Pi
	footprintRadius := max(source.Plan.NPCProfile.FootprintRadius, 0.25)
	sourceDestination := source.Plan.Position
	fakeDestination := source.Plan.Position
	isStrafeAvailable := false
	for attempt := range 4 {
		candidateAngle := strafeAngle + float64(attempt)*math.Pi/2
		projectedSource, isSourceFound, err := navigationClippedMovementDestination(
			peerSession.zone.Navigation(), source.Plan.Position,
			campaignDopplerStrafeCandidate(
				source.Plan.Position, strafeDistance, candidateAngle,
			),
			footprintRadius,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("dopplerSourceDestination[%d]: %w", attempt, err)
		}
		projectedFake, isFakeFound, err := navigationClippedMovementDestination(
			peerSession.zone.Navigation(), source.Plan.Position,
			campaignDopplerStrafeCandidate(
				source.Plan.Position, strafeDistance, candidateAngle+math.Pi,
			),
			footprintRadius,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("dopplerFakeDestination[%d]: %w", attempt, err)
		}
		if !isSourceFound || !isFakeFound {
			continue
		}
		sourceDestination = projectedSource
		fakeDestination = projectedFake
		isStrafeAvailable = true
		break
	}
	plan := source.Plan.Clone()
	plan.ObjectID = e.fakeObjectID
	plan.NounName = e.profile.RetainedObjectNoun
	plan.Experience = 0
	plan.LocusID = 0
	plan.Kind = 0
	plan.IsCaptain = false
	plan.IsBoss = false
	plan.IsRewardSuppressed = true
	plan.MarkerSetName = ""
	plan.BossIdentity = zonenpc.BossIdentity{}
	plan.ActionProfile = zonenpc.ActionProfile{}
	plan.IsActionKnown = false
	plan.NPCProfile.HitPoint = source.HitPoint
	spawnPackets, err := npcraknet.TargetedSpawn(plan, source.TargetObjectID)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("dopplerCloneMarshal: %w", err)
	}
	err = peerSession.zone.NPCs().Add(
		[]zonenpc.SpawnPlan{plan}, source.TargetObjectID,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("dopplerCloneAdd: %w", err)
	}
	if peerSession.campaignNPCDopplerFakeObjectIDs == nil {
		peerSession.campaignNPCDopplerFakeObjectIDs = make(map[uint32]uint32)
	}
	peerSession.campaignNPCDopplerFakeObjectIDs[e.objectID] = e.fakeObjectID
	err = peerSession.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: []zonenpc.SpawnPlan{plan}, TargetObjectID: source.TargetObjectID,
	}, peerSession.binding.UserID, e.generation)
	if err != nil {
		delete(peerSession.campaignNPCDopplerFakeObjectIDs, e.objectID)
		rollbackErr := peerSession.zone.NPCs().RollbackAdd([]zonenpc.SpawnPlan{plan})
		e.runtime.registry.mutex.Unlock()
		if rollbackErr != nil {
			return nil, fmt.Errorf(
				"dopplerCloneRollback: %w", errors.Join(err, rollbackErr),
			)
		}
		return nil, fmt.Errorf("dopplerClonePublish: %w", err)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	packets := make([][]byte, 0, len(spawnPackets)+6)
	packets = append(packets, spawnPackets...)
	splitPacket, err := npcraknet.PositionedEffect(
		e.profile.ImpactEffectName, source.Plan.Position,
	)
	if err != nil {
		return nil, fmt.Errorf("dopplerCloneSplit: %w", err)
	}
	shaderPacket, err := npcraknet.PositionedEffect(
		e.profile.RetainedEffectName, plan.Position,
	)
	if err != nil {
		return nil, fmt.Errorf("dopplerCloneShader: %w", err)
	}
	packets = append(packets, splitPacket, shaderPacket)
	strafeDuration := time.Duration(0)
	producers := make([]raknet.ScheduledPacketProducer, 0, 2)
	if isStrafeAvailable {
		strafeTravel := max(
			zonegeometry.Distance(source.Plan.Position, sourceDestination),
			zonegeometry.Distance(plan.Position, fakeDestination),
		)
		strafeDuration = time.Duration(
			float64(strafeTravel/e.profile.MovementSpeed) * float64(time.Second),
		)
		sourcePackets, movementErr := npcraknet.PursuitRedirect(
			e.objectID, source.Plan.Position, source.TargetObjectID,
			sourceDestination, 0.1,
		)
		if movementErr != nil {
			return nil, fmt.Errorf("dopplerSourceStrafe: %w", movementErr)
		}
		fakePackets, movementErr := npcraknet.PursuitRedirect(
			e.fakeObjectID, plan.Position, source.TargetObjectID,
			fakeDestination, 0.1,
		)
		if movementErr != nil {
			return nil, fmt.Errorf("dopplerFakeStrafe: %w", movementErr)
		}
		packets = append(packets, sourcePackets...)
		packets = append(packets, fakePackets...)
		arrival := campaignDopplerStrafeArrival{
			runtime: e.runtime, sessionKey: e.sessionKey,
			generation: e.generation, sourceObjectID: e.objectID,
			fakeObjectID: e.fakeObjectID,
			sourceOrigin: source.Plan.Position, fakeOrigin: plan.Position,
			sourceDestination: sourceDestination, fakeDestination: fakeDestination,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: strafeDuration, Produce: arrival.produce,
		})
	}
	expiryDelay := strafeDuration + campaignDopplerFakeLifetime
	expiry := campaignDopplerFakeExpiry{
		runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
		generation: e.generation, sourceObjectID: e.objectID,
		fakeObjectID: e.fakeObjectID,
		timestamp: e.timestamp + uint64(e.profile.HitDelay/time.Millisecond) +
			uint64(expiryDelay/time.Millisecond),
	}
	producers = append(producers, raknet.ScheduledPacketProducer{
		Delay: expiryDelay, Produce: expiry.produce,
	})
	_, err = e.packet.ScheduleProducers(producers)
	if err != nil {
		return nil, fmt.Errorf("dopplerCloneExpirySchedule: %w", err)
	}
	return packets, nil
}

func (e campaignDopplerCloneStep) next() ([][]byte, error) {
	resumeDelay := max(e.profile.HitDelay, e.profile.ReleaseDelay)
	timestamp := e.timestamp + uint64(resumeDelay/time.Millisecond)
	return e.runtime.produceZelemShot(
		e.packet, e.sessionKey, e.generation, e.objectID, timestamp,
	)
}

type campaignDopplerStrafeArrival struct {
	runtime           campaignNPCActionRuntime
	sessionKey        string
	generation        uint64
	sourceObjectID    uint32
	fakeObjectID      uint32
	sourceOrigin      game.Vec3
	fakeOrigin        game.Vec3
	sourceDestination game.Vec3
	fakeDestination   game.Vec3
}

func (e campaignDopplerStrafeArrival) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCDopplerFakeObjectIDs[e.sourceObjectID] == e.fakeObjectID
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	packets := make([][]byte, 0, 6)
	if _, isSourceFound := peerSession.zone.NPCs().LiveNPC(e.sourceObjectID); isSourceFound {
		err := peerSession.zone.NPCs().SetPosition(
			e.sourceObjectID, e.sourceDestination,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("dopplerSourceArrival: %w", err)
		}
		sourcePackets, marshalErr := npcraknet.ChargeCleanup(
			e.sourceObjectID, e.sourceDestination,
			e.sourceDestination.Sub(e.sourceOrigin),
		)
		if marshalErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("dopplerSourceStop: %w", marshalErr)
		}
		packets = append(packets, sourcePackets...)
	}
	if _, isFakeFound := peerSession.zone.NPCs().LiveNPC(e.fakeObjectID); isFakeFound {
		err := peerSession.zone.NPCs().SetPosition(
			e.fakeObjectID, e.fakeDestination,
		)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("dopplerFakeArrival: %w", err)
		}
		fakePackets, marshalErr := npcraknet.ChargeCleanup(
			e.fakeObjectID, e.fakeDestination,
			e.fakeDestination.Sub(e.fakeOrigin),
		)
		if marshalErr != nil {
			e.runtime.registry.mutex.Unlock()
			return nil, fmt.Errorf("dopplerFakeStop: %w", marshalErr)
		}
		packets = append(packets, fakePackets...)
	}
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	return packets, nil
}

type campaignDopplerFakeExpiry struct {
	runtime        campaignNPCActionRuntime
	packet         raknet.Packet
	sessionKey     string
	generation     uint64
	sourceObjectID uint32
	fakeObjectID   uint32
	timestamp      uint64
}

func (e campaignDopplerFakeExpiry) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	peerSession, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && peerSession.generation == e.generation &&
		peerSession.campaignNPCDopplerFakeObjectIDs[e.sourceObjectID] == e.fakeObjectID
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	fake, isFakeFound := peerSession.zone.NPCs().LiveNPC(e.fakeObjectID)
	if !isFakeFound {
		delete(peerSession.campaignNPCDopplerFakeObjectIDs, e.sourceObjectID)
		e.runtime.registry.sessions[e.sessionKey] = peerSession
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	result, err := peerSession.zone.NPCs().Damage(
		e.fakeObjectID, e.fakeObjectID, fake.HitPoint,
	)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("dopplerFakeDamage: %w", err)
	}
	transition, err := peerSession.applyCampaignNPCSelfDamageTransition(result)
	if err != nil {
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("dopplerFakeTransition: %w", err)
	}
	delete(peerSession.campaignNPCDopplerFakeObjectIDs, e.sourceObjectID)
	e.runtime.registry.sessions[e.sessionKey] = peerSession
	e.runtime.registry.mutex.Unlock()
	effectPacket, err := npcraknet.PositionedEffect(
		"shadow_doppler_death_effect.ServerEventDef", fake.Plan.Position,
	)
	if err != nil {
		return nil, fmt.Errorf("dopplerFakeDeathEffect: %w", err)
	}
	deathPackets, err := e.runtime.publishNPCSelfDeath(
		e.packet, e.sessionKey, e.generation, fake, result, transition, e.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("dopplerFakeDeathPublish: %w", err)
	}
	return append([][]byte{effectPacket}, deathPackets...), nil
}

func (r campaignNPCActionRuntime) produceScaldronBasicDopplerClone(
	packet raknet.Packet, sessionKey string, generation uint64,
	source zonenpc.Snapshot, timestamp uint64,
) ([][]byte, bool, error) {
	if sessionKey == "" || generation == 0 || source.Plan.ObjectID == 0 {
		return nil, false, errors.New("doppler clone request invalid")
	}
	profile, isProfileFound := zonenpc.ScaldronBasicDopplerCloneProfile(
		source.Plan.NounName,
	)
	if !isProfileFound {
		return nil, false, nil
	}
	r.registry.mutex.Lock()
	peerSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && peerSession.isCampaignNPCSourceActive(
		generation, source.Plan.ObjectID,
	)
	if !isCurrent ||
		peerSession.campaignNPCDopplerCloneReadiness[source.Plan.ObjectID] > timestamp {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	existingObjectID := peerSession.campaignNPCDopplerFakeObjectIDs[source.Plan.ObjectID]
	if _, isExistingFound := peerSession.zone.NPCs().LiveNPC(existingObjectID); isExistingFound {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	nearby, err := peerSession.zone.NPCs().ActorsInSphere(zonenpc.SphereRequest{
		Center: source.Plan.Position, Radius: 50,
	})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("dopplerClonePopulation: %w", err)
	}
	if len(nearby) >= 15 {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	fakeObjectID, err := peerSession.reserveCampaignObjectID()
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("dopplerCloneReserve: %w", err)
	}
	if peerSession.campaignNPCDopplerCloneReadiness == nil {
		peerSession.campaignNPCDopplerCloneReadiness = make(map[uint32]uint64)
	}
	peerSession.campaignNPCDopplerCloneReadiness[source.Plan.ObjectID] = timestamp +
		uint64(profile.Cooldown/time.Millisecond)
	r.registry.sessions[sessionKey] = peerSession
	r.registry.mutex.Unlock()
	plan := zonenpc.AttackPlan{
		ActionGeneration: source.ActionGeneration,
		SourceObjectID:   source.Plan.ObjectID,
		TargetObjectID:   source.Plan.ObjectID,
		SourcePosition:   source.Plan.Position,
		TargetPosition:   source.Plan.Position,
		Profile:          profile,
	}
	startPackets, err := r.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		r.releaseAction(sessionKey, generation, source.Plan.ObjectID)
		return nil, false, fmt.Errorf("dopplerCloneStart: %w", err)
	}
	step := campaignDopplerCloneStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: source.Plan.ObjectID,
		fakeObjectID: fakeObjectID, timestamp: timestamp, profile: profile,
	}
	resumeDelay := max(profile.HitDelay, profile.ReleaseDelay)
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: step.spawn},
		{Delay: resumeDelay, Produce: step.next},
	})
	if err == nil && cancel == nil {
		err = errors.New("nil cancellation")
	}
	if err != nil {
		r.releaseAction(sessionKey, generation, source.Plan.ObjectID)
		return nil, false, fmt.Errorf("dopplerCloneSchedule: %w", err)
	}
	return startPackets, true, nil
}
