package gameplay

import (
	"errors"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

func campaignCloudLobAim(
	member gameplayPeerSession, creature game.GameplayCreature,
	definition sim.AbilityDefinition, command raknet.ActionCommandData,
) (zoneability.CloudLobRequest, error) {
	if command.Ability == nil {
		return zoneability.CloudLobRequest{}, errors.New("cloud aim unavailable")
	}
	position := command.Ability.TargetPosition
	if command.Ability.TargetID == 0 && isReportedZonePosition(command.Ability.CursorPosition) {
		position = command.Ability.CursorPosition
	}
	if !isReportedZonePosition(position) {
		position = command.Ability.CursorPosition
	}
	if !isReportedZonePosition(position) || !isFiniteZonePosition(position) {
		return zoneability.CloudLobRequest{}, errors.New("cloud aim invalid")
	}
	targetFootprint := float32(0)
	if command.Ability.TargetID != 0 && member.zone != nil && member.zone.NPCs() != nil {
		target, isTargetFound := member.zone.NPCs().NPC(command.Ability.TargetID)
		if isTargetFound && !target.IsDefeated && target.HitPoint > 0 {
			targetFootprint = target.Plan.ActorFootprintRadius()
		}
	}
	return zoneability.CloudLobRequest{
		SourceObjectID: command.Common.ObjectID, TargetObjectID: command.Ability.TargetID,
		SourcePosition: game.Vec3(member.playerPosition), TargetPosition: game.Vec3(position),
		Creature: creature, Definition: definition,
		SourceFootprint: member.deployedCampaignActorFootprintRadius(),
		TargetFootprint: targetFootprint,
	}, nil
}
