package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sporenet"
	abilityraknet "github.com/darkspinnet/darkspin/server/zone/ability/raknet103"
	zonedifficulty "github.com/darkspinnet/darkspin/server/zone/difficulty"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

// Chunk688 hits every valid hostile at the fixed landing point. Only when no
// hostile occupies that area does it create Sloth.Noun instead. CreateObject
// does not establish GetOwnerID: the new actor survives its mortar's death.
func (e campaignNPCLobSchedule) babyLandingLocked(member *gameplayPeerSession) ([][]byte, error) {
	center := game.Vec3{X: e.toss.Destination.X, Y: e.toss.Destination.Y, Z: e.toss.Destination.Z}
	targets := campaignLobAreaTargets(member.zone.LiveNPCTargets(), center, e.plan.Profile.Radius)
	source, isSourceFound := member.zone.NPCs().NPC(e.objectID)
	if !isSourceFound || member.zone.NPCRandom() == nil {
		e.runtime.registry.mutex.Unlock()
		return nil, nil
	}
	var packets [][]byte
	statDelta := sporenet.PlayerStatDelta{}
	for _, target := range targets {
		plan, err := zonenpc.PlanAreaAttackWithProfile(source, target.ObjectID, target.Position, e.plan.Profile)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("babyAreaPlan", err)
		}
		result, err := zonenpc.CommitAttack(member.zone.NPCRandom(), plan, source.EffectiveCriticalRating(), e.runtime.program.Critical)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("babyAreaCommit", err)
		}
		hitPackets, targetStatDelta, isApplied, err := e.runtime.applyEnemyAreaAttackDamage(member, e.generation, plan, result, e.landingTimestamp())
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("babyAreaDamage", err)
		}
		if isApplied {
			statDelta.PVEDamageTaken += targetStatDelta.PVEDamageTaken
		}
		packets = append(packets, hitPackets...)
	}
	var plans []zonenpc.SpawnPlan
	if len(targets) == 0 {
		plan, spawnPackets, err := e.planBabySlothLocked(member, center)
		if err != nil {
			e.runtime.registry.mutex.Unlock()
			return e.fail("babySloth", err)
		}
		plans = append(plans, plan)
		packets = append(packets, spawnPackets...)
	}
	e.runtime.registry.sessions[e.sessionKey] = *member
	e.runtime.registry.mutex.Unlock()
	var landingPackets [][]byte
	var err error
	if len(targets) == 0 {
		deletePacket, deleteErr := raknet.MarshalApplication(raknet.ObjectDeleteMessage{ObjectID: []uint32{e.projectileObjectID}})
		if deleteErr != nil {
			return e.fail("babyEmptyDelete", deleteErr)
		}
		landingPackets = append([][]byte{deletePacket}, packets...)
	} else {
		landingPackets, err = abilityraknet.TossDirectLanding(e.toss, e.projectileObjectID, packets)
		if err != nil {
			return e.fail("babyLanding", err)
		}
	}
	if len(plans) > 0 {
		actionPackets, actionErr := e.runtime.scheduleFirstActions(e.packet, e.sessionKey, e.generation, plans, e.landingTimestamp())
		if actionErr != nil {
			return e.fail("babySlothAction", actionErr)
		}
		landingPackets = append(landingPackets, actionPackets...)
	}
	err = e.runtime.stats.Record(member.zone.Context(), e.binding, statDelta)
	if err != nil && e.runtime.logger != nil {
		e.runtime.logger.Printf("BabyToss statistics source=%d: %v", e.objectID, err)
	}
	return landingPackets, nil
}

func (e campaignNPCLobSchedule) planBabySlothLocked(member *gameplayPeerSession, position game.Vec3) (zonenpc.SpawnPlan, [][]byte, error) {
	profile, err := zonedifficulty.ProjectOwnedSummon(member.zone.DirectorDefinition(), "Sloth.Noun")
	if err != nil {
		return zonenpc.SpawnPlan{}, nil, fmt.Errorf("slothClass: %w", err)
	}
	objectID, err := member.reserveCampaignObjectID()
	if err != nil {
		return zonenpc.SpawnPlan{}, nil, fmt.Errorf("slothObject: %w", err)
	}
	action, isKnown := zonenpc.ActionProfileForNoun("Sloth.Noun")
	if !isKnown {
		return zonenpc.SpawnPlan{}, nil, fmt.Errorf("slothAction: unavailable")
	}
	plan := zonenpc.SpawnPlan{ObjectID: objectID, NounName: "Sloth.Noun", Position: position, LocusID: e.source.Plan.LocusID, MarkerSetName: e.source.Plan.MarkerSetName, IsEncounterAuxiliary: true, NPCProfile: profile, ActionProfile: action, IsActionKnown: true}
	err = member.zone.NPCs().Add([]zonenpc.SpawnPlan{plan}, e.plan.TargetObjectID)
	if err != nil {
		return zonenpc.SpawnPlan{}, nil, fmt.Errorf("slothAdd: %w", err)
	}
	packets, err := npcraknet.TargetedSpawn(plan, e.plan.TargetObjectID)
	if err != nil {
		rollbackErr := member.zone.NPCs().RollbackAdd([]zonenpc.SpawnPlan{plan})
		return zonenpc.SpawnPlan{}, nil, fmt.Errorf("slothMarshal: %w", errors.Join(err, rollbackErr))
	}
	err = member.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{Plans: []zonenpc.SpawnPlan{plan}, TargetObjectID: e.plan.TargetObjectID}, member.binding.UserID, member.generation)
	if err != nil {
		rollbackErr := member.zone.NPCs().RollbackAdd([]zonenpc.SpawnPlan{plan})
		return zonenpc.SpawnPlan{}, nil, fmt.Errorf("slothPublish: %w", errors.Join(err, rollbackErr))
	}
	return plan, packets, nil
}
