//go:build scenario

package main

import "github.com/darkspinnet/darkspin/server/scenario"

type debugWorldCampaign struct {
	Members []debugWorldCampaignMember `json:"members"`
}

type debugWorldCampaignMember struct {
	UserID              uint64           `json:"user_id"`
	PeerGeneration      uint64           `json:"peer_generation"`
	TransportGeneration uint64           `json:"transport_generation"`
	ZoneGeneration      uint64           `json:"zone_generation"`
	DeployedIndex       uint32           `json:"deployed_index"`
	IsReady             bool             `json:"is_ready"`
	Characters          []debugWorldHero `json:"characters"`
}

func debugWorldCampaignDTO(campaign *scenario.WorldCampaign) *debugWorldCampaign {
	if campaign == nil {
		return nil
	}
	result := &debugWorldCampaign{Members: make([]debugWorldCampaignMember, 0, len(campaign.Members))}
	for _, member := range campaign.Members {
		memberDTO := debugWorldCampaignMember{UserID: member.UserID,
			PeerGeneration: member.PeerGeneration, TransportGeneration: member.TransportGeneration,
			ZoneGeneration: member.ZoneGeneration, DeployedIndex: member.DeployedIndex, IsReady: member.IsReady,
			Characters: make([]debugWorldHero, 0, len(member.Characters))}
		for _, hero := range member.Characters {
			heroDTO := debugWorldHero{ObjectID: hero.ObjectID, Index: hero.Index, Noun: hero.Noun,
				PassiveKillStack: hero.PassiveKillStack, Position: debugWorldPositionDTO(hero.Position),
				HitPoint: hero.HitPoint, ManaPoint: hero.ManaPoint,
				MaximumHitPoint: hero.MaximumHitPoint, MaximumManaPoint: hero.MaximumManaPoint, IsDefeated: hero.IsDefeated}
			debugWorldHeroMotionDTO(&heroDTO, &hero)
			memberDTO.Characters = append(memberDTO.Characters, heroDTO)
		}
		result.Members = append(result.Members, memberDTO)
	}
	return result
}
