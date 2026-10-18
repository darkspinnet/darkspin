//go:build scenario

package npc

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
)

const scenarioFixtureRecordLimit = 4096
const scenarioFixtureRequestLimit = 256

type scenarioFixtureLedger struct {
	admissionGeneration uint64
	records             []ScenarioFixtureLifetimeRecord
	recordIndexesByID   map[uint32]int
	isIncomplete        bool
	reason              string
}

type ScenarioFixtureLifetimeRequest struct {
	ObjectIDs []uint32
}

// Records describe server registration and rollback, never client publication.
type ScenarioFixtureLifetimeRecord struct {
	ObjectID            uint32
	MarkerID            uint32
	NounName            string
	MarkerSetName       string
	AdmissionGeneration uint64
	AdmittedAt          time.Time
	RolledBackAt        *time.Time
	State               string
	IsCurrent           bool
	IsDefeated          bool
	Position            game.Vec3
	Rotation            game.Vec3
	PlacementScale      float32
}

type ScenarioFixtureLifetimeSnapshot struct {
	Records    []ScenarioFixtureLifetimeRecord
	IsComplete bool
	Reason     string
}

// Caller already holds the NPC write lock and has stored this actual actor.
func (e *Session) recordScenarioFixtureAdmission(plan SpawnPlan) {
	if !plan.IsFixture || e.isIncomplete {
		return
	}
	npc, isFound := e.npcs[plan.ObjectID]
	if !isFound || !scenarioFixturePlanMatches(plan, npc.Plan) || plan.ObjectID == 0 ||
		plan.LocusID == 0 || plan.NounName == "" || plan.MarkerSetName == "" {
		e.markScenarioFixtureIncomplete("fixture registration identity mismatch")
		return
	}
	if len(e.records) >= scenarioFixtureRecordLimit {
		e.markScenarioFixtureIncomplete("fixture admission record capacity reached")
		return
	}
	if e.admissionGeneration == ^uint64(0) {
		e.markScenarioFixtureIncomplete("fixture admission generation exhausted")
		return
	}
	previousIndex, isRecorded := e.recordIndexesByID[plan.ObjectID]
	if isRecorded && e.records[previousIndex].State != "rolled_back" {
		e.markScenarioFixtureIncomplete("fixture admission replaced an unretired episode")
		return
	}
	if e.recordIndexesByID == nil {
		e.recordIndexesByID = make(map[uint32]int)
	}
	e.admissionGeneration++
	record := ScenarioFixtureLifetimeRecord{
		ObjectID: plan.ObjectID, MarkerID: plan.LocusID,
		NounName: plan.NounName, MarkerSetName: plan.MarkerSetName,
		AdmissionGeneration: e.admissionGeneration, AdmittedAt: time.Now().UTC(),
		State: "admitted", Position: plan.Position, Rotation: plan.Rotation,
		PlacementScale: plan.PlacementScale,
	}
	e.recordIndexesByID[plan.ObjectID] = len(e.records)
	e.records = append(e.records, record)
}

// Caller already validated the complete rollback batch under the write lock.
func (e *Session) recordScenarioFixtureRollback(plan SpawnPlan) {
	npc, isFound := e.npcs[plan.ObjectID]
	if !isFound || !npc.Plan.IsFixture {
		if plan.IsFixture {
			e.markScenarioFixtureIncomplete("fixture rollback membership mismatch")
		}
		return
	}
	index, isRecorded := e.recordIndexesByID[plan.ObjectID]
	if !isRecorded {
		e.markScenarioFixtureIncomplete("fixture rollback has no admission episode")
		return
	}
	record := e.records[index]
	if record.State != "admitted" || !scenarioFixtureRecordMatches(record, npc.Plan) {
		e.markScenarioFixtureIncomplete("fixture rollback identity mismatch")
		return
	}
	if !scenarioFixturePlanMatches(plan, npc.Plan) {
		e.markScenarioFixtureIncomplete("fixture rollback caller identity mismatch")
	}
	rolledBackAt := time.Now().UTC()
	record.RolledBackAt = &rolledBackAt
	record.State = "rolled_back"
	e.records[index] = record
}

func (e *Session) markScenarioFixtureIncomplete(reason string) {
	if e.isIncomplete {
		return
	}
	e.isIncomplete = true
	e.reason = reason
}

