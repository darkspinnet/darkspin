//go:build scenario

package sporenet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// DebugProfileRequest supplies setup for a new run-owned profile. No destination
// login or user ID is accepted: reset means creating another fresh profile.
// Source cloning copies only selected template nouns and campaign progression.
type DebugProfileRequest struct {
	RunID                string
	ClientName           string
	Password             string
	HeroNouns            [3]uint32
	ChainProgression     uint32
	SourceLoginName      string
	IsSourceCloneAllowed bool
	IsPVPEnabled         bool
}

// DebugProfile contains effective supplied setup, never credentials or an
// account aggregate. Creating it does not log in, form a party, or enter a game.
type DebugProfile struct {
	RunID              string
	ClientName         string
	LoginName          string
	UserID             int64
	ActualSquadID      uint32
	ActualPVPSquadID   uint32
	CreatureIDs        [3]uint32
	PVPCreatureIDs     [3]uint32
	HeroNouns          [3]uint32
	ChainProgression   uint32
	OnboardingProgress uint32
	AccountLevel       uint32
	AccountXP          uint32
	IsSourceDerived    bool
	IsPVPEnabled       bool
}

func (e *UserManager) CreateDebugProfile(ctx context.Context, req DebugProfileRequest) (DebugProfile, error) {
	err := contextError(ctx)
	if err != nil {
		return DebugProfile{}, fmt.Errorf("debugContext: %w", err)
	}
	if e == nil || e.repository == nil || e.template == nil {
		return DebugProfile{}, errors.New("debug profile dependencies unavailable")
	}
	err = validateDebugProfileOwner(req.RunID, req.ClientName)
	if err != nil {
		return DebugProfile{}, fmt.Errorf("debugOwner: %w", err)
	}
	if req.SourceLoginName != "" {
		if !req.IsSourceCloneAllowed {
			return DebugProfile{}, errors.New("existing-profile derivation requires explicit opt-in")
		}
		if req.HeroNouns != [3]uint32{} || req.ChainProgression != 0 {
			return DebugProfile{}, errors.New("source derivation cannot be combined with explicit hero or progression setup")
		}
		req.HeroNouns, req.ChainProgression, err = e.debugProfileSource(ctx, req.SourceLoginName)
		if err != nil {
			return DebugProfile{}, fmt.Errorf("debugSource: %w", err)
		}
	} else if req.IsSourceCloneAllowed {
		return DebugProfile{}, errors.New("source clone opt-in requires a source login")
	}
	nonce := make([]byte, 16)
	count, err := rand.Read(nonce)
	if err != nil {
		return DebugProfile{}, fmt.Errorf("debugNonce: %w", err)
	}
	if count != len(nonce) {
		return DebugProfile{}, errors.New("debug profile identity random source returned a short read")
	}
	loginName := req.RunID + "." + req.ClientName + "." + hex.EncodeToString(nonce) + "@scenario.invalid"
	profileReq := ScenarioProfileRequest{LoginName: loginName, Password: req.Password,
		ProgressionID: req.RunID, SquadID: req.ClientName, HeroNouns: req.HeroNouns}
	err = validateScenarioProfile(profileReq)
	if err != nil {
		return DebugProfile{}, fmt.Errorf("debugSetup: %w", err)
	}
	templates := [3]*TemplateCreature{}
	for index, noun := range req.HeroNouns {
		template := e.template.ByNoun(noun)
		if template == nil || template.Noun != noun || template.Name == "" {
			return DebugProfile{}, fmt.Errorf("debugTemplate[%d]: %w", index, ErrCreatureTemplateNotFound)
		}
		templates[index] = template
	}
	storedPassword, err := passwordForStorage(req.Password)
	if err != nil {
		return DebugProfile{}, fmt.Errorf("debugPassword: %w", err)
	}
	session := e.sessionLock(loginName)
	session.Lock()
	defer session.Unlock()
	e.mu.RLock()
	activeUser := e.activeUsers[loginKey(loginName)]
	e.mu.RUnlock()
	if activeUser != nil {
		return DebugProfile{}, fmt.Errorf("debugActive: %w", ErrUserExists)
	}
	storedRecord, lookupErr := e.repository.LoadByLoginName(ctx, loginName)
	if lookupErr == nil {
		return DebugProfile{}, fmt.Errorf("debugExists[%s]: %w", storedRecord.LoginName, ErrUserExists)
	}
	if !errors.Is(lookupErr, ErrUserNotFound) {
		return DebugProfile{}, fmt.Errorf("debugLookup: %w", lookupErr)
	}
	user := NewUser("Scenario "+req.ClientName, loginName, storedPassword)
	isAccountChanged := completeTutorialAccount(&user.Account)
	if !isAccountChanged {
		return DebugProfile{}, errors.New("new debug profile onboarding was already complete")
	}
	user.Account.ChainProgression = req.ChainProgression
	creatureIDs := [3]uint32{1, 2, 3}
	for index, template := range templates {
		creature := NewCreature(template)
		creature.ID = creatureIDs[index]
		user.Creatures = append(user.Creatures, creature)
	}
	// Ordinary login supplies missing tutorial starters and then rewrites the
	// first squad. Seed those inventory prerequisites now so a completed debug
	// profile retains its explicitly requested squad on its first real login.
	starterNames := []string{tutorialBlitzTemplateName, tutorialSageTemplateName}
	for _, starterName := range starterNames {
		starterTemplate := e.template.ByName(starterName)
		if starterTemplate == nil || starterTemplate.Name != starterName || starterTemplate.Noun == 0 {
			return DebugProfile{}, fmt.Errorf("debugStarter[%s]: %w", starterName, ErrCreatureTemplateNotFound)
		}
		isStarterPresent := false
		for _, creature := range user.Creatures {
			if creature.TemplateName == starterName {
				isStarterPresent = true
				break
			}
		}
		if isStarterPresent {
			continue
		}
		starter := NewCreature(starterTemplate)
		starter.ID = uint32(len(user.Creatures) + 1)
		user.Creatures = append(user.Creatures, starter)
	}
	user.Squads[0].Category = "pve"
	user.Squads[0].CreatureIDs = creatureIDs
	user.Account.DefaultDeckPVEID = user.Squads[0].ID
	pvpSquadID := uint32(0)
	pvpCreatureIDs := [3]uint32{}
	if req.IsPVPEnabled {
		pvpSquadID, pvpCreatureIDs = seedDebugPVPProfile(user, templates)
	}
	err = ctx.Err()
	if err != nil {
		return DebugProfile{}, fmt.Errorf("debugCreateContext: %w", err)
	}
	userID, err := e.repository.Create(ctx, user.Record())
	if err != nil {
		return DebugProfile{}, fmt.Errorf("debugCreate: %w", err)
	}
	return DebugProfile{RunID: req.RunID, ClientName: req.ClientName, LoginName: loginName,
		UserID: userID, ActualSquadID: user.Squads[0].ID, CreatureIDs: creatureIDs,
		ActualPVPSquadID: pvpSquadID, PVPCreatureIDs: pvpCreatureIDs,
		HeroNouns: req.HeroNouns, ChainProgression: user.Account.ChainProgression,
		OnboardingProgress: user.Account.OnboardingProgress, AccountLevel: user.Account.Level,
		AccountXP: user.Account.XP, IsSourceDerived: req.SourceLoginName != "", IsPVPEnabled: req.IsPVPEnabled}, nil
}

