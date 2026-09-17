package boss

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/darkspinnet/darkspin/server/game"
	zonecallback "github.com/darkspinnet/darkspin/server/zone/callback"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const initialMarkerSetName = "zelems_1_design_spawners.Markerset"
const initialTriggerCallback = "nTutorial_SoloSupportUnlockClient.main"
const genericTriggerCallback = "DirectorTrigger_SpawnBoss"

type Phase uint8

const (
	PhaseDormant Phase = iota
	PhaseArming
	PhaseActive
	PhaseComplete
)

type Transition struct {
	LeaderObjectID     uint32
	NextWaveActorCount int
	IsComplete         bool
}

type Snapshot struct {
	Phase                 Phase
	IsInitialChain        bool
	IsLeaderDeferred      bool
	Publication           game.CampaignDirectorPublication
	LeaderObjectID        uint32
	LeaderHitPoint        float32
	Plans                 []zonenpc.SpawnPlan
	LiveObjectIDs         []uint32
	FirstWaveAddObjectIDs []uint32
	IsSecondWaveRequested bool
	IsSecondWaveAdmitted  bool
	IsBeamOutReserved     bool
	IsBeamOutCommitted    bool
}

type Session struct {
	mu                    sync.RWMutex
	phase                 Phase
	isInitialChain        bool
	isLeaderDeferred      bool
	publication           game.CampaignDirectorPublication
	leaderObjectID        uint32
	leaderHitPoint        float32
	plans                 []zonenpc.SpawnPlan
	liveObjectIDs         map[uint32]bool
	firstWaveAddObjectIDs map[uint32]bool
	isSecondWaveRequested bool
	isSecondWaveAdmitted  bool
	isBeamOutReserved     bool
	isBeamOutCommitted    bool
}

func NewSession() *Session {
	return &Session{}
}

