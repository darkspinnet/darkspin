package gameplay

import (
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func npcPolicyDiagnostic(director game.CampaignDirector, plan zonenpc.SpawnPlan) string {
	source := "authored_or_scripted"
	for _, pool := range director.Pools {
		for _, entry := range pool.Entries {
			if !strings.EqualFold(entry.NounName, plan.NounName) {
				continue
			}
			source = "local:" + pool.ConfigKind
			if entry.IsExternalSource {
				source = "external:" + pool.ConfigKind
			}
		}
	}
	for _, entry := range director.OrdinaryAgentEntries {
		if strings.EqualFold(entry.NounName, plan.NounName) {
			source = "external:agent"
		}
	}
	aiID := uint32(0)
	if plan.NPCProfile.AIGraph != nil {
		aiID = plan.NPCProfile.AIGraph.DefinitionID
	}
	return fmt.Sprintf("policy=darkspin stage=%d class=%#x ai=%#x class_rank=%d effective_rank=%d source=%s elite=%t captain=%t boss=%t affixes=%q max_hp=%g", director.Difficulty, plan.NPCProfile.NPCClassInstanceID, aiID, plan.NPCProfile.NPCRank, zonenpc.EffectiveNPCRank(plan), source, plan.IsElite, plan.IsCaptain, plan.IsBoss, plan.BossIdentity.AffixNames, plan.NPCProfile.HitPoint)
}
