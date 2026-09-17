package objective

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

const ObeliskID = uint32(0x61c07561)
const LootCrystalsID = uint32(0x53449566)
const finishLevelQuicklyID = uint32(0xff9733ee)
const damageOftenID = uint32(0xac4273f3)
const defeatAllMonstersID = uint32(0xa28485cc)
const hugeDamageID = uint32(0x0478facb)
const CompatibilityInitialStatus = uint8(1)

// Selection is one ordered objective chosen for a zone. Input contains the
// immutable packaged Lua definition plus census operands resolved while the
// zone is constructed.
type Selection struct {
	Input         sim.LuaObjectiveInput
	InitialStatus uint8
}

type Record struct {
	ObjectiveID uint32
	State       [4]uint8
	Token       [4][3]uint32
}

type UpdatePublication struct {
	ObjectiveID uint32
	PlayerIndex uint8
	Medal       uint8
	Token       [3]uint32
}

type Initialization struct {
	Records []Record
	Update  UpdatePublication
}

func SnapshotRecords(state *sim.ObjectiveState) ([]Record, error) {
	if state == nil {
		return nil, errors.New("nil objective state")
	}
	snapshot := state.Snapshot()
	records := make([]Record, len(snapshot))
	for index, objective := range snapshot {
		records[index].ObjectiveID = objective.ObjectiveID
		records[index].State = objective.State
		for playerIndex := range objective.Token {
			for tokenIndex, integer := range objective.Token[playerIndex] {
				records[index].Token[playerIndex][tokenIndex] = uint32(integer)
			}
		}
	}
	return records, nil
}

func LevelScriptObjects(
	director game.CampaignDirector, matchID uint32,
) ([]game.CampaignScriptObject, error) {
	var objects []game.CampaignScriptObject
	var err error
	if strings.EqualFold(director.Level, game.InitialChainLevel) {
		objects, err = director.InitialChainInteractables(matchID)
	} else {
		objects, err = director.CampaignInteractables(matchID)
	}
	if err != nil {
		return nil, fmt.Errorf("levelScriptObjects: %w", err)
	}
	levelObjects, err := director.CampaignCallbackObjects(
		matchID, "nLevelObject.OnTreeDeath",
	)
	if err != nil {
		return nil, fmt.Errorf("levelVisualObjects: %w", err)
	}
	return append(objects, levelObjects...), nil
}

func NewObelisk(
	ctx context.Context, inputs []sim.LuaObjectiveInput, interactableCount int, playerIndex uint8,
) (*sim.ObjectiveState, map[uint32]*sim.LuaObjectiveRuntime, error) {
	if ctx == nil {
		return nil, nil, errors.New("campaign objective: nil context")
	}
	if interactableCount <= 0 {
		return nil, nil, fmt.Errorf("campaign objective count: %d", interactableCount)
	}
	if playerIndex >= 4 {
		return nil, nil, fmt.Errorf("campaign objective player: %d", playerIndex)
	}
	if len(inputs) == 0 {
		return nil, nil, nil
	}
	var objectiveInput *sim.LuaObjectiveInput
	for index := range inputs {
		if inputs[index].ObjectiveID != ObeliskID {
			continue
		}
		if objectiveInput != nil {
			return nil, nil, errors.New("campaign objective: duplicate obelisk inputs")
		}
		current := inputs[index]
		objectiveInput = &current
	}
	if objectiveInput == nil {
		return nil, nil, errors.New("campaign objective: obelisk inputs missing")
	}
	objectiveInput.InteractableCount = interactableCount
	return NewSelected(ctx, []Selection{{
		Input: *objectiveInput, InitialStatus: CompatibilityInitialStatus,
	}}, playerIndex)
}

func NewLevel(
	ctx context.Context, inputs []sim.LuaObjectiveInput,
	objects []game.CampaignScriptObject, playerIndex uint8,
) (*sim.ObjectiveState, map[uint32]*sim.LuaObjectiveRuntime, error) {
	selection, err := Build103CompatibilitySelection(inputs, objects)
	if err != nil {
		return nil, nil, fmt.Errorf("levelObjectiveSelection: %w", err)
	}
	if len(selection) == 0 {
		return nil, nil, nil
	}
	state, runtime, err := NewSelected(ctx, selection, playerIndex)
	if err != nil {
		return nil, nil, fmt.Errorf("levelObjective: %w", err)
	}
	return state, runtime, nil
}

// Build103CompatibilitySelection names the current server-authority fallback
// explicitly. No recovered level binding proves this ordered draw; it remains
// a compatibility policy until content-backed pool selection is available.
func Build103CompatibilitySelection(
	inputs []sim.LuaObjectiveInput, objects []game.CampaignScriptObject,
) ([]Selection, error) {
	interactableCount := LootInteractableCount(objects)
	inputsByID := make(map[uint32]sim.LuaObjectiveInput, len(inputs))
	for index := range inputs {
		objectiveID := inputs[index].ObjectiveID
		if _, isDuplicate := inputsByID[objectiveID]; isDuplicate {
			return nil, fmt.Errorf(
				"campaign objective: duplicate input %#x", objectiveID,
			)
		}
		inputsByID[objectiveID] = inputs[index]
	}
	objectiveIDs := []uint32{
		finishLevelQuicklyID, damageOftenID, ObeliskID,
		defeatAllMonstersID, hugeDamageID,
	}
	selection := make([]Selection, 0, len(objectiveIDs))
	for _, objectiveID := range objectiveIDs {
		if objectiveID == ObeliskID && interactableCount == 0 {
			continue
		}
		selected, isFound := inputsByID[objectiveID]
		if !isFound {
			return nil, fmt.Errorf(
				"campaign objective: input missing %#x", objectiveID,
			)
		}
		if objectiveID == ObeliskID {
			selected.InteractableCount = interactableCount
		}
		selection = append(selection, Selection{
			Input: selected, InitialStatus: CompatibilityInitialStatus,
		})
	}
	return selection, nil
}

