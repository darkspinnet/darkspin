package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

const (
	campaignNashiraFirstSplitHealthFraction  = float32(0.66)
	campaignNashiraSecondSplitHealthFraction = float32(0.33)
	campaignNashiraMaximumCloneCount         = 2
	campaignNashiraCloneOffset               = float32(3)
	campaignNashiraMaximumFiendCount         = 12
	campaignNashiraFiendCooldown             = 12 * time.Second
	campaignNashiraPreSplitDuration          = 1700 * time.Millisecond
)

type campaignNashiraSplitPlan struct {
	nashira   zonenpc.Snapshot
	plans     []zonenpc.SpawnPlan
	objectIDs []uint32
}

type campaignNashiraSplitStep struct {
	runtime    campaignNPCActionRuntime
	packet     raknet.Packet
	sessionKey string
	generation uint64
	plan       campaignNashiraSplitPlan
	timestamp  uint64
}

func (e campaignNashiraSplitStep) produce() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	s, isFound := e.runtime.registry.sessions[e.sessionKey]
	isCurrent := isFound && s.isCampaignNPCSourceActive(
		e.generation, e.plan.nashira.Plan.ObjectID,
	)
	if !isCurrent {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	nashira, isNashiraFound := s.zone.NPCs().NPC(e.plan.nashira.Plan.ObjectID)
	if !isNashiraFound || nashira.IsDefeated {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	err := s.zone.NPCs().Add(e.plan.plans, nashira.TargetObjectID)
	if err != nil {
		s.rollbackCampaignNashiraSplit(e.plan)
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf("cloneAdd: %w", err)
	}
	err = s.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: e.plan.plans, TargetObjectID: nashira.TargetObjectID,
	}, s.binding.UserID, s.generation)
	if err != nil {
		rollbackErr := s.zone.NPCs().RollbackAdd(e.plan.plans)
		s.rollbackCampaignNashiraSplit(e.plan)
		e.runtime.registry.mutex.Unlock()
		return nil, fmt.Errorf(
			"clonePublish: %w", errors.Join(err, rollbackErr),
		)
	}
	delete(
		s.campaignNashiraSplitPendingObjectIDs,
		e.plan.nashira.Plan.ObjectID,
	)
	e.runtime.registry.sessions[e.sessionKey] = s
	e.runtime.registry.mutex.Unlock()

	packets, err := campaignNashiraSplitPackets(
		nashira, e.plan.objectIDs, e.plan.plans, e.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("cloneMarshal: %w", err)
	}
	actionPackets, err := e.runtime.scheduleFirstActions(
		e.packet, e.sessionKey, e.generation, e.plan.plans, e.timestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("cloneAction: %w", err)
	}
	return append(packets, actionPackets...), nil
}

type campaignNashiraPanicStep struct {
	runtime          campaignNPCActionRuntime
	packet           raknet.Packet
	sessionKey       string
	generation       uint64
	objectID         uint32
	actionGeneration uint64
	timestamp        uint64
}

func (e campaignNashiraPanicStep) produce() ([][]byte, error) {
	step := campaignNPCFirstActionStep{
		runtime: e.runtime, packet: e.packet, sessionKey: e.sessionKey,
		generation: e.generation, objectID: e.objectID,
		actionGeneration: e.actionGeneration, timestamp: e.timestamp,
	}
	return step.produce()
}

