package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
)

// One parent charge selects a child at release. Its projectile/pool has a
// separate lifetime: releasing the action cannot retract already thrown goop.
type nomadFrogCast struct {
	runtime      campaignNPCActionRuntime
	packet       raknet.Packet
	sessionKey   string
	generation   uint64
	timestamp    uint64
	plan         zonenpc.AttackPlan
	source       zonenpc.Snapshot
	readyAt      time.Time
	child        zonenpc.ActionProfile
	toss         zoneability.TossPlan
	projectileID uint32
	isLaunched   bool
	isLanded     bool
}

func (e campaignNPCActionRuntime) produceNomadFrogAction(packet raknet.Packet, sessionKey string, generation uint64, objectID uint32, timestamp uint64) ([][]byte, bool, error) {
	e.registry.mutex.RLock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || !member.isCampaignNPCSourceActive(generation, objectID) {
		e.registry.mutex.RUnlock()
		return nil, false, nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(objectID)
	profile, isFrog := zonenpc.NomadSpecialFourProfile(source.Plan.NounName)
	target, isTargetFound := member.campaignNPCTarget(generation, source.TargetObjectID)
	e.registry.mutex.RUnlock()
	if !isSourceFound || !isFrog {
		return nil, false, nil
	}
	profile = zonenpc.ApplyAffixTiming(profile, source.Plan)
	step := &nomadFrogCast{runtime: e, packet: packet.Autonomous(), sessionKey: sessionKey,
		generation: generation, timestamp: timestamp, source: source,
		plan: zonenpc.AttackPlan{SourceObjectID: objectID, ActionGeneration: source.ActionGeneration, Profile: profile}}
	isDeferred, deferErr := e.pursuit.deferAction(packet, sessionKey, generation, objectID, timestamp, step.resume)
	if deferErr != nil {
		return nil, true, fmt.Errorf("frogControlDefer: %w", deferErr)
	}
	if isDeferred {
		return nil, true, nil
	}
	if e.isSilenced(sessionKey, generation, objectID) {
		return nil, true, step.schedule(time.Second, step.retry)
	}
	if !isTargetFound || !target.IsHero || !e.canApplyOperativeCage(sessionKey, generation, zonenpc.AttackPlan{SourceObjectID: objectID, TargetObjectID: target.ObjectID}) {
		err := step.schedule(time.Second, step.retry)
		return nil, true, err
	}
	plan, err := zonenpc.PlanControlWithProfile(source, target.ObjectID, target.Position, profile, target.ActorFootprintRadius)
	if err != nil {
		step.readyAt = e.now().Add(time.Second)
		packets, jumpErr := step.idle()
		if jumpErr != nil {
			return nil, true, fmt.Errorf("frogRangeJump: %w", jumpErr)
		}
		return packets, true, nil
	}
	step.plan = plan
	step.readyAt = e.now().Add(profile.HitDelay + profile.Cooldown)
	packets, err := e.startNPCAttack(sessionKey, generation, plan, timestamp)
	if err != nil {
		return nil, true, fmt.Errorf("frogChargeStart: %w", err)
	}
	err = step.schedule(profile.ReleaseDelay, step.release)
	if err != nil {
		return nil, true, fmt.Errorf("frogChargeSchedule: %w", err)
	}
	return packets, true, nil
}

func (e *nomadFrogCast) schedule(delay time.Duration, produce func() ([][]byte, error)) error {
	err := scheduleNPCProducer(e.runtime.registry, e.packet, delay, produce)
	if err != nil {
		return fmt.Errorf("frogSchedule: %w", err)
	}
	return nil
}

func (e *nomadFrogCast) currentLocked(member gameplayPeerSession, isFound bool) bool {
	return isFound && member.isCampaignNPCSourceGenerationActive(e.generation, e.plan.SourceObjectID, e.plan.ActionGeneration) && !member.isZoneTerminal()
}

func (e *nomadFrogCast) canCastLocked(member gameplayPeerSession, isFound bool) bool {
	return e.currentLocked(member, isFound) && member.isCampaignNPCActionActiveAt(e.generation, e.plan.SourceObjectID, e.runtime.now()) && member.zone.NPCs().SilenceRemaining(e.plan.SourceObjectID, e.runtime.now()) == 0
}

func (e *nomadFrogCast) resume(timestamp uint64) ([][]byte, error) {
	packets, isHandled, err := e.runtime.produceNomadFrogAction(e.packet, e.sessionKey, e.generation, e.plan.SourceObjectID, timestamp)
	if err != nil {
		return nil, fmt.Errorf("frogResume: %w", err)
	}
	if !isHandled {
		return nil, nil
	}
	return packets, nil
}

func (e *nomadFrogCast) retry() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.currentLocked(member, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent {
		return nil, nil
	}
	return e.resume(e.timestamp + 1000)
}

func (e *nomadFrogCast) release() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.canCastLocked(member, isFound) {
		isCurrent := e.currentLocked(member, isFound)
		e.runtime.registry.mutex.RUnlock()
		if isCurrent {
			return nil, e.schedule(time.Second, e.idle)
		}
		return nil, nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(e.plan.SourceObjectID)
	target, isTargetFound := member.campaignNPCTarget(e.generation, e.plan.TargetObjectID)
	e.runtime.registry.mutex.RUnlock()
	if !isSourceFound {
		return nil, nil
	}
	isToss := isTargetFound && source.Plan.Position.Sub(target.Position).Length() > 4
	e.child = zonenpc.NomadSpecialFourChildProfile(e.plan.Profile.AbilityRank, isToss)
	e.child = zonenpc.ApplyAffixTiming(e.child, source.Plan)
	e.source = source
	plan := e.plan
	plan.Profile = e.child
	plan.SourcePosition = source.Plan.Position
	if isTargetFound {
		plan.TargetPosition = target.Position
	} else {
		plan.TargetPosition = source.Plan.Position
		plan.TargetObjectID = source.Plan.ObjectID
	}
	packets, err := npcraknet.AnimationState(source.Plan.ObjectID, e.child.AnimationName, e.timestamp+1000)
	if err != nil {
		return nil, fmt.Errorf("frogChildAnimation: %w", err)
	}
	if isToss {
		err = e.prepareToss(source, plan)
		if err != nil {
			return nil, fmt.Errorf("frogTossPrepare: %w", err)
		}
		err = e.schedule(e.child.HitDelay, e.launch)
		if err != nil {
			return nil, fmt.Errorf("frogTossLaunch: %w", err)
		}
	} else {
		err = e.schedule(e.child.HitDelay, e.vomit)
		if err != nil {
			return nil, fmt.Errorf("frogVomitSchedule: %w", err)
		}
	}
	err = e.schedule(e.child.ReleaseDelay, e.idle)
	if err != nil {
		return nil, fmt.Errorf("frogIdleSchedule: %w", err)
	}
	return [][]byte{packets}, nil
}

func (e *nomadFrogCast) prepareToss(source zonenpc.Snapshot, plan zonenpc.AttackPlan) error {
	definition := campaignNPCTossDefinition(e.child)
	definition.Toss.Offset.Z = 2
	cast, err := sim.ResolveTossCast(definition, source.Plan.Position.Sub(plan.TargetPosition).Length())
	if err != nil {
		return fmt.Errorf("frogTossCast: %w", err)
	}
	launchPosition, err := zoneability.TossLaunchPosition(source.Plan.Position, plan.TargetPosition, cast.Offset)
	if err != nil {
		return fmt.Errorf("frogTossPosition: %w", err)
	}
	destination := sim.Position(plan.TargetPosition)
	lob, err := sim.BuildTossLob(e.child.HitDelay, launchPosition, destination, cast.Height, definition.Toss.FlightTime, cast.Speed, 0, 0, true, false)
	if err != nil {
		return fmt.Errorf("frogTossLob: %w", err)
	}
	e.runtime.registry.mutex.Lock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.currentLocked(member, isFound) {
		e.runtime.registry.mutex.Unlock()
		return nil
	}
	e.projectileID, err = member.reserveCampaignProjectileIDs(1, 1000)
	if err == nil {
		e.runtime.registry.sessions[e.sessionKey] = member
	}
	e.runtime.registry.mutex.Unlock()
	if err != nil {
		return fmt.Errorf("frogTossReserve: %w", err)
	}
	e.toss = zoneability.TossPlan{SourceObjectID: source.Plan.ObjectID, TargetObjectID: plan.TargetObjectID,
		AbilityID: util.HashID(e.child.AbilityName), Definition: definition, Cast: cast,
		LaunchPosition: launchPosition, Destination: destination, Lob: lob}
	return nil
}

func (e *nomadFrogCast) launch() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := e.canCastLocked(member, isFound)
	e.runtime.registry.mutex.RUnlock()
	if !isCurrent || e.projectileID == 0 {
		return nil, nil
	}
	packets, err := abilityraknet.TossLaunchForTeam(e.toss, e.projectileID, e.timestamp+1000, 0)
	if err != nil {
		return nil, fmt.Errorf("frogLaunchMarshal: %w", err)
	}
	e.isLaunched = true
	err = e.schedule(e.toss.Lob.Duration, e.land)
	if err != nil {
		return nil, fmt.Errorf("frogLandingSchedule: %w", err)
	}
	return packets, nil
}

