package npc

import (
	"errors"
	"fmt"
	"math"

	"github.com/darkspinnet/darkspin/server/game"
)

// AttributeMirror retains the explicit MirrorBaseAttributes link independently
// of ownership. Authored coefficient/cap arrays remain on the child's profile.
type AttributeMirror struct {
	SourceObjectID      uint32
	Tuning              game.AttributeMirrorTuning
	HealthModifierScale float32
	HealthMultiplier    float32
	BaseAttributes      [115]float32
	MovementSpeedBuff   float32
}

func (e *Session) AttachAttributeMirror(childObjectID, sourceObjectID uint32, tuning game.AttributeMirrorTuning) (Snapshot, error) {
	if e == nil || childObjectID == 0 || sourceObjectID == 0 || childObjectID == sourceObjectID || !tuning.IsKnown {
		return Snapshot{}, errors.New("attribute mirror attachment invalid")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	child, isChildFound := e.npcs[childObjectID]
	source, isSourceFound := e.npcs[sourceObjectID]
	if !isChildFound || !isSourceFound || child.IsDefeated || !child.Plan.NPCProfile.AreClassAttributeBasesKnown || !source.Plan.NPCProfile.AreClassAttributeBasesKnown {
		return Snapshot{}, errors.New("attribute mirror actor baseline unavailable")
	}
	if child.Plan.AttributeMirror.SourceObjectID != 0 {
		return Snapshot{}, errors.New("attribute mirror already attached")
	}
	if child.Plan.NPCProfile.ClassAttributeBases[0] <= 0 {
		return Snapshot{}, errors.New("attribute mirror health coefficient unavailable")
	}
	for ancestorID := sourceObjectID; ancestorID != 0; {
		if ancestorID == childObjectID {
			return Snapshot{}, errors.New("attribute mirror source cycle")
		}
		ancestor, isFound := e.npcs[ancestorID]
		if !isFound {
			break
		}
		ancestorID = ancestor.Plan.AttributeMirror.SourceObjectID
	}
	oldMaximum := child.Plan.NPCProfile.HitPoint
	fraction := float32(1)
	if oldMaximum > 0 {
		fraction = child.HitPoint / oldMaximum
	}
	modifierScale := float32(1)
	if child.Plan.NPCProfile.BaseHitPoint > 0 {
		modifierScale = oldMaximum / child.Plan.NPCProfile.BaseHitPoint
	}
	healthMultiplier := oldMaximum / child.Plan.NPCProfile.ClassAttributeBases[0]
	if child.Plan.NPCProfile.BaseHitPoint > 0 {
		healthMultiplier = child.Plan.NPCProfile.BaseHitPoint / child.Plan.NPCProfile.ClassAttributeBases[0]
	}
	child.Plan.AttributeMirror = AttributeMirror{SourceObjectID: sourceObjectID, Tuning: tuning, HealthModifierScale: modifierScale, HealthMultiplier: healthMultiplier}
	projected, err := projectAttributeMirror(child, source)
	if err != nil {
		return Snapshot{}, fmt.Errorf("mirrorInitial: %w", err)
	}
	projected.HitPoint = min(max(fraction*projected.Plan.NPCProfile.HitPoint, 0), projected.Plan.NPCProfile.HitPoint)
	// Publish a current-HP initial baseline when the caller marshals this plan.
	projected.Plan.InitialHitPoint = projected.HitPoint
	e.npcs[childObjectID] = projected
	e.indexAttributeMirrorLocked(projected.Plan)
	delete(e.attributeMirrorChanges, childObjectID)
	return projected, nil
}

// RefreshAttributeMirrors returns accumulated changes even if a prior getter
// refreshed the child. The gameplay adapter owns broadcasting these snapshots.
func (e *Session) RefreshAttributeMirrors() []Snapshot {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.refreshAttributeMirrorsLocked()
	if len(e.attributeMirrorChanges) == 0 {
		return nil
	}
	snapshots := make([]Snapshot, 0, len(e.attributeMirrorChanges))
	for _, objectID := range e.objectIDs {
		if !e.attributeMirrorChanges[objectID] {
			continue
		}
		snapshot, isFound := e.npcs[objectID]
		if isFound && !snapshot.IsDefeated {
			snapshots = append(snapshots, snapshot)
		}
	}
	return snapshots
}

func (e *Session) ConfirmAttributeMirrorChanges(snapshots []Snapshot) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, snapshot := range snapshots {
		current, isFound := e.npcs[snapshot.Plan.ObjectID]
		if !isFound || (mirrorAttributes(current) == mirrorAttributes(snapshot) && current.HitPoint == snapshot.HitPoint && current.ManaPoint == snapshot.ManaPoint) {
			delete(e.attributeMirrorChanges, snapshot.Plan.ObjectID)
		}
	}
}

