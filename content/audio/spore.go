package audio

import "sync"

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
		var err error
		sporeNames, sporeAliases, sporeUsages, err = decodeSporeRegistry(sporeRegistryData)
		if err != nil {
			panic(err)
		}
	})
}
