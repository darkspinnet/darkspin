package npc

import (
	"errors"
	"fmt"
	"time"
)

type status struct {
	areaShiftExpiresAt       time.Time
	absorptionShieldAmount   float32
	absorptionShieldMaximum  float32
	absorptionShieldReadyAt  time.Time
	absorptionShieldRecharge time.Duration
	carapaceDamageMaximum    float32
	banishExpiresAt          time.Time
	curseExpiresAt           time.Time
	curseDamage              CurseDamageProfile
	damageTakenIncrease      float32
	damageVulnerabilityOwner uint32
	physicalTakenIncrease    float32
	physicalVulnerabilityEnd time.Time
	damageReduction          float32
	damageReductionEnd       time.Time
	isDamageFleePending      bool
	energyTakenIncrease      float32
	energyVulnerabilityOwner uint32
	energyVulnerabilityEnd   time.Time
	healingReduction         float32
	healingReductionEnd      time.Time
	hasteExpiresAt           time.Time
	hasteAttackSpeed         float32
	hasteCooldownReduction   float32
	hasteMovementSpeedBuff   float32
	hasteActionProfile       ActionProfile
	isHasteActionKnown       bool
	isHasteProfileStored     bool
	energyBuffExpiresAt      time.Time
	energyDamageIncrease     float32
	fearExpiresAt            time.Time
	intangibleExpiresAt      time.Time
	chargeProtectionEnd      time.Time
	munchExpiresAt           time.Time
	munchDamageIncrease      float32
	munchStack               uint32
	passiveDamageIncrease    float32
	passiveEnrageStack       uint32
	oozeBaseGraphicsScale    float32
	oozeBaseMaximumHitPoint  float32
	oozeGrowthDamageIncrease float32
	oozeGrowthStack          uint32
	corpseConsumerCount      uint32
	isCorpseFading           bool
	isCorpseFadePublished    bool
	isStealthed              bool
	rootExpiresAt            time.Time
	silenceExpiresAt         time.Time
	sleepExpiresAt           time.Time
	slowExpiresAt            time.Time
	slowMovementScale        float32
	slowAttackScale          float32
	stunExpiresAt            time.Time
	tauntExpiresAt           time.Time
	tauntTargetObjectID      uint32
}

