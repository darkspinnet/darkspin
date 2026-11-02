package npc

import "time"

type BasicModifierKind uint8

const (
	BasicModifierBurn BasicModifierKind = iota + 1
	BasicModifierEnergyVulnerability
	BasicModifierHaste
	BasicModifierWeakness
	BasicModifierPummel
	BasicModifierKnockback
)

type BasicModifierKey struct {
	Kind           BasicModifierKind
	SourceObjectID uint32
}

type BasicModifier struct {
	Key         BasicModifierKey
	StackCount  uint32
	ExpiresAt   time.Time
	ActiveUntil time.Time
	Rank        uint32
}

// Build 103 Modifiers/0x9B80AB0C.lua owns both the jump and its caster-unique
// immunity marker. Share this profile between NPC and player recipients.
func BasicKnockbackProfile() ActionProfile {
	return ActionProfile{
		ModifierName: "KnockbackWithImmunityModifier", ModifierDuration: 10 * time.Second,
		ForcedMovementSpeed: 25, ForcedMovementDistance: 3,
		ForcedMovementReactionName: "react_knockback",
	}
}

func (e BasicModifier) IsDebuff() bool {
	return e.Key.Kind == BasicModifierBurn || e.Key.Kind == BasicModifierEnergyVulnerability || e.Key.Kind == BasicModifierWeakness
}

