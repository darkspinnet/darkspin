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
	positionIDs           []uint32
	rotations             []game.Vec3
	radius                float32
	provisionalCount      int
	isProvisionalCaptain  bool
	isAmbush              bool
	navigationComponentID uint32
	provisionalNounNames  []string
	sourceLocusIDs        []uint32
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
	groupChallengeMultiplier float32
	mixedRandom              *mixedGroupRandom
	sectionRosters           []SectionRoster
	inputLoci                []PopulationLocus
	excludedLoci             []PopulationLocus
	recipe                   string
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
	multiplier := director.CompositionTuning.GroupChallengeMultiplier
	if math.IsNaN(float64(multiplier)) || math.IsInf(float64(multiplier), 0) || multiplier < 0 {
		return nil, errors.New("populationCreate: invalid group multiplier")
	}
	director.IsFirstClear = isFirstClear
	sectionRosters, sectionErr := loadSectionRosters(director, sim.NewSimulatorRandom(seed^0x53454354))
	if sectionErr != nil {
		return nil, fmt.Errorf("populationSections: %w", sectionErr)
	}
	volumes := spawnExclusionVolumes(director)
	random := sim.NewSimulatorRandom(seed)
	selectedSpikeSets := make(map[string]int)
	groupWeights := make(map[string]uint32)
	for _, markerSet := range director.MarkerSets {
		if markerSet.GroupName == "" || strings.EqualFold(markerSet.GroupName, "none") ||
			!strings.Contains(strings.ToLower(markerSet.Name), "_ai_spike") {
			continue
		}
		groupWeights[strings.ToLower(markerSet.GroupName)] += markerSet.Weight
	}
	var candidates []candidate
	inputLoci := make([]PopulationLocus, 0)
	excludedLoci := make([]PopulationLocus, 0)
	for _, markerSet := range director.MarkerSets {
		if strings.Contains(strings.ToLower(markerSet.Name), "_ai_horde_") {
			continue
		}
		if !director.IsInitialLayoutSelected &&
			markerSet.GroupName != "" && !strings.EqualFold(markerSet.GroupName, "none") &&
			strings.Contains(strings.ToLower(markerSet.Name), "_ai_spike") {
			groupName := strings.ToLower(markerSet.GroupName)
			roll, isSelected := selectedSpikeSets[groupName]
			if !isSelected {
				weight := groupWeights[groupName]
				if weight == 0 {
					return nil, fmt.Errorf("populationGroupWeight[%s]: zero", groupName)
				}
				selection, err := random.Index(weight)
				if err != nil {
					return nil, fmt.Errorf("populationGroupRoll[%s]: %w", groupName, err)
				}
				roll = int(selection)
			}
			if uint32(roll) >= markerSet.Weight {
				selectedSpikeSets[groupName] = roll - int(markerSet.Weight)
				continue
			}
			selectedSpikeSets[groupName] = -1
		}
		fallbackSection := Section(markerSet.Name)
		spikesBySection := make(map[sim.DirectorRouteSection]*candidate)
		for _, marker := range markerSet.Markers {
			if !marker.IsSpawnKindKnown {
				continue
			}
			section := fallbackSection
			if marker.IsSpawnSectionKnown {
				section = sim.DirectorRouteSection(marker.SpawnSectionType + 1)
			}
			// Sets such as zelems_3_Ai_Wander and Ai_Spike_8a do not
			// encode a route section in their name. Their markers do.
			if section == 0 {
				continue
			}
			switch marker.SpawnKind {
			case 7:
				inputLoci = append(inputLoci, PopulationLocus{ID: marker.MarkerID,
					Kind: sim.DirectorLocusWanderer, Section: section})
				candidate := candidate{
					locusID: marker.MarkerID, kind: sim.DirectorLocusWanderer, section: section,
					markerSetOrdinal: markerSet.Ordinal, markerSetName: markerSet.Name,
					markerOrdinal: marker.Ordinal, positions: []game.Vec3{marker.Position},
					rotations: []game.Vec3{marker.Rotation},
					radius:    sim.LocalWandererRadius, sourceLocusIDs: []uint32{marker.MarkerID},
				}
				candidates = append(candidates, candidate)
			case 8:
				locus := PopulationLocus{ID: marker.MarkerID,
					Kind: sim.DirectorLocusSpike, Section: section}
				inputLoci = append(inputLoci, locus)
				if isSpikeExcluded(marker, volumes) {
					excludedLoci = append(excludedLoci, locus)
					continue
				}
				spike := spikesBySection[section]
				if spike == nil {
					spike = &candidate{
						locusID: marker.MarkerID, kind: sim.DirectorLocusSpike, section: section,
						markerSetOrdinal: markerSet.Ordinal, markerSetName: markerSet.Name,
						markerOrdinal: marker.Ordinal, radius: sim.LocalSpikeRadius,
					}
					spikesBySection[section] = spike
				}
				spike.positions = append(spike.positions, marker.Position)
				spike.positionIDs = append(spike.positionIDs, marker.MarkerID)
				spike.sourceLocusIDs = append(spike.sourceLocusIDs, marker.MarkerID)
				spike.rotations = append(spike.rotations, marker.Rotation)
			}
		}
		for _, section := range []sim.DirectorRouteSection{
			sim.DirectorRouteSectionA, sim.DirectorRouteSectionB,
			sim.DirectorRouteSectionC, sim.DirectorRouteSectionAny,
		} {
			if spike := spikesBySection[section]; spike != nil {
				candidates = append(candidates, *spike)
			}
		}
	}
	recipe := "authored-candidates"
	if strings.EqualFold(director.Level, game.InitialChainLevel) && isFirstClear {
		recipe = "initial-chain-local-plan"
		var planErr error
		candidates, planErr = applyInitialChainPopulationPlan(candidates, director, isFirstClear,
			random, sim.NewSimulatorRandom(seed^0x53454354))
		if planErr != nil {
			return nil, fmt.Errorf("populationInitialPlan: %w", planErr)
		}
	} else {
		recipe = "section-roster-15-per-section"
		var planErr error
		candidates, planErr = applyCampaignSectionPopulation(candidates, director, sectionRosters, random)
		if planErr != nil {
			return nil, fmt.Errorf("populationSectionPlan: %w", planErr)
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
		groupChallengeMultiplier: multiplier,
		mixedRandom:              &mixedGroupRandom{state: seed ^ 0x4d495845},
		sectionRosters:           sectionRosters,
		inputLoci:                inputLoci,
		excludedLoci:             excludedLoci,
		recipe:                   recipe,
		candidates:               candidates, insideStates: make(map[uint32]bool, len(candidates)),
		resolvedDirectorPointIDs: make(map[uint32]bool, len(candidates)),
		enteredComponents:        make(map[uint32]bool, floorComponentCount),
		navigation:               mesh,
		navigationPlanLayer:      navigationPlanLayer,
		floorComponentCount:      floorComponentCount,
		policy:                   policy, spikeHistory: sim.NewLocalSpikeHistory(), random: random,
	}, nil
}

