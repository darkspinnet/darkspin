package raknet103

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zoneobject "github.com/darkspinnet/darkspin/server/zone/object"
)

const permanentModifierStartMilliseconds = 1

func Spawn(plan zonenpc.SpawnPlan) ([][]byte, error) {
	err := zonenpc.ValidateSpawnPlan(plan, zoneobject.ProjectileIDStart)
	if err != nil {
		return nil, fmt.Errorf("spawnValidate: %w", err)
	}
	profile := plan.NPCProfile
	attribute := map[uint8]float32{
		0: profile.Strength, 1: profile.Dexterity, 2: profile.Mind,
		4: profile.HitPoint, 5: profile.PowerPoint,
		7: profile.DodgeRating, 9: profile.ResistRating,
		10: profile.CriticalRating,
	}
	actionProfile, isActionKnown := zonenpc.ActionProfileForPlan(plan)
	isVisible := plan.Introduction == zonenpc.SpawnIntroductionFloorWarp ||
		!isActionKnown || !actionProfile.IsSpawnStealthed
	if isActionKnown && actionProfile.MovementSpeed > 0 {
		nonCombatMovementSpeed := actionProfile.NonCombatMovementSpeed
		if nonCombatMovementSpeed <= 0 {
			nonCombatMovementSpeed = actionProfile.MovementSpeed
		}
		attribute[11] = nonCombatMovementSpeed
		attribute[12] = actionProfile.MovementSpeed
		attribute[48] = 0
	}
	if isActionKnown && actionProfile.PassiveEnergyDefense > 0 {
		attribute[uint8(game.AttributeEnergyDefense)] +=
			actionProfile.PassiveEnergyDefense
	}
	if isActionKnown && actionProfile.PassivePhysicalDefense > 0 {
		attribute[uint8(game.AttributePhysicalDefense)] +=
			actionProfile.PassivePhysicalDefense
	}
	isElite := plan.IsCaptain || plan.IsElite || plan.IsBoss ||
		plan.BossIdentity.HasModifier(zonenpc.EliteModifierName)
	if isElite {
		attribute[uint8(game.AttributeBodyScale)] = zonenpc.EliteBodyScaleBonus
	}
	// The noun already owns its authored graphics scale. The create field is a
	// runtime multiplier, so replaying the noun scale here enlarges major actors
	// twice (for example, TutorialSpecialOne became 2.3 * 2.3).
	runtimeScale := plan.PlacementScale
	if runtimeScale == 0 {
		runtimeScale = 1
	}
	clientPosition := plan.Position
	var createMessage raknet.ApplicationMessage = raknet.EnemyObjectCreateMessage{
		ObjectID: plan.ObjectID, Noun: util.HashID(plan.NounName),
		Position: raknet.Vector3{
			X: clientPosition.X, Y: clientPosition.Y, Z: clientPosition.Z,
		},
		Rotation: raknet.Vector3{
			X: plan.Rotation.X, Y: plan.Rotation.Y, Z: plan.Rotation.Z,
		},
		Scale: runtimeScale, IsCollidable: profile.IsTargetable,
	}
	if plan.OwnerObjectID != 0 && plan.NounName != "NomadDrone.Noun" {
		createMessage = raknet.ObjectCreateMessage{
			ObjectID: plan.ObjectID, Noun: util.HashID(plan.NounName),
			PositionX: clientPosition.X, PositionY: clientPosition.Y,
			PositionZ: clientPosition.Z, Scale: runtimeScale,
			Rotation: vector(plan.Rotation),
			Team:     0, OwnerID: plan.OwnerObjectID,
			IsCollisionEnabled: profile.IsTargetable,
		}
	}
	messages := []raknet.ApplicationMessage{
		createMessage,
		raknet.CombatantDataUpdateMessage{
			ObjectID: plan.ObjectID, HitPoints: profile.HitPoint,
			ManaPoints: profile.PowerPoint,
		},
		raknet.AttributeDataUpdateMessage{
			ObjectID: plan.ObjectID, Value: attribute,
		},
		raknet.ObjectUpdateMessage{
			ObjectID: plan.ObjectID, PositionX: clientPosition.X,
			PositionY: clientPosition.Y, PositionZ: clientPosition.Z,
			IsVisible: isVisible,
		},
	}
	if isElite {
		// Establish the agent component before attaching its permanent status so
		// HUD_NPCBar observes the modifier against an initialized agent.
		messages = append(messages, raknet.AgentBlackboardUpdateMessage{
			ObjectID: plan.ObjectID, IsTargetable: profile.IsTargetable,
			Stealth: uint8(actionStealthType(plan)),
		})
		messages = append(messages, raknet.ModifierCreatedMessage{
			TargetID:          plan.ObjectID,
			ModifierGUID:      util.HashID(zonenpc.EliteModifierName),
			InstanceID:        zoneeffect.NounModifierInstanceID(plan.ObjectID),
			StartMilliseconds: permanentModifierStartMilliseconds,
			DurationMilliseconds: uint32(
				zonenpc.EliteModifierDuration.Milliseconds(),
			),
			StackCount: 1,
			SourceID:   plan.ObjectID,
		})
	}
	// HUD_NPCBar reads live modifier instances, not ClassAttributes alone.
	// Replicate each authored affix after object creation on spawn and rejoin.
	for affixIndex, affixName := range plan.BossIdentity.AffixNames {
		if affixName == "" {
			continue
		}
		instanceID, instanceErr := zoneeffect.NounAffixModifierInstanceID(plan.ObjectID, affixIndex)
		if instanceErr != nil {
			return nil, fmt.Errorf("affixInstance[%d]: %w", affixIndex, instanceErr)
		}
		messages = append(messages, raknet.ModifierCreatedMessage{
			TargetID:             plan.ObjectID,
			ModifierGUID:         util.HashID(plan.BossIdentity.ModifierNames[affixIndex]),
			InstanceID:           instanceID,
			StartMilliseconds:    permanentModifierStartMilliseconds,
			DurationMilliseconds: uint32(zonenpc.EliteModifierDuration.Milliseconds()),
			StackCount:           1, SourceID: plan.ObjectID,
		})
	}
	if isActionKnown && actionProfile.StealthType != 0 {
		messages = append(messages, raknet.AgentBlackboardUpdateMessage{
			ObjectID: plan.ObjectID, Stealth: uint8(actionProfile.StealthType),
			IsTargetable: profile.IsTargetable,
		})
	}
	if isActionKnown && actionProfile.PassiveCreateEffectName != "" {
		messages = append(messages, raknet.ObjectEffectMessage{
			Asset:    util.HashID(actionProfile.PassiveCreateEffectName),
			ObjectID: plan.ObjectID,
		})
	}
	if isActionKnown && actionProfile.PassiveEffectName != "" {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: 16, IsForceAttached: true,
			Asset:    util.HashID(actionProfile.PassiveEffectName),
			ObjectID: plan.ObjectID,
		})
	}
	if plan.Introduction == zonenpc.SpawnIntroductionDormant &&
		isActionKnown && actionProfile.PreAggroAnimationName != "" {
		messages = append(messages, raknet.SetAnimationStateMessage{
			ObjectID: plan.ObjectID,
			State:    util.HashID(actionProfile.PreAggroAnimationName),
			Scale:    1,
		})
	}
	packets := make([][]byte, 0, len(messages))
	for index, message := range messages {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("spawnMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func TargetedSpawn(
	plan zonenpc.SpawnPlan, targetObjectID uint32,
) ([][]byte, error) {
	if targetObjectID == 0 {
		return nil, errors.New("target object missing")
	}
	packets, err := Spawn(plan)
	if err != nil {
		return nil, fmt.Errorf("targetSpawn: %w", err)
	}
	blackboardPacket, err := raknet.MarshalApplication(
		raknet.AgentBlackboardUpdateMessage{
			ObjectID: plan.ObjectID, TargetID: targetObjectID,
			IsInCombat: true, IsTargetable: plan.NPCProfile.IsTargetable,
			Stealth: uint8(actionStealthType(plan)),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("targetBlackboard: %w", err)
	}
	return append(packets, blackboardPacket), nil
}

func actionStealthType(plan zonenpc.SpawnPlan) game.StealthType {
	profile, isFound := zonenpc.ActionProfileForPlan(plan)
	if !isFound {
		return 0
	}
	return profile.StealthType
}

func TargetedSpawns(
	plans []zonenpc.SpawnPlan, targetObjectID uint32,
) ([][]byte, error) {
	if len(plans) == 0 {
		return nil, nil
	}
	if targetObjectID == 0 {
		return nil, errors.New("target object missing")
	}
	for index, plan := range plans {
		err := zonenpc.ValidateSpawnPlan(plan, zoneobject.ProjectileIDStart)
		if err != nil {
			return nil, fmt.Errorf("targetsValidate[%d]: %w", index, err)
		}
	}
	packets := make([][]byte, 0, len(plans)*5)
	for index, plan := range plans {
		spawnPackets, err := TargetedSpawn(plan, targetObjectID)
		if err != nil {
			return nil, fmt.Errorf("targetsMarshal[%d]: %w", index, err)
		}
		packets = append(packets, spawnPackets...)
	}
	return packets, nil
}

func DormantSpawns(plans []zonenpc.SpawnPlan) ([][]byte, error) {
	if len(plans) == 0 {
		return nil, nil
	}
	packets := make([][]byte, 0, len(plans)*5)
	for index, plan := range plans {
		spawnPackets, err := Spawn(plan)
		if err != nil {
			return nil, fmt.Errorf("dormantSpawn[%d]: %w", index, err)
		}
		blackboardPacket, err := raknet.MarshalApplication(
			raknet.AgentBlackboardUpdateMessage{
				ObjectID:     plan.ObjectID,
				IsTargetable: plan.NPCProfile.IsTargetable,
				Stealth:      uint8(actionStealthType(plan)),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("dormantBlackboard[%d]: %w", index, err)
		}
		packets = append(packets, spawnPackets...)
		packets = append(packets, blackboardPacket)
	}
	return packets, nil
}

func TargetUpdates(snapshots []zonenpc.Snapshot) ([][]byte, error) {
	packets := make([][]byte, 0, len(snapshots))
	for index, snapshot := range snapshots {
		if snapshot.IsDefeated || snapshot.Plan.IsFixture {
			continue
		}
		if snapshot.Plan.ObjectID == 0 {
			return nil, fmt.Errorf("targetUpdate[%d]: invalid", index)
		}
		packet, err := raknet.MarshalApplication(
			raknet.AgentBlackboardUpdateMessage{
				ObjectID:     snapshot.Plan.ObjectID,
				TargetID:     snapshot.TargetObjectID,
				IsInCombat:   snapshot.TargetObjectID != 0,
				IsTargetable: snapshot.Plan.NPCProfile.IsTargetable,
				Stealth:      uint8(actionStealthType(snapshot.Plan)),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("targetUpdateMarshal[%d]: %w", index, err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