func (s *Session) HasDebuff(objectID uint32, at time.Time) bool {
	if s == nil || objectID == 0 || at.IsZero() {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsPublished {
		return false
	}
	status := npc.status
	return at.Before(status.banishExpiresAt) ||
		at.Before(status.curseExpiresAt) ||
		status.damageTakenIncrease > 0 ||
		at.Before(status.physicalVulnerabilityEnd) ||
		at.Before(status.energyVulnerabilityEnd) ||
		at.Before(status.healingReductionEnd) ||
		at.Before(status.fearExpiresAt) ||
		at.Before(status.intangibleExpiresAt) ||
		at.Before(status.rootExpiresAt) ||
		at.Before(status.silenceExpiresAt) ||
		at.Before(status.sleepExpiresAt) ||
		at.Before(status.slowExpiresAt) ||
		at.Before(status.stunExpiresAt) ||
		at.Before(status.tauntExpiresAt)
}

func (s *Session) PurgeDebuffs(objectID uint32) bool {
	if s == nil || objectID == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsPublished {
		return false
	}
	status := &npc.status
	isPurged := !status.banishExpiresAt.IsZero() ||
		!status.curseExpiresAt.IsZero() ||
		status.damageTakenIncrease > 0 ||
		!status.energyVulnerabilityEnd.IsZero() ||
		!status.healingReductionEnd.IsZero() ||
		!status.fearExpiresAt.IsZero() ||
		!status.intangibleExpiresAt.IsZero() ||
		!status.rootExpiresAt.IsZero() ||
		!status.silenceExpiresAt.IsZero() ||
		!status.sleepExpiresAt.IsZero() ||
		!status.slowExpiresAt.IsZero() ||
		!status.stunExpiresAt.IsZero() ||
		!status.tauntExpiresAt.IsZero()
	if !isPurged {
		return false
	}
	status.banishExpiresAt = time.Time{}
	status.curseExpiresAt = time.Time{}
	status.curseDamage = CurseDamageProfile{}
	status.damageTakenIncrease = 0
	status.damageVulnerabilityOwner = 0
	status.energyTakenIncrease = 0
	status.energyVulnerabilityOwner = 0
	status.energyVulnerabilityEnd = time.Time{}
	status.healingReduction = 0
	status.healingReductionEnd = time.Time{}
	status.fearExpiresAt = time.Time{}
	status.intangibleExpiresAt = time.Time{}
	status.rootExpiresAt = time.Time{}
	status.silenceExpiresAt = time.Time{}
	status.sleepExpiresAt = time.Time{}
	status.slowExpiresAt = time.Time{}
	status.slowMovementScale = 0
	status.slowAttackScale = 0
	status.stunExpiresAt = time.Time{}
	status.tauntExpiresAt = time.Time{}
	status.tauntTargetObjectID = 0
	s.npcs[objectID] = npc
	return true
}

func (s *Session) ApplyStealth(objectID uint32) error {
	if s == nil || objectID == 0 {
		return errors.New("invalid npc stealth")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsPublished || npc.Plan.IsFixture {
		return fmt.Errorf("npcStealthMissing: %d", objectID)
	}
	npc.status.isStealthed = true
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) ClearStealth(objectID uint32) {
	if s == nil || objectID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || !npc.status.isStealthed {
		return
	}
	npc.status.isStealthed = false
	s.npcs[objectID] = npc
}

func (s *Session) IsStealthed(objectID uint32) bool {
	if s == nil || objectID == 0 {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	return isFound && !npc.IsDefeated && npc.IsPublished && npc.status.isStealthed
}

func (s *Session) ApplyTaunt(
	objectID uint32, target Target, expiresAt time.Time,
) (Snapshot, error) {
	if s == nil || objectID == 0 || target.ObjectID == 0 || !target.IsAlive ||
		target.Faction == FactionUnknown || expiresAt.IsZero() {
		return Snapshot{}, errors.New("invalid npc taunt")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsPublished || npc.Plan.IsFixture ||
		npc.Faction == target.Faction {
		return Snapshot{}, fmt.Errorf("npcTauntMissing: %d", objectID)
	}
	npc.TargetObjectID = target.ObjectID
	npc.TargetFaction = target.Faction
	npc.TargetOwner = target.Owner
	npc.IsActionStarted = false
	npc.ActionOwner = ActionOwner{}
	npc.status.tauntExpiresAt = expiresAt
	npc.status.tauntTargetObjectID = target.ObjectID
	s.npcs[objectID] = npc
	return npc, nil
}

func (s *Session) ClearTaunt(
	objectID uint32, targetObjectID uint32, expiresAt time.Time,
) {
	if s == nil || objectID == 0 || targetObjectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.tauntTargetObjectID != targetObjectID ||
		npc.status.tauntExpiresAt != expiresAt {
		return
	}
	npc.status.tauntExpiresAt = time.Time{}
	npc.status.tauntTargetObjectID = 0
	s.npcs[objectID] = npc
}

func (s *Session) ExpireTaunt(
	objectID uint32, targetObjectID uint32, expiresAt time.Time,
) (Snapshot, bool) {
	if s == nil || objectID == 0 || targetObjectID == 0 || expiresAt.IsZero() {
		return Snapshot{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.tauntTargetObjectID != targetObjectID ||
		npc.status.tauntExpiresAt != expiresAt {
		return Snapshot{}, false
	}
	npc.status.tauntExpiresAt = time.Time{}
	npc.status.tauntTargetObjectID = 0
	if npc.TargetObjectID == targetObjectID {
		npc.TargetObjectID = 0
		npc.TargetFaction = FactionUnknown
		npc.TargetOwner = ActionOwner{}
		npc.IsActionStarted = false
		npc.ActionOwner = ActionOwner{}
	}
	s.npcs[objectID] = npc
	return npc, true
}

func (s *Session) ApplyHealingReduction(
	objectID uint32, reduction float32, expiresAt time.Time,
) error {
	if s == nil || objectID == 0 || reduction <= 0 || reduction >= 1 ||
		expiresAt.IsZero() {
		return errors.New("invalid npc healing reduction")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcHealingReductionMissing: %d", objectID)
	}
	if isStatusBlockedByShield(npc) {
		return nil
	}
	if npc.status.healingReductionEnd.Before(expiresAt) {
		npc.status.healingReduction = reduction
		npc.status.healingReductionEnd = expiresAt
		s.npcs[objectID] = npc
	}
	return nil
}

func (s *Session) ClearHealingReduction(
	objectID uint32, expiresAt time.Time,
) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.healingReductionEnd != expiresAt {
		return
	}
	npc.status.healingReduction = 0
	npc.status.healingReductionEnd = time.Time{}
	s.npcs[objectID] = npc
}

func (s *Session) ApplySlow(
	objectID uint32, expiresAt time.Time,
	movementScale float32, attackScale float32,
) error {
	if s == nil || objectID == 0 || expiresAt.IsZero() ||
		movementScale <= 0 || movementScale > 1 ||
		attackScale <= 0 || attackScale > 1 {
		return errors.New("invalid npc slow")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcSlowMissing: %d", objectID)
	}
	if npc.status.slowExpiresAt.Before(expiresAt) {
		npc.status.slowExpiresAt = expiresAt
		npc.status.slowMovementScale = movementScale
		npc.status.slowAttackScale = attackScale
		s.npcs[objectID] = npc
	}
	return nil
}

func (s *Session) ClearSlow(objectID uint32, expiresAt time.Time) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.slowExpiresAt != expiresAt {
		return
	}
	npc.status.slowExpiresAt = time.Time{}
	npc.status.slowMovementScale = 0
	npc.status.slowAttackScale = 0
	s.npcs[objectID] = npc
}

type SlowScales struct {
	Movement float32
	Attack   float32
}

func (s *Session) slowScales(
	objectID uint32, at time.Time,
) SlowScales {
	if s == nil || objectID == 0 || at.IsZero() {
		return SlowScales{Movement: 1, Attack: 1}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return SlowScales{Movement: 1, Attack: 1}
	}
	movementScale := float32(1)
	attackScale := float32(1)
	if at.Before(npc.status.slowExpiresAt) {
		movementScale = max(float32(0.05), npc.status.slowMovementScale)
		attackScale = max(float32(0.05), npc.status.slowAttackScale)
	}
	if at.Before(npc.status.areaShiftExpiresAt) {
		movementScale *= 0.5
	}
	return SlowScales{Movement: max(float32(0.05), movementScale), Attack: attackScale}
}

func (s *Session) SlowProfile(objectID uint32, at time.Time) (float32, float32) {
	scales := s.slowScales(objectID, at)
	return scales.Movement, scales.Attack
}

func (s *Session) SlowMovementScale(objectID uint32, at time.Time) float32 {
	return s.slowScales(objectID, at).Movement
}

func (s *Session) ApplyHaste(
	objectID uint32, expiresAt time.Time, attackSpeed float32,
	cooldownReduction float32, movementSpeedBuff float32,
) error {
	if s == nil || objectID == 0 || expiresAt.IsZero() || attackSpeed <= 0 ||
		cooldownReduction <= 0 || cooldownReduction >= 1 || movementSpeedBuff <= 0 {
		return errors.New("invalid npc haste")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsPublished {
		return fmt.Errorf("npcHasteMissing: %d", objectID)
	}
	npc.status.hasteExpiresAt = expiresAt
	npc.status.hasteAttackSpeed = attackSpeed
	npc.status.hasteCooldownReduction = cooldownReduction
	npc.status.hasteMovementSpeedBuff = movementSpeedBuff
	if !npc.status.isHasteProfileStored {
		profile, isProfileFound := ActionProfileForPlan(npc.Plan)
		if isProfileFound {
			npc.status.hasteActionProfile = profile
			npc.status.isHasteActionKnown = npc.Plan.IsActionKnown
			npc.status.isHasteProfileStored = true
		}
	}
	if npc.status.isHasteProfileStored {
		profile := npc.status.hasteActionProfile
		profile.HitDelay = time.Duration(
			float64(profile.HitDelay) / float64(1+attackSpeed),
		)
		profile.ReleaseDelay = time.Duration(
			float64(profile.ReleaseDelay) / float64(1+attackSpeed),
		)
		profile.Cooldown = time.Duration(
			float64(profile.Cooldown) / float64(1+attackSpeed) *
				float64(1-cooldownReduction),
		)
		profile.MovementSpeed *= 1 + movementSpeedBuff
		profile.NonCombatMovementSpeed *= 1 + movementSpeedBuff
		npc.Plan.ActionProfile = profile
		npc.Plan.IsActionKnown = true
	}
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) ClearHaste(objectID uint32, expiresAt time.Time) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.hasteExpiresAt != expiresAt {
		return
	}
	npc.status.hasteExpiresAt = time.Time{}
	npc.status.hasteAttackSpeed = 0
	npc.status.hasteCooldownReduction = 0
	npc.status.hasteMovementSpeedBuff = 0
	if npc.status.isHasteProfileStored {
		npc.Plan.ActionProfile = npc.status.hasteActionProfile
		npc.Plan.IsActionKnown = npc.status.isHasteActionKnown
	}
	npc.status.hasteActionProfile = ActionProfile{}
	npc.status.isHasteActionKnown = false
	npc.status.isHasteProfileStored = false
	s.npcs[objectID] = npc
}

func (s *Session) HasteProfile(
	objectID uint32, at time.Time,
) (float32, float32, float32) {
	if s == nil || objectID == 0 || at.IsZero() {
		return 0, 0, 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !at.Before(npc.status.hasteExpiresAt) {
		return 0, 0, 0
	}
	return npc.status.hasteAttackSpeed,
		npc.status.hasteCooldownReduction,
		npc.status.hasteMovementSpeedBuff
}

func (s *Session) ApplyIntangible(objectID uint32, expiresAt time.Time) error {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return errors.New("invalid npc intangible")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcIntangibleMissing: %d", objectID)
	}
	if npc.status.intangibleExpiresAt.Before(expiresAt) {
		npc.status.intangibleExpiresAt = expiresAt
		s.npcs[objectID] = npc
	}
	return nil
}

func (s *Session) ApplyChargeProtection(objectID uint32, expiresAt time.Time) error {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return errors.New("invalid npc charge protection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcChargeProtectionMissing: %d", objectID)
	}
	if npc.status.chargeProtectionEnd.Before(expiresAt) {
		npc.status.chargeProtectionEnd = expiresAt
		npc.IsNavigationCollisionEnabled = false
		s.npcs[objectID] = npc
	}
	return nil
}

func (s *Session) ClearChargeProtection(objectID uint32) {
	if s == nil || objectID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound {
		return
	}
	npc.status.chargeProtectionEnd = time.Time{}
	npc.IsNavigationCollisionEnabled = true
	s.npcs[objectID] = npc
}

func (s *Session) ExpireChargeProtection(
	objectID uint32, expiresAt time.Time,
) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.chargeProtectionEnd != expiresAt {
		return
	}
	npc.status.chargeProtectionEnd = time.Time{}
	npc.IsNavigationCollisionEnabled = true
	s.npcs[objectID] = npc
}

func (s *Session) ClearIntangible(objectID uint32, expiresAt time.Time) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.intangibleExpiresAt != expiresAt {
		return
	}
	npc.status.intangibleExpiresAt = time.Time{}
	s.npcs[objectID] = npc
}

func (s *Session) IntangibleRemaining(
	objectID uint32, at time.Time,
) time.Duration {
	if s == nil || objectID == 0 || at.IsZero() {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !at.Before(npc.status.intangibleExpiresAt) {
		return 0
	}
	return npc.status.intangibleExpiresAt.Sub(at)
}

func (s *Session) ApplyDamageVulnerability(
	objectID uint32, sourceObjectID uint32, increase float32,
) error {
	if s == nil || objectID == 0 || sourceObjectID == 0 || increase <= 0 {
		return errors.New("invalid npc damage vulnerability")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcVulnerabilityMissing: %d", objectID)
	}
	if isStatusBlockedByShield(npc) {
		return nil
	}
	npc.status.damageVulnerabilityOwner = sourceObjectID
	npc.status.damageTakenIncrease = increase
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) ApplyDamageReduction(
	objectID uint32, reduction float32, expiresAt time.Time,
) error {
	if s == nil || objectID == 0 || reduction <= 0 || reduction >= 1 ||
		expiresAt.IsZero() {
		return errors.New("invalid npc damage reduction")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcReductionMissing: %d", objectID)
	}
	if npc.status.damageReductionEnd.After(expiresAt) {
		return nil
	}
	npc.status.damageReduction = reduction
	npc.status.damageReductionEnd = expiresAt
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) ClearDamageReduction(
	objectID uint32, expiresAt time.Time,
) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.damageReductionEnd != expiresAt {
		return
	}
	npc.status.damageReduction = 0
	npc.status.damageReductionEnd = time.Time{}
	s.npcs[objectID] = npc
}

func (s *Session) ClearDamageVulnerability(
	objectID uint32, sourceObjectID uint32,
) {
	if s == nil || objectID == 0 || sourceObjectID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.damageVulnerabilityOwner != sourceObjectID {
		return
	}
	npc.status.damageVulnerabilityOwner = 0
	npc.status.damageTakenIncrease = 0
	s.npcs[objectID] = npc
}

func (s *Session) ApplyPhysicalDamageVulnerability(
	objectID uint32, increase float32, expiresAt time.Time,
) error {
	if s == nil || objectID == 0 || increase <= 0 || expiresAt.IsZero() {
		return errors.New("invalid npc physical vulnerability")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcPhysicalVulnerabilityMissing: %d", objectID)
	}
	if isStatusBlockedByShield(npc) {
		return nil
	}
	if npc.status.physicalVulnerabilityEnd.Before(expiresAt) {
		npc.status.physicalTakenIncrease = increase
		npc.status.physicalVulnerabilityEnd = expiresAt
		s.npcs[objectID] = npc
	}
	return nil
}

func (s *Session) ClearPhysicalDamageVulnerability(
	objectID uint32, expiresAt time.Time,
) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.physicalVulnerabilityEnd != expiresAt {
		return
	}
	npc.status.physicalTakenIncrease = 0
	npc.status.physicalVulnerabilityEnd = time.Time{}
	s.npcs[objectID] = npc
}

