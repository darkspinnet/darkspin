//go:build scenario

package gameplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/scenario"
	"github.com/darkspinnet/darkspin/server/snapshot"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type scenarioPeerFixture struct{ scenarioFixtures scenarioFixtureRecord }

// Producers replace the record and boundary slice. Published observations are
// immutable even while initialization operates on a detached peer copy.
type scenarioFixtureRecord struct {
	userID              uint64
	gameID              uint32
	peerGeneration      uint64
	transportGeneration uint64
	runSeed             uint64
	mapSeed             uint32
	prepareMask         uint32
	zoneGeneration      uint64
	boundaries          []snapshot.ScenarioBoundary
	mappings            []snapshot.ScenarioObject
	lifetimes           []ScenarioFixtureLifetimeEvidence
	isLifetimeComplete  bool
	lifetimeReason      string
}

type ScenarioFixtureLifetimeEvidence struct {
	ObjectID            uint32     `json:"object_id"`
	MarkerID            uint32     `json:"marker_id"`
	NounName            string     `json:"noun_name"`
	MarkerSetName       string     `json:"marker_set_name"`
	AdmissionGeneration uint64     `json:"admission_generation"`
	AdmittedAt          *time.Time `json:"admitted_at"`
	RolledBackAt        *time.Time `json:"rolled_back_at"`
	State               string     `json:"state"`
	IsCurrent           bool       `json:"is_current"`
}

type scenarioFixtureJournal struct {
	Boundaries []snapshot.ScenarioBoundary
	Lifetimes  []ScenarioFixtureLifetimeEvidence
}

type ScenarioFixtureEvidence struct {
	UserID              uint64
	GameID              uint32
	GameplaySessionID   string
	PeerGeneration      uint64
	TransportGeneration uint64
	ZoneGeneration      uint64
	Boundaries          []snapshot.ScenarioBoundary
	Lifetimes           []ScenarioFixtureLifetimeEvidence
	IsLifetimeComplete  bool
	LifetimeReason      string
}

type scenarioFixtureProvider interface {
	ScenarioFixtureCapture(context.Context, int64) (ScenarioFixtureEvidence, error)
}

func (e Lifecycle) ScenarioFixtureCapture(ctx context.Context, userID int64) (ScenarioFixtureEvidence, error) {
	if ctx == nil || userID <= 0 {
		return ScenarioFixtureEvidence{}, errors.New("fixture capture requires context and isolated user")
	}
	err := ctx.Err()
	if err != nil {
		return ScenarioFixtureEvidence{}, fmt.Errorf("fixtureContext: %w", err)
	}
	provider, isSupported := e.syncSnapshot.(scenarioFixtureProvider)
	if !isSupported {
		return ScenarioFixtureEvidence{}, errors.New("fixture capture provider unavailable")
	}
	evidence, err := provider.ScenarioFixtureCapture(ctx, userID)
	if err != nil {
		return ScenarioFixtureEvidence{}, fmt.Errorf("fixtureProvider: %w", err)
	}
	return evidence, nil
}

func (e *gameplaySessionRegistry) ScenarioFixtureCapture(ctx context.Context, userID int64) (ScenarioFixtureEvidence, error) {
	peerSession, observation, err := e.scenarioSession(ctx, userID)
	if err != nil {
		return ScenarioFixtureEvidence{}, fmt.Errorf("fixtureSession: %w", err)
	}
	if observation.Outcome != scenario.Passed {
		return ScenarioFixtureEvidence{}, fmt.Errorf("fixtureUnavailable: %s", observation.Detail)
	}
	record := peerSession.scenarioFixtures
	if !record.isCurrent(peerSession) || len(record.boundaries) == 0 {
		return ScenarioFixtureEvidence{}, errors.New("fixture selection has not been retained for this live peer")
	}
	mapSeed, prepareMask, isConfigured, err := peerSession.binding.ScenarioMapInputs()
	if err != nil {
		return ScenarioFixtureEvidence{}, fmt.Errorf("fixtureBinding: %w", err)
	}
	if !isConfigured || mapSeed != record.mapSeed || prepareMask != record.prepareMask {
		return ScenarioFixtureEvidence{}, errors.New("retained fixture journal does not belong to the current fresh scenario binding")
	}
	if record.zoneGeneration != 0 {
		if !peerSession.isScenarioFixtureZoneCurrent() {
			return ScenarioFixtureEvidence{}, errors.New("fixture capture belongs to a previous authoritative zone")
		}
	}
	// Deep-copy only the allowlisted snapshot DTOs. Callers attach evidence
	// references without gaining access to the retained journal's pointers.
	payload, err := json.Marshal(scenarioFixtureJournal{Boundaries: record.boundaries, Lifetimes: record.lifetimes})
	if err != nil {
		return ScenarioFixtureEvidence{}, fmt.Errorf("fixtureCloneMarshal: %w", err)
	}
	var journal scenarioFixtureJournal
	err = json.Unmarshal(payload, &journal)
	if err != nil {
		return ScenarioFixtureEvidence{}, fmt.Errorf("fixtureCloneDecode: %w", err)
	}
	err = ctx.Err()
	if err != nil {
		return ScenarioFixtureEvidence{}, fmt.Errorf("fixtureCloneContext: %w", err)
	}
	return ScenarioFixtureEvidence{UserID: record.userID, GameID: record.gameID,
		GameplaySessionID: observation.SessionID, PeerGeneration: record.peerGeneration,
		TransportGeneration: record.transportGeneration, ZoneGeneration: record.zoneGeneration,
		Boundaries: journal.Boundaries, Lifetimes: journal.Lifetimes,
		IsLifetimeComplete: record.isLifetimeComplete, LifetimeReason: record.lifetimeReason}, nil
}

