//go:build scenario

package sporenet

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ScenarioProfileRequest configures development prerequisites for a fresh
// occurrence-2 run. HeroNouns are ordered authoritative template identities,
// not persistent creature IDs. Labels identify the supplied setup, not play.
type ScenarioProfileRequest struct {
	LoginName     string
	Password      string
	ProgressionID string
	SquadID       string
	HeroNouns     [3]uint32
}

// ScenarioProfile exposes only the effective setup needed to correlate the
// ordinary campaign binding. It contains no credentials or account aggregate.
type ScenarioProfile struct {
	UserID             int64
	ProgressionID      string
	SquadID            string
	ActualSquadID      uint32
	CreatureIDs        [3]uint32
	HeroNouns          [3]uint32
	ChainProgression   uint32
	OnboardingProgress uint32
	AccountLevel       uint32
	AccountXP          uint32
}

// CreateScenarioProfile persists one fresh, inactive profile in one repository
// transaction. Completed onboarding and the first campaign stage are supplied
// development setup: this operation does not play either mission, activate a
// session, admit a game, or claim client readiness.
func (e *UserManager) CreateScenarioProfile(
	ctx context.Context, req ScenarioProfileRequest,
) (ScenarioProfile, error) {
	err := contextError(ctx)
	if err != nil {
		return ScenarioProfile{}, fmt.Errorf("profileContext: %w", err)
	}
	if e == nil || e.repository == nil || e.template == nil {
		return ScenarioProfile{}, errors.New("scenario profile dependencies unavailable")
	}
	err = validateScenarioProfile(req)
	if err != nil {
		return ScenarioProfile{}, fmt.Errorf("profileRequest: %w", err)
	}
	templates := [3]*TemplateCreature{}
	for index, noun := range req.HeroNouns {
		template := e.template.ByNoun(noun)
		if template == nil || template.Noun != noun || template.Name == "" {
			return ScenarioProfile{}, fmt.Errorf("heroTemplate[%d]: %w", index, ErrCreatureTemplateNotFound)
		}
		templates[index] = template
	}
	session := e.sessionLock(req.LoginName)
	session.Lock()
	defer session.Unlock()
	e.mu.RLock()
	activeUser, isActive := e.activeUsers[loginKey(req.LoginName)]
	e.mu.RUnlock()
	if isActive || activeUser != nil {
		return ScenarioProfile{}, fmt.Errorf("profileActive: %w", ErrUserExists)
	}
	record, err := e.repository.LoadByLoginName(ctx, req.LoginName)
	if err == nil {
		if record.LoginName == "" {
			return ScenarioProfile{}, errors.New("scenario profile lookup returned an invalid identity")
		}
		return ScenarioProfile{}, fmt.Errorf("profileExists: %w", ErrUserExists)
	}
	if !errors.Is(err, ErrUserNotFound) {
		return ScenarioProfile{}, fmt.Errorf("profileLookup: %w", err)
	}
	storedPassword, err := passwordForStorage(req.Password)
	if err != nil {
		return ScenarioProfile{}, fmt.Errorf("profilePassword: %w", err)
	}
	user := NewUser("Scenario", req.LoginName, storedPassword)
	// Reuse the normal completed-onboarding account defaults without the
	// tutorial's fixed starter roster, reward part, login, or extra writes.
	isAccountChanged := completeTutorialAccount(&user.Account)
	if !isAccountChanged {
		return ScenarioProfile{}, errors.New("fresh scenario onboarding defaults were already complete")
	}
	// ChainLevelForProgression consumes the highest completed one-based stage:
	// one completed stage selects occurrence 2 while leaving it first-run.
	user.Account.ChainProgression = 1
	creatureIDs := [3]uint32{1, 2, 3}
	for index, template := range templates {
		creature := NewCreature(template)
		creature.ID = creatureIDs[index]
		user.Creatures = append(user.Creatures, creature)
	}
	user.Squads[0].Category = "pve"
	user.Squads[0].CreatureIDs = creatureIDs
	user.Account.DefaultDeckPVEID = user.Squads[0].ID
	userID, err := e.repository.Create(ctx, user.Record())
	if err != nil {
		return ScenarioProfile{}, fmt.Errorf("profileCreate: %w", err)
	}
	return ScenarioProfile{
		UserID: userID, ProgressionID: req.ProgressionID, SquadID: req.SquadID,
		ActualSquadID: user.Squads[0].ID, CreatureIDs: creatureIDs, HeroNouns: req.HeroNouns,
		ChainProgression:   user.Account.ChainProgression,
		OnboardingProgress: user.Account.OnboardingProgress,
		AccountLevel:       user.Account.Level, AccountXP: user.Account.XP,
	}, nil
}

func validateScenarioProfile(req ScenarioProfileRequest) error {
	if req.LoginName == "" || req.LoginName != strings.TrimSpace(req.LoginName) || len(req.LoginName) > 320 {
		return errors.New("scenario login identity is invalid")
	}
	for _, character := range req.LoginName {
		if character <= ' ' || character > '~' {
			return errors.New("scenario login identity contains unsupported characters")
		}
	}
	if req.Password == "" || len(req.Password) > 72 {
		return errors.New("scenario password must contain between 1 and 72 bytes")
	}
	labels := []string{req.ProgressionID, req.SquadID}
	for _, label := range labels {
		if label == "" || len(label) > 80 || label == "." || label == ".." {
			return errors.New("scenario setup label is invalid")
		}
		for _, character := range label {
			isLetter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
			isDigit := character >= '0' && character <= '9'
			if !isLetter && !isDigit && character != '_' && character != '-' && character != '.' {
				return errors.New("scenario setup label contains unsupported characters")
			}
		}
	}
	for index, noun := range req.HeroNouns {
		if noun == 0 {
			return errors.New("scenario hero noun is zero")
		}
		for previousIndex := 0; previousIndex < index; previousIndex++ {
			if req.HeroNouns[previousIndex] == noun {
				return errors.New("scenario hero nouns must be distinct")
			}
		}
	}
	return nil
}