func (s *Session) ApplyEnergyDamageVulnerability(
	objectID uint32, sourceObjectID uint32, increase float32, expiresAt time.Time,
) error {
	if s == nil || objectID == 0 || sourceObjectID == 0 || increase <= 0 ||
		expiresAt.IsZero() {
		return errors.New("invalid npc energy vulnerability")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcEnergyVulnerabilityMissing: %d", objectID)
	}
	if isStatusBlockedByShield(npc) {
		return nil
	}
	npc.status.energyVulnerabilityOwner = sourceObjectID
	npc.status.energyTakenIncrease = increase
	npc.status.energyVulnerabilityEnd = expiresAt
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) ClearEnergyDamageVulnerability(
	objectID uint32, sourceObjectID uint32, expiresAt time.Time,
) {
	if s == nil || objectID == 0 || sourceObjectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.energyVulnerabilityOwner != sourceObjectID ||
		npc.status.energyVulnerabilityEnd != expiresAt {
		return
	}
	npc.status.energyVulnerabilityOwner = 0
	npc.status.energyTakenIncrease = 0
	npc.status.energyVulnerabilityEnd = time.Time{}
	s.npcs[objectID] = npc
}

func (s *Session) ApplySilence(objectID uint32, expiresAt time.Time) error {
	return s.applySimpleStatus(objectID, expiresAt, "silence")
}

