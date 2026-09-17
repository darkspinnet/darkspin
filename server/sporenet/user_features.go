package sporenet

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	tutorialprogress "github.com/darkspinnet/darkspin/server/zone/tutorial/progression"
)

var (
	// ErrInvalidUser indicates that an application command has no target user.
	ErrInvalidUser = errors.New("invalid user")
	// ErrCreatureTemplateNotFound indicates that a requested noun is unknown.
	ErrCreatureTemplateNotFound = errors.New("creature template not found")
	// ErrCreatureRewardRequired protects the creature-unlock economy invariant.
	ErrCreatureRewardRequired = errors.New("creature reward required")
	// ErrSquadLocked protects a deck slot that progression has not unlocked.
	ErrSquadLocked = errors.New("squad locked")
	// ErrSquadDuplicate protects the three-distinct-members deck invariant.
	ErrSquadDuplicate = errors.New("creature repeated within squad")
	// ErrUpgradeInvalid indicates an unknown progression upgrade identifier.
	ErrUpgradeInvalid = errors.New("upgrade invalid")
	// ErrUpgradeFunds indicates that the account cannot afford an upgrade.
	ErrUpgradeFunds = errors.New("upgrade funds")
	// ErrPartExists indicates a duplicate stable inventory item identity.
	ErrPartExists = errors.New("part already exists")
	// ErrPartEquipped protects an item still owned by a creature loadout.
	ErrPartEquipped = errors.New("part equipped")
	// ErrInventoryFull protects the account's client-visible inventory ceiling.
	ErrInventoryFull = errors.New("inventory full")
	// ErrPartNotFound indicates that an inventory mutation references an item the actor does not own.
	ErrPartNotFound = errors.New("part not found")
	// ErrPartLoadoutLimit protects the six functional equipment slots.
	ErrPartLoadoutLimit = errors.New("part loadout exceeds six functional items")
	// ErrCreatureNotFound indicates that a creature mutation references an unowned instance.
	ErrCreatureNotFound = errors.New("creature not found")
	// ErrOnboardingTransition protects the recovered first-session milestone graph.
	ErrOnboardingTransition = errors.New("onboarding transition invalid")
	// ErrDNAOverflow protects account currency from wrapping.
	ErrDNAOverflow = errors.New("DNA overflow")
)

const tutorialCompletionExperience = uint32(335)
const tutorialCompletionLevel = uint32(3)
const tutorialCompletionDNA = uint32(100)
const tutorialCompletionMaximumExperience = uint32(1<<31 - 1)
const tutorialBlitzTemplateName = "Blitz Alpha"
const tutorialSageTemplateName = "Sage Alpha"
const tutorialWraithTemplateName = "Wraith Alpha"
const tutorialElectroClawsRigblock = uint16(268)
const tutorialElectroClawsLevel = uint16(5)
const maximumFunctionalItemCount = 6

// TutorialCompletion is the durable account snapshot sent to build 103 when
// the authoritative tutorial encounter completes.
type TutorialCompletion = tutorialprogress.Completion

// TutorialExperience is the durable account progression returned after an
// authoritative tutorial enemy award.
type TutorialExperience = tutorialprogress.Experience

func accountLevelForExperience(experience uint32) uint32 {
	for index, bound := range accountExperienceUpperBound {
		if experience <= bound {
			return uint32(index + 1)
		}
	}
	return uint32(len(accountExperienceUpperBound) + 1)
}

// AccountLevelForExperience maps cumulative build-103 XP to its Crogenitor
// level for live, provisional progression presentation.
func AccountLevelForExperience(experience uint32) uint32 {
	return accountLevelForExperience(experience)
}

// GrantAccountExperience applies one server-authored XP award through build
// 103's complete account-level curve. The mutation and persistence are atomic,
// and cumulative experience never wraps.
func (m *UserManager) GrantAccountExperience(ctx context.Context, userID int64, amount uint32) (TutorialExperience, error) {
	user := m.UserByID(userID)
	if user == nil {
		return TutorialExperience{}, ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	previous := user.Account
	if amount > ^uint32(0)-user.Account.XP {
		user.mu.Unlock()
		return TutorialExperience{}, errors.New("account experience overflow")
	}
	user.Account.XP += amount
	user.Account.Level = accountLevelForExperience(user.Account.XP)
	user.Account.CreatureRewards += heroRewardEntitlementCount(user.Account.Level) -
		heroRewardEntitlementCount(previous.Level)
	experience := TutorialExperience{CumulativeXP: user.Account.XP, Level: user.Account.Level}
	user.mu.Unlock()
	eventAppend := user.appendEvents(levelMilestoneEvents(previous.Level, experience.Level)...)
	previousEvents := eventAppend.previousEvents
	if amount == 0 {
		return experience, nil
	}
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreAccount(previous)
		user.restoreEvents(previousEvents)
		return TutorialExperience{}, fmt.Errorf("accountExperienceSave: %w", err)
	}
	return experience, nil
}

// GrantTutorialExperience preserves the tutorial progression port while using
// the same account XP curve as every other progression source.
func (m *UserManager) GrantTutorialExperience(ctx context.Context, userID int64, amount uint32) (TutorialExperience, error) {
	experience, err := m.GrantAccountExperience(ctx, userID, amount)
	if err != nil {
		return TutorialExperience{}, fmt.Errorf("tutorialExperience: %w", err)
	}
	return experience, nil
}

// GrantDNA atomically adds collected campaign currency and returns the durable
// account total used by the in-match sparse player update.
func (m *UserManager) GrantDNA(ctx context.Context, userID int64, amount uint32) (uint32, error) {
	user := m.UserByID(userID)
	if user == nil {
		return 0, ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	previous := user.Account
	if amount > ^uint32(0)-user.Account.DNA {
		user.mu.Unlock()
		return 0, ErrDNAOverflow
	}
	user.Account.DNA += amount
	dna := user.Account.DNA
	user.mu.Unlock()
	if amount == 0 {
		return dna, nil
	}
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreAccount(previous)
		return 0, fmt.Errorf("dnaSave: %w", err)
	}
	return dna, nil
}

// ResetTutorialExperience restores the authored progression snapshot used when
// replaying the tutorial after a game over. Completion state is intentionally
// untouched because a completed tutorial is not eligible for this restart.
func (m *UserManager) ResetTutorialExperience(ctx context.Context, userID int64) (TutorialExperience, error) {
	user := m.UserByID(userID)
	if user == nil {
		return TutorialExperience{}, ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	previous := user.Account
	user.Account.XP = 0
	user.Account.Level = 1
	experience := TutorialExperience{CumulativeXP: user.Account.XP, Level: user.Account.Level}
	user.mu.Unlock()
	if previous.XP == 0 && previous.Level == 1 {
		return experience, nil
	}
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreAccount(previous)
		return TutorialExperience{}, fmt.Errorf("tutorialResetSave: %w", err)
	}
	return experience, nil
}

