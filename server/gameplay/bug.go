package gameplay

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/game"
	zonemember "github.com/darkspinnet/darkspin/server/zone/member"
)

func (e *gameplaySessionRegistry) BugContext(
	ctx context.Context, req chat.BugContextRequest,
) (chat.BugContext, error) {
	err := ctx.Err()
	if err != nil {
		return chat.BugContext{}, fmt.Errorf("bugContextCheck: %w", err)
	}
	result := chat.BugContext{
		CapturedAt: time.Now().UTC(), UserID: req.Sender.ID,
		UserName: req.Sender.Name, GameID: req.GameID,
	}
	if e == nil {
		return result, nil
	}
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	for _, peerSession := range e.sessions {
		if peerSession.binding.UserID != uint64(req.Sender.ID) ||
			(req.GameID != 0 && peerSession.binding.GameID != req.GameID) {
			continue
		}
		return bugContextForSession(result, peerSession)
	}
	return result, nil
}

func bugContextForSession(
	result chat.BugContext, peerSession gameplayPeerSession,
) (chat.BugContext, error) {
	binding := peerSession.binding
	result.IsGameplayActive = true
	result.GameID = binding.GameID
	result.RunSeed = binding.RunSeed
	result.SessionGeneration = peerSession.generation
	result.TransportGeneration = peerSession.transportGeneration
	result.SessionStage = bugSessionStage(peerSession.stage)
	result.PlayerSlot = binding.Slot
	result.SquadID = binding.SquadID
	result.MemberLimit = binding.MemberLimit
	result.ParticipantCount = binding.ParticipantCount
	result.CrogenitorLevel = binding.AvatarLevel
	result.CampaignProgression = binding.ChainProgression
	result.IsReplay = binding.IsReplay
	result.IsCheckpointRestore = binding.IsCheckpointRestore
	result.IsOverdriveUnlocked = binding.IsOverdriveUnlocked
	result.IsDungeonSetupReady = peerSession.dungeonSetup.CanReserve()
	result.IsDungeonSetupFinal = peerSession.dungeonSetup.IsCommitted()
	result.Mission = chat.BugMission{
		Label: bugMissionLabel(binding), Asset: binding.Level,
		Mode: bugModeName(binding.Mode), Index: binding.ChainLevelIndex,
		Difficulty: binding.Difficulty,
	}
	result.Location = bugLocation(peerSession)
	result.SquadHeroes = make([]chat.BugSquadHero, 0, len(binding.Creatures))
	for index, creature := range binding.Creatures {
		if creature.ID == 0 && creature.Noun == 0 {
			continue
		}
		maximumHitPoint, maximumPowerPoint := peerSession.characterResourceMaximum(
			uint32(index),
		)
		character, isCharacterFound := peerSession.squad.Character(uint32(index))
		if !isCharacterFound {
			return result, fmt.Errorf("bugSquadCharacter[%d]: unavailable", index)
		}
		result.SquadHeroes = append(result.SquadHeroes, chat.BugSquadHero{
			Index: uint32(index), Name: creature.Name,
			CreatureID: creature.ID, NounID: creature.Noun,
			FunctionalItemCount:        creature.FunctionalItemCount,
			IgnoredFunctionalItemCount: creature.IgnoredFunctionalItemCount,
			FunctionalWeaponItemCount:  creature.FunctionalWeaponItemCount,
			MinimumFunctionalItemLevel: creature.MinimumFunctionalItemLevel,
			MaximumFunctionalItemLevel: creature.MaximumFunctionalItemLevel,
			WeaponItemLevel:            creature.WeaponItemLevel,
			WeaponDamageModifier:       creature.WeaponDamageModifier,
			GearScore:                  creature.GearScore,
			FlattenedGearScore:         creature.FlattenedGearScore,
			HitPoint:                   character.HitPoints,
			MaximumHitPoint:            maximumHitPoint,
			PowerPoint:                 character.ManaPoints,
			MaximumPowerPoint:          maximumPowerPoint,
			IsAvailable:                character.IsAvailable,
			IsDefeated:                 character.IsAvailable && character.HitPoints <= 0,
			MinimumWeaponDamage:        creature.MinimumWeaponDamage,
			MaximumWeaponDamage:        creature.MaximumWeaponDamage,
			PrimaryAttribute:           creature.DamageProfile.PrimaryAttribute,
			DamageBuff:                 creature.DamageProfile.DamageBuff,
			ProjectileDamage:           creature.DamageProfile.ProjectileDamage,
			EnergyDamageBuff:           creature.DamageProfile.EnergyDamageBuff,
			EnergyDamage:               creature.DamageProfile.EnergyDamage,
			DirectAttackDamage:         creature.DamageProfile.DirectAttackDamage,
			DirectAttackDamagePercent:  creature.DamageProfile.DirectAttackDamagePercent,
			CriticalRating:             creature.CriticalRating,
			AutoCrit:                   creature.AutoCrit,
			CriticalDamageIncrease:     creature.CriticalDamageIncrease,
			AttackSpeed:                creature.TimingProfile.AttackSpeed,
			CooldownReduction:          creature.TimingProfile.CooldownReduction,
		})
	}
	if peerSession.deployedCreatureIndex < uint32(len(binding.Creatures)) {
		creature := binding.Creatures[peerSession.deployedCreatureIndex]
		maximumHitPoint, maximumPowerPoint, err := peerSession.deployedResourceMaximum()
		if err != nil {
			return result, fmt.Errorf("bugHeroResources: %w", err)
		}
		result.Hero = chat.BugHero{
			Name: creature.Name, CreatureID: creature.ID, NounID: creature.Noun,
			ObjectID:   peerSession.deployedObjectID,
			SquadIndex: peerSession.deployedCreatureIndex,
			Level:      binding.AvatarLevel, HitPoint: peerSession.deployedHitPoint(),
			MaximumHitPoint:   maximumHitPoint,
			PowerPoint:        peerSession.deployedManaPoint(),
			MaximumPowerPoint: maximumPowerPoint,
			IsDefeated:        peerSession.deployedHitPoint() <= 0,
		}
	}
	if peerSession.zone != nil {
		zoneSnapshot := peerSession.zone.Snapshot()
		result.ZoneGeneration = zoneSnapshot.Generation
		result.ZoneState = uint32(zoneSnapshot.State)
		abilityCount, isFound := peerSession.zone.AbilityCount(
			zoneResultMember(peerSession),
		)
		if isFound {
			result.AbilityCount = abilityCount
		}
	}
	return result, nil
}