func (r campaignNPCActionRuntime) produceNashiraPanic(
	packet raknet.Packet, sessionKey string, generation uint64,
	objectID uint32, timestamp uint64,
) ([][]byte, bool, error) {
	r.registry.mutex.Lock()
	s, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && s.isCampaignNPCSourceActive(generation, objectID)
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	nashira, isNashiraFound := s.zone.NPCs().NPC(objectID)
	profile, isProfileFound := zonenpc.NashiraPanicProfile(nashira.Plan.NounName)
	if !isProfileFound {
		profile, isProfileFound = zonenpc.ActionProfileForPlan(nashira.Plan)
		isProfileFound = isProfileFound && zonenpc.IsCorruptorNoun(nashira.Plan.NounName) &&
			profile.AbilityName == "ShadowPanic"
	}
	target, isTargetFound := s.campaignNPCTarget(
		generation, nashira.TargetObjectID,
	)
	isReady := s.campaignNashiraPanicReadiness[objectID] <= timestamp
	if zonenpc.IsCorruptorNoun(nashira.Plan.NounName) {
		// Corruptor selection already owns the current phase's cooldown.
		isReady = true
	}
	sourceFootprint := float32(0)
	if zonenpc.IsCorruptorNoun(nashira.Plan.NounName) {
		sourceFootprint = nashira.Plan.NPCProfile.FootprintRadius
	}
	isInRange := isTargetFound && target.Position.Sub(
		nashira.Plan.Position,
	).Length() <= profile.Range+sourceFootprint+target.FootprintRadius
	if !isNashiraFound || nashira.Plan.OwnerObjectID != 0 || !isProfileFound ||
		!isReady || !isInRange {
		r.registry.mutex.Unlock()
		return nil, false, nil
	}
	if s.campaignNashiraPanicReadiness == nil {
		s.campaignNashiraPanicReadiness = make(map[uint32]uint64)
	}
	s.campaignNashiraPanicReadiness[objectID] = timestamp +
		uint64(profile.Cooldown/time.Millisecond)
	r.registry.sessions[sessionKey] = s
	r.registry.mutex.Unlock()

	startPackets, err := r.startNPCAttack(sessionKey, generation, zonenpc.AttackPlan{
		ActionGeneration: nashira.ActionGeneration,
		SourceObjectID:   objectID, TargetObjectID: target.ObjectID,
		SourcePosition: nashira.Plan.Position, TargetPosition: target.Position,
		Profile: profile,
	}, timestamp)
	if err != nil {
		return nil, true, fmt.Errorf("panicStart: %w", err)
	}
	next := campaignNashiraPanicStep{
		runtime: r, packet: packet, sessionKey: sessionKey,
		generation: generation, objectID: objectID,
		actionGeneration: nashira.ActionGeneration,
		timestamp:        timestamp + uint64(profile.ReleaseDelay/time.Millisecond),
	}
	hit := campaignNashiraFearStep{step: next, profile: profile}
	hit.step.timestamp = timestamp + uint64(profile.HitDelay/time.Millisecond)
	cancel, err := packet.ScheduleProducers([]raknet.ScheduledPacketProducer{
		{Delay: profile.HitDelay, Produce: hit.produce},
		{Delay: profile.ReleaseDelay, Produce: next.produce},
	})
	if err == nil && cancel == nil {
		err = errors.New("panic cancellation unavailable")
	}
	if err != nil {
		r.releaseActionGeneration(
			sessionKey, generation, objectID, nashira.ActionGeneration,
		)
		return nil, true, fmt.Errorf("panicSchedule: %w", err)
	}
	return startPackets, true, nil
}

func (r campaignNPCActionRuntime) planCampaignNashiraFiend(
	s *gameplayPeerSession, ownerObjectID uint32, position game.Vec3,
	timestamp uint64,
) ([]zonenpc.SpawnPlan, [][]byte, error) {
	if s == nil || s.zone == nil || s.zone.NPCs() == nil || ownerObjectID == 0 {
		return nil, nil, errors.New("fiend spawn unavailable")
	}
	owner, isFound := s.zone.NPCs().NPC(ownerObjectID)
	if !isFound || owner.IsDefeated || owner.Plan.OwnerObjectID != 0 ||
		s.zone.NPCs().OwnedActiveCount(ownerObjectID) >= campaignNashiraMaximumFiendCount {
		return nil, nil, nil
	}
	if s.campaignNashiraFiendReadiness[ownerObjectID] > timestamp {
		return nil, nil, nil
	}
	nounName, isNounFound := zonenpc.NashiraFiendNoun(owner.Plan.NounName)
	profile, isProfileFound := zonenpc.NashiraFiendProfile(nounName)
	if !isNounFound || !isProfileFound {
		return nil, nil, nil
	}
	objectID, err := s.reserveCampaignObjectID()
	if err != nil {
		return nil, nil, fmt.Errorf("fiendReserve: %w", err)
	}
	hitPoint := r.program.NonPlayerHitPoint[util.HashID(
		nounName[:len(nounName)-len(".Noun")],
	)]
	if hitPoint <= 0 {
		hitPoint = 16
	}
	footprintRadius, footprintErr := r.program.FootprintRadius(nounName)
	if footprintErr != nil || footprintRadius <= 0 {
		footprintRadius = 0.5
	}
	npcProfile := owner.Plan.NPCProfile
	npcProfile.HitPoint = hitPoint
	npcProfile.FootprintRadius = footprintRadius
	npcProfile.GraphicsScale = 1
	npcProfile.IsTargetable = true
	position.X += 0.1
	plan := zonenpc.SpawnPlan{
		ObjectID: objectID, OwnerObjectID: ownerObjectID,
		NounName: nounName, Position: position,
		IsRewardSuppressed: true, NPCProfile: npcProfile,
		ActionProfile: profile, IsActionKnown: true,
	}
	targetObjectID := owner.TargetObjectID
	if targetObjectID == 0 {
		targetObjectID = s.deployedObjectID
	}
	if targetObjectID == 0 {
		return nil, nil, errors.New("fiend target unavailable")
	}
	packets, err := npcraknet.TargetedSpawn(plan, targetObjectID)
	if err != nil {
		return nil, nil, fmt.Errorf("fiendMarshal: %w", err)
	}
	err = s.zone.NPCs().Add([]zonenpc.SpawnPlan{plan}, targetObjectID)
	if err != nil {
		return nil, nil, fmt.Errorf("fiendAdd: %w", err)
	}
	err = s.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: []zonenpc.SpawnPlan{plan}, TargetObjectID: targetObjectID,
	}, s.binding.UserID, s.generation)
	if err != nil {
		rollbackErr := s.zone.NPCs().RollbackAdd([]zonenpc.SpawnPlan{plan})
		return nil, nil, fmt.Errorf(
			"fiendPublish: %w", errors.Join(err, rollbackErr),
		)
	}
	if s.campaignNashiraFiendReadiness == nil {
		s.campaignNashiraFiendReadiness = make(map[uint32]uint64)
	}
	s.campaignNashiraFiendReadiness[ownerObjectID] = timestamp +
		uint64(campaignNashiraFiendCooldown/time.Millisecond)
	spawnEffect, err := npcraknet.PositionedEffect(
		"shadow_lob_impact_spawn_creature", position,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("fiendEffect: %w", err)
	}
	return []zonenpc.SpawnPlan{plan}, append([][]byte{spawnEffect}, packets...), nil
}

