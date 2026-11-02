package gameplay

import (
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/snapshot"
)

func snapshotArenaLoading(member gameplayPeerSession) *snapshot.ArenaLoadingState {
	if member.binding.Mode != game.ModeArena {
		return nil
	}
	state := &snapshot.ArenaLoadingState{
		ExpectedPlayerMask: member.binding.PlayerMask, KnownPlayerMask: member.knownPlayerMask,
		ParticipantCount: member.binding.ParticipantCount, SquadID: member.binding.SquadID,
		IsPartyMerged: member.isPartyMerged, IsLobbyTransitionSent: member.isArenaLobbyTransitionSent,
		IsLobbyEntered: member.isArenaLobbyEntered, IsSquadAccepted: member.isArenaSquadAccepted,
		IsPreparationSent:         member.isArenaPreparationSent,
		IsPreparationAcknowledged: member.isArenaPreparationAcknowledged,
		IsPlayerStatusKnown:       member.isPlayerStatusKnown,
		PlayerStatus:              uint32(member.lastPlayerStatus.Status), PlayerProgress: member.lastPlayerStatus.Progress,
		LobbyTransitionAt: member.arenaLobbyTransitionAt, LobbyTransitionCount: member.arenaLobbyTransitionCount,
	}
	for index, creature := range member.binding.Creatures {
		state.CreatureIDs[index] = creature.ID
		state.Nouns[index] = creature.Noun
	}
	return state
}

// Arena participants have no campaign zone. Capture their authoritative pose
// directly rather than returning an empty object list and zero control state.
func snapshotArenaHero(member gameplayPeerSession, capturedAt time.Time) []snapshot.ObjectState {
	if member.deployedObjectID == 0 || member.deployedCreatureIndex >= uint32(len(member.binding.Creatures)) {
		return nil
	}
	object := snapshot.ObjectState{
		Kind: "hero", ObjectID: member.deployedObjectID,
		NounID: member.binding.Creatures[member.deployedCreatureIndex].Noun,
		UserID: member.binding.UserID, PeerGeneration: member.generation,
		Position: vector3(member.playerPosition), GoalPosition: vector3(member.playerMovementGoal),
		Facing: vector3(member.attackPose.facing), TargetPosition: vector3(member.attackPose.targetPosition),
		TargetObjectID: member.attackPose.targetObjectID,
		HitPoint:       member.deployedHitPoint(), ManaPoint: member.deployedManaPoint(),
		IsDefeated: member.deployedHitPoint() <= 0, IsPublished: member.stage.IsDungeon(),
		IsTargetable: member.isArenaCombatActive && member.deployedHitPoint() > 0,
	}
	if member.playerMotion != nil {
		object.ActionRevision = member.playerMotion.Revision()
		object.LinearVelocity = simPosition(member.playerMotion.Velocity())
		position, err := member.playerMotion.SamplePosition(capturedAt)
		if err != nil {
			log.Printf("Sync Snapshot Arena position sample failed object=%d: %v", member.deployedObjectID, err)
		} else {
			object.Position = simPosition(position)
		}
	}
	return []snapshot.ObjectState{object}
}