func (e scenarioFixtureRecord) isCurrent(peerSession gameplayPeerSession) bool {
	return e.userID != 0 && e.userID == peerSession.binding.UserID &&
		e.gameID != 0 && e.gameID == peerSession.binding.GameID &&
		e.peerGeneration != 0 && e.peerGeneration == peerSession.generation &&
		e.transportGeneration != 0 && e.transportGeneration == peerSession.transportGeneration &&
		e.runSeed == peerSession.binding.RunSeed
}

func (e *gameplayPeerSession) recordScenarioFixtureSelection(director game.CampaignDirector) {
	if e == nil {
		return
	}
	mapSeed, prepareMask, isConfigured, err := e.binding.ScenarioMapInputs()
	if err != nil || !isConfigured || director.MapVariantSeed != mapSeed || prepareMask != 0 {
		return
	}
	if e.scenarioFixtures.isCurrent(*e) {
		return
	}
	startedAt := time.Now().UTC()
	peerGeneration := e.generation
	markers, deletedObjectIDs, err := director.MapDestructibles()
	boundary := snapshot.ScenarioBoundary{Name: "fixture_takeover", Stage: snapshot.ScenarioSelectedPlan,
		SessionGeneration: &peerGeneration, StartedAt: &startedAt,
		Completeness: "complete", Reason: "scope: selected takeover fixtures only; retained before server zone resolution; native observation and publication are unobserved"}
	isBeforeMutation := true
	isMutationObserved := false
	droppedRecordCount := uint64(0)
	boundary.IsBeforeMutation = &isBeforeMutation
	boundary.IsMutationObserved = &isMutationObserved
	boundary.DroppedRecordCount = &droppedRecordCount
	if err != nil || len(markers) != len(deletedObjectIDs) {
		boundary.Completeness = "unavailable"
		boundary.Reason = "selected fixture markers are unavailable"
	} else {
		for _, marker := range markers {
			boundary.Objects = append(boundary.Objects, scenarioFixtureMarker(director, marker, startedAt))
		}
	}
	completedAt := time.Now().UTC()
	boundary.CompletedAt = &completedAt
	e.scenarioFixtures = scenarioFixtureRecord{userID: e.binding.UserID, gameID: e.binding.GameID,
		peerGeneration: e.generation, transportGeneration: e.transportGeneration,
		runSeed: e.binding.RunSeed, mapSeed: mapSeed, prepareMask: prepareMask,
		boundaries: []snapshot.ScenarioBoundary{boundary}}
}

func scenarioFixtureMarker(director game.CampaignDirector, marker game.CampaignDirectorMarker, observedAt time.Time) snapshot.ScenarioObject {
	nounID := util.HashID(marker.NounName)
	position := [3]float32{marker.Position.X, marker.Position.Y, marker.Position.Z}
	rotation := [3]float32{marker.Rotation.X, marker.Rotation.Y, marker.Rotation.Z}
	object := snapshot.ScenarioObject{Kind: "fixture", NounName: &marker.NounName, NounID: &nounID,
		MarkerID: &marker.MarkerID, SetName: &marker.MarkerSetName,
		Position: &position, Rotation: &rotation, Scale: &marker.Scale,
		IsVisible: &marker.IsVisible, IsCollisionEnabled: &marker.IsCollisionEnabled,
		LifecycleState: "planned", MappingProvenance: "selected_plan", ObservedAt: &observedAt}
	for _, set := range director.MarkerSets {
		if set.Name == marker.MarkerSetName && set.Ordinal >= 0 {
			ordinal := uint32(set.Ordinal)
			object.SetOrdinal = &ordinal
			break
		}
	}
	return object
}

