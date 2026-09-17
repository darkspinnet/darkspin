package game

const abilityDescriptorBasic uint32 = 1 << 0
const abilityDescriptorRange uint32 = 1 << 1

// AbilityAdmissionRange reproduces the RangeIncrease branch in build-103
// sub_9DE710. Basic descriptors never receive the item modifier.
func AbilityAdmissionRange(
	baseRange float32, rangeIncrease float32, descriptorMask uint32,
	isDescriptorFound bool,
) float32 {
	if baseRange <= 0 || rangeIncrease <= 0 || !isDescriptorFound ||
		descriptorMask&abilityDescriptorRange == 0 ||
		descriptorMask&abilityDescriptorBasic != 0 {
		return baseRange
	}
	return baseRange * (1 + rangeIncrease)
}
