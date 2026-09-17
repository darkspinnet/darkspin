package result

const (
	campaignPlanetsPerChain = uint32(4)
	campaignMajorLevelScale = uint32(10)
	campaignBoundaryBonus   = uint32(6)
	purifiedUnlockIndex     = uint32(17)
	maximumReward           = 4
	cashOutRollMaximum      = uint32(100)
	cashOutPurifiedMaximum  = uint32(10)
	dailyBonusNumerator     = uint64(3)
	dailyBonusDenominator   = uint64(2)
)

// RarityBands contains the two server-authored chances from which the client
// formats its Special, Rarified, and Purified roll ranges.
type RarityBands struct {
	RareChance     uint32
	PurifiedChance uint32
}

// RewardTier is the semantic cash-out item class selected by the displayed
// roll. The inventory adapter maps these tiers to the client's unique rarity
// family rather than the ordinary in-level drop rarity family.
type RewardTier uint8

const (
	RewardTierUnknown RewardTier = iota
	RewardTierSpecial
	RewardTierRarified
	RewardTierPurified
)

// RewardCount returns the bounded number of Cash Out part rewards.
func RewardCount(planetsCompleted uint8) int {
	return min(int(planetsCompleted), maximumReward)
}

// RewardLevel returns the recovered campaign-mode Cash Out item level for the
// completed difficulty and represented chain length.
func RewardLevel(difficulty uint32, planetsCompleted uint8) uint32 {
	if difficulty == 0 || planetsCompleted == 0 {
		return 0
	}
	major := (difficulty-1)/campaignPlanetsPerChain + 1
	minor := (difficulty-1)%campaignPlanetsPerChain + 1
	level := campaignMajorLevelScale*major + minor
	if minor == campaignPlanetsPerChain {
		return level + campaignBoundaryBonus
	}
	chainBonus := min(uint32(planetsCompleted)-1, campaignPlanetsPerChain-1)
	return level + chainBonus
}

// CashOutRewardRoll returns the stable one-based roll displayed beside a
// committed reward. A result retry therefore presents the same roll.
func CashOutRewardRoll(resultID uint64, rewardIndex int) uint32 {
	if resultID == 0 || rewardIndex < 0 {
		return 0
	}
	draw := resultID + uint64(rewardIndex+1)*0x9e3779b97f4a7c15
	draw ^= draw >> 30
	draw *= 0xbf58476d1ce4e5b9
	draw ^= draw >> 27
	draw *= 0x94d049bb133111eb
	draw ^= draw >> 31
	return uint32(draw%uint64(cashOutRollMaximum) + 1)
}

// CashOutRarityBands applies the medal weights exposed by the client tooltip.
// The Daily Bonus applies its recovered 1.5 multiplier before the packet's
// integer truncation. Purified rewards remain locked until 5-1 is completed.
func CashOutRarityBands(
	completedIndex uint32, planetsCompleted uint8, medalCount MedalCount,
	isDailyBonusGranted bool,
) RarityBands {
	if planetsCompleted == 0 {
		return RarityBands{}
	}
	weightedMedals := uint64(medalCount.Bronze) +
		3*uint64(medalCount.Silver) + 6*uint64(medalCount.Gold)
	averageNumerator := weightedMedals
	averageDenominator := uint64(planetsCompleted)
	if isDailyBonusGranted {
		averageNumerator *= dailyBonusNumerator
		averageDenominator *= dailyBonusDenominator
	}
	averageIncrease := averageNumerator / averageDenominator
	rareChance := min(uint64(cashOutRollMaximum), averageIncrease)
	purifiedChance := uint64(0)
	if completedIndex >= purifiedUnlockIndex {
		purifiedChance = min(uint64(cashOutPurifiedMaximum), rareChance/5)
	}
	return RarityBands{
		RareChance: uint32(rareChance), PurifiedChance: uint32(purifiedChance),
	}
}

// CashOutRewardTier applies the client-recovered boundary arithmetic to the
// same chances sent to presentation. A nonzero Purified chance enables the
// highest third band.
func CashOutRewardTier(roll uint32, bands RarityBands) RewardTier {
	if roll == 0 || roll > cashOutRollMaximum {
		return RewardTierUnknown
	}
	if bands.RareChance > cashOutRollMaximum ||
		bands.PurifiedChance > bands.RareChance {
		return RewardTierUnknown
	}
	specialMaximum := cashOutRollMaximum - bands.RareChance
	rarifiedMaximum := cashOutRollMaximum - bands.PurifiedChance
	if roll <= specialMaximum {
		return RewardTierSpecial
	}
	if roll <= rarifiedMaximum {
		return RewardTierRarified
	}
	return RewardTierPurified
}
