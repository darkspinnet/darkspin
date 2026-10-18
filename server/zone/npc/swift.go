package npc

// Swift's indexed Lua (chunk 157) adds MovementSpeedBuff=1. Its
// UniqueIrreplaceable activation prevents direct and aura instances stacking.
const SwiftModifierName = "Swift_NPCAffixModifier"

const swiftAuraModifierName = "Aura_Swift_NPCAffixModifier"

func EffectiveMovementSpeedBuff(plan SpawnPlan) float32 {
	bonus := plan.MovementSpeedBuff
	if plan.BossIdentity.HasModifier(SwiftModifierName) || plan.SwiftAuraSourceObjectID != 0 {
		bonus++
	}
	return bonus
}

type SwiftChange struct {
	Target                 Snapshot
	PreviousSourceObjectID uint32
	MovementSpeedBuff      float32
}

// RefreshSwift retains one supplying source until it leaves. Other overlapping
// sources can take over without multiplying the UniqueIrreplaceable bonus.
// Aura group chunk 283 uses ranked radius 12/16/20 and friendly non-destructibles.
// Server polling supplies proximity contacts; the native first-Tick resume and
// precise collider contact ordering remain unrecovered.
func (e *Session) RefreshSwift() []SwiftChange {
	e.mu.Lock()
	defer e.mu.Unlock()
	sources := make([]Snapshot, 0)
	for _, objectID := range e.objectIDs {
		source := e.npcs[objectID]
		if isSwiftActorActive(source) && source.Plan.BossIdentity.HasModifier(swiftAuraModifierName) {
			sources = append(sources, source)
		}
	}
	changes := make([]SwiftChange, 0)
	for _, objectID := range e.objectIDs {
		target := e.npcs[objectID]
		previousSourceID := target.Plan.SwiftAuraSourceObjectID
		sourceID := uint32(0)
		if isSwiftActorActive(target) && !target.Plan.IsFixture && target.Plan.NPCProfile.NPCType != 3 &&
			!target.Plan.BossIdentity.HasModifier(SwiftModifierName) {
			for _, source := range sources {
				if source.Faction != target.Faction {
					continue
				}
				rank := max(int32(1), min(int32(3), source.Plan.NPCProfile.NPCRank))
				radius := float32(8 + 4*rank)
				if target.Plan.Position.Sub(source.Plan.Position).Length() > radius {
					continue
				}
				if sourceID == 0 || source.Plan.ObjectID == previousSourceID {
					sourceID = source.Plan.ObjectID
				}
				if sourceID == previousSourceID {
					break
				}
			}
		}
		target.Plan.SwiftAuraSourceObjectID = sourceID
		bonus := float32(0)
		if isSwiftActorActive(target) {
			bonus = EffectiveMovementSpeedBuff(target.Plan)
		}
		if previousSourceID == sourceID && target.status.swiftMovementSpeedBuff == bonus {
			continue
		}
		target.status.swiftMovementSpeedBuff = bonus
		e.npcs[objectID] = target
		changes = append(changes, SwiftChange{
			Target: target, PreviousSourceObjectID: previousSourceID, MovementSpeedBuff: bonus,
		})
	}
	return changes
}

func isSwiftActorActive(actor Snapshot) bool {
	return actor.IsPublished && !actor.IsDefeated && actor.HitPoint > 0 && !actor.Plan.IsIntroductionHidden
}
