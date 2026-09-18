package ability

import (
	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func NPCDamageMetadata(definition sim.AbilityDefinition) zonenpc.DamageMetadata {
	return zonenpc.DamageMetadata{
		DamageSource:      definition.DamageSource,
		DamageType:        definition.DamageType,
		DescriptorMask:    definition.DescriptorMask,
		IsDamageTypeKnown: definition.IsDamageTypeFound,
	}
}
