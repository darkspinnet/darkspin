package population

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/sim"
)

type candidate struct {
	locusID               uint32
	kind                  sim.DirectorLocusKind
	section               sim.DirectorRouteSection
	markerSetOrdinal      int
	markerSetName         string
	markerOrdinal         int
	positions             []game.Vec3
	rotations             []game.Vec3
	radius                float32
	provisionalCount      int
	isProvisionalCaptain  bool
	isAmbush              bool
	navigationComponentID uint32
	provisionalNounNames  []string
}

type Decision struct {
	LocusID               uint32
	MarkerSetName         string
	Kind                  sim.DirectorLocusKind
	Section               sim.DirectorRouteSection
	NavigationComponentID uint32
	Wanderer              sim.WandererDecision
	SpikeOutcome          sim.SpikeOutcome
	Challenge             uint32
	Positions             []game.Vec3
	Rotations             []game.Vec3
	ProvisionalCount      int
	IsProvisionalCaptain  bool
	IsFloorIntroduction   bool
	IsAmbush              bool
	ProvisionalNounNames  []string
}

type CandidateSnapshot struct {
	LocusID               uint32
	Section               sim.DirectorRouteSection
	NavigationComponentID uint32
	IsProvisionalCaptain  bool
	IsFloorIntroduction   bool
	IsAmbush              bool
	ProvisionalNounNames  []string
}

type Session struct {
	mu                       sync.RWMutex
	candidates               []candidate
	insideStates             map[uint32]bool
	resolvedDirectorPointIDs map[uint32]bool
	enteredComponents        map[uint32]bool
	navigation               *navigation.Mesh
	navigationPlanLayer      uint8
	floorComponentCount      int
	isOpeningPrimed          bool
	policy                   *sim.DirectorPolicyState
	spikeHistory             *sim.SpikeHistory
	random                   *sim.SimulatorRandom
}

func (s *Session) Random() *sim.SimulatorRandom {
	if s == nil {
		return nil
	}
	return s.random
}

func (s *Session) Candidates() []CandidateSnapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := make([]CandidateSnapshot, 0, len(s.candidates))
	for _, candidate := range s.candidates {
		snapshot = append(snapshot, CandidateSnapshot{
			LocusID: candidate.locusID, Section: candidate.section,
			NavigationComponentID: candidate.navigationComponentID,
			IsProvisionalCaptain:  candidate.isProvisionalCaptain,
			IsFloorIntroduction:   isFloorIntroductionCandidate(candidate),
			IsAmbush:              candidate.isAmbush,
			ProvisionalNounNames:  append([]string(nil), candidate.provisionalNounNames...),
		})
	}
	return snapshot
}

func NewSession(
	director game.CampaignDirector, seed uint32,
) (*Session, error) {
	return newSession(director, seed, false, nil)
}

func NewFirstClearSession(
	director game.CampaignDirector, seed uint32,
) (*Session, error) {
	return newSession(director, seed, true, nil)
}

func NewSessionWithNavigation(
	director game.CampaignDirector, seed uint32, mesh *navigation.Mesh,
) (*Session, error) {
	return newSession(director, seed, false, mesh)
}

func NewFirstClearSessionWithNavigation(
	director game.CampaignDirector, seed uint32, mesh *navigation.Mesh,
) (*Session, error) {
	return newSession(director, seed, true, mesh)
}