// DeckUpdate is the application command decoded from api.deck.updateDecks.
type DeckUpdate struct {
	PVEActiveSlot uint32
	PVECreatures  []uint32
	PVPActiveSlot uint32
	PVPCreatures  []uint32
}

// CreatureUpdate is the application command decoded from api.creature.updateCreature.
type CreatureUpdate struct {
	CreatureID     uint32
	GearScore      float32
	ItemPoints     float32
	Stats          string
	AbilityStats   string
	EquippedPartID []uint64
	LargeImageURL  string
	ThumbImageURL  string
}

// UpdateCreature durably applies one editor save, including its authoritative equipped-part set.
func (m *UserManager) UpdateCreature(ctx context.Context, user *User, command CreatureUpdate) (*Creature, error) {
	if user == nil {
		return nil, ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	m.mu.RLock()
	partScorer := m.partScorer
	m.mu.RUnlock()
	user.mu.Lock()
	previousCreature := cloneCreatures(user.Creatures)
	previousPart := append([]Part(nil), user.Parts...)
	var creature *Creature
	for _, candidate := range user.Creatures {
		if candidate != nil && candidate.ID == command.CreatureID {
			creature = candidate
			break
		}
	}
	if creature == nil {
		user.mu.Unlock()
		return nil, ErrCreatureNotFound
	}
	selected := make(map[uint64]struct{}, len(command.EquippedPartID))
	for _, itemID := range command.EquippedPartID {
		selected[itemID] = struct{}{}
	}
	for itemID := range selected {
		isOwned := false
		for index := range user.Parts {
			if user.Parts[index].ID == itemID &&
				user.Parts[index].MarketStatus == PartMarketOwned {
				isOwned = true
				break
			}
		}
		if !isOwned {
			user.mu.Unlock()
			return nil, ErrPartNotFound
		}
	}
	functionalItemCount := 0
	for index := range user.Parts {
		part := &user.Parts[index]
		if _, isSelected := selected[part.ID]; !isSelected || part.IsFlair {
			continue
		}
		functionalItemCount++
		if functionalItemCount > maximumFunctionalItemCount {
			user.mu.Unlock()
			return nil, ErrPartLoadoutLimit
		}
	}
	for index := range user.Parts {
		if _, isSelected := selected[user.Parts[index].ID]; isSelected {
			user.Parts[index].EquippedToCreatureID = command.CreatureID
		} else if user.Parts[index].EquippedToCreatureID == command.CreatureID {
			user.Parts[index].EquippedToCreatureID = 0
		}
	}
	gearScore := command.GearScore
	itemPoints := command.ItemPoints
	if partScorer != nil {
		equippedParts := make([]Part, 0, maximumFunctionalItemCount)
		for _, part := range user.Parts {
			if part.EquippedToCreatureID != command.CreatureID || part.IsFlair {
				continue
			}
			equippedParts = append(equippedParts, part)
			if len(equippedParts) == maximumFunctionalItemCount {
				break
			}
		}
		gearScore, itemPoints = partScorer.EquipmentScore(equippedParts)
	}
	creature.Update(gearScore, itemPoints, command.Stats, command.AbilityStats)
	creature.Version++
	if command.LargeImageURL != "" {
		creature.LargeImageURL = command.LargeImageURL
	}
	if command.ThumbImageURL != "" {
		creature.ThumbImageURL = command.ThumbImageURL
	}
	result := *creature
	user.mu.Unlock()
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		user.Creatures = previousCreature
		user.Parts = previousPart
		user.mu.Unlock()
		return nil, fmt.Errorf("creatureSave: %w", err)
	}
	return &result, nil
}

// CreatureByID finds a user's creature.
func (u *User) CreatureByID(id uint32) *Creature {
	u.mu.RLock()
	defer u.mu.RUnlock()
	for _, creature := range u.Creatures {
		if creature.ID == id {
			return creature
		}
	}
	return nil
}

