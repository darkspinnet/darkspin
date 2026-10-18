//go:build scenario

package sporenet

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type ScenarioContinueMemberRequest struct {
	LoginName string
	UserID    int64
	SquadID   uint32
	HeroNouns [3]uint32
}

// ScenarioContinueMember loads an inactive detached profile. It neither logs
// in nor issues credentials, publishes active membership or writes storage.
func (e *UserManager) ScenarioContinueMember(
	ctx context.Context, req ScenarioContinueMemberRequest,
) (*User, error) {
	err := contextError(ctx)
	if err != nil {
		return nil, fmt.Errorf("memberContext: %w", err)
	}
	if e == nil || e.repository == nil || e.template == nil {
		return nil, errors.New("scenario Continue profile dependencies unavailable")
	}
	if req.UserID <= 0 || req.SquadID == 0 || req.LoginName == "" ||
		len(req.LoginName) > 320 || req.LoginName != strings.TrimSpace(req.LoginName) {
		return nil, errors.New("scenario Continue profile identity invalid")
	}
	for _, character := range req.LoginName {
		if character <= ' ' || character > '~' {
			return nil, errors.New("scenario Continue login identity invalid")
		}
	}
	session := e.sessionLock(req.LoginName)
	session.Lock()
	defer session.Unlock()
	err = ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("memberLockContext: %w", err)
	}
	e.mu.RLock()
	active := e.activeUsers[loginKey(req.LoginName)]
	activeByID := e.activeUsersByID[req.UserID]
	e.mu.RUnlock()
	if active != nil || activeByID != nil {
		return nil, errors.New("scenario Continue profile already active")
	}
	record, err := e.repository.LoadByLoginName(ctx, req.LoginName)
	if err != nil {
		return nil, fmt.Errorf("memberLoad: %w", err)
	}
	if record.LoginName != req.LoginName || record.Account.ID != req.UserID {
		return nil, errors.New("scenario Continue stored identity mismatch")
	}
	member := NewUserFromRecord(record, e.template)
	err = validateScenarioContinueMember(member, req)
	if err != nil {
		return nil, fmt.Errorf("memberProfile: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("memberReturnContext: %w", err)
	}
	return member, nil
}

func validateScenarioContinueMember(member *User, req ScenarioContinueMemberRequest) error {
	view := member.View()
	if view.AuthToken != "" || member.CurrentGameID() != 0 || member.CurrentPlaygroupID() != 0 ||
		view.Account.ChainProgression != 1 || !view.Account.IsTutorialCompleted() ||
		view.Account.DefaultDeckPVEID != req.SquadID {
		return errors.New("scenario Continue requires inactive fresh solo campaign profile")
	}
	var squad Squad
	squadCount := 0
	for _, candidate := range view.Squads {
		if candidate.ID == req.SquadID {
			squad = candidate
			squadCount++
		}
	}
	if squadCount != 1 || squad.Category != "pve" || squad.IsLockedFor(view.Account) {
		return errors.New("scenario Continue squad unavailable")
	}
	for index, creatureID := range squad.CreatureIDs {
		if creatureID == 0 || req.HeroNouns[index] == 0 {
			return errors.New("scenario Continue hero identity unavailable")
		}
		for previous := 0; previous < index; previous++ {
			if req.HeroNouns[previous] == req.HeroNouns[index] {
				return errors.New("scenario Continue hero nouns must be distinct")
			}
		}
		creatureCount := 0
		for _, creature := range view.Creatures {
			if creature == nil || creature.ID != creatureID {
				continue
			}
			if creature.Template == nil || creature.Template.Noun != req.HeroNouns[index] {
				return fmt.Errorf("scenario Continue hero[%d] mismatch", index)
			}
			creatureCount++
		}
		if creatureCount != 1 {
			return fmt.Errorf("scenario Continue hero[%d] unavailable or ambiguous", index)
		}
	}
	return nil
}