// This seam runs only after the ordinary audit proves selected marker,
// deletion identity, fixture runtime ID, noun and transform correspondence.
func (e *gameplayPeerSession) recordScenarioFixtureTakeover(
	director game.CampaignDirector, markers []game.CampaignDirectorMarker,
	deletedObjectIDs []uint32, plans []zonenpc.SpawnPlan,
) {
	if e == nil || !e.scenarioFixtures.isCurrent(*e) || len(markers) != len(plans) || len(markers) != len(deletedObjectIDs) {
		return
	}
	mappings := make([]snapshot.ScenarioObject, 0, len(plans))
	observedAt := time.Now().UTC()
	for index, marker := range markers {
		plan := plans[index]
		if deletedObjectIDs[index] != marker.MarkerID || plan.LocusID != marker.MarkerID ||
			plan.ObjectID == 0 || !plan.IsFixture || plan.MarkerSetName != marker.MarkerSetName ||
			plan.NounName != marker.NounName || plan.Position != marker.Position ||
			plan.Rotation != marker.Rotation || plan.PlacementScale != marker.Scale {
			return
		}
		object := scenarioFixtureMarker(director, marker, observedAt)
		object.NetworkObjectID = &plan.ObjectID
		object.MappingProvenance = "fixture_takeover"
		object.IsVisible = nil
		object.IsCollisionEnabled = nil
		mappings = append(mappings, object)
	}
	e.scenarioFixtures.mappings = mappings
}

func (e *gameplayPeerSession) recordScenarioFixtureAdmission() {
	if e == nil || !e.scenarioFixtures.isCurrent(*e) || e.zone == nil || e.scenarioFixtures.zoneGeneration != 0 {
		return
	}
	zoneSnapshot := e.zone.Snapshot()
	if zoneSnapshot.ID != uint64(e.binding.GameID) || zoneSnapshot.Generation == 0 || zoneSnapshot.IsRestored {
		return
	}
	isMemberFound := false
	for _, member := range zoneSnapshot.Members {
		if member.UserID == e.binding.UserID && member.PeerGeneration == e.generation {
			isMemberFound = true
			break
		}
	}
	if !isMemberFound {
		return
	}
	e.scenarioFixtures.zoneGeneration = zoneSnapshot.Generation
	boundaries := make([]snapshot.ScenarioBoundary, len(e.scenarioFixtures.boundaries))
	copy(boundaries, e.scenarioFixtures.boundaries)
	for index := range boundaries {
		generation := zoneSnapshot.Generation
		boundaries[index].ZoneGeneration = &generation
		boundaries[index].Reason += "; authoritative zone epoch associated later through the same admitted server binding"
	}
	e.scenarioFixtures.boundaries = boundaries
	e.appendScenarioFixtureState("zone_admitted")
}

func (e *gameplayPeerSession) recordScenarioFixtureCommit() {
	if e == nil || !e.scenarioFixtures.isCurrent(*e) || !e.dungeonSetup.IsCommitted() || e.scenarioFixtures.zoneGeneration == 0 {
		return
	}
	for _, boundary := range e.scenarioFixtures.boundaries {
		if boundary.Name == "dungeon_committed" {
			return
		}
	}
	e.appendScenarioFixtureState("fixture_takeover")
	e.appendScenarioFixtureState("dungeon_committed")
}

