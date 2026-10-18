package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// VentFlameCone (chunk 1008) waits a random 3–5 seconds, warns for two
// seconds, then emits hazard_plasma_geyser and one Elements/Energy hit.
// The pointer is shared by party sessions; each member publishes that phase
// once and takes damage independently, including while standing still.
type campaignFireVent struct {
	warningAt      time.Time
	eruptionAt     time.Time
	cycle          uint64
	completedCycle uint64
	completedAt    time.Time
}

func (e *gameplayPeerSession) pollFireVents(runtime campaignNPCActionRuntime, timestamp uint64, now time.Time) ([][]byte, sporenet.PlayerStatDelta, error) {
	packets := make([][]byte, 0)
	stats := sporenet.PlayerStatDelta{}
	if e.fireVents == nil {
		e.fireVents = make(map[uint32]*campaignFireVent)
		e.fireVentWarnings = make(map[uint32]uint64)
		e.fireVentEruptions = make(map[uint32]uint64)
	}
	for _, vent := range e.fireVentSources() {
		if e.zone.NPCRandom() == nil {
			return nil, stats, fmt.Errorf("ventRandom: random unavailable")
		}
		objectID := vent.Plan.ObjectID
		state := e.fireVents[objectID]
		if state == nil {
			for _, ally := range runtime.registry.sessions {
				if ally.zone == e.zone && ally.fireVents[objectID] != nil {
					state = ally.fireVents[objectID]
					break
				}
			}
			if state == nil {
				state = &campaignFireVent{cycle: 1}
				state.rest(e, now)
			}
			e.fireVents[objectID] = state
		}
		if !now.Before(state.eruptionAt) {
			state.completedCycle = state.cycle
			state.completedAt = now
			state.cycle++
			state.rest(e, now)
		}
		if !now.Before(state.warningAt) && e.fireVentWarnings[objectID] != state.cycle {
			asset := util.HashID("effect_Environment_Cryos_Geyser_Warning.ServerEventDef")
			packet, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{Asset: &asset, ObjectID: &objectID})
			if err != nil {
				return nil, stats, fmt.Errorf("ventWarning: %w", err)
			}
			e.fireVentWarnings[objectID] = state.cycle
			packets = append(packets, packet)
		}
		cycle := state.completedCycle
		if cycle == 0 || !now.Before(state.completedAt.Add(time.Second)) {
			continue
		}
		if e.fireVentEruptions[objectID] != cycle {
			asset := util.HashID("hazard_plasma_geyser")
			position := raknet.Vector3(vent.Plan.Position)
			facing := raknet.Vector3(vent.Facing.Scale(-1))
			packet, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{
				SimpleSwarmEffectID: &asset, Position: &position, Facing: &facing,
			})
			if err != nil {
				return nil, stats, fmt.Errorf("ventEruption: %w", err)
			}
			e.fireVentEruptions[objectID] = cycle
			packets = append(packets, packet)
			if e.deployedObjectID != 0 && e.deployedHitPoint() > 0 {
				target, isFound := e.campaignNPCTarget(e.generation, e.deployedObjectID)
				if isFound && target.Position.Sub(vent.Plan.Position).Length() <= 0.1+target.ActorFootprintRadius {
					profile := zonenpc.ActionProfile{Family: zonenpc.ActionRetainedArea,
						AbilityName: "VentFlameCone", MinimumDamage: 10, MaximumDamage: 20,
						IsRetainedVolumeDamage: true,
						DamageType:             3, DamageSource: 1, DescriptorMask: 1 << 14, IsDamageProfileKnown: true,
					}
					plan, err := zonenpc.PlanRetainedAreaAttackWithProfile(vent, target.ObjectID, target.Position, profile)
					if err != nil {
						return nil, stats, fmt.Errorf("ventPlan: %w", err)
					}
					result, err := zonenpc.CommitAttack(e.zone.NPCRandom(), plan, 0, runtime.program.Critical)
					if err != nil {
						return nil, stats, fmt.Errorf("ventRoll: %w", err)
					}
					hits, delta, isApplied, err := runtime.applyEnemyDamage(e, e.generation, plan, result, timestamp, false, false, false)
					if err != nil {
						return nil, stats, fmt.Errorf("ventDamage: %w", err)
					}
					packets = append(packets, hits...)
					stats.PVEDamageTaken += delta.PVEDamageTaken
					if isApplied {
						asset = 0x78da313e
						packet, err = raknet.MarshalApplication(raknet.ServerEventContractMessage{SimpleSwarmEffectID: &asset, ObjectID: &target.ObjectID})
						if err != nil {
							return nil, stats, fmt.Errorf("ventHit: %w", err)
						}
						packets = append(packets, packet)
					}
				}
			}
		}
	}
	return packets, stats, nil
}

// Some maps place the bare, noncombatant vent instead of its damageable prefab.
// Keep selected scenery sources alongside live fixtures, without inventing NPC
// health or allowing destroyed/unpublished fixtures to restart their passive.
func (e *gameplayPeerSession) fireVentSources() []zonenpc.Snapshot {
	vents := make([]zonenpc.Snapshot, 0)
	seenObjectIDs := make(map[uint32]bool)
	for _, vent := range e.zone.NPCs().Snapshots() {
		if game.SceneryHazardAbility(vent.Plan.NounName) != "VentFlameCone" {
			continue
		}
		seenObjectIDs[vent.Plan.ObjectID] = true
		if vent.IsPublished && !vent.IsDefeated {
			vents = append(vents, vent)
		}
	}
	director := e.zone.DirectorDefinition()
	if !director.IsInitialLayoutSelected {
		return vents
	}
	for _, set := range director.MarkerSets {
		for _, definition := range set.Definitions {
			if definition.MarkerID == 0 || seenObjectIDs[definition.MarkerID] ||
				game.SceneryHazardAbility(definition.NounName) != "VentFlameCone" {
				continue
			}
			fixture, isFixtureFound := e.zone.NPCs().NPC(definition.MarkerID)
			if isFixtureFound && (!fixture.IsPublished || fixture.IsDefeated) {
				continue
			}
			seenObjectIDs[definition.MarkerID] = true
			plan := zonenpc.SpawnPlan{ObjectID: definition.MarkerID,
				NounName: definition.NounName, Position: definition.Position, Rotation: definition.Rotation}
			vents = append(vents, zonenpc.Snapshot{Plan: plan, Facing: plan.InitialFacing()})
		}
	}
	return vents
}

func (e *campaignFireVent) rest(member *gameplayPeerSession, now time.Time) {
	rest := 3 + int(member.zone.NPCRandom().Float64()*3)
	e.warningAt = now.Add(time.Duration(rest) * time.Second)
	e.eruptionAt = e.warningAt.Add(2 * time.Second)
}