func validateAttributeMirrorPlan(plan SpawnPlan) error {
	mirror := plan.AttributeMirror
	if math.IsNaN(float64(plan.PetHealthIncrease)) || math.IsInf(float64(plan.PetHealthIncrease), 0) || plan.PetHealthIncrease <= -1 {
		return errors.New("invalid pet health increase")
	}
	if mirror.SourceObjectID == 0 {
		return nil
	}
	for _, scalar := range mirror.Tuning.GlobalBaseMaximums {
		if math.IsNaN(float64(scalar)) || math.IsInf(float64(scalar), 0) {
			return errors.New("invalid mirror base limit")
		}
	}
	for _, scalar := range mirror.Tuning.GlobalMaximums {
		if math.IsNaN(float64(scalar)) || math.IsInf(float64(scalar), 0) {
			return errors.New("invalid mirror final limit")
		}
	}
	for _, scalar := range []float32{mirror.Tuning.HealthPerStrength, mirror.Tuning.ManaPerMind, mirror.Tuning.PhysicalDefensePerDexterity, mirror.Tuning.EnergyDefensePerMind, mirror.Tuning.CriticalPerDexterity} {
		if scalar < 0 || math.IsNaN(float64(scalar)) || math.IsInf(float64(scalar), 0) {
			return errors.New("invalid mirror derivation")
		}
	}
	if mirror.SourceObjectID == plan.ObjectID || !mirror.Tuning.IsKnown || !plan.NPCProfile.AreClassAttributeBasesKnown {
		return errors.New("invalid attribute mirror identity")
	}
	for _, scale := range []float32{mirror.HealthMultiplier, mirror.HealthModifierScale} {
		if scale <= 0 || math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
			return errors.New("invalid attribute mirror health scale")
		}
	}
	return nil
}

func validateAttributeMirrorSnapshots(snapshots []Snapshot) error {
	sourcesByID := make(map[uint32]uint32, len(snapshots))
	for _, snapshot := range snapshots {
		sourcesByID[snapshot.Plan.ObjectID] = snapshot.Plan.AttributeMirror.SourceObjectID
	}
	for _, snapshot := range snapshots {
		visitedIDs := make(map[uint32]bool)
		for objectID := snapshot.Plan.ObjectID; objectID != 0; objectID = sourcesByID[objectID] {
			if visitedIDs[objectID] {
				return fmt.Errorf("mirrorRestoreCycle[%d]: repeated source", objectID)
			}
			visitedIDs[objectID] = true
		}
	}
	return nil
}

func (e *Session) refreshAttributeMirrorsLocked() {
	if len(e.attributeMirrorIDs) == 0 {
		return
	}
	// Attachment rejects cycles. Multiple passes resolve a child created before
	// a subsequently linked ancestor without depending on object-ID order.
	for pass := 0; pass < len(e.attributeMirrorIDs); pass++ {
		isChanged := false
		for _, objectID := range e.attributeMirrorIDs {
			child := e.npcs[objectID]
			if child.IsDefeated || child.Plan.AttributeMirror.SourceObjectID == 0 {
				continue
			}
			source, isFound := e.npcs[child.Plan.AttributeMirror.SourceObjectID]
			if !isFound {
				continue
			} // Native retains the last cached attributes.
			projected, err := projectAttributeMirror(child, source)
			if err != nil {
				continue
			} // Keep the last valid state; attachment validates inputs.
			if mirrorAttributes(projected) == mirrorAttributes(child) {
				continue
			}
			e.npcs[objectID] = projected
			if e.attributeMirrorChanges == nil {
				e.attributeMirrorChanges = make(map[uint32]bool)
			}
			e.attributeMirrorChanges[objectID] = true
			isChanged = true
		}
		if !isChanged {
			return
		}
	}
}

