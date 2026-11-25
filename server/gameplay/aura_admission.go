package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type luaModifierAdmission interface {
	CanUse(req sim.LuaModifierAdmission) (bool, error)
}

// NPC authority supplies current immunity under its lock. The Lua callback
// reads only this snapshot and game mode; it never calls back into the world.
type luaAuraAdmission struct {
	modifier    luaModifierAdmission
	isChainGame bool
}

func (e luaAuraAdmission) CanUseAura(req zonenpc.AuraImmunitySnapshot) (bool, error) {
	isAccepted, err := e.modifier.CanUse(sim.LuaModifierAdmission{
		ImmuneToSleep: float64(req.ImmuneToSleep), ImmuneToSilence: float64(req.ImmuneToSilence),
		IsChainGame: e.isChainGame,
	})
	if err != nil {
		return false, fmt.Errorf("auraAdmission: %w", err)
	}
	return isAccepted, nil
}
