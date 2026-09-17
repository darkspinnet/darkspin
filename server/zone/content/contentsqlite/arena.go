package contentsqlite

import (
	"context"
	"fmt"

	contentstore "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonecontent "github.com/darkspinnet/darkspin/server/zone/content"
)

func loadArenaLevels(
	ctx context.Context, store *contentstore.Store,
) (map[string]zonecontent.ArenaLevel, error) {
	markers, err := store.ArenaSpawnMarkers(ctx)
	if err != nil {
		return nil, fmt.Errorf("spawnLoad: %w", err)
	}
	levels := make(map[string]zonecontent.ArenaLevel)
	for _, levelName := range game.ArenaLevelNames() {
		levels[levelName] = zonecontent.ArenaLevel{}
	}
	for _, marker := range markers {
		level, isFound := levels[marker.LevelName]
		if !isFound {
			continue
		}
		group := arenaSpawnGroup(marker.NounName)
		if group == 0 {
			continue
		}
		spawn := zonecontent.ArenaSpawn{
			Position: sim.Position{
				X: marker.PositionX, Y: marker.PositionY, Z: marker.PositionZ,
			},
			RotationDegrees: marker.RotationDegrees,
		}
		level.SpawnGroups[group-1] = append(level.SpawnGroups[group-1], spawn)
		levels[marker.LevelName] = level
	}
	for _, levelName := range game.ArenaLevelNames() {
		level := levels[levelName]
		for groupIndex, spawns := range level.SpawnGroups {
			if len(spawns) == 0 {
				return nil, fmt.Errorf(
					"spawnGroup[%s/%d]: empty", levelName, groupIndex+1,
				)
			}
		}
	}
	return levels, nil
}

func arenaSpawnGroup(nounName string) int {
	switch nounName {
	case "SpawnPoint_Team1.Noun":
		return 1
	case "SpawnPoint_Team2.Noun":
		return 2
	case "SpawnPoint_Team3.Noun":
		return 3
	case "SpawnPoint_Team4.Noun":
		return 4
	default:
		return 0
	}
}