func newSession(
	director game.CampaignDirector, seed uint32, isFirstClear bool,
	mesh *navigation.Mesh,
) (*Session, error) {
	if director.Level == "" {
		return nil, errors.New("populationCreate: empty level")
	}
	var candidates []candidate
	for _, markerSet := range director.MarkerSets {
		if strings.Contains(strings.ToLower(markerSet.Name), "_ai_horde_") {
			continue
		}
		section := Section(markerSet.Name)
		if section == 0 {
			continue
		}
		spikePositions := make([]game.Vec3, 0)
		spikeRotations := make([]game.Vec3, 0)
		spikeMarkerID := uint32(0)
		spikeMarkerOrdinal := 0
		for _, marker := range markerSet.Markers {
			if !marker.IsSpawnKindKnown {
				continue
			}
			switch marker.SpawnKind {
			case 7:
				candidate := candidate{
					locusID: marker.MarkerID, kind: sim.DirectorLocusWanderer, section: section,
					markerSetOrdinal: markerSet.Ordinal, markerSetName: markerSet.Name,
					markerOrdinal: marker.Ordinal, positions: []game.Vec3{marker.Position},
					rotations: []game.Vec3{marker.Rotation},
					radius:    sim.LocalWandererRadius,
				}
				candidates = append(candidates, candidate)
			case 8:
				if spikeMarkerID == 0 {
					spikeMarkerID = marker.MarkerID
					spikeMarkerOrdinal = marker.Ordinal
				}
				spikePositions = append(spikePositions, marker.Position)
				spikeRotations = append(spikeRotations, marker.Rotation)
			}
		}
		if spikeMarkerID == 0 {
			continue
		}
		candidate := candidate{
			locusID: spikeMarkerID, kind: sim.DirectorLocusSpike, section: section,
			markerSetOrdinal: markerSet.Ordinal, markerSetName: markerSet.Name,
			markerOrdinal: spikeMarkerOrdinal, positions: spikePositions,
			rotations: spikeRotations,
			radius:    sim.LocalSpikeRadius,
		}
		candidates = append(candidates, candidate)
	}
	random := sim.NewSimulatorRandom(seed)
	if strings.EqualFold(director.Level, game.InitialChainLevel) {
		var planErr error
		candidates, planErr = applyInitialChainPopulationPlan(candidates, random)
		if planErr != nil {
			return nil, fmt.Errorf("populationInitialPlan: %w", planErr)
		}
		if isFirstClear {
			candidates = applyInitialChainFirstClearPopulation(candidates)
		}
	} else if strings.EqualFold(director.Level, "zelems_3") &&
		hasSecondChainBaseRoster(director) {
		var planErr error
		candidates, planErr = applySecondChainPopulationPlan(candidates, random)
		if planErr != nil {
			return nil, fmt.Errorf("populationSecondPlan: %w", planErr)
		}
	} else if strings.EqualFold(director.Level, "nocturna_4") &&
		hasThirdChainBaseRoster(director) {
		var planErr error
		candidates, planErr = applyThirdChainPopulationPlan(candidates, random)
		if planErr != nil {
			return nil, fmt.Errorf("populationThirdPlan: %w", planErr)
		}
	} else if strings.EqualFold(director.Level, "zelems_2") &&
		hasSeventhChainBaseRoster(director) {
		var planErr error
		candidates, planErr = applySeventhChainPopulationPlan(candidates, random)
		if planErr != nil {
			return nil, fmt.Errorf("populationSeventhPlan: %w", planErr)
		}
	} else if strings.EqualFold(director.Level, "zelems_4") &&
		hasEighthChainBaseRoster(director) {
		var planErr error
		candidates, planErr = applyEighthChainPopulationPlan(candidates, random)
		if planErr != nil {
			return nil, fmt.Errorf("populationEighthPlan: %w", planErr)
		}
	} else {
		theme, isThemeFound, planErr := campaignPopulationPoolTheme(director, random)
		if planErr != nil {
			return nil, fmt.Errorf("populationPoolTheme: %w", planErr)
		}
		if isThemeFound {
			candidates, planErr = applyCampaignPopulationThemes(
				candidates, [2]campaignPopulationTheme{theme, theme}, random,
			)
			if planErr != nil {
				return nil, fmt.Errorf("populationPoolPlan: %w", planErr)
			}
		}
	}
	assignCandidateRotations(candidates, director)
	navigationPlanLayer, floorComponentCount :=
		assignNavigationComponents(candidates, mesh)
	loci := make([]sim.DirectorLocus, 0, len(candidates))
	for _, candidate := range candidates {
		loci = append(loci, sim.DirectorLocus{ID: candidate.locusID, Kind: candidate.kind})
	}
	policy, err := sim.NewDirectorPolicyState(loci)
	if err != nil {
		return nil, fmt.Errorf("populationPolicy: %w", err)
	}
	return &Session{
		candidates: candidates, insideStates: make(map[uint32]bool, len(candidates)),
		resolvedDirectorPointIDs: make(map[uint32]bool, len(candidates)),
		enteredComponents:        make(map[uint32]bool, floorComponentCount),
		navigation:               mesh,
		navigationPlanLayer:      navigationPlanLayer,
		floorComponentCount:      floorComponentCount,
		policy:                   policy, spikeHistory: sim.NewLocalSpikeHistory(), random: random,
	}, nil
}

type initialChainFirstClearGroup struct {
	section   sim.DirectorRouteSection
	anchor    game.Vec3
	positions []game.Vec3
	nounNames []string
}