// UnlockCreature validates and persists one creature unlock. This application
// operation is the only path transports should use for this mutation.
func (m *UserManager) UnlockCreature(ctx context.Context, user *User, noun uint32) (uint32, error) {
	if user == nil {
		return 0, ErrInvalidUser
	}
	if m.template == nil {
		return 0, ErrCreatureTemplateNotFound
	}
	template := m.template.ByNoun(noun)
	if template == nil {
		return 0, ErrCreatureTemplateNotFound
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	id, isChanged, err := user.unlockCreature(template)
	if err != nil {
		return 0, fmt.Errorf("domainUnlock: %w", err)
	}
	if !isChanged {
		return id, nil
	}
	eventAppend := user.appendEvents(creatureAcquiredEvent(template, id))
	previousEvents := eventAppend.previousEvents
	err = m.repository.Save(ctx, user.Record())
	if err != nil {
		user.rollbackCreatureUnlock(id)
		user.restoreEvents(previousEvents)
		return 0, fmt.Errorf("unlockSave: %w", err)
	}
	return id, nil
}

// UpdateDecks validates creature ownership and durably applies every submitted
// three-creature deck. Persistence failure restores the aggregate.
func (m *UserManager) UpdateDecks(ctx context.Context, user *User, command DeckUpdate) error {
	if user == nil {
		return ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	previousAccount, previousSquads, isChanged, err := user.updateDecks(command)
	if err != nil {
		return fmt.Errorf("domainDecks: %w", err)
	}
	if !isChanged {
		return nil
	}
	err = m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreDecks(previousAccount, previousSquads)
		return fmt.Errorf("decksSave: %w", err)
	}
	return nil
}

func (u *User) updateDecks(command DeckUpdate) (Account, []Squad, bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	previousAccount := u.Account
	previousSquads := append([]Squad(nil), u.Squads...)
	ownedIDs := make(map[uint32]struct{}, len(u.Creatures))
	for _, creature := range u.Creatures {
		if creature != nil {
			ownedIDs[creature.ID] = struct{}{}
		}
	}
	isPVERequested := hasCreatureID(command.PVECreatures)
	isPVPRequested := hasCreatureID(command.PVPCreatures)
	isChanged := false
	var err error
	if isPVERequested {
		isChanged, err = updateSquadSlots(
			u.Squads, command.PVEActiveSlot, command.PVECreatures, "pve", ownedIDs,
		)
		if err != nil {
			return previousAccount, previousSquads, false, fmt.Errorf("pveDeck: %w", err)
		}
		isChanged = updateActiveDeck(
			u.Squads, command.PVEActiveSlot, "pve", &u.Account.DefaultDeckPVEID,
		) || isChanged
	}
	if isPVPRequested {
		isPVPChanged, updateErr := updateSquadSlots(
			u.Squads, command.PVPActiveSlot, command.PVPCreatures, "pvp", ownedIDs,
		)
		if updateErr != nil {
			u.Account = previousAccount
			copy(u.Squads, previousSquads)
			return previousAccount, previousSquads, false, fmt.Errorf("pvpDeck: %w", updateErr)
		}
		isPVPActiveChanged := updateActiveDeck(
			u.Squads, command.PVPActiveSlot, "pvp", &u.Account.DefaultDeckPVPID,
		)
		isChanged = isChanged || isPVPChanged || isPVPActiveChanged
	}
	return previousAccount, previousSquads, isChanged, nil
}

func hasCreatureID(creatureIDs []uint32) bool {
	for _, creatureID := range creatureIDs {
		if creatureID != 0 {
			return true
		}
	}
	return false
}

func updateSquadSlots(
	squads []Squad, activeSlot uint32, requestedIDs []uint32, category string,
	ownedIDs map[uint32]struct{},
) (bool, error) {
	if activeSlot == 0 || len(requestedIDs) == 0 {
		return false, nil
	}
	for requestOffset := 0; requestOffset < len(requestedIDs); requestOffset += 3 {
		assignedIDs := make(map[uint32]struct{}, 3)
		requestLimit := min(requestOffset+3, len(requestedIDs))
		for _, creatureID := range requestedIDs[requestOffset:requestLimit] {
			if creatureID == 0 {
				continue
			}
			if _, isAssigned := assignedIDs[creatureID]; isAssigned {
				return false, ErrSquadDuplicate
			}
			assignedIDs[creatureID] = struct{}{}
		}
	}
	indexes := make([]int, 0, len(squads))
	// Build 103 serializes the active deck first, followed by the remaining
	// records from the same PVE or PVP destination in account-response order.
	for index := range squads {
		if isSquadDestination(squads[index], category) && squads[index].Slot == activeSlot {
			indexes = append(indexes, index)
			break
		}
	}
	if len(indexes) == 0 {
		return false, nil
	}
	for index := range squads {
		if isSquadDestination(squads[index], category) && squads[index].Slot != activeSlot {
			indexes = append(indexes, index)
		}
	}
	isChanged := false
	for requestIndex, squadIndex := range indexes {
		requestOffset := requestIndex * len(squads[squadIndex].CreatureIDs)
		if requestOffset >= len(requestedIDs) {
			break
		}
		creatures := [3]uint32{}
		for creatureIndex := range creatures {
			requestedIndex := requestOffset + creatureIndex
			if requestedIndex >= len(requestedIDs) {
				break
			}
			creatureID := requestedIDs[requestedIndex]
			if creatureID == 0 {
				continue
			}
			if _, isFound := ownedIDs[creatureID]; !isFound {
				continue
			}
			creatures[creatureIndex] = creatureID
		}
		isCategoryChanged := hasCreatureID(creatures[:]) && squads[squadIndex].Category != category
		if squads[squadIndex].CreatureIDs == creatures && !isCategoryChanged {
			continue
		}
		if squads[squadIndex].IsLocked {
			return false, ErrSquadLocked
		}
		squads[squadIndex].CreatureIDs = creatures
		if isCategoryChanged {
			squads[squadIndex].Category = category
		}
		isChanged = true
	}
	return isChanged, nil
}

func isSquadDestination(squad Squad, category string) bool {
	return squad.Category == category || category == "pve" && squad.Category == ""
}

func updateActiveDeck(
	squads []Squad, activeSlot uint32, category string, activeDeckID *uint32,
) bool {
	for _, squad := range squads {
		if squad.Category != category || squad.Slot != activeSlot || *activeDeckID == squad.ID {
			continue
		}
		*activeDeckID = squad.ID
		return true
	}
	return false
}

func (u *User) restoreDecks(previousAccount Account, previousSquads []Squad) {
	u.mu.Lock()
	u.Account = previousAccount
	u.Squads = append(u.Squads[:0], previousSquads...)
	u.mu.Unlock()
}

// prepareProfileStart repairs durable profile state before build 103 receives
// its account projection.
func (m *UserManager) prepareProfileStart(ctx context.Context, user *User) error {
	if user == nil {
		return ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	previousAccount, previousSquads, isChanged := user.recoverThirdHeroLesson()
	maximumCreatureCount := 0
	if m.template != nil {
		maximumCreatureCount = len(m.template.List())
	}
	isRewardChanged := user.repairHeroRewards(maximumCreatureCount)
	isPVEDeckChanged := user.repairPVEDeck()
	isPVPDeckChanged := user.repairPVPDeck()
	isChanged = isChanged || isRewardChanged || isPVEDeckChanged || isPVPDeckChanged
	if !isChanged {
		return nil
	}
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreDecks(previousAccount, previousSquads)
		return fmt.Errorf("profileRepairSave: %w", err)
	}
	return nil
}

// repairPVEDeck keeps the selected campaign squad playable by preserving its
// distinct owned members and filling invalid positions before the native
// editor validates the selected group.
func (e *User) repairPVEDeck() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Account.OnboardingProgress <= tutorialCompletedProgress {
		return false
	}
	ownedIDs := make(map[uint32]struct{}, len(e.Creatures))
	orderedOwnedIDs := make([]uint32, 0, len(e.Creatures))
	for _, creature := range e.Creatures {
		if creature == nil || creature.ID == 0 {
			continue
		}
		if _, isOwned := ownedIDs[creature.ID]; isOwned {
			continue
		}
		ownedIDs[creature.ID] = struct{}{}
		orderedOwnedIDs = append(orderedOwnedIDs, creature.ID)
	}
	if len(orderedOwnedIDs) < 3 {
		return false
	}
	targetIndex := -1
	for squadIndex := range e.Squads {
		squad := e.Squads[squadIndex]
		if squad.ID == e.Account.DefaultDeckPVEID && !squad.IsLocked &&
			(squad.Category == "pve" || squad.Category == "") {
			targetIndex = squadIndex
			break
		}
	}
	if targetIndex < 0 {
		for squadIndex := range e.Squads {
			squad := e.Squads[squadIndex]
			if squad.Category == "pve" && !squad.IsLocked {
				targetIndex = squadIndex
				break
			}
		}
	}
	if targetIndex < 0 {
		for squadIndex := range e.Squads {
			squad := e.Squads[squadIndex]
			if squad.Category == "" && !squad.IsLocked {
				targetIndex = squadIndex
				break
			}
		}
	}
	if targetIndex < 0 {
		return false
	}
	isChanged := false
	assignedIDs := make(map[uint32]struct{}, len(orderedOwnedIDs))
	targetSquad := &e.Squads[targetIndex]
	for creatureIndex, creatureID := range targetSquad.CreatureIDs {
		_, isOwned := ownedIDs[creatureID]
		_, isAssigned := assignedIDs[creatureID]
		if creatureID == 0 || !isOwned || isAssigned {
			if creatureID != 0 {
				targetSquad.CreatureIDs[creatureIndex] = 0
				isChanged = true
			}
			continue
		}
		assignedIDs[creatureID] = struct{}{}
	}
	for creatureIndex, creatureID := range targetSquad.CreatureIDs {
		if creatureID != 0 {
			continue
		}
		for _, ownedID := range orderedOwnedIDs {
			if _, isAssigned := assignedIDs[ownedID]; isAssigned {
				continue
			}
			targetSquad.CreatureIDs[creatureIndex] = ownedID
			assignedIDs[ownedID] = struct{}{}
			isChanged = true
			break
		}
	}
	if targetSquad.Category != "pve" {
		targetSquad.Category = "pve"
		isChanged = true
	}
	if e.Account.DefaultDeckPVEID != targetSquad.ID {
		e.Account.DefaultDeckPVEID = targetSquad.ID
		isChanged = true
	}
	return isChanged
}

