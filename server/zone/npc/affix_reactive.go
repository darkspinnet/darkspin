package npc

// ThornsDamage is the retaliation requested by the shipped Thorns affix.
// The transport applies it to the attacker after publishing the incoming hit.
type ThornsDamage struct {
	SourceObjectID uint32
	TargetObjectID uint32
	Damage         float32
	DamageType     uint32
	DamageSource   uint32
	DescriptorMask uint32
}

const (
	thornsMeleeDescriptor            = uint32(1)
	thornsNoHitReactDescriptor       = uint32(1 << 11)
	thornsIgnoreDifficultyDescriptor = uint32(1 << 15)
	thornsReflectionDescriptor       = uint32(1 << 18)
)

// thornsDamage follows Thorns_NPCAffixModifier.HandleEvent: only a melee
// TookDamage event from a valid attacker reflects the event's damage amount.
func thornsDamage(npc Snapshot, sourceObjectID uint32, damage float32,
	meta DamageMetadata, difficulty uint32,
) *ThornsDamage {
	if meta.isForcedDefeat || damage <= 0 || sourceObjectID == 0 ||
		sourceObjectID == npc.Plan.ObjectID ||
		meta.DescriptorMask&thornsMeleeDescriptor == 0 ||
		meta.DescriptorMask&thornsReflectionDescriptor != 0 ||
		!npc.HasAffixModifier("Thorns_NPCAffixModifier") {
		return nil
	}
	// The shipped TookDamage handler selects this table by major difficulty,
	// independent of the modifier's ability rank. Campaign difficulty stores
	// four minor stages per major level.
	majorDifficulty := (difficulty + 3) / 4
	percent := float32(50)
	if majorDifficulty <= 6 {
		percent = 20
	} else if majorDifficulty <= 12 {
		percent = 35
	}
	return &ThornsDamage{
		SourceObjectID: npc.Plan.ObjectID,
		TargetObjectID: sourceObjectID,
		Damage:         damage * percent / 100,
		DamageType:     2, // Life
		DamageSource:   0, // Physical
		DescriptorMask: thornsNoHitReactDescriptor |
			thornsIgnoreDifficultyDescriptor | thornsReflectionDescriptor,
	}
}

// AffixResurrectionFraction is the maximum-health fraction restored by the
// Persistent affix's rezzing child or the Persistent aura's Resurrect child.
// The no-resurrect child makes either path one-shot for an actor lifetime.
func AffixResurrectionFraction(npc Snapshot) float32 {
	if npc.IsAffixResurrectionTriggered {
		return 0
	}
	if npc.HasAffixModifier("Persistent_NPCAffixModifier") {
		return 0.5
	}
	if npc.HasAffixModifier("ResurrectModifier") {
		rank := npc.Plan.AffixRank("ResurrectModifier")
		return [...]float32{0.4, 0.6, 0.8}[rank-1]
	}
	return 0
}

// affixResurrectionFraction validates the aura child at the instant of the
// lethal hit. The transition revives synchronously under the gameplay session
// lock, so an accepted revival cannot be stranded by a later aura refresh.
func (s *Session) affixResurrectionFraction(npc Snapshot) float32 {
	fraction := AffixResurrectionFraction(npc)
	if fraction == 0 || npc.HasAffixModifier("Persistent_NPCAffixModifier") ||
		npc.Plan.BossIdentity.HasModifier("ResurrectModifier") {
		return fraction
	}
	for _, aura := range npc.Plan.AffixAuras {
		if aura.ModifierName != "ResurrectModifier" {
			continue
		}
		source, isSourceFound := s.npcs[aura.SourceObjectID]
		if !isSourceFound || !source.IsPublished || source.IsDefeated ||
			source.Plan.NounName == npc.Plan.NounName ||
			source.Plan.Position.Sub(npc.Plan.Position).Length() > 11 {
			return 0
		}
		return fraction
	}
	return 0
}