// PrimeOpening resolves standing map population before exploration. Selected
// spike ambushes and horde marker sets remain encounter-triggered.
func (e *Session) PrimeOpening(position game.Vec3) ([]Decision, error) {
	if e == nil {
		return nil, errors.New("populationPrime: nil session")
	}
	if !IsFinitePosition(position) {
		return nil, errors.New("populationPrime: invalid position")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.isOpeningPrimed {
		return nil, nil
	}
	decisions := make([]Decision, 0, len(e.candidates))
	for _, candidate := range e.candidates {
		if e.resolvedDirectorPointIDs[candidate.locusID] {
			continue
		}
		if candidate.kind == sim.DirectorLocusSpike && candidate.isAmbush {
			continue
		}
		decision, err := e.resolveCandidate(candidate, true)
		if err != nil {
			return nil, fmt.Errorf("openingResolve[%d]: %w", candidate.locusID, err)
		}
		decisions = append(decisions, decision)
		e.resolvedDirectorPointIDs[candidate.locusID] = true
	}
	e.isOpeningPrimed = true
	return decisions, nil
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
		decision, err := s.resolveCandidate(candidate, isDoingWell)
		if err != nil {
			return nil, fmt.Errorf("candidateResolve[%d]: %w", candidate.locusID, err)
		}
		decisions = append(decisions, decision)
		s.resolvedDirectorPointIDs[candidate.locusID] = true
	}
	return decisions, nil
}

