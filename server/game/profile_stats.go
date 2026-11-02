package game

import (
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// Build 103's native statsToString (0x4ce650) writes base,bonus and
// multiplies percentage attributes by 100. Gameplay uses normalized fractions;
// profile XML must retain the native display units and additive pair contract.
func profileStatsWithAttributes(
	stats []sporenet.Stat, fallback string, attributes [partAttributeCount]float32,
) string {
	fields := strings.Split(strings.TrimSuffix(profileStatsText(stats, fallback), ";"), ";")
	fieldIndexes := make(map[string]int, len(fields))
	for index, field := range fields {
		pieces := strings.SplitN(field, ",", 3)
		if len(pieces) == 3 {
			fieldIndexes[pieces[0]] = index
		}
	}
	for attributeIndex, amount := range attributes {
		token, isFound := partStatTokens[attributeIndex]
		if !isFound || amount == 0 {
			continue
		}
		coreAttributeIndex, isCore := gameplayCorePartAttribute(token)
		if isCore && coreAttributeIndex == attributeIndex {
			// gameplayCreatureStats already includes these equipment deltas.
			continue
		}
		if isPercentagePartAttribute(attributeIndex) {
			amount *= 100
		}
		index, isPresent := fieldIndexes[token]
		if !isPresent {
			fields = append(fields, token+",0,"+profileStatBonus(amount))
			continue
		}
		pieces := strings.SplitN(fields[index], ",", 3)
		bonus, err := strconv.ParseFloat(pieces[2], 32)
		if err != nil {
			// Preserve a malformed legacy field rather than replacing its base.
			continue
		}
		fields[index] = pieces[0] + "," + pieces[1] + "," + profileStatBonus(float32(bonus)+amount)
	}
	return strings.Join(fields, ";") + ";"
}

func profileStatBonus(amount float32) string {
	// Native statsToString converts each display component to an integer.
	return strconv.FormatInt(int64(amount), 10)
}
