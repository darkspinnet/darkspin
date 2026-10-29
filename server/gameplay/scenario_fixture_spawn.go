//go:build scenario

package gameplay

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

func (e gameplayPendingRuntime) spawnScenarioFixture(packet raknet.Packet, queued gameplayPeerSession, receipt scenario.SpawnReceipt) ([][]byte, bool, error) {
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[packet.Address.String()]
	if !isFound || member.generation != queued.generation || member.transportGeneration != packet.TransportGeneration || member.zone == nil || member.isZoneTerminal() {
		e.registry.mutex.Unlock()
		return nil, true, errors.New("fixture packet owner is stale")
	}
	err := e.registry.validateScenarioSpawnLocked(receipt.RequestID, member)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("fixtureSpawnIdentity: %w", err)
	}
	profile, isProfileFound := member.zone.DirectorDefinition().FixtureProfilesByNoun[strings.ToLower(receipt.NounName)]
	if !isProfileFound || !profile.IsKnown || !profile.IsClassKnown || profile.AIGraph == nil || !profile.AIGraph.IsResolved || profile.HitPoint <= 0 {
		e.registry.mutex.Unlock()
		return nil, true, errors.New("fixture requires its installed class, AI graph and positive health")
	}
	position := game.Vec3(member.playerPosition)
	position.X += 5
	reserved, err := member.zone.AssignSpawnPlanIDs([]zonenpc.SpawnPlan{{NounName: receipt.NounName, NPCProfile: profile, Position: position, IsFixture: true}})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("fixtureSpawnReserve: %w", err)
	}
	objectID := reserved[0].ObjectID
	markers := []game.CampaignDirectorMarker{{MarkerID: objectID, NounName: receipt.NounName, AuthoredNounName: receipt.NounName, Position: position, Scale: 1, IsVisible: true, IsCollisionEnabled: true, NPCProfile: profile, MarkerSetName: "scenario_owned_fixture.markerset"}}
	plans, nextObjectID, err := zonenpc.PlanFixtures(markers, objectID, objectID+1)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("fixtureSpawnPlan: %w", err)
	}
	if nextObjectID != objectID+1 {
		e.registry.mutex.Unlock()
		return nil, true, errors.New("fixture planner did not consume its exact reservation")
	}
	plans[0], err = zonenpc.PrepareScenarioControlledFixture(plans[0], receipt.RequestID)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("fixtureSpawnControl: %w", err)
	}
	packets, err := npcraknet.DormantSpawns(plans)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("fixtureSpawnMarshal: %w", err)
	}
	err = member.zone.NPCs().AddDormant(plans)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("fixtureSpawnAdd: %w", err)
	}
	err = member.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{Plans: plans, IsDormant: true}, member.binding.UserID, member.generation)
	if err != nil {
		rollbackErr := member.zone.NPCs().RollbackAdd(plans)
		e.registry.mutex.Unlock()
		return nil, true, fmt.Errorf("fixtureSpawnPublish: %w", errors.Join(err, rollbackErr))
	}
	e.registry.recordScenarioSpawnAdmissionLocked(receipt.RequestID, member, plans[0])
	e.registry.mutex.Unlock()
	err = e.registry.finishScenarioSpawnPublication(packet, receipt.RequestID)
	if err != nil {
		return packets, true, fmt.Errorf("fixtureSpawnReceipt: %w", err)
	}
	return packets, true, nil
}

// Retirement is cleanup, never a shortcut through the authored death callback.
// Accepted damage/death publication must settle before its geometry can retire.
func (e *gameplaySessionRegistry) validateFixtureRetirementLocked(peer gameplayPeerSession, objectID uint32) error {
	if peer.zone.Death().IsScenarioPending(objectID) {
		return errors.New("fixture death presentation is still active; wait for its authored final deadline before retirement")
	}
	for publication := range e.fixturePublications {
		if publication.source().npc == peer.zone.NPCs() && publication.source().source.Plan.ObjectID == objectID {
			return errors.New("fixture callback publication is still active; wait for normal settlement before retirement")
		}
		for _, death := range publication.deaths {
			if death.world == peer.zone && death.targetID == objectID && publication.isProducing {
				return errors.New("fixture accepted death publication is still active")
			}
		}
	}
	for _, member := range e.sessions {
		if member.zone != peer.zone {
			continue
		}
		for _, run := range member.campaignNPCModifiers {
			if run.record.SourceObjectID != objectID {
				continue
			}
			if run.fixtureBurn == nil {
				if run.isActive() {
					return errors.New("fixture recipient status is still active; wait for normal expiry or reset the recipient before retirement")
				}
				continue
			}
			publication := run.fixtureBurn.publication
			if run.fixtureBurn.isRetiring || publication != nil && (publication.isProducing || publication.isProduced && !publication.isCommitted) {
				return errors.New("fixture modifier publication is still settling before retirement")
			}
		}
	}
	return nil
}

func (e gameplayPendingRuntime) retireFixtureHazardsLocked(peer gameplayPeerSession, objectID uint32) error {
	var presentations [][]byte
	var cancels []raknet.CancelSchedule
	for key, member := range e.registry.sessions {
		if member.zone != peer.zone {
			continue
		}
		delete(member.acMalfunctions, objectID)
		delete(member.acMalfunctionWarnings, objectID)
		delete(member.acMalfunctionDischarges, objectID)
		delete(member.warpTotems, objectID)
		delete(member.warpTotemWarnings, objectID)
		delete(member.warpTotemDischarges, objectID)
		delete(member.fireVents, objectID)
		delete(member.fireVentWarnings, objectID)
		delete(member.fireVentEruptions, objectID)
		delete(member.cryosFungusExpirations, objectID)
		if member.cryosPoisonSourceID == objectID {
			member.cryosPoisonSourceID = 0
			member.cryosPoisonExpiresAt = time.Time{}
		}
		for _, run := range member.campaignNPCModifiers {
			if run.record.SourceObjectID != objectID {
				continue
			}
			deleted, deleteErr := effectraknet.ModifierDelete(run.record.TargetObjectID, run.instanceID)
			if deleteErr != nil {
				return fmt.Errorf("fixtureRetireModifierDelete: %w", deleteErr)
			}
			isCreated, isDeferred, releaseErr := retireFixtureRunLocked(run, e.modifierPool)
			if releaseErr != nil {
				return fmt.Errorf("fixtureRetireModifier: %w", releaseErr)
			}
			if isDeferred {
				return errors.New("fixture modifier retirement unexpectedly deferred")
			}
			if run.cancel != nil {
				cancels = append(cancels, run.cancel)
				run.cancel = nil
			}
			member.untrackCampaignNPCModifier(run)
			member.zone.Effect().Remove(run.instanceID)
			if isCreated {
				presentations = append(presentations, deleted)
			}
		}
		packets, err := e.updateGraviticContactLocked(&member, e.now())
		if err != nil {
			return fmt.Errorf("fixtureRetireGravitic: %w", err)
		}
		presentations = append(presentations, packets...)
		e.registry.sessions[key] = member
	}
	for _, cancel := range cancels {
		cancel()
	}
	e.registry.queueChannelPresentationLocked(peer.zone, presentations)
	return nil
}
