package combat

import (
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
)

func ResolvePlayer(
	random *sim.SimulatorRandom, damage float32, profile sim.CriticalProfile,
	difficulty uint32, tuning sim.CriticalTuning,
) (sim.CriticalDamageResult, error) {
	result, err := sim.ResolveCriticalDamage(
		random, damage, difficulty, profile, tuning,
	)
	if err != nil {
		return sim.CriticalDamageResult{}, fmt.Errorf("damageResolve: %w", err)
	}
	return result, nil
}

func ResolveNonPlayer(
	random *sim.SimulatorRandom, damage float32, profile sim.CriticalProfile,
	tuning sim.CriticalTuning,
) (sim.CriticalDamageResult, error) {
	result, err := sim.ResolveNonPlayerCriticalDamage(random, damage, profile, tuning)
	if err != nil {
		return sim.CriticalDamageResult{}, fmt.Errorf("damageResolve: %w", err)
	}
	return result, nil
}

func ResolveNPC(
	random *sim.SimulatorRandom, damage float32, noun string,
	profilesByClass map[uint32]sim.CriticalProfile, tuning sim.CriticalTuning,
) (sim.CriticalDamageResult, error) {
	baseName := strings.TrimSuffix(noun, ".Noun")
	profile, isFound := profilesByClass[util.HashID(baseName)]
	if !isFound {
		return sim.CriticalDamageResult{}, fmt.Errorf("criticalProfile: %s", baseName)
	}
	result, err := ResolveNonPlayer(random, damage, profile, tuning)
	if err != nil {
		return sim.CriticalDamageResult{}, fmt.Errorf("enemyCritical: %w", err)
	}
	return result, nil
}
