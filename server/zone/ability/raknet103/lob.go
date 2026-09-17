package raknet103

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

func TossLaunch(
	plan zoneability.TossPlan, projectileObjectID uint32, sourceTime uint64,
) ([][]byte, error) {
	return TossLaunchForTeam(plan, projectileObjectID, sourceTime, 1)
}

func TossLaunchForTeam(
	plan zoneability.TossPlan, projectileObjectID uint32, sourceTime uint64,
	team uint8,
) ([][]byte, error) {
	if projectileObjectID == 0 || plan.SourceObjectID == 0 ||
		plan.AbilityID == 0 ||
		plan.Definition.Toss.ProjectileNoun == "" ||
		plan.Lob.Duration <= 0 || plan.Lob.StartTime < 0 {
		return nil, errors.New("invalid toss launch")
	}
	direction := lobDirection(plan.LaunchPosition, plan.Destination)
	startTimeMilliseconds := plan.Lob.StartTime.Milliseconds()
	if startTimeMilliseconds < 0 ||
		uint64(startTimeMilliseconds) > ^uint64(0)-sourceTime {
		return nil, errors.New("invalid toss source time")
	}
	messages := []raknet.ApplicationMessage{
		raknet.ProjectileObjectCreateMessage{
			ObjectID: projectileObjectID,
			Noun:     util.HashID(plan.Definition.Toss.ProjectileNoun),
			Position: positionVector(plan.LaunchPosition),
			Rotation: direction,
			Orientation: raknet.Quaternion{
				W: 1,
			},
			LinearVelocity: lobInitialVelocity(plan.Lob),
			Scale:          1,
			Team:           team,
		},
	}
	if plan.Definition.Toss.ProjectileEffectName != "" {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot:            1,
			IsForceAttached: true,
			Asset: util.HashID(
				plan.Definition.Toss.ProjectileEffectName,
			),
			ObjectID: projectileObjectID,
		})
	}
	messages = append(messages, raknet.LobLocomotionMessage{
		ObjectID: projectileObjectID,
		StartTimeMilliseconds: sourceTime +
			uint64(startTimeMilliseconds),
		Parameter: lobParameter(
			plan.LaunchPosition, plan.Destination, plan.Lob,
			plan.Lob.IsGroundCollisionOnly,
			plan.Lob.IsStopBounceOnCreature,
		),
	})
	return marshalApplicationMessages(messages, "campaignTossLaunch")
}

func TossLanding(
	plan zoneability.TossPlan, projectileObjectID uint32,
	damagePacket [][]byte, isStrongImpact bool,
) ([][]byte, error) {
	if projectileObjectID == 0 ||
		plan.Definition.Kind != sim.AbilityKindToss ||
		plan.Definition.Toss.ImpactEffectName == "" {
		return nil, errors.New("invalid toss landing")
	}
	impactEffectName := plan.Definition.Toss.ImpactEffectName
	if isStrongImpact {
		if plan.Definition.Toss.StrongImpactEffectName == "" {
			return nil, errors.New("strong toss impact unavailable")
		}
		impactEffectName = plan.Definition.Toss.StrongImpactEffectName
	}
	effectPacket, deletePacket, err := tossLandingPresentation(
		plan, projectileObjectID, impactEffectName,
	)
	if err != nil {
		return nil, fmt.Errorf("tossPresentation: %w", err)
	}
	packets := append([][]byte(nil), damagePacket...)
	switch plan.Definition.Toss.Behavior {
	case sim.TossAbilityBehaviorVoodoo:
		return append(packets, effectPacket, deletePacket), nil
	case sim.TossAbilityBehaviorTrapper:
		return append(packets, deletePacket, effectPacket), nil
	default:
		return nil,
			fmt.Errorf(
				"unsupported toss behavior: %s",
				plan.Definition.Toss.Behavior,
			)
	}
}

