package sim

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const crystalMinimumDifficulty uint32 = 4
const tutorialCrystalDropChance uint32 = 150
const tutorialCrystalChanceRange uint32 = 100
const tutorialCrystalLobHeight = float32(2.5)
const tutorialCrystalLobDuration = 500 * time.Millisecond
const dropLobEpsilon = float32(1.0 / 65536.0)
const crystalDifficultyMinorCount uint32 = 4
const crystalLevelMajorStride uint32 = 10

// CrystalDefinition is one content-owned weighted pickup choice. The content
// adapter supplies these rows; the simulator only applies the recovered
// difficulty and weight rules.
type CrystalDefinition struct {
	NounName     string
	CrystalType  int32
	Rarity       int32
	MinimumLevel uint32
	MaximumLevel uint32
	Weight       uint32
}

// CrystalLevelOffset is one content-owned weighted adjustment to the level
// derived from encounter difficulty.
type CrystalLevelOffset struct {
	Offset int32
	Weight float32
}

// CrystalDropInput supplies the world state which the DropCrystals native
// reads outside Lua. Destinations and stable pickup roles are resolved by the
// owning phase so this operation does not invent collision or allocation data.
type CrystalDropInput struct {
	CurrentDifficulty uint32
	MinimumDifficulty uint32
	MinorStageCount   uint32
	PlayerRole        Role
	SourceRole        Role
	ControlledRole    Role
	PlayerCount       uint32
	SimulationTime    time.Duration
	SourcePosition    Position
	Destinations      []Position
	PickupRoles       []Role
	Definitions       []CrystalDefinition
	LevelOffsets      []CrystalLevelOffset
	RecentTypes       []int32
	RarityMisses      []uint32
	Random            *SimulatorRandom
}

// CrystalSelectionInput materializes one accepted attempt without a chance
// roll or a stage clamp. Its destination is chosen before the attempt.
type CrystalSelectionInput struct {
	Stage           uint32
	MinorStageCount uint32
	Role            Role
	SimulationTime  time.Duration
	SourcePosition  Position
	Destination     Position
	Definitions     []CrystalDefinition
	LevelOffsets    []CrystalLevelOffset
	RecentTypes     []int32
	RarityMisses    []uint32
	Random          *SimulatorRandom
}

// IsCrystalDropStageEligible gates ordinary attempts before their chance draw.
func IsCrystalDropStageEligible(stage uint32, minimumStage uint32) bool {
	if minimumStage == 0 {
		minimumStage = crystalMinimumDifficulty
	}
	return stage >= minimumStage
}

type weightedCrystalDefinition struct {
	definition CrystalDefinition
	weight     float32
}

type CrystalLob struct {
	StartTime              time.Duration
	Duration               time.Duration
	Height                 float32
	PlaneDirection         Position
	PlaneDirectionVelocity float32
	UpLinearParameter      float32
	UpQuadraticParameter   float32
	BounceCount            uint32
	BounceRestitution      float32
	IsGroundCollisionOnly  bool
	IsStopBounceOnCreature bool
}

type CrystalPickupRequest struct {
	Role         Role
	NounName     string
	CrystalType  int32
	CrystalLevel int32
	Rarity       int32
	Position     Position
	Destination  Position
	Lob          CrystalLob
}

type CrystalDropWorldRequest struct {
	PlayerRole          Role
	SourceRole          Role
	ControlledRole      Role
	EffectiveDifficulty uint32
	Pickups             []CrystalPickupRequest
}

