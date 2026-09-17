package teleport

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sim"
)

func CompileSimulation(
	security sim.LuaBytecode,
	modifier sim.LuaBytecode,
	platformPosition sim.Position,
	destination sim.Position,
) (Simulation, error) {
	if len(security.Contents) == 0 {
		return Simulation{}, errors.New("security teleporter unavailable")
	}
	if len(modifier.Contents) == 0 {
		return Simulation{}, errors.New("teleporter modifier unavailable")
	}
	activation, err := sim.CompileLuaModifier(sim.LuaModifierInput{
		Root: security, Role: SecurityRole,
		Position: platformPosition, Callback: "Activate",
	})
	if err != nil {
		return Simulation{}, fmt.Errorf("activationCompile: %w", err)
	}
	entry, err := sim.CompileLuaModifierTriggerCallback(sim.LuaModifierInput{
		Root: security, Role: SecurityRole,
		Position: platformPosition, Destination: destination,
	}, sim.LuaTriggerEnter, EntrantRole)
	if err != nil {
		return Simulation{}, fmt.Errorf("entryCompile: %w", err)
	}
	teleport, err := sim.CompileLuaModifier(sim.LuaModifierInput{
		Root: modifier, Role: EntrantRole,
		Destination: destination, Callback: "Activate",
	})
	if err != nil {
		return Simulation{}, fmt.Errorf("teleportCompile: %w", err)
	}
	err = validateHandoff(entry, teleport, destination)
	if err != nil {
		return Simulation{}, fmt.Errorf("handoffValidate: %w", err)
	}
	return Simulation{
		Activation: activation,
		Entry:      entry,
		Teleport:   teleport,
	}, nil
}

func validateHandoff(
	entry sim.Program,
	teleport sim.Program,
	destination sim.Position,
) error {
	if len(entry.Steps) != 1 {
		return fmt.Errorf("entryStepCount: %d", len(entry.Steps))
	}
	emit, isEmit := entry.Steps[0].(sim.EmitStep)
	if !isEmit {
		return fmt.Errorf("entryStep: %T", entry.Steps[0])
	}
	req, isRequest := emit.Intent.(sim.ModifierRequestIntent)
	if !isRequest || req.TargetRole != EntrantRole ||
		req.InitiatorRole != SecurityRole ||
		req.ModifierGUID != ModifierGUID ||
		req.Destination != destination || req.Rank != 1 {
		return fmt.Errorf("entryRequest: %#v", emit.Intent)
	}
	if teleport.Provenance.LuaChunkID != ModifierChunkID ||
		teleport.Provenance.BytecodeSHA256 != ModifierSHA256 ||
		teleport.Provenance.FunctionName != "TeleporterModifier.Activate" {
		return fmt.Errorf(
			"teleportIdentity: %d/%s/%s",
			teleport.Provenance.LuaChunkID,
			teleport.Provenance.BytecodeSHA256,
			teleport.Provenance.FunctionName,
		)
	}
	return nil
}
