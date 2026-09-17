package boss

import (
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type namedBossDefinition struct {
	level           string
	nounName        [3]string
	chainLevelIndex [3]uint32
}

var namedBossCatalog = [...]namedBossDefinition{
	1:  {level: "zelems_1", nounName: [3]string{"ZelemSpecialHaster_Captain.Noun", "ZelemSpecialOne_Captain_2.Noun", "ZelemSpecialTwo_Captain_3.Noun"}, chainLevelIndex: [3]uint32{1, 41, 68}},
	2:  {level: "zelems_3", nounName: [3]string{"ZelemSpecialOne_Captain.Noun", "NomadSnipe_Captain_2.Noun", "ZelemSpecialHaster_Captain_3.Noun"}, chainLevelIndex: [3]uint32{2, 28, 54}},
	3:  {level: "nocturna_4", nounName: [3]string{"NocturnaSpecialLeech_Captain.Noun", "NocturnaSpecialMunch_Captain_2.Noun", "nct_lieu_su_stealther_Captain_3.Noun"}, chainLevelIndex: [3]uint32{3, 43, 55}},
	4:  {level: "nocturna_1", nounName: [3]string{"ShadowBoss.Noun", "NocturnaSpecialLeech_Captain_2.Noun", "NocturnaSpecialMunch_Captain_3.Noun"}, chainLevelIndex: [3]uint32{4, 37, 70}},
	5:  {level: "verdanth_1", nounName: [3]string{"VerdanthSpecialOne_Captain.Noun", "CryosSpecialThree_Captain_2.Noun", "CryosSpecialThree_Captain_3.Noun"}, chainLevelIndex: [3]uint32{5, 45, 52}},
	6:  {level: "verdanth_3", nounName: [3]string{"NomadSpecialOne_Captain.Noun", "VerdanthSpecialOne_Captain_2.Noun", "VerdanthSpecialOne_Captain_3.Noun"}, chainLevelIndex: [3]uint32{6, 32, 61}},
	7:  {level: "zelems_2", nounName: [3]string{"NomadSnipe_Captain.Noun", "ZelemSpecialTwo_Captain_2.Noun", "NomadSnipe_Captain_3.Noun"}, chainLevelIndex: [3]uint32{7, 27, 57}},
	8:  {level: "zelems_4", nounName: [3]string{"ZelemBoss.Noun", "ZelemBoss_2.Noun", "ZelemBoss_3.Noun"}, chainLevelIndex: [3]uint32{8, 42, 63}},
	9:  {level: "cryos_4", nounName: [3]string{"CryosElementalSpecialThree_Captain.Noun", "NomadRuption_Captain_2.Noun", "CryosSpecialOne_Captain_3.Noun"}, chainLevelIndex: [3]uint32{9, 35, 69}},
	10: {level: "cryos_3", nounName: [3]string{"NomadRuption_Captain.Noun", "CryosSpecialTwo_Captain_2.Noun", "CryosElementalSpecialThree_Captain_3.Noun"}, chainLevelIndex: [3]uint32{10, 36, 62}},
	11: {level: "verdanth_2", nounName: [3]string{"CryosSpecialThree_Captain.Noun", "VerdanthSpecialTwo_Captain_2.Noun", "VerdanthSpecialTwo_Captain_3.Noun"}, chainLevelIndex: [3]uint32{11, 31, 67}},
	12: {level: "verdanth_4", nounName: [3]string{"VerdanthBoss.Noun", "VerdanthBoss_2.Noun", "VerdanthBoss_3.Noun"}, chainLevelIndex: [3]uint32{12, 46, 58}},
	13: {level: "infinity_2", nounName: [3]string{"CitadelSpecialFour_Captain.Noun", "NomadShielder_Captain_2.Noun", "NomadShielder_Captain_3.Noun"}, chainLevelIndex: [3]uint32{13, 40, 65}},
	14: {level: "infinity_3", nounName: [3]string{"NomadShielder_Captain.Noun", "CitadelSpecialFour_Captain_2.Noun", "CitadelSpecialFour_Captain_3.Noun"}, chainLevelIndex: [3]uint32{14, 30, 56}},
	15: {level: "cryos_1", nounName: [3]string{"CryosSpecialTwo_Captain.Noun", "CryosSpecialOne_Captain_2.Noun", "NomadRuption_Captain_3.Noun"}, chainLevelIndex: [3]uint32{15, 25, 60}},
	16: {level: "cryos_2", nounName: [3]string{"CryosBoss.Noun", "CryosElementalSpecialThree_Captain_2.Noun", "CryosSpecialTwo_Captain_3.Noun"}, chainLevelIndex: [3]uint32{16, 26, 51}},
	17: {level: "nocturna_3", nounName: [3]string{"NocturnaSpecialMunch_Captain.Noun", "Rezzer_Captain_2.Noun", "Rezzer_Captain_3.Noun"}, chainLevelIndex: [3]uint32{17, 44, 49}},
	18: {level: "nocturna_2", nounName: [3]string{"Rezzer_Captain.Noun", "nct_lieu_su_stealther_Captain_2.Noun", "NocturnaSpecialLeech_Captain_3.Noun"}, chainLevelIndex: [3]uint32{18, 38, 64}},
	19: {level: "infinity_1", nounName: [3]string{"CitadelSpecialThree_Captain.Noun", "CitadelSpecialTwo_Captain_2.Noun", "CitadelSpecialTwo_Captain_3.Noun"}, chainLevelIndex: [3]uint32{19, 29, 71}},
	20: {level: "infinity_4", nounName: [3]string{"CitadelBoss.Noun", "CitadelSpecialThree_Captain_2.Noun", "CitadelSpecialThree_Captain_3.Noun"}, chainLevelIndex: [3]uint32{20, 39, 50}},
	21: {level: "scaldron_2", nounName: [3]string{"NomadBioSpecialTwo_Captain.Noun", "VerdanthSpecialThree_Captain_2.Noun", "NomadBioSpecialTwo_Captain_3.Noun"}, chainLevelIndex: [3]uint32{21, 33, 66}},
	22: {level: "scaldron_1", nounName: [3]string{"NomadScope_Captain.Noun", "NomadBioSpecialTwo_Captain_2.Noun", "VerdanthSpecialThree_Captain_3.Noun"}, chainLevelIndex: [3]uint32{22, 48, 53}},
	23: {level: "scaldron_3", nounName: [3]string{"NomadWithDrone_Captain.Noun", "NomadScope_Captain_2.Noun", "NomadWithDrone_Captain_3.Noun"}, chainLevelIndex: [3]uint32{23, 47, 72}},
	24: {level: "scaldron_4", nounName: [3]string{"ScaldronBoss.Noun", "NomadWithDrone_Captain_2.Noun", "NomadScope_Captain_3.Noun"}, chainLevelIndex: [3]uint32{24, 34, 59}},
}

func SelectNamedLeader(
	director game.CampaignDirector, entry []game.CampaignDirectorEntry,
	chainLevelIndex uint32, gameID uint32, markerID uint32,
) (game.CampaignDirectorEntry, error) {
	if director.Level == "" || len(entry) == 0 || chainLevelIndex == 0 ||
		gameID == 0 || markerID == 0 {
		return game.CampaignDirectorEntry{}, errors.New("selection invalid")
	}
	nounName, isNamedBoss := NamedBossNoun(
		chainLevelIndex, director.Level,
	)
	if !isNamedBoss {
		return entry[int((gameID^markerID)%uint32(len(entry)))], nil
	}
	var selected game.CampaignDirectorEntry
	for _, candidate := range entry {
		if !strings.EqualFold(candidate.NounName, nounName) {
			continue
		}
		if selected.NounName != "" {
			return game.CampaignDirectorEntry{},
				fmt.Errorf("duplicate %q", nounName)
		}
		selected = candidate
	}
	if selected.NounName == "" {
		return game.CampaignDirectorEntry{}, fmt.Errorf("missing %q", nounName)
	}
	return selected, nil
}

func ValidateNamedBossDirector(
	director game.CampaignDirector, chainLevelIndex uint32,
) error {
	nounName, isNamedBoss := NamedBossNoun(
		chainLevelIndex, director.Level,
	)
	if !isNamedBoss {
		return nil
	}
	_, isIdentityFound := namedBossIdentity(director, nounName)
	if !isIdentityFound {
		return fmt.Errorf("named boss: identity missing for %q", nounName)
	}
	entry := NamedBossEntries(director, chainLevelIndex)
	if len(entry) == 0 {
		return fmt.Errorf(
			"named boss: complete %q missing from %q",
			nounName, director.Level,
		)
	}
	_, err := SelectNamedLeader(director, entry, chainLevelIndex, 1, 1)
	if err != nil {
		return fmt.Errorf("named boss: %w", err)
	}
	return nil
}

func NamedBossEntries(
	director game.CampaignDirector, chainLevelIndex uint32,
) []game.CampaignDirectorEntry {
	nounName, isNamedBoss := NamedBossNoun(
		chainLevelIndex, director.Level,
	)
	if !isNamedBoss {
		return nil
	}
	if IsFinalBossNoun(nounName) {
		for _, entry := range director.StandaloneBossEntries {
			if strings.EqualFold(entry.NounName, nounName) &&
				entry.NPCProfile.IsKnown && entry.NPCProfile.HitPoint > 0 {
				return []game.CampaignDirectorEntry{entry}
			}
		}
		return nil
	}
	entry := make([]game.CampaignDirectorEntry, 0, 1)
	for _, pool := range director.Pools {
		for _, candidate := range pool.Entries {
			if !strings.EqualFold(candidate.NounName, nounName) {
				continue
			}
			if len(CompleteEntries(
				[]game.CampaignDirectorEntry{candidate}, false,
			)) != 0 {
				entry = append(entry, candidate)
				continue
			}
			profile, isProfileFound := positionAlignedCaptainProfile(
				director, candidate.ConfigurationEntryOrdinal,
			)
			if isProfileFound {
				candidate.NPCProfile = profile
				entry = append(entry, candidate)
			}
		}
	}
	return entry
}

// PrepareNamedBossDirector attaches package-authored boss data before shared
// difficulty projection. Standalone bosses remain outside ordinary population
// pools, while recovered captain profiles replace incomplete inheritance stubs.
func PrepareNamedBossDirector(
	director game.CampaignDirector, chainLevelIndex uint32,
) (game.CampaignDirector, error) {
	nounName, isNamedBoss := NamedBossNoun(chainLevelIndex, director.Level)
	if !isNamedBoss {
		return director, nil
	}
	_, isIdentityFound := namedBossIdentity(director, nounName)
	if !isIdentityFound {
		return game.CampaignDirector{}, fmt.Errorf(
			"named boss identity missing: %q", nounName,
		)
	}
	if !IsFinalBossNoun(nounName) {
		return prepareNamedCaptainProfile(director, nounName)
	}
	rank, isRankFound := campaignBossRank(chainLevelIndex, director.Level)
	if !isRankFound {
		return game.CampaignDirector{}, errors.New("standalone boss rank unavailable")
	}
	normalizedName := strings.ToLower(nounName)
	profile, isProfileFound := director.NPCProfilesByNoun[normalizedName]
	if !isProfileFound || !profile.IsKnown || profile.HitPoint <= 0 {
		profile = standaloneBossClassProfile(rank)
	}
	profile.NPCRank = int32(rank)
	profile.IsTargetable = true
	profile.IsKnown = true
	if profile.PlayerCountHealthScale <= 0 {
		profile.PlayerCountHealthScale = 1
	}
	profile = NormalizeFinalBossProfile(nounName, profile)
	director.StandaloneBossEntries = append(
		[]game.CampaignDirectorEntry(nil), director.StandaloneBossEntries...,
	)
	director.StandaloneBossEntries = append(
		director.StandaloneBossEntries,
		game.CampaignDirectorEntry{
			NounName: nounName, MinimumDifficulty: chainLevelIndex,
			MaximumDifficulty: chainLevelIndex, NPCProfile: profile,
		},
	)
	return director, nil
}

// NormalizeFinalBossProfile retains the authored presentation scale while
// giving every Destructor the broad combat footprint used by the packaged
// Zelem and Verdanth boss nouns. Developer zoo spawns use this same boundary.
func NormalizeFinalBossProfile(
	nounName string, profile game.CampaignNPCProfile,
) game.CampaignNPCProfile {
	if !IsFinalBossNoun(nounName) {
		return profile
	}
	normalizedName := strings.ToLower(nounName)
	if strings.HasPrefix(normalizedName, "zelemboss") ||
		strings.HasPrefix(normalizedName, "verdanthboss") {
		profile.GraphicsScale = 5.4
	}
	if profile.GraphicsScale <= 0 {
		profile.GraphicsScale = 1
	}
	profile.FootprintRadius = max(profile.FootprintRadius, float32(2.7))
	return profile
}

func prepareNamedCaptainProfile(
	director game.CampaignDirector, nounName string,
) (game.CampaignDirector, error) {
	profile, isProfileFound := exactNamedCaptainProfile(nounName)
	if !isProfileFound {
		return director, nil
	}
	director.Pools = append([]game.CampaignDirectorPool(nil), director.Pools...)
	for poolIndex := range director.Pools {
		director.Pools[poolIndex].Entries = append(
			[]game.CampaignDirectorEntry(nil), director.Pools[poolIndex].Entries...,
		)
		for entryIndex := range director.Pools[poolIndex].Entries {
			entry := &director.Pools[poolIndex].Entries[entryIndex]
			if !strings.EqualFold(entry.NounName, nounName) {
				continue
			}
			profile = retainBossClassMetadata(profile, entry.NPCProfile)
			entry.NPCProfile = profile
			return director, nil
		}
	}
	return game.CampaignDirector{}, fmt.Errorf(
		"named captain profile missing: %q", nounName,
	)
}

// IsFinalBossNoun reports whether a named encounter uses a standalone final
// boss rather than a captain promoted as the mission's lesser boss.
func IsFinalBossNoun(nounName string) bool {
	normalizedName := strings.TrimSuffix(
		strings.ToLower(strings.TrimSpace(nounName)), ".noun",
	)
	for _, rankSuffix := range []string{"_2", "_3"} {
		if strings.HasSuffix(normalizedName, rankSuffix) {
			normalizedName = strings.TrimSuffix(normalizedName, rankSuffix)
			break
		}
	}
	switch normalizedName {
	case "shadowboss", "zelemboss", "verdanthboss", "cryosboss",
		"citadelboss", "scaldronboss":
		return true
	default:
		return false
	}
}

func namedBossIdentity(
	director game.CampaignDirector, nounName string,
) (zonenpc.BossIdentity, bool) {
	identity, isIdentityFound := director.NPCIdentitiesByNoun[strings.ToLower(nounName)]
	if !isIdentityFound {
		baseNounName := baseBossIdentityNoun(nounName)
		identity, isIdentityFound = director.NPCIdentitiesByNoun[baseNounName]
	}
	if !isIdentityFound {
		return zonenpc.BossIdentity{}, false
	}
	return zonenpc.BossIdentityFromContent(identity)
}

func baseBossIdentityNoun(nounName string) string {
	baseName := strings.TrimSuffix(
		strings.ToLower(strings.TrimSpace(nounName)), ".noun",
	)
	rankSuffix := ""
	for _, candidateSuffix := range []string{"_2", "_3"} {
		if !strings.HasSuffix(baseName, candidateSuffix) {
			continue
		}
		baseName = strings.TrimSuffix(baseName, candidateSuffix)
		rankSuffix = candidateSuffix
		break
	}
	baseName = strings.TrimSuffix(baseName, "_captain")
	return baseName + rankSuffix + ".noun"
}

func standaloneBossClassProfile(rank uint32) game.CampaignNPCProfile {
	return game.CampaignNPCProfile{
		ChallengeValue: 500, NPCRank: int32(rank), IsTargetable: true,
		PlayerCountHealthScale: 1,
		HitPoint:               1500 + float32(rank-1)*250,
		PowerPoint:             100,
		Strength:               10, Dexterity: 10, Mind: 10,
		DodgeRating: 60, ResistRating: 60, CriticalRating: 45,
		IsKnown: true,
	}
}

func retainBossClassMetadata(
	profile game.CampaignNPCProfile, authored game.CampaignNPCProfile,
) game.CampaignNPCProfile {
	profile.ChallengeValue = authored.ChallengeValue
	profile.NPCRank = authored.NPCRank
	profile.IsTargetable = authored.IsTargetable
	profile.IsPlayerPet = authored.IsPlayerPet
	profile.PlayerCountHealthScale = authored.PlayerCountHealthScale
	return profile
}

func exactNamedCaptainProfile(nounName string) (game.CampaignNPCProfile, bool) {
	switch strings.ToLower(nounName) {
	case "rezzer_captain.noun":
		return game.CampaignNPCProfile{
			HitPoint: 80, PowerPoint: 100,
			Strength: 10, Dexterity: 10, Mind: 10,
			DodgeRating: 60, ResistRating: 60, CriticalRating: 45,
			GraphicsScale: 2.7, FootprintRadius: 1.35, IsKnown: true,
		}, true
	case "citadelspecialthree_captain.noun":
		return game.CampaignNPCProfile{
			HitPoint: 120, PowerPoint: 100,
			Strength: 10, Dexterity: 10, Mind: 10,
			DodgeRating: 60, ResistRating: 260, CriticalRating: 90,
			GraphicsScale: 3.3, FootprintRadius: 1.65, IsKnown: true,
		}, true
	case "citadelspecialtwo_captain.noun":
		return game.CampaignNPCProfile{
			HitPoint: 80, PowerPoint: 100,
			Strength: 10, Dexterity: 10, Mind: 10,
			DodgeRating: 60, ResistRating: 60, CriticalRating: 45,
			GraphicsScale: 2.1, FootprintRadius: 1.05, IsKnown: true,
		}, true
	default:
		return game.CampaignNPCProfile{}, false
	}
}

func NamedBossNoun(chainLevelIndex uint32, level string) (string, bool) {
	for _, definition := range namedBossCatalog {
		if definition.level == "" ||
			!strings.EqualFold(level, definition.level) {
			continue
		}
		rank, isRankFound := campaignBossDefinitionRank(
			definition, chainLevelIndex,
		)
		if !isRankFound {
			return "", false
		}
		nounName := definition.nounName[rank-1]
		if nounName == "" {
			return "", false
		}
		return nounName, true
	}
	return "", false
}

func campaignBossRank(chainLevelIndex uint32, level string) (uint32, bool) {
	for _, definition := range namedBossCatalog {
		if definition.level == "" || !strings.EqualFold(level, definition.level) {
			continue
		}
		return campaignBossDefinitionRank(definition, chainLevelIndex)
	}
	return 0, false
}

func campaignBossDefinitionRank(
	definition namedBossDefinition, chainLevelIndex uint32,
) (uint32, bool) {
	for rankIndex, expectedIndex := range definition.chainLevelIndex {
		if chainLevelIndex == expectedIndex {
			return uint32(rankIndex + 1), true
		}
	}
	return 0, false
}

func CompleteEntries(
	entries []game.CampaignDirectorEntry, isHordeLegalRequired bool,
) []game.CampaignDirectorEntry {
	complete := make([]game.CampaignDirectorEntry, 0, len(entries))
	for _, candidate := range entries {
		if candidate.NounName == "" || !candidate.NPCProfile.IsKnown ||
			candidate.NPCProfile.HitPoint <= 0 ||
			(isHordeLegalRequired && !candidate.IsHordeLegal) {
			continue
		}
		complete = append(complete, candidate)
	}
	return complete
}

func positionAlignedCaptainProfile(
	director game.CampaignDirector, configurationEntryOrdinal int,
) (game.CampaignNPCProfile, bool) {
	var fallback game.CampaignNPCProfile
	for _, pool := range director.Pools {
		if pool.ConfigurationOrdinal != 3 &&
			!strings.EqualFold(pool.ConfigKind, "captain") {
			continue
		}
		for _, entry := range CompleteEntries(pool.Entries, false) {
			if entry.ConfigurationEntryOrdinal == configurationEntryOrdinal {
				return entry.NPCProfile, true
			}
			if !fallback.IsKnown {
				fallback = entry.NPCProfile
			}
		}
	}
	if fallback.IsKnown {
		return fallback, true
	}
	return game.CampaignNPCProfile{
		ChallengeValue: game.CampaignFallbackChallenge(director.Level),
		HitPoint:       100, PowerPoint: 75,
		Strength: 10, Dexterity: 10, Mind: 10,
		DodgeRating: 60, ResistRating: 60, CriticalRating: 45,
		GraphicsScale: 1, FootprintRadius: 0.5, IsKnown: true,
	}, true
}
