package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
	securityraknet "github.com/darkspinnet/darkspin/server/zone/security/raknet103"
)

const tutorialBossTeleporterObjectID = uint32(28)

var tutorialBossTeleport = zonesecurity.Teleport{
	Source:      game.Vec3{X: 260.83334, Y: 232.78407, Z: 20.16802},
	Destination: game.Vec3{X: -347.57553, Y: -224.60829, Z: 10.08803},
	IsBoss:      true,
}

func (e *gameplayPeerSession) tutorialTeleporterInitialState() ([][]byte, error) {
	if e == nil || e.binding.Mode != game.ModeTutorial {
		return nil, nil
	}
	packets, err := securityraknet.State(
		tutorialBossTeleporterObjectID, tutorialBossTeleport,
		e.isTutorialTeleporterActive, false,
	)
	if err != nil {
		return nil, fmt.Errorf("tutorialTeleporterState: %w", err)
	}
	return packets, nil
}

func (e *gameplayPeerSession) activateTutorialTeleporter() ([][]byte, error) {
	if e == nil || e.binding.Mode != game.ModeTutorial || e.zone == nil ||
		e.isTutorialTeleporterUsed {
		return nil, nil
	}
	isActive := !hasTutorialTeleporterThreat(e.zone.SecurityThreats())
	if e.isTutorialTeleporterActive == isActive {
		return nil, nil
	}
	packets, err := securityraknet.State(
		tutorialBossTeleporterObjectID, tutorialBossTeleport, isActive, isActive,
	)
	if err != nil {
		return nil, fmt.Errorf("tutorialTeleporterActivate: %w", err)
	}
	e.isTutorialTeleporterActive = isActive
	return packets, nil
}

func (e *gameplayPeerSession) observeTutorialTeleporter(
	previous game.Vec3, current game.Vec3, orientation raknet.Quaternion,
	timestamp uint64, now time.Time,
) ([][]byte, bool, error) {
	packets, err := e.activateTutorialTeleporter()
	if err != nil {
		return nil, false, fmt.Errorf("tutorialTeleporterObserve: %w", err)
	}
	if !e.isTutorialTeleporterActive || e.isTutorialTeleporterUsed ||
		!isTutorialTeleporterContact(previous, current, e.deployedCampaignFootprintRadius()) {
		return packets, false, nil
	}
	teleportPackets, err := securityraknet.Teleport(securityraknet.TeleportRequest{
		ObjectID: e.deployedObjectID, Teleport: tutorialBossTeleport,
		Orientation: orientation, Timestamp: timestamp,
	})
	if err != nil {
		return nil, false, fmt.Errorf("tutorialTeleporterMarshal: %w", err)
	}
	destination := raknet.Vector3{
		X: tutorialBossTeleport.Destination.X,
		Y: tutorialBossTeleport.Destination.Y,
		Z: tutorialBossTeleport.Destination.Z,
	}
	err = e.teleportPlayer(now, destination)
	if err != nil {
		return nil, false, fmt.Errorf("tutorialTeleporterMove: %w", err)
	}
	companionPackets, err := e.teleportOwnedCompanions(
		game.Vec3(destination),
		game.Quaternion{
			X: orientation.X, Y: orientation.Y,
			Z: orientation.Z, W: orientation.W,
		},
	)
	if err != nil {
		return nil, false, fmt.Errorf("tutorialTeleporterCompanion: %w", err)
	}
	err = e.syncZoneHeroPose()
	if err != nil {
		return nil, false, fmt.Errorf("tutorialTeleporterPose: %w", err)
	}
	e.isTutorialTeleporterUsed = true
	teleportPackets = append(teleportPackets, companionPackets...)
	return append(packets, teleportPackets...), true, nil
}

func hasTutorialTeleporterThreat(threats []zonesecurity.Threat) bool {
	return zonesecurity.HasThreat(tutorialBossTeleport, threats)
}

func isTutorialTeleporterContact(
	previous game.Vec3, current game.Vec3, footprintRadius float32,
) bool {
	return zonesecurity.IsInsideTrigger(
		previous, current, tutorialBossTeleport.Source, zonesecurity.TriggerRadius, footprintRadius,
	)
}