// repairPVPDeck provisions the unlocked native Arena destination with one
// selectable three-creature squad while preserving every authored PVE squad.
func (e *User) repairPVPDeck() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Account.UnlockPVPDecks == 0 {
		return false
	}
	existingPVPIndex := -1
	for squadIndex := range e.Squads {
		squad := e.Squads[squadIndex]
		if squad.Category != "pvp" {
			continue
		}
		if squad.ID == e.Account.DefaultDeckPVPID {
			if squad.Slot == 1 {
				return false
			}
			e.Squads[squadIndex].Slot = 1
			return true
		}
		if existingPVPIndex < 0 {
			existingPVPIndex = squadIndex
		}
	}
	if existingPVPIndex >= 0 {
		targetSquad := &e.Squads[existingPVPIndex]
		targetSquad.Slot = 1
		e.Account.DefaultDeckPVPID = targetSquad.ID
		return true
	}
	ownedIDs := make(map[uint32]struct{}, len(e.Creatures))
	for _, creature := range e.Creatures {
		if creature != nil {
			ownedIDs[creature.ID] = struct{}{}
		}
	}
	sourceIndex := -1
	targetIndex := -1
	for squadIndex := range e.Squads {
		squad := e.Squads[squadIndex]
		if squad.ID == e.Account.DefaultDeckPVEID && squad.Category == "pve" &&
			isCompleteOwnedSquad(squad, ownedIDs) {
			sourceIndex = squadIndex
		}
		if targetIndex < 0 && squad.Category == "" && !squad.IsLocked {
			targetIndex = squadIndex
		}
	}
	if sourceIndex < 0 {
		for squadIndex := range e.Squads {
			squad := e.Squads[squadIndex]
			if squad.Category == "pve" && isCompleteOwnedSquad(squad, ownedIDs) {
				sourceIndex = squadIndex
				break
			}
		}
	}
	if sourceIndex < 0 || targetIndex < 0 {
		return false
	}
	sourceSquad := e.Squads[sourceIndex]
	targetSquad := &e.Squads[targetIndex]
	targetSquad.Category = "pvp"
	targetSquad.Slot = 1
	targetSquad.CreatureIDs = sourceSquad.CreatureIDs
	e.Account.DefaultDeckPVPID = targetSquad.ID
	return true
}

func isCompleteOwnedSquad(squad Squad, ownedIDs map[uint32]struct{}) bool {
	assignedIDs := make(map[uint32]struct{}, len(squad.CreatureIDs))
	for _, creatureID := range squad.CreatureIDs {
		if creatureID == 0 {
			return false
		}
		if _, isOwned := ownedIDs[creatureID]; !isOwned {
			return false
		}
		if _, isAssigned := assignedIDs[creatureID]; isAssigned {
			return false
		}
		assignedIDs[creatureID] = struct{}{}
	}
	return true
}

func (u *User) recoverThirdHeroLesson() (Account, []Squad, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	previousAccount := u.Account
	previousSquad := append([]Squad(nil), u.Squads...)
	if u.Account.OnboardingProgress != tutorialCompletedProgress {
		return previousAccount, previousSquad, false
	}
	if len(u.Creatures) >= 4 {
		u.Account.OnboardingProgress = 4000
		u.Account.CreatureRewards = 0
		for index := range u.Squads {
			squad := &u.Squads[index]
			if squad.ID != u.Account.DefaultDeckPVEID || squad.Category != "pve" ||
				squad.CreatureIDs[0] == 0 || squad.CreatureIDs[1] == 0 || squad.CreatureIDs[2] != 0 {
				continue
			}
			var reserveID uint32
			for _, creature := range u.Creatures {
				if creature == nil || creature.ID == squad.CreatureIDs[0] ||
					creature.ID == squad.CreatureIDs[1] || creature.ID <= reserveID {
					continue
				}
				reserveID = creature.ID
			}
			if reserveID != 0 {
				squad.CreatureIDs[2] = reserveID
			}
			break
		}
		return previousAccount, previousSquad,
			u.Account != previousAccount || !slices.Equal(u.Squads, previousSquad)
	}
	if len(u.Creatures) != 3 || u.Account.CreatureRewards != 0 {
		return previousAccount, previousSquad, false
	}
	for index := range u.Squads {
		squad := &u.Squads[index]
		if squad.ID != u.Account.DefaultDeckPVEID || squad.Category != "pve" ||
			squad.CreatureIDs[0] == 0 || squad.CreatureIDs[1] == 0 || squad.CreatureIDs[2] == 0 {
			continue
		}
		squad.CreatureIDs[2] = 0
		u.Account.CreatureRewards = 1
		return previousAccount, previousSquad, true
	}
	return previousAccount, previousSquad, false
}

func (e *User) repairHeroRewards(maximumCreatureCount int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	earned := heroRewardEntitlementCount(e.Account.Level)
	if e.Account.IsTutorialCompleted() {
		earned++
	}
	unlocked := uint32(0)
	if len(e.Creatures) > 3 {
		unlocked = uint32(len(e.Creatures) - 3)
	}
	expected := uint32(0)
	maximumAccountLevel := uint32(len(accountExperienceUpperBound) + 1)
	if e.Account.Level == maximumAccountLevel && maximumCreatureCount > len(e.Creatures) {
		expected = uint32(maximumCreatureCount - len(e.Creatures))
	} else if earned > unlocked {
		expected = earned - unlocked
	}
	if e.Account.CreatureRewards >= expected {
		return false
	}
	e.Account.CreatureRewards = expected
	return true
}

