package gameplay

import (
	"errors"
	"fmt"
	"sort"

	"github.com/darkspinnet/darkspin/server/sim"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
)

const campaignCrystalPityMaximumMisses = uint32(55)
const campaignCrystalPityScalePerMiss = float32(0.08)
const campaignCrystalRecentTypeCount = 9
const campaignCrystalRarityPityMaximumMisses = uint32(20)

type campaignEquipmentWinnerBag struct {
	participantUserIDs []uint64
	remainingUserIDs   []uint64
	lastWinnerUserID   uint64
}

func (e campaignEquipmentWinnerBag) Clone() campaignEquipmentWinnerBag {
	return campaignEquipmentWinnerBag{
		participantUserIDs: append([]uint64(nil), e.participantUserIDs...),
		remainingUserIDs:   append([]uint64(nil), e.remainingUserIDs...),
		lastWinnerUserID:   e.lastWinnerUserID,
	}
}

func (e *campaignEquipmentWinnerBag) roll(
	participants []zoneloot.EquipmentRollParticipant,
	random *sim.SimulatorRandom,
) (zoneloot.EquipmentRollResult, campaignEquipmentWinnerBag, error) {
	if e == nil || len(participants) == 0 || random == nil {
		return zoneloot.EquipmentRollResult{}, campaignEquipmentWinnerBag{},
			errors.New("invalid equipment winner bag")
	}
	userIDs := make([]uint64, 0, len(participants))
	for _, participant := range participants {
		userIDs = append(userIDs, participant.UserID)
	}
	sort.Slice(userIDs, func(left int, right int) bool {
		return userIDs[left] < userIDs[right]
	})
	remainingUserIDs := append([]uint64(nil), e.remainingUserIDs...)
	if !sameCampaignUserIDs(userIDs, e.participantUserIDs) || len(remainingUserIDs) == 0 {
		remainingUserIDs = append([]uint64(nil), userIDs...)
	}
	choice, err := random.Index(uint32(len(remainingUserIDs)))
	if err != nil {
		return zoneloot.EquipmentRollResult{}, campaignEquipmentWinnerBag{},
			fmt.Errorf("winnerChoice: %w", err)
	}
	winnerUserID := remainingUserIDs[choice]
	if len(remainingUserIDs) == len(userIDs) && len(userIDs) > 1 &&
		winnerUserID == e.lastWinnerUserID {
		choice = (choice + 1) % uint32(len(remainingUserIDs))
		winnerUserID = remainingUserIDs[choice]
	}
	result, err := zoneloot.RollEquipment(participants, random)
	if err != nil {
		return zoneloot.EquipmentRollResult{}, campaignEquipmentWinnerBag{},
			fmt.Errorf("winnerRoll: %w", err)
	}
	selectedIndex := -1
	naturalIndex := -1
	for index := range result.Rolls {
		if result.Rolls[index].UserID == winnerUserID {
			selectedIndex = index
		}
		if result.Rolls[index].UserID == result.Winner.UserID {
			naturalIndex = index
		}
	}
	if selectedIndex < 0 || naturalIndex < 0 {
		return zoneloot.EquipmentRollResult{}, campaignEquipmentWinnerBag{},
			errors.New("equipment winner unavailable")
	}
	result.Rolls[selectedIndex].Roll, result.Rolls[naturalIndex].Roll =
		result.Rolls[naturalIndex].Roll, result.Rolls[selectedIndex].Roll
	result.Winner = result.Rolls[selectedIndex]
	remainingUserIDs = append(remainingUserIDs[:choice], remainingUserIDs[choice+1:]...)
	commit := campaignEquipmentWinnerBag{
		participantUserIDs: userIDs,
		remainingUserIDs:   remainingUserIDs,
		lastWinnerUserID:   winnerUserID,
	}
	return result, commit, nil
}

func sameCampaignUserIDs(left []uint64, right []uint64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type campaignCrystalDropBag struct {
	missCount uint32
}

func (e campaignCrystalDropBag) chanceThreshold(baseThreshold float32) float32 {
	if e.missCount >= campaignCrystalPityMaximumMisses {
		return 1
	}
	boost := 1 + float32(e.missCount)*campaignCrystalPityScalePerMiss
	return min(float32(1), baseThreshold*boost)
}

func (e *campaignCrystalDropBag) recordDrop(isDropped bool) {
	if isDropped {
		e.missCount = 0
		return
	}
	e.missCount = min(e.missCount+1, campaignCrystalPityMaximumMisses)
}

type campaignCrystalSelectionBag struct {
	recentTypes  []int32
	rarityMisses [3]uint32
}

func (e campaignCrystalSelectionBag) Clone() campaignCrystalSelectionBag {
	clone := e
	clone.recentTypes = append([]int32(nil), e.recentTypes...)
	return clone
}

func (e campaignCrystalSelectionBag) policy() ([]int32, []uint32) {
	return append([]int32(nil), e.recentTypes...), append([]uint32(nil), e.rarityMisses[:]...)
}

func (e *campaignCrystalSelectionBag) record(crystalType int32, rarity int32) {
	if e == nil {
		return
	}
	filteredTypes := make([]int32, 0, campaignCrystalRecentTypeCount)
	for _, recentType := range e.recentTypes {
		if recentType != crystalType {
			filteredTypes = append(filteredTypes, recentType)
		}
	}
	filteredTypes = append(filteredTypes, crystalType)
	if len(filteredTypes) > campaignCrystalRecentTypeCount {
		filteredTypes = filteredTypes[len(filteredTypes)-campaignCrystalRecentTypeCount:]
	}
	e.recentTypes = filteredTypes
	for rarityIndex := range e.rarityMisses {
		if int32(rarityIndex) == rarity {
			e.rarityMisses[rarityIndex] = 0
			continue
		}
		e.rarityMisses[rarityIndex] = min(
			e.rarityMisses[rarityIndex]+1,
			campaignCrystalRarityPityMaximumMisses,
		)
	}
}