func TossDirectLanding(
	plan zoneability.TossPlan, projectileObjectID uint32,
	damagePacket [][]byte,
) ([][]byte, error) {
	if projectileObjectID == 0 ||
		plan.Definition.Kind != sim.AbilityKindToss ||
		plan.Definition.Toss.ImpactEffectName == "" {
		return nil, errors.New("invalid direct toss landing")
	}
	effectPacket, deletePacket, err := tossLandingPresentation(
		plan, projectileObjectID, plan.Definition.Toss.ImpactEffectName,
	)
	if err != nil {
		return nil, fmt.Errorf("directTossPresentation: %w", err)
	}
	packets := append([][]byte(nil), damagePacket...)
	return append(packets, effectPacket, deletePacket), nil
}

func tossLandingPresentation(
	plan zoneability.TossPlan, projectileObjectID uint32,
	impactEffectName string,
) ([]byte, []byte, error) {
	effectPacket, err := raknet.MarshalApplication(
		raknet.DropPresentationMessage{
			Asset:    util.HashID(impactEffectName),
			Position: positionVector(plan.Destination),
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("tossImpact: %w", err)
	}
	deletePacket, err := raknet.MarshalApplication(
		raknet.ObjectDeleteMessage{
			ObjectID: []uint32{projectileObjectID},
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("tossDelete: %w", err)
	}
	return effectPacket, deletePacket, nil
}

func CloudLobLaunch(
	plan zoneability.CloudLobPlan, projectileObjectID uint32,
	projectileIndex int, sourceTime uint64,
) ([][]byte, error) {
	if projectileObjectID == 0 || projectileIndex < 0 ||
		projectileIndex >= len(plan.Lob) ||
		plan.Lob[projectileIndex].Duration <= 0 ||
		plan.Lob[projectileIndex].StartTime < 0 {
		return nil, errors.New("invalid cloud lob launch")
	}
	startTimeMilliseconds := plan.Lob[projectileIndex].StartTime.Milliseconds()
	if startTimeMilliseconds < 0 ||
		uint64(startTimeMilliseconds) > ^uint64(0)-sourceTime {
		return nil, errors.New("invalid cloud lob source time")
	}
	packets, err := marshalApplicationMessages(
		[]raknet.ApplicationMessage{
			raknet.ProjectileObjectCreateMessage{
				ObjectID: projectileObjectID,
				Noun: util.HashID(
					plan.Definition.CloudLob.ProjectileNoun,
				),
				Position: positionVector(plan.LaunchPosition[projectileIndex]),
				Rotation: lobDirection(
					plan.LaunchPosition[projectileIndex], plan.Destination,
				),
				Orientation: raknet.Quaternion{W: 1},
				LinearVelocity: lobInitialVelocity(
					plan.Lob[projectileIndex],
				),
				Scale: 1,
				Team:  1,
			},
			raknet.AttachedEffectMessage{
				Slot:            1,
				IsForceAttached: true,
				Asset: util.HashID(
					plan.Definition.CloudLob.ProjectileEffectName,
				),
				ObjectID: projectileObjectID,
			},
			raknet.LobLocomotionMessage{
				ObjectID: projectileObjectID,
				StartTimeMilliseconds: sourceTime +
					uint64(startTimeMilliseconds),
				Parameter: lobParameter(
					plan.LaunchPosition[projectileIndex], plan.Destination,
					plan.Lob[projectileIndex], true, false,
				),
			},
		},
		"campaignCloudLobLaunch",
	)
	if err != nil {
		return nil, fmt.Errorf("cloudLobMarshal: %w", err)
	}
	return packets, nil
}

func CloudLobLanding(
	plan zoneability.CloudLobPlan, projectileObjectID uint32,
	cloudObjectID uint32,
) ([][]byte, error) {
	if projectileObjectID == 0 || cloudObjectID == 0 ||
		plan.Definition.Kind != sim.AbilityKindCloudLob ||
		plan.Definition.CloudLob.ImpactEffectName == "" ||
		plan.Definition.CloudLob.CloudNoun == "" ||
		plan.Definition.CloudLob.CloudEffectName == "" {
		return nil, errors.New("invalid cloud lob landing")
	}
	return marshalApplicationMessages(
		[]raknet.ApplicationMessage{
			raknet.ObjectDeleteMessage{
				ObjectID: []uint32{projectileObjectID},
			},
			raknet.DropPresentationMessage{
				Asset: util.HashID(
					plan.Definition.CloudLob.ImpactEffectName,
				),
				Position: positionVector(plan.Destination),
			},
			raknet.ObjectCreateMessage{
				ObjectID:  cloudObjectID,
				Noun:      util.HashID(plan.Definition.CloudLob.CloudNoun),
				PositionX: plan.Destination.X,
				PositionY: plan.Destination.Y,
				PositionZ: plan.Destination.Z,
				Scale:     1,
				Team:      1,
			},
			raknet.AttachedEffectMessage{
				Slot:            1,
				IsForceAttached: true,
				Asset: util.HashID(
					plan.Definition.CloudLob.CloudEffectName,
				),
				ObjectID: cloudObjectID,
			},
		},
		"campaignCloudLobLanding",
	)
}

func CloudEntryEffects(
	definition sim.AbilityDefinition, sourceObjectID uint32,
	targetObjectID []uint32,
) ([][]byte, error) {
	if definition.Kind != sim.AbilityKindCloudLob ||
		definition.CloudLob.CloudHitEffectName == "" ||
		sourceObjectID == 0 {
		return nil, errors.New("invalid cloud entry effect")
	}
	packets := make([][]byte, 0, len(targetObjectID))
	for index, objectID := range targetObjectID {
		if objectID == 0 {
			return nil, fmt.Errorf("invalid cloud entry target[%d]", index)
		}
		packet, err := raknet.MarshalApplication(raknet.ObjectEffectMessage{
			Asset:      util.HashID(definition.CloudLob.CloudHitEffectName),
			ObjectID:   objectID,
			AttackerID: sourceObjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("cloudEntryMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func positionVector(position sim.Position) raknet.Vector3 {
	return raknet.Vector3{X: position.X, Y: position.Y, Z: position.Z}
}

func lobDirection(start sim.Position, end sim.Position) raknet.Vector3 {
	direction := raknet.Vector3{
		X: end.X - start.X,
		Y: end.Y - start.Y,
		Z: end.Z - start.Z,
	}
	lengthSquared := direction.X*direction.X +
		direction.Y*direction.Y + direction.Z*direction.Z
	if lengthSquared == 0 {
		return raknet.Vector3{X: 1}
	}
	length := float32(math.Sqrt(float64(lengthSquared)))
	return raknet.Vector3{
		X: direction.X / length,
		Y: direction.Y / length,
		Z: direction.Z / length,
	}
}

func lobParameter(
	start sim.Position, destination sim.Position, lob sim.CrystalLob,
	isGroundCollisionOnly bool, isStopBounceOnCreature bool,
) raknet.LobParameter {
	return raknet.LobParameter{
		StartPosition:                positionVector(start),
		Destination:                  positionVector(destination),
		UpDirection:                  raknet.Vector3{Z: 1},
		PlaneDirectionVelocity:       lob.PlaneDirectionVelocity,
		Height:                       lob.Height,
		DurationSecond:               float32(lob.Duration.Seconds()),
		BounceNumber:                 int32(lob.BounceCount),
		BounceRestitution:            lob.BounceRestitution,
		IsGroundCollisionOnly:        isGroundCollisionOnly,
		IsStopBounceOnCreature:       isStopBounceOnCreature,
		PlaneDirection:               positionVector(lob.PlaneDirection),
		ReflectedPlaneDirectionSpeed: lob.PlaneDirectionVelocity,
		UpLinearParameter:            lob.UpLinearParameter,
		UpQuadraticParameter:         lob.UpQuadraticParameter,
	}
}

func lobInitialVelocity(lob sim.CrystalLob) raknet.Vector3 {
	return raknet.Vector3{
		X: lob.PlaneDirection.X * lob.PlaneDirectionVelocity,
		Y: lob.PlaneDirection.Y * lob.PlaneDirectionVelocity,
		Z: lob.UpLinearParameter,
	}
}