// BuildCrystalDropWorldRequest owns tutorial DropCrystals attempts and its
// minimum-stage clamp through pickup creation. Packet publication remains a transport
// concern because build 103 does not contain the original 0x9a/0x94 sender.
func BuildCrystalDropWorldRequest(input CrystalDropInput) (CrystalDropWorldRequest, error) {
	if input.Random == nil || input.PlayerRole == "" || input.SourceRole == "" || input.ControlledRole == "" ||
		input.PlayerCount == 0 || uint32(len(input.Destinations)) != input.PlayerCount ||
		uint32(len(input.PickupRoles)) != input.PlayerCount || input.SimulationTime < 0 {
		return CrystalDropWorldRequest{}, errors.New("invalid crystal drop input")
	}
	if !isFinitePosition(input.SourcePosition) {
		return CrystalDropWorldRequest{}, errors.New("invalid crystal source position")
	}
	minimumDifficulty := input.MinimumDifficulty
	if minimumDifficulty == 0 {
		minimumDifficulty = crystalMinimumDifficulty
	}
	effectiveDifficulty := max(input.CurrentDifficulty, minimumDifficulty)
	request := CrystalDropWorldRequest{
		PlayerRole: input.PlayerRole, SourceRole: input.SourceRole, ControlledRole: input.ControlledRole,
		EffectiveDifficulty: effectiveDifficulty,
		Pickups:             make([]CrystalPickupRequest, 0, input.PlayerCount),
	}
	seenRole := make(map[Role]struct{}, input.PlayerCount)
	for index := uint32(0); index < input.PlayerCount; index++ {
		role := input.PickupRoles[index]
		if role == "" {
			return CrystalDropWorldRequest{}, fmt.Errorf("pickupRole[%d]: empty", index)
		}
		if _, isDuplicate := seenRole[role]; isDuplicate {
			return CrystalDropWorldRequest{}, fmt.Errorf("pickupRole[%d]: duplicate", index)
		}
		seenRole[role] = struct{}{}
		destination := input.Destinations[index]
		if !isFinitePosition(destination) {
			return CrystalDropWorldRequest{}, fmt.Errorf("destination[%d]: invalid", index)
		}
		chance, drawErr := input.Random.Index(tutorialCrystalChanceRange)
		if drawErr != nil {
			return CrystalDropWorldRequest{}, fmt.Errorf("chanceDraw[%d]: %w", index, drawErr)
		}
		if chance >= tutorialCrystalDropChance {
			continue
		}
		pickup, isSelected, selectErr := SelectCrystalPickup(CrystalSelectionInput{
			Stage: effectiveDifficulty, MinorStageCount: input.MinorStageCount, Role: role,
			SimulationTime: input.SimulationTime,
			SourcePosition: input.SourcePosition, Destination: destination,
			Definitions: input.Definitions, LevelOffsets: input.LevelOffsets,
			RecentTypes: input.RecentTypes, RarityMisses: input.RarityMisses,
			Random: input.Random,
		})
		if selectErr != nil {
			return CrystalDropWorldRequest{}, fmt.Errorf("pickupSelect[%d]: %w", index, selectErr)
		}
		if isSelected {
			request.Pickups = append(request.Pickups, pickup)
		}
	}
	return request, nil
}

// SelectCrystalPickup consumes only the level-offset and definition draws.
// Tutorial and ordinary callers own their distinct attempt policies.
func SelectCrystalPickup(req CrystalSelectionInput) (CrystalPickupRequest, bool, error) {
	if req.Random == nil || req.Stage == 0 || req.Role == "" || req.SimulationTime < 0 ||
		!isFinitePosition(req.SourcePosition) || !isFinitePosition(req.Destination) {
		return CrystalPickupRequest{}, false, errors.New("invalid crystal selection input")
	}
	crystalLevel, err := selectCrystalLevel(req.Random, req.Stage, req.LevelOffsets, req.MinorStageCount)
	if err != nil {
		return CrystalPickupRequest{}, false, fmt.Errorf("levelSelect: %w", err)
	}
	definitions, totalWeight, err := eligibleCrystalDefinition(req.Definitions, uint32(crystalLevel))
	if err != nil {
		return CrystalPickupRequest{}, false, fmt.Errorf("definitionSelect: %w", err)
	}
	weightedDefinitions, policyTotalWeight, err := applyCrystalSelectionPolicy(
		definitions, totalWeight, req.RecentTypes, req.RarityMisses,
	)
	if err != nil {
		return CrystalPickupRequest{}, false, fmt.Errorf("selectionPolicy: %w", err)
	}
	// Empty and all-zero pools still consume the native definition draw.
	choice := float32(req.Random.Float64())
	selected, isSelected := selectCrystalDefinition(weightedDefinitions, policyTotalWeight, choice)
	if !isSelected {
		return CrystalPickupRequest{}, false, nil
	}
	lob, err := BuildDropLob(req.SimulationTime, req.SourcePosition, req.Destination)
	if err != nil {
		return CrystalPickupRequest{}, false, fmt.Errorf("pickupLob: %w", err)
	}
	return CrystalPickupRequest{
		Role: req.Role, NounName: selected.NounName,
		CrystalType: selected.CrystalType, CrystalLevel: crystalLevel,
		Rarity: selected.Rarity, Position: req.SourcePosition,
		Destination: req.Destination, Lob: lob,
	}, true, nil
}

