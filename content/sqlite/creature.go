package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const (
	nounFixedSize        = 480
	playerClassFixedSize = 256
	playerClassStatSize  = 88
	playerClassStatType  = 0x474940a5
	descriptionLocaleKey = "0x0acaf252"
)

var creatureAbilitySlots = []string{"basic", "special_1", "special_2", "random", "passive"}

type creatureTemplate struct {
	ID              uint32
	NameLocaleKey   string
	Name            string
	ElementType     string
	ClassType       string
	WeaponMinDamage float64
	WeaponMaxDamage float64
	StatTemplate    string
	IsHandPresent   bool
	IsFootPresent   bool
	BasicAbilityID  uint32
	Ability         map[string]string
}

type nounIdentity struct {
	BaseName        string
	PlayerClassName string
}

type playerClassIdentity struct {
	BaseName        string
	NameLocaleKey   string
	ElementType     string
	ClassType       string
	WeaponMinDamage float64
	WeaponMaxDamage float64
	IsHandPresent   bool
	IsFootPresent   bool
	BasicAbility    string
	BasicAbilityID  uint32
	SpecialAbility1 string
	SpecialAbility2 string
	RandomAbility   string
	PassiveAbility  string
}

func writeCreatureTemplates(ctx context.Context, transaction *sql.Tx, installPath string) error {
	packagePath := filepath.Join(installPath, "Data", "AssetData_Binary.package")
	r, err := os.Open(packagePath)
	if err != nil {
		return fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return fmt.Errorf("packageStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return fmt.Errorf("packageRead: %w", err)
	}
	nouns := make([]nounIdentity, 0, 100)
	classes := make(map[string]playerClassIdentity, 100)
	statTemplates := make(map[uint32]string, 100)
	for ordinal, entry := range pkg.Entries {
		select {
		case <-ctx.Done():
			return fmt.Errorf("scanContext: %w", ctx.Err())
		default:
		}
		decoded, openErr := pkg.Open(entry)
		if openErr != nil {
			return fmt.Errorf("payloadOpen[%d]: %w", ordinal, openErr)
		}
		payload, readErr := io.ReadAll(decoded)
		if readErr != nil {
			return fmt.Errorf("payloadRead[%d]: %w", ordinal, readErr)
		}
		noun, isNoun := decodePlayerNoun(payload)
		if isNoun {
			nouns = append(nouns, noun)
		}
		class, isClass := decodePlayerClass(payload)
		if isClass {
			classes[strings.ToLower(class.BaseName)] = class
		}
		if entry.Type == playerClassStatType {
			statTemplate, isStatTemplate := decodePlayerClassStats(payload)
			if isStatTemplate {
				statTemplates[uint32(entry.Instance)] = statTemplate
			}
		}
	}
	if len(pkg.Entries) == 0 {
		return nil
	}
	if len(nouns) != 100 {
		return fmt.Errorf("nounCount: got %d, want 100", len(nouns))
	}
	templates := make([]creatureTemplate, 0, len(nouns))
	for _, noun := range nouns {
		class, isFound := classes[strings.ToLower(noun.BaseName)]
		if !isFound {
			return fmt.Errorf("classMissing[%s]: %s", noun.BaseName, noun.PlayerClassName)
		}
		statTemplate, isFound := statTemplates[hashID(noun.BaseName)]
		if !isFound {
			return fmt.Errorf("statMissing[%s]: %s", noun.BaseName, class.BaseName)
		}
		baseName := strings.SplitN(class.NameLocaleKey, "!", 2)[0]
		templates = append(templates, creatureTemplate{
			ID:              hashID(noun.BaseName + ".Noun"),
			NameLocaleKey:   localeKey(class.NameLocaleKey),
			Name:            baseName + " " + creatureVariant(noun.BaseName),
			ElementType:     class.ElementType,
			ClassType:       class.ClassType,
			WeaponMinDamage: class.WeaponMinDamage,
			WeaponMaxDamage: class.WeaponMaxDamage,
			StatTemplate:    statTemplate,
			IsHandPresent:   class.IsHandPresent,
			IsFootPresent:   class.IsFootPresent,
			BasicAbilityID:  class.BasicAbilityID,
			Ability: map[string]string{
				"basic":     class.BasicAbility,
				"special_1": class.SpecialAbility1,
				"special_2": class.SpecialAbility2,
				"random":    class.RandomAbility,
				"passive":   class.PassiveAbility,
			},
		})
	}
	sort.Slice(templates, func(left, right int) bool { return templates[left].ID < templates[right].ID })
	return insertCreatureTemplates(ctx, transaction, templates)
}

func decodePlayerNoun(payload []byte) (nounIdentity, bool) {
	if len(payload) <= nounFixedSize || payload[330] != 1 || payload[331] != 1 {
		return nounIdentity{}, false
	}
	playerClassName, isFound := readStringWithSuffix(payload, nounFixedSize, 5, ".PlayerClass")
	if !isFound {
		return nounIdentity{}, false
	}
	baseName := strings.TrimSuffix(playerClassName, ".PlayerClass")
	if !strings.HasPrefix(strings.ToLower(baseName), "pc_") {
		return nounIdentity{}, false
	}
	return nounIdentity{BaseName: baseName, PlayerClassName: playerClassName}, true
}

func readStringWithSuffix(payload []byte, offset, count int, suffix string) (string, bool) {
	for index := 0; index < count; index++ {
		if offset >= len(payload) {
			return "", false
		}
		end := offset
		for end < len(payload) && payload[end] != 0 {
			if payload[end] < 0x20 || payload[end] > 0x7e {
				return "", false
			}
			end++
		}
		if end == len(payload) {
			return "", false
		}
		field := string(payload[offset:end])
		if strings.HasSuffix(field, suffix) {
			return field, true
		}
		offset = end + 1
	}
	return "", false
}

func decodePlayerClass(payload []byte) (playerClassIdentity, bool) {
	if len(payload) <= playerClassFixedSize {
		return playerClassIdentity{}, false
	}
	element := int(binary.LittleEndian.Uint32(payload[4:8]))
	class := int(binary.LittleEndian.Uint32(payload[72:76]))
	if element < 0 || element > 4 || class < 0 || class > 2 {
		return playerClassIdentity{}, false
	}
	fields, err := readStrings(payload, playerClassFixedSize, 10)
	if err != nil || !strings.Contains(fields[1], "!0x") || !strings.HasSuffix(fields[9], ".ClassAttributes") {
		return playerClassIdentity{}, false
	}
	baseName := strings.TrimSuffix(fields[9], ".ClassAttributes")
	if !strings.HasPrefix(strings.ToLower(baseName), "pc_") {
		return playerClassIdentity{}, false
	}
	minimumDamage := math.Float32frombits(binary.LittleEndian.Uint32(payload[232:236]))
	maximumDamage := math.Float32frombits(binary.LittleEndian.Uint32(payload[236:240]))
	return playerClassIdentity{
		BaseName: baseName, NameLocaleKey: fields[1], ElementType: creatureElement(element), ClassType: creatureClass(class),
		WeaponMinDamage: float64(minimumDamage), WeaponMaxDamage: float64(maximumDamage),
		IsHandPresent: payload[245] == 0, IsFootPresent: payload[246] == 0,
		BasicAbility: fields[4], BasicAbilityID: binary.LittleEndian.Uint32(payload[96:100]),
		SpecialAbility1: fields[7], SpecialAbility2: fields[5], RandomAbility: fields[6], PassiveAbility: fields[8],
	}, true
}

func decodePlayerClassStats(payload []byte) (string, bool) {
	if len(payload) != playerClassStatSize {
		return "", false
	}
	field := func(index int) float64 {
		offset := index * 4
		bits := binary.LittleEndian.Uint32(payload[offset : offset+4])
		return float64(math.Float32frombits(bits))
	}
	baseHealth := field(0)
	basePower := field(1)
	strength := field(2)
	dexterity := field(3)
	mind := field(4)
	baseDodge := field(5)
	baseResist := field(7)
	baseCritical := field(8)
	stats := []float64{baseHealth, basePower, strength, dexterity, mind, baseDodge, baseResist, baseCritical}
	for _, stat := range stats {
		if math.IsNaN(stat) || math.IsInf(stat, 0) || stat < 0 || stat > 100000 {
			return "", false
		}
	}
	health := baseHealth + (strength-10)*5
	if health < 0 {
		health = 0
	}
	power := basePower + mind
	dodge := baseDodge + dexterity*6
	resist := baseResist + mind*6
	critical := baseCritical + dexterity*4
	return fmt.Sprintf(
		"STR,%.0f,0;DEX,%.0f,0;MIND,%.0f,0;HLTH,%.0f,0;MANA,%.0f,0;PDEF,%.0f,0;EDEF,%.0f,0;CRTR,%.0f,0;",
		math.Round(strength), math.Round(dexterity), math.Round(mind), math.Round(health),
		math.Round(power), math.Round(dodge), math.Round(resist), math.Round(critical),
	), true
}

func readStrings(payload []byte, offset, count int) ([]string, error) {
	result := make([]string, 0, count)
	for len(result) < count {
		if offset >= len(payload) {
			return nil, errors.New("stringOffset: exceeds payload")
		}
		end := offset
		for end < len(payload) && payload[end] != 0 {
			if payload[end] < 0x20 || payload[end] > 0x7e {
				return nil, fmt.Errorf("stringByte: 0x%02x at %d", payload[end], end)
			}
			end++
		}
		if end == len(payload) {
			return nil, errors.New("stringEnd: missing terminator")
		}
		result = append(result, string(payload[offset:end]))
		offset = end + 1
	}
	return result, nil
}

func insertCreatureTemplates(ctx context.Context, transaction *sql.Tx, templates []creatureTemplate) error {
	templateStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO creature_template
		(id, name_locale_key, description_locale_key, name, element_type, class_type,
		 weapon_min_damage, weapon_max_damage, gear_score, stat_template,
		 ability_passive, ability_basic, ability_random, ability_special_1, ability_special_2,
		 is_hand_present, is_foot_present)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("templatePrepare: %w", err)
	}
	defer templateStatement.Close()
	abilityStatement, err := transaction.PrepareContext(ctx, `
		INSERT INTO creature_template_ability (creature_template_id, slot, asset_name)
		VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("abilityPrepare: %w", err)
	}
	defer abilityStatement.Close()
	for _, template := range templates {
		abilityPassive, abilityErr := creatureAbilityID(template.Ability["passive"])
		if abilityErr != nil {
			return fmt.Errorf("abilityPassive[%d]: %w", template.ID, abilityErr)
		}
		abilityBasic := template.BasicAbilityID
		if abilityBasic == 0 {
			abilityBasic, abilityErr = creatureAbilityID(template.Ability["basic"])
			if abilityErr != nil {
				return fmt.Errorf("abilityBasic[%d]: %w", template.ID, abilityErr)
			}
		}
		abilityRandom, abilityErr := creatureAbilityID(template.Ability["random"])
		if abilityErr != nil {
			return fmt.Errorf("abilityRandom[%d]: %w", template.ID, abilityErr)
		}
		abilitySpecial1, abilityErr := creatureAbilityID(template.Ability["special_1"])
		if abilityErr != nil {
			return fmt.Errorf("abilitySpecial1[%d]: %w", template.ID, abilityErr)
		}
		abilitySpecial2, abilityErr := creatureAbilityID(template.Ability["special_2"])
		if abilityErr != nil {
			return fmt.Errorf("abilitySpecial2[%d]: %w", template.ID, abilityErr)
		}
		_, err = templateStatement.ExecContext(ctx, template.ID, template.NameLocaleKey, descriptionLocaleKey, template.Name,
			template.ElementType, template.ClassType, template.WeaponMinDamage, template.WeaponMaxDamage,
			template.StatTemplate,
			abilityPassive, abilityBasic, abilityRandom, abilitySpecial1, abilitySpecial2,
			template.IsHandPresent, template.IsFootPresent)
		if err != nil {
			return fmt.Errorf("templateRow[%d]: %w", template.ID, err)
		}
		for _, slot := range creatureAbilitySlots {
			abilityName := template.Ability[slot]
			if abilityName == "" || abilityName == "0" {
				continue
			}
			_, err = abilityStatement.ExecContext(ctx, template.ID, slot, abilityName)
			if err != nil {
				return fmt.Errorf("abilityRow[%d:%s]: %w", template.ID, slot, err)
			}
		}
	}
	return nil
}

func creatureAbilityID(assetName string) (uint32, error) {
	if assetName == "" || assetName == "0" {
		return 0, nil
	}
	parsedID, err := strconv.ParseUint(assetName, 10, 32)
	if err == nil {
		return uint32(parsedID), nil
	}
	var numberErr *strconv.NumError
	if errors.As(err, &numberErr) && errors.Is(numberErr.Err, strconv.ErrRange) {
		return 0, fmt.Errorf("abilityParse: %w", err)
	}
	return hashID(assetName), nil
}

func localeKey(authored string) string {
	parts := strings.SplitN(authored, "!", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}

func creatureVariant(baseName string) string {
	switch {
	case strings.HasSuffix(strings.ToLower(baseName), "_v1"):
		return "Beta"
	case strings.HasSuffix(strings.ToLower(baseName), "_v2"):
		return "Gamma"
	case strings.HasSuffix(strings.ToLower(baseName), "_v3"):
		return "Delta"
	default:
		return "Alpha"
	}
}

func creatureElement(element int) string {
	return map[int]string{0: "CYBER", 1: "CHRONO", 2: "BIO", 3: "PLASMA", 4: "NECRO"}[element]
}

func creatureClass(class int) string {
	return map[int]string{0: "SENTINEL", 1: "RAVAGER", 2: "TEMPEST"}[class]
}

func hashID(name string) uint32 {
	hash := uint32(0x811c9dc5)
	for index := 0; index < len(name); index++ {
		hash *= 0x01000193
		character := name[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		hash ^= uint32(character)
	}
	return hash
}