// UnlockUpgrade validates, applies, and persists one account progression
// purchase. Persistence failure restores the complete account snapshot.
func (m *UserManager) UnlockUpgrade(ctx context.Context, user *User, unlockID uint32) error {
	if user == nil {
		return ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	previous, err := user.unlockUpgrade(unlockID)
	if err != nil {
		return fmt.Errorf("domainUpgrade: %w", err)
	}
	err = m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreAccount(previous)
		return fmt.Errorf("upgradeSave: %w", err)
	}
	return nil
}

// SetSettings replaces the supplied account settings and persists them.
func (m *UserManager) SetSettings(ctx context.Context, user *User, settings map[string]string) error {
	if user == nil {
		return ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	previous, isChanged := user.setSettings(settings)
	if !isChanged {
		return nil
	}
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreSettings(previous)
		return fmt.Errorf("settingsSave: %w", err)
	}
	return nil
}

// UpdateOnboarding persists the client-observed first-run and ship lesson
// milestones plus the separate first-run inventory marker. Combat completion
// remains the only authority that advances tutorial progress to 3000.
func (m *UserManager) UpdateOnboarding(
	ctx context.Context, user *User, progress *uint32, inventory *uint32,
) error {
	if user == nil {
		return ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	previous := user.Account
	if progress != nil {
		if !isOnboardingTransition(user.Account.OnboardingProgress, *progress) {
			user.mu.Unlock()
			return ErrOnboardingTransition
		}
		user.Account.OnboardingProgress = *progress
	}
	if inventory != nil {
		user.Account.NewPlayerInventory = *inventory
	}
	isChanged := user.Account != previous
	user.mu.Unlock()
	if !isChanged {
		return nil
	}
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreAccount(previous)
		return fmt.Errorf("onboardingSave: %w", err)
	}
	return nil
}

func isOnboardingTransition(current uint32, requested uint32) bool {
	if requested < current {
		return false
	}
	switch requested {
	case 1000:
		return current == 0 || current == requested
	case 2000:
		return current == 1000 || current == requested
	case 3000, 4000, 5000, 6000, 6500, 6800, 8000, 9000:
		return current >= tutorialCompletedProgress
	default:
		return false
	}
}

// CompleteTutorial advances the server-owned onboarding boundary and persists
// the cumulative XP/level snapshot consumed by TutorialGameMsgs subtype 0.
// The floor is the video-observed 335/level-3 tutorial result, which also
// normalizes compatibility routes that permit an optional encounter bypass.
// Replays are idempotent and never reduce later account progression.
func (m *UserManager) CompleteTutorial(ctx context.Context, userID int64) (TutorialCompletion, error) {
	user := m.UserByID(userID)
	if user == nil {
		return TutorialCompletion{}, ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	var blitzTemplate *TemplateCreature
	var sageTemplate *TemplateCreature
	var wraithTemplate *TemplateCreature
	if m.template != nil {
		blitzTemplate = m.template.ByName(tutorialBlitzTemplateName)
		sageTemplate = m.template.ByName(tutorialSageTemplateName)
		wraithTemplate = m.template.ByName(tutorialWraithTemplateName)
		if blitzTemplate == nil || sageTemplate == nil || wraithTemplate == nil {
			return TutorialCompletion{}, ErrCreatureTemplateNotFound
		}
	}
	previous := user.Record()
	user.mu.Lock()
	isFirstCompletion := user.Account.IsTutorialPending()
	if user.Account.XP > tutorialCompletionMaximumExperience {
		user.mu.Unlock()
		return TutorialCompletion{}, errors.New("tutorial experience exceeds build-103 width")
	}
	isChanged := completeTutorialAccount(&user.Account)
	blitzID := uint32(0)
	if blitzTemplate != nil && sageTemplate != nil && wraithTemplate != nil {
		starterChanged := user.completeTutorialStarterSquad(blitzTemplate, sageTemplate, wraithTemplate)
		isChanged = isChanged || starterChanged
		blitzID = user.creatureIDByTemplateName(blitzTemplate.Name)
		if isFirstCompletion && user.Account.CreatureRewards == 0 {
			user.Account.CreatureRewards = 1
			isChanged = true
		}
		if user.IsTutorialCompletionPending {
			user.IsTutorialCompletionPending = false
			isChanged = true
		}
	}
	part := NewPart(tutorialElectroClawsRigblock)
	part.Level = tutorialElectroClawsLevel
	part.Rarity = PartBasic
	partChanged, err := user.grantPartOnce(userID, part)
	if err != nil {
		user.mu.Unlock()
		return TutorialCompletion{}, fmt.Errorf("tutorialPart: %w", err)
	}
	partEquipped := user.equipTutorialPart(part, blitzID)
	isChanged = isChanged || partChanged || partEquipped
	completion := TutorialCompletion{CumulativeXP: int32(user.Account.XP), IsChanged: isChanged}
	user.mu.Unlock()
	if !isChanged {
		return completion, nil
	}
	err = m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreTutorialCompletion(previous)
		return TutorialCompletion{}, fmt.Errorf("tutorialSave: %w", err)
	}
	return completion, nil
}

// CompletePendingTutorials materializes deferred starter loadouts after the
// authoritative content templates become available. Each profile remains
// pending until its idempotent completion transaction persists successfully.
func (m *UserManager) CompletePendingTutorials(ctx context.Context) (int, error) {
	if ctx == nil {
		return 0, errors.New("pending tutorial completion: nil context")
	}
	queue, isSupported := m.repository.(TutorialCompletionQueueRepository)
	if !isSupported {
		return 0, nil
	}
	loginNames, err := queue.PendingTutorialCompletionLoginNames(ctx)
	if err != nil {
		return 0, fmt.Errorf("pendingList: %w", err)
	}
	completedCount := 0
	completionErrors := make([]error, 0)
	for index, loginName := range loginNames {
		login := m.LoginTrusted(ctx, loginName)
		if login.IsAlreadyLoggedIn {
			continue
		}
		if !login.IsSuccess || login.User == nil {
			completionErrors = append(
				completionErrors, fmt.Errorf("pendingLogin[%d]: %w", index, ErrInvalidUser),
			)
			continue
		}
		_, completeErr := m.CompleteTutorial(ctx, login.User.Account.ID)
		logoutErr := m.Logout(ctx, login.User)
		if completeErr != nil || logoutErr != nil {
			completionErrors = append(completionErrors, fmt.Errorf(
				"pendingComplete[%d]: %w", index, errors.Join(completeErr, logoutErr),
			))
			continue
		}
		completedCount++
	}
	completionErr := errors.Join(completionErrors...)
	if completionErr != nil {
		return completedCount, fmt.Errorf("pendingBatch: %w", completionErr)
	}
	return completedCount, nil
}