func (s *Session) ClearSilence(objectID uint32, expiresAt time.Time) {
	s.clearSimpleStatus(objectID, expiresAt, "silence")
}

func (s *Session) SilenceRemaining(objectID uint32, at time.Time) time.Duration {
	return s.simpleStatusRemaining(objectID, at, "silence")
}

func (s *Session) ApplySleep(objectID uint32, expiresAt time.Time) error {
	return s.applySimpleStatus(objectID, expiresAt, "sleep")
}

func (s *Session) ClearSleep(objectID uint32, expiresAt time.Time) {
	s.clearSimpleStatus(objectID, expiresAt, "sleep")
}

func (s *Session) SleepRemaining(objectID uint32, at time.Time) time.Duration {
	return s.simpleStatusRemaining(objectID, at, "sleep")
}

func (s *Session) applySimpleStatus(
	objectID uint32, expiresAt time.Time, kind string,
) error {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return errors.New("invalid npc status")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcStatusMissing: %d", objectID)
	}
	if isStatusBlockedByShield(npc) {
		return nil
	}
	if kind == "silence" {
		endDirectionalShield(&npc)
	}
	switch kind {
	case "silence":
		if npc.status.silenceExpiresAt.Before(expiresAt) {
			npc.status.silenceExpiresAt = expiresAt
		}
	case "sleep":
		if npc.status.sleepExpiresAt.Before(expiresAt) {
			npc.status.sleepExpiresAt = expiresAt
		}
	default:
		return errors.New("unsupported npc status")
	}
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) clearSimpleStatus(
	objectID uint32, expiresAt time.Time, kind string,
) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound {
		return
	}
	switch kind {
	case "silence":
		if npc.status.silenceExpiresAt != expiresAt {
			return
		}
		npc.status.silenceExpiresAt = time.Time{}
	case "sleep":
		if npc.status.sleepExpiresAt != expiresAt {
			return
		}
		npc.status.sleepExpiresAt = time.Time{}
	default:
		return
	}
	s.npcs[objectID] = npc
}