func bugMissionLabel(binding game.GameplayBinding) string {
	if binding.Mode == game.ModeTutorial || game.IsTutorialLevel(binding.Level) {
		return "TUTORIAL"
	}
	if binding.ChainLevelIndex == 0 {
		return binding.Level
	}
	planet := (binding.ChainLevelIndex-1)/4 + 1
	mission := (binding.ChainLevelIndex-1)%4 + 1
	return fmt.Sprintf("%d-%d", planet, mission)
}

func bugModeName(mode game.Mode) string {
	switch mode {
	case game.ModeTutorial:
		return "tutorial"
	case game.ModeChain:
		return "campaign"
	case game.ModeArena:
		return "arena"
	default:
		return fmt.Sprintf("unknown-%d", mode)
	}
}

func bugSessionStage(stage zonemember.Stage) string {
	switch stage {
	case zonemember.StageAwaitingStart:
		return "awaiting-start"
	case zonemember.StageChainVoteSent:
		return "chain-vote-sent"
	case zonemember.StageChainCountdownSent:
		return "chain-countdown-sent"
	case zonemember.StageResumePending:
		return "resume-pending"
	case zonemember.StagePreparing:
		return "preparing"
	case zonemember.StageDungeon:
		return "dungeon"
	default:
		return fmt.Sprintf("unknown-%d", stage)
	}
}

func bugLocation(peerSession gameplayPeerSession) chat.BugLocation {
	position := game.Vec3(peerSession.playerPosition)
	result := chat.BugLocation{X: position.X, Y: position.Y, Z: position.Z}
	if peerSession.zone == nil {
		return result
	}
	nearestDistance := float32(math.Inf(1))
	for _, markerSet := range peerSession.zone.DirectorDefinition().MarkerSets {
		for _, marker := range markerSet.Markers {
			nearestDistance = selectBugMarker(
				&result, position, markerSet.Name, marker.Name,
				marker.Position, nearestDistance,
			)
		}
		for _, trigger := range markerSet.Triggers {
			nearestDistance = selectBugMarker(
				&result, position, markerSet.Name, trigger.Name,
				trigger.Position, nearestDistance,
			)
		}
	}
	return result
}

func selectBugMarker(
	result *chat.BugLocation, position game.Vec3,
	markerSetName string, markerName string, markerPosition game.Vec3,
	nearestDistance float32,
) float32 {
	distance := position.Sub(markerPosition).Length()
	if distance >= nearestDistance {
		return nearestDistance
	}
	result.NearestMarkerSet = markerSetName
	result.NearestMarker = markerName
	result.MarkerDistance = distance
	return distance
}