// eligibleCrystalDefinition preserves authored order, including disabled rows,
// and sums the native signed integer weights before any float conversion.
func eligibleCrystalDefinition(
	definitions []CrystalDefinition, crystalLevel uint32,
) ([]weightedCrystalDefinition, uint32, error) {
	eligibleDefinitions := make([]weightedCrystalDefinition, 0, len(definitions))
	var totalWeight uint32
	for index, candidate := range definitions {
		if candidate.NounName == "" || candidate.MaximumLevel < candidate.MinimumLevel || candidate.Weight > math.MaxInt32 {
			return nil, 0, fmt.Errorf("definition[%d]: invalid", index)
		}
		if crystalLevel < candidate.MinimumLevel || crystalLevel > candidate.MaximumLevel {
			continue
		}
		if candidate.Weight > math.MaxInt32-totalWeight {
			return nil, 0, errors.New("crystal weight sum overflow")
		}
		totalWeight += candidate.Weight
		eligibleDefinitions = append(eligibleDefinitions, weightedCrystalDefinition{
			definition: candidate, weight: float32(candidate.Weight),
		})
	}
	return eligibleDefinitions, totalWeight, nil
}

// applyCrystalSelectionPolicy is deliberate server policy, separate from the
// native integer-weight pool. It suppresses recent types when a positive-weight
// alternative exists and boosts rarity weights for their recorded misses.
func applyCrystalSelectionPolicy(
	definitions []weightedCrystalDefinition, totalWeight uint32,
	recentTypes []int32, rarityMisses []uint32,
) ([]weightedCrystalDefinition, float32, error) {
	if len(recentTypes) == 0 && len(rarityMisses) == 0 {
		return definitions, float32(totalWeight), nil
	}
	recentTypesByID := make(map[int32]struct{}, len(recentTypes))
	for _, crystalType := range recentTypes {
		recentTypesByID[crystalType] = struct{}{}
	}
	isAlternativeFound := false
	for _, candidate := range definitions {
		_, isRecent := recentTypesByID[candidate.definition.CrystalType]
		if candidate.weight > 0 && !isRecent {
			isAlternativeFound = true
			break
		}
	}
	weightedDefinitions := make([]weightedCrystalDefinition, 0, len(definitions))
	var policyTotalWeight float64
	isAdjusted := false
	for _, candidate := range definitions {
		_, isRecent := recentTypesByID[candidate.definition.CrystalType]
		if isAlternativeFound && isRecent && candidate.weight > 0 {
			isAdjusted = true
			candidate.weight = 0
		}
		rarity := candidate.definition.Rarity
		weightScale := float64(1)
		if rarity >= 0 && int(rarity) < len(rarityMisses) {
			missCount := min(rarityMisses[rarity], uint32(20))
			scalePerMiss := float64(0)
			if rarity == 1 {
				scalePerMiss = 0.12
			} else if rarity >= 2 {
				scalePerMiss = 0.18
			}
			weightScale += float64(missCount) * scalePerMiss
		}
		if candidate.weight > 0 && weightScale != 1 {
			isAdjusted = true
		}
		candidate.weight = float32(float64(candidate.weight) * weightScale)
		if math.IsNaN(float64(candidate.weight)) || math.IsInf(float64(candidate.weight), 0) || candidate.weight < 0 {
			return nil, 0, errors.New("invalid crystal policy weight")
		}
		policyTotalWeight += float64(candidate.weight)
		weightedDefinitions = append(weightedDefinitions, candidate)
	}
	if !isAdjusted {
		return definitions, float32(totalWeight), nil
	}
	if math.IsInf(float64(float32(policyTotalWeight)), 0) {
		return nil, 0, errors.New("crystal policy weight sum overflow")
	}
	return weightedDefinitions, float32(policyTotalWeight), nil
}

func selectCrystalDefinition(
	definitions []weightedCrystalDefinition, totalWeight float32, choice float32,
) (CrystalDefinition, bool) {
	if totalWeight <= 0 {
		return CrystalDefinition{}, false
	}
	var cumulativeWeight float32
	for _, candidate := range definitions {
		increment := float32(candidate.weight / totalWeight)
		cumulativeWeight += increment
		if cumulativeWeight > choice {
			return candidate.definition, true
		}
	}
	return CrystalDefinition{}, false
}

