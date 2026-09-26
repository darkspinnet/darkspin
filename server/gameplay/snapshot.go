package gameplay

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/snapshot"
	"github.com/darkspinnet/darkspin/server/zone"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	deathraknet "github.com/darkspinnet/darkspin/server/zone/death/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e *gameplaySessionRegistry) SyncSnapshot(
	ctx context.Context, req snapshot.StateRequest,
) (snapshot.StateFrame, error) {
	err := ctx.Err()
	if err != nil {
		return snapshot.StateFrame{}, fmt.Errorf("snapshotContext: %w", err)
	}
	frame := snapshot.StateFrame{
		CapturedAt: time.Now().UTC(), Actor: req.Actor,
	}
	if e == nil {
		return frame, nil
	}
	for !e.mutex.TryRLock() {
		select {
		case <-ctx.Done():
			return frame, fmt.Errorf("snapshotLock: %w", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	defer e.mutex.RUnlock()
	selectedZone := selectSnapshotZone(e.sessions, req.Actor)
	for remote, peerSession := range e.sessions {
		err = ctx.Err()
		if err != nil {
			return frame, fmt.Errorf("snapshotSession: %w", err)
		}
		if !isSnapshotSessionSelected(peerSession, remote, req.Actor, selectedZone) {
			continue
		}
		if frame.Actor.UserID == 0 && (req.Actor.Remote == "" || req.Actor.Remote == remote) {
			frame.Actor.UserID = int64(peerSession.binding.UserID)
			frame.Actor.GameID = peerSession.binding.GameID
			frame.Actor.Remote = remote
		}
		frame.Sessions = append(
			frame.Sessions,
			snapshotSession(remote, peerSession, frame.CapturedAt),
		)
	}
	for _, decision := range e.diagnosticDecisions {
		if req.Actor.Remote != "" && decision.remote != req.Actor.Remote {
			continue
		}
		frame.GameplayDecisions = append(
			frame.GameplayDecisions,
			snapshot.GameplayDecisionState{
				OccurredAt: decision.occurredAt, Kind: decision.kind,
				TraceID: decision.traceID, Remote: decision.remote,
				TransportGeneration: decision.transportGeneration,
				ZoneGeneration:      decision.zoneGeneration, ObjectID: decision.objectID,
				ActionType: decision.actionType, SyncStamp: decision.syncStamp,
				Outcome: decision.outcome, Reason: decision.reason,
				PacketCount: decision.packetCount,
			},
		)
	}
	sort.Slice(frame.Sessions, func(left int, right int) bool {
		if frame.Sessions[left].GameID != frame.Sessions[right].GameID {
			return frame.Sessions[left].GameID < frame.Sessions[right].GameID
		}
		return frame.Sessions[left].UserID < frame.Sessions[right].UserID
	})
	return frame, nil
}

const maximumGameplayDiagnosticDecisionCount = 256

func (e *gameplaySessionRegistry) recordDiagnosticDecision(
	decision gameplayDiagnosticDecision,
) {
	if e == nil {
		return
	}
	e.mutex.Lock()
	e.diagnosticDecisions = append(e.diagnosticDecisions, decision)
	if len(e.diagnosticDecisions) > maximumGameplayDiagnosticDecisionCount {
		copy(e.diagnosticDecisions, e.diagnosticDecisions[1:])
		e.diagnosticDecisions = e.diagnosticDecisions[:maximumGameplayDiagnosticDecisionCount]
	}
	e.mutex.Unlock()
}

func selectSnapshotZone(
	sessions map[string]gameplayPeerSession, actor snapshot.Actor,
) *gameplayPeerSession {
	for remote, peerSession := range sessions {
		if actor.Remote != "" && actor.Remote == remote {
			selected := peerSession
			return &selected
		}
		if actor.UserID != 0 && peerSession.binding.UserID == uint64(actor.UserID) &&
			(actor.GameID == 0 || peerSession.binding.GameID == actor.GameID) {
			selected := peerSession
			return &selected
		}
	}
	return nil
}

func isSnapshotSessionSelected(
	peerSession gameplayPeerSession, remote string, actor snapshot.Actor,
	selected *gameplayPeerSession,
) bool {
	if selected != nil {
		return peerSession.zone == selected.zone &&
			peerSession.binding.GameID == selected.binding.GameID
	}
	if actor.Remote != "" {
		return actor.Remote == remote
	}
	if actor.GameID != 0 {
		return peerSession.binding.GameID == actor.GameID
	}
	if actor.UserID != 0 {
		return peerSession.binding.UserID == uint64(actor.UserID)
	}
	return true
}

func snapshotSession(
	remote string, peerSession gameplayPeerSession, capturedAt time.Time,
) snapshot.SessionState {
	state := snapshot.SessionState{
		Remote: remote, UserID: peerSession.binding.UserID,
		GameID:              peerSession.binding.GameID,
		SessionGeneration:   peerSession.generation,
		TransportGeneration: peerSession.transportGeneration,
		Stage:               bugSessionStage(peerSession.stage), PlayerSlot: peerSession.binding.Slot,
		DeployedObjectID:  peerSession.deployedObjectID,
		IsPendingOverflow: peerSession.isPendingPacketOverflow,
	}
	for _, batch := range peerSession.pendingPacketBatches {
		for packetIndex, packet := range batch.packets {
			state.PendingPackets = append(
				state.PendingPackets,
				snapshotPendingPacket(batch.id, packetIndex, packet),
			)
		}
	}
	state.PendingPacketCount = len(state.PendingPackets)
	if peerSession.zone == nil {
		return state
	}
	zoneSnapshot := peerSession.zone.Snapshot()
	state.ZoneID = zoneSnapshot.ID
	state.ZoneGeneration = zoneSnapshot.Generation
	state.ZoneState = uint8(zoneSnapshot.State)
	state.ProjectionRevision = peerSession.zone.ProjectionRevision()
	state.ZoneElapsedMS = peerSession.zone.Elapsed(capturedAt).Milliseconds()
	state.ZoneCompletionID = zoneSnapshot.CompletionID
	state.ZoneLevel = zoneSnapshot.Level
	state.ZoneDifficulty = zoneSnapshot.Difficulty
	state.ZoneChainLevelIndex = zoneSnapshot.ChainLevelIndex
	state.ZoneMemberLimit = zoneSnapshot.MemberLimit
	state.ClearedSpawnGroupIDs = zoneSnapshot.ClearedSpawnGroupIDs
	state.IsPopulationPrimed = zoneSnapshot.IsPopulationPrimed
	state.IsZoneRestored = zoneSnapshot.IsRestored
	state.IsObjectiveComplete = zoneSnapshot.IsObjectiveComplete
	state.ZoneMembers = snapshotZoneMembers(zoneSnapshot.Members)
	state.DirectorPublications = snapshotDirectorPublications(peerSession)
	state.PlayerControl = snapshotPlayerControl(peerSession, capturedAt)
	state.Cooldowns = snapshotCooldowns(peerSession, capturedAt)
	state.ActionSchedules = snapshotActionSchedules(peerSession)
	state.Deaths = snapshotDeaths(peerSession)
	state.Objectives = snapshotObjectives(peerSession)
	state.Squad = snapshotSquad(peerSession, capturedAt)
	state.CrystalInventory = snapshotCrystalInventory(peerSession.crystalInventory)
	state.AbilityRuntimes = snapshotAbilityRuntimes(peerSession, capturedAt)
	state.EncounterStages = snapshotEncounterStages(peerSession)
	state.Hordes = snapshotHordes(peerSession)
	state.Boss = snapshotBoss(peerSession)
	state.Security = snapshotSecurity(peerSession)
	state.Interactables = snapshotInteractables(peerSession)
	state.TimelineTasks = snapshotTimelineTasks(peerSession, capturedAt)
	for _, task := range state.TimelineTasks {
		state.TimelineKeys = append(state.TimelineKeys, task.Key)
	}
	state.Randoms = appendSnapshotRandom(
		state.Randoms, "npc", peerSession.zone.NPCRandom(),
	)
	state.Randoms = appendSnapshotRandom(
		state.Randoms, "drop", peerSession.zone.DropRandom(),
	)
	heroes := peerSession.zone.Hero().Snapshots()
	pursuit := peerSession.campaignPlayerPursuit.Snapshot()
	for _, hero := range heroes {
		object := snapshot.ObjectState{
			Kind: "hero", ObjectID: hero.ObjectID, UserID: hero.UserID,
			PeerGeneration: hero.PeerGeneration,
			Position:       vec3(hero.Position), LinearVelocity: vec3(hero.LinearVelocity),
			HitPoint: hero.HitPoint, ManaPoint: hero.ManaPoint,
			IsDefeated: hero.HitPoint <= 0, IsPublished: true,
			IsTargetable: hero.HitPoint > 0,
		}
		if hero.ObjectID == peerSession.deployedObjectID {
			object.GoalPosition = vector3(peerSession.playerMovementGoal)
			object.Facing = vector3(peerSession.attackPose.facing)
			object.TargetPosition = vector3(peerSession.attackPose.targetPosition)
			object.TargetObjectID = peerSession.attackPose.targetObjectID
			if peerSession.playerMotion != nil {
				object.ActionRevision = peerSession.playerMotion.Revision()
			}
			if pursuit.IsActive && pursuit.SourceObjectID == hero.ObjectID {
				object.TargetObjectID = pursuit.TargetObjectID
				object.AbilityIndex = pursuit.AbilityIndex
				object.SyncStamp = pursuit.SyncStamp
				object.IsPursuing = true
			}
		}
		if run := peerSession.campaignNPCFears[hero.ObjectID]; run != nil {
			object.FearSourceObjectID = run.sourceObjectID
			object.FearRemainingMS = snapshotDurationMS(
				run.expiresAt.Sub(capturedAt),
			)
		}
		state.Objects = append(state.Objects, object)
	}
	companions := peerSession.zone.Companion().Snapshots()
	for _, companion := range companions {
		state.Objects = append(state.Objects, snapshot.ObjectState{
			Kind: "companion", ObjectID: companion.ObjectID,
			OwnerObjectID: companion.OwnerObjectID, UserID: companion.UserID,
			PeerGeneration: companion.PeerGeneration,
			Position:       vec3(companion.Position), HitPoint: companion.HitPoint,
			TargetObjectID: companion.TargetObjectID,
			IsDefeated:     companion.HitPoint <= 0, IsPublished: true,
			IsTargetable: companion.IsTargetable,
		})
	}
	npcs := peerSession.zone.NPCs().Snapshots()
	for _, npc := range npcs {
		object := snapshot.ObjectState{
			Kind: "npc", ObjectID: npc.Plan.ObjectID,
			OwnerObjectID: npc.Plan.OwnerObjectID, NounName: npc.Plan.NounName,
			Position: vec3(npc.Plan.Position), Rotation: vec3(npc.Plan.Rotation),
			Facing: vec3(npc.Facing), HitPoint: npc.HitPoint, ManaPoint: npc.ManaPoint,
			TargetObjectID: npc.TargetObjectID, ActionRevision: npc.ActionGeneration,
			IsDefeated: npc.IsDefeated, IsPublished: npc.IsPublished,
			IsTargetable:                 npc.Plan.NPCProfile.IsTargetable && !npc.IsDefeated,
			IsActionStarted:              npc.IsActionStarted,
			IsFirstActionStarted:         npc.IsFirstActionStarted,
			IsSelfResurrectionTriggered:  npc.IsSelfResurrectionTriggered,
			IsNavigationCollisionEnabled: npc.IsNavigationCollisionEnabled,
			ActionOwnerUserID:            npc.ActionOwner.UserID,
			ActionOwnerGeneration:        npc.ActionOwner.PeerGeneration,
		}
		object.SlowMovementScale = peerSession.zone.NPCs().SlowMovementScale(
			npc.Plan.ObjectID, capturedAt,
		)
		object.StunRemainingMS = snapshotDurationMS(
			peerSession.zone.NPCs().StunRemaining(npc.Plan.ObjectID, capturedAt),
		)
		object.SleepRemainingMS = snapshotDurationMS(
			peerSession.zone.NPCs().SleepRemaining(npc.Plan.ObjectID, capturedAt),
		)
		object.RootRemainingMS = snapshotDurationMS(
			peerSession.zone.NPCs().RootRemaining(npc.Plan.ObjectID, capturedAt),
		)
		object.SilenceRemainingMS = snapshotDurationMS(
			peerSession.zone.NPCs().SilenceRemaining(npc.Plan.ObjectID, capturedAt),
		)
		object.FearRemainingMS = snapshotDurationMS(
			peerSession.zone.NPCs().FearRemaining(npc.Plan.ObjectID, capturedAt),
		)
		profile, isProfileFound := zonenpc.ActionProfileForPlan(npc.Plan)
		if isProfileFound {
			object.AbilityName = profile.AbilityName
			object.StopDistance = profile.Range
			object.Speed = graspingDeadMovementSpeed(
				peerSession, npc.Plan.Position, profile.MovementSpeed,
			) * object.SlowMovementScale
		}
		target, isTargetFound := peerSession.campaignNPCTarget(
			peerSession.generation, npc.TargetObjectID,
		)
		if isTargetFound {
			object.GoalPosition = vec3(target.Position)
			object.TargetPosition = vec3(target.Position)
			object.IsPursuing = npc.IsActionStarted && isProfileFound
		}
		state.Objects = append(state.Objects, object)
	}
	pickups := peerSession.zone.Pickups().Snapshots()
	for _, pickup := range pickups {
		state.Objects = append(state.Objects, snapshot.ObjectState{
			Kind: "pickup", SubType: uint32(pickup.Kind),
			ObjectID: pickup.ObjectID, Position: vec3(pickup.Position),
			TargetPosition: vec3(pickup.SourcePosition), IsPublished: true,
		})
	}
	dnaPickups := peerSession.zone.DNA().Snapshots()
	for _, pickup := range dnaPickups {
		state.Objects = append(state.Objects, snapshot.ObjectState{
			Kind: "dna", ObjectID: pickup.ObjectID, Amount: pickup.Amount,
			Position: vec3(pickup.Position), IsPublished: true,
		})
	}
	orbs := peerSession.zone.Orbs().Orbs()
	for _, orb := range orbs {
		state.Objects = append(state.Objects, snapshot.ObjectState{
			Kind: "orb", SubType: uint32(orb.Request.Kind),
			ObjectID: orb.ObjectID, NounName: orb.Request.NounName,
			Position: [3]float32{
				orb.Request.Destination.X,
				orb.Request.Destination.Y,
				orb.Request.Destination.Z,
			},
			IsPublished: true,
		})
	}
	state.Objects = append(
		state.Objects,
		snapshotProjectileObjects(peerSession, capturedAt)...,
	)
	modifiers := peerSession.zone.Effect().Snapshot()
	for _, modifier := range modifiers {
		state.Modifiers = append(state.Modifiers, snapshot.ModifierState{
			InstanceID: modifier.InstanceID, GUID: modifier.GUID,
			SourceObjectID: modifier.SourceObjectID,
			TargetObjectID: modifier.TargetObjectID, Rank: modifier.Rank,
			DurationMS: int64(modifier.Duration / time.Millisecond),
			Kind:       uint8(modifier.Kind), StackCount: modifier.StackCount,
			DamageBuff: modifier.DamageBuff, EnergyDamageBuff: modifier.EnergyDamageBuff,
			EnergyDamageTakenIncrease: modifier.EnergyDamageTakenIncrease,
			HealingReduction:          modifier.HealingReduction, AttackSpeed: modifier.AttackSpeed,
			CooldownReduction: modifier.CooldownReduction,
			MovementSpeedBuff: modifier.MovementSpeedBuff,
			IsChannel:         modifier.IsChannel, IsHaste: modifier.IsHaste,
		})
	}
	sort.Slice(state.Objects, func(left int, right int) bool {
		if state.Objects[left].ObjectID != state.Objects[right].ObjectID {
			return state.Objects[left].ObjectID < state.Objects[right].ObjectID
		}
		return state.Objects[left].Kind < state.Objects[right].Kind
	})
	return state
}

func snapshotPlayerControl(
	peerSession gameplayPeerSession, capturedAt time.Time,
) snapshot.PlayerControlState {
	control := snapshot.PlayerControlState{
		ReportedPosition:              vector3(peerSession.playerPosition),
		MovementGoal:                  vector3(peerSession.playerMovementGoal),
		AttackFacing:                  vector3(peerSession.attackPose.facing),
		AttackTargetPosition:          vector3(peerSession.attackPose.targetPosition),
		AttackTargetObjectID:          peerSession.attackPose.targetObjectID,
		AttackPoseRemainingMS:         snapshotDurationMS(peerSession.attackPose.expiresAt.Sub(capturedAt)),
		HeroSelectionRemainingMS:      snapshotDurationMS(peerSession.heroSelectionReadyAt.Sub(capturedAt)),
		EnemySilenceRemainingMS:       snapshotDurationMS(peerSession.enemySilenceExpiresAt.Sub(capturedAt)),
		EnemySleepRemainingMS:         snapshotDurationMS(peerSession.enemySleepExpiresAt.Sub(capturedAt)),
		EnemyStunRemainingMS:          snapshotDurationMS(peerSession.enemyStunExpiresAt.Sub(capturedAt)),
		EnemyStunTargetObjectID:       peerSession.enemyStunTargetObjectID,
		EnemyRootRemainingMS:          snapshotDurationMS(peerSession.enemyRootExpiresAt.Sub(capturedAt)),
		EnemyRootTargetObjectID:       peerSession.enemyRootTargetObjectID,
		EnemyFearRemainingMS:          snapshotDurationMS(peerSession.enemyFearExpiresAt.Sub(capturedAt)),
		EnemyFearTargetObjectID:       peerSession.enemyFearTargetObjectID,
		OverdriveRemainingMS:          snapshotDurationMS(peerSession.overdriveExpiresAt.Sub(capturedAt)),
		FollowTargetUserID:            peerSession.followTargetUserID,
		NextProjectileObjectID:        peerSession.nextProjectileObjectID,
		IsAttackPoseActive:            peerSession.attackPose.isActiveAt(capturedAt),
		IsHeroSelectionPending:        peerSession.isHeroSelectionPending,
		IsHeroSelectionScheduled:      peerSession.isHeroSelectionScheduled,
		IsOverdrivePersistencePending: peerSession.isOverdrivePersistencePending,
		IsOverdriveSpent:              peerSession.isOverdriveSpent,
		IsTrapperStealthed:            peerSession.isTrapperStealthed,
	}
	if peerSession.playerMotion != nil {
		motion := peerSession.playerMotion.Snapshot()
		movement := motion.Movement()
		control.MotionPosition = simPosition(motion.Position())
		control.MotionGoal = simPosition(movement.Goal)
		control.MotionVelocity = simPosition(movement.Velocity)
		control.MotionSpeed = movement.Speed
		control.MotionRevision = motion.Revision
		control.MotionStartedAt = motion.StartedAt()
		control.MotionSegmentAtMS = movement.At.Milliseconds()
		control.IsMotionMoving = movement.IsMoving
	}
	if peerSession.abilityRelease != nil {
		release := peerSession.abilityRelease.Snapshot()
		control.AbilityReleaseRemainingMS = snapshotDurationMS(
			release.End.Sub(capturedAt),
		)
		control.AbilityReleaseRevision = release.Revision
	}
	if peerSession.basicSequence != nil {
		sequence := peerSession.basicSequence.Snapshot()
		control.BasicSequence = snapshot.BasicSequenceState{
			Revision:            sequence.Revision,
			CooldownRemainingMS: snapshotDurationMS(sequence.CooldownEnd.Sub(capturedAt)),
			ContinueRemainingMS: snapshotDurationMS(sequence.ContinueEnd.Sub(capturedAt)),
			HeldGeneration:      sequence.HeldGeneration, Index: sequence.Index,
			StarterIndex: sequence.StarterIndex, IsHeld: sequence.IsHeld,
			IsStarted:              sequence.IsStarted,
			IsFullSequenceComplete: sequence.IsFullSequenceComplete,
		}
	}
	return control
}

func snapshotCooldowns(
	peerSession gameplayPeerSession, capturedAt time.Time,
) []snapshot.CooldownState {
	if peerSession.abilityCooldown == nil {
		return nil
	}
	cooldowns := peerSession.abilityCooldown.Snapshots()
	states := make([]snapshot.CooldownState, 0, len(cooldowns))
	for _, cooldown := range cooldowns {
		states = append(states, snapshot.CooldownState{
			Key: uint64(cooldown.Key), AbilityID: cooldown.AbilityID,
			RemainingMS: snapshotDurationMS(cooldown.End.Sub(capturedAt)),
			Revision:    cooldown.Revision, IsHeroAbility: cooldown.IsHeroAbility,
		})
	}
	return states
}

func snapshotActionSchedules(
	peerSession gameplayPeerSession,
) []snapshot.ActionScheduleState {
	if peerSession.campaignSchedule == nil {
		return nil
	}
	schedules := peerSession.campaignSchedule.Snapshots()
	states := make([]snapshot.ActionScheduleState, 0, len(schedules))
	for _, schedule := range schedules {
		states = append(states, snapshot.ActionScheduleState{
			Kind: snapshotActionScheduleKind(schedule.Kind), ObjectID: schedule.ObjectID,
			IsRunAttached:     schedule.IsRunAttached,
			IsCleanerAttached: schedule.IsCleanerAttached,
		})
	}
	return states
}

func snapshotActionScheduleKind(kind zoneaction.ScheduleKind) string {
	switch kind {
	case zoneaction.ScheduleInteractable:
		return "interactable"
	case zoneaction.ScheduleCrystal:
		return "crystal"
	case zoneaction.ScheduleEquipment:
		return "equipment"
	default:
		return fmt.Sprintf("unknown-%d", kind)
	}
}

func snapshotDeaths(peerSession gameplayPeerSession) []snapshot.DeathState {
	objectIDs := make([]uint32, 0, len(peerSession.enemyDeaths))
	for objectID := range peerSession.enemyDeaths {
		objectIDs = append(objectIDs, objectID)
	}
	sort.Slice(objectIDs, func(left int, right int) bool {
		return objectIDs[left] < objectIDs[right]
	})
	states := make([]snapshot.DeathState, 0, len(objectIDs))
	for _, objectID := range objectIDs {
		run := peerSession.enemyDeaths[objectID]
		death := run.Snapshot()
		deadlinesMS := make([]int64, 0, len(death.Deadlines))
		remainingDeadlinesMS := make([]int64, 0, len(death.Deadlines))
		for _, deadline := range death.Deadlines {
			deadlinesMS = append(deadlinesMS, deadline.Milliseconds())
			remainingDeadlinesMS = append(
				remainingDeadlinesMS,
				snapshotDurationMS(deadline-death.Elapsed),
			)
		}
		states = append(states, snapshot.DeathState{
			ObjectID: objectID, SourceObjectID: death.SourceObjectID,
			AnimationName: deathraknet.DeathAnimation(
				death.Target.OrdinaryDeathAnimation,
			),
			ElapsedMS: death.Elapsed.Milliseconds(), DeadlinesMS: deadlinesMS,
			RemainingDeadlinesMS:         remainingDeadlinesMS,
			PendingTaskCount:             death.PendingTaskCount,
			IsTargetCleared:              death.State.IsTargetCleared,
			IsImmobilized:                death.State.IsImmobilized,
			IsLocomotionStopped:          death.State.IsLocomotionStopped,
			IsCorpseFading:               death.State.IsCorpseFading,
			IsMarkedForDeletion:          death.State.IsMarkedForDeletion,
			IsPhysicsCollisionEnabled:    death.State.IsPhysicsCollisionEnabled,
			IsNavigationCollisionEnabled: death.State.IsNavigationCollisionEnabled,
		})
	}
	return states
}

func snapshotObjectives(peerSession gameplayPeerSession) []snapshot.ObjectiveState {
	objective := peerSession.zone.Objective()
	if objective == nil {
		return nil
	}
	records := objective.Snapshot()
	states := make([]snapshot.ObjectiveState, 0, len(records))
	for _, record := range records {
		states = append(states, snapshot.ObjectiveState{
			ObjectiveID: record.ObjectiveID,
			States:      record.State, Tokens: record.Token,
		})
	}
	return states
}

func snapshotZoneMembers(members []zone.Member) []snapshot.ZoneMemberState {
	states := make([]snapshot.ZoneMemberState, 0, len(members))
	for _, member := range members {
		states = append(states, snapshot.ZoneMemberState{
			UserID: member.UserID, PeerGeneration: member.PeerGeneration,
			PlayerSlot: member.Slot, AbilityCount: member.AbilityCount,
			IsReplay: member.IsReplay, IsConnected: member.IsConnected,
		})
	}
	return states
}

func snapshotDirectorPublications(
	peerSession gameplayPeerSession,
) []snapshot.DirectorPublicationState {
	director := peerSession.zone.CampaignDirectorSnapshot()
	states := make(
		[]snapshot.DirectorPublicationState, 0,
		len(director.Publications)+len(director.NamedPublications),
	)
	for _, publication := range director.Publications {
		states = append(states, snapshot.DirectorPublicationState{
			Kind: "trigger", MarkerSetOrdinal: publication.MarkerSetOrdinal,
			MarkerSetName:   publication.MarkerSetName,
			TriggerOrdinal:  publication.TriggerOrdinal,
			TriggerMarkerID: publication.TriggerMarkerID,
			EventOrdinal:    publication.EventOrdinal, EventName: publication.EventName,
			CallbackName:  publication.CallbackName,
			ListenerCount: len(publication.Listeners),
		})
	}
	for _, publication := range director.NamedPublications {
		states = append(states, snapshot.DirectorPublicationState{
			Kind: "named", PublicationID: publication.PublicationID,
			MarkerSetOrdinal: publication.MarkerSetOrdinal,
			MarkerSetName:    publication.MarkerSetName,
			EventName:        publication.EventName,
			SourceObjectID:   publication.SourceObjectID,
			ListenerCount:    len(publication.Listeners),
		})
	}
	return states
}

func snapshotSquad(
	peerSession gameplayPeerSession, capturedAt time.Time,
) snapshot.SquadState {
	if peerSession.squad == nil {
		return snapshot.SquadState{}
	}
	squadSnapshot := peerSession.squad.Snapshot()
	state := snapshot.SquadState{
		DeployedCreatureIndex: peerSession.deployedCreatureIndex,
		DeployCooldownRemainingMS: snapshotDurationMS(
			squadSnapshot.DeployCooldownEnd.Sub(capturedAt),
		),
		IsGameOver:        squadSnapshot.State.IsGameOver,
		IsRestartReserved: squadSnapshot.IsRestartReserved,
	}
	for index, character := range squadSnapshot.State.Characters {
		state.Characters = append(state.Characters, snapshot.SquadCharacterState{
			Index: uint32(index), HitPoint: character.HitPoints,
			ManaPoint:        character.ManaPoints,
			MaximumHitPoint:  peerSession.maximumHitPoints[index],
			MaximumManaPoint: peerSession.maximumManaPoints[index],
			IsAvailable:      character.IsAvailable,
			IsDeployed:       uint32(index) == peerSession.deployedCreatureIndex,
		})
	}
	return state
}

func snapshotCrystalInventory(
	inventory sim.CrystalInventory,
) snapshot.CrystalInventoryState {
	state := snapshot.CrystalInventoryState{
		Slots:              make([]snapshot.CrystalSlotState, 0, len(inventory.Slots)),
		IsDiagonalUnlocked: inventory.IsDiagonalUnlocked,
	}
	for index, slot := range inventory.Slots {
		state.Slots = append(state.Slots, snapshot.CrystalSlotState{
			Index: index, NounName: slot.NounName, NounAsset: slot.NounAsset,
			CrystalType: slot.CrystalType, CrystalLevel: slot.CrystalLevel,
			Rarity: slot.Rarity, IsOccupied: slot.IsOccupied,
		})
	}
	return state
}

func snapshotAbilityRuntimes(
	peerSession gameplayPeerSession, capturedAt time.Time,
) []snapshot.AbilityRuntimeState {
	states := make([]snapshot.AbilityRuntimeState, 0)
	for objectID, run := range peerSession.heroTraps {
		if run == nil {
			continue
		}
		states = append(states, snapshot.AbilityRuntimeState{
			Kind: "hero_trap", Name: run.assetName, ObjectID: objectID,
			OwnerObjectID: run.ownerObjectID, IsTriggered: run.isTriggered,
		})
	}
	for objectID, run := range peerSession.heroSummons {
		if run == nil {
			continue
		}
		states = append(states, snapshot.AbilityRuntimeState{
			Kind: "hero_summon", Name: run.definition.Name, ObjectID: objectID,
			OwnerObjectID: run.ownerID,
		})
	}
	for abilityID, run := range peerSession.heroStatusAreas {
		if run != nil {
			states = append(states, snapshot.AbilityRuntimeState{
				Kind: "status_area", AbilityID: abilityID,
			})
		}
	}
	for abilityID, run := range peerSession.heroAuraAreas {
		if run != nil {
			states = append(states, snapshot.AbilityRuntimeState{
				Kind: "aura_area", AbilityID: abilityID, ObjectID: run.objectID,
			})
		}
	}
	states = appendActiveAbilityRuntime(states, "basic_attack", peerSession.basicAttack != nil)
	states = appendActiveAbilityRuntime(states, "channel_drain", peerSession.heroDrain != nil)
	states = appendActiveAbilityRuntime(states, "timed_area", peerSession.heroTimedArea != nil)
	states = appendActiveAbilityRuntime(states, "channel_area", peerSession.heroChannelArea != nil)
	states = appendActiveAbilityRuntime(states, "quantum_blink", peerSession.heroQuantumBlink != nil)
	states = appendActiveAbilityRuntime(states, "fire_tempest", peerSession.fireTempestActive != nil)
	states = appendActiveAbilityRuntime(states, "plasma_sentinel", peerSession.plasmaSentinelActive != nil)
	states = appendActiveAbilityRuntime(states, "infection", peerSession.heroInfection != nil)
	states = appendActiveAbilityRuntime(states, "healing_ticks", peerSession.heroHealingTicks != nil)
	states = appendActiveAbilityRuntime(states, "charge", peerSession.heroCharge != nil)
	states = appendActiveAbilityRuntime(states, "electron_sphere", peerSession.sphereAttack != nil)
	states = appendActiveAbilityRuntime(states, "tree_of_life", peerSession.treeOfLifeRun != nil)
	states = appendAbilityObjectRuntime(states, "electron_sphere_object", peerSession.sphereObjectID)
	states = appendAbilityObjectRuntime(states, "field_medic_drone", peerSession.fieldMedicDroneObjectID)
	states = appendAbilityObjectRuntime(states, "beast_pet", peerSession.beastPetObjectID)
	states = appendAbilityObjectRuntime(states, "tree_of_life_object", peerSession.treeOfLifeObjectID)
	states = appendAbilityObjectRuntime(states, "lightspeed_effect", peerSession.lightspeedEffectObjectID)
	if peerSession.roarReductionObjectID != 0 {
		states = append(states, snapshot.AbilityRuntimeState{
			Kind: "roar_reduction", ObjectID: peerSession.roarReductionObjectID,
			RemainingMS: snapshotDurationMS(peerSession.roarReductionExpiresAt.Sub(capturedAt)),
		})
	}
	sort.Slice(states, func(left int, right int) bool {
		if states[left].Kind != states[right].Kind {
			return states[left].Kind < states[right].Kind
		}
		if states[left].AbilityID != states[right].AbilityID {
			return states[left].AbilityID < states[right].AbilityID
		}
		return states[left].ObjectID < states[right].ObjectID
	})
	return states
}

func appendActiveAbilityRuntime(
	states []snapshot.AbilityRuntimeState, kind string, isActive bool,
) []snapshot.AbilityRuntimeState {
	if !isActive {
		return states
	}
	return append(states, snapshot.AbilityRuntimeState{Kind: kind})
}

func appendAbilityObjectRuntime(
	states []snapshot.AbilityRuntimeState, kind string, objectID uint32,
) []snapshot.AbilityRuntimeState {
	if objectID == 0 {
		return states
	}
	return append(states, snapshot.AbilityRuntimeState{
		Kind: kind, ObjectID: objectID,
	})
}

func snapshotEncounterStages(
	peerSession gameplayPeerSession,
) []snapshot.EncounterStageState {
	records := peerSession.zone.Encounter().Snapshots()
	states := make([]snapshot.EncounterStageState, 0, len(records))
	for _, record := range records {
		states = append(states, snapshot.EncounterStageState{
			Key: record.Key, Stage: record.Stage,
			TerminalStages: record.TerminalStages, IsPublished: record.IsPublished,
		})
	}
	return states
}

func snapshotHordes(peerSession gameplayPeerSession) []snapshot.HordeState {
	records := peerSession.zone.Horde().Snapshots()
	states := make([]snapshot.HordeState, 0, len(records))
	for _, record := range records {
		states = append(states, snapshot.HordeState{
			MarkerSetName: record.MarkerSetName, Phase: uint8(record.Phase),
			WaveOrdinal: record.WaveOrdinal, LiveObjectIDs: record.LiveObjectIDs,
			IsGateActive:    record.IsGateActive,
			CompletionEvent: record.Completion.EventName,
		})
	}
	return states
}

func snapshotBoss(peerSession gameplayPeerSession) *snapshot.BossState {
	record := peerSession.zone.Boss().Snapshot()
	return &snapshot.BossState{
		Phase: uint8(record.Phase), LeaderObjectID: record.LeaderObjectID,
		LeaderHitPoint: record.LeaderHitPoint, LiveObjectIDs: record.LiveObjectIDs,
		FirstWaveAddObjectIDs: record.FirstWaveAddObjectIDs,
		PlanCount:             len(record.Plans), IsInitialChain: record.IsInitialChain,
		IsLeaderDeferred:      record.IsLeaderDeferred,
		IsSecondWaveRequested: record.IsSecondWaveRequested,
		IsSecondWaveAdmitted:  record.IsSecondWaveAdmitted,
		IsBeamOutReserved:     record.IsBeamOutReserved,
		IsBeamOutCommitted:    record.IsBeamOutCommitted,
	}
}

func snapshotSecurity(peerSession gameplayPeerSession) *snapshot.SecurityState {
	record := peerSession.zone.Security().Snapshot()
	return &snapshot.SecurityState{
		ObjectIDs: record.ObjectID, RouteIndex: record.RouteIndex,
		Presented: record.Presented,
	}
}

func snapshotInteractables(
	peerSession gameplayPeerSession,
) []snapshot.InteractableState {
	records := peerSession.zone.Interactable().Snapshots()
	states := make([]snapshot.InteractableState, 0, len(records))
	for _, record := range records {
		states = append(states, snapshot.InteractableState{
			ObjectID: record.ObjectID, Limit: record.Limit, Count: record.Count,
		})
	}
	return states
}

func snapshotTimelineTasks(
	peerSession gameplayPeerSession, capturedAt time.Time,
) []snapshot.TimelineTaskState {
	records := peerSession.zone.Timeline().Snapshots(capturedAt)
	states := make([]snapshot.TimelineTaskState, 0, len(records))
	for _, record := range records {
		states = append(states, snapshot.TimelineTaskState{
			Key: record.Key, DueAt: record.DueAt,
			RemainingMS: snapshotDurationMS(record.Remaining),
		})
	}
	return states
}

func appendSnapshotRandom(
	randoms []snapshot.RandomState, kind string, random *sim.SimulatorRandom,
) []snapshot.RandomState {
	if random == nil {
		return randoms
	}
	state := random.Snapshot()
	words := append([]uint32(nil), state.Words[:]...)
	payload := make([]byte, 12+len(words)*4)
	binary.LittleEndian.PutUint32(payload, state.Index)
	binary.LittleEndian.PutUint64(payload[4:], state.DrawCount)
	for index, word := range words {
		binary.LittleEndian.PutUint32(payload[12+index*4:], word)
	}
	digest := sha256.Sum256(payload)
	return append(randoms, snapshot.RandomState{
		Kind: kind, Words: words, Index: state.Index, DrawCount: state.DrawCount,
		StateSHA256: hex.EncodeToString(digest[:]),
	})
}

func snapshotDurationMS(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}

func snapshotPendingPacket(
	batchID uint64, packetIndex int, payload []byte,
) snapshot.PendingPacketState {
	state := snapshot.PendingPacketState{
		BatchID: batchID, PacketIndex: packetIndex, PayloadSize: len(payload),
		PayloadHex: hex.EncodeToString(payload), IsCritical: isCriticalPendingPacket(payload),
	}
	if len(payload) == 0 {
		return state
	}
	digest := sha256.Sum256(payload)
	state.PayloadSHA256 = hex.EncodeToString(digest[:])
	state.PacketID = payload[0]
	contract, isFound := raknet.LookupApplicationPacketContract(raknet.PacketID(state.PacketID))
	if isFound {
		state.PacketName = contract.Name
	}
	if raknet.PacketID(state.PacketID) == raknet.ActionCommandMsgs && len(payload) > 1 {
		command, err := raknet.DecodeActionCommand(payload[1:])
		if err == nil {
			state.ObjectID = command.Common.ObjectID
		}
		return state
	}
	if len(payload) >= 5 && state.PacketID >= uint8(raknet.ObjectCreate) {
		state.ObjectID = binary.LittleEndian.Uint32(payload[1:5])
	}
	return state
}

func snapshotProjectileObjects(
	peerSession gameplayPeerSession, capturedAt time.Time,
) []snapshot.ObjectState {
	projectilesByID := make(map[uint32]abilityraknet.ProjectileSnapshot)
	for _, run := range peerSession.campaignNPCProjectiles {
		collectSnapshotProjectile(projectilesByID, run, capturedAt)
	}
	for _, run := range peerSession.sageAttacks {
		collectSnapshotProjectile(projectilesByID, run, capturedAt)
	}
	for _, statusRun := range peerSession.heroProjectileRuns {
		if statusRun == nil {
			continue
		}
		collectSnapshotProjectile(projectilesByID, statusRun.projectile, capturedAt)
	}
	for _, burstRun := range peerSession.heroBurstAttacks {
		collectSnapshotBurstProjectiles(projectilesByID, burstRun, capturedAt)
	}
	collectSnapshotProjectile(
		projectilesByID, peerSession.fieldMedicDroneAttack, capturedAt,
	)
	if peerSession.fireTempestActive != nil {
		collectSnapshotProjectile(
			projectilesByID, peerSession.fireTempestActive.attack, capturedAt,
		)
	}
	objectIDs := make([]uint32, 0, len(projectilesByID))
	for objectID := range projectilesByID {
		objectIDs = append(objectIDs, objectID)
	}
	sort.Slice(objectIDs, func(left int, right int) bool {
		return objectIDs[left] < objectIDs[right]
	})
	objects := make([]snapshot.ObjectState, 0, len(objectIDs))
	for _, objectID := range objectIDs {
		projectile := projectilesByID[objectID]
		objects = append(objects, snapshot.ObjectState{
			Kind: "projectile", ObjectID: projectile.ObjectID,
			OwnerObjectID:  projectile.SourceObjectID,
			TargetObjectID: projectile.TargetObjectID,
			NounName:       projectile.ProjectileNoun,
			AbilityName:    projectile.AbilityName,
			Position:       simPosition(projectile.Position),
			TargetPosition: simPosition(projectile.TargetPosition),
			Facing:         simPosition(projectile.Direction),
			LinearVelocity: [3]float32{
				projectile.Direction.X * projectile.Speed,
				projectile.Direction.Y * projectile.Speed,
				projectile.Direction.Z * projectile.Speed,
			},
			GoalPosition: [3]float32{
				projectile.Position.X + projectile.Direction.X*projectile.RemainingDistance,
				projectile.Position.Y + projectile.Direction.Y*projectile.RemainingDistance,
				projectile.Position.Z + projectile.Direction.Z*projectile.RemainingDistance,
			},
			Speed: projectile.Speed, RemainingDistance: projectile.RemainingDistance,
			RemainingDurationMS: int64(projectile.RemainingFlightDuration / time.Millisecond),
			IsPublished:         true, IsActive: projectile.IsActive,
			IsFrozen:           projectile.IsFrozen,
			IsGravityDeflected: projectile.IsGravityDeflected,
		})
	}
	return objects
}

func collectSnapshotBurstProjectiles(
	projectilesByID map[uint32]abilityraknet.ProjectileSnapshot,
	run *abilityraknet.BurstRun, capturedAt time.Time,
) {
	for _, projectile := range run.Snapshots(capturedAt) {
		if !projectile.IsActive || projectile.ObjectID == 0 {
			continue
		}
		projectilesByID[projectile.ObjectID] = projectile
	}
}

func collectSnapshotProjectile(
	projectilesByID map[uint32]abilityraknet.ProjectileSnapshot,
	run *abilityraknet.ProjectileRun, capturedAt time.Time,
) {
	projectile := run.Snapshot(capturedAt)
	if !projectile.IsActive || projectile.ObjectID == 0 {
		return
	}
	projectilesByID[projectile.ObjectID] = projectile
}

func vec3(position game.Vec3) [3]float32 {
	return [3]float32{position.X, position.Y, position.Z}
}

func vector3(position raknet.Vector3) [3]float32 {
	return [3]float32{position.X, position.Y, position.Z}
}

func simPosition(position sim.Position) [3]float32 {
	return [3]float32{position.X, position.Y, position.Z}
}