func (u *User) creatureIDByTemplateName(templateName string) uint32 {
	for _, creature := range u.Creatures {
		if creature != nil && creature.TemplateName == templateName {
			return creature.ID
		}
	}
	return 0
}

// equipTutorialPart repairs the historical skipped-tutorial reward only while
// it remains unequipped. A player's later equipment choice always wins.
func (u *User) equipTutorialPart(part Part, creatureID uint32) bool {
	if creatureID == 0 {
		return false
	}
	for index := range u.Parts {
		if !u.Parts[index].hasGrantIdentity(part) {
			continue
		}
		if u.Parts[index].EquippedToCreatureID != 0 {
			return false
		}
		u.Parts[index].EquippedToCreatureID = creatureID
		return true
	}
	return false
}

func completeTutorialAccount(account *Account) bool {
	if account == nil {
		return false
	}
	previous := *account
	// Build 103 continues from combat completion at 3000 through mandatory ship
	// guides. The local server deliberately completes that onboarding chain so a
	// victorious player reaches an immediately usable ship instead of another
	// resumable soft-lock boundary.
	account.OnboardingProgress = max(account.OnboardingProgress, shipOnboardingCompletedProgress)
	account.NewPlayerInventory = max(account.NewPlayerInventory, uint32(1))
	account.CreatureRewards = 0
	account.XP = max(account.XP, tutorialCompletionExperience)
	account.Level = max(account.Level, tutorialCompletionLevel)
	account.DNA = max(account.DNA, tutorialCompletionDNA)
	return *account != previous
}

// completeTutorialStarterSquad ensures Blitz, Sage, and Wraith occupy the first
// PvE squad. First completion separately grants the live client's native
// post-tutorial Arsenal activation choice.
func (u *User) completeTutorialStarterSquad(
	blitzTemplate *TemplateCreature, sageTemplate *TemplateCreature, wraithTemplate *TemplateCreature,
) bool {
	if u == nil || blitzTemplate == nil || sageTemplate == nil || wraithTemplate == nil {
		return false
	}
	builder := tutorialStarterSquadBuilder{user: u, nextCreatureID: 1}
	for _, creature := range u.Creatures {
		if creature != nil && creature.ID >= builder.nextCreatureID {
			builder.nextCreatureID = creature.ID + 1
		}
	}
	blitzID := builder.ensureCreature(blitzTemplate)
	sageID := builder.ensureCreature(sageTemplate)
	wraithID := builder.ensureCreature(wraithTemplate)
	squadIndex := -1
	for index := range u.Squads {
		if u.Squads[index].Slot == 1 {
			squadIndex = index
			break
		}
	}
	if squadIndex < 0 {
		u.Squads = append(u.Squads, NewSquad(1))
		squadIndex = len(u.Squads) - 1
		builder.isChanged = true
	}
	squad := &u.Squads[squadIndex]
	thirdCreatureID := squad.CreatureIDs[2]
	if thirdCreatureID == blitzID || thirdCreatureID == sageID {
		thirdCreatureID = 0
	}
	if thirdCreatureID == 0 {
		thirdCreatureID = wraithID
	}
	if u.Account.CreatureRewards != 0 {
		u.Account.CreatureRewards = 0
		builder.isChanged = true
	}
	expectedCreatureIDs := [3]uint32{blitzID, sageID, thirdCreatureID}
	if squad.CreatureIDs != expectedCreatureIDs || squad.Category != "pve" || squad.IsLocked {
		squad.CreatureIDs = expectedCreatureIDs
		squad.Category = "pve"
		squad.IsLocked = false
		builder.isChanged = true
	}
	if u.Account.DefaultDeckPVEID != squad.ID {
		u.Account.DefaultDeckPVEID = squad.ID
		builder.isChanged = true
	}
	return builder.isChanged
}

type tutorialStarterSquadBuilder struct {
	user           *User
	nextCreatureID uint32
	isChanged      bool
}

func (e *tutorialStarterSquadBuilder) ensureCreature(
	template *TemplateCreature,
) uint32 {
	for _, creature := range e.user.Creatures {
		if creature != nil && creature.TemplateName == template.Name {
			return creature.ID
		}
	}
	creature := NewCreature(template)
	creature.ID = e.nextCreatureID
	e.nextCreatureID++
	e.user.Creatures = append(e.user.Creatures, creature)
	e.isChanged = true
	return creature.ID
}

func (u *User) restoreTutorialCompletion(previous UserRecord) {
	u.mu.Lock()
	u.IsTutorialCompletionPending = previous.IsTutorialCompletionPending
	u.Account = previous.Account
	u.Squads = append(u.Squads[:0], previous.Squads...)
	u.Creatures = cloneCreatures(previous.Creatures)
	u.Parts = append(u.Parts[:0], previous.Parts...)
	u.mu.Unlock()
}

func (u *User) setSettings(settings map[string]string) (map[string]string, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	previous := make(map[string]string, len(u.Settings))
	for key, value := range u.Settings {
		previous[key] = value
	}
	if u.Settings == nil {
		u.Settings = make(map[string]string)
	}
	isChanged := false
	for key, value := range settings {
		current, isFound := u.Settings[key]
		if key == "" || isFound && current == value {
			continue
		}
		u.Settings[key] = value
		isChanged = true
	}
	return previous, isChanged
}

func (u *User) restoreSettings(previous map[string]string) {
	u.mu.Lock()
	u.Settings = previous
	u.mu.Unlock()
}

// SettingsSnapshot returns a transport-safe copy of persistent settings.
func (u *User) SettingsSnapshot() map[string]string {
	u.mu.RLock()
	settings := make(map[string]string, len(u.Settings))
	for key, value := range u.Settings {
		settings[key] = value
	}
	u.mu.RUnlock()
	return settings
}

func (u *User) restoreAccount(previous Account) {
	u.mu.Lock()
	u.Account = previous
	u.mu.Unlock()
}