func (e *nomadFrogCast) land() ([][]byte, error) {
	if !e.isLaunched || e.isLanded {
		return nil, nil
	}
	e.isLanded = true
	center := game.Vec3(e.toss.Destination)
	timestamp := e.timestamp + 1000 + uint64((e.child.HitDelay+e.toss.Lob.Duration)/time.Millisecond)
	packets, err := e.runtime.frogTossLanding(e, center, timestamp)
	if err != nil {
		return nil, fmt.Errorf("frogLanding: %w", err)
	}
	landingPackets, err := abilityraknet.TossDirectLanding(e.toss, e.projectileID, packets)
	if err != nil {
		return nil, fmt.Errorf("frogLandingMarshal: %w", err)
	}
	return landingPackets, nil
}

func (e *nomadFrogCast) vomit() ([][]byte, error) {
	e.runtime.registry.mutex.RLock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	if !e.canCastLocked(member, isFound) {
		e.runtime.registry.mutex.RUnlock()
		return nil, nil
	}
	source, isSourceFound := member.zone.NPCs().NPC(e.plan.SourceObjectID)
	target, isTargetFound := member.campaignNPCTarget(e.generation, e.plan.TargetObjectID)
	e.runtime.registry.mutex.RUnlock()
	if !isSourceFound {
		return nil, nil
	}
	center := source.Plan.Position
	if isTargetFound {
		center = target.Position
	}
	timestamp := e.timestamp + 1000 + uint64(e.child.HitDelay/time.Millisecond)
	var packets [][]byte
	if isTargetFound && target.IsHero {
		plan := e.plan
		plan.Profile = e.child
		plan.SourcePosition = source.Plan.Position
		plan.TargetPosition = target.Position
		cagePackets, cageErr := e.runtime.applyCampaignNPCTimedModifier(e.packet, e.sessionKey, e.generation, plan, timestamp)
		if cageErr != nil {
			return nil, fmt.Errorf("frogVomitCage: %w", cageErr)
		}
		packets = append(packets, cagePackets...)
	}
	e.source = source
	poolPackets, err := e.runtime.startNomadFrogPool(e, center, timestamp)
	if err != nil {
		return nil, fmt.Errorf("frogVomitPool: %w", err)
	}
	return append(packets, poolPackets...), nil
}
