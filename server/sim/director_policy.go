package sim

import (
	"errors"
	"fmt"
)

type SpikeOutcome uint8

const (
	SpikeOutcomeHard SpikeOutcome = iota + 1
	SpikeOutcomeMedium
	SpikeOutcomeSkip
)

type DirectorLocusKind uint8

const (
	DirectorLocusWanderer DirectorLocusKind = iota + 1
	DirectorLocusSpike
	DirectorLocusHorde
	DirectorLocusBoss
)

type DirectorLocus struct {
	ID   uint32
	Kind DirectorLocusKind
}

type WandererDecision struct {
	HitNumber uint8
	Threshold uint32
	IsSpawn   bool
	ClumpSize uint8
}

type DirectorRouteSection uint8

const (
	DirectorRouteSectionA DirectorRouteSection = iota + 1
	DirectorRouteSectionB
	DirectorRouteSectionC
)

const (
	LocalWandererRadius = float32(32)
	LocalSpikeRadius    = float32(25)
)

// DirectorPolicyState retains only the proven per-run locus and decision
// history. Spatial hit testing, random rolls, group composition, and spawning
// remain the caller's authority.
type DirectorPolicyState struct {
	kindByLocusID    map[uint32]DirectorLocusKind
	consumedLocusIDs map[uint32]bool
	wandererHitCount uint8
}

type SpikeHistory struct {
	previousOutcome SpikeOutcome
	currentOutcome  SpikeOutcome
}

func NewDirectorPolicyState(loci []DirectorLocus) (*DirectorPolicyState, error) {
	state := &DirectorPolicyState{
		kindByLocusID:    make(map[uint32]DirectorLocusKind, len(loci)),
		consumedLocusIDs: make(map[uint32]bool, len(loci)),
	}
	for index, locus := range loci {
		if locus.ID == 0 {
			return nil, fmt.Errorf("locusID[%d]: zero", index)
		}
		if locus.Kind != DirectorLocusWanderer && locus.Kind != DirectorLocusSpike {
			return nil, fmt.Errorf("locusKind[%d]: %d", index, locus.Kind)
		}
		if _, isFound := state.kindByLocusID[locus.ID]; isFound {
			return nil, fmt.Errorf("locusDuplicate[%d]: %d", index, locus.ID)
		}
		state.kindByLocusID[locus.ID] = locus.Kind
	}
	return state, nil
}

// HitWanderer consumes one explicitly observed Wanderer locus and returns the
// documented threshold for its run-local hit position. The unsupported hit
// after five remains unconsumed so a future recovered reset policy can decide
// it without repairing state.
func (s *DirectorPolicyState) HitWanderer(locusID uint32) (uint8, uint32, error) {
	err := s.validateLocus(locusID, DirectorLocusWanderer)
	if err != nil {
		return 0, 0, fmt.Errorf("wandererLocus: %w", err)
	}
	hitNumber := s.wandererHitCount + 1
	threshold, err := WandererHitThreshold(hitNumber)
	if err != nil {
		return 0, 0, fmt.Errorf("wandererThreshold: %w", err)
	}
	s.wandererHitCount = hitNumber
	s.consumedLocusIDs[locusID] = true
	return hitNumber, threshold, nil
}

// ResolveLocalWanderer applies the documented hit thresholds and the local
// playable fallback recorded in notes/help.md. Rolls are uniform integers in
// [0,99]. A success or the fifth miss resets the sequence; clump branches are
// interpreted as total counts rather than bonus counts.
func (s *DirectorPolicyState) ResolveLocalWanderer(
	locusID, spawnRoll, clumpRoll uint32,
) (WandererDecision, error) {
	if spawnRoll >= 100 {
		return WandererDecision{}, fmt.Errorf("wandererSpawnRoll: %d", spawnRoll)
	}
	if clumpRoll >= 100 {
		return WandererDecision{}, fmt.Errorf("wandererClumpRoll: %d", clumpRoll)
	}
	err := s.validateLocus(locusID, DirectorLocusWanderer)
	if err != nil {
		return WandererDecision{}, fmt.Errorf("wandererLocus: %w", err)
	}
	hitNumber := s.wandererHitCount + 1
	threshold, err := WandererHitThreshold(hitNumber)
	if err != nil {
		return WandererDecision{}, fmt.Errorf("wandererThreshold: %w", err)
	}
	decision := WandererDecision{
		HitNumber: hitNumber,
		Threshold: threshold,
		IsSpawn:   spawnRoll < threshold,
	}
	if decision.IsSpawn {
		decision.ClumpSize = localWandererClumpSize(clumpRoll)
	}
	s.consumedLocusIDs[locusID] = true
	if decision.IsSpawn || hitNumber == 5 {
		s.wandererHitCount = 0
		return decision, nil
	}
	s.wandererHitCount = hitNumber
	return decision, nil
}

// HitSpike consumes one explicitly observed Spike locus after advancing a
// caller-seeded history. No initial history is fabricated here.
func (s *DirectorPolicyState) HitSpike(
	locusID uint32, history *SpikeHistory, isDoingWell bool,
) (SpikeOutcome, error) {
	err := s.validateLocus(locusID, DirectorLocusSpike)
	if err != nil {
		return 0, fmt.Errorf("spikeLocus: %w", err)
	}
	if history == nil {
		return 0, errors.New("spikeHistory: nil")
	}
	outcome, err := history.Advance(isDoingWell)
	if err != nil {
		return 0, fmt.Errorf("spikeAdvance: %w", err)
	}
	s.consumedLocusIDs[locusID] = true
	return outcome, nil
}

