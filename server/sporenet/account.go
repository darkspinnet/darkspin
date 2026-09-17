package sporenet

const tutorialCompletedProgress uint32 = 3000
const shipOnboardingCompletedProgress uint32 = 9000
const defaultInventoryCapacity uint32 = 180
const unlockedFuelTankCapacity uint32 = 5

// Account contains persistent Game progression and unlock state.
type Account struct {
	IsAllAccessGranted      bool
	IsOnlineAccessGranted   bool
	IsOverdriveUnlocked     bool
	ID                      int64
	ChainProgression        uint32
	CreatureRewards         uint32
	CurrentGameID           uint32
	CurrentPlaygroupID      uint32
	DefaultDeckPVEID        uint32
	DefaultDeckPVPID        uint32
	Level                   uint32
	XP                      uint32
	DNA                     uint32
	AvatarID                uint32
	NewPlayerInventory      uint32
	OnboardingProgress      uint32
	CashoutBonusTime        uint32
	StarLevel               uint32
	UnlockCatalysts         uint32
	UnlockDiagonalCatalysts uint32
	UnlockInventory         uint32
	UnlockFuelTanks         uint32
	UnlockPVEDecks          uint32
	UnlockPVPDecks          uint32
	UnlockStats             uint32
	UnlockInventoryIdentify uint32
	UnlockEditorFlairSlots  uint32
	Upsell                  uint32
	CapLevel                uint32
	CapProgression          uint32
}

func defaultAccount() Account {
	return Account{
		IsAllAccessGranted:      true,
		IsOnlineAccessGranted:   true,
		DefaultDeckPVEID:        1,
		DefaultDeckPVPID:        1,
		Level:                   1,
		UnlockFuelTanks:         unlockedFuelTankCapacity,
		UnlockPVPDecks:          1,
		UnlockInventoryIdentify: defaultInventoryCapacity,
	}
}

func normalizeAccount(account Account) Account {
	account.IsAllAccessGranted = true
	account.IsOnlineAccessGranted = true
	if account.UnlockPVPDecks == 0 {
		account.UnlockPVPDecks = 1
	}
	if account.UnlockInventoryIdentify == 0 {
		account.UnlockInventoryIdentify = defaultInventoryCapacity
	}
	if account.UnlockInventory <= 14 {
		purchasedCapacity := defaultInventoryCapacity + 30*account.UnlockInventory
		if account.UnlockInventoryIdentify < purchasedCapacity {
			account.UnlockInventoryIdentify = purchasedCapacity
		}
	}
	if account.UnlockFuelTanks < unlockedFuelTankCapacity {
		account.UnlockFuelTanks = unlockedFuelTankCapacity
	}
	return account
}

// IsTutorialPending reports whether a game request belongs to first-run play.
func (a Account) IsTutorialPending() bool {
	return a.OnboardingProgress < tutorialCompletedProgress
}

// IsTutorialCompleted derives the client-facing completion flag from progress.
func (a Account) IsTutorialCompleted() bool {
	return !a.IsTutorialPending()
}
