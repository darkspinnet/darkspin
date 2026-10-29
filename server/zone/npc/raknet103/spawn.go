package raknet103

import (
	"errors"
	"fmt"
	"time"

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

// SpawnSnapshot seeds the new client's animation component before its first
// update. Native bootstrap seeks from these reflected state/time fields.
func SpawnSnapshot(snapshot zonenpc.Snapshot, now time.Time, sourceTime uint64) ([][]byte, error) {
	if snapshot.IsCorruptorTransitionHidden(now) {
		// The corpse has retired but the portal-clear job has not created stage
		// two yet. Its ordinary entry will create the object for every observer.
		return nil, nil
	}
	animationName, elapsed, isPlaying := snapshot.IntroductionAnimation(now)
	deathNounName, deathElapsed, isStageOneDeathPlaying := snapshot.CorruptorStageOneDeath(now)
	if isStageOneDeathPlaying {
		// The authoritative actor has accepted stage two, but existing clients
		// still show its original noun until the delayed replacement arrives.
		snapshot.Plan.NounName = deathNounName
		animationName = zonenpc.CorruptorStageOneDeathAnimation
		elapsed = deathElapsed
		isPlaying = true
	}
	if snapshot.IsPublished && !snapshot.IsDefeated && snapshot.HitPoint > 0 &&
		snapshot.Mutation.Transform.IsActive && now.Before(snapshot.Mutation.Transform.CompleteAt) {
		animationName = "npc_mutationagent_infected_grow"
		elapsed = max(time.Duration(0), now.Sub(snapshot.Mutation.Transform.CompleteAt.Add(-zonenpc.MutationAgentTransformDuration)))
		isPlaying = true
	}
	elapsedMilliseconds := uint64(elapsed / time.Millisecond)
	reflection := raknet.ObjectReflection{}
	if isPlaying {
		animationState := util.HashID(animationName)
		// Both clients subtract clock origins then FILD the signed 64-bit
		// result. Preserve that bit pattern for starts before a fresh uptime.
		animationTime := sourceTime - elapsedMilliseconds
		if animationTime != 0 {
			// Zero bypasses native clock conversion and cannot encode a mapped
			// epoch start. Keep the ordinary baseline for that exact sentinel.
			reflection.LastAnimationState = &animationState
			reflection.LastAnimationPlayMilliseconds = &animationTime
		}
	}
	packets, err := spawnWithAnimation(snapshot.FacingSpawnPlan(), false, reflection)
	if err != nil {
		return nil, fmt.Errorf("snapshotSpawn: %w", err)
	}
	if snapshot.AdditionalKit.IsHitAnimationDisabled && !snapshot.IsDefeated {
		packet, attributeErr := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: snapshot.Plan.ObjectID, Value: map[uint8]float32{110: 1},
		})
		if attributeErr != nil {
			return nil, fmt.Errorf("snapshotHitAnimation: %w", attributeErr)
		}
		packets = append(packets, packet)
		if !snapshot.IsNavigationCollisionEnabled {
			movementPackets, movementErr := ChargeMovementState(snapshot.Plan.ObjectID,
				zonenpc.EffectiveMovementSpeedBuff(snapshot.Plan), 0, true)
			if movementErr != nil {
				return nil, fmt.Errorf("snapshotBurrow: %w", movementErr)
			}
			packets = append(packets, movementPackets...)
		}
	}
	if bodyScale := snapshot.MutationBodyScale(); bodyScale != snapshot.Plan.PermanentBodyScale() {
		packet, scaleErr := raknet.MarshalApplication(raknet.AttributeDataUpdateMessage{
			ObjectID: snapshot.Plan.ObjectID,
			Value:    map[uint8]float32{uint8(game.AttributeBodyScale): bodyScale},
		})
		if scaleErr != nil {
			return nil, fmt.Errorf("snapshotScale: %w", scaleErr)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func spawn(plan zonenpc.SpawnPlan, isRemnant bool) ([][]byte, error) {
	packets, err := spawnWithAnimation(plan, isRemnant, raknet.ObjectReflection{})
	if err != nil {
		return nil, fmt.Errorf("spawnAnimation: %w", err)
	}
	return packets, nil
}

func spawnWithAnimation(plan zonenpc.SpawnPlan, isRemnant bool, reflection raknet.ObjectReflection) ([][]byte, error) {
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
	isCollidable := profile.IsTargetable
	if plan.IsFixture && plan.IsPlacementKnown {
		isVisible = plan.IsPlacementVisible && !plan.IsIntroductionHidden
		isCollidable = plan.IsPlacementCollidable
	}
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
	isElite := plan.IsEliteModifierApplied()
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
		Scale: runtimeScale, IsCollidable: isCollidable,
	}
	if plan.OwnerObjectID != 0 && plan.NounName != "NomadDrone.Noun" {
		createMessage = raknet.ObjectCreateMessage{
			IsInitiallyHidden: !isVisible,
			ObjectID:          plan.ObjectID, Noun: util.HashID(plan.NounName),
			PositionX: clientPosition.X, PositionY: clientPosition.Y,
			PositionZ: clientPosition.Z, Scale: runtimeScale,
			Rotation: vector(plan.Rotation),
			Team:     0, OwnerID: plan.OwnerObjectID,
			IsCollisionEnabled: isCollidable,
		}
	}
	if reflection.LastAnimationState != nil {
		nounID := util.HashID(plan.NounName)
		position := vector(clientPosition)
		rotation := vector(plan.Rotation)
		assetID := uint64(0)
		team := uint8(0)
		isPlayerControlled := false
		reflection.Position = &position
		createMessage = raknet.ObjectCreateContractMessage{
			ObjectID: plan.ObjectID, Noun: &nounID, Position: &position, Rotation: &rotation,
			AssetID: &assetID, Scale: &runtimeScale, Team: &team,
			IsCollisionEnabled: &isCollidable, IsPlayerControlled: &isPlayerControlled,
			Object: reflection,
		}
	}
	messages := []raknet.ApplicationMessage{
		createMessage,
		raknet.CombatantDataUpdateMessage{
			ObjectID: plan.ObjectID, HitPoints: plan.SpawnHitPoint(),
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
		eliteAffixAssetID := uint32(0)
		if affixName := plan.EliteModifierAffixName(); affixName != "" {
			eliteAffixAssetID = util.HashID(affixName)
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
	if plan.IsCaptain {
		// The native CaptainCreate startup requests this permanent modifier.
		// Replicate it directly so clients see the captain's live status on both
		// initial spawn and rejoin; it has no NPC affix nameplate identity.
		messages = append(messages, raknet.ModifierCreatedMessage{
			TargetID:             plan.ObjectID,
			ModifierGUID:         util.HashID(zonenpc.CaptainModifierName),
			InstanceID:           zoneeffect.NounCaptainModifierInstanceID(plan.ObjectID),
			StartMilliseconds:    permanentModifierStartMilliseconds,
			DurationMilliseconds: uint32(zonenpc.EliteModifierDuration.Milliseconds()),
			StackCount:           1,
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
	createEffectName := actionProfile.PassiveCreateEffectName
	effectName := actionProfile.PassiveEffectName
	if plan.HasAffixModifier(zonenpc.ShieldedModifierName) {
		createEffectName = "cyber_shield_generate_shield.ServerEventDef"
		effectName = "citadelBasicShield_Shield.ServerEventDef"
	}
	if (plan.IsAbsorptionShieldDepleted || isRemnant) &&
		(plan.HasAffixModifier(zonenpc.ShieldedModifierName) || actionProfile.PassiveAbsorptionShield > 0) {
		createEffectName = ""
		effectName = ""
	}
	if createEffectName != "" {
		messages = append(messages, raknet.ObjectEffectMessage{
			Asset:    util.HashID(createEffectName),
			ObjectID: plan.ObjectID,
		})
	}
	if effectName != "" {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: 16, IsForceAttached: true,
			Asset:    util.HashID(effectName),
			ObjectID: plan.ObjectID,
		})
	}
	if zonenpc.IsGraviticStabilizer(plan) && !isRemnant {
		messages = append(messages, raknet.AttachedEffectMessage{
			Slot: 16, IsForceAttached: true,
			Asset: util.HashID(zonenpc.GraviticFieldEffectName), ObjectID: plan.ObjectID,
		})
	}
	if plan.Introduction == zonenpc.SpawnIntroductionDormant &&
		isActionKnown && actionProfile.PreAggroAnimationName != "" && reflection.LastAnimationState == nil {
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