func (s *DirectorPolicyState) validateLocus(locusID uint32, kind DirectorLocusKind) error {
	if s == nil {
		return errors.New("nil state")
	}
	locusKind, isFound := s.kindByLocusID[locusID]
	if !isFound {
		return errors.New("unknown locus")
	}
	if locusKind != kind {
		return errors.New("wrong locus kind")
	}
	if s.consumedLocusIDs[locusID] {
		return errors.New("consumed locus")
	}
	return nil
}

func NewSpikeHistory(previousOutcome, currentOutcome SpikeOutcome) (*SpikeHistory, error) {
	if !isSpikeOutcome(previousOutcome) || !isSpikeOutcome(currentOutcome) {
		return nil, errors.New("invalid spike history")
	}
	if previousOutcome == SpikeOutcomeSkip && currentOutcome == SpikeOutcomeSkip {
		return nil, errors.New("unsupported skip/skip spike history")
	}
	return &SpikeHistory{previousOutcome: previousOutcome, currentOutcome: currentOutcome}, nil
}

// NewLocalSpikeHistory seeds the unresolved retail history with a conservative
// Medium/Skip window. Its next outcome is Hard while doing well and Medium
// while doing poorly, avoiding an opening forced skip or oversized encounter.
func NewLocalSpikeHistory() *SpikeHistory {
	return &SpikeHistory{
		previousOutcome: SpikeOutcomeMedium,
		currentOutcome:  SpikeOutcomeSkip,
	}
}

// LocalSpikeChallenge returns conservative 1-1 challenge targets. The
// presentation supports rising section intensity and Medium at roughly half
// Hard, but these exact targets are local fallback tuning.
func LocalSpikeChallenge(section DirectorRouteSection, outcome SpikeOutcome) (uint32, error) {
	if outcome == SpikeOutcomeSkip {
		return 0, nil
	}
	if outcome != SpikeOutcomeHard && outcome != SpikeOutcomeMedium {
		return 0, errors.New("local spike challenge: invalid outcome")
	}
	var hardChallenge uint32
	switch section {
	case DirectorRouteSectionA:
		hardChallenge = 50
	case DirectorRouteSectionB:
		hardChallenge = 80
	case DirectorRouteSectionC:
		hardChallenge = 110
	default:
		return 0, errors.New("local spike challenge: invalid section")
	}
	if outcome == SpikeOutcomeMedium {
		return (hardChallenge + 1) / 2, nil
	}
	return hardChallenge, nil
}

func (h *SpikeHistory) Advance(isDoingWell bool) (SpikeOutcome, error) {
	if h == nil {
		return 0, errors.New("advance spike history: nil history")
	}
	outcome, err := NextSpikeOutcome(h.previousOutcome, h.currentOutcome, isDoingWell)
	if err != nil {
		return 0, fmt.Errorf("advance spike history: %w", err)
	}
	h.previousOutcome = h.currentOutcome
	h.currentOutcome = outcome
	return outcome, nil
}

func WandererHitThreshold(hitNumber uint8) (uint32, error) {
	switch hitNumber {
	case 1:
		return 18, nil
	case 2, 3, 4, 5:
		return 25, nil
	default:
		return 0, fmt.Errorf("unsupported wanderer hit: %d", hitNumber)
	}
}

func NextSpikeOutcome(
	previousOutcome SpikeOutcome, currentOutcome SpikeOutcome, isDoingWell bool,
) (SpikeOutcome, error) {
	if !isSpikeOutcome(previousOutcome) || !isSpikeOutcome(currentOutcome) {
		return 0, errors.New("invalid spike outcome")
	}
	if previousOutcome == SpikeOutcomeSkip && currentOutcome == SpikeOutcomeSkip {
		return 0, errors.New("unsupported skip/skip spike history")
	}
	if isDoingWell {
		switch {
		case previousOutcome == SpikeOutcomeMedium && currentOutcome == SpikeOutcomeHard,
			previousOutcome == SpikeOutcomeSkip && currentOutcome == SpikeOutcomeHard:
			return SpikeOutcomeMedium, nil
		default:
			return SpikeOutcomeHard, nil
		}
	}
	switch {
	case previousOutcome == SpikeOutcomeHard:
		return SpikeOutcomeMedium, nil
	case previousOutcome == SpikeOutcomeMedium && currentOutcome == SpikeOutcomeSkip,
		previousOutcome == SpikeOutcomeSkip && currentOutcome == SpikeOutcomeMedium:
		return SpikeOutcomeMedium, nil
	default:
		return SpikeOutcomeSkip, nil
	}
}

func isSpikeOutcome(outcome SpikeOutcome) bool {
	return outcome >= SpikeOutcomeHard && outcome <= SpikeOutcomeSkip
}

func localWandererClumpSize(clumpRoll uint32) uint8 {
	switch {
	case clumpRoll < 2:
		return 4
	case clumpRoll < 8:
		return 3
	case clumpRoll < 20:
		return 2
	default:
		return 1
	}
}