// unlockCreature enforces the aggregate invariant without knowing persistence
// or transport details.
func (u *User) unlockCreature(template *TemplateCreature) (uint32, bool, error) {
	if template == nil {
		return 0, false, ErrCreatureTemplateNotFound
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, creature := range u.Creatures {
		if creature != nil && creature.TemplateName == template.Name {
			return creature.ID, false, nil
		}
	}
	if u.Account.CreatureRewards == 0 {
		return 0, false, ErrCreatureRewardRequired
	}
	u.Account.CreatureRewards--
	creature := NewCreature(template)
	if len(u.Creatures) == 0 {
		creature.ID = 1
	} else {
		creature.ID = u.Creatures[len(u.Creatures)-1].ID + 1
	}
	u.Creatures = append(u.Creatures, creature)
	return creature.ID, true, nil
}

func (u *User) rollbackCreatureUnlock(id uint32) {
	u.mu.Lock()
	defer u.mu.Unlock()
	last := len(u.Creatures) - 1
	if last < 0 || u.Creatures[last].ID != id {
		return
	}
	u.Creatures = u.Creatures[:last]
	u.Account.CreatureRewards++
}

// AddPart appends an inventory item.
func (u *User) AddPart(part Part) {
	u.mu.Lock()
	u.Parts = append(u.Parts, part)
	u.mu.Unlock()
}

// GrantPartOnce durably appends an authored one-time inventory reward. An
// existing part with the same gameplay identity makes the command idempotent.
func (m *UserManager) GrantPartOnce(ctx context.Context, userID int64, part Part) (bool, error) {
	user := m.UserByID(userID)
	if user == nil {
		return false, ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	isGranted, err := user.grantPartOnce(userID, part)
	if err != nil {
		user.mu.Unlock()
		return false, fmt.Errorf("partGrant: %w", err)
	}
	var grantedPart Part
	if isGranted {
		grantedPart = user.Parts[len(user.Parts)-1]
	}
	user.mu.Unlock()
	if !isGranted {
		return false, nil
	}
	err = m.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		last := len(user.Parts) - 1
		if last >= 0 && user.Parts[last] == grantedPart {
			user.Parts = user.Parts[:last]
		}
		user.mu.Unlock()
		return false, fmt.Errorf("partSave: %w", err)
	}
	return true, nil
}

// grantPartOnce appends one authored reward while the caller holds u.mu.
func (u *User) grantPartOnce(userID int64, part Part) (bool, error) {
	part.Normalize()
	for _, existing := range u.Parts {
		if existing.hasGrantIdentity(part) {
			return false, nil
		}
	}
	part.ID = 1
	for _, existing := range u.Parts {
		if existing.ID >= part.ID {
			if existing.ID >= uint64(^uint32(0)) {
				return false, ErrPartExists
			}
			part.ID = existing.ID + 1
		}
	}
	if userID <= 0 || uint64(userID) > uint64(^uint32(0)>>1) || part.ID > uint64(^uint32(0)) {
		return false, ErrPartExists
	}
	part.ReferenceID = uint64(userID)<<32 | part.ID
	if part.CreationDate == 0 {
		part.CreationDate = uint64(time.Now().Unix())
	}
	u.Parts = append(u.Parts, part)
	return true, nil
}

// GrantPart durably appends one explicitly identified inventory item. Unlike
// GrantPartOnce, distinct item IDs may carry the same gameplay definition.
func (m *UserManager) GrantPart(ctx context.Context, userID int64, part Part) (Part, error) {
	user := m.UserByID(userID)
	if user == nil {
		return Part{}, ErrInvalidUser
	}
	part.Normalize()
	if part.CreationDate == 0 {
		part.CreationDate = uint64(time.Now().Unix())
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	if part.ID == 0 {
		part.ID = 1
		for _, existing := range user.Parts {
			if existing.ID >= part.ID {
				part.ID = existing.ID + 1
			}
		}
	}
	if part.ReferenceID == 0 {
		if userID <= 0 || uint64(userID) > uint64(^uint32(0)>>1) || part.ID > uint64(^uint32(0)) {
			user.mu.Unlock()
			return Part{}, ErrPartExists
		}
		part.ReferenceID = uint64(userID)<<32 | part.ID
	}
	for _, existing := range user.Parts {
		if existing.ID == part.ID {
			user.mu.Unlock()
			return Part{}, ErrPartExists
		}
	}
	user.Parts = append(user.Parts, part)
	user.mu.Unlock()
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		last := len(user.Parts) - 1
		if last >= 0 && user.Parts[last] == part {
			user.Parts = user.Parts[:last]
		}
		user.mu.Unlock()
		return Part{}, fmt.Errorf("partSave: %w", err)
	}
	return part, nil
}

// GrantPartWithinCapacity durably appends one generated pickup only when the
// account has an available inventory slot. Capacity admission and persistence
// share the user's mutation lock so concurrent pickups cannot overbook it.
func (m *UserManager) GrantPartWithinCapacity(ctx context.Context, userID int64, part Part) (Part, error) {
	user := m.UserByID(userID)
	if user == nil {
		return Part{}, ErrInvalidUser
	}
	part.Normalize()
	if part.CreationDate == 0 {
		part.CreationDate = uint64(time.Now().Unix())
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	capacity := user.Account.UnlockInventoryIdentify
	ownedCount := uint32(0)
	for index := range user.Parts {
		if user.Parts[index].MarketStatus == PartMarketOwned {
			ownedCount++
		}
	}
	if capacity == 0 || ownedCount >= capacity {
		user.mu.Unlock()
		return Part{}, ErrInventoryFull
	}
	if part.ID == 0 {
		part.ID = 1
		for _, existing := range user.Parts {
			if existing.ID >= part.ID {
				part.ID = existing.ID + 1
			}
		}
	}
	if part.ReferenceID == 0 {
		if userID <= 0 || uint64(userID) > uint64(^uint32(0)>>1) || part.ID > uint64(^uint32(0)) {
			user.mu.Unlock()
			return Part{}, ErrPartExists
		}
		part.ReferenceID = uint64(userID)<<32 | part.ID
	}
	for _, existing := range user.Parts {
		if existing.ID == part.ID {
			user.mu.Unlock()
			return Part{}, ErrPartExists
		}
	}
	user.Parts = append(user.Parts, part)
	user.mu.Unlock()
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		last := len(user.Parts) - 1
		if last >= 0 && user.Parts[last] == part {
			user.Parts = user.Parts[:last]
		}
		user.mu.Unlock()
		return Part{}, fmt.Errorf("partSave: %w", err)
	}
	return part, nil
}

// DropPart durably discards one unequipped inventory item owned by the user.
// A missing item is idempotent so duplicate client commands are harmless.
func (m *UserManager) DropPart(ctx context.Context, userID int64, itemID uint64) (bool, error) {
	user := m.UserByID(userID)
	if user == nil {
		return false, ErrInvalidUser
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	partIndex := -1
	for index, part := range user.Parts {
		if part.ID != itemID {
			continue
		}
		partIndex = index
		if part.EquippedToCreatureID != 0 {
			user.mu.Unlock()
			return false, ErrPartEquipped
		}
		break
	}
	if partIndex < 0 {
		user.mu.Unlock()
		return false, nil
	}
	previousPart := append([]Part(nil), user.Parts...)
	user.Parts = append(user.Parts[:partIndex], user.Parts[partIndex+1:]...)
	user.mu.Unlock()
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.mu.Lock()
		user.Parts = previousPart
		user.mu.Unlock()
		return false, fmt.Errorf("partDropSave: %w", err)
	}
	return true, nil
}

// SetAccountLevel durably assigns the developer-selected account/Crogenitor
// level. It keeps XP coherent and makes the maximum level expose every loaded
// player template for complete-catalog testing.
func (m *UserManager) SetAccountLevel(ctx context.Context, userID int64, level uint32) error {
	user := m.UserByID(userID)
	if user == nil {
		return ErrInvalidUser
	}
	maximumCreatureCount := 0
	if m.template != nil {
		maximumCreatureCount = len(m.template.List())
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	user.mu.Lock()
	minimumExperience, isMinimumKnown := minimumExperienceForLevel(level)
	previous := user.Account
	user.Account.Level = level
	if level > previous.Level {
		user.Account.CreatureRewards += heroRewardEntitlementCount(level) -
			heroRewardEntitlementCount(previous.Level)
	}
	if isMinimumKnown {
		user.Account.XP = minimumExperience
	}
	if level == uint32(len(accountExperienceUpperBound)+1) && maximumCreatureCount > len(user.Creatures) {
		maximumCreatureReward := uint32(maximumCreatureCount - len(user.Creatures))
		user.Account.CreatureRewards = max(user.Account.CreatureRewards, maximumCreatureReward)
	}
	isChanged := user.Account != previous
	user.mu.Unlock()
	if !isChanged {
		return nil
	}
	eventAppend := user.appendEvents(levelMilestoneEvents(previous.Level, level)...)
	previousEvents := eventAppend.previousEvents
	err := m.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreAccount(previous)
		user.restoreEvents(previousEvents)
		return fmt.Errorf("levelSave: %w", err)
	}
	return nil
}

func heroRewardEntitlementCount(level uint32) uint32 {
	milestones := [...]uint32{
		4, 5, 8, 11, 14, 17, 18, 20, 22, 23, 25, 27, 28, 30, 32, 33,
		35, 37, 38, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50,
	}
	count := uint32(0)
	for _, milestone := range milestones {
		if milestone > level {
			break
		}
		count++
	}
	return count
}

func minimumExperienceForLevel(level uint32) (uint32, bool) {
	if level == 1 {
		return 0, true
	}
	if level < 1 || level > uint32(len(accountExperienceUpperBound))+1 {
		return 0, false
	}
	return accountExperienceUpperBound[level-2] + 1, true
}

// accountExperienceUpperBound is build 103's complete client-loaded tuning
// vector (property 0xC0B32F0F). The client advances while XP is strictly
// greater than the preceding entry, so level N starts at bound[N-2] + 1.
var accountExperienceUpperBound = [...]uint32{
	100, 200, 3000, 6000, 9000, 12000, 15000, 18000, 21000, 24500,
	28500, 33000, 38000, 43000, 48000, 53000, 58000, 63000, 68000, 73000,
	78000, 83000, 88000, 93500, 99500, 106000, 113000, 120500, 128500, 137000,
	146000, 155500, 165500, 176000, 187000, 198500, 210500, 223000, 236000, 249500,
	263500, 278000, 293000, 308500, 324500, 341000, 358000, 375500, 393500, 412000,
	431000, 451000, 471500, 492500, 514000, 536000, 558500, 581500, 605000, 629000,
	653500, 678500, 704500, 731500, 759500, 788500, 818500, 849500, 881500, 914500,
	948500, 983500, 1019500, 1056500, 1094500, 1133500, 1173500, 1214500, 1256500, 1299500,
	1343500, 1388500, 1434500, 1481500, 1529500, 1578500, 1628500, 1679500, 1731500, 1784500,
	1839500, 1896500, 1955500, 2016500, 2080500, 2149500, 2225500, 2310500, 2410500,
}

var upgradeCosts = [...]uint32{
	0,
	2000, 6000, 16000, 30000, 80000, 150000,
	300000,
	200, 350, 500, 700, 900, 1200, 1600, 4000, 8000, 13000, 19000, 29000, 42000, 57000, 76000, 100000, 160000, 250000,
	5000, 25000, 50000, 100000, 200000, 400000, 1000000, 2500000, 5000000, 10000000,
	500, 4000,
	0,
	0, 0, 0,
	400, 1200, 4000,
	0,
	1000, 5000, 10000,
	200, 600, 1200, 20000000,
}

// unlockUpgrade applies the legacy upgrade-cost table inside the aggregate.
func (u *User) unlockUpgrade(unlockID uint32) (Account, error) {
	if unlockID >= uint32(len(upgradeCosts)) {
		return Account{}, ErrUpgradeInvalid
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	inventoryTier, isInventoryUpgrade := inventoryUpgradeTier(unlockID)
	if isInventoryUpgrade && inventoryTier <= u.Account.UnlockInventory {
		return Account{}, ErrUpgradeInvalid
	}
	previous := u.Account
	cost := upgradeCosts[unlockID]
	if cost > u.Account.DNA {
		return Account{}, ErrUpgradeFunds
	}
	u.Account.DNA -= cost
	switch {
	case unlockID >= 1 && unlockID <= 6:
		u.Account.UnlockCatalysts = unlockID + 3
	case unlockID == 7:
		u.Account.UnlockDiagonalCatalysts = 1
	case unlockID >= 8 && unlockID <= 25:
		u.Account.UnlockStats = unlockID - 7
	case unlockID >= 26 && unlockID <= 35:
		u.Account.UnlockInventory = unlockID - 22
		u.Account.UnlockInventoryIdentify = 180 + 30*u.Account.UnlockInventory
	case unlockID >= 36 && unlockID <= 37:
		u.Account.UnlockPVEDecks = unlockID - 34
	case unlockID == 38:
		u.Account.UnlockPVPDecks = 1
	case unlockID >= 42 && unlockID <= 44:
		u.Account.UnlockFuelTanks = max(
			u.Account.UnlockFuelTanks, unlockedFuelTankCapacity,
		)
	case unlockID >= 46 && unlockID <= 48:
		u.Account.UnlockEditorFlairSlots = unlockID - 42
	case unlockID >= 49 && unlockID <= 51:
		u.Account.UnlockInventory = unlockID - 48
		u.Account.UnlockInventoryIdentify = 180 + 30*u.Account.UnlockInventory
	case unlockID == 52:
		u.Account.UnlockInventory = 14
		u.Account.UnlockInventoryIdentify = 180 + 30*u.Account.UnlockInventory
	}
	return previous, nil
}

func inventoryUpgradeTier(unlockID uint32) (uint32, bool) {
	switch {
	case unlockID >= 49 && unlockID <= 51:
		return unlockID - 48, true
	case unlockID >= 26 && unlockID <= 35:
		return unlockID - 22, true
	case unlockID == 52:
		return 14, true
	default:
		return 0, false
	}
}