func (e *Session) ApplyBasicModifier(objectID uint32, key BasicModifierKey, rank uint32, at time.Time) (BasicModifier, bool) {
	if e == nil || objectID == 0 || at.IsZero() {
		return BasicModifier{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[objectID]
	if !isFound || npc.IsDefeated || npc.HitPoint <= 0 || (key.Kind != BasicModifierHaste && isStatusBlockedByShield(npc)) {
		return BasicModifier{}, false
	}
	previous := npc.status.basicModifiers[key]
	current := previous
	isActive := at.Before(previous.ExpiresAt)
	if isActive && key.Kind != BasicModifierBurn && key.Kind != BasicModifierWeakness {
		if key.Kind != BasicModifierEnergyVulnerability && key.Kind != BasicModifierHaste {
			return previous, false
		}
		if current.StackCount >= 5 {
			return previous, false
		}
		current.StackCount++
	} else {
		current = BasicModifier{Key: key, StackCount: 1, Rank: rank}
	}
	duration := 4 * time.Second
	switch key.Kind {
	case BasicModifierBurn:
	case BasicModifierEnergyVulnerability:
		duration = 3 * time.Second
	case BasicModifierHaste:
		duration = 20 * time.Second
	case BasicModifierWeakness:
	case BasicModifierPummel:
		duration = 10 * time.Second
	case BasicModifierKnockback:
		duration = BasicKnockbackProfile().ModifierDuration
	default:
		return BasicModifier{}, false
	}
	current.ExpiresAt = at.Add(duration)
	current.ActiveUntil = current.ExpiresAt
	if key.Kind == BasicModifierPummel {
		// PummelDebuff's ten-second marker admits independently of its
		// PummelSilence child, whose CanUseModifier checks ImmuneToSilence.
		current.ActiveUntil = time.Time{}
		if !npc.HasAffixModifier(unstoppableModifierName) {
			silenceDuration := 2500 * time.Millisecond
			if current.Rank >= 2 {
				silenceDuration = 1500 * time.Millisecond
			}
			current.ActiveUntil = at.Add(CrowdControlDuration(npc.Plan, silenceDuration))
			if npc.status.silenceExpiresAt.Before(current.ActiveUntil) {
				npc.status.silenceExpiresAt = current.ActiveUntil
			}
		}
	}
	if npc.status.basicModifiers == nil {
		npc.status.basicModifiers = make(map[BasicModifierKey]BasicModifier)
	}
	npc.status.basicModifiers[key] = current
	if key.Kind == BasicModifierHaste {
		npc.refreshHasteProfile(at)
	}
	e.npcs[objectID] = npc
	return current, true
}

func (e *Session) BasicModifier(objectID uint32, key BasicModifierKey) BasicModifier {
	if e == nil {
		return BasicModifier{}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	npc, isFound := e.npcs[objectID]
	if !isFound || npc.IsDefeated || npc.HitPoint <= 0 {
		return BasicModifier{}
	}
	return npc.status.basicModifiers[key]
}

func (e *Session) RemoveBasicModifier(objectID uint32, expected BasicModifier) {
	e.RestoreBasicModifier(objectID, expected, BasicModifier{})
}

func (e *Session) RestoreBasicModifier(objectID uint32, expected BasicModifier, previous BasicModifier) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	npc, isFound := e.npcs[objectID]
	if !isFound || npc.status.basicModifiers[expected.Key] != expected {
		return
	}
	delete(npc.status.basicModifiers, expected.Key)
	if previous.StackCount > 0 {
		npc.status.basicModifiers[expected.Key] = previous
	}
	if expected.Key.Kind == BasicModifierPummel && npc.status.silenceExpiresAt == expected.ActiveUntil {
		npc.status.silenceExpiresAt = previous.ActiveUntil
	}
	if expected.Key.Kind == BasicModifierHaste {
		npc.refreshHasteProfile(time.Now())
	}
	e.npcs[objectID] = npc
}

func (e status) basicEnergyIncrease(at time.Time) float32 {
	modifier := e.basicModifiers[BasicModifierKey{Kind: BasicModifierEnergyVulnerability}]
	if !at.Before(modifier.ExpiresAt) {
		return 0
	}
	return .1 * float32(modifier.StackCount)
}

func (e status) basicWeakness(at time.Time) float32 {
	modifier := e.basicModifiers[BasicModifierKey{Kind: BasicModifierWeakness}]
	if !at.Before(modifier.ExpiresAt) {
		return 0
	}
	if modifier.Rank >= 2 {
		return .15
	}
	return .25
}

func (e status) basicHaste(at time.Time) float32 {
	modifier := e.basicModifiers[BasicModifierKey{Kind: BasicModifierHaste}]
	if !at.Before(modifier.ExpiresAt) {
		return 0
	}
	return .08 * float32(modifier.StackCount)
}

func (e *Snapshot) refreshHasteProfile(at time.Time) {
	basicHaste := e.status.basicHaste(at)
	attackSpeed, cooldownReduction, movementSpeed := basicHaste, float32(0), basicHaste
	if at.Before(e.status.hasteExpiresAt) {
		attackSpeed += e.status.hasteAttackSpeed
		cooldownReduction = e.status.hasteCooldownReduction
		movementSpeed += e.status.hasteMovementSpeedBuff
	}
	if !e.status.isHasteProfileStored {
		basePlan := e.Plan
		basePlan.AffixAttackSpeedBonus = 0
		basePlan.AffixCooldownReductionBonus = 0
		basePlan.AffixMovementSpeedBuff = 0
		profile, isFound := ActionProfileForPlan(basePlan)
		if isFound {
			e.status.hasteActionProfile = profile
			e.status.isHasteActionKnown = e.Plan.IsActionKnown
			e.status.isHasteProfileStored = true
		}
	}
	if e.status.isHasteProfileStored {
		profile := e.status.hasteActionProfile
		profile.HitDelay = time.Duration(float64(profile.HitDelay) / float64(1+attackSpeed))
		profile.ReleaseDelay = time.Duration(float64(profile.ReleaseDelay) / float64(1+attackSpeed))
		profile.Cooldown = time.Duration(float64(profile.Cooldown) / float64(1+attackSpeed) * float64(1-cooldownReduction))
		e.Plan.ActionProfile = profile
		e.Plan.IsActionKnown = true
	}
	e.Plan.MovementSpeedBuff = movementSpeed
}