// resolveCandidate is called with the population session locked.
func (e *Session) resolveCandidate(candidate candidate, isDoingWell bool) (Decision, error) {
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
		return decision, nil
	}
	switch candidate.kind {
	case sim.DirectorLocusWanderer:
		spawnRoll, err := e.random.Index(100)
		if err != nil {
			return Decision{}, fmt.Errorf("populationSpawnRoll: %w", err)
		}
		clumpRoll, err := e.random.Index(100)
		if err != nil {
			return Decision{}, fmt.Errorf("populationClumpRoll: %w", err)
		}
		decision.Wanderer, err = e.policy.ResolveLocalWanderer(
			candidate.locusID, spawnRoll, clumpRoll,
		)
		if err != nil {
			return Decision{}, fmt.Errorf("populationWanderer: %w", err)
		}
	case sim.DirectorLocusSpike:
		outcome, err := e.policy.HitSpike(candidate.locusID, e.spikeHistory, isDoingWell)
		if err != nil {
			return Decision{}, fmt.Errorf("populationSpike: %w", err)
		}
		challenge, err := sim.LocalSpikeChallenge(candidate.section, outcome)
		if err != nil {
			return Decision{}, fmt.Errorf("populationChallenge: %w", err)
		}
		decision.SpikeOutcome = outcome
		decision.Challenge = challenge
	}
	return decision, nil
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
	// The authored path4579 node sits beside the opening SpikeA set.
	X: -173.15, Y: -46.05, Z: 0.088,
}

var initialChainEntryAnchor = game.Vec3{
	X: -123.8707, Y: -151.64705, Z: 10.037109,
}

var initialChainSecondEncounterAnchor = game.Vec3{
	// Visual reference for selecting the nearest authored WandererA point.
	X: -172.795, Y: -63.524, Z: 0.088,
}

type campaignPopulationTheme struct {
	minionNouns     []string
	lieutenantNouns []string
}

var initialChainRepairTheme = campaignPopulationTheme{
	minionNouns:     []string{"ZelemBasicRepair.Noun", "ZelemBasicHybrid.Noun"},
	lieutenantNouns: []string{"NomadWithDrone.Noun"},
}

var initialChainBarracudaTheme = campaignPopulationTheme{
	minionNouns:     []string{"ZelemBasicRanged.Noun", "ZelemBasicRanged.Noun"},
	lieutenantNouns: []string{"ZelemSpecialHaster.Noun", "NomadSnipe.Noun"},
}

