package contentsqlite

import (
	"context"
	"errors"
	"fmt"

	contentstore "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/attribute"
	"github.com/darkspinnet/darkspin/server/sim"
)

// loadFieldMedicHealthContribution executes the installed build-103 modifier's
// Activate callback through the general Lua attribute binding. Runtime owns its
// recipient and lifetime; this import recovers the authored operation/amount.
func loadFieldMedicHealthContribution(ctx context.Context, store *contentstore.Store) (attribute.Contribution, error) {
	root, err := loadPinnedLuaBytecode(ctx, store, 829,
		"93d97660d49c1df6f4b0af41eac47a36fd40d4540a3121313b23f76a344378bb", "fieldMedicHealth")
	if err != nil {
		return attribute.Contribution{}, fmt.Errorf("healthRoot: %w", err)
	}
	storedModules, err := store.LuaModules(ctx, root.ChunkID)
	if err != nil {
		return attribute.Contribution{}, fmt.Errorf("healthModules: %w", err)
	}
	modules := make(map[string]sim.LuaBytecode, len(storedModules))
	for name, module := range storedModules {
		modules[name] = sim.LuaBytecode{ChunkID: module.ID, SHA256: module.SHA256, Contents: module.Bytecode}
	}
	const recipientRole sim.Role = "fieldMedicRecipient"
	program, err := sim.CompileLuaModifier(sim.LuaModifierInput{
		Root: root, Modules: modules, Role: recipientRole,
		Callback: "Activate", IsYieldAfterWait: true,
	})
	if err != nil {
		return attribute.Contribution{}, fmt.Errorf("healthCompile: %w", err)
	}
	contribution := attribute.Contribution{}
	for _, step := range program.Steps {
		switch instruction := step.(type) {
		case sim.WaitStep:
			continue
		case sim.EmitStep:
			grant, isAttribute := instruction.Intent.(sim.AttributeModifierIntent)
			if !isAttribute || contribution.Handle != 0 || grant.Role != recipientRole ||
				grant.AttributeKind != sim.AttributeMaxHealth || grant.Handle == 0 ||
				grant.Operation != attribute.PercentChange || grant.IsRemoval || grant.Amount < 0 {
				return attribute.Contribution{}, errors.New("unexpected field medic health grant")
			}
			contribution = attribute.Contribution{Handle: grant.Handle, Operation: grant.Operation, Amount: grant.Amount}
		default:
			return attribute.Contribution{}, fmt.Errorf("unexpected field medic health step: %T", step)
		}
	}
	if contribution.Handle == 0 {
		return attribute.Contribution{}, errors.New("missing field medic health grant")
	}
	capacity, err := attribute.Resolve(1, []attribute.Contribution{contribution})
	if err != nil {
		return attribute.Contribution{}, fmt.Errorf("healthResolve: %w", err)
	}
	if capacity <= 0 {
		return attribute.Contribution{}, errors.New("invalid field medic health capacity")
	}
	return contribution, nil
}
