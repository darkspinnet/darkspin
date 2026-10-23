package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/util"
)

const modHeroFitScale = 2.0

// ModHero retains its original bridge name and noun identity for old profiles.
// The catalog now includes every imported non-player noun, not just heroes.
func (e *App) modHeroes(paths *spinnerPathSet) ([]ModHero, error) {
	store, err := contentsqlite.New(e.modContext(), paths.contentPath)
	if err != nil {
		return nil, fmt.Errorf("catalogOpen: %w", err)
	}
	defer e.closeModContent(store)
	templates, err := store.CreatureTemplates(e.modContext())
	if err != nil {
		return nil, fmt.Errorf("catalogHero: %w", err)
	}
	profiles, err := store.NonPlayerNounProfiles(e.modContext())
	if err != nil {
		return nil, fmt.Errorf("catalogNPC: %w", err)
	}
	nounRecords, err := store.NounPhysicsCatalog(e.modContext())
	if err != nil {
		return nil, fmt.Errorf("catalogPhysics: %w", err)
	}
	nounRecordsByID := make(map[uint32]contentsqlite.NounPhysics, len(nounRecords))
	for _, noun := range nounRecords {
		nounRecordsByID[util.HashID(noun.AssetName)] = noun
	}
	sizes, err := readModNounSizes(e.modContext(), filepath.Join(paths.gamePath, "Data", "AssetData_Binary.package"))
	if err != nil {
		return nil, fmt.Errorf("catalogSize: %w", err)
	}
	characters := make([]ModHero, 0, len(templates)+len(profiles))
	nouns := make(map[uint32]bool)
	for _, template := range templates {
		character := ModHero{Noun: template.ID, Name: template.Name, Category: "Hero"}
		modCharacterSize(&character, nounRecordsByID[character.Noun], 1)
		modAuthoredSize(&character, sizes)
		// Apply once after resolving the target, including authored-size fallbacks.
		// Only bound hero presentation consumes this shared size policy.
		character.TargetExtent *= modHeroFitScale
		payload, readErr := os.ReadFile(filepath.Join(paths.staticPath, "template_png", fmt.Sprintf("%d_thumb.png", template.ID)))
		if readErr == nil {
			character.PortraitURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload)
		}
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return nil, fmt.Errorf("catalogPortrait: %w", readErr)
		}
		characters = append(characters, character)
		nouns[character.Noun] = true
	}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.NounName) == "" {
			continue
		}
		noun := util.HashID(profile.NounName)
		if noun == 0 || nouns[noun] {
			continue
		}
		name := strings.TrimSpace(profile.DisplayName)
		if name == "" {
			name = strings.ReplaceAll(strings.TrimSuffix(profile.NounName, ".Noun"), "_", " ")
		}
		character := ModHero{Noun: noun, Name: name, NounName: profile.NounName, Category: modCharacterCategory(profile)}
		modCharacterSize(&character, nounRecordsByID[noun], float64(profile.GraphicsScale))
		modAuthoredSize(&character, sizes)
		characters = append(characters, character)
		nouns[noun] = true
	}
	sort.Slice(characters, func(left, right int) bool {
		if characters[left].Noun == characters[right].Noun {
			return false
		}
		if characters[left].Noun == 1667741389 {
			return true
		}
		if characters[right].Noun == 1667741389 {
			return false
		}
		if characters[left].Name != characters[right].Name {
			return characters[left].Name < characters[right].Name
		}
		return characters[left].NounName < characters[right].NounName
	})
	return characters, nil
}

func modCharacterCategory(profile contentsqlite.NonPlayerNounProfile) string {
	if profile.IsPlayerPet {
		return "Pet"
	}
	if strings.Contains(strings.ToLower(profile.NounName), "captain") {
		return "Captain"
	}
	if profile.NPCType == 2 || profile.NPCType == 7 {
		return "Boss"
	}
	if profile.NPCRank > 1 {
		return "Elite"
	}
	switch profile.NPCType {
	case 0:
		return "Minion"
	case 1:
		return "Lieutenant"
	case 3:
		return "Support"
	case 4:
		return "Destructible"
	case 5:
		return "Agent"
	case 6:
		return "Neutral"
	default:
		return "Other"
	}
}

func modCharacterSize(character *ModHero, physics contentsqlite.NounPhysics, fallbackScale float64) {
	scale := float64(physics.GraphicsScale)
	if !(scale > 0) || math.IsInf(scale, 0) {
		scale = fallbackScale
	}
	if !(scale > 0) || math.IsInf(scale, 0) {
		scale = 1
	}
	span := max(float64(physics.BoundMaximumX-physics.BoundMinimumX), float64(physics.BoundMaximumY-physics.BoundMinimumY), float64(physics.BoundMaximumZ-physics.BoundMinimumZ))
	if !(span > 0) || math.IsInf(span, 0) {
		span = 2
	}
	if character.Category == "Hero" {
		span = max(span, 2)
	}
	character.NominalScale = scale
	character.TargetExtent = span * scale
	if physics.AssetName != "" {
		character.NounName = physics.AssetName
	}
}

func modAuthoredSize(character *ModHero, sizes map[uint32]modNounSize) {
	stem := strings.TrimSuffix(character.NounName, ".Noun")
	size, isKnown := sizes[util.HashID(stem)]
	if !isKnown {
		return
	}
	if !(size.scale > 0) || math.IsInf(size.scale, 0) {
		return
	}
	span := size.extent
	if !(span > 0) {
		span = 2
	}
	if character.Category == "Hero" {
		span = max(span, 2)
	}
	character.NominalScale = size.scale
	character.TargetExtent = span * size.scale
}
