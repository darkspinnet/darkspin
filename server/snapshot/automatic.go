package snapshot

import (
	"sort"
	"time"
)

const automaticIncidentLifetime = time.Minute

type automaticCategory struct {
	remote     string
	generation uint64
	category   string
}

func (e anomaly) automaticCategory() automaticCategory {
	return automaticCategory{remote: e.Actor.Remote, generation: e.PeerGeneration, category: e.Category}
}

func isAutomaticPeerActive(provider TransportDiagnosticsProvider, category automaticCategory, now time.Time) bool {
	if provider == nil {
		return true
	}
	peer, isFound := provider.SnapshotTransportDiagnostics(category.remote, category.generation, now)
	return isFound && peer.IsConnected
}

func (e *Service) pruneAutomaticCategories() {
	e.mu.Lock()
	provider := e.transportProvider
	categories := make([]automaticCategory, 0, len(e.capturedCategories))
	for category := range e.capturedCategories {
		categories = append(categories, category)
	}
	e.mu.Unlock()
	now := time.Now().UTC()
	for _, category := range categories {
		if isAutomaticPeerActive(provider, category, now) {
			continue
		}
		e.mu.Lock()
		delete(e.capturedCategories, category)
		e.mu.Unlock()
	}
}

func (e *Service) capturedAutomaticRecordsLocked() []automaticIncidentRecord {
	records := make([]automaticIncidentRecord, 0, len(e.capturedCategories))
	for _, captured := range e.capturedCategories {
		records = append(records, automaticIncidentRecordFrom(captured))
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].LastObservedAt.Equal(records[j].LastObservedAt) {
			return records[i].Fingerprint < records[j].Fingerprint
		}
		return records[i].LastObservedAt.After(records[j].LastObservedAt)
	})
	return records[:min(len(records), maximumAutomaticIncidentRecordCount)]
}
