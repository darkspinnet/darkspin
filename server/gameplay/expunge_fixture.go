package gameplay

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type expungeFixtureBurst struct {
	state      *campaignFixtureBurnState
	definition sim.AbilityDefinition
	targetID   uint32
}

// The caller holds the registry lock. Typed metadata distinguishes these
// namespaces from unrelated NPC-owned modifiers with no remaining-damage API.
func (e *gameplayPeerSession) consumeExpungeFixtures(targetID uint32, at time.Time, pool *modifierPool) ([]expungeFixtureBurst, []expungeModifierDelete, []func(), error) {
	instanceIDs := make([]uint32, 0)
	for instanceID, run := range e.campaignNPCModifiers {
		if run != nil && run.fixtureBurn != nil && !run.fixtureBurn.isRetiring && run.record.TargetObjectID == targetID && run.isActive() {
			instanceIDs = append(instanceIDs, instanceID)
		}
	}
	sort.Slice(instanceIDs, func(left, right int) bool { return instanceIDs[left] < instanceIDs[right] })
	bursts := make([]expungeFixtureBurst, 0, len(instanceIDs))
	deletes := make([]expungeModifierDelete, 0, len(instanceIDs))
	cancels := make([]func(), 0, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		run := e.campaignNPCModifiers[instanceID]
		state := run.fixtureBurn
		definition, isKnown := state.expungeDefinition(at)
		if !isKnown {
			continue
		}
		isCreated, isDeferred, err := retireFixtureRunLocked(run, pool)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("fixtureRelease: %w", err)
		}
		if !isCreated {
			continue
		}
		if !isDeferred {
			e.untrackCampaignNPCModifier(run)
		}
		if !isDeferred && run.cancel != nil {
			cancels = append(cancels, run.cancel)
			run.cancel = nil
		}
		bursts = append(bursts, expungeFixtureBurst{state: state, definition: definition, targetID: targetID})
		if !isDeferred {
			deletes = append(deletes, expungeModifierDelete{targetObjectID: targetID, instanceID: instanceID})
		}
	}
	return bursts, deletes, cancels, nil
}

func (e *campaignFixtureBurnState) expungeDefinition(at time.Time) (sim.AbilityDefinition, bool) {
	source := sim.AbilityDefinition{DamageType: 3, DamageSource: 1, IsDamageTypeFound: true, IsDamageSourceFound: true}
	switch e.profile.ModifierName {
	case "LifePlagueSpread":
		source.DamageType = 2
	case "PlasmaSentinelBurn", "CitadelPlasmaBurn":
	default:
		return sim.AbilityDefinition{}, false
	}
	// Native duration zero remains zero in GetRemainingDuration. DOT149 does
	// not turn a permanent modifier into an infinite Expunge damage budget.
	tickCount := uint32(0)
	if e.profile.ModifierDuration > 0 {
		tickCount = expungeRemainingTickCount(e.expiresAt, at, time.Second)
	}
	return expungeDamageDefinition(source, tickCount, e.profile.ModifierMinimumTickDamage, e.profile.ModifierMaximumTickDamage), true
}

func (e *campaignFixtureBurnState) projectDamage(profile zonenpc.ActionProfile) (game.DamageRange, error) {
	damage, err := game.ResolveAbilityDamageRange(game.AbilityDamage{
		Minimum: profile.MinimumDamage, Maximum: profile.MaximumDamage,
		Coefficient: profile.DamageCoefficient, Descriptor: profile.DescriptorMask,
		DamageType: profile.DamageType, DamageSource: profile.DamageSource,
		IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true,
	}, e.sourceProfile)
	if err != nil {
		return game.DamageRange{}, fmt.Errorf("fixtureProjection: %w", err)
	}
	multiplier := e.blast.source.Plan.NPCProfile.DifficultyDamageMultiplier
	if multiplier == 0 {
		multiplier = 1
	}
	damage.Minimum = float32(math.Floor(float64(damage.Minimum * multiplier)))
	damage.Maximum = float32(math.Ceil(float64(damage.Maximum * multiplier)))
	return damage, nil
}

func (e expungeFixtureBurst) produce(timestamp uint64) ([][]byte, bool, error) {
	blast := e.state.blast
	blast.timestamp = timestamp
	profile := zonenpc.ActionProfile{
		MinimumDamage: e.definition.MinimumDamage, MaximumDamage: e.definition.MaximumDamage,
		DamageCoefficient: e.definition.DamageCoefficient, DescriptorMask: e.definition.DescriptorMask,
		DamageType: uint8(e.definition.DamageType), DamageSource: uint8(e.definition.DamageSource), IsDamageProfileKnown: true,
	}
	damage, err := e.state.projectDamage(profile)
	if err != nil {
		return nil, false, fmt.Errorf("burstProjection: %w", err)
	}
	// A zero remaining budget still removes the consumed modifier, but cannot
	// invoke a positive-damage-only NPC attack planner.
	if damage.Maximum <= 0 {
		return nil, true, nil
	}
	packets, isSurviving, err := blast.damageProjected(e.targetID, profile, true, nil, &damage)
	if err != nil {
		return packets, false, fmt.Errorf("burstDamage: %w", err)
	}
	return packets, isSurviving, nil
}
