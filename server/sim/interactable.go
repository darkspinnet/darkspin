package sim

import (
	"errors"
	"fmt"
	"time"
)

// InteractableProgram projects the authored timing and mutations of an
// interactable ability definition into the simulator's closed intent set.
func InteractableProgram(definition AbilityDefinition, objectRole Role, playerRole Role) (Program, error) {
	if definition.Kind != AbilityKindInteractable || definition.AnimationName == "" ||
		definition.GraphicsState == 0 || definition.ActivationEffectName == "" ||
		definition.HitDelay <= 0 || definition.LootDelay < 0 ||
		definition.ReleaseDelay < definition.HitDelay ||
		definition.FinalWaitDelay < definition.HitDelay+definition.LootDelay {
		return Program{}, fmt.Errorf("definition: %#v", definition)
	}
	if !definition.IsLootDrop && !definition.IsCrystalDrop && !definition.IsOrbDrop {
		return Program{}, errors.New("empty drop selector")
	}
	if definition.IsOrbDrop && (definition.IsLootDrop || definition.IsCrystalDrop) {
		return Program{}, errors.New("mixed orb drop selector")
	}
	if objectRole == "" || playerRole == "" {
		return Program{}, errors.New("empty role")
	}
	finalDelay := definition.FinalWaitDelay - definition.HitDelay - definition.LootDelay
	steps := []Step{
		EmitStep{Intent: AnimationIntent{Role: playerRole, AnimationName: definition.AnimationName}},
		WaitStep{Duration: definition.HitDelay},
		EmitStep{Intent: GraphicsStateIntent{Role: objectRole, State: definition.GraphicsState}},
		EmitStep{Intent: EffectIntent{Role: objectRole, EffectName: definition.ActivationEffectName}},
		EmitStep{Intent: InteractableUseIntent{Role: objectRole, UseCount: 1}},
		WaitStep{Duration: definition.LootDelay},
		EmitStep{Intent: LootDropIntent{
			SourceRole: objectRole, PlayerRole: playerRole,
			IsLoot: definition.IsLootDrop, IsCrystal: definition.IsCrystalDrop, IsOrb: definition.IsOrbDrop,
		}},
	}
	if finalDelay > 0 {
		steps = append(steps, WaitStep{Duration: finalDelay})
	}
	steps = append(steps,
		EmitStep{Intent: PhysicsStateIntent{
			Role: objectRole, CollisionKind: CollisionStatePhysics, IsCollisionEnabled: false,
		}},
		EmitStep{Intent: SequenceCompleteIntent{Role: objectRole}},
	)
	return Program{Provenance: definition.Provenance, Steps: steps}, nil
}

func InteractableDeadlines(definition AbilityDefinition) ([]time.Duration, error) {
	_, err := InteractableProgram(definition, "interactable", "player")
	if err != nil {
		return nil, fmt.Errorf("program: %w", err)
	}
	return []time.Duration{
		definition.HitDelay,
		definition.HitDelay + definition.LootDelay,
		definition.FinalWaitDelay,
	}, nil
}
