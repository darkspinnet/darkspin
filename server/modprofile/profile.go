// Package modprofile owns offline, launcher-managed creature bindings.
// Its repository must be dedicated to modded profiles and have no live sessions.
package modprofile

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

const bindingPrefix = "mod.binding."

type Binding struct {
	HeroNoun   uint32
	CreationID string
}

type SaveRequest struct {
	LoginName         string
	AvatarID          uint32
	IsTutorialSkipped bool
	IsNew             bool
	Bindings          []Binding
}

// Save validates the whole replacement before one atomic repository write.
// Existing profiles retain every normal account field and progression choice.
func Save(ctx context.Context, repository sporenet.UserRepository, req SaveRequest) error {
	if len(req.Bindings) > 128 {
		return errors.New("a Mod profile supports at most 128 bindings")
	}
	if req.LoginName == "" {
		return errors.New("profile name is required")
	}
	nouns := make(map[uint32]bool)
	for _, binding := range req.Bindings {
		if binding.HeroNoun == 0 || binding.CreationID == "" || nouns[binding.HeroNoun] {
			return errors.New("each character must have one valid creature binding")
		}
		nouns[binding.HeroNoun] = true
	}
	record, err := repository.LoadByLoginName(ctx, req.LoginName)
	if req.IsNew {
		if err == nil {
			return sporenet.ErrUserExists
		}
		if !errors.Is(err, sporenet.ErrUserNotFound) {
			return fmt.Errorf("profileCheck: %w", err)
		}
		user := sporenet.NewUser(req.LoginName, req.LoginName, "")
		user.Account.AvatarID = req.AvatarID
		user.IsTutorialCompletionPending = req.IsTutorialSkipped
		record = user.Record()
	} else if err != nil {
		return fmt.Errorf("profileLoad: %w", err)
	}
	if record.Settings == nil {
		record.Settings = make(map[string]string)
	}
	for key := range record.Settings {
		if strings.HasPrefix(key, bindingPrefix) {
			delete(record.Settings, key)
		}
	}
	for _, binding := range req.Bindings {
		record.Settings[bindingPrefix+strconv.FormatUint(uint64(binding.HeroNoun), 10)] = binding.CreationID
	}
	if req.IsNew {
		createdID, createErr := repository.Create(ctx, record)
		if createErr != nil {
			return fmt.Errorf("profileCreate: %w", createErr)
		}
		if createdID == 0 {
			return errors.New("created profile has no identity")
		}
		return nil
	}
	err = repository.Save(ctx, record)
	if err != nil {
		return fmt.Errorf("profileSave: %w", err)
	}
	return nil
}

func Bindings(record sporenet.UserRecord) ([]Binding, error) {
	bindings := make([]Binding, 0)
	for key, creationID := range record.Settings {
		if !strings.HasPrefix(key, bindingPrefix) {
			continue
		}
		noun, err := strconv.ParseUint(strings.TrimPrefix(key, bindingPrefix), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("bindingNoun: %w", err)
		}
		bindings = append(bindings, Binding{HeroNoun: uint32(noun), CreationID: creationID})
	}
	sort.Slice(bindings, func(left, right int) bool { return bindings[left].HeroNoun < bindings[right].HeroNoun })
	return bindings, nil
}

type Appearance struct {
	HeroName       string
	CreationID     string
	ThumbPath      string
	LargePath      string
	SavedThumbPath string
	SavedLargePath string
}

// ApplyAppearances changes only presentation on a detached profile record.
// Hero abilities, equipment, statistics and unlock progression are retained.
func ApplyAppearances(ctx context.Context, repository sporenet.UserRepository, loginName string, appearances []Appearance) error {
	record, err := repository.LoadByLoginName(ctx, loginName)
	if err != nil {
		return fmt.Errorf("appearanceLoad: %w", err)
	}
	if record.Settings == nil {
		record.Settings = make(map[string]string)
	}
	for _, creature := range record.Creatures {
		if creature == nil {
			continue
		}
		key := "mod.original." + strconv.FormatUint(uint64(creature.ID), 10)
		previousVersion := creature.Version
		originalThumb, isOriginal := record.Settings[key+".thumb"]
		if isOriginal {
			creature.ThumbImageURL = originalThumb
			creature.LargeImageURL = record.Settings[key+".large"]
			creature.Version = 1
		}
		isBound := false
		for _, appearance := range appearances {
			if !strings.EqualFold(creature.TemplateName, appearance.HeroName) {
				continue
			}
			if !isOriginal {
				record.Settings[key+".thumb"] = creature.ThumbImageURL
				record.Settings[key+".large"] = creature.LargeImageURL
			}
			creature.ThumbImageURL = appearance.ThumbPath
			creature.LargeImageURL = appearance.LargePath
			previousCreationID := record.Settings[key+".creation"]
			// Older Mod profiles have no creation marker. Their already-saved
			// edits migrate once; subsequent binding changes select a fresh import.
			isSameBinding := previousCreationID == appearance.CreationID || (previousCreationID == "" && isOriginal)
			if isSameBinding && previousVersion > 1 && appearance.SavedThumbPath != "" && appearance.SavedLargePath != "" {
				creature.ThumbImageURL = appearance.SavedThumbPath
				creature.LargeImageURL = appearance.SavedLargePath
				creature.Version = previousVersion
			} else {
				creature.Version = 1
			}
			record.Settings[key+".creation"] = appearance.CreationID
			isBound = true
		}
		if !isBound {
			delete(record.Settings, key+".creation")
		}
	}
	err = repository.Save(ctx, record)
	if err != nil {
		return fmt.Errorf("appearanceSave: %w", err)
	}
	return nil
}