func (s *gameplayPeerSession) planCampaignNashiraSplit(
	result zonenpc.DamageResult,
) (*campaignNashiraSplitPlan, [][]byte, error) {
	if s == nil || s.zone == nil || s.zone.NPCs() == nil ||
		result.ObjectID == 0 || result.IsDefeated || result.IsDamageImmune {
		return nil, nil, nil
	}
	nashira, isFound := s.zone.NPCs().NPC(result.ObjectID)
	if !isFound || nashira.Plan.OwnerObjectID != 0 ||
		!zonenpc.IsNashiraNoun(nashira.Plan.NounName) {
		return nil, nil, nil
	}
	maximumHitPoint := nashira.Plan.NPCProfile.HitPoint
	objectIDs := s.campaignNashiraSplitObjectIDs[result.ObjectID]
	cloneCount := len(objectIDs)
	if cloneCount >= campaignNashiraMaximumCloneCount ||
		s.campaignNashiraSplitPendingObjectIDs[result.ObjectID] != 0 {
		return nil, nil, nil
	}
	thresholdFraction := campaignNashiraFirstSplitHealthFraction
	if cloneCount == 1 {
		thresholdFraction = campaignNashiraSecondSplitHealthFraction
	}
	threshold := maximumHitPoint * thresholdFraction
	if maximumHitPoint <= 0 || result.HitPoint > threshold {
		return nil, nil, nil
	}

	targetPosition := nashira.Plan.Position
	targetPosition.X += nashira.Facing.X
	targetPosition.Y += nashira.Facing.Y
	target, isTargetFound := s.campaignNPCTarget(
		s.generation, nashira.TargetObjectID,
	)
	if isTargetFound {
		targetPosition = target.Position
	}
	objectID, err := s.reserveCampaignObjectID()
	if err != nil {
		return nil, nil, fmt.Errorf("cloneReserve: %w", err)
	}
	plan := campaignNashiraClonePlan(
		nashira, targetPosition, objectID, cloneCount,
	)
	plans := []zonenpc.SpawnPlan{plan}
	if s.campaignNashiraSplitObjectIDs == nil {
		s.campaignNashiraSplitObjectIDs = make(map[uint32][]uint32)
	}
	if s.campaignNashiraSplitPendingObjectIDs == nil {
		s.campaignNashiraSplitPendingObjectIDs = make(map[uint32]uint32)
	}
	s.campaignNashiraSplitObjectIDs[result.ObjectID] = append(objectIDs, objectID)
	s.campaignNashiraSplitPendingObjectIDs[result.ObjectID] = objectID
	splitObjectIDs := make([]uint32, 1, len(objectIDs)+1)
	splitObjectIDs[0] = result.ObjectID
	splitObjectIDs = append(splitObjectIDs, objectIDs...)
	return &campaignNashiraSplitPlan{
		nashira: nashira, plans: plans, objectIDs: splitObjectIDs,
	}, nil, nil
}

