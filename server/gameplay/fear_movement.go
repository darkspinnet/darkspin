package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zonenavigation "github.com/darkspinnet/darkspin/server/zone/navigation"
)

const heroFearMovementInterval = 600 * time.Millisecond

// Refresh the goal without player input, always around the initial fear location.
// Revision and deployment checks prevent an old fear from moving a new hero.
func (e campaignNPCFearExpiryStep) move() ([][]byte, error) {
	e.runtime.registry.mutex.Lock()
	defer e.runtime.registry.mutex.Unlock()
	member, isFound := e.runtime.registry.sessions[e.sessionKey]
	now := e.runtime.now()
	if !isFound || member.generation != e.generation || member.isZoneTerminal() ||
		member.zone == nil || member.zone.NPCRandom() == nil ||
		member.campaignNPCFears[e.run.targetObjectID] != e.run ||
		e.run.revision != e.revision || member.deployedObjectID != e.run.targetObjectID ||
		member.deployedHitPoint() <= 0 || !member.isEnemyFearActive(now) {
		return nil, nil
	}
	if member.isEnemyStunActive(now) || member.isEnemySleepActive(now) ||
		member.isEnemyRootActive(now) || member.isOperativeCaged(now) {
		packets, err := member.stopHeroFearMovement(now)
		e.runtime.registry.sessions[e.sessionKey] = member
		if err != nil {
			return nil, fmt.Errorf("fearHeld: %w", err)
		}
		return packets, nil
	}
	destination, isDestinationFound, err := zonenavigation.RandomTeleportDestination(
		member.zone.Navigation(), member.zone.NPCRandom(),
		zonenavigation.RandomTeleportRequest{
			SourcePosition: e.run.origin, FootprintRadius: member.deployedCampaignNavigationRadius(),
			MinimumDistance: 1, NormalDistance: 4, MaximumDistance: 6,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("fearDestination: %w", err)
	}
	if !isDestinationFound {
		return nil, nil
	}
	err = member.startEnemyFearMovement(
		now, raknet.Vector3(destination), e.runtime.registry.passiveMovementIncrease(member),
	)
	e.runtime.registry.sessions[e.sessionKey] = member
	if err != nil {
		return nil, fmt.Errorf("fearMotion: %w", err)
	}
	packets, err := marshalHeroFearMovement(member, true)
	if err != nil {
		return nil, fmt.Errorf("fearMoveMarshal: %w", err)
	}
	return packets, nil
}

func marshalHeroFearMovement(member gameplayPeerSession, isMoving bool) ([][]byte, error) {
	goal := member.playerMovementGoal
	flags := uint32(0x01)
	if !isMoving {
		flags, goal = 0x20, member.playerPosition
	}
	targetObjectID := uint32(0)
	stopDistance := float32(0.1)
	// ObjectPlayerMove is ignored by the locally controlled hero. Reliable
	// locomotion reflection changes its goal just as it does for /follow.
	packet, err := raknet.MarshalApplication(raknet.LocomotionUpdateContractMessage{
		ObjectID: member.deployedObjectID, Locomotion: raknet.LocomotionReflection{
			GoalFlags: &flags, GoalPosition: &goal, PartialGoalPosition: &goal,
			TargetObjectID: &targetObjectID, DesiredStopDistance: &stopDistance,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("fearLocomotion: %w", err)
	}
	return [][]byte{packet}, nil
}

func (e *gameplayPeerSession) stopHeroFearMovement(now time.Time) ([][]byte, error) {
	err := e.stopPlayerMovement(now)
	if err != nil {
		return nil, fmt.Errorf("fearStop: %w", err)
	}
	packets, err := marshalHeroFearMovement(*e, false)
	if err != nil {
		return nil, fmt.Errorf("fearStopMarshal: %w", err)
	}
	return packets, nil
}