func applyInitialChainFirstClearPopulation(candidates []candidate) []candidate {
	groups := []initialChainFirstClearGroup{
		{
			section: sim.DirectorRouteSectionA,
			anchor:  game.Vec3{X: -172.795, Y: -63.524, Z: 0.088},
			positions: []game.Vec3{
				{X: -172.795, Y: -63.524, Z: 0.088},
				{X: -176.795, Y: -59.524, Z: 0.088},
				{X: -168.795, Y: -59.524, Z: 0.088},
				{X: -178.795, Y: -67.524, Z: 0.088},
				{X: -166.795, Y: -67.524, Z: 0.088},
				{X: -172.795, Y: -71.524, Z: 0.088},
			},
			nounNames: []string{
				"ZelemBasicRepair.Noun",
				"ZelemBasicHybrid.Noun", "ZelemBasicHybrid.Noun",
				"ZelemBasicHybrid.Noun", "ZelemBasicHybrid.Noun",
				"NomadWithDrone.Noun",
			},
		},
		{
			section: sim.DirectorRouteSectionB,
			anchor:  game.Vec3{X: 559.614, Y: -11.579, Z: 33.090},
			positions: []game.Vec3{
				{X: 559.614, Y: -11.579, Z: 33.090},
				{X: 576.708, Y: -19.188, Z: 31.088},
				{X: 563.614, Y: -8.579, Z: 33.090},
				{X: 559.614, Y: -17.579, Z: 33.090},
			},
			nounNames: []string{
				"ZelemSpecialHaster.Noun", "ZelemBasicRanged.Noun",
				"ZelemBasicRanged.Noun", "ZelemBasicRanged.Noun",
			},
		},
		{
			section: sim.DirectorRouteSectionB,
			anchor:  game.Vec3{X: 589.621, Y: -32.123, Z: 29.339},
			positions: []game.Vec3{
				{X: 595.149, Y: -46.874, Z: 27.242},
				{X: 593.388, Y: -30.777, Z: 30.084},
				{X: 580.325, Y: -18.718, Z: 30.690},
			},
			nounNames: []string{
				"ZelemBasicHybrid.Noun", "ZelemBasicHybrid.Noun", "ZelemBasicHybrid.Noun",
			},
		},
		{
			section: sim.DirectorRouteSectionB,
			anchor:  game.Vec3{X: 663.868, Y: -35.423, Z: 20.088},
			positions: []game.Vec3{
				{X: 660.868, Y: -37.423, Z: 20.088},
				{X: 666.868, Y: -37.423, Z: 20.088},
				{X: 660.868, Y: -33.423, Z: 20.088},
				{X: 666.868, Y: -33.423, Z: 20.088},
			},
			nounNames: []string{
				"ZelemBasicRanged.Noun", "ZelemBasicRanged.Noun",
				"ZelemBasicHybrid.Noun", "ZelemBasicHybrid.Noun",
			},
		},
		{
			section: sim.DirectorRouteSectionB,
			anchor:  game.Vec3{X: 620.540, Y: -60.157, Z: 24.673},
			positions: []game.Vec3{
				{X: 618.540, Y: -60.157, Z: 24.673},
				{X: 622.540, Y: -60.157, Z: 24.673},
			},
			nounNames: []string{"ZelemBasicRanged.Noun", "ZelemBasicRanged.Noun"},
		},
		{
			section: sim.DirectorRouteSectionC,
			anchor:  game.Vec3{X: -592.733, Y: 630.616, Z: 0.088},
			positions: []game.Vec3{
				{X: -592.733, Y: 630.616, Z: 0.088},
			},
			nounNames: []string{"ZelemBasicRanged.Noun"},
		},
		{
			section: sim.DirectorRouteSectionC,
			anchor:  game.Vec3{X: 255.014, Y: 666.813, Z: 10.088},
			positions: []game.Vec3{
				{X: 255.014, Y: 666.813, Z: 10.088},
				{X: 249.014, Y: 662.813, Z: 10.088},
				{X: 261.014, Y: 662.813, Z: 10.088},
				{X: 247.014, Y: 668.813, Z: 10.088},
				{X: 263.014, Y: 668.813, Z: 10.088},
				{X: 249.014, Y: 674.813, Z: 10.088},
				{X: 261.014, Y: 674.813, Z: 10.088},
				{X: 255.014, Y: 678.813, Z: 10.088},
				{X: 255.014, Y: 658.813, Z: 10.088},
			},
			nounNames: []string{
				"ZelemSpecialHaster.Noun",
				"ZelemBasicRanged.Noun", "ZelemBasicRanged.Noun",
				"ZelemBasicRepair.Noun", "ZelemBasicRepair.Noun",
				"ZelemBasicHybrid.Noun", "ZelemBasicHybrid.Noun",
				"ZelemBasicHybrid.Noun", "ZelemBasicHybrid.Noun",
			},
		},
		{
			section: sim.DirectorRouteSectionC,
			anchor:  game.Vec3{X: 187.032, Y: 659.184, Z: 5.088},
			positions: []game.Vec3{
				{X: 185.032, Y: 659.184, Z: 5.088},
				{X: 189.032, Y: 659.184, Z: 5.088},
			},
			nounNames: []string{"ZelemBasicHybrid.Noun", "ZelemBasicHybrid.Noun"},
		},
	}
	selectedIndexes := make(map[int]bool, len(groups))
	for _, group := range groups {
		selectedIndex := -1
		selectedDistance := float32(math.MaxFloat32)
		for candidateIndex, currentCandidate := range candidates {
			if selectedIndexes[candidateIndex] || currentCandidate.section != group.section ||
				currentCandidate.kind != sim.DirectorLocusWanderer {
				continue
			}
			distance := candidateDistanceSquared(currentCandidate, group.anchor)
			if distance >= selectedDistance {
				continue
			}
			selectedIndex = candidateIndex
			selectedDistance = distance
		}
		if selectedIndex < 0 {
			continue
		}
		selectedIndexes[selectedIndex] = true
		candidates[selectedIndex].positions = append([]game.Vec3(nil), group.positions...)
		candidates[selectedIndex].provisionalCount = len(group.positions)
		candidates[selectedIndex].isProvisionalCaptain = false
		candidates[selectedIndex].isAmbush = false
		candidates[selectedIndex].provisionalNounNames = append(
			[]string(nil), group.nounNames...,
		)
	}
	return candidates
}

