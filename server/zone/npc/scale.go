package npc

// SpawnObjectScale applies the normal spawn size independently of /scale.
// Authored placement scale is retained; the noun's graphics scale is client-owned.
func (e SpawnPlan) SpawnObjectScale() float32 {
	objectScale := e.PlacementScale
	if objectScale == 0 {
		objectScale = 1
	}
	return objectScale
}

// PermanentBodyScale combines the additive BodyScale attributes from the
// client-authored EliteModifier (chunk 650) and CaptainModifier (chunk 802).
func (e SpawnPlan) PermanentBodyScale() float32 {
	bodyScale := float32(0)
	if e.IsCaptain || e.IsElite || e.IsBoss || e.BossIdentity.HasModifier(EliteModifierName) {
		bodyScale = EliteBonusForType(e.NPCProfile.NPCType).BodyScale
	}
	if e.IsCaptain {
		bodyScale += 1
	}
	return bodyScale
}

// RuntimeObjectScale adds an optional developer preview to the normal spawn size.
func (e SpawnPlan) RuntimeObjectScale() float32 {
	objectScale := e.SpawnObjectScale()
	if e.ScalePreviewMultiplier > 0 {
		objectScale *= e.ScalePreviewMultiplier
	}
	return objectScale
}
