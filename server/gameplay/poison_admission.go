package gameplay

import zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"

func nativeNPCPoisonKey(plan zonenpc.AttackPlan) campaignNPCPoisonKey {
	key := campaignNPCPoisonKey{targetObjectID: plan.TargetObjectID, modifierName: plan.Profile.ModifierName}
	switch plan.Profile.ModifierName {
	case "Poison", "RuptionBurn":
		// Build-103 modifiers620/809 inherit CasterUnique from DOT149 and
		// never replace that activation type. Distinct initiators therefore
		// own independent handles, damage snapshots, expiry and cancellation.
		key.sourceObjectID = plan.SourceObjectID
	}
	// Stacks, Unique and unverified families retain their existing target
	// key. In particular NoctFlyerPoison677 explicitly overrides Stacks.
	return key
}