func (s *Session) Arm(
	publication game.CampaignDirectorPublication, plans []zonenpc.SpawnPlan,
) error {
	if s == nil {
		return errors.New("boss arm: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.canArm(publication, plans)
	if err != nil {
		return err
	}
	s.phase = PhaseArming
	s.isInitialChain = isInitialPublication(publication)
	s.isLeaderDeferred = s.isInitialChain ||
		(plans[0].IsCaptain && len(plans) > 1)
	s.publication = clonePublication(publication)
	s.leaderObjectID = plans[0].ObjectID
	s.leaderHitPoint = plans[0].NPCProfile.HitPoint
	s.plans = make([]zonenpc.SpawnPlan, len(plans))
	for index, plan := range plans {
		s.plans[index] = plan.Clone()
	}
	return nil
}

func (s *Session) CanArm(
	publication game.CampaignDirectorPublication, plans []zonenpc.SpawnPlan,
) error {
	if s == nil {
		return errors.New("boss arm: nil session")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.canArm(publication, plans)
}

func (s *Session) canArm(
	publication game.CampaignDirectorPublication, plans []zonenpc.SpawnPlan,
) error {
	if s.phase != PhaseDormant || len(plans) == 0 ||
		publication.MarkerSetName == "" || publication.TriggerMarkerID == 0 {
		return errors.New("boss arm: invalid")
	}
	if !plans[0].IsBoss || plans[0].ObjectID == 0 {
		return errors.New("boss arm: missing leader")
	}
	isInitialChain := isInitialPublication(publication)
	isGeneric := isGenericCallback(publication.CallbackName) &&
		publication.EventName != ""
	if !isInitialChain && !isGeneric {
		return errors.New("boss arm: unsupported publication")
	}
	if isInitialChain {
		if len(plans) != 5 ||
			!zonenpc.IsBossIdentityValid(plans[0].BossIdentity) {
			return errors.New("boss arm: invalid Illust identity")
		}
	} else if plans[0].BossIdentity.IsKnown {
		if !zonenpc.IsBossIdentityValid(plans[0].BossIdentity) {
			return errors.New("boss arm: invalid generic identity")
		}
	}
	for index, plan := range plans {
		if !strings.EqualFold(plan.MarkerSetName, publication.MarkerSetName) ||
			plan.ObjectID != plans[0].ObjectID+uint32(index) {
			return fmt.Errorf("boss arm plan[%d]: invalid", index)
		}
	}
	return nil
}

func (s *Session) Admit(plans []zonenpc.SpawnPlan) error {
	if s == nil {
		return errors.New("boss admit: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.canAdmit(plans)
	if err != nil {
		return err
	}
	s.phase = PhaseActive
	firstLiveIndex := 0
	if s.isLeaderDeferred {
		firstLiveIndex = 1
	}
	s.liveObjectIDs = make(map[uint32]bool, len(plans)-firstLiveIndex)
	s.firstWaveAddObjectIDs = make(map[uint32]bool, len(plans)-1)
	for index, plan := range plans {
		if index < firstLiveIndex {
			continue
		}
		s.liveObjectIDs[plan.ObjectID] = true
		if index != 0 {
			s.firstWaveAddObjectIDs[plan.ObjectID] = true
		}
	}
	return nil
}

func (s *Session) CanAdmit(plans []zonenpc.SpawnPlan) error {
	if s == nil {
		return errors.New("boss admit: nil session")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.canAdmit(plans)
}

func (s *Session) canAdmit(plans []zonenpc.SpawnPlan) error {
	if s.phase != PhaseArming || len(plans) != len(s.plans) {
		return errors.New("boss admit: invalid")
	}
	for index, plan := range plans {
		if !plan.IsEqual(s.plans[index]) {
			return fmt.Errorf("boss admit plan[%d]: changed", index)
		}
	}
	return nil
}

func (s *Session) ObserveDamage(
	result zonenpc.DamageResult,
) (Transition, error) {
	if s == nil {
		return Transition{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if result.ObjectID == 0 || result.MarkerSetName == "" ||
		!strings.EqualFold(result.MarkerSetName, s.publication.MarkerSetName) {
		return Transition{}, nil
	}
	// Scripted summons can inherit the boss marker without joining its wave.
	// Their damage/death belongs to the NPC session, not this encounter roster.
	isEncounterActor := false
	for _, plan := range s.plans {
		if plan.ObjectID == result.ObjectID {
			isEncounterActor = true
			break
		}
	}
	if !isEncounterActor {
		return Transition{}, nil
	}
	if s.phase != PhaseActive || !s.liveObjectIDs[result.ObjectID] {
		return Transition{}, errors.New("boss damage: unexpected actor")
	}
	if result.ObjectID == s.leaderObjectID {
		if result.HitPoint < 0 || result.HitPoint > result.PreviousHealth {
			return Transition{}, errors.New("boss damage: invalid leader health")
		}
		s.leaderHitPoint = result.HitPoint
	}
	if result.IsDefeated {
		delete(s.liveObjectIDs, result.ObjectID)
	}
	transition := Transition{LeaderObjectID: s.leaderObjectID}
	if s.isLeaderDeferred && !s.isSecondWaveRequested &&
		s.isFirstWaveAddClear() {
		s.isSecondWaveRequested = true
		s.phase = PhaseArming
		transition.NextWaveActorCount = 1
		if s.isInitialChain {
			transition.NextWaveActorCount = 3
		}
		return transition, nil
	}
	if (!s.isLeaderDeferred || s.isSecondWaveAdmitted) &&
		s.leaderHitPoint == 0 && len(s.liveObjectIDs) == 0 {
		s.phase = PhaseComplete
		transition.IsComplete = true
	}
	return transition, nil
}

func (s *Session) isFirstWaveAddClear() bool {
	for objectID := range s.firstWaveAddObjectIDs {
		if s.liveObjectIDs[objectID] {
			return false
		}
	}
	return true
}

func (s *Session) CanAdmitSecondWave(plans []zonenpc.SpawnPlan) error {
	if s == nil {
		return errors.New("boss second wave: nil session")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.canAdmitSecondWave(plans)
}

func (s *Session) canAdmitSecondWave(plans []zonenpc.SpawnPlan) error {
	if s.phase != PhaseArming || !s.isSecondWaveRequested ||
		s.isSecondWaveAdmitted || !s.isLeaderDeferred {
		return errors.New("boss second wave: invalid")
	}
	expectedActorCount := 1
	if s.isInitialChain {
		expectedActorCount = 3
	}
	if len(plans) != expectedActorCount {
		return errors.New("boss second wave: actor count invalid")
	}
	if !plans[0].IsEqual(s.plans[0]) || !plans[0].IsBoss {
		return errors.New("boss second wave: leader changed")
	}
	for index, plan := range plans {
		if plan.ObjectID == 0 ||
			!strings.EqualFold(plan.MarkerSetName, s.publication.MarkerSetName) {
			return fmt.Errorf("boss second wave plan[%d]: invalid", index)
		}
		if index != 0 && plan.IsBoss {
			return fmt.Errorf("boss second wave plan[%d]: unexpected boss", index)
		}
	}
	return nil
}

func (s *Session) AdmitSecondWave(plans []zonenpc.SpawnPlan) error {
	if s == nil {
		return errors.New("boss second wave: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.canAdmitSecondWave(plans)
	if err != nil {
		return err
	}
	existingObjectIDs := make(map[uint32]bool, len(s.plans))
	for _, plan := range s.plans {
		existingObjectIDs[plan.ObjectID] = true
	}
	for _, plan := range plans {
		s.liveObjectIDs[plan.ObjectID] = true
		if !existingObjectIDs[plan.ObjectID] {
			s.plans = append(s.plans, plan)
		}
	}
	s.isSecondWaveAdmitted = true
	s.phase = PhaseActive
	return nil
}

func (s *Session) LeaderPlan() (zonenpc.SpawnPlan, bool) {
	if s == nil {
		return zonenpc.SpawnPlan{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.plans) == 0 || !s.plans[0].IsBoss {
		return zonenpc.SpawnPlan{}, false
	}
	return s.plans[0], true
}

func (s *Session) IsLeaderDeferred() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isLeaderDeferred
}

func (s *Session) Publication() (game.CampaignDirectorPublication, bool) {
	if s == nil {
		return game.CampaignDirectorPublication{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.publication.TriggerMarkerID == 0 {
		return game.CampaignDirectorPublication{}, false
	}
	return clonePublication(s.publication), true
}

func (s *Session) Identity() (zonenpc.BossIdentity, bool) {
	if s == nil {
		return zonenpc.BossIdentity{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.plans) == 0 || !s.plans[0].BossIdentity.IsKnown {
		return zonenpc.BossIdentity{}, false
	}
	return s.plans[0].BossIdentity, true
}

func (s *Session) IsDormant() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.phase == PhaseDormant
}

func (s *Session) ReserveBeamOut() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseComplete || s.isBeamOutReserved || s.isBeamOutCommitted {
		return false
	}
	s.isBeamOutReserved = true
	return true
}

func (s *Session) RollbackBeamOut() error {
	if s == nil {
		return errors.New("boss beam rollback: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isBeamOutReserved || s.isBeamOutCommitted {
		return errors.New("boss beam rollback: invalid")
	}
	s.isBeamOutReserved = false
	return nil
}

func (s *Session) CommitBeamOut() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isBeamOutReserved || s.isBeamOutCommitted {
		return false
	}
	s.isBeamOutReserved = false
	s.isBeamOutCommitted = true
	return true
}

// CommitOutcome records the shared encounter transition after any member has
// entered their personal result flow. It is idempotent so other members can
// complete their own result transactions independently.
func (s *Session) CommitOutcome() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != PhaseComplete {
		return false
	}
	s.isBeamOutReserved = false
	s.isBeamOutCommitted = true
	return true
}

func (s *Session) IsBeamOutCommitted() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isBeamOutCommitted
}

func (s *Session) ReservedBeamOutBossObjectID() (uint32, bool) {
	if s == nil {
		return 0, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.phase != PhaseComplete || !s.isBeamOutReserved ||
		s.isBeamOutCommitted || s.leaderObjectID == 0 {
		return 0, false
	}
	return s.leaderObjectID, true
}

func (s *Session) CompleteForDeveloper() (uint32, error) {
	if s == nil {
		return 0, errors.New("boss developer completion unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaderObjectID == 0 ||
		(s.phase != PhaseArming && s.phase != PhaseActive) ||
		s.isBeamOutReserved || s.isBeamOutCommitted {
		return 0, errors.New("boss developer completion unavailable")
	}
	s.complete(s.leaderObjectID)
	return s.leaderObjectID, nil
}

func (s *Session) CompleteWithLeaderForDeveloper(leaderObjectID uint32) error {
	if s == nil || leaderObjectID == 0 {
		return errors.New("boss developer victory unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.complete(leaderObjectID)
	return nil
}

func (s *Session) complete(leaderObjectID uint32) {
	s.phase = PhaseComplete
	s.leaderObjectID = leaderObjectID
	s.leaderHitPoint = 0
	s.liveObjectIDs = make(map[uint32]bool)
	s.isSecondWaveRequested = true
	s.isSecondWaveAdmitted = true
	s.isBeamOutReserved = false
	s.isBeamOutCommitted = false
}

func (s *Session) Replace(candidate *Session) error {
	if s == nil || candidate == nil {
		return errors.New("boss replace: nil session")
	}
	if s == candidate {
		return nil
	}
	candidate.mu.RLock()
	clone := candidate.clone()
	candidate.mu.RUnlock()
	s.mu.Lock()
	s.restore(clone)
	s.mu.Unlock()
	return nil
}

func (s *Session) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clone()
}

func (s *Session) clone() Snapshot {
	liveObjectIDs := make([]uint32, 0, len(s.liveObjectIDs))
	for objectID := range s.liveObjectIDs {
		liveObjectIDs = append(liveObjectIDs, objectID)
	}
	slices.Sort(liveObjectIDs)
	firstWaveAddObjectIDs := make([]uint32, 0, len(s.firstWaveAddObjectIDs))
	for objectID := range s.firstWaveAddObjectIDs {
		firstWaveAddObjectIDs = append(firstWaveAddObjectIDs, objectID)
	}
	slices.Sort(firstWaveAddObjectIDs)
	return Snapshot{
		Phase: s.phase, IsInitialChain: s.isInitialChain,
		IsLeaderDeferred: s.isLeaderDeferred,
		Publication:      clonePublication(s.publication), LeaderObjectID: s.leaderObjectID,
		LeaderHitPoint:        s.leaderHitPoint,
		Plans:                 append([]zonenpc.SpawnPlan(nil), s.plans...),
		LiveObjectIDs:         liveObjectIDs,
		FirstWaveAddObjectIDs: firstWaveAddObjectIDs,
		IsSecondWaveRequested: s.isSecondWaveRequested,
		IsSecondWaveAdmitted:  s.isSecondWaveAdmitted,
		IsBeamOutReserved:     s.isBeamOutReserved,
		IsBeamOutCommitted:    s.isBeamOutCommitted,
	}
}

func (s *Session) restore(snapshot Snapshot) {
	s.phase = snapshot.Phase
	s.isInitialChain = snapshot.IsInitialChain
	s.isLeaderDeferred = snapshot.IsLeaderDeferred
	s.publication = clonePublication(snapshot.Publication)
	s.leaderObjectID = snapshot.LeaderObjectID
	s.leaderHitPoint = snapshot.LeaderHitPoint
	s.plans = append([]zonenpc.SpawnPlan(nil), snapshot.Plans...)
	s.liveObjectIDs = make(map[uint32]bool, len(snapshot.LiveObjectIDs))
	for _, objectID := range snapshot.LiveObjectIDs {
		s.liveObjectIDs[objectID] = true
	}
	s.firstWaveAddObjectIDs = make(
		map[uint32]bool, len(snapshot.FirstWaveAddObjectIDs),
	)
	for _, objectID := range snapshot.FirstWaveAddObjectIDs {
		s.firstWaveAddObjectIDs[objectID] = true
	}
	s.isSecondWaveRequested = snapshot.IsSecondWaveRequested
	s.isSecondWaveAdmitted = snapshot.IsSecondWaveAdmitted
	s.isBeamOutReserved = snapshot.IsBeamOutReserved
	s.isBeamOutCommitted = snapshot.IsBeamOutCommitted
}

func isInitialPublication(publication game.CampaignDirectorPublication) bool {
	return strings.EqualFold(publication.MarkerSetName, initialMarkerSetName) &&
		publication.CallbackName == initialTriggerCallback
}

func isGenericCallback(callbackName string) bool {
	return callbackName == genericTriggerCallback ||
		callbackName == zonecallback.CatalystUnlock ||
		callbackName == zonecallback.OverdriveUnlock
}

func clonePublication(
	publication game.CampaignDirectorPublication,
) game.CampaignDirectorPublication {
	publication.Listeners = append(
		[]game.CampaignDirectorListenerPublication(nil),
		publication.Listeners...,
	)
	return publication
}