func NewSelected(
	ctx context.Context, selection []Selection, playerIndex uint8,
) (*sim.ObjectiveState, map[uint32]*sim.LuaObjectiveRuntime, error) {
	if ctx == nil {
		return nil, nil, errors.New("campaign objective: nil context")
	}
	if playerIndex >= 4 {
		return nil, nil, fmt.Errorf("campaign objective player: %d", playerIndex)
	}
	if len(selection) == 0 {
		return nil, nil, nil
	}
	seed := make([]sim.ObjectiveSeed, 0, len(selection))
	runtime := make(map[uint32]*sim.LuaObjectiveRuntime, len(selection))
	initializers := make([]sim.Program, 0, len(selection))
	for index, selected := range selection {
		objectiveID := selected.Input.ObjectiveID
		if objectiveID == 0 {
			return nil, nil, fmt.Errorf("campaignObjectiveID[%d]: zero", index)
		}
		if _, isDuplicate := runtime[objectiveID]; isDuplicate {
			return nil, nil, fmt.Errorf("campaignObjectiveDuplicate[%d]: %#x", index, objectiveID)
		}
		objectiveRuntime, initializer, err := sim.NewLuaObjectiveRuntime(selected.Input)
		if err != nil {
			return nil, nil, fmt.Errorf("campaignObjectiveRuntime[%d]: %w", index, err)
		}
		objectiveState := [4]uint8{}
		objectiveState[playerIndex] = selected.InitialStatus
		seed = append(seed, sim.ObjectiveSeed{
			ObjectiveID: objectiveID, State: objectiveState,
		})
		runtime[objectiveID] = objectiveRuntime
		initializers = append(initializers, initializer)
	}
	state, err := sim.NewObjectiveState(seed)
	if err != nil {
		return nil, nil, fmt.Errorf("campaignObjectiveState: %w", err)
	}
	for initializerIndex, initializer := range initializers {
		objectiveID := selection[initializerIndex].Input.ObjectiveID
		for stepIndex, step := range initializer.Steps {
			emit, isEmit := step.(sim.EmitStep)
			if !isEmit {
				return nil, nil, fmt.Errorf(
					"campaignObjectiveStep[%d:%d]: %T",
					initializerIndex, stepIndex, step,
				)
			}
			intent, isObjectiveData := emit.Intent.(sim.ObjectiveDataIntent)
			if !isObjectiveData || intent.ObjectiveID != objectiveID {
				return nil, nil, fmt.Errorf(
					"campaignObjectiveIntent[%d:%d]: %T",
					initializerIndex, stepIndex, emit.Intent,
				)
			}
			err = state.SetObjectiveData(ctx, sim.EventMeta{}, intent)
			if err != nil {
				return nil, nil, fmt.Errorf(
					"campaignObjectiveData[%d:%d]: %w",
					initializerIndex, stepIndex, err,
				)
			}
		}
	}
	return state, runtime, nil
}

func InitializationPublication(
	state *sim.ObjectiveState, playerIndex uint8,
) (*Initialization, error) {
	if state == nil {
		return nil, nil
	}
	if playerIndex >= 4 {
		return nil, fmt.Errorf("campaign objective player: %d", playerIndex)
	}
	snapshot := state.Snapshot()
	if len(snapshot) == 0 || len(snapshot) > 255 {
		return nil, errors.New("campaign objective snapshot invalid")
	}
	records, err := SnapshotRecords(state)
	if err != nil {
		return nil, fmt.Errorf("campaignObjectiveRecords: %w", err)
	}
	objective := snapshot[0]
	token := [3]uint32{}
	for tokenIndex, integer := range objective.Token[playerIndex] {
		token[tokenIndex] = uint32(integer)
	}
	return &Initialization{
		Records: records,
		Update: UpdatePublication{
			ObjectiveID: objective.ObjectiveID, PlayerIndex: playerIndex,
			Medal: objective.State[playerIndex], Token: token,
		},
	}, nil
}

func CompletionPublication(state *sim.ObjectiveState) ([]Record, error) {
	if state != nil {
		snapshot := state.Snapshot()
		if len(snapshot) > 255 {
			return nil, fmt.Errorf("campaign objective count: %d", len(snapshot))
		}
		records := make([]Record, 0, len(snapshot))
		for _, objective := range snapshot {
			objectiveRecord := Record{
				ObjectiveID: objective.ObjectiveID,
				State:       objective.State,
			}
			for playerIndex := range objective.Token {
				for tokenIndex, integer := range objective.Token[playerIndex] {
					objectiveRecord.Token[playerIndex][tokenIndex] = uint32(integer)
				}
			}
			records = append(records, objectiveRecord)
		}
		return records, nil
	}
	return nil, nil
}

func LootInteractableCount(objects []game.CampaignScriptObject) int {
	count := 0
	for _, object := range objects {
		if object.InteractableAbility == "InteractWithObelisk" {
			count++
		}
	}
	return count
}
