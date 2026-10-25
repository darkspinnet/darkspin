package gameplay

import (
	"errors"
	"fmt"
	"strings"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	npcraknet "github.com/darkspinnet/darkspin/server/zone/npc/raknet103"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
	zoneprojection "github.com/darkspinnet/darkspin/server/zone/projection"
)

func (e gameplayPendingRuntime) spawnDeveloperNPC(packet raknet.Packet, queuedSession gameplayPeerSession, command game.PlayerEventCommand) ([][]byte, bool, error) {
	packets, isHandled, err := e.spawnDeveloperNPCForRequest(packet, queuedSession, command, "")
	if err != nil {
		return packets, isHandled, fmt.Errorf("developerSpawn: %w", err)
	}
	return packets, isHandled, nil
}
func (e gameplayPendingRuntime) spawnDeveloperNPCForRequest(
	packet raknet.Packet, queuedSession gameplayPeerSession, command game.PlayerEventCommand, requestID string,
) ([][]byte, bool, error) {
	sessionKey := packet.Address.String()
	e.registry.mutex.Lock()
	currentSession, isFound := e.registry.sessions[sessionKey]
	isCurrent := isFound && currentSession.generation == queuedSession.generation &&
		currentSession.stage.IsDungeon() && currentSession.zone != nil &&
		currentSession.zone.NPCs() != nil && currentSession.deployedObjectID != 0 &&
		!currentSession.isZoneTerminal()
	if !isCurrent {
		e.registry.mutex.Unlock()
		return nil, false, errors.New("npcSpawnSession: unavailable")
	}
	requestErr := e.registry.validateScenarioSpawnLocked(requestID, currentSession)
	if requestErr != nil {
		e.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("npcSpawnRequest: %w", requestErr)
	}
	director := currentSession.zone.DirectorDefinition()
	profile, isProfileFound := developerNPCProfile(director, command.NounName)
	if !isProfileFound {
		e.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("npcSpawnProfile: unavailable %q", command.NounName)
	}
	profile = zoneboss.NormalizeFinalBossProfile(command.NounName, profile)
	isCaptain := strings.Contains(strings.ToLower(command.NounName), "_captain")
	bossIdentity := zonenpc.BossIdentity{}
	if isCaptain {
		bossIdentity = zonepopulation.CaptainIdentity(director, command.NounName)
		profile = zonenpc.ApplyCaptainProfile(profile, bossIdentity)
	}
	position := game.Vec3(currentSession.playerPosition)
	position.X += 5
	plan := zonenpc.SpawnPlan{
		NounName: command.NounName, Position: position,
		IsCaptain:            isCaptain,
		IsArena:              currentSession.binding.Mode == game.ModeArena,
		IsEncounterAuxiliary: true, IsLootSuppressed: true, IsExperienceSuppressed: true, NPCProfile: profile,
	}
	if isCaptain {
		plan.BossIdentity = bossIdentity
	}
	if strings.EqualFold(command.NounName, zonenpc.MutationAgentNounName) {
		// The agent owns its class rank and smoke-model presentation. A
		// minion body changes its identity and bypasses mutation admission.
		plan.NounName = zonenpc.MutationAgentNounName
		plan.ActionProfile = zonenpc.MutationAgentActionProfile()
		plan.IsActionKnown = true
	}
	plans, err := currentSession.zone.AssignSpawnPlanIDs([]zonenpc.SpawnPlan{plan})
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("npcSpawnAllocate: %w", err)
	}
	plan = plans[0]
	plan, err = e.registry.prepareScenarioSpawnPlanLocked(requestID, plan)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("npcSpawnControl: %w", err)
	}
	plans[0] = plan
	targetObjectID := currentSession.deployedObjectID
	packets, err := npcraknet.TargetedSpawn(plan, targetObjectID)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("npcSpawnMarshal: %w", err)
	}
	err = currentSession.zone.NPCs().Add(plans, targetObjectID)
	if err != nil {
		e.registry.mutex.Unlock()
		return nil, false, fmt.Errorf("npcSpawnAdd: %w", err)
	}
	err = currentSession.zone.PublishNPCSpawn(zoneprojection.NPCSpawn{
		Plans: plans, TargetObjectID: targetObjectID,
	}, currentSession.binding.UserID, currentSession.generation)
	if err != nil {
		rollbackErr := currentSession.zone.NPCs().RollbackAdd(plans)
		e.registry.mutex.Unlock()
		return nil, false, fmt.Errorf(
			"npcSpawnPublish: %w", errors.Join(err, rollbackErr),
		)
	}
	currentSession.zone.MarkNPCSpawnCommandUsed()
	e.registry.recordScenarioSpawnAdmissionLocked(requestID, currentSession, plan)
	generation := currentSession.generation
	e.registry.sessions[sessionKey] = currentSession
	e.registry.mutex.Unlock()
	actionPackets, actionErr := e.damage.npc.scheduleFirstActions(packet, sessionKey, generation, plans, packet.SourceTime)
	if actionErr != nil {
		e.registry.recordScenarioSpawnFailure(requestID, actionErr)
		e.logger.Printf(
			"RakNet developer NPC spawn retained object=%d noun=%q for %s while first action failed: %v",
			plan.ObjectID, plan.NounName, packet.Address, actionErr,
		)
	} else {
		packets = append(packets, actionPackets...)
	}
	e.logger.Printf(
		"RakNet developer NPC spawn accepted object=%d noun=%q presentation_noun=%q for %s",
		plan.ObjectID, command.NounName, plan.NounName, packet.Address,
	)
	publicationErr := e.registry.finishScenarioSpawnPublication(packet, requestID)
	if publicationErr != nil {
		return packets, true, fmt.Errorf("npcSpawnReceipt: %w", publicationErr)
	}
	return packets, true, nil
}
