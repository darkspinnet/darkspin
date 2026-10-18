package boss

import (
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

// AdmissionAudit describes the selected layout and rank-selected encounter
// before any runtime trigger is accepted or object ID is reserved.
type AdmissionAudit struct {
	MarkerSetName    string
	MarkerSetOrdinal int
	TriggerMarkerID  uint32
	BossMarkerID     uint32
	EventName        string
	CallbackName     string
	LeaderNoun       string
	ListenerCount    int
	PlanCount        int
	IsLeaderDeferred bool
	IsFinalBoss      bool
	IsOnceOnly       bool
	Delay            time.Duration
}

func InspectSelectedAdmission(
	director game.CampaignDirector, chainLevelIndex uint32,
) (AdmissionAudit, error) {
	if director.Level == "" || chainLevelIndex == 0 {
		return AdmissionAudit{}, fmt.Errorf("boss audit input invalid")
	}
	if strings.EqualFold(director.Level, game.InitialChainLevel) {
		publication, err := InitialDeveloperPublication(director)
		if err != nil {
			return AdmissionAudit{}, fmt.Errorf("initialBossAuditTrigger: %w", err)
		}
		plans, _, err := PlanInitialEncounter(
			director, publication, 1, 1, chainLevelIndex,
		)
		if err != nil {
			return AdmissionAudit{}, fmt.Errorf("initialBossAuditPlan: %w", err)
		}
		if len(plans) == 0 {
			return AdmissionAudit{}, fmt.Errorf("initialBossAuditPlan: empty")
		}
		audit := AdmissionAudit{
			MarkerSetName:    publication.MarkerSetName,
			MarkerSetOrdinal: publication.MarkerSetOrdinal,
			TriggerMarkerID:  publication.TriggerMarkerID,
			BossMarkerID:     plans[0].LocusID,
			EventName:        publication.EventName, CallbackName: publication.CallbackName,
			LeaderNoun: plans[0].NounName, ListenerCount: len(publication.Listeners),
			PlanCount: len(plans), IsLeaderDeferred: true,
			IsFinalBoss: IsFinalBossNoun(plans[0].NounName),
		}
		for _, markerSet := range director.MarkerSets {
			if markerSet.Ordinal != publication.MarkerSetOrdinal ||
				!strings.EqualFold(markerSet.Name, publication.MarkerSetName) {
				continue
			}
			for _, trigger := range markerSet.Triggers {
				if trigger.MarkerID != publication.TriggerMarkerID {
					continue
				}
				for _, event := range trigger.Events {
					if event.CallbackName != publication.CallbackName {
						continue
					}
					audit.IsOnceOnly = event.IsTriggerOnceOnly
					if trigger.SpawnTrigger != nil && trigger.SpawnTrigger.TriggerVolume != nil {
						audit.Delay = time.Duration(
							float64(trigger.SpawnTrigger.TriggerVolume.TimeToActivate) * float64(time.Second),
						)
					}
					return audit, nil
				}
			}
		}
		return audit, nil
	}
	publication, err := DeveloperPublication(director)
	if err != nil {
		return AdmissionAudit{}, fmt.Errorf("namedBossAuditTrigger: %w", err)
	}
	plannedPublication, plans, _, err := PlanNamedEncounter(
		director, publication, 1, 1, chainLevelIndex,
	)
	if err != nil {
		return AdmissionAudit{}, fmt.Errorf("namedBossAuditPlan: %w", err)
	}
	if len(plans) == 0 {
		return AdmissionAudit{}, fmt.Errorf("namedBossAuditPlan: empty")
	}
	audit := AdmissionAudit{
		MarkerSetName:    publication.MarkerSetName,
		MarkerSetOrdinal: publication.MarkerSetOrdinal,
		TriggerMarkerID:  publication.SourceObjectID,
		BossMarkerID:     plannedPublication.TriggerMarkerID,
		EventName:        publication.EventName, CallbackName: plannedPublication.CallbackName,
		LeaderNoun: plans[0].NounName, ListenerCount: len(publication.Listeners),
		PlanCount: len(plans), IsLeaderDeferred: plans[0].IsCaptain && len(plans) > 1,
		IsFinalBoss: IsFinalBossNoun(plans[0].NounName),
	}
	for _, markerSet := range director.MarkerSets {
		if markerSet.Ordinal != audit.MarkerSetOrdinal ||
			!strings.EqualFold(markerSet.Name, audit.MarkerSetName) {
			continue
		}
		for _, trigger := range markerSet.Triggers {
			if trigger.MarkerID != audit.TriggerMarkerID {
				continue
			}
			for _, event := range trigger.Events {
				if namedTriggerEventName(trigger, event) != audit.EventName ||
					!IsNamedCallback(event.CallbackName) {
					continue
				}
				audit.IsOnceOnly = event.IsTriggerOnceOnly
				if trigger.SpawnTrigger != nil && trigger.SpawnTrigger.TriggerVolume != nil {
					audit.Delay = time.Duration(
						float64(trigger.SpawnTrigger.TriggerVolume.TimeToActivate) * float64(time.Second),
					)
				}
				return audit, nil
			}
		}
	}
	return AdmissionAudit{}, fmt.Errorf(
		"namedBossAuditSource: set=%q marker=%d event=%q unavailable",
		audit.MarkerSetName, audit.TriggerMarkerID, audit.EventName,
	)
}