// ScenarioFixtureLifetimes reads episode identity and current membership under
// one lock. It performs bounded memory-only work and returns owned records.
func (e *Session) ScenarioFixtureLifetimes(req ScenarioFixtureLifetimeRequest) ScenarioFixtureLifetimeSnapshot {
	result := ScenarioFixtureLifetimeSnapshot{Records: []ScenarioFixtureLifetimeRecord{}}
	if e == nil {
		result.Reason = "fixture session unavailable"
		return result
	}
	if len(req.ObjectIDs) > scenarioFixtureRequestLimit {
		result.Reason = "fixture requested ID capacity exceeded"
		return result
	}
	objectIDs := make(map[uint32]bool, len(req.ObjectIDs))
	for _, objectID := range req.ObjectIDs {
		if objectID == 0 || objectIDs[objectID] {
			result.Reason = "fixture requested IDs are zero or duplicated"
			return result
		}
		objectIDs[objectID] = true
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	result.IsComplete = !e.isIncomplete
	result.Reason = e.reason
	for index, record := range e.records {
		if !objectIDs[record.ObjectID] {
			continue
		}
		if record.RolledBackAt != nil {
			rolledBackAt := *record.RolledBackAt
			record.RolledBackAt = &rolledBackAt
		}
		currentIndex, isCurrentEpisode := e.recordIndexesByID[record.ObjectID]
		if isCurrentEpisode && currentIndex == index {
			objectIDs[record.ObjectID] = false
			npc, isFound := e.npcs[record.ObjectID]
			switch {
			case record.State == "rolled_back":
				if isFound {
					result.IsComplete = false
					if result.Reason == "" {
						result.Reason = "rolled-back fixture has unrecorded current membership"
					}
					if npc.Plan.IsFixture {
						result.Records = append(result.Records, scenarioFixtureCurrentMembership(npc))
					}
				}
			case record.State != "admitted" || !isFound || !scenarioFixtureRecordMatches(record, npc.Plan):
				result.IsComplete = false
				if result.Reason == "" {
					result.Reason = "fixture episode current membership identity mismatch"
				}
				if isFound && npc.Plan.IsFixture {
					result.Records = append(result.Records, scenarioFixtureCurrentMembership(npc))
				}
			default:
				record.IsCurrent = true
				record.IsDefeated = npc.IsDefeated
				record.Position = npc.Plan.Position
				record.Rotation = npc.Plan.Rotation
				record.PlacementScale = npc.Plan.PlacementScale
				if npc.IsDefeated {
					record.State = "defeated"
				}
			}
		}
		result.Records = append(result.Records, record)
	}
	for _, objectID := range req.ObjectIDs {
		if !objectIDs[objectID] {
			continue
		}
		result.IsComplete = false
		if result.Reason == "" {
			result.Reason = fmt.Sprintf("fixture admission episode unavailable for object %d", objectID)
		}
		npc, isFound := e.npcs[objectID]
		if isFound && npc.Plan.IsFixture {
			result.Records = append(result.Records, scenarioFixtureCurrentMembership(npc))
		}
	}
	return result
}

// Missing history does not erase known membership or manufacture an episode.
func scenarioFixtureCurrentMembership(npc Snapshot) ScenarioFixtureLifetimeRecord {
	record := ScenarioFixtureLifetimeRecord{
		ObjectID: npc.Plan.ObjectID, MarkerID: npc.Plan.LocusID,
		NounName: npc.Plan.NounName, MarkerSetName: npc.Plan.MarkerSetName,
		State: "admitted", IsCurrent: true, IsDefeated: npc.IsDefeated,
		Position: npc.Plan.Position, Rotation: npc.Plan.Rotation,
		PlacementScale: npc.Plan.PlacementScale,
	}
	if npc.IsDefeated {
		record.State = "defeated"
	}
	return record
}

func scenarioFixtureRecordMatches(record ScenarioFixtureLifetimeRecord, plan SpawnPlan) bool {
	return plan.IsFixture && record.ObjectID == plan.ObjectID &&
		record.MarkerID == plan.LocusID && record.NounName == plan.NounName &&
		record.MarkerSetName == plan.MarkerSetName
}

func scenarioFixturePlanMatches(plan, current SpawnPlan) bool {
	return plan.IsFixture && current.IsFixture && plan.ObjectID == current.ObjectID &&
		plan.LocusID == current.LocusID && plan.NounName == current.NounName &&
		plan.MarkerSetName == current.MarkerSetName
}
