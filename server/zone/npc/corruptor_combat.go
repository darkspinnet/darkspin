package npc

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// A phase owns its special's cooldown independently of melee and cosmetic
// actions. Keep the selected profile stable through pursuit and cast recovery.
type corruptorCombat struct {
	specialProfile        ActionProfile
	specialReadyTimestamp uint64
	poseReadyTimestamp    uint64
}

type CorruptorActionRequest struct {
	ObjectID         uint32
	ActionGeneration uint64
	Timestamp        uint64
	TargetPosition   game.Vec3
	RandomRoll       float64
}

func IsCorruptorNoun(nounName string) bool {
	return scaldronBossRank(nounName) != 0
}

func (e *Session) SelectCorruptorAction(req CorruptorActionRequest) (Snapshot, bool) {
	if e == nil {
		return Snapshot{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[req.ObjectID]
	if !isFound || npc.IsDefeated || !npc.IsActionStarted ||
		npc.ActionGeneration != req.ActionGeneration || !IsCorruptorNoun(npc.Plan.NounName) {
		return Snapshot{}, false
	}
	combat := npc.corruptorCombat
	if combat.specialProfile.AbilityName == "" {
		profile, isProfileFound := ActionProfileForPlan(npc.Plan)
		if !isProfileFound {
			return Snapshot{}, false
		}
		combat.specialProfile = profile.Clone()
	}
	if combat.poseReadyTimestamp == 0 {
		combat.poseReadyTimestamp = req.Timestamp + 12000
	}
	profile := combat.specialProfile.Clone()
	if req.Timestamp < combat.specialReadyTimestamp {
		var isMeleeFound bool
		profile, isMeleeFound = ScaldronBossMeleeProfile(npc.Plan.NounName, combat.specialProfile)
		if !isMeleeFound {
			return Snapshot{}, false
		}
		if req.Timestamp >= combat.poseReadyTimestamp &&
			req.TargetPosition.Sub(npc.Plan.Position).Length() <= 32 {
			profile = corruptorPoseProfile(req.RandomRoll)
		}
	}
	// The introduction belongs to the initial spawn, never to an action cycle.
	profile.FirstAggroDelay = 0
	npc.Plan.ActionProfile = profile
	npc.Plan.IsActionKnown = true
	npc.corruptorCombat = combat
	e.npcs[req.ObjectID] = npc
	return npc, true
}

func corruptorPoseProfile(randomRoll float64) ActionProfile {
	index := min(max(int(randomRoll*3), 0), 2)
	animations := [...]string{"sca_boss_pose_1", "sca_boss_pose_2", "sca_boss_pose_3"}
	durations := [...]time.Duration{3200 * time.Millisecond, 3200 * time.Millisecond, 4200 * time.Millisecond}
	return ActionProfile{
		Family: ActionRetainedArea, AbilityName: "ScaldronBoss_Pose",
		AnimationName: animations[index], ReleaseDelay: durations[index],
		Cooldown: 12 * time.Second,
		Range:    32, MovementSpeed: 5, NonCombatMovementSpeed: 3,
		IsFirstAggroDurationKnown: true,
	}
}

// CommitCorruptorAction starts cooldown only once the cast is accepted, not
// when it is selected for pursuit. Retargeting preserves the phase's cooldown.
func (e *Session) CommitCorruptorAction(plan AttackPlan, timestamp uint64) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[plan.SourceObjectID]
	if !isFound || npc.IsDefeated || !npc.IsActionStarted ||
		npc.ActionGeneration != plan.ActionGeneration {
		return false
	}
	if !IsCorruptorNoun(npc.Plan.NounName) {
		return true
	}
	if plan.Profile.AbilityName != npc.Plan.ActionProfile.AbilityName {
		return false
	}
	if plan.Profile.AbilityName == npc.corruptorCombat.specialProfile.AbilityName {
		npc.corruptorCombat.specialReadyTimestamp = timestamp + uint64(plan.Profile.Cooldown/time.Millisecond)
	}
	if plan.Profile.AbilityName == "ScaldronBoss_Pose" {
		npc.corruptorCombat.poseReadyTimestamp = timestamp + uint64(plan.Profile.Cooldown/time.Millisecond)
	}
	e.npcs[plan.SourceObjectID] = npc
	return true
}
