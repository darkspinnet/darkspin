package gameplay

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type developerDropSource struct {
	NPC       zonenpc.Snapshot
	Challenge int32
	Distance  float32
	IsFound   bool
}

func (r gameplayPendingRuntime) createDeveloperEquipmentDrop(
	ctx context.Context, packet raknet.Packet, queuedSession gameplayPeerSession,
	command game.PlayerEventCommand,
) ([][]byte, bool, error) {
	sessionKey := packet.Address.String()
	r.registry.mutex.Lock()
	currentSession, isFound := r.registry.sessions[sessionKey]
	isCurrent := isFound && currentSession.generation == queuedSession.generation &&
		currentSession.binding.Mode == game.ModeChain && currentSession.zone != nil &&
		currentSession.zone.NPCs() != nil && currentSession.deployedObjectID != 0 &&
		!currentSession.isZoneTerminal()
	if !isCurrent {
		r.registry.mutex.Unlock()
		return nil, false, errors.New("dropCreateSession: unavailable")
	}
	source := nearestDeveloperDropSource(currentSession)
	invocation := game.CampaignScriptInvocation{
		Position:  game.Vec3(currentSession.playerPosition),
		Challenge: source.Challenge,
	}
	packets, objectID, roll, err := currentSession.spawnCampaignEquipmentWithPolicy(
		invocation, r.gameplayJoin, packet.SourceTime, source.NPC.Plan.IsBoss, true,
		command.Category,
	)
	if err != nil {
		r.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("dropCreateSpawn: %w", err)
	}
	binding := currentSession.binding
	r.registry.sessions[sessionKey] = currentSession
	r.registry.mutex.Unlock()

	err = publishCampaignPeersAfterCommit(r.registry, packet, packets)
	if err != nil {
		return nil, false, fmt.Errorf("dropCreatePublish: %w", err)
	}
	definition, isDefinitionFound := r.gameplayJoin.CampaignPartDefinition(
		roll.Part.RigblockAssetID,
	)
	messages := developerDropMessages(
		binding, source, objectID, roll, definition, isDefinitionFound,
	)
	for index, message := range messages {
		err = r.gameplayJoin.PublishSystemChat(
			ctx, int64(binding.UserID), binding.GameID, message,
		)
		if err != nil {
			r.logger.Printf(
				"RakNet developer drop chat line %d unavailable for %s: %v",
				index, packet.Address, err,
			)
		}
	}
	r.logger.Printf(
		"RakNet developer drop created object=%d rigblock=%d map=%s difficulty=%d challenge=%d for %s",
		objectID, roll.Part.RigblockAssetID, binding.Level, binding.Difficulty,
		source.Challenge, packet.Address,
	)
	return packets, true, nil
}

func nearestDeveloperDropSource(peerSession gameplayPeerSession) developerDropSource {
	source := developerDropSource{Challenge: campaignNPCEquipmentSourceAmount}
	if peerSession.zone == nil || peerSession.zone.NPCs() == nil {
		return source
	}
	position := game.Vec3(peerSession.playerPosition)
	for _, candidate := range peerSession.zone.NPCs().LiveSnapshots() {
		if candidate.Faction != zonenpc.FactionNonPlayerAligned ||
			candidate.Plan.IsFixture || candidate.Plan.IsRewardSuppressed {
			continue
		}
		distance := zonegeometry.Distance(position, candidate.Plan.Position)
		if source.IsFound && distance >= source.Distance {
			continue
		}
		source.NPC = candidate
		source.Distance = distance
		source.IsFound = true
		if candidate.Plan.NPCProfile.ChallengeValue > 0 {
			source.Challenge = candidate.Plan.NPCProfile.ChallengeValue
		} else {
			source.Challenge = campaignNPCEquipmentSourceAmount
		}
	}
	return source
}

func developerDropMessages(
	binding game.GameplayBinding,
	source developerDropSource,
	objectID uint32,
	roll campaignEquipmentRoll,
	definition game.PartDefinition,
	isDefinitionFound bool,
) []string {
	sourceDescription := "no live hostile; fallback challenge"
	if source.IsFound {
		sourceDescription = fmt.Sprintf(
			"nearest=%s object=%d distance=%.1f boss=%t",
			trimDeveloperDropNoun(source.NPC.Plan.NounName), source.NPC.Plan.ObjectID,
			source.Distance, source.NPC.Plan.IsBoss,
		)
	}
	naturalResult := "miss"
	if roll.IsNaturalDrop {
		naturalResult = "hit"
	}
	messages := []string{
		fmt.Sprintf(
			"Drop context: run=%016x map=%s difficulty=%d challenge=%d %s",
			binding.RunSeed, binding.Level, binding.Difficulty, source.Challenge,
			sourceDescription,
		),
		fmt.Sprintf(
			"Drop roll: %.6f < %.6f (loot scale %.3f) = %s; create forced",
			roll.ChanceDraw, roll.ChanceThreshold, roll.ChanceScale, naturalResult,
		),
	}
	if roll.IsLimitedEditionRolled {
		limitedResult := "ordinary base"
		if roll.LimitedEditionRigblockID != 0 {
			limitedResult = fmt.Sprintf("limited rigblock=%d", roll.LimitedEditionRigblockID)
		}
		messages = append(messages, fmt.Sprintf(
			"Boss base roll: %d/%d threshold=%d => %s",
			roll.LimitedEditionDraw, campaignBossLimitedEditionChanceBasis,
			campaignBossLimitedEditionChanceThreshold, limitedResult,
		))
	} else if source.IsFound && source.NPC.Plan.IsBoss && roll.RequestedSlotType != "" {
		messages = append(messages, "Boss base roll: skipped because a category was requested")
	}
	slot := "unknown"
	if isDefinitionFound {
		slot = definition.SlotType
	}
	requestedSlot := "any"
	if roll.RequestedSlotType != "" {
		requestedSlot = developerDropSlotDisplay(roll.RequestedSlotType)
	}
	messages = append(messages, fmt.Sprintf(
		"Item roll: choice=%d category=%s squad=%d/%d hero=%s class=%s element=%s",
		roll.PartChoice, requestedSlot, roll.PartSubjectIndex+1, roll.PartSubjectCount,
		roll.PartSubject.Name, roll.PartSubject.ClassType, roll.PartSubject.ElementType,
	))
	messages = append(messages, fmt.Sprintf(
		"Created: object=%d rigblock=%d slot=%s level=%d rarity=%s affixes=%d/%d/%d",
		objectID, roll.Part.RigblockAssetID, developerDropSlotDisplay(slot), roll.Part.Level,
		developerDropRarity(roll.Part.Rarity), roll.Part.PrefixAssetID,
		roll.Part.PrefixSecondaryAssetID, roll.Part.SuffixAssetID,
	))
	return messages
}

func developerDropSlotDisplay(slotType string) string {
	if slotType == "grasper" {
		return "hand"
	}
	return slotType
}

func developerDropRarity(rarity sporenet.PartRarity) string {
	switch rarity {
	case sporenet.PartBasic:
		return "basic"
	case sporenet.PartUncommon:
		return "uncommon"
	case sporenet.PartRare:
		return "rare"
	case sporenet.PartEpic:
		return "epic"
	case sporenet.PartUnique:
		return "unique"
	case sporenet.PartRareUnique:
		return "rare-unique"
	case sporenet.PartEpicUnique:
		return "epic-unique"
	default:
		return fmt.Sprintf("unknown-%d", rarity)
	}
}

func trimDeveloperDropNoun(nounName string) string {
	trimmed := strings.TrimSuffix(nounName, ".Noun")
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}