func (s *Session) simpleStatusRemaining(
	objectID uint32, at time.Time, kind string,
) time.Duration {
	if s == nil || objectID == 0 || at.IsZero() {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || isStatusBlockedByShield(npc) {
		return 0
	}
	expiresAt := time.Time{}
	switch kind {
	case "silence":
		expiresAt = npc.status.silenceExpiresAt
	case "sleep":
		expiresAt = npc.status.sleepExpiresAt
	}
	if !at.Before(expiresAt) {
		return 0
	}
	return expiresAt.Sub(at)
}

func (s *Session) ApplyRoot(objectID uint32, expiresAt time.Time) error {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return errors.New("invalid npc root")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcRootMissing: %d", objectID)
	}
	if isStatusBlockedByShield(npc) {
		return nil
	}
	if npc.status.rootExpiresAt.Before(expiresAt) {
		npc.status.rootExpiresAt = expiresAt
		s.npcs[objectID] = npc
	}
	return nil
}

func (s *Session) ClearRoot(objectID uint32, expiresAt time.Time) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.rootExpiresAt != expiresAt {
		return
	}
	npc.status.rootExpiresAt = time.Time{}
	s.npcs[objectID] = npc
}

func (s *Session) RootRemaining(objectID uint32, at time.Time) time.Duration {
	if s == nil || objectID == 0 || at.IsZero() {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || isStatusBlockedByShield(npc) ||
		!at.Before(npc.status.rootExpiresAt) {
		return 0
	}
	return npc.status.rootExpiresAt.Sub(at)
}

type CurseDamageProfile struct {
	DamageBuff       float32
	PhysicalIncrease float32
	EnergyIncrease   float32
}

func (s *Session) ApplyBanish(objectID uint32, expiresAt time.Time) error {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return errors.New("invalid npc banish")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcBanishMissing: %d", objectID)
	}
	if isStatusBlockedByShield(npc) {
		return nil
	}
	endDirectionalShield(&npc)
	if npc.status.banishExpiresAt.Before(expiresAt) {
		npc.status.banishExpiresAt = expiresAt
	}
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) ClearBanish(objectID uint32, expiresAt time.Time) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.banishExpiresAt != expiresAt {
		return
	}
	npc.status.banishExpiresAt = time.Time{}
	s.npcs[objectID] = npc
}

