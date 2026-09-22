package audio

import "sync"

// SampleUsage identifies a shipped content resource containing an exact
// authored audio-event name. It is provenance for search and reconstruction,
// not serialized game data.
type SampleUsage struct {
	PackageName      string
	Context          string
	ReferenceKind    string
	ByteOffset       int
	Ordinal          int
	ResourceType     uint32
	ResourceGroup    uint32
	ResourceInstance uint64
}

type darksporeNameRecord struct {
	eventInstance uint32
	name          string
	usage         SampleUsage
}

type darksporeUsageRecord struct {
	eventInstance uint32
	usage         SampleUsage
}

var (
	darksporeDataOnce sync.Once
	darksporeNames    map[uint32]string
	darksporeUsages   map[uint32][]SampleUsage
)

// DarksporeNames returns audio-event identities recovered by following exact
// hashed names embedded in shipped content resources.
func DarksporeNames() map[uint32]string {
	loadDarksporeData()
	names := make(map[uint32]string, len(darksporeNames))
	for eventInstance, name := range darksporeNames {
		names[eventInstance] = name
	}
	return names
}

// DarksporeUsages returns the content resources that supplied each recovered
// audio-event identity.
func DarksporeUsages() map[uint32][]SampleUsage {
	loadDarksporeData()
	usages := make(map[uint32][]SampleUsage, len(darksporeUsages))
	for eventInstance, eventUsages := range darksporeUsages {
		usages[eventInstance] = append([]SampleUsage(nil), eventUsages...)
	}
	return usages
}

func loadDarksporeData() {
	darksporeDataOnce.Do(func() {
		darksporeNames = make(map[uint32]string)
		darksporeUsages = make(map[uint32][]SampleUsage)
		for _, record := range darksporeNameRecords {
			if darksporeNames[record.eventInstance] == "" {
				darksporeNames[record.eventInstance] = record.name
			}
			darksporeUsages[record.eventInstance] = append(darksporeUsages[record.eventInstance], record.usage)
		}
		for eventInstance, name := range darksporeReferenceNames {
			if darksporeNames[eventInstance] == "" {
				darksporeNames[eventInstance] = name
			}
		}
		for _, record := range darksporeReferenceUsageRecords {
			darksporeUsages[record.eventInstance] = append(darksporeUsages[record.eventInstance], record.usage)
		}
	})
}