// applyInitialChainPopulationPlan selects locations from authored spawn points.
// The roster is authored on first clear; budgets and occupied locations remain
// a local approximation until the native director policy is recovered.
func applyInitialChainPopulationPlan(
	candidates []candidate, director game.CampaignDirector, isFirstClear bool,
	random *sim.SimulatorRandom, sectionRandom *sim.SimulatorRandom,
) ([]candidate, error) {
	if random == nil {
		return nil, errors.New("nil initial population random")
	}
	firstTimeTheme := campaignPopulationTheme{}
	if isFirstClear {
		minionEntries := PoolEntries(director, "minion")
		specialEntries := PoolEntries(director, "special")
		if len(minionEntries) == 0 || len(specialEntries) == 0 {
			return nil, errors.New("first-time roster incomplete")
		}
		firstTimeTheme.minionNouns = make([]string, 0, len(minionEntries))
		for _, entry := range minionEntries {
			firstTimeTheme.minionNouns = append(firstTimeTheme.minionNouns, entry.NounName)
		}
		firstTimeTheme.lieutenantNouns = make([]string, 0, len(specialEntries))
		for _, entry := range specialEntries {
			firstTimeTheme.lieutenantNouns = append(firstTimeTheme.lieutenantNouns, entry.NounName)
		}
	}
	sectionThemes := make(map[sim.DirectorRouteSection]campaignPopulationTheme)
	if isFirstClear && director.Difficulty == 1 && len(director.SectionBuckets) != 4 {
		return nil, fmt.Errorf("difficultyOneSectionBuckets: got %d, want 4", len(director.SectionBuckets))
	}
	if isFirstClear && len(director.SectionBuckets) > 0 {
		var err error
		sectionThemes, err = selectSectionThemes(director.SectionBuckets, firstTimeTheme, sectionRandom)
		if err != nil {
			return nil, fmt.Errorf("sectionThemes: %w", err)
		}
	}
	if !canPlanCampaignFloorPopulation(candidates) {
		if isFirstClear {
			return nil, errors.New("first-time spawn points incomplete")
		}
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
		sectionTheme := firstTimeTheme
		if selectedTheme, isSelected := sectionThemes[section]; isSelected {
			sectionTheme = selectedTheme
		}
		if isFirstClear && section == sim.DirectorRouteSectionA {
			floorPlan, err := planInitialChainFirstClearOpening(
				sectionCandidate, firstTimeTheme, sectionTheme, random,
			)
			if err != nil {
				return nil, fmt.Errorf("openingFloor: %w", err)
			}
			planned = append(planned, floorPlan...)
			continue
		}
		theme := sectionTheme
		if !isFirstClear {
			theme = initialChainRepairTheme
		}
		if !isFirstClear && section != sim.DirectorRouteSectionA {
			themeIndex, err := random.Index(2)
			if err != nil {
				return nil, fmt.Errorf("theme[%d]: %w", section, err)
			}
			if themeIndex == 1 {
				theme = initialChainBarracudaTheme
			}
		}
		var openingAnchor *game.Vec3
		eliteTarget := 2
		if section == sim.DirectorRouteSectionA {
			openingAnchor = &initialChainOpeningAnchor
			// A spans the opening SpikeA/SpikeA2 sets and the later SpikeA3
			// platform. Keep an anchor in each area under the same floor budget.
			eliteTarget = 3
		}
		floorPlan, err := planCampaignFloor(
			sectionCandidate, theme, random,
			CampaignFloorPopulationTarget, eliteTarget, openingAnchor,
			isFirstClear && section == sim.DirectorRouteSectionA,
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

// planInitialChainFirstClearOpening fixes the two encounters visible at the
// entrance to authored points. Later A encounters use the selected first-time
// roster and are published with the rest of the standing floor population.
func planInitialChainFirstClearOpening(
	candidates []candidate, theme campaignPopulationTheme,
	laterTheme campaignPopulationTheme,
	random *sim.SimulatorRandom,
) ([]candidate, error) {
	wanderers := make([]candidate, 0)
	for _, candidate := range candidates {
		if candidate.kind == sim.DirectorLocusWanderer && len(candidate.positions) == 1 {
			wanderers = append(wanderers, candidate)
		}
	}
	if len(wanderers) < 4 {
		return nil, errors.New("opening candidates incomplete")
	}
	repairNoun, repairErr := initialChainFirstClearNoun(theme.minionNouns, "ZelemBasicRepair")
	hybridNoun, hybridErr := initialChainFirstClearNoun(theme.minionNouns, "ZelemBasicHybrid")
	eliteNoun, eliteErr := initialChainFirstClearNoun(theme.lieutenantNouns, "NomadWithDrone")
	if repairErr != nil || hybridErr != nil || eliteErr != nil {
		// The visual reference and its named roster apply to difficulty 1-24.
		return planCampaignFloor(
			candidates, laterTheme, random, CampaignFloorPopulationTarget,
			3, &initialChainOpeningAnchor, false,
		)
	}
	firstMob, remaining := nearestCampaignCandidates(wanderers, initialChainEntryAnchor, 1)
	firstMob[0].provisionalCount = 1
	firstMob[0].provisionalNounNames = []string{repairNoun}
	elite, remaining := nearestCampaignCandidates(
		remaining, initialChainSecondEncounterAnchor, 1,
	)
	elite[0].provisionalCount = 1
	elite[0].isProvisionalCaptain = true
	elite[0].provisionalNounNames = []string{eliteNoun}
	nearby, remaining := nearestCampaignCandidates(remaining, elite[0].positions[0], 2)
	for index := range nearby {
		nearby[index].provisionalCount = 1
		nearby[index].provisionalNounNames = []string{hybridNoun}
	}
	usedIDs := make(map[uint32]bool, 2+len(nearby))
	usedIDs[firstMob[0].locusID] = true
	usedIDs[elite[0].locusID] = true
	for _, candidate := range nearby {
		usedIDs[candidate.locusID] = true
	}
	laterCandidates := make([]candidate, 0, len(candidates)-len(usedIDs))
	for _, candidate := range candidates {
		if usedIDs[candidate.locusID] {
			continue
		}
		laterCandidates = append(laterCandidates, candidate)
	}
	laterPlan, err := planCampaignFloor(
		laterCandidates, laterTheme, random, 11, 2, nil, false,
	)
	if err != nil {
		return nil, fmt.Errorf("openingLaterFloor: %w", err)
	}
	plans := make([]candidate, 0, 1+1+len(nearby)+len(laterPlan))
	plans = append(plans, firstMob[0], elite[0])
	plans = append(plans, nearby...)
	plans = append(plans, laterPlan...)
	return plans, nil
}

func initialChainFirstClearNoun(nouns []string, baseName string) (string, error) {
	baseName = strings.ToLower(baseName)
	for _, noun := range nouns {
		name := strings.TrimSuffix(strings.ToLower(noun), ".noun")
		if name == baseName || strings.HasPrefix(name, baseName+"_") {
			return noun, nil
		}
	}
	return "", fmt.Errorf("roster noun %s missing", baseName)
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
	random *sim.SimulatorRandom, populationTarget, eliteTarget int,
	openingAnchor *game.Vec3, isOpeningFirstClear bool,
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
	if eliteTarget > 0 && len(elitePosition) == 0 {
		return nil, errors.New("no elite spawn points")
	}
	if len(theme.minionNouns) == 0 || (eliteTarget > 0 && len(theme.lieutenantNouns) == 0) {
		return nil, errors.New("floor roster incomplete")
	}
	selectedElite := make([]game.Vec3, 0, eliteTarget)
	if eliteTarget > 0 {
		firstElite := game.Vec3{}
		if openingAnchor != nil {
			firstElite = nearestCampaignPosition(elitePosition, *openingAnchor)
		} else {
			firstEliteIndex, err := random.Index(uint32(len(elitePosition)))
			if err != nil {
				return nil, fmt.Errorf("firstElite: %w", err)
			}
			firstElite = elitePosition[firstEliteIndex]
		}
		selectedElite = append(selectedElite, firstElite)
	}
	for len(selectedElite) < min(eliteTarget, len(elitePosition)) {
		selectedElite = append(selectedElite,
			furthestCampaignPositionFromGroup(elitePosition, selectedElite),
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
		if isOpeningFirstClear {
			spike, isFound := campaignSpikeAt(candidates, position)
			if !isFound {
				return nil, fmt.Errorf("openingSpike[%d]: missing", eliteIndex)
			}
			spike.provisionalCount = 1
			spike.isProvisionalCaptain = true
			spike.isAmbush = true
			spike.provisionalNounNames = []string{theme.lieutenantNouns[lieutenantIndex]}
			plans = append(plans, spike)
			for index, candidate := range nearby {
				candidate.provisionalCount = 1
				candidate.provisionalNounNames = []string{
					theme.minionNouns[index%len(theme.minionNouns)],
				}
				plans = append(plans, candidate)
			}
			spawnedCount += 1 + len(nearby)
			continue
		}
		positions := make([]game.Vec3, 0, 1+len(nearby))
		positions = append(positions, position)
		nounNames := make([]string, 0, 1+len(nearby))
		nounNames = append(nounNames, theme.lieutenantNouns[lieutenantIndex])
		for index, candidate := range nearby {
			positions = append(positions, candidate.positions[0])
			nounNames = append(nounNames, theme.minionNouns[index%len(theme.minionNouns)])
		}
		plans = append(plans, initialChainClusterCandidate(
			nearby[0], positions, nounNames, candidates, nearby, position,
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
			theme.minionNouns[spawnedCount%len(theme.minionNouns)],
		}
		plans = append(plans, candidate)
		spawnedCount++
	}
	return plans, nil
}

func campaignSpikeAt(candidates []candidate, position game.Vec3) (candidate, bool) {
	for _, candidate := range candidates {
		if candidate.kind != sim.DirectorLocusSpike {
			continue
		}
		for index, markerPosition := range candidate.positions {
			if markerPosition != position {
				continue
			}
			candidate.positions = []game.Vec3{position}
			candidate.markerOrdinal += index
			if index < len(candidate.positionIDs) {
				candidate.locusID = candidate.positionIDs[index]
				candidate.positionIDs = []uint32{candidate.locusID}
				candidate.sourceLocusIDs = []uint32{candidate.locusID}
			}
			if index < len(candidate.rotations) {
				candidate.rotations = []game.Vec3{candidate.rotations[index]}
			}
			return candidate, true
		}
	}
	return candidate{}, false
}

func initialChainClusterCandidate(
	anchor candidate, positions []game.Vec3, nounNames []string,
	candidates []candidate, nearby []candidate, elitePosition game.Vec3,
) candidate {
	anchor.positions = append([]game.Vec3(nil), positions...)
	anchor.sourceLocusIDs = make([]uint32, 0, len(nearby)+1)
	if spike, isFound := campaignSpikeAt(candidates, elitePosition); isFound {
		anchor.sourceLocusIDs = append(anchor.sourceLocusIDs, spike.locusID)
	}
	for _, candidate := range nearby {
		anchor.sourceLocusIDs = append(anchor.sourceLocusIDs, candidate.sourceLocusIDs...)
	}
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

func furthestCampaignPositionFromGroup(
	positions, origins []game.Vec3,
) game.Vec3 {
	selected := positions[0]
	selectedDistance := float32(-1)
	for _, position := range positions {
		nearestDistance := float32(math.MaxFloat32)
		for _, origin := range origins {
			deltaX := position.X - origin.X
			deltaY := position.Y - origin.Y
			deltaZ := position.Z - origin.Z
			distance := deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ
			nearestDistance = min(nearestDistance, distance)
		}
		if nearestDistance > selectedDistance {
			selected = position
			selectedDistance = nearestDistance
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
	kindLength := len("wanderer")
	if directorKindIndex < 0 {
		directorKindIndex = strings.Index(name, "wander")
		kindLength = len("wander")
	}
	if directorKindIndex < 0 {
		directorKindIndex = strings.Index(name, "spike")
		kindLength = len("spike")
	}
	if directorKindIndex < 0 {
		return 0
	}
	sectionIndex := directorKindIndex + kindLength
	if sectionIndex >= len(name) {
		return 0
	}
	switch name[sectionIndex] {
	case 'a':
		return sim.DirectorRouteSectionA
	case 'b':
		return sim.DirectorRouteSectionB
	case 'c':
		return sim.DirectorRouteSectionC
	}
	return 0
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
