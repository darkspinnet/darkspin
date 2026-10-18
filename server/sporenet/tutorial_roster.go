package sporenet

import (
	"context"
	"fmt"
)

// prepareTutorialRoster supplies the tutorial heroes before authentication
// populates the native owned-creature cache. Build 103's getAccount response
// (4B0370) refreshes account/decks but does not parse the creatures collection;
// granting starters only at Return to Ship leaves that cache empty until login.
// Tutorial completion still owns XP, onboarding progress and activation credit.
func (e *UserManager) prepareTutorialRoster(ctx context.Context, user *User) error {
	if user == nil {
		return fmt.Errorf("starterUser: %w", ErrInvalidUser)
	}
	if e.template == nil {
		// Content-free managers cannot materialize authored hero templates.
		return nil
	}
	blitzTemplate := e.template.ByName(tutorialBlitzTemplateName)
	sageTemplate := e.template.ByName(tutorialSageTemplateName)
	if blitzTemplate == nil || sageTemplate == nil {
		return fmt.Errorf("starterTemplate: %w", ErrCreatureTemplateNotFound)
	}
	user.mutation.Lock()
	defer user.mutation.Unlock()
	previous := user.Record()
	user.mu.Lock()
	isChanged := user.completeTutorialStarterSquad(blitzTemplate, sageTemplate)
	user.mu.Unlock()
	if !isChanged {
		return nil
	}
	err := e.repository.Save(ctx, user.Record())
	if err != nil {
		user.restoreTutorialCompletion(previous)
		return fmt.Errorf("starterSave: %w", err)
	}
	return nil
}
