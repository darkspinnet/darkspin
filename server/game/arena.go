package game

import "math/rand/v2"

var arenaLevelNames = [...]string{
	"cryos_2_PVP",
	"infinity_1_PVP",
	"nocturna_2_PVP",
	"scaldron_1_PVP",
	"verdanth_3_PVP",
	"zelems_2_PVP",
}

// RandomArenaLevel returns one of the six Arena levels shipped by build 103.
func RandomArenaLevel() string {
	return arenaLevelNames[rand.IntN(len(arenaLevelNames))]
}

// ArenaLevelNames returns the immutable shipped Arena pool in authored-name order.
func ArenaLevelNames() []string {
	levels := make([]string, len(arenaLevelNames))
	copy(levels, arenaLevelNames[:])
	return levels
}
