//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type debugNPCCatalog struct {
	ClientID       string                 `json:"client_id"`
	InstanceID     string                 `json:"instance_id"`
	PID            uint32                 `json:"pid"`
	GameID         uint32                 `json:"game_id"`
	ZoneGeneration uint64                 `json:"zone_generation"`
	Level          string                 `json:"level"`
	Difficulty     uint32                 `json:"difficulty"`
	Entries        []debugNPCCatalogEntry `json:"entries"`
}

type debugNPCCatalogEntry struct {
	Noun               string  `json:"noun"`
	HitPoint           float32 `json:"hit_point"`
	Rank               int32   `json:"rank"`
	AbilityName        string  `json:"ability_name"`
	Family             string  `json:"family"`
	TargetKind         string  `json:"target_kind"`
	Range              float32 `json:"range"`
	MinimumRange       float32 `json:"minimum_range"`
	IsActionRecovered  bool    `json:"is_action_recovered"`
	IsControlSupported bool    `json:"is_control_supported"`
	IsFinalBoss        bool    `json:"is_final_boss"`
}

func readDebugNPCCatalog(ctx context.Context, host *debugHost, clientManager *debugClientManager, payload json.RawMessage) (debugNPCCatalog, error) {
	result := debugNPCCatalog{Entries: []debugNPCCatalogEntry{}}
	var req debugClientRequest
	err := decodeDebugPayload(payload, &req)
	if err != nil {
		return result, fmt.Errorf("catalogRequest: %w", err)
	}
	owner, err := observeDebugWorldOwner(ctx, host, clientManager, req.ClientID)
	if err != nil {
		return result, fmt.Errorf("catalogOwner: %w", err)
	}
	if owner.profile.GameID == 0 || !owner.profile.IsGameplayActive {
		return result, errors.New("NPC catalog requires an admitted live dungeon")
	}
	catalog, err := host.server.DebugNPCCatalog(ctx, owner.client.UserID, owner.profile.GameID)
	if err != nil {
		return result, fmt.Errorf("catalogRead: %w", err)
	}
	err = recheckDebugWorldOwner(ctx, host, clientManager, owner)
	if err != nil {
		return result, fmt.Errorf("catalogRecheck: %w", err)
	}
	result.ClientID, result.InstanceID, result.PID = owner.client.ClientID, owner.client.InstanceID, owner.client.ProcessID
	result.GameID, result.ZoneGeneration, result.Level, result.Difficulty = catalog.GameID, catalog.ZoneGeneration, catalog.Level, catalog.Difficulty
	for _, entry := range catalog.Entries {
		result.Entries = append(result.Entries, debugNPCCatalogEntry{
			Noun: entry.Noun, HitPoint: entry.HitPoint, Rank: entry.Rank,
			AbilityName: entry.AbilityName, Family: entry.Family,
			TargetKind:         entry.TargetKind,
			Range:              entry.Range,
			MinimumRange:       entry.MinimumRange,
			IsActionRecovered:  entry.IsActionRecovered,
			IsControlSupported: entry.IsControlSupported, IsFinalBoss: entry.IsFinalBoss,
		})
	}
	return result, nil
}