func (s *gameplayPeerSession) rollbackCampaignNashiraSplit(
	plan campaignNashiraSplitPlan,
) {
	if s == nil || len(plan.plans) != 1 {
		return
	}
	objectID := plan.plans[0].ObjectID
	originalObjectID := plan.nashira.Plan.ObjectID
	if s.campaignNashiraSplitPendingObjectIDs[originalObjectID] != objectID {
		return
	}
	delete(s.campaignNashiraSplitPendingObjectIDs, originalObjectID)
	objectIDs := s.campaignNashiraSplitObjectIDs[originalObjectID]
	if len(objectIDs) == 0 || objectIDs[len(objectIDs)-1] != objectID {
		return
	}
	objectIDs = objectIDs[:len(objectIDs)-1]
	if len(objectIDs) == 0 {
		delete(s.campaignNashiraSplitObjectIDs, originalObjectID)
		return
	}
	s.campaignNashiraSplitObjectIDs[originalObjectID] = objectIDs
}

func campaignNashiraClonePlan(
	nashira zonenpc.Snapshot, targetPosition game.Vec3,
	objectID uint32, cloneIndex int,
) zonenpc.SpawnPlan {
	plan := nashira.Plan.Clone()
	plan.ObjectID = objectID
	plan.OwnerObjectID = nashira.Plan.ObjectID
	plan.Position = campaignNashiraClonePosition(
		nashira.Plan.Position, targetPosition, cloneIndex,
	)
	plan.LocusID = 0
	plan.MarkerSetName = ""
	plan.Experience = 0
	plan.Kind = 0
	plan.IsCaptain = false
	plan.IsElite = false
	plan.IsBoss = false
	plan.IsRewardSuppressed = true
	plan.BossIdentity = zonenpc.BossIdentity{}
	// ShadowBossDuplicate copies remaining health; SetIsDuplicate supplies
	// the physical/energy vulnerability in the NPC damage path instead.
	plan.NPCProfile.HitPoint = nashira.HitPoint
	// Resolve noun-backed profiles before overriding them; otherwise lookup
	// discards the cleared introduction and reloads the original boss profile.
	profile, isProfileFound := zonenpc.ActionProfileForPlan(nashira.Plan)
	plan.ActionProfile = zonenpc.NashiraCloneProfile(profile)
	plan.IsActionKnown = isProfileFound
	return plan
}

func campaignNashiraClonePosition(
	source game.Vec3, target game.Vec3, cloneIndex int,
) game.Vec3 {
	deltaX := target.X - source.X
	deltaY := target.Y - source.Y
	length := float32(math.Hypot(float64(deltaX), float64(deltaY)))
	if length <= 0 {
		deltaX, deltaY, length = 1, 0, 1
	}
	offset := campaignNashiraCloneOffset
	if cloneIndex%2 != 0 {
		offset = -offset
	}
	source.X += (-deltaY / length) * offset
	source.Y += (deltaX / length) * offset
	return source
}

func campaignNashiraSplitPackets(
	nashira zonenpc.Snapshot, objectIDs []uint32, plans []zonenpc.SpawnPlan,
	timestamp uint64,
) ([][]byte, error) {
	packets := make([][]byte, 0, len(objectIDs)*2+len(plans)*4+1)
	for index, objectID := range objectIDs {
		animationPacket, err := npcraknet.AnimationState(
			objectID, "shadowboss_split_b", timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("splitAnimation[%d]: %w", index, err)
		}
		packets = append(packets, animationPacket)
	}
	splitPacket, err := npcraknet.PositionedEffect(
		"shadow_boss_duplicate_effect.ServerEventDef", nashira.Plan.Position,
	)
	if err != nil {
		return nil, fmt.Errorf("splitEffect: %w", err)
	}
	packets = append(packets, splitPacket)
	for index, plan := range plans {
		spawnPackets, marshalErr := npcraknet.TargetedSpawn(
			plan, nashira.TargetObjectID,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("spawn[%d]: %w", index, marshalErr)
		}
		animationPacket, marshalErr := npcraknet.AnimationState(
			plan.ObjectID, "shadowboss_split_b", timestamp,
		)
		if marshalErr != nil {
			return nil, fmt.Errorf("animation[%d]: %w", index, marshalErr)
		}
		packets = append(packets, spawnPackets...)
		// Spawn attaches the illusion shader through the passive-effect slot.
		packets = append(packets, animationPacket)
	}
	return packets, nil
}
