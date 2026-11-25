package contentsqlite

import (
	"context"
	"fmt"

	contentstore "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/sim"
)

func loadStrafeLua(ctx context.Context, store *contentstore.Store, chunkID int64, sha256 string) (sim.LuaAbilityInput, error) {
	root, err := loadPinnedLuaBytecode(ctx, store, chunkID, sha256, "strafe")
	if err != nil {
		return sim.LuaAbilityInput{}, fmt.Errorf("strafeRoot: %w", err)
	}
	storedModules, err := store.LuaModules(ctx, root.ChunkID)
	if err != nil {
		return sim.LuaAbilityInput{}, fmt.Errorf("strafeModules: %w", err)
	}
	modules := make(map[string]sim.LuaBytecode, len(storedModules))
	for name, module := range storedModules {
		modules[name] = sim.LuaBytecode{ChunkID: module.ID, SHA256: module.SHA256, Contents: module.Bytecode}
	}
	return sim.LuaAbilityInput{Root: root, Modules: modules}, nil
}
