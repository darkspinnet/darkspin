package horde

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const InterWaveDelay = 1500 * time.Millisecond
const gateContactRadius = float32(2)

type Phase uint8

const (
	PhaseDormant Phase = iota
	PhaseActiveWave
	PhaseInterWave
	PhaseComplete
)

type Transition struct {
	MarkerSetName      string
	NextWaveActorCount int
	NextWaveDelay      time.Duration
	IsComplete         bool
	Completion         Completion
}

type Completion struct {
	MarkerSetName   string
	EventName       string
	TriggerMarkerID uint32
}

type Snapshot struct {
	MarkerSetName string
	Phase         Phase
	WaveOrdinal   int
	LiveObjectIDs []uint32
	IsGateActive  bool
	Completion    Completion
}

type GateContact struct {
	MarkerSetName  string
	MarkerID       uint32
	Position       game.Vec3
	ReturnPosition game.Vec3
}

type encounter struct {
	publication   game.CampaignDirectorPublication
	phase         Phase
	waveOrdinal   int
	liveObjectIDs map[uint32]bool
	isGateActive  bool
	completion    Completion
}

type Session struct {
	mu                    sync.RWMutex
	encountersByMarkerSet map[string]*encounter
	wavesByMarkerSet      map[string]*WaveSequence
}

func NewSession() *Session {
	return &Session{
		encountersByMarkerSet: make(map[string]*encounter),
		wavesByMarkerSet:      make(map[string]*WaveSequence),
	}
}

