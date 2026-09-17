package gameplay

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zone "github.com/darkspinnet/darkspin/server/zone"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

const playerFollowDistance = float32(3)

type playerFollowPublication struct {
	zone       *zone.Zone
	movement   zoneprojection.HeroMovement
	userID     uint64
	generation uint64
}

func playerFollowGoal(
	targetPosition raknet.Vector3, targetGoal raknet.Vector3,
) raknet.Vector3 {
	if targetGoal == (raknet.Vector3{}) {
		return targetPosition
	}
	deltaX := targetGoal.X - targetPosition.X
	deltaY := targetGoal.Y - targetPosition.Y
	deltaZ := targetGoal.Z - targetPosition.Z
	distance := float32(math.Sqrt(float64(deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ)))
	if distance <= playerFollowDistance {
		return targetPosition
	}
	ratio := playerFollowDistance / distance
	return raknet.Vector3{
		X: targetGoal.X - deltaX*ratio,
		Y: targetGoal.Y - deltaY*ratio,
		Z: targetGoal.Z - deltaZ*ratio,
	}
}

func (e *gameplaySessionRegistry) updatePlayerFollowersLocked(
	target gameplayPeerSession, now time.Time, timestamp uint64,
) []playerFollowPublication {
	if e == nil || target.binding.UserID == 0 || target.zone == nil {
		return nil
	}
	publications := make([]playerFollowPublication, 0)
	goal := playerFollowGoal(target.playerPosition, target.playerMovementGoal)
	for sessionKey, follower := range e.sessions {
		isFollowing := follower.followTargetUserID == target.binding.UserID &&
			follower.binding.GameID == target.binding.GameID &&
			follower.binding.UserID != target.binding.UserID &&
			follower.stage.IsDungeon() && follower.dungeonSetup.IsCommitted() &&
			follower.deployedObjectID != 0 && !follower.isZoneTerminal()
		if !isFollowing {
			continue
		}
		err := follower.advancePlayerPosition(now, raknet.Vector3{})
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("RakNet multiplayer follow position skipped user=%d: %v", follower.binding.UserID, err)
			}
			continue
		}
		_, _, err = follower.advancePlayerMovement(
			now, follower.playerPosition, goal, false,
			e.passiveMovementIncrease(follower),
		)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("RakNet multiplayer follow advance skipped user=%d: %v", follower.binding.UserID, err)
			}
			continue
		}
		err = follower.syncZoneHeroPose()
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("RakNet multiplayer follow hero sync skipped user=%d: %v", follower.binding.UserID, err)
			}
			continue
		}
		packets, err := marshalZonePlayerMove(follower.deployedObjectID, goal)
		if err != nil {
			if e.logger != nil {
				e.logger.Printf("RakNet multiplayer follow marshal skipped user=%d: %v", follower.binding.UserID, err)
			}
			continue
		}
		follower.queuePackets(packets)
		e.sessions[sessionKey] = follower
		publications = append(publications, playerFollowPublication{
			zone: follower.zone,
			movement: zoneprojection.HeroMovement{
				ObjectID:           follower.deployedObjectID,
				Goal:               game.Vec3{X: goal.X, Y: goal.Y, Z: goal.Z},
				AnimationTimestamp: timestamp,
			},
			userID: follower.binding.UserID, generation: follower.generation,
		})
	}
	return publications
}

func publishPlayerFollowMovements(publications []playerFollowPublication) {
	for _, publication := range publications {
		if publication.zone == nil {
			continue
		}
		publication.zone.PublishHeroMovement(
			publication.movement, publication.userID, publication.generation,
		)
	}
}

func (r gameplayPendingRuntime) activatePlayerFollow(
	packet raknet.Packet, queuedSession gameplayPeerSession,
	command game.PlayerEventCommand,
) ([][]byte, bool, error) {
	if command.TargetUserID == 0 {
		return nil, false, errors.New("follow target missing")
	}
	r.registry.mutex.Lock()
	follower, isFollowerFound := r.registry.sessions[packet.Address.String()]
	isFollowerCurrent := isFollowerFound && follower.generation == queuedSession.generation &&
		follower.stage.IsDungeon() && follower.dungeonSetup.IsCommitted() &&
		follower.deployedObjectID != 0 && !follower.isZoneTerminal()
	if !isFollowerCurrent {
		r.registry.mutex.Unlock()
		return nil, false, errors.New("follow session unavailable")
	}
	target := gameplayPeerSession{}
	isTargetFound := false
	for _, candidate := range r.registry.sessions {
		if candidate.binding.GameID == follower.binding.GameID &&
			candidate.binding.UserID == command.TargetUserID &&
			candidate.stage.IsDungeon() && candidate.dungeonSetup.IsCommitted() &&
			candidate.deployedObjectID != 0 && !candidate.isZoneTerminal() {
			target = candidate
			isTargetFound = true
			break
		}
	}
	if !isTargetFound {
		r.registry.mutex.Unlock()
		return nil, false, errors.New("follow target unavailable")
	}
	now := r.now()
	err := target.advancePlayerPosition(now, raknet.Vector3{})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("followTargetPosition: %w", err)
	}
	err = follower.advancePlayerPosition(now, raknet.Vector3{})
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("followSourcePosition: %w", err)
	}
	goal := playerFollowGoal(target.playerPosition, target.playerMovementGoal)
	_, _, err = follower.advancePlayerMovement(
		now, follower.playerPosition, goal, false,
		r.registry.passiveMovementIncrease(follower),
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("followAdvance: %w", err)
	}
	err = follower.syncZoneHeroPose()
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("followHero: %w", err)
	}
	follower.followTargetUserID = command.TargetUserID
	r.registry.sessions[packet.Address.String()] = follower
	packets, err := marshalZonePlayerMove(follower.deployedObjectID, goal)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("followMarshal: %w", err)
	}
	publication := playerFollowPublication{
		zone: follower.zone,
		movement: zoneprojection.HeroMovement{
			ObjectID:           follower.deployedObjectID,
			Goal:               game.Vec3{X: goal.X, Y: goal.Y, Z: goal.Z},
			AnimationTimestamp: packet.SourceTime,
		},
		userID: follower.binding.UserID, generation: follower.generation,
	}
	r.registry.mutex.Unlock()
	publishPlayerFollowMovements([]playerFollowPublication{publication})
	r.logger.Printf(
		"RakNet multiplayer follow activated game=%d user=%d target_user=%d",
		follower.binding.GameID, follower.binding.UserID, command.TargetUserID,
	)
	return packets, true, nil
}