func (s *Session) BanishRemaining(objectID uint32, at time.Time) time.Duration {
	if s == nil || objectID == 0 || at.IsZero() {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || isStatusBlockedByShield(npc) ||
		!at.Before(npc.status.banishExpiresAt) {
		return 0
	}
	return npc.status.banishExpiresAt.Sub(at)
}

func (s *Session) ApplyStun(objectID uint32, expiresAt time.Time) error {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return errors.New("invalid npc stun")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcStunMissing: %d", objectID)
	}
	if isStatusBlockedByShield(npc) {
		return nil
	}
	endDirectionalShield(&npc)
	if npc.status.stunExpiresAt.Before(expiresAt) {
		npc.status.stunExpiresAt = expiresAt
	}
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) StunRemaining(objectID uint32, at time.Time) time.Duration {
	if s == nil || objectID == 0 || at.IsZero() {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || isStatusBlockedByShield(npc) {
		return 0
	}
	expiresAt := npc.status.stunExpiresAt
	if expiresAt.Before(npc.status.banishExpiresAt) {
		expiresAt = npc.status.banishExpiresAt
	}
	if expiresAt.Before(npc.status.sleepExpiresAt) {
		expiresAt = npc.status.sleepExpiresAt
	}
	if !at.Before(expiresAt) {
		return 0
	}
	return expiresAt.Sub(at)
}

func (s *Session) ClearStun(objectID uint32, expiresAt time.Time) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.stunExpiresAt != expiresAt {
		return
	}
	npc.status.stunExpiresAt = time.Time{}
	s.npcs[objectID] = npc
}

