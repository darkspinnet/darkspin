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
	return spawn(plan, false)
}

func spawn(plan zonenpc.SpawnPlan, isRemnant bool) ([][]byte, error) {
	if !isRemnant {
		plan = zonenpc.WithPendingIntroduction(plan)
	}
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
	for attributeID, amount := range affixAttributes(zonenpc.Snapshot{Plan: plan}) {
		attribute[attributeID] = amount
	}
	basePlan := plan
	basePlan.MovementSpeedBuff = 0
	basePlan.AffixMovementSpeedBuff = 0
	basePlan.SwiftAuraSourceObjectID = 0
	basePlan.BossIdentity = zonenpc.BossIdentity{}
	actionProfile, isActionKnown := zonenpc.ActionProfileForPlan(basePlan)
	isVisible := plan.Introduction == zonenpc.SpawnIntroductionFloorWarp ||
		!isActionKnown || !actionProfile.IsSpawnStealthed
	isVisible = isVisible && !plan.IsIntroductionHidden
	if isActionKnown && (actionProfile.MovementSpeed > 0 || actionProfile.NonCombatMovementSpeed > 0) {
		nonCombatMovementSpeed := actionProfile.NonCombatMovementSpeed
		if profile.IdleMovementSpeed > 0 {
			nonCombatMovementSpeed = profile.IdleMovementSpeed
		}
		if nonCombatMovementSpeed <= 0 {
			nonCombatMovementSpeed = actionProfile.MovementSpeed
		}
		attribute[11] = nonCombatMovementSpeed
		if plan.Introduction == zonenpc.SpawnIntroductionDormant && plan.OwnerObjectID == 0 {
			// Preplaced enemies stay at their authoritative idle positions.
			// Combat uses attribute 12 when target acquisition starts pursuit.
			attribute[11] = 0
		}
		attribute[12] = actionProfile.MovementSpeed
		attribute[48] = zonenpc.EffectiveMovementSpeedBuff(plan)
	}
	isElite := plan.IsCaptain || plan.IsElite || plan.IsBoss ||
		plan.BossIdentity.HasModifier(zonenpc.EliteModifierName)
	if isElite {
		attribute[uint8(game.AttributeBodyScale)] = plan.PermanentBodyScale()
	}
	// The noun already owns its authored graphics scale. The create field is a
	// runtime multiplier, so replaying the noun scale here enlarges major actors
	// twice (for example, TutorialSpecialOne became 2.3 * 2.3).
	runtimeScale := plan.RuntimeObjectScale()
	clientPosition := plan.Position
	var createMessage raknet.ApplicationMessage = raknet.EnemyObjectCreateMessage{
		IsInitiallyHidden: !isVisible,
		ObjectID:          plan.ObjectID, Noun: util.HashID(plan.NounName),
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
			IsInitiallyHidden: !isVisible,
			ObjectID:          plan.ObjectID, Noun: util.HashID(plan.NounName),
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
			ObjectID: plan.ObjectID, IsTargetable: profile.IsTargetable && !plan.IsIntroductionHidden,
			IsInCombat: plan.IsArena,
			Stealth:    uint8(actionStealthType(plan)),
		})
		eliteAffixAssetID := util.HashID("Elite.NPCAffix")
		if profile.NPCType == 2 {
			// Destructor class identity is its own nameplate. Retain the
			// existing modifier bonuses without assigning a generic elite name.
			eliteAffixAssetID = 0
		}
		messages = append(messages, raknet.ModifierCreatedMessage{
			TargetID:          plan.ObjectID,
			ModifierGUID:      util.HashID(zonenpc.EliteModifierName),
			InstanceID:        zoneeffect.NounModifierInstanceID(plan.ObjectID),
			StartMilliseconds: permanentModifierStartMilliseconds,
			DurationMilliseconds: uint32(
				zonenpc.EliteModifierDuration.Milliseconds(),
			),
			StackCount:      1,
			NPCAffixAssetID: eliteAffixAssetID,
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
			ModifierGUID:         plan.BossIdentity.ModifierIDs[affixIndex],
			InstanceID:           instanceID,
			StartMilliseconds:    permanentModifierStartMilliseconds,
			DurationMilliseconds: uint32(zonenpc.EliteModifierDuration.Milliseconds()),
			StackCount:           1,
			NPCAffixAssetID:      util.HashID(affixName),
		})
	}
	if plan.SwiftAuraSourceObjectID != 0 {
		messages = append(messages, swiftAuraMessage(plan))
	}
	for _, aura := range plan.AffixAuras {
		message, err := affixAuraMessage(plan, aura)
		if err != nil {
			return nil, fmt.Errorf("spawnAura: %w", err)
		}
		messages = append(messages, message)
	}
	if effectName := zonenpc.PackflyBoostEffectName(plan.PackflyHelperCount); effectName != "" {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: 19, ObjectID: plan.ObjectID, IsForceAttached: true, Asset: util.HashID(effectName),
		})
	}
	if isActionKnown && actionProfile.StealthType != 0 {
		messages = append(messages, raknet.AgentBlackboardUpdateMessage{
			ObjectID: plan.ObjectID, Stealth: uint8(actionProfile.StealthType),
			IsInCombat:   plan.IsArena,
			IsTargetable: profile.IsTargetable && !plan.IsIntroductionHidden,
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
	if plan.NounName == zonenpc.MutationAgentNounName {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: 17, IsForceAttached: true, ObjectID: plan.ObjectID,
			Asset: util.HashID("effect_environment_mutant_agent_smoke_model.ServerEventDef"),
		})
	}
	if zonenpc.IsGraviticStabilizer(plan) && !isRemnant {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: 16, IsForceAttached: true,
			Asset: util.HashID(zonenpc.GraviticFieldEffectName), ObjectID: plan.ObjectID,
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
	plan = zonenpc.WithPendingIntroduction(plan)
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
			IsInCombat:   zonenpc.IsCombatMovementState(plan, targetObjectID),
			IsTargetable: plan.NPCProfile.IsTargetable && !plan.IsIntroductionHidden,
			Stealth:      uint8(actionStealthType(plan)),
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
		plan = zonenpc.WithPendingIntroduction(plan)
		spawnPackets, err := Spawn(plan)
		if err != nil {
			return nil, fmt.Errorf("dormantSpawn[%d]: %w", index, err)
		}
		blackboardPacket, err := raknet.MarshalApplication(
			raknet.AgentBlackboardUpdateMessage{
				ObjectID:     plan.ObjectID,
				IsInCombat:   plan.IsArena,
				IsTargetable: plan.NPCProfile.IsTargetable && !plan.IsIntroductionHidden,
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
				IsInCombat:   zonenpc.IsCombatMovementState(snapshot.Plan, snapshot.TargetObjectID),
				IsTargetable: snapshot.Plan.NPCProfile.IsTargetable && !snapshot.Plan.IsIntroductionHidden,
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
