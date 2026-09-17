package object

import (
	"errors"
	"fmt"
	"slices"

	"github.com/darkspinnet/darkspin/server/game"
)

const ProjectileIDStart = uint32(2000)

type ScriptPlan struct {
	objectID uint32
	object   game.CampaignScriptObject
}

type ScriptPublication struct {
	ObjectID           uint32
	MarkerID           uint32
	NounName           string
	Position           game.Vec3
	Rotation           game.Vec3
	Scale              float32
	IsCollisionEnabled bool
	IsVisible          bool
	AbilityName        string
	UseLimit           int32
}

type UsePublication struct {
	ObjectID    uint32
	MarkerID    uint32
	AbilityName string
	UseCount    int32
	UseLimit    int32
	State       uint32
}

func PlanScripts(
	registry *game.CampaignScriptRegistry, objects []game.CampaignScriptObject, firstObjectID uint32,
) ([]ScriptPlan, uint32, error) {
	if registry == nil {
		return nil, firstObjectID, errors.New("scriptPlanRegistry: nil")
	}
	if firstObjectID == 0 || firstObjectID >= ProjectileIDStart {
		return nil, firstObjectID, fmt.Errorf("scriptPlanFirstID: %d", firstObjectID)
	}
	plans := make([]ScriptPlan, 0)
	nextObjectID := firstObjectID
	for index, object := range objects {
		callbackName := object.InteractableAbility
		if callbackName == "" &&
			slices.Contains(object.CallbackNames, "nLevelObject.OnTreeDeath") {
			callbackName = "nLevelObject.OnTreeDeath"
		}
		if callbackName == "" {
			continue
		}
		if object.InteractableAbility != "" &&
			(object.InteractableUseLimit == 0 || object.InteractableUseLimit < -1 ||
				!slices.Contains(object.CallbackNames, object.InteractableAbility)) {
			return nil, firstObjectID, fmt.Errorf("scriptPlan[%d]: invalid interactable", index)
		}
		if nextObjectID >= ProjectileIDStart {
			return nil, firstObjectID, errors.New("scriptPlanObjectID: exhausted")
		}
		plan := ScriptPlan{objectID: nextObjectID, object: object}
		err := registry.Register(game.CampaignScriptRegistration{
			ObjectID: nextObjectID, MarkerID: object.MarkerID,
			CallbackName: callbackName, Position: object.Position,
			UseLimit: object.InteractableUseLimit, Challenge: object.InteractableChallenge,
		})
		if err != nil {
			return nil, firstObjectID, fmt.Errorf("scriptPlanRegister[%d]: %w", index, err)
		}
		plans = append(plans, plan)
		nextObjectID++
	}
	return plans, nextObjectID, nil
}

func PublishScriptUse(use game.CampaignScriptUse) (UsePublication, error) {
	invocation := use.Invocation
	if invocation.TargetObjectID == 0 || invocation.MarkerID == 0 ||
		invocation.CallbackName == "" || use.UseCount <= 0 || use.UseLimit == 0 {
		return UsePublication{}, errors.New("scriptUse: invalid")
	}
	state := uint32(3)
	if use.UseLimit >= 0 && use.UseCount >= use.UseLimit {
		state = 1
	}
	return UsePublication{
		ObjectID:    invocation.TargetObjectID,
		MarkerID:    invocation.MarkerID,
		AbilityName: invocation.CallbackName,
		UseCount:    use.UseCount,
		UseLimit:    use.UseLimit,
		State:       state,
	}, nil
}

func PublishScript(plan ScriptPlan) (ScriptPublication, error) {
	if plan.objectID == 0 || plan.object.MarkerID == 0 || plan.object.NounName == "" {
		return ScriptPublication{}, errors.New("scriptObject: invalid")
	}
	return ScriptPublication{
		ObjectID:           plan.objectID,
		MarkerID:           plan.object.MarkerID,
		NounName:           plan.object.NounName,
		Position:           plan.object.Position,
		Rotation:           plan.object.Rotation,
		Scale:              plan.object.Scale,
		IsCollisionEnabled: plan.object.IsCollisionEnabled,
		IsVisible:          plan.object.IsVisible,
		AbilityName:        plan.object.InteractableAbility,
		UseLimit:           plan.object.InteractableUseLimit,
	}, nil
}