func selectCrystalLevel(
	random *SimulatorRandom, stage uint32, offsets []CrystalLevelOffset, minorStageCount uint32,
) (int32, error) {
	if random == nil || stage == 0 {
		return 0, errors.New("invalid crystal level input")
	}
	if minorStageCount == 0 {
		minorStageCount = crystalDifficultyMinorCount
	}
	major := (stage-1)/minorStageCount + 1
	minor := (stage-1)%minorStageCount + 1
	base := uint64(minor) + uint64(crystalLevelMajorStride)*uint64(major)
	if base > math.MaxInt32 {
		return 0, errors.New("crystal level overflow")
	}
	for index, offset := range offsets {
		if math.IsNaN(float64(offset.Weight)) || math.IsInf(float64(offset.Weight), 0) || offset.Weight < 0 {
			return 0, fmt.Errorf("offset[%d]: invalid", index)
		}
	}
	choice := float32(random.Float64())
	var cumulativeWeight float32
	var selectedOffset int32
	for _, offset := range offsets {
		cumulativeWeight += offset.Weight
		if math.IsInf(float64(cumulativeWeight), 0) {
			return 0, errors.New("crystal offset sum overflow")
		}
		if cumulativeWeight > choice {
			selectedOffset = offset.Offset
			break
		}
	}
	crystalLevel := int64(base) + int64(selectedOffset)
	if crystalLevel < 0 || crystalLevel > math.MaxUint16 {
		return 0, errors.New("crystal level out of range")
	}
	return int32(crystalLevel), nil
}

func BuildDropLob(
	startTime time.Duration, start Position, destination Position,
) (CrystalLob, error) {
	if !isFinitePosition(start) || !isFinitePosition(destination) {
		return CrystalLob{}, errors.New("invalid drop position")
	}
	deltaX := destination.X - start.X
	deltaY := destination.Y - start.Y
	deltaZ := destination.Z - start.Z
	if math.IsNaN(float64(deltaZ)) || math.IsInf(float64(deltaZ), 0) {
		return CrystalLob{}, errors.New("invalid drop displacement")
	}
	planeLength := math.Sqrt(float64(deltaX)*float64(deltaX) + float64(deltaY)*float64(deltaY))
	planeDistance := float32(planeLength)
	if math.IsNaN(float64(planeDistance)) || math.IsInf(float64(planeDistance), 0) {
		return CrystalLob{}, errors.New("invalid plane distance")
	}
	height := tutorialCrystalLobHeight
	if destination.Z > start.Z {
		height = (destination.Z + tutorialCrystalLobHeight) - start.Z
	}
	if math.IsNaN(float64(height)) || math.IsInf(float64(height), 0) {
		return CrystalLob{}, errors.New("invalid drop height")
	}
	speedDistance := planeDistance
	if planeDistance < dropLobEpsilon {
		speedDistance = 1
	}
	durationSecond := float32(tutorialCrystalLobDuration.Seconds())
	speed := speedDistance / durationSecond
	direction := Position{X: deltaX, Y: deltaY}
	if planeLength > float64(dropLobEpsilon) {
		direction.X /= planeDistance
		direction.Y /= planeDistance
	}
	apexDistance := planeDistance * 0.5
	if float32(math.Abs(float64(deltaZ))) >= dropLobEpsilon {
		discriminant := float64(height)*float64(height) - float64(deltaZ)*float64(height)
		if discriminant < 0 {
			return CrystalLob{}, errors.New("invalid apex discriminant")
		}
		apexDistance = float32((float64(height) - math.Sqrt(discriminant)) / float64(deltaZ) * float64(planeDistance))
	}
	if math.IsNaN(float64(apexDistance)) || math.IsInf(float64(apexDistance), 0) {
		return CrystalLob{}, errors.New("invalid apex distance")
	}
	linear := float32(0)
	quadratic := float32(0)
	if apexDistance <= dropLobEpsilon {
		direction = Position{}
		linear = 4 * height
		quadratic = -4 * height
	} else {
		linear = (2 * height) / apexDistance
		quadratic = -height / (apexDistance * apexDistance)
	}
	if math.IsNaN(float64(linear)) || math.IsInf(float64(linear), 0) ||
		math.IsNaN(float64(quadratic)) || math.IsInf(float64(quadratic), 0) {
		return CrystalLob{}, errors.New("invalid drop coefficients")
	}
	return CrystalLob{
		StartTime: startTime, Duration: tutorialCrystalLobDuration, Height: height,
		PlaneDirection:         direction,
		PlaneDirectionVelocity: speed,
		UpLinearParameter:      linear,
		UpQuadraticParameter:   quadratic,
	}, nil
}