func (s *Session) RegisterSequence(
	markerSetName string, waveCount int, completedWave int,
) error {
	if s == nil {
		return errors.New("horde sequence: nil session")
	}
	key := strings.ToLower(markerSetName)
	if key == "" || waveCount <= 0 {
		return errors.New("horde sequence: invalid registration")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.wavesByMarkerSet[key]
	if current != nil &&
		current.waveCount == waveCount &&
		current.Current() == completedWave {
		return nil
	}
	if current != nil {
		return errors.New("horde sequence: already registered")
	}
	s.wavesByMarkerSet[key] = NewWaveSequence(waveCount, completedWave)
	return nil
}

func (s *Session) AdvanceSequence(
	markerSetName string, isCurrentWaveClear bool,
) (int, bool) {
	if s == nil {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sequence := s.wavesByMarkerSet[strings.ToLower(markerSetName)]
	return sequence.Next(isCurrentWaveClear)
}

func (s *Session) SequenceWave(markerSetName string) (int, bool) {
	if s == nil {
		return 0, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sequence := s.wavesByMarkerSet[strings.ToLower(markerSetName)]
	if sequence == nil {
		return 0, false
	}
	return sequence.Current(), true
}

func (s *Session) IsSequenceComplete(
	markerSetName string, isCurrentWaveClear bool,
) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sequence := s.wavesByMarkerSet[strings.ToLower(markerSetName)]
	return sequence.IsComplete(isCurrentWaveClear)
}

func ListenerCount(publication game.CampaignDirectorPublication) (int, bool) {
	if !IsTrigger(publication) || len(publication.Listeners) == 0 {
		return 0, false
	}
	listenerCount := 0
	for _, listener := range publication.Listeners {
		if listener.CallbackName != "HordeSpawner_Register" {
			continue
		}
		if listener.MarkerSetOrdinal != publication.MarkerSetOrdinal ||
			!strings.EqualFold(listener.MarkerSetName, publication.MarkerSetName) ||
			!listener.IsSpawnKindKnown || listener.SpawnKind != 5 ||
			!strings.EqualFold(listener.PoolKind, "agent") || listener.MarkerID == 0 ||
			!isFinitePosition(listener.Position) {
			return 0, false
		}
		listenerCount++
	}
	return listenerCount, listenerCount != 0
}

func IsTrigger(publication game.CampaignDirectorPublication) bool {
	return publication.MarkerSetName != "" && publication.TriggerMarkerID != 0 &&
		publication.EventName == "horde triggered" &&
		publication.CallbackName == "HordeTrigger_OnEnterPlayer"
}

func (s *Session) AdmitFirstWave(
	publication game.CampaignDirectorPublication, plans []zonenpc.SpawnPlan,
) error {
	if s == nil {
		return errors.New("horde first wave: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.canAdmitFirstWave(publication, plans)
	if err != nil {
		return fmt.Errorf("hordeFirstCheck: %w", err)
	}
	key := strings.ToLower(publication.MarkerSetName)
	liveObjectIDs := make(map[uint32]bool, len(plans))
	for _, plan := range plans {
		liveObjectIDs[plan.ObjectID] = true
	}
	s.encountersByMarkerSet[key] = &encounter{
		publication: clonePublication(publication), phase: PhaseActiveWave,
		waveOrdinal: 1, liveObjectIDs: liveObjectIDs, isGateActive: true,
	}
	return nil
}

func (s *Session) CanAdmitFirstWave(
	publication game.CampaignDirectorPublication, plans []zonenpc.SpawnPlan,
) error {
	if s == nil {
		return errors.New("horde first wave: nil session")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.canAdmitFirstWave(publication, plans)
}

func (s *Session) canAdmitFirstWave(
	publication game.CampaignDirectorPublication,
	plans []zonenpc.SpawnPlan,
) error {
	_, isHorde := ListenerCount(publication)
	key := strings.ToLower(publication.MarkerSetName)
	if !isHorde || key == "" || s.encountersByMarkerSet[key] != nil || len(plans) == 0 {
		return errors.New("horde first wave: invalid admission")
	}
	seenObjectIDs := make(map[uint32]bool, len(plans))
	for index, plan := range plans {
		if !strings.EqualFold(plan.MarkerSetName, publication.MarkerSetName) ||
			plan.LocusID != publication.TriggerMarkerID || plan.ObjectID == 0 || seenObjectIDs[plan.ObjectID] {
			return fmt.Errorf("horde first wave plan[%d]: invalid", index)
		}
		seenObjectIDs[plan.ObjectID] = true
	}
	return nil
}

func (s *Session) Defeat(
	result zonenpc.DamageResult,
) (Transition, error) {
	if s == nil || !result.IsDefeated || result.ObjectID == 0 || result.MarkerSetName == "" {
		return Transition{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(result.MarkerSetName)
	encounter := s.encountersByMarkerSet[key]
	if encounter == nil {
		return Transition{}, nil
	}
	if encounter.phase != PhaseActiveWave || !encounter.liveObjectIDs[result.ObjectID] {
		return Transition{}, fmt.Errorf("horde defeat: unexpected actor %d", result.ObjectID)
	}
	delete(encounter.liveObjectIDs, result.ObjectID)
	if len(encounter.liveObjectIDs) != 0 {
		return Transition{}, nil
	}
	transition := Transition{MarkerSetName: encounter.publication.MarkerSetName}
	if strings.EqualFold(encounter.publication.MarkerSetName, "zelems_1_Ai_Horde_2.Markerset") &&
		encounter.waveOrdinal == 1 {
		encounter.phase = PhaseInterWave
		transition.NextWaveActorCount = 3
		transition.NextWaveDelay = InterWaveDelay
		return transition, nil
	}
	encounter.phase = PhaseComplete
	encounter.isGateActive = false
	encounter.completion = Completion{
		MarkerSetName: encounter.publication.MarkerSetName,
		EventName:     "horde complete", TriggerMarkerID: encounter.publication.TriggerMarkerID,
	}
	transition.IsComplete = true
	transition.Completion = encounter.completion
	return transition, nil
}

func (s *Session) Revive(snapshot zonenpc.Snapshot) error {
	if s == nil || snapshot.Plan.ObjectID == 0 ||
		snapshot.Plan.MarkerSetName == "" || snapshot.IsDefeated {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	encounter := s.encountersByMarkerSet[strings.ToLower(snapshot.Plan.MarkerSetName)]
	if encounter == nil {
		return nil
	}
	if encounter.phase != PhaseActiveWave ||
		encounter.liveObjectIDs[snapshot.Plan.ObjectID] {
		return fmt.Errorf("horde revive: unexpected actor %d", snapshot.Plan.ObjectID)
	}
	encounter.liveObjectIDs[snapshot.Plan.ObjectID] = true
	return nil
}

func (s *Session) AdmitNextWave(
	markerSetName string, plans []zonenpc.SpawnPlan,
) error {
	if s == nil {
		return errors.New("horde next wave: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.canAdmitNextWave(markerSetName, plans)
	if err != nil {
		return fmt.Errorf("hordeNextCheck: %w", err)
	}
	encounter := s.encountersByMarkerSet[strings.ToLower(markerSetName)]
	liveObjectIDs := make(map[uint32]bool, len(plans))
	for _, plan := range plans {
		liveObjectIDs[plan.ObjectID] = true
	}
	encounter.waveOrdinal = 2
	encounter.phase = PhaseActiveWave
	encounter.liveObjectIDs = liveObjectIDs
	return nil
}

func (s *Session) CanAdmitNextWave(
	markerSetName string, plans []zonenpc.SpawnPlan,
) error {
	if s == nil || markerSetName == "" || len(plans) == 0 {
		return errors.New("horde next wave: invalid admission")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.canAdmitNextWave(markerSetName, plans)
}

func (s *Session) canAdmitNextWave(
	markerSetName string, plans []zonenpc.SpawnPlan,
) error {
	if markerSetName == "" || len(plans) == 0 {
		return errors.New("horde next wave: invalid admission")
	}
	encounter := s.encountersByMarkerSet[strings.ToLower(markerSetName)]
	if encounter == nil || encounter.phase != PhaseInterWave || encounter.waveOrdinal != 1 {
		return errors.New("horde next wave: invalid state")
	}
	seenObjectIDs := make(map[uint32]bool, len(plans))
	for index, plan := range plans {
		if !strings.EqualFold(plan.MarkerSetName, markerSetName) || plan.ObjectID == 0 ||
			seenObjectIDs[plan.ObjectID] {
			return fmt.Errorf("horde next wave plan[%d]: invalid", index)
		}
		seenObjectIDs[plan.ObjectID] = true
	}
	return nil
}

func (s *Session) Publication(markerSetName string) (game.CampaignDirectorPublication, bool) {
	if s == nil {
		return game.CampaignDirectorPublication{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	encounter := s.encountersByMarkerSet[strings.ToLower(markerSetName)]
	if encounter == nil {
		return game.CampaignDirectorPublication{}, false
	}
	return clonePublication(encounter.publication), true
}

func (s *Session) Snapshot(markerSetName string) (Snapshot, bool) {
	if s == nil {
		return Snapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	encounter := s.encountersByMarkerSet[strings.ToLower(markerSetName)]
	if encounter == nil {
		return Snapshot{}, false
	}
	liveObjectIDs := make([]uint32, 0, len(encounter.liveObjectIDs))
	for objectID := range encounter.liveObjectIDs {
		liveObjectIDs = append(liveObjectIDs, objectID)
	}
	slices.Sort(liveObjectIDs)
	return Snapshot{
		MarkerSetName: encounter.publication.MarkerSetName,
		Phase:         encounter.phase, WaveOrdinal: encounter.waveOrdinal,
		LiveObjectIDs: liveObjectIDs, IsGateActive: encounter.isGateActive,
		Completion: encounter.completion,
	}, true
}

func (s *Session) Snapshots() []Snapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshots := make([]Snapshot, 0, len(s.encountersByMarkerSet))
	for _, encounter := range s.encountersByMarkerSet {
		liveObjectIDs := make([]uint32, 0, len(encounter.liveObjectIDs))
		for objectID := range encounter.liveObjectIDs {
			liveObjectIDs = append(liveObjectIDs, objectID)
		}
		slices.Sort(liveObjectIDs)
		snapshots = append(snapshots, Snapshot{
			MarkerSetName: encounter.publication.MarkerSetName,
			Phase:         encounter.phase, WaveOrdinal: encounter.waveOrdinal,
			LiveObjectIDs: liveObjectIDs, IsGateActive: encounter.isGateActive,
			Completion: encounter.completion,
		})
	}
	slices.SortFunc(snapshots, func(left Snapshot, right Snapshot) int {
		return strings.Compare(left.MarkerSetName, right.MarkerSetName)
	})
	return snapshots
}

// RestoreCompleted restores terminal horde state captured at a safe durable
// boundary. Active encounters are intentionally excluded from checkpoints.
func (s *Session) RestoreCompleted(snapshots []Snapshot) error {
	if s == nil {
		return errors.New("horde restore: nil session")
	}
	err := ValidateCompletedSnapshots(snapshots)
	if err != nil {
		return fmt.Errorf("hordeRestoreValidate: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.encountersByMarkerSet) != 0 {
		return errors.New("horde restore: session active")
	}
	for _, snapshot := range snapshots {
		key := strings.ToLower(snapshot.MarkerSetName)
		s.encountersByMarkerSet[key] = &encounter{
			publication: game.CampaignDirectorPublication{
				MarkerSetName:   snapshot.MarkerSetName,
				TriggerMarkerID: snapshot.Completion.TriggerMarkerID,
			},
			phase: snapshot.Phase, waveOrdinal: snapshot.WaveOrdinal,
			liveObjectIDs: make(map[uint32]bool),
			isGateActive:  false, completion: snapshot.Completion,
		}
	}
	return nil
}

func ValidateCompletedSnapshots(snapshots []Snapshot) error {
	markerSets := make(map[string]struct{}, len(snapshots))
	for index, snapshot := range snapshots {
		key := strings.ToLower(strings.TrimSpace(snapshot.MarkerSetName))
		isCompletionValid := snapshot.Phase == PhaseComplete &&
			snapshot.WaveOrdinal > 0 && len(snapshot.LiveObjectIDs) == 0 &&
			!snapshot.IsGateActive && snapshot.Completion.EventName == "horde complete" &&
			strings.EqualFold(snapshot.Completion.MarkerSetName, snapshot.MarkerSetName) &&
			snapshot.Completion.TriggerMarkerID != 0
		if key == "" || !isCompletionValid {
			return fmt.Errorf("horde snapshot[%d]: invalid", index)
		}
		if _, isDuplicate := markerSets[key]; isDuplicate {
			return fmt.Errorf("horde snapshot[%d]: duplicate", index)
		}
		markerSets[key] = struct{}{}
	}
	return nil
}

func (s *Session) IsComplete(markerSetName string) bool {
	snapshot, isFound := s.Snapshot(markerSetName)
	return isFound && snapshot.Phase == PhaseComplete && !snapshot.IsGateActive &&
		snapshot.Completion.EventName == "horde complete"
}

// IsCheckpointSafe reports whether every triggered horde has reached its
// terminal state. Dormant, never-triggered sequences do not create encounter
// snapshots and are therefore safe. Active and inter-wave encounters require
// more state than the durable checkpoint currently owns.
func (s *Session) IsCheckpointSafe() bool {
	if s == nil {
		return false
	}
	for _, snapshot := range s.Snapshots() {
		if snapshot.Phase != PhaseComplete || snapshot.IsGateActive {
			return false
		}
	}
	return true
}

func (s *Session) ConstrainMovement(
	director game.CampaignDirector, previous game.Vec3, current game.Vec3,
) (game.Vec3, GateContact, error) {
	if s == nil {
		return current, GateContact{}, errors.New("horde gate: nil session")
	}
	if !isFinitePosition(previous) || !isFinitePosition(current) {
		return current, GateContact{}, errors.New("horde gate: invalid position")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, markerSet := range director.MarkerSets {
		encounter := s.encountersByMarkerSet[strings.ToLower(markerSet.Name)]
		if encounter == nil || !encounter.isGateActive {
			continue
		}
		isGateFound := false
		for _, marker := range markerSet.Markers {
			if !strings.EqualFold(marker.NounName, "HordeGateTeleporter.Noun") ||
				!gateHasContact(marker) {
				continue
			}
			isGateFound = true
			radiusSquared := gateContactRadius * gateContactRadius
			if segmentDistanceSquared(previous, current, marker.Position) > radiusSquared {
				continue
			}
			return previous, GateContact{
				MarkerSetName: markerSet.Name, MarkerID: marker.MarkerID,
				Position: marker.Position, ReturnPosition: previous,
			}, nil
		}
		if !isGateFound {
			continue
		}
	}
	return current, GateContact{}, nil
}

func clonePublication(
	publication game.CampaignDirectorPublication,
) game.CampaignDirectorPublication {
	cloned := publication
	cloned.Listeners = append(
		[]game.CampaignDirectorListenerPublication(nil),
		publication.Listeners...,
	)
	return cloned
}

func gateHasContact(marker game.CampaignDirectorMarker) bool {
	for _, event := range marker.Events {
		if event.CallbackName == "HordeGateTeleporter_OnEnter" {
			return true
		}
	}
	return false
}

func segmentDistanceSquared(start game.Vec3, end game.Vec3, point game.Vec3) float32 {
	delta := game.Vec3{X: end.X - start.X, Y: end.Y - start.Y, Z: end.Z - start.Z}
	lengthSquared := delta.X*delta.X + delta.Y*delta.Y + delta.Z*delta.Z
	if lengthSquared == 0 {
		x := start.X - point.X
		y := start.Y - point.Y
		z := start.Z - point.Z
		return x*x + y*y + z*z
	}
	projection := ((point.X-start.X)*delta.X + (point.Y-start.Y)*delta.Y +
		(point.Z-start.Z)*delta.Z) / lengthSquared
	projection = min(float32(1), max(float32(0), projection))
	x := start.X + projection*delta.X - point.X
	y := start.Y + projection*delta.Y - point.Y
	z := start.Z + projection*delta.Z - point.Z
	return x*x + y*y + z*z
}

func isFinitePosition(position game.Vec3) bool {
	return !math.IsNaN(float64(position.X)) &&
		!math.IsNaN(float64(position.Y)) &&
		!math.IsNaN(float64(position.Z)) &&
		!math.IsInf(float64(position.X), 0) &&
		!math.IsInf(float64(position.Y), 0) &&
		!math.IsInf(float64(position.Z), 0)
}