func (s *Session) ApplyFear(objectID uint32, expiresAt time.Time) error {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return errors.New("invalid npc fear")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcFearMissing: %d", objectID)
	}
	if isStatusBlockedByShield(npc) {
		return nil
	}
	if npc.status.fearExpiresAt.Before(expiresAt) {
		npc.status.fearExpiresAt = expiresAt
		s.npcs[objectID] = npc
	}
	return nil
}

func (s *Session) ClearFear(objectID uint32, expiresAt time.Time) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.fearExpiresAt != expiresAt {
		return
	}
	npc.status.fearExpiresAt = time.Time{}
	s.npcs[objectID] = npc
}

func (s *Session) FearRemaining(objectID uint32, at time.Time) time.Duration {
	if s == nil || objectID == 0 || at.IsZero() {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || isStatusBlockedByShield(npc) ||
		!at.Before(npc.status.fearExpiresAt) {
		return 0
	}
	return npc.status.fearExpiresAt.Sub(at)
}

func (s *Session) ApplyCurse(objectID uint32, expiresAt time.Time) error {
	return s.ApplyCurseProfile(objectID, expiresAt, CurseDamageProfile{})
}

func (s *Session) ApplyCurseProfile(
	objectID uint32, expiresAt time.Time, profile CurseDamageProfile,
) error {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return errors.New("invalid npc curse")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcCurseMissing: %d", objectID)
	}
	if isStatusBlockedByShield(npc) {
		return nil
	}
	if npc.status.curseExpiresAt.Before(expiresAt) {
		npc.status.curseExpiresAt = expiresAt
		npc.status.curseDamage = profile
		s.npcs[objectID] = npc
	}
	return nil
}

func (s *Session) ClearCurse(objectID uint32, expiresAt time.Time) {
	if s == nil || objectID == 0 || expiresAt.IsZero() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.status.curseExpiresAt != expiresAt {
		return
	}
	npc.status.curseExpiresAt = time.Time{}
	npc.status.curseDamage = CurseDamageProfile{}
	s.npcs[objectID] = npc
}

func (s *Session) CurseRemaining(objectID uint32, at time.Time) time.Duration {
	if s == nil || objectID == 0 || at.IsZero() {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || isStatusBlockedByShield(npc) ||
		!at.Before(npc.status.curseExpiresAt) {
		return 0
	}
	return npc.status.curseExpiresAt.Sub(at)
}

func (s *Session) HasCurseOrFear(objectID uint32, at time.Time) bool {
	if s == nil || objectID == 0 || at.IsZero() {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return false
	}
	return at.Before(npc.status.curseExpiresAt) || at.Before(npc.status.fearExpiresAt)
}

func isStatusBlockedByShield(npc Snapshot) bool {
	if time.Now().Before(npc.status.chargeProtectionEnd) {
		return true
	}
	if npc.IsTurtleActive {
		return true
	}
	if !npc.IsShieldActive {
		return false
	}
	_, isDirectional := NomadShielderDirectionalShieldProfile(npc.Plan.NounName)
	return !isDirectional
}

func endDirectionalShield(npc *Snapshot) {
	if npc == nil || !npc.IsShieldActive {
		return
	}
	_, isDirectional := NomadShielderDirectionalShieldProfile(npc.Plan.NounName)
	if isDirectional {
		npc.IsShieldActive = false
	}
}
