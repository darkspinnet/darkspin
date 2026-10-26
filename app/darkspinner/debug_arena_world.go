//go:build scenario

package main

import "github.com/darkspinnet/darkspin/server/scenario"

type debugWorldArena struct {
	Team           uint16                  `json:"team"`
	WinningTeam    uint32                  `json:"winning_team"`
	IsCombatActive bool                    `json:"is_combat_active"`
	IsResultReady  bool                    `json:"is_result_ready"`
	Members        []debugWorldArenaMember `json:"members"`
}

type debugWorldArenaMember struct {
	DamageDone      float32          `json:"damage_done"`
	DamageTaken     float32          `json:"damage_taken"`
	HealingDone     float32          `json:"healing_done"`
	HealingReceived float32          `json:"healing_received"`
	SwapRemainingMS uint32           `json:"swap_remaining_ms"`
	UserID          uint64           `json:"user_id"`
	Team            uint16           `json:"team"`
	DeployedIndex   uint32           `json:"deployed_index"`
	Characters      []debugWorldHero `json:"characters"`
}

func debugWorldArenaDTO(arena *scenario.WorldArena) *debugWorldArena {
	if arena == nil {
		return nil
	}
	result := &debugWorldArena{Team: arena.Team, WinningTeam: arena.WinningTeam,
		IsCombatActive: arena.IsCombatActive, IsResultReady: arena.IsResultReady,
		Members: make([]debugWorldArenaMember, 0, len(arena.Members))}
	for _, member := range arena.Members {
		memberDTO := debugWorldArenaMember{UserID: member.UserID, Team: member.Team,
			DamageDone: member.DamageDone, DamageTaken: member.DamageTaken,
			HealingDone: member.HealingDone, HealingReceived: member.HealingReceived,
			SwapRemainingMS: member.SwapRemainingMS,
			DeployedIndex:   member.DeployedIndex, Characters: make([]debugWorldHero, 0, len(member.Characters))}
		for _, hero := range member.Characters {
			memberDTO.Characters = append(memberDTO.Characters, debugWorldHero{Index: hero.Index,
				HitPoint: hero.HitPoint, ManaPoint: hero.ManaPoint,
				MaximumHitPoint: hero.MaximumHitPoint, MaximumManaPoint: hero.MaximumManaPoint,
				IsDefeated: hero.IsDefeated})
		}
		result.Members = append(result.Members, memberDTO)
	}
	return result
}