func (e *gameplayPeerSession) appendScenarioFixtureState(name string) {
	startedAt := time.Now().UTC()
	zoneGeneration := e.scenarioFixtures.zoneGeneration
	peerGeneration := e.generation
	if !e.isScenarioFixtureZoneCurrent() {
		return
	}
	boundary := snapshot.ScenarioBoundary{Name: name, Stage: snapshot.ScenarioServerState,
		SessionGeneration: &peerGeneration, ZoneGeneration: &zoneGeneration, StartedAt: &startedAt,
		Completeness: "partial", Reason: "scope: retained selected fixture mapping and authoritative NPC admission; stored registration episodes and rollback history cover this fixture ledger only; client lifetime, removal/publication, visibility, collision and mutation during collection remain unknown"}
	droppedRecordCount := uint64(0)
	boundary.DroppedRecordCount = &droppedRecordCount
	isBeforeMutation := false
	boundary.IsBeforeMutation = &isBeforeMutation
	objectIDs := make([]uint32, 0, len(e.scenarioFixtures.mappings))
	for _, mapping := range e.scenarioFixtures.mappings {
		if mapping.NetworkObjectID != nil {
			objectIDs = append(objectIDs, *mapping.NetworkObjectID)
		}
	}
	lifetimes := e.zone.NPCs().ScenarioFixtureLifetimes(zonenpc.ScenarioFixtureLifetimeRequest{ObjectIDs: objectIDs})
	if !e.isScenarioFixtureZoneCurrent() {
		return
	}
	e.scenarioFixtures.lifetimes = scenarioFixtureLifetimeProofs(lifetimes.Records)
	e.scenarioFixtures.isLifetimeComplete = lifetimes.IsComplete
	e.scenarioFixtures.lifetimeReason = lifetimes.Reason
	lifetimesByID := make(map[uint32]zonenpc.ScenarioFixtureLifetimeRecord, len(lifetimes.Records))
	for _, lifetime := range lifetimes.Records {
		if lifetime.IsCurrent {
			lifetimesByID[lifetime.ObjectID] = lifetime
		}
	}
	for _, mapping := range e.scenarioFixtures.mappings {
		if mapping.NetworkObjectID == nil || mapping.MarkerID == nil ||
			mapping.NounName == nil || mapping.NounID == nil || mapping.SetName == nil {
			boundary.Reason += "; selected fixture identity unavailable"
			continue
		}
		lifetime, isFound := lifetimesByID[*mapping.NetworkObjectID]
		if !isFound || lifetime.MarkerID != *mapping.MarkerID ||
			lifetime.NounName != *mapping.NounName || util.HashID(lifetime.NounName) != *mapping.NounID ||
			lifetime.MarkerSetName != *mapping.SetName {
			boundary.Reason += "; selected fixture current episode unavailable or changed"
			continue
		}
		object := mapping
		position := [3]float32{lifetime.Position.X, lifetime.Position.Y, lifetime.Position.Z}
		rotation := [3]float32{lifetime.Rotation.X, lifetime.Rotation.Y, lifetime.Rotation.Z}
		object.Position = &position
		object.Rotation = &rotation
		object.Scale = &lifetime.PlacementScale
		nounID := util.HashID(lifetime.NounName)
		object.NounID = &nounID
		object.NounName = &lifetime.NounName
		object.ObservedAt = &startedAt
		if lifetimes.IsComplete && lifetime.AdmissionGeneration != 0 && lifetime.RolledBackAt == nil {
			generation := lifetime.AdmissionGeneration
			object.LifecycleGeneration = &generation
		}
		object.LifecycleState = "admitted"
		if lifetime.IsDefeated {
			// Defeat does not establish that a deletion was published.
			object.LifecycleState = "defeated"
		}
		boundary.Objects = append(boundary.Objects, object)
	}
	if !lifetimes.IsComplete {
		boundary.Reason += "; fixture lifetime ledger incomplete: " + lifetimes.Reason
	}
	completedAt := time.Now().UTC()
	boundary.CompletedAt = &completedAt
	boundary.Reason += fmt.Sprintf("; observed mapped fixtures=%d planned mappings=%d", len(boundary.Objects), len(e.scenarioFixtures.mappings))
	boundaries := make([]snapshot.ScenarioBoundary, len(e.scenarioFixtures.boundaries), len(e.scenarioFixtures.boundaries)+1)
	copy(boundaries, e.scenarioFixtures.boundaries)
	e.scenarioFixtures.boundaries = append(boundaries, boundary)
}

func scenarioFixtureLifetimeProofs(records []zonenpc.ScenarioFixtureLifetimeRecord) []ScenarioFixtureLifetimeEvidence {
	lifetimes := make([]ScenarioFixtureLifetimeEvidence, 0, len(records))
	for _, record := range records {
		lifetime := ScenarioFixtureLifetimeEvidence{
			ObjectID: record.ObjectID, MarkerID: record.MarkerID,
			NounName: record.NounName, MarkerSetName: record.MarkerSetName,
			AdmissionGeneration: record.AdmissionGeneration, State: record.State,
			IsCurrent: record.IsCurrent,
		}
		if record.AdmissionGeneration != 0 && !record.AdmittedAt.IsZero() {
			admittedAt := record.AdmittedAt
			lifetime.AdmittedAt = &admittedAt
		}
		if record.RolledBackAt != nil {
			rolledBackAt := *record.RolledBackAt
			lifetime.RolledBackAt = &rolledBackAt
		}
		lifetimes = append(lifetimes, lifetime)
	}
	return lifetimes
}

func (e gameplayPeerSession) isScenarioFixtureZoneCurrent() bool {
	if e.zone == nil || e.scenarioFixtures.zoneGeneration == 0 {
		return false
	}
	zoneSnapshot := e.zone.Snapshot()
	if zoneSnapshot.ID != uint64(e.binding.GameID) || zoneSnapshot.Generation != e.scenarioFixtures.zoneGeneration || zoneSnapshot.IsRestored {
		return false
	}
	for _, member := range zoneSnapshot.Members {
		if member.UserID == e.binding.UserID && member.PeerGeneration == e.generation {
			return true
		}
	}
	return false
}
