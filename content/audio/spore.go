package audio

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

var (
	sporeDataOnce sync.Once
	sporeNames    map[uint32]string
	sporeAliases  map[uint32]SampleAlias
	sporeUsages   map[uint32][]SampleUsage
)

// SporeNames returns the authored and inferred names resolved once from the
// retail Spore audio packages and pinned into Darkrun.
func SporeNames() map[uint32]string {
	loadSporeData()
	names := make(map[uint32]string, len(sporeNames))
	for instanceID, name := range sporeNames {
		names[instanceID] = name
	}
	return names
}

// SporeSampleAliases returns the pinned editable WAV aliases and their direct
// Spore_Audio2 event references without scanning another package or registry.
func SporeSampleAliases() map[uint32]SampleAlias {
	loadSporeData()
	aliases := make(map[uint32]SampleAlias, len(sporeAliases))
	for instanceID, alias := range sporeAliases {
		alias.References = append([]SampleReference(nil), alias.References...)
		for referenceIndex := range alias.References {
			alias.References[referenceIndex].ContextNames = append([]string(nil), alias.References[referenceIndex].ContextNames...)
			alias.References[referenceIndex].Usages = append([]SampleUsage(nil), alias.References[referenceIndex].Usages...)
		}
		aliases[instanceID] = alias
	}
	return aliases
}

// SporeUsages returns the shipped resources containing exact references to
// each pinned Spore audio-event identity.
func SporeUsages() map[uint32][]SampleUsage {
	loadSporeData()
	usages := make(map[uint32][]SampleUsage, len(sporeUsages))
	for eventInstance, eventUsages := range sporeUsages {
		usages[eventInstance] = append([]SampleUsage(nil), eventUsages...)
	}
	return usages
}

func loadSporeData() {
	sporeDataOnce.Do(func() {
		sporeNames = make(map[uint32]string)
		for _, record := range sporeDataRecords(sporeNameData) {
			fields := strings.Split(record, ",")
			if len(fields) != 3 || fields[0] != "N" {
				panic(fmt.Sprintf("invalid Spore name record %q", record))
			}
			instanceID := uint32(sporeDataUint(fields[1], 16, 32))
			name := sporeDataText(fields[2])
			if strings.HasPrefix(strings.ToLower(name), "ds_") {
				name = name[len("ds_"):] + SampleAliasSuffix
			}
			sporeNames[instanceID] = name
		}
		loadSporeUsages()

		sporeAliases = make(map[uint32]SampleAlias)
		for _, record := range sporeDataRecords(sporeAliasData) {
			fields := strings.Split(record, ",")
			if len(fields) < 2 {
				panic(fmt.Sprintf("invalid Spore alias record %q", record))
			}
			instanceID := uint32(sporeDataUint(fields[1], 16, 32))
			switch fields[0] {
			case "I":
				if len(fields) != 2 {
					panic(fmt.Sprintf("invalid Spore alias identity %q", record))
				}
				name := sporeNames[instanceID]
				if name != "" && !IsSampleAlias(name) {
					name += SampleAliasSuffix
					sporeNames[instanceID] = name
				}
				sporeAliases[instanceID] = SampleAlias{
					Name:   name,
					Source: "SporeModder-FX name registry",
				}
			case "A":
				if len(fields) != 3 {
					panic(fmt.Sprintf("invalid Spore alias source %q", record))
				}
				alias := sporeAliases[instanceID]
				alias.Source = sporeDataText(fields[2])
				sporeAliases[instanceID] = alias
			case "R":
				appendSporeReference(instanceID, fields, record)
			default:
				panic(fmt.Sprintf("unknown Spore alias record %q", record))
			}
		}
	})
}

func loadSporeUsages() {
	sporeUsages = make(map[uint32][]SampleUsage)
	for _, record := range sporeDataRecords(sporeUsageData) {
		fields := strings.Split(record, ",")
		if len(fields) != 8 || fields[0] != "U" {
			panic(fmt.Sprintf("invalid Spore usage record %q", record))
		}
		eventInstance := uint32(sporeDataUint(fields[1], 16, 32))
		sporeUsages[eventInstance] = append(sporeUsages[eventInstance], SampleUsage{
			PackageName:      sporeDataText(fields[2]),
			Ordinal:          int(sporeDataUint(fields[3], 10, 32)),
			ResourceType:     uint32(sporeDataUint(fields[4], 16, 32)),
			ResourceGroup:    uint32(sporeDataUint(fields[5], 16, 32)),
			ResourceInstance: sporeDataUint(fields[6], 16, 64),
			Context:          sporeDataText(fields[7]),
		})
	}
}

func appendSporeReference(instanceID uint32, fields []string, record string) {
	if len(fields) != 12 {
		panic(fmt.Sprintf("invalid Spore reference record %q", record))
	}
	referenceIndex := int(sporeDataUint(fields[2], 10, 32))
	alias := sporeAliases[instanceID]
	if referenceIndex != len(alias.References) {
		panic(fmt.Sprintf("nonsequential Spore reference %q", record))
	}
	reference := SampleReference{
		ArchiveName:   sporeDataText(fields[3]),
		EventInstance: uint32(sporeDataUint(fields[4], 16, 32)),
		EventName:     sporeDataText(fields[5]),
		PropertyID:    uint32(sporeDataUint(fields[6], 16, 32)),
		PropertyName:  sporeDataText(fields[7]),
		PointerName:   sporeDataText(fields[8]),
		ItemIndex:     int(sporeDataUint(fields[9], 10, 32)),
		ItemCount:     int(sporeDataUint(fields[10], 10, 32)),
		Usages:        append([]SampleUsage(nil), sporeUsages[uint32(sporeDataUint(fields[4], 16, 32))]...),
	}
	isLooped, err := strconv.ParseBool(fields[11])
	if err != nil {
		panic(fmt.Sprintf("invalid Spore loop flag %q: %v", record, err))
	}
	reference.IsLooped = isLooped
	alias.References = append(alias.References, reference)
	sporeAliases[instanceID] = alias
}

func sporeDataRecords(data string) []string {
	return strings.FieldsFunc(data, func(character rune) bool {
		return character == '|' || character == '\r' || character == '\n'
	})
}

func sporeDataText(encodedText string) string {
	decodedText, err := base64.RawStdEncoding.DecodeString(encodedText)
	if err != nil {
		panic(fmt.Sprintf("invalid Spore text %q: %v", encodedText, err))
	}
	return string(decodedText)
}

func sporeDataUint(text string, base, bitSize int) uint64 {
	number, err := strconv.ParseUint(text, base, bitSize)
	if err != nil {
		panic(fmt.Sprintf("invalid Spore number %q: %v", text, err))
	}
	return number
}