// Arena membership must use separate creature records: ordinary login removes
// creature IDs shared between campaign and Arena squads. Fresh owned copies of
// the requested templates preserve that invariant and the selected PVE squad.
func seedDebugPVPProfile(user *User, templates [3]*TemplateCreature) (uint32, [3]uint32) {
	creatureIDs := [3]uint32{}
	for index, template := range templates {
		creature := NewCreature(template)
		creature.ID = uint32(len(user.Creatures) + 1)
		creatureIDs[index] = creature.ID
		user.Creatures = append(user.Creatures, creature)
	}
	// NewUser owns three default slots. Claim its unused second slot just as
	// the production Arena deck operation does, keeping the first PVE slot.
	squad := &user.Squads[1]
	squad.Category = "pvp"
	squad.Slot = 1
	squad.IsLocked = false
	squad.CreatureIDs = creatureIDs
	user.Account.UnlockPVPDecks = max(uint32(1), user.Account.UnlockPVPDecks)
	user.Account.DefaultDeckPVPID = squad.ID
	return squad.ID, creatureIDs
}

func validateDebugProfileOwner(runID, clientName string) error {
	labels := []string{runID, clientName}
	for _, label := range labels {
		if label == "" || len(label) > 80 || label == "." || label == ".." {
			return errors.New("debug run and client identities must contain 1 to 80 characters")
		}
		for _, character := range label {
			isLetter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
			isDigit := character >= '0' && character <= '9'
			if !isLetter && !isDigit && character != '-' && character != '_' {
				return errors.New("debug run and client identities contain unsupported characters")
			}
		}
	}
	return nil
}

// Read a durable source snapshot only. This deliberately avoids inheriting
// passwords, rewards, inventory, transient membership, or future account fields.
func (e *UserManager) debugProfileSource(ctx context.Context, loginName string) ([3]uint32, uint32, error) {
	if loginName == "" || loginName != strings.TrimSpace(loginName) || len(loginName) > 320 {
		return [3]uint32{}, 0, errors.New("source login identity is invalid")
	}
	record, err := e.repository.LoadByLoginName(ctx, loginName)
	if err != nil {
		return [3]uint32{}, 0, fmt.Errorf("sourceRead: %w", err)
	}
	if !strings.EqualFold(record.LoginName, loginName) || record.Account.ID <= 0 {
		return [3]uint32{}, 0, errors.New("source profile identity mismatch")
	}
	var selectedSquad Squad
	squadCount := 0
	for _, squad := range record.Squads {
		if squad.ID == record.Account.DefaultDeckPVEID {
			selectedSquad = squad
			squadCount++
		}
	}
	if squadCount != 1 || selectedSquad.Category != "pve" || selectedSquad.IsLockedFor(record.Account) {
		return [3]uint32{}, 0, errors.New("source profile has no unambiguous usable PVE squad")
	}
	heroNouns := [3]uint32{}
	for index, creatureID := range selectedSquad.CreatureIDs {
		creatureCount := 0
		for _, creature := range record.Creatures {
			if creature == nil || creature.ID != creatureID || creatureID == 0 {
				continue
			}
			template := e.template.ByName(creature.TemplateName)
			if template == nil || template.Noun == 0 {
				return [3]uint32{}, 0, fmt.Errorf("sourceHero[%d]: %w", index, ErrCreatureTemplateNotFound)
			}
			heroNouns[index] = template.Noun
			creatureCount++
		}
		if creatureCount != 1 {
			return [3]uint32{}, 0, errors.New("source squad has an empty or ambiguous hero slot")
		}
	}
	return heroNouns, record.Account.ChainProgression, nil
}
