package npc

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
)

type Session struct {
	mu                sync.RWMutex
	objectIDLimit     uint32
	aggroRadius       float32
	defenseConversion float32
	objectIDs         []uint32
	npcs              map[uint32]Snapshot
}

func NewSession(objectIDLimit uint32, aggroRadius float32) *Session {
	return &Session{
		objectIDLimit:     objectIDLimit,
		aggroRadius:       aggroRadius,
		defenseConversion: 10,
		npcs:              make(map[uint32]Snapshot),
	}
}

func (s *Session) AggroRadius() float32 {
	if s == nil {
		return 0
	}
	return s.aggroRadius
}

func (s *Session) Add(plans []SpawnPlan, targetObjectID uint32) error {
	if s == nil || targetObjectID == 0 {
		return errors.New("zero target object")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plans = rewardSpawnPlans(plans)
	err := s.canAdd(plans)
	if err != nil {
		return fmt.Errorf("addValidate: %w", err)
	}
	for _, plan := range plans {
		s.objectIDs = append(s.objectIDs, plan.ObjectID)
		s.npcs[plan.ObjectID] = Snapshot{
			Plan: plan, Origin: plan.Position, Faction: FactionNonPlayerAligned,
			Facing:   plan.InitialFacing(),
			HitPoint: plan.NPCProfile.HitPoint, ManaPoint: plan.NPCProfile.PowerPoint,
			TargetObjectID:               targetObjectID,
			TargetFaction:                FactionPlayerAligned,
			IsPublished:                  true,
			IsSpawnStealthActive:         isSpawnStealthedPlan(plan),
			IsNavigationCollisionEnabled: true,
			status:                       initialStatus(plan),
		}
	}
	return nil
}

func (s *Session) CanAdd(plans []SpawnPlan, targetObjectID uint32) error {
	if s == nil || targetObjectID == 0 {
		return errors.New("zero target object")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.canAdd(plans)
}

// OwnedActiveCount returns the living published actors created by one NPC.
// Static encounter adds deliberately have no owner and therefore never count
// against an ability-owned population cap.
func (s *Session) OwnedActiveCount(ownerObjectID uint32) int {
	if s == nil || ownerObjectID == 0 {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.Plan.OwnerObjectID != ownerObjectID || npc.IsDefeated ||
			!npc.IsPublished || npc.HitPoint <= 0 {
			continue
		}
		count++
	}
	return count
}

func (s *Session) AddDormant(plans []SpawnPlan) error {
	if s == nil {
		return errors.New("nil npc session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plans = rewardSpawnPlans(plans)
	err := s.canAdd(plans)
	if err != nil {
		return fmt.Errorf("dormantValidate: %w", err)
	}
	for _, plan := range plans {
		s.objectIDs = append(s.objectIDs, plan.ObjectID)
		s.npcs[plan.ObjectID] = Snapshot{
			Plan: plan, Origin: plan.Position, Faction: FactionNonPlayerAligned,
			Facing:                          plan.InitialFacing(),
			HitPoint:                        plan.NPCProfile.HitPoint,
			ManaPoint:                       plan.NPCProfile.PowerPoint,
			IsPublished:                     true,
			IsSpawnStealthActive:            isSpawnStealthedPlan(plan),
			IsNavigationCollisionEnabled:    true,
			IsInvisibleToSecurityTeleporter: isPreAggroInvisibleNoun(plan.NounName),
			status:                          initialStatus(plan),
		}
	}
	return nil
}

// AddStaged records director-planned actors without exposing them to the
// retail client. They become published only when a live opposing target enters
// the ordinary aggro boundary, preventing client-owned idle movement from
// separating an actor's presentation from its authoritative position.
func (s *Session) AddStaged(plans []SpawnPlan) error {
	if s == nil {
		return errors.New("nil npc session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plans = rewardSpawnPlans(plans)
	err := s.canAdd(plans)
	if err != nil {
		return fmt.Errorf("stagedValidate: %w", err)
	}
	for _, plan := range plans {
		s.objectIDs = append(s.objectIDs, plan.ObjectID)
		s.npcs[plan.ObjectID] = Snapshot{
			Plan: plan, Origin: plan.Position, Faction: FactionNonPlayerAligned,
			Facing:                          plan.InitialFacing(),
			HitPoint:                        plan.NPCProfile.HitPoint,
			ManaPoint:                       plan.NPCProfile.PowerPoint,
			IsSpawnStealthActive:            isSpawnStealthedPlan(plan),
			IsNavigationCollisionEnabled:    true,
			IsInvisibleToSecurityTeleporter: isPreAggroInvisibleNoun(plan.NounName),
			status:                          initialStatus(plan),
		}
	}
	return nil
}

// Restore replaces the session with normalized checkpoint actors. In-flight
// targets, actions, modifiers, and movement are intentionally discarded; live
// NPCs resume idle at their saved position and defeated NPCs remain defeated.
func (s *Session) Restore(snapshots []Snapshot) error {
	if s == nil {
		return errors.New("nil npc session")
	}
	if len(snapshots) == 0 {
		return nil
	}
	plans := make([]SpawnPlan, 0, len(snapshots))
	for _, snapshot := range snapshots {
		plans = append(plans, snapshot.Plan)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.objectIDs) != 0 || len(s.npcs) != 0 {
		return errors.New("npc restore requires empty session")
	}
	err := s.canAdd(plans)
	if err != nil {
		return fmt.Errorf("restoreValidate: %w", err)
	}
	for _, snapshot := range snapshots {
		plan := snapshot.Plan.Clone()
		if !zonegeometry.IsFinite(snapshot.Origin) {
			return fmt.Errorf("restoreOrigin[%d]: invalid", plan.ObjectID)
		}
		if !zonegeometry.IsFinite(plan.Position) {
			return fmt.Errorf("restorePosition[%d]: invalid", plan.ObjectID)
		}
		maximumHitPoint := plan.NPCProfile.HitPoint
		if snapshot.HitPoint < 0 || snapshot.HitPoint > maximumHitPoint ||
			math.IsNaN(float64(snapshot.HitPoint)) ||
			math.IsInf(float64(snapshot.HitPoint), 0) {
			return fmt.Errorf("restoreHealth[%d]: invalid", plan.ObjectID)
		}
		if snapshot.ManaPoint < 0 || math.IsNaN(float64(snapshot.ManaPoint)) ||
			math.IsInf(float64(snapshot.ManaPoint), 0) {
			return fmt.Errorf("restoreMana[%d]: invalid", plan.ObjectID)
		}
		isDefeated := snapshot.IsDefeated || snapshot.HitPoint == 0
		restored := Snapshot{
			Plan: plan, Origin: snapshot.Origin, Facing: snapshot.Facing,
			Faction:  FactionNonPlayerAligned,
			HitPoint: snapshot.HitPoint, ManaPoint: snapshot.ManaPoint,
			IsDefeated: isDefeated, IsPublished: snapshot.IsPublished,
			IsSpawnStealthActive:            isSpawnStealthedPlan(plan) && !isDefeated,
			IsNavigationCollisionEnabled:    !isDefeated,
			IsInvisibleToSecurityTeleporter: isPreAggroInvisibleNoun(plan.NounName) && !isDefeated,
			status:                          initialStatus(plan),
		}
		if restored.Facing.Length() <= 0 || !zonegeometry.IsFinite(restored.Facing) {
			restored.Facing = plan.InitialFacing()
		}
		s.objectIDs = append(s.objectIDs, plan.ObjectID)
		s.npcs[plan.ObjectID] = restored
	}
	slices.Sort(s.objectIDs)
	return nil
}

func rewardSpawnPlans(plans []SpawnPlan) []SpawnPlan {
	rewarded := make([]SpawnPlan, len(plans))
	copy(rewarded, plans)
	for index := range rewarded {
		plan := &rewarded[index]
		if plan.Experience != 0 || plan.IsFixture || plan.IsRewardSuppressed ||
			plan.NPCProfile.IsPlayerPet ||
			plan.NPCProfile.ChallengeValue <= 0 {
			continue
		}
		challenge := uint64(plan.NPCProfile.ChallengeValue)
		experience := challenge * 2
		if experience > math.MaxUint32 {
			experience = math.MaxUint32
		}
		plan.Experience = uint32(experience)
	}
	return rewarded
}

func isSpawnStealthedPlan(plan SpawnPlan) bool {
	if plan.Introduction == SpawnIntroductionFloorWarp {
		return false
	}
	profile, isFound := ActionProfileForPlan(plan)
	return isFound && profile.IsSpawnStealthed
}

func initialStatus(plan SpawnPlan) status {
	profile, isFound := ActionProfileForPlan(plan)
	if !isFound || profile.PassiveAbsorptionShield <= 0 ||
		profile.PassiveAbsorptionShieldRecharge <= 0 {
		return status{}
	}
	return status{
		absorptionShieldAmount:   profile.PassiveAbsorptionShield,
		absorptionShieldMaximum:  profile.PassiveAbsorptionShield,
		absorptionShieldRecharge: profile.PassiveAbsorptionShieldRecharge,
	}
}

func (s *Session) RevealSpawnStealth(objectID uint32) (bool, error) {
	if s == nil || objectID == 0 {
		return false, errors.New("invalid npc stealth")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return false, nil
	}
	if !npc.IsSpawnStealthActive {
		return false, nil
	}
	npc.IsSpawnStealthActive = false
	s.npcs[objectID] = npc
	return true, nil
}

func (s *Session) CanAddDormant(plans []SpawnPlan) error {
	if s == nil {
		return errors.New("nil npc session")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.canAdd(plans)
}

func (s *Session) ConfigureShieldedAffix(
	plans []SpawnPlan, difficulty uint32, playerCount uint32,
) error {
	if s == nil || difficulty == 0 || playerCount == 0 {
		return errors.New("shielded affix configuration invalid")
	}
	shieldAmount := float32(10 * math.Pow(1.037500023841858, float64(difficulty)) *
		float64(playerCount))
	if !zonegeometry.IsFinitePositiveScalar(shieldAmount) {
		return errors.New("shielded affix amount invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, plan := range plans {
		isShielded := false
		for _, modifierName := range plan.BossIdentity.ModifierNames {
			if strings.EqualFold(modifierName, "Shielded_NPCAffixModifier") {
				isShielded = true
				break
			}
		}
		if !isShielded {
			continue
		}
		npc, isFound := s.npcs[plan.ObjectID]
		if !isFound || !npc.IsPublished {
			return fmt.Errorf("shieldedAffixMissing: %d", plan.ObjectID)
		}
		if npc.IsDefeated {
			continue
		}
		recharge := 12 * time.Second
		switch {
		case strings.HasSuffix(strings.ToLower(plan.NounName), "_2.noun"):
			recharge = 10 * time.Second
		case strings.HasSuffix(strings.ToLower(plan.NounName), "_3.noun"):
			recharge = 8 * time.Second
		}
		npc.status.absorptionShieldAmount = shieldAmount
		npc.status.absorptionShieldMaximum = shieldAmount
		npc.status.absorptionShieldReadyAt = time.Time{}
		npc.status.absorptionShieldRecharge = recharge
		s.npcs[plan.ObjectID] = npc
	}
	return nil
}

func (s *Session) ConfigureCarapaceAffix(
	plans []SpawnPlan, difficulty uint32,
) error {
	if s == nil || difficulty == 0 {
		return errors.New("carapace affix configuration invalid")
	}
	damageMaximum := float32(math.Floor(
		6 * math.Pow(1.037500023841858, float64(difficulty)),
	))
	if !zonegeometry.IsFinitePositiveScalar(damageMaximum) {
		return errors.New("carapace affix damage invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, plan := range plans {
		isCarapace := false
		for _, modifierName := range plan.BossIdentity.ModifierNames {
			if strings.EqualFold(modifierName, "Carapace_NPCAffixModifier") {
				isCarapace = true
				break
			}
		}
		if !isCarapace {
			continue
		}
		npc, isFound := s.npcs[plan.ObjectID]
		if !isFound || !npc.IsPublished {
			return fmt.Errorf("carapaceAffixMissing: %d", plan.ObjectID)
		}
		if npc.IsDefeated {
			continue
		}
		npc.status.carapaceDamageMaximum = damageMaximum
		s.npcs[plan.ObjectID] = npc
	}
	return nil
}

func (s *Session) canAdd(plans []SpawnPlan) error {
	if s == nil {
		return errors.New("nil npc session")
	}
	if s.objectIDLimit <= 1 ||
		!zonegeometry.IsFinitePositiveScalar(s.aggroRadius) {
		return errors.New("invalid npc session policy")
	}
	if len(plans) == 0 {
		return errors.New("empty npc plans")
	}
	seenObjectIDs := make(map[uint32]bool, len(plans))
	for index, plan := range plans {
		err := ValidateSpawnPlan(plan, s.objectIDLimit)
		if err != nil {
			return fmt.Errorf("planValidate[%d]: %w", index, err)
		}
		if seenObjectIDs[plan.ObjectID] || s.npcs[plan.ObjectID].Plan.ObjectID != 0 {
			return fmt.Errorf("planDuplicate[%d]: %d", index, plan.ObjectID)
		}
		if plan.OwnerObjectID == plan.ObjectID {
			return fmt.Errorf("planOwner[%d]: self-owned", index)
		}
		seenObjectIDs[plan.ObjectID] = true
	}
	return nil
}

func (s *Session) AcquireNearby(
	position game.Vec3, targetObjectID uint32, playerFootprintRadius float32,
) ([]Snapshot, error) {
	return s.AcquireTargets([]Target{{
		ObjectID: targetObjectID, Position: position,
		FootprintRadius: playerFootprintRadius,
		Faction:         FactionPlayerAligned, IsAlive: true,
	}}, false)
}

func (s *Session) AcquireReadyNearby(
	position game.Vec3, targetObjectID uint32, playerFootprintRadius float32,
) ([]Snapshot, error) {
	return s.AcquireTargets([]Target{{
		ObjectID: targetObjectID, Position: position,
		FootprintRadius: playerFootprintRadius,
		Faction:         FactionPlayerAligned, IsAlive: true,
	}}, true)
}

func (s *Session) AcquireTargets(
	targets []Target, isCurrentTargetIncluded bool,
) ([]Snapshot, error) {
	if s == nil {
		return nil, errors.New("nil npc session")
	}
	if len(targets) == 0 {
		return nil, nil
	}
	for index, target := range targets {
		if target.ObjectID == 0 || !zonegeometry.IsFinite(target.Position) ||
			math.IsNaN(float64(target.FootprintRadius)) ||
			math.IsInf(float64(target.FootprintRadius), 0) ||
			target.FootprintRadius < 0 ||
			target.Faction == FactionUnknown {
			return nil, fmt.Errorf("npcAcquireTarget[%d]: invalid", index)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	acquired := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated || !npc.IsPublished ||
			npc.Plan.IsFixture || npc.IsActionStarted {
			continue
		}
		if npc.TargetObjectID != 0 && !isCurrentTargetIncluded {
			continue
		}
		if npc.TargetObjectID != 0 && now.Before(npc.status.tauntExpiresAt) {
			continue
		}
		var selected Target
		selectedDistanceSquared := float32(math.MaxFloat32)
		for _, target := range targets {
			if !target.IsAlive || target.Faction == npc.Faction {
				continue
			}
			deltaX := npc.Plan.Position.X - target.Position.X
			deltaY := npc.Plan.Position.Y - target.Position.Y
			deltaZ := npc.Plan.Position.Z - target.Position.Z
			distance := s.aggroRadius + target.FootprintRadius +
				max(float32(0), npc.Plan.NPCProfile.FootprintRadius)
			distanceSquared := deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ
			if distanceSquared > distance*distance ||
				distanceSquared >= selectedDistanceSquared {
				continue
			}
			selected = target
			selectedDistanceSquared = distanceSquared
		}
		if selected.ObjectID == 0 {
			continue
		}
		npc.TargetObjectID = selected.ObjectID
		npc.TargetFaction = selected.Faction
		npc.TargetOwner = selected.Owner
		npc.IsInvisibleToSecurityTeleporter = false
		s.npcs[objectID] = npc
		acquired = append(acquired, npc)
	}
	return acquired, nil
}

func (s *Session) AcquireTarget(
	objectID uint32, targetObjectID uint32,
) (Snapshot, bool, error) {
	if s == nil || objectID == 0 || targetObjectID == 0 {
		return Snapshot{}, false, errors.New("npc target acquire invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsPublished || npc.HitPoint <= 0 {
		return Snapshot{}, false, fmt.Errorf("npc target unavailable: %d", objectID)
	}
	if npc.Plan.IsFixture || npc.TargetObjectID != 0 {
		return npc, false, nil
	}
	npc.TargetObjectID = targetObjectID
	npc.TargetFaction = FactionPlayerAligned
	npc.IsInvisibleToSecurityTeleporter = false
	s.npcs[objectID] = npc
	return npc, true, nil
}

func (s *Session) Retarget(objectID uint32, target Target) (Snapshot, bool, error) {
	if s == nil || objectID == 0 || target.ObjectID == 0 || !target.IsAlive ||
		target.Faction == FactionUnknown ||
		!zonegeometry.IsFinite(target.Position) || target.FootprintRadius < 0 ||
		math.IsNaN(float64(target.FootprintRadius)) ||
		math.IsInf(float64(target.FootprintRadius), 0) {
		return Snapshot{}, false, errors.New("npc target replacement invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsPublished || npc.HitPoint <= 0 ||
		npc.Plan.IsFixture || npc.Faction == target.Faction {
		return npc, false, nil
	}
	if npc.TargetObjectID != target.ObjectID && time.Now().Before(npc.status.tauntExpiresAt) {
		return npc, false, nil
	}
	npc.TargetObjectID = target.ObjectID
	npc.TargetFaction = target.Faction
	npc.TargetOwner = target.Owner
	npc.IsInvisibleToSecurityTeleporter = false
	s.npcs[objectID] = npc
	return npc, true, nil
}

func (s *Session) StartAction(
	objectID uint32, owner ActionOwner, targetObjectID uint32,
) (Snapshot, bool, bool, error) {
	if s == nil || objectID == 0 || owner.UserID == 0 ||
		owner.PeerGeneration == 0 || targetObjectID == 0 {
		return Snapshot{}, false, false, errors.New("npc action start invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsPublished || npc.HitPoint <= 0 {
		return Snapshot{}, false, false, fmt.Errorf("npcActionMissing: %d", objectID)
	}
	if npc.TargetObjectID != 0 && npc.TargetObjectID != targetObjectID {
		return npc, false, false, nil
	}
	if npc.IsActionStarted {
		return npc, false, false, nil
	}
	if npc.TargetObjectID == 0 {
		npc.TargetObjectID = targetObjectID
		npc.TargetFaction = FactionPlayerAligned
	}
	npc.ActionGeneration++
	if npc.ActionGeneration == 0 {
		npc.ActionGeneration = 1
	}
	npc.IsActionStarted = true
	isFirstAction := !npc.IsFirstActionStarted
	npc.IsFirstActionStarted = true
	npc.ActionOwner = owner
	s.npcs[objectID] = npc
	return npc, true, isFirstAction, nil
}

func (s *Session) ResetAction(objectID uint32) bool {
	if s == nil || objectID == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || !npc.IsActionStarted {
		return false
	}
	npc.IsActionStarted = false
	npc.ActionOwner = ActionOwner{}
	s.npcs[objectID] = npc
	return true
}

func (s *Session) ReleaseAction(objectID uint32, owner ActionOwner) bool {
	if s == nil || objectID == 0 || owner.UserID == 0 || owner.PeerGeneration == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || !npc.IsActionStarted || npc.ActionOwner != owner {
		return false
	}
	npc.IsActionStarted = false
	npc.ActionOwner = ActionOwner{}
	s.npcs[objectID] = npc
	return true
}

func (s *Session) ReleaseActionGeneration(
	objectID uint32, owner ActionOwner, actionGeneration uint64,
) bool {
	if s == nil || objectID == 0 || owner.UserID == 0 ||
		owner.PeerGeneration == 0 || actionGeneration == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || !npc.IsActionStarted || npc.ActionOwner != owner ||
		npc.ActionGeneration != actionGeneration {
		return false
	}
	npc.IsActionStarted = false
	npc.ActionOwner = ActionOwner{}
	s.npcs[objectID] = npc
	return true
}

func (s *Session) ReleaseActions(owner ActionOwner) []Snapshot {
	if s == nil || owner.UserID == 0 || owner.PeerGeneration == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	released := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated || !npc.IsActionStarted ||
			npc.ActionOwner != owner {
			continue
		}
		npc.IsActionStarted = false
		npc.ActionOwner = ActionOwner{}
		s.npcs[objectID] = npc
		released = append(released, npc)
	}
	return released
}

func (s *Session) RollbackAdd(plans []SpawnPlan) error {
	if s == nil || len(plans) == 0 {
		return errors.New("rollback npc add: invalid batch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(plans) > len(s.objectIDs) {
		return errors.New("rollback npc add: invalid batch")
	}
	firstIndex := len(s.objectIDs) - len(plans)
	for index, plan := range plans {
		objectID := s.objectIDs[firstIndex+index]
		npc, isFound := s.npcs[objectID]
		if objectID != plan.ObjectID || !isFound || npc.Plan.ObjectID != plan.ObjectID ||
			npc.IsDefeated || npc.HitPoint != plan.NPCProfile.HitPoint {
			return fmt.Errorf("rollback npc add[%d]: changed", index)
		}
	}
	for _, plan := range plans {
		delete(s.npcs, plan.ObjectID)
	}
	s.objectIDs = s.objectIDs[:firstIndex]
	return nil
}

// Despawn retires retained encounter objects without awarding damage, loot, or
// encounter progress. The snapshots remain addressable for late scheduled
// packets, but no longer participate in targeting or live-NPC queries.
func (s *Session) Despawn(objectIDs []uint32) error {
	if s == nil || len(objectIDs) == 0 {
		return errors.New("despawn npc: invalid batch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, objectID := range objectIDs {
		npc, isFound := s.npcs[objectID]
		if !isFound {
			continue
		}
		if !npc.Plan.IsRewardSuppressed {
			return fmt.Errorf("despawn npc[%d]: reward eligible", index)
		}
	}
	for _, objectID := range objectIDs {
		npc, isFound := s.npcs[objectID]
		if !isFound {
			continue
		}
		npc.HitPoint = 0
		npc.IsDefeated = true
		npc.TargetObjectID = 0
		npc.TargetFaction = FactionUnknown
		npc.TargetOwner = ActionOwner{}
		npc.IsActionStarted = false
		npc.ActionOwner = ActionOwner{}
		s.npcs[objectID] = npc
	}
	return nil
}

func (s *Session) ReleaseTarget(targetObjectID uint32) ([]Snapshot, error) {
	if s == nil {
		return nil, errors.New("nil NPC session")
	}
	if targetObjectID == 0 {
		return nil, errors.New("zero target object")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	released := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated || npc.Plan.IsFixture ||
			npc.TargetObjectID != targetObjectID {
			continue
		}
		previous := npc
		npc.TargetObjectID = 0
		npc.TargetFaction = FactionUnknown
		npc.TargetOwner = ActionOwner{}
		npc.IsActionStarted = false
		npc.ActionOwner = ActionOwner{}
		s.npcs[objectID] = npc
		released = append(released, previous)
	}
	return released, nil
}

func (s *Session) ReplaceTarget(
	targetObjectID uint32, targets []Target,
) ([]Snapshot, error) {
	if s == nil {
		return nil, errors.New("nil npc session")
	}
	if targetObjectID == 0 {
		return nil, errors.New("zero target object")
	}
	for index, target := range targets {
		if target.ObjectID == 0 || !zonegeometry.IsFinite(target.Position) ||
			math.IsNaN(float64(target.FootprintRadius)) ||
			math.IsInf(float64(target.FootprintRadius), 0) ||
			target.FootprintRadius < 0 ||
			target.Faction == FactionUnknown {
			return nil, fmt.Errorf("npcReplaceTarget[%d]: invalid", index)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	released := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated || npc.Plan.IsFixture ||
			npc.TargetObjectID != targetObjectID {
			continue
		}
		var selected Target
		selectedDistanceSquared := float32(math.MaxFloat32)
		for _, target := range targets {
			if !target.IsAlive || target.ObjectID == targetObjectID ||
				target.Faction == npc.Faction {
				continue
			}
			deltaX := npc.Plan.Position.X - target.Position.X
			deltaY := npc.Plan.Position.Y - target.Position.Y
			deltaZ := npc.Plan.Position.Z - target.Position.Z
			distance := s.aggroRadius + target.FootprintRadius +
				max(float32(0), npc.Plan.NPCProfile.FootprintRadius)
			distanceSquared := deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ
			if distanceSquared > distance*distance ||
				distanceSquared >= selectedDistanceSquared {
				continue
			}
			selected = target
			selectedDistanceSquared = distanceSquared
		}
		if selected.ObjectID != 0 {
			npc.TargetObjectID = selected.ObjectID
			npc.TargetFaction = selected.Faction
			npc.TargetOwner = selected.Owner
			s.npcs[objectID] = npc
			continue
		}
		npc.TargetObjectID = 0
		npc.TargetFaction = FactionUnknown
		npc.TargetOwner = ActionOwner{}
		npc.IsActionStarted = false
		npc.ActionOwner = ActionOwner{}
		s.npcs[objectID] = npc
		released = append(released, npc)
	}
	return released, nil
}

func (s *Session) ForceTargetInRadius(
	target Target, radius float32,
) ([]Snapshot, error) {
	if s == nil {
		return nil, errors.New("nil npc session")
	}
	if target.ObjectID == 0 || !target.IsAlive ||
		!zonegeometry.IsFinite(target.Position) || target.Faction == FactionUnknown ||
		math.IsNaN(float64(radius)) || math.IsInf(float64(radius), 0) || radius <= 0 {
		return nil, errors.New("npc forced target invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acquired := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated || npc.Plan.IsFixture || npc.Faction == target.Faction {
			continue
		}
		distance := radius + target.FootprintRadius +
			max(float32(0), npc.Plan.NPCProfile.FootprintRadius)
		if npc.Plan.Position.Sub(target.Position).Length() > distance {
			continue
		}
		npc.TargetObjectID = target.ObjectID
		npc.TargetFaction = target.Faction
		npc.TargetOwner = target.Owner
		npc.IsActionStarted = false
		npc.ActionOwner = ActionOwner{}
		s.npcs[objectID] = npc
		acquired = append(acquired, npc)
	}
	return acquired, nil
}

func (s *Session) ReturnHome(objectIDs []uint32) []Snapshot {
	if s == nil || len(objectIDs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	returning := make([]Snapshot, 0, len(objectIDs))
	for _, objectID := range objectIDs {
		npc, isFound := s.npcs[objectID]
		if !isFound || npc.IsDefeated || npc.Plan.IsFixture {
			continue
		}
		npc.Plan.Position = npc.Origin
		npc.TargetObjectID = 0
		npc.TargetFaction = FactionUnknown
		npc.TargetOwner = ActionOwner{}
		npc.IsActionStarted = false
		npc.ActionOwner = ActionOwner{}
		s.npcs[objectID] = npc
		returning = append(returning, npc)
	}
	return returning
}

func (s *Session) RestartTarget(targetObjectID uint32) ([]Snapshot, error) {
	if s == nil {
		return nil, errors.New("nil npc session")
	}
	if targetObjectID == 0 {
		return nil, errors.New("zero target object")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	restarted := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated || npc.Plan.IsFixture ||
			npc.TargetObjectID != targetObjectID {
			continue
		}
		npc.IsActionStarted = false
		npc.ActionOwner = ActionOwner{}
		s.npcs[objectID] = npc
		restarted = append(restarted, npc)
	}
	return restarted, nil
}

func (s *Session) ClearTargets() error {
	if s == nil {
		return errors.New("nil NPC session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated || npc.Plan.IsFixture {
			continue
		}
		npc.TargetObjectID = 0
		npc.TargetFaction = FactionUnknown
		npc.TargetOwner = ActionOwner{}
		npc.IsActionStarted = false
		npc.ActionOwner = ActionOwner{}
		s.npcs[objectID] = npc
	}
	return nil
}

func (s *Session) Damage(
	sourceObjectID uint32, targetObjectID uint32, damage float32,
	damageSource ...uint32,
) (DamageResult, error) {
	return s.damage(sourceObjectID, targetObjectID, damage, nil, false, false, damageSource)
}

func (s *Session) DamageOverTime(
	sourceObjectID uint32, targetObjectID uint32, damage float32,
	damageSource ...uint32,
) (DamageResult, error) {
	return s.damage(sourceObjectID, targetObjectID, damage, nil, false, true, damageSource)
}

func (s *Session) DamageFromPosition(
	sourceObjectID uint32, targetObjectID uint32, damage float32,
	sourcePosition game.Vec3, damageSource ...uint32,
) (DamageResult, error) {
	if !zonegeometry.IsFinite(sourcePosition) {
		return DamageResult{}, errors.New("invalid npc damage source position")
	}
	return s.damage(
		sourceObjectID, targetObjectID, damage, &sourcePosition, false, false,
		damageSource,
	)
}

func (s *Session) DamageAreaFromPosition(
	sourceObjectID uint32, targetObjectID uint32, damage float32,
	sourcePosition game.Vec3, damageSource ...uint32,
) (DamageResult, error) {
	if !zonegeometry.IsFinite(sourcePosition) {
		return DamageResult{}, errors.New("invalid npc area damage source position")
	}
	return s.damage(
		sourceObjectID, targetObjectID, damage, &sourcePosition, true, false,
		damageSource,
	)
}

func (s *Session) damage(
	sourceObjectID uint32, targetObjectID uint32, damage float32,
	sourcePosition *game.Vec3, isAreaAttack bool, isDamageOverTime bool,
	damageSource []uint32,
) (DamageResult, error) {
	if s == nil {
		return DamageResult{}, errors.New("nil npc session")
	}
	if sourceObjectID == 0 || targetObjectID == 0 || math.IsNaN(float64(damage)) ||
		math.IsInf(float64(damage), 0) || damage <= 0 {
		return DamageResult{}, errors.New("invalid npc damage")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[targetObjectID]
	if !isFound {
		return DamageResult{}, fmt.Errorf("npcMissing: %d", targetObjectID)
	}
	if !npc.IsPublished {
		return DamageResult{}, fmt.Errorf("npcUnpublished: %d", targetObjectID)
	}
	if npc.IsDefeated {
		return DamageResult{}, fmt.Errorf("npcDefeated: %d", targetObjectID)
	}
	now := time.Now()
	if now.Before(npc.status.intangibleExpiresAt) ||
		now.Before(npc.status.banishExpiresAt) {
		return DamageResult{
			ObjectID: targetObjectID, LocusID: npc.Plan.LocusID,
			MarkerSetName: npc.Plan.MarkerSetName, PreviousHealth: npc.HitPoint,
			HitPoint: npc.HitPoint, IsDamageImmune: true,
		}, nil
	}
	isAbsorptionShieldRenewed := false
	if npc.status.absorptionShieldMaximum > 0 &&
		npc.status.absorptionShieldAmount <= 0 &&
		!npc.status.absorptionShieldReadyAt.IsZero() &&
		!now.Before(npc.status.absorptionShieldReadyAt) {
		npc.status.absorptionShieldAmount = npc.status.absorptionShieldMaximum
		npc.status.absorptionShieldReadyAt = time.Time{}
		isAbsorptionShieldRenewed = true
	}
	actionProfile, isActionProfileFound := ActionProfileForPlan(npc.Plan)
	species := nounSpecies(npc.Plan.NounName)
	isPhysicalDamage := len(damageSource) == 0 || damageSource[0] == 0
	isEnergyDamage := len(damageSource) > 0 && damageSource[0] == 1
	damage = s.reduceDamage(npc, actionProfile, damage, sourcePosition,
		isPhysicalDamage, isEnergyDamage, isAreaAttack, isDamageOverTime, now)
	damage *= 1 + npc.status.damageTakenIncrease
	if npc.Plan.OwnerObjectID != 0 && IsNashiraNoun(npc.Plan.NounName) &&
		(isPhysicalDamage || isEnergyDamage) {
		// ShadowBossPassive.SetIsDuplicate adds 3 to both incoming damage
		// attributes. This is the illusion's identity, not a cleansable debuff.
		damage *= 4
	}
	if isPhysicalDamage && now.Before(npc.status.physicalVulnerabilityEnd) {
		damage *= 1 + npc.status.physicalTakenIncrease
	}
	if len(damageSource) > 0 && damageSource[0] == 1 &&
		time.Now().Before(npc.status.energyVulnerabilityEnd) {
		damage *= 1 + npc.status.energyTakenIncrease
	}
	absorbedDamage := min(damage, npc.status.absorptionShieldAmount)
	if absorbedDamage > 0 {
		npc.status.absorptionShieldAmount -= absorbedDamage
		damage -= absorbedDamage
		if npc.status.absorptionShieldAmount <= 0 {
			npc.status.absorptionShieldAmount = 0
			npc.status.absorptionShieldReadyAt = now.Add(
				npc.status.absorptionShieldRecharge,
			)
		}
	}
	previousHealth := npc.HitPoint
	appliedDamage := min(damage, previousHealth)
	if !npc.IsTurtleTriggered && isNomadSpecialThreeNoun(npc.Plan.NounName) &&
		npc.Plan.NPCProfile.HitPoint > 0 {
		turtleThreshold := npc.Plan.NPCProfile.HitPoint * 0.5
		isTurtleThresholdCrossed := previousHealth > turtleThreshold &&
			previousHealth-appliedDamage <= turtleThreshold
		if isTurtleThresholdCrossed {
			appliedDamage = previousHealth - turtleThreshold
		}
	}
	npc.HitPoint = max(0, previousHealth-appliedDamage)
	npc.IsDefeated = npc.HitPoint == 0
	if appliedDamage > 0 && !npc.IsDefeated &&
		species == "cryoselementalspecialthree" {
		npc.status.isDamageFleePending = true
	}
	isSelfResurrectionStarted := npc.IsDefeated &&
		!npc.IsSelfResurrectionTriggered &&
		nounSpecies(npc.Plan.NounName) == "nomadbiospecialtwo"
	if isSelfResurrectionStarted {
		npc.IsSelfResurrectionTriggered = true
	}
	isCorruptorStageTwoStarted := npc.IsDefeated &&
		!npc.IsCorruptorStageTwo &&
		nounSpecies(npc.Plan.NounName) == "scaldronboss"
	if isCorruptorStageTwoStarted {
		npc.IsCorruptorStageTwo = true
	}
	isAreaShiftStarted := false
	if !npc.IsDefeated && isAreaAttack &&
		species == "zelembasicflyingmelee" {
		isAreaShiftStarted = !now.Before(npc.status.areaShiftExpiresAt)
		npc.status.areaShiftExpiresAt = now.Add(4 * time.Second)
	}
	isSpawnStealthRevealed := npc.IsSpawnStealthActive &&
		isActionProfileFound && actionProfile.IsSpawnStealthed
	if isSpawnStealthRevealed {
		npc.IsSpawnStealthActive = false
	}
	if npc.IsDefeated {
		if npc.status.oozeBaseMaximumHitPoint > 0 {
			npc.Plan.NPCProfile.HitPoint = npc.status.oozeBaseMaximumHitPoint
			npc.Plan.NPCProfile.GraphicsScale = npc.status.oozeBaseGraphicsScale
			npc.status.oozeBaseMaximumHitPoint = 0
			npc.status.oozeBaseGraphicsScale = 0
			npc.status.oozeGrowthDamageIncrease = 0
			npc.status.oozeGrowthStack = 0
		}
		npc.TargetObjectID = 0
		npc.TargetFaction = FactionUnknown
		npc.TargetOwner = ActionOwner{}
		npc.IsActionStarted = false
		npc.ActionOwner = ActionOwner{}
	}
	deletedOwnedObjectIDs := make([]uint32, 0, 1)
	if npc.IsDefeated {
		for _, objectID := range s.objectIDs {
			owned := s.npcs[objectID]
			if owned.Plan.OwnerObjectID != npc.Plan.ObjectID ||
				owned.IsDefeated || !owned.IsPublished ||
				!owned.Plan.IsRewardSuppressed {
				continue
			}
			owned.HitPoint = 0
			owned.IsDefeated = true
			owned.TargetObjectID = 0
			owned.TargetFaction = FactionUnknown
			owned.TargetOwner = ActionOwner{}
			owned.IsActionStarted = false
			owned.ActionOwner = ActionOwner{}
			s.npcs[objectID] = owned
			deletedOwnedObjectIDs = append(deletedOwnedObjectIDs, objectID)
		}
	}
	_, isNomadWithDrone := NomadWithDroneShieldDuration(npc.Plan.NounName)
	isShieldStarted := !npc.IsDefeated && !npc.IsShieldTriggered && isNomadWithDrone &&
		npc.Plan.NPCProfile.HitPoint > 0 &&
		npc.HitPoint/npc.Plan.NPCProfile.HitPoint < 0.5
	if isShieldStarted {
		npc.IsShieldTriggered = true
		npc.IsShieldActive = true
		if npc.status.isHasteProfileStored {
			npc.Plan.ActionProfile = npc.status.hasteActionProfile
			npc.Plan.IsActionKnown = npc.status.isHasteActionKnown
		}
		npc.status = status{}
	}
	isTurtleStarted := !npc.IsDefeated && !npc.IsTurtleTriggered &&
		isNomadSpecialThreeNoun(npc.Plan.NounName) &&
		npc.Plan.NPCProfile.HitPoint > 0 && npc.HitPoint > 0 &&
		npc.HitPoint/npc.Plan.NPCProfile.HitPoint <= 0.5
	if isTurtleStarted {
		npc.IsTurtleTriggered = true
		npc.IsTurtleActive = true
	}
	s.npcs[targetObjectID] = npc
	passiveHeals := s.applyNearbyLeechHeals(npc, appliedDamage)
	passiveEnrages := s.applyNearbyRagetuskEnrages(npc)
	remainingLocusActorCount := 0
	remainingMarkerSetCount := 0
	remainingActorCount := 0
	for _, objectID := range s.objectIDs {
		candidate := s.npcs[objectID]
		if candidate.IsDefeated || candidate.Plan.IsFixture {
			continue
		}
		remainingActorCount++
		if candidate.Plan.LocusID == npc.Plan.LocusID {
			remainingLocusActorCount++
		}
		if npc.Plan.MarkerSetName != "" &&
			candidate.Plan.MarkerSetName == npc.Plan.MarkerSetName {
			remainingMarkerSetCount++
		}
	}
	isTerminalDefeat := npc.IsDefeated && !isSelfResurrectionStarted &&
		!isCorruptorStageTwoStarted
	return DamageResult{
		ObjectID: targetObjectID, DeletedOwnedObjectIDs: deletedOwnedObjectIDs,
		LocusID: npc.Plan.LocusID, MarkerSetName: npc.Plan.MarkerSetName,
		PreviousHealth: previousHealth, HitPoint: npc.HitPoint,
		Damage:                   appliedDamage + absorbedDamage,
		AbsorbedDamage:           absorbedDamage,
		RemainingLocusActorCount: remainingLocusActorCount, RemainingActorCount: remainingActorCount,
		RemainingMarkerSetCount:    remainingMarkerSetCount,
		IsDefeated:                 isTerminalDefeat,
		IsAbsorptionShieldHit:      absorbedDamage > 0,
		IsAbsorptionShieldBroken:   absorbedDamage > 0 && npc.status.absorptionShieldAmount == 0,
		IsAbsorptionShieldRenewed:  isAbsorptionShieldRenewed,
		IsShieldStarted:            isShieldStarted,
		IsSpawnStealthRevealed:     isSpawnStealthRevealed,
		IsTurtleStarted:            isTurtleStarted,
		IsSelfResurrectionStarted:  isSelfResurrectionStarted,
		IsCorruptorStageTwoStarted: isCorruptorStageTwoStarted,
		IsAreaShiftStarted:         isAreaShiftStarted,
		IsLocusCleared:             !npc.Plan.IsFixture && isTerminalDefeat && remainingLocusActorCount == 0,
		AreAllNPCsDefeated:         !npc.Plan.IsFixture && isTerminalDefeat && remainingActorCount == 0,
		PassiveHeals:               passiveHeals,
		PassiveEnrages:             passiveEnrages,
	}, nil
}

func (s *Session) applyNearbyLeechHeals(
	damaged Snapshot, appliedDamage float32,
) []PassiveHeal {
	if appliedDamage <= 0 || damaged.Plan.IsFixture {
		return nil
	}
	heals := make([]PassiveHeal, 0, 1)
	for _, objectID := range s.objectIDs {
		candidate := s.npcs[objectID]
		if candidate.Plan.ObjectID == damaged.Plan.ObjectID ||
			candidate.IsDefeated || !candidate.IsPublished ||
			candidate.Faction != damaged.Faction ||
			nounSpecies(candidate.Plan.NounName) != "nocturnaspecialleech" ||
			candidate.Plan.Position.Sub(damaged.Plan.Position).Length() > 12 {
			continue
		}
		amount := min(
			candidate.Plan.NPCProfile.HitPoint-candidate.HitPoint,
			appliedDamage*0.25,
		)
		if amount <= 0 {
			continue
		}
		candidate.HitPoint += amount
		s.npcs[objectID] = candidate
		heals = append(heals, PassiveHeal{
			SourceObjectID: damaged.Plan.ObjectID,
			Target:         candidate,
			Amount:         amount,
		})
	}
	return heals
}

func (s *Session) applyNearbyRagetuskEnrages(defeated Snapshot) []PassiveEnrage {
	if !defeated.IsDefeated || defeated.Plan.IsFixture {
		return nil
	}
	enrages := make([]PassiveEnrage, 0, 1)
	for _, objectID := range s.objectIDs {
		candidate := s.npcs[objectID]
		if candidate.Plan.ObjectID == defeated.Plan.ObjectID ||
			candidate.IsDefeated || !candidate.IsPublished ||
			candidate.Faction != defeated.Faction ||
			nounSpecies(candidate.Plan.NounName) != "nomadspecialone" ||
			candidate.Plan.Position.Sub(defeated.Plan.Position).Length() > 12 ||
			candidate.status.passiveEnrageStack >= 5 {
			continue
		}
		candidate.status.passiveEnrageStack++
		candidate.status.passiveDamageIncrease += 0.15
		bodyScale := float32(candidate.status.passiveEnrageStack) * 0.08
		s.npcs[objectID] = candidate
		enrages = append(enrages, PassiveEnrage{
			Target:    candidate,
			BodyScale: bodyScale,
		})
	}
	return enrages
}

func isNomadSpecialThreeNoun(nounName string) bool {
	species := nounSpecies(nounName)
	return species == "nomadspecialthree"
}

func isShieldDamageImmune(npc Snapshot, sourcePosition *game.Vec3) bool {
	_, isDirectional := NomadShielderDirectionalShieldProfile(npc.Plan.NounName)
	if !isDirectional || sourcePosition == nil {
		return true
	}
	incoming := sourcePosition.Sub(npc.Plan.Position)
	incomingLength := incoming.Length()
	facingLength := npc.Facing.Length()
	if incomingLength <= 0 || facingLength <= 0 {
		return false
	}
	incoming = incoming.Scale(1 / incomingLength)
	facing := npc.Facing.Scale(1 / facingLength)
	if incoming.Z >= 0.4 {
		return false
	}
	return incoming.X*facing.X+incoming.Y*facing.Y+incoming.Z*facing.Z > 0
}

func (s *Session) Heal(objectID uint32, amount float32) (Snapshot, float32, error) {
	if s == nil || objectID == 0 || math.IsNaN(float64(amount)) ||
		math.IsInf(float64(amount), 0) || amount <= 0 {
		return Snapshot{}, 0, errors.New("invalid npc healing")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || !npc.IsPublished || npc.IsDefeated || npc.HitPoint <= 0 {
		return Snapshot{}, 0, fmt.Errorf("npcHealingUnavailable: %d", objectID)
	}
	maximumHitPoint := npc.Plan.NPCProfile.HitPoint
	if maximumHitPoint <= 0 {
		return Snapshot{}, 0, fmt.Errorf("npcHealingMaximum: %d", objectID)
	}
	previousHitPoint := npc.HitPoint
	if time.Now().Before(npc.status.healingReductionEnd) {
		amount *= 1 - npc.status.healingReduction
	}
	npc.HitPoint = min(maximumHitPoint, previousHitPoint+amount)
	healedAmount := npc.HitPoint - previousHitPoint
	s.npcs[objectID] = npc
	return npc, healedAmount, nil
}

func (s *Session) ConsumeDamageFlee(objectID uint32) bool {
	if s == nil || objectID == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsPublished ||
		!npc.status.isDamageFleePending {
		return false
	}
	npc.status.isDamageFleePending = false
	s.npcs[objectID] = npc
	return true
}

func (s *Session) IncreaseMana(
	objectID uint32, amount float32,
) (Snapshot, error) {
	if s == nil || objectID == 0 || amount <= 0 ||
		math.IsNaN(float64(amount)) || math.IsInf(float64(amount), 0) {
		return Snapshot{}, errors.New("invalid npc mana increase")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || !npc.IsPublished || npc.IsDefeated || npc.HitPoint <= 0 {
		return Snapshot{}, fmt.Errorf("npcManaUnavailable: %d", objectID)
	}
	npc.ManaPoint += amount
	s.npcs[objectID] = npc
	return npc, nil
}

func (s *Session) Resurrect(
	objectID uint32, hitPointFraction float32,
) (Snapshot, error) {
	if s == nil || objectID == 0 || hitPointFraction <= 0 ||
		hitPointFraction > 1 || math.IsNaN(float64(hitPointFraction)) ||
		math.IsInf(float64(hitPointFraction), 0) {
		return Snapshot{}, errors.New("invalid npc resurrection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || !npc.IsPublished || !npc.IsDefeated || npc.Plan.IsFixture {
		return Snapshot{}, fmt.Errorf("npcResurrectionUnavailable: %d", objectID)
	}
	maximumHitPoint := npc.Plan.NPCProfile.HitPoint
	if maximumHitPoint <= 0 {
		return Snapshot{}, fmt.Errorf("npcResurrectionMaximum: %d", objectID)
	}
	npc.HitPoint = maximumHitPoint * hitPointFraction
	npc.ManaPoint = npc.Plan.NPCProfile.PowerPoint
	npc.IsDefeated = false
	npc.TargetObjectID = 0
	npc.TargetFaction = FactionUnknown
	npc.TargetOwner = ActionOwner{}
	npc.IsActionStarted = false
	npc.ActionOwner = ActionOwner{}
	npc.status = status{}
	if npc.IsCorruptorStageTwo {
		profile, isProfileFound := scaldronBossChainLightningProfile(npc.Plan.NounName)
		if !isProfileFound {
			return Snapshot{}, fmt.Errorf("npcResurrectionCorruptorProfile: %d", objectID)
		}
		npc.Plan.ActionProfile = profile
		npc.Plan.IsActionKnown = true
	}
	s.npcs[objectID] = npc
	return npc, nil
}

func (s *Session) SetActionProfile(
	objectID uint32, profile ActionProfile,
) (Snapshot, error) {
	if s == nil || objectID == 0 || profile.Family == ActionUnknown ||
		profile.AbilityName == "" {
		return Snapshot{}, errors.New("invalid npc action profile")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || !npc.IsPublished || npc.IsDefeated || npc.Plan.IsFixture {
		return Snapshot{}, fmt.Errorf("npcActionProfileUnavailable: %d", objectID)
	}
	npc.Plan.ActionProfile = profile.Clone()
	npc.Plan.IsActionKnown = true
	if IsCorruptorNoun(npc.Plan.NounName) {
		npc.corruptorCombat.specialProfile = profile.Clone()
		npc.corruptorCombat.specialReadyTimestamp = 0
	}
	npc.IsActionStarted = false
	npc.ActionOwner = ActionOwner{}
	npc.ActionGeneration++
	s.npcs[objectID] = npc
	return npc, nil
}

func (s *Session) SetNounName(objectID uint32, nounName string) (Snapshot, error) {
	if s == nil || objectID == 0 || strings.TrimSpace(nounName) == "" {
		return Snapshot{}, errors.New("invalid npc noun replacement")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || !npc.IsPublished || npc.IsDefeated || npc.Plan.IsFixture {
		return Snapshot{}, fmt.Errorf("npcNounReplacementUnavailable: %d", objectID)
	}
	npc.Plan.NounName = nounName
	s.npcs[objectID] = npc
	return npc, nil
}

func (s *Session) DefeatedCandidates(
	source game.Vec3, maximumRange float32,
) []Snapshot {
	if s == nil || !zonegeometry.IsFinite(source) || maximumRange <= 0 ||
		math.IsNaN(float64(maximumRange)) || math.IsInf(float64(maximumRange), 0) {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	candidate := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if !npc.IsDefeated || !npc.IsPublished || npc.Plan.IsFixture ||
			zonegeometry.Distance(source, npc.Plan.Position) > maximumRange {
			continue
		}
		candidate = append(candidate, npc)
	}
	slices.SortFunc(candidate, func(left Snapshot, right Snapshot) int {
		leftDistance := zonegeometry.Distance(source, left.Plan.Position)
		rightDistance := zonegeometry.Distance(source, right.Plan.Position)
		if leftDistance < rightDistance {
			return -1
		}
		if leftDistance > rightDistance {
			return 1
		}
		return cmp.Compare(left.Plan.ObjectID, right.Plan.ObjectID)
	})
	return candidate
}

func (s *Session) FirstLowHealthAlly(
	source game.Vec3, maximumRange float32, healthFraction float32,
) (Snapshot, bool) {
	if s == nil || !zonegeometry.IsFinite(source) || maximumRange <= 0 ||
		healthFraction <= 0 || healthFraction >= 1 ||
		math.IsNaN(float64(maximumRange)) || math.IsInf(float64(maximumRange), 0) {
		return Snapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		maximumHitPoint := npc.Plan.NPCProfile.HitPoint
		if npc.IsDefeated || !npc.IsPublished || npc.Plan.IsFixture ||
			npc.HitPoint <= 0 || maximumHitPoint <= 0 ||
			npc.HitPoint/maximumHitPoint >= healthFraction ||
			zonegeometry.Distance(source, npc.Plan.Position) > maximumRange {
			continue
		}
		return npc, true
	}
	return Snapshot{}, false
}

func (s *Session) HasOtherSpeciesAlly(
	sourceObjectID uint32, maximumRange float32,
) bool {
	return len(s.otherSpeciesAllies(sourceObjectID, maximumRange, false)) > 0
}

func (s *Session) HasSameSpeciesAlly(
	sourceObjectID uint32, maximumRange float32,
) bool {
	if s == nil || sourceObjectID == 0 || maximumRange <= 0 ||
		math.IsNaN(float64(maximumRange)) ||
		math.IsInf(float64(maximumRange), 0) {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, isFound := s.npcs[sourceObjectID]
	if !isFound || source.IsDefeated || !source.IsPublished {
		return false
	}
	sourceSpecies := nounSpecies(source.Plan.NounName)
	for _, objectID := range s.objectIDs {
		candidate := s.npcs[objectID]
		if objectID == sourceObjectID || candidate.IsDefeated ||
			!candidate.IsPublished || candidate.Faction != source.Faction ||
			nounSpecies(candidate.Plan.NounName) != sourceSpecies ||
			zonegeometry.Distance(source.Plan.Position, candidate.Plan.Position) >
				maximumRange {
			continue
		}
		return true
	}
	return false
}

func (s *Session) HasLivingAlly(sourceObjectID uint32, maximumRange float32) bool {
	if s == nil || sourceObjectID == 0 || maximumRange <= 0 ||
		math.IsNaN(float64(maximumRange)) || math.IsInf(float64(maximumRange), 0) {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, isFound := s.npcs[sourceObjectID]
	if !isFound || source.IsDefeated || !source.IsPublished {
		return false
	}
	for _, objectID := range s.objectIDs {
		candidate := s.npcs[objectID]
		if objectID == sourceObjectID || candidate.Faction != source.Faction ||
			candidate.IsDefeated || !candidate.IsPublished || candidate.Plan.IsFixture ||
			candidate.HitPoint <= 0 || zonegeometry.Distance(
			source.Plan.Position, candidate.Plan.Position,
		) > maximumRange {
			continue
		}
		return true
	}
	return false
}

func (s *Session) FirstLivingAlly(
	sourceObjectID uint32, maximumRange float32,
) (Snapshot, bool) {
	if s == nil || sourceObjectID == 0 || maximumRange <= 0 ||
		math.IsNaN(float64(maximumRange)) || math.IsInf(float64(maximumRange), 0) {
		return Snapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, isFound := s.npcs[sourceObjectID]
	if !isFound || source.IsDefeated || !source.IsPublished {
		return Snapshot{}, false
	}
	allies := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		candidate := s.npcs[objectID]
		if objectID == sourceObjectID || candidate.Faction != source.Faction ||
			candidate.IsDefeated || !candidate.IsPublished || candidate.Plan.IsFixture ||
			candidate.HitPoint <= 0 || zonegeometry.Distance(
			source.Plan.Position, candidate.Plan.Position,
		) > maximumRange {
			continue
		}
		allies = append(allies, candidate)
	}
	slices.SortFunc(allies, func(left Snapshot, right Snapshot) int {
		leftDistance := zonegeometry.Distance(source.Plan.Position, left.Plan.Position)
		rightDistance := zonegeometry.Distance(source.Plan.Position, right.Plan.Position)
		if leftDistance < rightDistance {
			return -1
		}
		if leftDistance > rightDistance {
			return 1
		}
		return cmp.Compare(left.Plan.ObjectID, right.Plan.ObjectID)
	})
	if len(allies) == 0 {
		return Snapshot{}, false
	}
	return allies[0], true
}

func (s *Session) FocusAllies(
	sourceObjectID uint32, targetObjectID uint32, maximumRange float32,
) error {
	if s == nil || sourceObjectID == 0 || targetObjectID == 0 || maximumRange <= 0 ||
		math.IsNaN(float64(maximumRange)) || math.IsInf(float64(maximumRange), 0) {
		return errors.New("invalid npc ally focus")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	source, isFound := s.npcs[sourceObjectID]
	if !isFound || source.IsDefeated || !source.IsPublished {
		return fmt.Errorf("npcFocusSource: %d", sourceObjectID)
	}
	for _, objectID := range s.objectIDs {
		ally := s.npcs[objectID]
		if ally.Faction != source.Faction || ally.IsDefeated || !ally.IsPublished ||
			ally.Plan.IsFixture || zonegeometry.Distance(
			source.Plan.Position, ally.Plan.Position,
		) > maximumRange {
			continue
		}
		ally.TargetObjectID = targetObjectID
		ally.TargetFaction = FactionPlayerAligned
		ally.TargetOwner = source.TargetOwner
		s.npcs[objectID] = ally
	}
	return nil
}

func (s *Session) WoundedOtherSpeciesAllies(
	sourceObjectID uint32, maximumRange float32,
) []Snapshot {
	return s.otherSpeciesAllies(sourceObjectID, maximumRange, true)
}

func (s *Session) otherSpeciesAllies(
	sourceObjectID uint32, maximumRange float32, isWoundedOnly bool,
) []Snapshot {
	if s == nil || sourceObjectID == 0 || maximumRange <= 0 ||
		math.IsNaN(float64(maximumRange)) || math.IsInf(float64(maximumRange), 0) {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, isFound := s.npcs[sourceObjectID]
	if !isFound || source.IsDefeated || !source.IsPublished {
		return nil
	}
	sourceSpecies := nounSpecies(source.Plan.NounName)
	ally := make([]Snapshot, 0)
	for _, objectID := range s.objectIDs {
		candidate := s.npcs[objectID]
		maximumHitPoint := candidate.Plan.NPCProfile.HitPoint
		if objectID == sourceObjectID || candidate.Faction != source.Faction ||
			candidate.IsDefeated || !candidate.IsPublished || candidate.Plan.IsFixture ||
			candidate.HitPoint <= 0 || maximumHitPoint <= 0 ||
			nounSpecies(candidate.Plan.NounName) == sourceSpecies ||
			zonegeometry.Distance(source.Plan.Position, candidate.Plan.Position) > maximumRange {
			continue
		}
		if isWoundedOnly && candidate.HitPoint >= maximumHitPoint {
			continue
		}
		ally = append(ally, candidate)
	}
	return ally
}

func nounSpecies(nounName string) string {
	species := strings.TrimSuffix(strings.ToLower(nounName), ".noun")
	for _, suffix := range []string{"_3", "_2", "_captain"} {
		species = strings.TrimSuffix(species, suffix)
	}
	return species
}

func (s *Session) EndShield(objectID uint32) error {
	if s == nil || objectID == 0 {
		return errors.New("invalid npc shield")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated || !npc.IsShieldTriggered {
		return fmt.Errorf("npcShieldMissing: %d", objectID)
	}
	npc.IsShieldActive = false
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) EndTurtle(objectID uint32) error {
	if s == nil || objectID == 0 {
		return errors.New("invalid npc turtle")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || !npc.IsTurtleTriggered {
		return fmt.Errorf("npcTurtleMissing: %d", objectID)
	}
	npc.IsTurtleActive = false
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) StartShield(objectID uint32) error {
	if s == nil || objectID == 0 {
		return errors.New("invalid npc shield")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcShieldMissing: %d", objectID)
	}
	npc.IsShieldTriggered = true
	npc.IsShieldActive = true
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) FacePosition(objectID uint32, position game.Vec3) error {
	if s == nil || objectID == 0 || !zonegeometry.IsFinite(position) {
		return errors.New("invalid npc facing")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcFacingMissing: %d", objectID)
	}
	facing := directionTo(npc.Plan.Position, position)
	if facing.Length() <= 0 {
		return errors.New("npc facing direction unavailable")
	}
	npc.Facing = facing
	s.npcs[objectID] = npc
	return nil
}

func (s *Session) LiveSnapshots() []Snapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshots := make([]Snapshot, 0, len(s.objectIDs))
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated {
			continue
		}
		snapshots = append(snapshots, npc)
	}
	return snapshots
}

func (s *Session) LivingActorCount() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated || npc.Plan.IsFixture {
			continue
		}
		count++
	}
	return count
}

func (s *Session) LivingMarkerSetActorCount(markerSetName string) int {
	if s == nil || markerSetName == "" {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated || npc.HitPoint <= 0 ||
			npc.Plan.MarkerSetName != markerSetName {
			continue
		}
		count++
	}
	return count
}

func (s *Session) LiveActorObjectIDs() []uint32 {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	objectIDs := make([]uint32, 0, len(s.objectIDs))
	for _, objectID := range s.objectIDs {
		npc := s.npcs[objectID]
		if npc.IsDefeated || !npc.IsPublished || npc.Plan.IsFixture ||
			npc.HitPoint <= 0 {
			continue
		}
		objectIDs = append(objectIDs, objectID)
	}
	slices.Sort(objectIDs)
	return objectIDs
}

func (s *Session) Snapshots() []Snapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshots := make([]Snapshot, 0, len(s.objectIDs))
	for _, objectID := range s.objectIDs {
		snapshots = append(snapshots, s.npcs[objectID])
	}
	return snapshots
}

func (s *Session) NPC(objectID uint32) (Snapshot, bool) {
	if s == nil || objectID == 0 {
		return Snapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	npc, isFound := s.npcs[objectID]
	return npc, isFound
}

func (s *Session) LiveNPC(objectID uint32) (Snapshot, bool) {
	npc, isFound := s.NPC(objectID)
	if !isFound || npc.IsDefeated || !npc.IsPublished || npc.HitPoint <= 0 {
		return Snapshot{}, false
	}
	return npc, true
}

func (s *Session) SetPosition(objectID uint32, position game.Vec3) error {
	if s == nil || objectID == 0 || !zonegeometry.IsFinite(position) {
		return errors.New("npc position invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	npc, isFound := s.npcs[objectID]
	if !isFound || npc.IsDefeated {
		return fmt.Errorf("npcPositionMissing: %d", objectID)
	}
	// Teleports and forced corrections preserve heading.
	npc.Plan.Position = position
	s.npcs[objectID] = npc
	return nil
}

func directionTo(source game.Vec3, target game.Vec3) game.Vec3 {
	direction := target.Sub(source)
	length := direction.Length()
	if length <= 0 {
		return game.Vec3{}
	}
	return direction.Scale(1 / length)
}

func isPreAggroInvisibleNoun(nounName string) bool {
	switch strings.ToLower(nounName) {
	case "zelembasicranged.noun", "nomadsnipe.noun":
		return true
	default:
		return false
	}
}
