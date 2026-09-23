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
		var err error
		darksporeNames, darksporeUsages, err = decodeDarksporeRegistry(darksporeRegistryData)
		if err != nil {
			panic(err)
		}
	})
}
