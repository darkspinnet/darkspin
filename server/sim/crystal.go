package sim

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const tutorialCrystalMinimumDifficulty uint32 = 4
const tutorialCrystalDropChance uint32 = 150
const tutorialCrystalChanceRange uint32 = 100
const tutorialCrystalLobHeight = float32(2.5)
const tutorialCrystalLobDuration = 500 * time.Millisecond
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
	Random            *SimulatorRandom
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

// BuildCrystalDropWorldRequest applies the recovered DropCrystals native
// policy through pickup creation. Packet publication remains a transport
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
		minimumDifficulty = tutorialCrystalMinimumDifficulty
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
		crystalLevel, levelErr := selectCrystalLevel(input.Random, effectiveDifficulty, input.LevelOffsets)
		if levelErr != nil {
			return CrystalDropWorldRequest{}, fmt.Errorf("levelSelect[%d]: %w", index, levelErr)
		}
		definition, totalWeight, definitionErr := eligibleCrystalDefinition(input.Definitions, uint32(crystalLevel))
		if definitionErr != nil {
			return CrystalDropWorldRequest{}, fmt.Errorf("definitionSelect[%d]: %w", index, definitionErr)
		}
		choice := input.Random.Float64() * totalWeight
		selected := selectCrystalDefinition(definition, choice)
		lob, lobErr := BuildDropLob(input.SimulationTime, input.SourcePosition, destination)
		if lobErr != nil {
			return CrystalDropWorldRequest{}, fmt.Errorf("lob[%d]: %w", index, lobErr)
		}
		request.Pickups = append(request.Pickups, CrystalPickupRequest{
			Role: role, NounName: selected.NounName,
			CrystalType: selected.CrystalType, CrystalLevel: crystalLevel,
			Rarity:   selected.Rarity,
			Position: input.SourcePosition, Destination: destination, Lob: lob,
		})
	}
	return request, nil
}

func eligibleCrystalDefinition(
	definitions []CrystalDefinition, crystalLevel uint32,
) ([]CrystalDefinition, float64, error) {
	eligible := make([]CrystalDefinition, 0, len(definitions))
	var totalWeight uint64
	for index, candidate := range definitions {
		if candidate.NounName == "" || candidate.MaximumLevel < candidate.MinimumLevel {
			return nil, 0, fmt.Errorf("definition[%d]: invalid", index)
		}
		if crystalLevel < candidate.MinimumLevel || crystalLevel > candidate.MaximumLevel || candidate.Weight == 0 {
			continue
		}
		totalWeight += uint64(candidate.Weight)
		if totalWeight > math.MaxUint32 {
			return nil, 0, errors.New("weight overflow")
		}
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		return nil, 0, errors.New("no eligible crystal definition")
	}
	return eligible, float64(totalWeight), nil
}

func selectCrystalDefinition(definitions []CrystalDefinition, choice float64) CrystalDefinition {
	var cumulativeWeight float64
	for _, candidate := range definitions {
		cumulativeWeight += float64(candidate.Weight)
		if cumulativeWeight > choice {
			return candidate
		}
	}
	return definitions[len(definitions)-1]
}

func selectCrystalLevel(
	random *SimulatorRandom, difficulty uint32, offsets []CrystalLevelOffset,
) (int32, error) {
	if random == nil || difficulty == 0 || len(offsets) == 0 {
		return 0, errors.New("invalid crystal level input")
	}
	minor := difficulty % crystalDifficultyMinorCount
	major := difficulty/crystalDifficultyMinorCount + 1
	if minor == 0 {
		minor = crystalDifficultyMinorCount
		major = difficulty / crystalDifficultyMinorCount
	}
	base := uint64(minor) + uint64(crystalLevelMajorStride)*uint64(major)
	if base > math.MaxInt32 {
		return 0, errors.New("crystal level overflow")
	}
	for index, offset := range offsets {
		weight := float64(offset.Weight)
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 {
			return 0, fmt.Errorf("offset[%d]: invalid", index)
		}
	}
	choice := random.Float64()
	var cumulativeWeight float64
	var selectedOffset int32
	for _, offset := range offsets {
		cumulativeWeight += float64(offset.Weight)
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
	deltaX := float64(destination.X - start.X)
	deltaY := float64(destination.Y - start.Y)
	deltaZ := float64(destination.Z - start.Z)
	planeDistance := math.Hypot(deltaX, deltaY)
	if planeDistance <= 0 {
		return CrystalLob{}, errors.New("zero plane distance")
	}
	height := float64(tutorialCrystalLobHeight) + math.Max(deltaZ, 0)
	apexDistance := planeDistance / 2
	if deltaZ != 0 {
		discriminant := height*height - deltaZ*height
		if discriminant < 0 {
			return CrystalLob{}, errors.New("invalid apex discriminant")
		}
		apexDistance = (height - math.Sqrt(discriminant)) / deltaZ * planeDistance
	}
	if apexDistance <= 0 || math.IsNaN(apexDistance) || math.IsInf(apexDistance, 0) {
		return CrystalLob{}, errors.New("invalid apex distance")
	}
	durationSecond := tutorialCrystalLobDuration.Seconds()
	return CrystalLob{
		StartTime: startTime, Duration: tutorialCrystalLobDuration, Height: tutorialCrystalLobHeight,
		PlaneDirection:         Position{X: float32(deltaX / planeDistance), Y: float32(deltaY / planeDistance)},
		PlaneDirectionVelocity: float32(planeDistance / durationSecond),
		UpLinearParameter:      float32(2 * height / apexDistance),
		UpQuadraticParameter:   float32(-height / (apexDistance * apexDistance)),
	}, nil
}