// PrimeOpening introduces every ordinary pre-baked group connected to the
// entrance. When navigation or pre-baked population is unavailable, it falls
// back to the nearest authored Wanderer only inside that locus' activation
// radius. The retail opening selector remains server-owned and unrecovered.
func (s *Session) PrimeOpening(
	position game.Vec3,
) ([]Decision, error) {
	if s == nil {
		return nil, errors.New("populationPrime: nil session")
	}
	if !IsFinitePosition(position) {
		return nil, errors.New("populationPrime: invalid position")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isOpeningPrimed {
		return nil, nil
	}
	floorDecisions := s.resolveFloorIntroduction(position)
	if len(floorDecisions) != 0 {
		s.isOpeningPrimed = true
		return floorDecisions, nil
	}
	selected := candidate{}
	selectedDistance := float32(math.MaxFloat32)
	isSelected := false
	for _, candidate := range s.candidates {
		if candidate.kind != sim.DirectorLocusWanderer || s.resolvedDirectorPointIDs[candidate.locusID] {
			continue
		}
		distanceSquared := candidateDistanceSquared(candidate, position)
		if isSelected && (distanceSquared > selectedDistance ||
			(distanceSquared == selectedDistance && !candidateBefore(candidate, selected))) {
			continue
		}
		selected = candidate
		selectedDistance = distanceSquared
		isSelected = true
	}
	if !isSelected {
		return nil, nil
	}
	if selectedDistance > selected.radius*selected.radius {
		s.isOpeningPrimed = true
		return nil, nil
	}
	clumpRoll, err := s.random.Index(100)
	if err != nil {
		return nil, fmt.Errorf("populationPrimeClump: %w", err)
	}
	wanderer, err := s.policy.ResolveLocalWanderer(selected.locusID, 0, clumpRoll)
	if err != nil {
		return nil, fmt.Errorf("populationPrimeWanderer: %w", err)
	}
	s.resolvedDirectorPointIDs[selected.locusID] = true
	s.isOpeningPrimed = true
	return []Decision{{
		LocusID: selected.locusID, MarkerSetName: selected.markerSetName,
		Kind: selected.kind, Section: selected.section, Wanderer: wanderer,
		NavigationComponentID: selected.navigationComponentID,
		Positions:             append([]game.Vec3(nil), selected.positions...),
		Rotations:             append([]game.Vec3(nil), selected.rotations...),
		ProvisionalCount:      selected.provisionalCount,
		IsProvisionalCaptain:  selected.isProvisionalCaptain,
		ProvisionalNounNames:  append([]string(nil), selected.provisionalNounNames...),
	}}, nil
}

// Observe evaluates the reported current hero position, never the future move
// goal. At most the nearest newly entered candidate of each kind is resolved.
func (s *Session) Observe(
	position game.Vec3, isDoingWell bool,
) ([]Decision, error) {
	if s == nil {
		return nil, errors.New("populationObserve: nil session")
	}
	if !IsFinitePosition(position) {
		return nil, errors.New("populationObserve: invalid position")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	decisions := s.resolveFloorIntroduction(position)
	selectedByKind := make(map[sim.DirectorLocusKind]candidate, 2)
	distanceByKind := make(map[sim.DirectorLocusKind]float32, 2)
	for _, candidate := range s.candidates {
		if s.resolvedDirectorPointIDs[candidate.locusID] {
			continue
		}
		distanceSquared := candidateDistanceSquared(candidate, position)
		isInside := distanceSquared <= candidate.radius*candidate.radius
		wasInside := s.insideStates[candidate.locusID]
		s.insideStates[candidate.locusID] = isInside
		if wasInside || !isInside {
			continue
		}
		selected, isSelected := selectedByKind[candidate.kind]
		selectedDistance := distanceByKind[candidate.kind]
		if isSelected && (distanceSquared > selectedDistance ||
			(distanceSquared == selectedDistance && !candidateBefore(candidate, selected))) {
			continue
		}
		selectedByKind[candidate.kind] = candidate
		distanceByKind[candidate.kind] = distanceSquared
	}
	for _, kind := range []sim.DirectorLocusKind{sim.DirectorLocusWanderer, sim.DirectorLocusSpike} {
		candidate, isSelected := selectedByKind[kind]
		if !isSelected {
			continue
		}
		decision := Decision{
			LocusID: candidate.locusID, MarkerSetName: candidate.markerSetName,
			Kind: candidate.kind, Section: candidate.section,
			NavigationComponentID: candidate.navigationComponentID,
			Positions:             append([]game.Vec3(nil), candidate.positions...),
			Rotations:             append([]game.Vec3(nil), candidate.rotations...),
			ProvisionalCount:      candidate.provisionalCount,
			IsProvisionalCaptain:  candidate.isProvisionalCaptain,
			IsAmbush:              candidate.isAmbush,
			ProvisionalNounNames:  append([]string(nil), candidate.provisionalNounNames...),
		}
		if len(candidate.provisionalNounNames) != 0 {
			decisions = append(decisions, decision)
			s.resolvedDirectorPointIDs[candidate.locusID] = true
			continue
		}
		switch kind {
		case sim.DirectorLocusWanderer:
			spawnRoll, err := s.random.Index(100)
			if err != nil {
				return nil, fmt.Errorf("populationSpawnRoll: %w", err)
			}
			clumpRoll, err := s.random.Index(100)
			if err != nil {
				return nil, fmt.Errorf("populationClumpRoll: %w", err)
			}
			decision.Wanderer, err = s.policy.ResolveLocalWanderer(
				candidate.locusID, spawnRoll, clumpRoll,
			)
			if err != nil {
				return nil, fmt.Errorf("populationWanderer: %w", err)
			}
		case sim.DirectorLocusSpike:
			outcome, err := s.policy.HitSpike(candidate.locusID, s.spikeHistory, isDoingWell)
			if err != nil {
				return nil, fmt.Errorf("populationSpike: %w", err)
			}
			challenge, err := sim.LocalSpikeChallenge(candidate.section, outcome)
			if err != nil {
				return nil, fmt.Errorf("populationChallenge: %w", err)
			}
			decision.SpikeOutcome = outcome
			decision.Challenge = challenge
		}
		decisions = append(decisions, decision)
		s.resolvedDirectorPointIDs[candidate.locusID] = true
	}
	return decisions, nil
}

func (s *Session) resolveFloorIntroduction(position game.Vec3) []Decision {
	if s.navigation == nil || s.floorComponentCount == 0 ||
		len(s.enteredComponents) >= s.floorComponentCount {
		return nil
	}
	projection, err := s.navigation.Project(navigation.Vec3{
		X: position.X, Y: position.Y, Z: position.Z,
	}, navigation.ProjectionOptions{
		PlanLayer: s.navigationPlanLayer, MaxDistance: populationNavigationProjectionDistance,
	})
	if err != nil || projection.ComponentID == 0 ||
		s.enteredComponents[projection.ComponentID] {
		return nil
	}
	decisions := make([]Decision, 0)
	for _, candidate := range s.candidates {
		if candidate.navigationComponentID != projection.ComponentID ||
			!isFloorIntroductionCandidate(candidate) ||
			s.resolvedDirectorPointIDs[candidate.locusID] {
			continue
		}
		decisions = append(decisions, Decision{
			LocusID: candidate.locusID, MarkerSetName: candidate.markerSetName,
			Kind: candidate.kind, Section: candidate.section,
			NavigationComponentID: candidate.navigationComponentID,
			Positions:             append([]game.Vec3(nil), candidate.positions...),
			Rotations:             append([]game.Vec3(nil), candidate.rotations...),
			ProvisionalCount:      candidate.provisionalCount,
			IsProvisionalCaptain:  candidate.isProvisionalCaptain,
			IsFloorIntroduction:   true,
			ProvisionalNounNames:  append([]string(nil), candidate.provisionalNounNames...),
		})
		s.resolvedDirectorPointIDs[candidate.locusID] = true
	}
	if len(decisions) != 0 {
		s.enteredComponents[projection.ComponentID] = true
	}
	return decisions
}

func isFloorIntroductionCandidate(candidate candidate) bool {
	return !candidate.isAmbush && candidate.navigationComponentID != 0 &&
		len(candidate.provisionalNounNames) != 0
}

const (
	populationNavigationFootprintRadius    = float32(0.5)
	populationNavigationHeight             = float32(1.75)
	populationNavigationProjectionDistance = float32(6)
)

func assignNavigationComponents(
	candidates []candidate, mesh *navigation.Mesh,
) (uint8, int) {
	if mesh == nil {
		return 0, 0
	}
	planLayer, isLayerFound := mesh.SelectLayer(
		populationNavigationFootprintRadius, populationNavigationHeight,
	)
	if !isLayerFound {
		return 0, 0
	}
	floorComponents := make(map[uint32]struct{})
	for index := range candidates {
		if len(candidates[index].positions) == 0 ||
			len(candidates[index].provisionalNounNames) == 0 {
			continue
		}
		position := candidates[index].positions[0]
		projection, err := mesh.Project(navigation.Vec3{
			X: position.X, Y: position.Y, Z: position.Z,
		}, navigation.ProjectionOptions{
			PlanLayer: planLayer, MaxDistance: populationNavigationProjectionDistance,
		})
		if err != nil || projection.ComponentID == 0 {
			continue
		}
		candidates[index].navigationComponentID = projection.ComponentID
		if candidates[index].isAmbush {
			continue
		}
		floorComponents[projection.ComponentID] = struct{}{}
	}
	return planLayer, len(floorComponents)
}

const CampaignFloorPopulationTarget = 15

var initialChainOpeningAnchor = game.Vec3{
	X: -152.01, Y: -28.20, Z: 0.088,
}

type campaignPopulationTheme struct {
	minionPair      [2]string
	lieutenantNouns []string
}

var initialChainRepairTheme = campaignPopulationTheme{
	minionPair:      [2]string{"ZelemBasicRepair.Noun", "ZelemBasicHybrid.Noun"},
	lieutenantNouns: []string{"NomadWithDrone.Noun"},
}

var initialChainBarracudaTheme = campaignPopulationTheme{
	minionPair:      [2]string{"ZelemBasicRanged.Noun", "ZelemBasicRanged.Noun"},
	lieutenantNouns: []string{"ZelemSpecialHaster.Noun", "NomadSnipe.Noun"},
}

// applyInitialChainPopulationPlan replaces the dense authored control cloud
// with two lieutenant-centered traversal clusters and a bounded minion fill on
// each route floor. Floor A always uses the walkthrough's
// Cannonator/Reparatron/Invincitron theme. Floors B and C independently choose
// that theme or the Space Barracuda/Haster/Decelerator theme.
func applyInitialChainPopulationPlan(
	candidates []candidate, random *sim.SimulatorRandom,
) ([]candidate, error) {
	if random == nil {
		return nil, errors.New("nil initial population random")
	}
	if !canPlanCampaignFloorPopulation(candidates) {
		return candidates, nil
	}
	planned := make([]candidate, 0, 12)
	for _, section := range []sim.DirectorRouteSection{
		sim.DirectorRouteSectionA,
		sim.DirectorRouteSectionB,
		sim.DirectorRouteSectionC,
	} {
		sectionCandidate := make([]candidate, 0)
		for _, candidate := range candidates {
			if candidate.section == section {
				sectionCandidate = append(sectionCandidate, candidate)
			}
		}
		theme := initialChainRepairTheme
		if section != sim.DirectorRouteSectionA {
			themeIndex, err := random.Index(2)
			if err != nil {
				return nil, fmt.Errorf("theme[%d]: %w", section, err)
			}
			if themeIndex == 1 {
				theme = initialChainBarracudaTheme
			}
		}
		var openingAnchor *game.Vec3
		if section == sim.DirectorRouteSectionA {
			openingAnchor = &initialChainOpeningAnchor
		}
		floorPlan, err := planCampaignFloor(
			sectionCandidate, theme, random,
			CampaignFloorPopulationTarget, openingAnchor,
		)
		if err != nil {
			return nil, fmt.Errorf("floor[%d]: %w", section, err)
		}
		planned = append(planned, floorPlan...)
	}
	for _, candidate := range candidates {
		if candidate.section == sim.DirectorRouteSectionA ||
			candidate.section == sim.DirectorRouteSectionB ||
			candidate.section == sim.DirectorRouteSectionC {
			continue
		}
		planned = append(planned, candidate)
	}
	return planned, nil
}

var secondChainQuantumTheme = campaignPopulationTheme{
	minionPair: [2]string{
		"ZelemBasicMelee.Noun", "ZelemBasicRangedHoming.Noun",
	},
	lieutenantNouns: []string{
		"ZelemSpecialOne.Noun", "ZelemSpecialTwo.noun",
	},
}

var secondChainBioTheme = campaignPopulationTheme{
	minionPair: [2]string{
		"VerdanthBasicPlunge.Noun", "VerdanthBasicPlunge.Noun",
	},
	lieutenantNouns: []string{"NomadSpecialThree.Noun"},
}

var thirdChainNecroTheme = campaignPopulationTheme{
	minionPair: [2]string{
		"NocturnaBasicHealthDrain.Noun", "NoctBasicFlyer.Noun",
	},
	lieutenantNouns: []string{
		"NocturnaSpecialLeech.Noun", "Rezzer.Noun",
	},
}

var thirdChainPlasmaTheme = campaignPopulationTheme{
	minionPair: [2]string{
		"CitadelBasicMelee.Noun", "CitadelBasicMelee.Noun",
	},
	lieutenantNouns: []string{"Boomer.Noun"},
}

var seventhChainQuantumTheme = campaignPopulationTheme{
	minionPair: [2]string{
		"ZelemBasicChargeup.Noun", "ZelemBasicFlyingMelee.Noun",
	},
	lieutenantNouns: []string{
		"ZelemSpecialOne.Noun", "NomadSnipe.Noun",
	},
}

var seventhChainCyberTheme = campaignPopulationTheme{
	minionPair: [2]string{
		"CitadelSpecificThree.Noun", "CitadelSpecificThree.Noun",
	},
	lieutenantNouns: []string{"ZelemSpecialThree.Noun"},
}

var eighthChainQuantumTheme = campaignPopulationTheme{
	minionPair: [2]string{
		"ZelemBasicPackfly.Noun", "VerdanthBasicMelee.Noun",
	},
	lieutenantNouns: []string{
		"ZelemSpecialTwo.noun", "ZelemSpecialHaster.noun",
	},
}

var eighthChainNecroTheme = campaignPopulationTheme{
	minionPair: [2]string{
		"Shooter.Noun", "Shooter.Noun",
	},
	lieutenantNouns: []string{"NocturnaSpecialHomer.Noun"},
}

func campaignPopulationPoolTheme(
	director game.CampaignDirector, random *sim.SimulatorRandom,
) (campaignPopulationTheme, bool, error) {
	if random == nil {
		return campaignPopulationTheme{}, false, errors.New("nil pool theme random")
	}
	minionEntries := PoolEntries(director, "minion")
	lieutenantEntries := PoolEntries(director, "captain")
	if len(lieutenantEntries) == 0 {
		lieutenantEntries = PoolEntries(director, "special")
	}
	if len(minionEntries) == 0 || len(lieutenantEntries) == 0 {
		return campaignPopulationTheme{}, false, nil
	}
	firstMinionIndex, err := random.Index(uint32(len(minionEntries)))
	if err != nil {
		return campaignPopulationTheme{}, false, fmt.Errorf("firstMinion: %w", err)
	}
	secondMinionIndex, err := random.Index(uint32(len(minionEntries)))
	if err != nil {
		return campaignPopulationTheme{}, false, fmt.Errorf("secondMinion: %w", err)
	}
	theme := campaignPopulationTheme{
		minionPair: [2]string{
			minionEntries[firstMinionIndex].NounName,
			minionEntries[secondMinionIndex].NounName,
		},
		lieutenantNouns: make([]string, 0, len(lieutenantEntries)),
	}
	for _, entry := range lieutenantEntries {
		theme.lieutenantNouns = append(theme.lieutenantNouns, entry.NounName)
	}
	return theme, true, nil
}

func applySecondChainPopulationPlan(
	candidates []candidate, random *sim.SimulatorRandom,
) ([]candidate, error) {
	return applyCampaignPopulationThemes(
		candidates,
		[2]campaignPopulationTheme{
			secondChainQuantumTheme, secondChainBioTheme,
		},
		random,
	)
}

func applyThirdChainPopulationPlan(
	candidates []candidate, random *sim.SimulatorRandom,
) ([]candidate, error) {
	return applyCampaignPopulationThemes(
		candidates,
		[2]campaignPopulationTheme{
			thirdChainNecroTheme, thirdChainPlasmaTheme,
		},
		random,
	)
}

func applySeventhChainPopulationPlan(
	candidates []candidate, random *sim.SimulatorRandom,
) ([]candidate, error) {
	return applyCampaignPopulationThemes(
		candidates,
		[2]campaignPopulationTheme{
			seventhChainQuantumTheme, seventhChainCyberTheme,
		},
		random,
	)
}

func applyEighthChainPopulationPlan(
	candidates []candidate, random *sim.SimulatorRandom,
) ([]candidate, error) {
	return applyCampaignPopulationThemes(
		candidates,
		[2]campaignPopulationTheme{
			eighthChainQuantumTheme, eighthChainNecroTheme,
		},
		random,
	)
}

func applyCampaignPopulationThemes(
	candidates []candidate, themes [2]campaignPopulationTheme,
	random *sim.SimulatorRandom,
) ([]candidate, error) {
	if random == nil {
		return nil, errors.New("nil campaign population random")
	}
	if !canPlanCampaignFloorPopulation(candidates) {
		return candidates, nil
	}
	planned := make([]candidate, 0, 12)
	for _, section := range []sim.DirectorRouteSection{
		sim.DirectorRouteSectionA,
		sim.DirectorRouteSectionB,
		sim.DirectorRouteSectionC,
	} {
		sectionCandidate := make([]candidate, 0)
		for _, candidate := range candidates {
			if candidate.section == section {
				sectionCandidate = append(sectionCandidate, candidate)
			}
		}
		theme := themes[0]
		themeIndex, err := random.Index(2)
		if err != nil {
			return nil, fmt.Errorf("theme[%d]: %w", section, err)
		}
		if themeIndex == 1 {
			theme = themes[1]
		}
		floorPlan, err := planCampaignFloor(
			sectionCandidate, theme, random,
			CampaignFloorPopulationTarget, nil,
		)
		if err != nil {
			return nil, fmt.Errorf("floor[%d]: %w", section, err)
		}
		planned = append(planned, floorPlan...)
	}
	for _, candidate := range candidates {
		if candidate.section == sim.DirectorRouteSectionA ||
			candidate.section == sim.DirectorRouteSectionB ||
			candidate.section == sim.DirectorRouteSectionC {
			continue
		}
		planned = append(planned, candidate)
	}
	return planned, nil
}

func hasSecondChainBaseRoster(director game.CampaignDirector) bool {
	return hasCampaignBaseRoster(director, []string{
		"ZelemBasicMelee.Noun",
		"ZelemBasicRangedHoming.Noun",
		"VerdanthBasicPlunge.Noun",
		"ZelemSpecialOne.Noun",
		"ZelemSpecialTwo.Noun",
		"NomadSpecialThree.Noun",
	})
}

func hasThirdChainBaseRoster(director game.CampaignDirector) bool {
	return hasCampaignBaseRoster(director, []string{
		"NocturnaBasicHealthDrain.Noun",
		"NoctBasicFlyer.Noun",
		"NocturnaSpecialLeech.Noun",
		"Rezzer.Noun",
		"CitadelBasicMelee.Noun",
		"Boomer.Noun",
	})
}

func hasSeventhChainBaseRoster(director game.CampaignDirector) bool {
	return hasCampaignBaseRoster(director, []string{
		"ZelemBasicChargeup.Noun",
		"ZelemBasicFlyingMelee.Noun",
		"CitadelSpecificThree.Noun",
		"ZelemSpecialOne.Noun",
		"NomadSnipe.Noun",
		"ZelemSpecialThree.Noun",
	})
}

func hasEighthChainBaseRoster(director game.CampaignDirector) bool {
	return hasCampaignBaseRoster(director, []string{
		"ZelemBasicPackfly.Noun",
		"VerdanthBasicMelee.Noun",
		"Shooter.Noun",
		"ZelemSpecialTwo.Noun",
		"ZelemSpecialHaster.Noun",
		"NocturnaSpecialHomer.Noun",
	})
}

func hasCampaignBaseRoster(
	director game.CampaignDirector, nounNames []string,
) bool {
	if len(nounNames) == 0 {
		return false
	}
	requiredNouns := make(map[string]bool, len(nounNames))
	for _, nounName := range nounNames {
		requiredNouns[strings.ToLower(nounName)] = false
	}
	for _, pool := range director.Pools {
		for _, entry := range pool.Entries {
			nounName := strings.ToLower(entry.NounName)
			if _, isRequired := requiredNouns[nounName]; isRequired {
				requiredNouns[nounName] = true
			}
		}
	}
	for _, isFound := range requiredNouns {
		if !isFound {
			return false
		}
	}
	return true
}

func canPlanCampaignFloorPopulation(candidates []candidate) bool {
	for _, section := range []sim.DirectorRouteSection{
		sim.DirectorRouteSectionA,
		sim.DirectorRouteSectionB,
		sim.DirectorRouteSectionC,
	} {
		isMinionFound := false
		isEliteFound := false
		for _, candidate := range candidates {
			if candidate.section != section || len(candidate.positions) == 0 {
				continue
			}
			isMinionFound = isMinionFound || candidate.kind == sim.DirectorLocusWanderer
			isEliteFound = isEliteFound || candidate.kind == sim.DirectorLocusSpike
		}
		if !isMinionFound || !isEliteFound {
			return false
		}
	}
	return true
}

func planCampaignFloor(
	candidates []candidate, theme campaignPopulationTheme,
	random *sim.SimulatorRandom, populationTarget int,
	openingAnchor *game.Vec3,
) ([]candidate, error) {
	minionCandidate := make([]candidate, 0)
	elitePosition := make([]game.Vec3, 0)
	for _, candidate := range candidates {
		switch candidate.kind {
		case sim.DirectorLocusWanderer:
			if len(candidate.positions) == 1 {
				minionCandidate = append(minionCandidate, candidate)
			}
		case sim.DirectorLocusSpike:
			elitePosition = append(elitePosition, candidate.positions...)
		}
	}
	if len(minionCandidate) == 0 {
		return nil, errors.New("no minion spawn points")
	}
	if len(elitePosition) == 0 {
		return nil, errors.New("no elite spawn points")
	}
	firstElite := game.Vec3{}
	if openingAnchor != nil {
		firstElite = nearestCampaignPosition(
			elitePosition, *openingAnchor,
		)
	} else {
		firstEliteIndex, err := random.Index(uint32(len(elitePosition)))
		if err != nil {
			return nil, fmt.Errorf("firstElite: %w", err)
		}
		firstElite = elitePosition[firstEliteIndex]
	}
	selectedElite := []game.Vec3{firstElite}
	if len(elitePosition) > 1 {
		selectedElite = append(
			selectedElite, furthestCampaignPosition(elitePosition, firstElite),
		)
	}
	available := append([]candidate(nil), minionCandidate...)
	plans := make([]candidate, 0, 4)
	spawnedCount := 0
	for eliteIndex, position := range selectedElite {
		remainingCluster := len(selectedElite) - eliteIndex - 1
		maximumPairCount := min(
			4,
			(populationTarget-spawnedCount-remainingCluster*5-1)/2,
		)
		maximumPairCount = min(maximumPairCount, len(available)/2)
		if maximumPairCount < 2 {
			continue
		}
		pairRoll, pairErr := random.Index(uint32(maximumPairCount - 1))
		if pairErr != nil {
			return nil, fmt.Errorf("pairCount[%d]: %w", eliteIndex, pairErr)
		}
		pairCount := 2 + int(pairRoll)
		nearby, remaining := nearestCampaignCandidates(available, position, pairCount*2)
		available = remaining
		lieutenantIndex, lieutenantErr := random.Index(uint32(len(theme.lieutenantNouns)))
		if lieutenantErr != nil {
			return nil, fmt.Errorf("lieutenant[%d]: %w", eliteIndex, lieutenantErr)
		}
		positions := make([]game.Vec3, 0, 1+len(nearby))
		positions = append(positions, position)
		nounNames := make([]string, 0, 1+len(nearby))
		nounNames = append(nounNames, theme.lieutenantNouns[lieutenantIndex])
		for index, candidate := range nearby {
			positions = append(positions, candidate.positions[0])
			nounNames = append(nounNames, theme.minionPair[index%len(theme.minionPair)])
		}
		plans = append(plans, initialChainClusterCandidate(
			nearby[0], positions, nounNames,
		))
		spawnedCount += len(nounNames)
	}
	for spawnedCount < populationTarget && len(available) != 0 {
		candidateIndex, indexErr := random.Index(uint32(len(available)))
		if indexErr != nil {
			return nil, fmt.Errorf("fillPoint: %w", indexErr)
		}
		candidate := available[candidateIndex]
		available = slices.Delete(available, int(candidateIndex), int(candidateIndex)+1)
		candidate.positions = append([]game.Vec3(nil), candidate.positions[0])
		candidate.provisionalCount = 1
		candidate.isProvisionalCaptain = false
		candidate.isAmbush = false
		candidate.provisionalNounNames = []string{
			theme.minionPair[spawnedCount%len(theme.minionPair)],
		}
		plans = append(plans, candidate)
		spawnedCount++
	}
	return plans, nil
}

func initialChainClusterCandidate(
	anchor candidate, positions []game.Vec3, nounNames []string,
) candidate {
	anchor.positions = append([]game.Vec3(nil), positions...)
	anchor.provisionalCount = len(nounNames)
	anchor.isProvisionalCaptain = true
	anchor.isAmbush = true
	anchor.provisionalNounNames = append([]string(nil), nounNames...)
	return anchor
}

func nearestCampaignCandidates(
	candidates []candidate, position game.Vec3, count int,
) ([]candidate, []candidate) {
	ordered := append([]candidate(nil), candidates...)
	slices.SortFunc(ordered, func(first, second candidate) int {
		firstDistance := candidateDistanceSquared(first, position)
		secondDistance := candidateDistanceSquared(second, position)
		switch {
		case firstDistance < secondDistance:
			return -1
		case firstDistance > secondDistance:
			return 1
		case candidateBefore(first, second):
			return -1
		default:
			return 1
		}
	})
	count = min(count, len(ordered))
	return ordered[:count], ordered[count:]
}

func furthestCampaignPosition(positions []game.Vec3, origin game.Vec3) game.Vec3 {
	selected := positions[0]
	selectedDistance := float32(-1)
	for _, position := range positions {
		deltaX := position.X - origin.X
		deltaY := position.Y - origin.Y
		deltaZ := position.Z - origin.Z
		distance := deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ
		if distance > selectedDistance {
			selected = position
			selectedDistance = distance
		}
	}
	return selected
}

func nearestCampaignPosition(positions []game.Vec3, origin game.Vec3) game.Vec3 {
	selected := positions[0]
	selectedDistance := float32(math.MaxFloat32)
	for _, position := range positions {
		deltaX := position.X - origin.X
		deltaY := position.Y - origin.Y
		deltaZ := position.Z - origin.Z
		distance := deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ
		if distance < selectedDistance {
			selected = position
			selectedDistance = distance
		}
	}
	return selected
}

func Section(markerSetName string) sim.DirectorRouteSection {
	name := strings.ToLower(markerSetName)
	directorKindIndex := strings.Index(name, "wanderer")
	if directorKindIndex < 0 {
		directorKindIndex = strings.Index(name, "wander")
	}
	if directorKindIndex < 0 {
		directorKindIndex = strings.Index(name, "spike")
	}
	if directorKindIndex < 0 {
		return 0
	}
	stem := strings.TrimSuffix(name, ".markerset")
	switch stem[len(stem)-1] {
	case 'a':
		return sim.DirectorRouteSectionA
	case 'b':
		return sim.DirectorRouteSectionB
	case 'c':
		return sim.DirectorRouteSectionC
	}
	return sim.DirectorRouteSectionA
}

func candidateDistanceSquared(
	candidate candidate, position game.Vec3,
) float32 {
	minimum := float32(math.MaxFloat32)
	for _, candidatePosition := range candidate.positions {
		x := candidatePosition.X - position.X
		y := candidatePosition.Y - position.Y
		z := candidatePosition.Z - position.Z
		distanceSquared := x*x + y*y + z*z
		minimum = min(minimum, distanceSquared)
	}
	return minimum
}

func candidateBefore(
	first, second candidate,
) bool {
	if first.markerSetOrdinal != second.markerSetOrdinal {
		return first.markerSetOrdinal < second.markerSetOrdinal
	}
	return first.markerOrdinal < second.markerOrdinal
}

func IsFinitePosition(position game.Vec3) bool {
	for _, coordinate := range []float32{position.X, position.Y, position.Z} {
		if math.IsNaN(float64(coordinate)) || math.IsInf(float64(coordinate), 0) {
			return false
		}
	}
	return true
}