func projectAttributeMirror(child, source Snapshot) (Snapshot, error) {
	profile := child.Plan.NPCProfile
	bases := profile.ClassAttributeBases
	mirror := child.Plan.AttributeMirror
	tuning := mirror.Tuning
	sourceBases := sourceMirrorBases(source, tuning)
	strength := mirrorLimit(bases[2]*sourceBases[0], 0, 15, bases, tuning)
	dexterity := mirrorLimit(bases[3]*sourceBases[1], 1, 16, bases, tuning)
	mind := mirrorLimit(bases[4]*sourceBases[2], 2, 17, bases, tuning)
	sourceHealth := sourceBases[4]
	health := bases[0] * sourceHealth * (1 + source.Plan.PetHealthIncrease)
	unprojectedHealth := mirrorBaseLimit(health, 4, 13, bases, tuning)
	health = unprojectedHealth * mirror.HealthMultiplier * mirror.HealthModifierScale
	health = mirrorFinalLimit(health, 4, tuning)
	mana := mirrorLimit(bases[1]+mind*tuning.ManaPerMind, 5, 14, bases, tuning)
	physical := mirrorLimit(bases[5]*sourceBases[7], 7, 18, bases, tuning)
	energy := mirrorLimit(bases[7]*sourceBases[9], 9, 20, bases, tuning)
	critical := mirrorLimit(bases[8]*sourceBases[10], 10, 21, bases, tuning)
	moveBuff := mirrorFinalLimit(mirrorBaseLimit(bases[12]*sourceBases[48], 48, -1, bases, tuning), 48, tuning)
	for _, attribute := range []float32{strength, dexterity, mind, health, mana, physical, energy, critical, moveBuff} {
		if attribute < 0 || math.IsNaN(float64(attribute)) || math.IsInf(float64(attribute), 0) {
			return Snapshot{}, errors.New("attribute mirror projection invalid")
		}
	}
	if health <= 0 {
		return Snapshot{}, errors.New("attribute mirror health unavailable")
	}
	profile.Strength, profile.Dexterity, profile.Mind = strength, dexterity, mind
	profile.HitPoint, profile.BaseHitPoint, profile.PowerPoint = health, unprojectedHealth*mirror.HealthMultiplier, mana
	child.Plan.AttributeMirror.BaseAttributes[0] = strength
	child.Plan.AttributeMirror.BaseAttributes[1] = dexterity
	child.Plan.AttributeMirror.BaseAttributes[2] = mind
	child.Plan.AttributeMirror.BaseAttributes[4] = unprojectedHealth
	child.Plan.AttributeMirror.BaseAttributes[7] = physical
	child.Plan.AttributeMirror.BaseAttributes[9] = energy
	child.Plan.AttributeMirror.BaseAttributes[10] = critical
	child.Plan.AttributeMirror.BaseAttributes[48] = moveBuff
	profile.DodgeRating, profile.ResistRating, profile.CriticalRating = physical, energy, critical
	child.Plan.NPCProfile = profile
	child.Plan.AttributeMirror.MovementSpeedBuff = moveBuff
	child.HitPoint = min(child.HitPoint, health)
	child.ManaPoint = min(child.ManaPoint, mana)
	return child, nil
}

func mirrorLimit(attribute float32, id, capIndex int, bases [22]float32, tuning game.AttributeMirrorTuning) float32 {
	return mirrorFinalLimit(mirrorBaseLimit(attribute, id, capIndex, bases, tuning), id, tuning)
}

func mirrorBaseLimit(attribute float32, id, capIndex int, bases [22]float32, tuning game.AttributeMirrorTuning) float32 {
	if capIndex >= 0 && bases[capIndex] > 0 {
		attribute = min(attribute, bases[capIndex])
	}
	if tuning.GlobalBaseMaximums[id] != 0 {
		attribute = min(attribute, tuning.GlobalBaseMaximums[id])
	}
	return attribute
}

func mirrorFinalLimit(attribute float32, id int, tuning game.AttributeMirrorTuning) float32 {
	if tuning.GlobalMaximums[id] != 0 {
		attribute = min(attribute, tuning.GlobalMaximums[id])
	}
	return attribute
}

func mirrorAttributes(actor Snapshot) [9]float32 {
	profile := actor.Plan.NPCProfile
	return [9]float32{profile.Strength, profile.Dexterity, profile.Mind, profile.HitPoint, profile.PowerPoint, profile.DodgeRating, profile.ResistRating, profile.CriticalRating, actor.Plan.AttributeMirror.MovementSpeedBuff}
}

func sourceMirrorBases(source Snapshot, tuning game.AttributeMirrorTuning) [115]float32 {
	if source.Plan.AttributeMirror.SourceObjectID != 0 {
		return source.Plan.AttributeMirror.BaseAttributes
	}
	bases := source.Plan.NPCProfile.ClassAttributeBases
	attributes := [115]float32{}
	attributes[0] = mirrorBaseLimit(bases[2], 0, 15, bases, tuning)
	attributes[1] = mirrorBaseLimit(bases[3], 1, 16, bases, tuning)
	attributes[2] = mirrorBaseLimit(bases[4], 2, 17, bases, tuning)
	attributes[4] = mirrorBaseLimit(bases[0]+attributes[0]*tuning.HealthPerStrength, 4, 13, bases, tuning)
	attributes[7] = mirrorBaseLimit(bases[5]+attributes[1]*tuning.PhysicalDefensePerDexterity, 7, 18, bases, tuning)
	attributes[9] = mirrorBaseLimit(bases[7]+attributes[2]*tuning.EnergyDefensePerMind, 9, 20, bases, tuning)
	attributes[10] = mirrorBaseLimit(bases[8]+attributes[1]*tuning.CriticalPerDexterity, 10, 21, bases, tuning)
	attributes[48] = mirrorBaseLimit(bases[12], 48, -1, bases, tuning)
	return attributes
}

func (e *Session) indexAttributeMirrorLocked(plan SpawnPlan) {
	if plan.AttributeMirror.SourceObjectID == 0 {
		return
	}
	for _, objectID := range e.attributeMirrorIDs {
		if objectID == plan.ObjectID {
			return
		}
	}
	e.attributeMirrorIDs = append(e.attributeMirrorIDs, plan.ObjectID)
}
