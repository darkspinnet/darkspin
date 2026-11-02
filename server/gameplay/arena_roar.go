package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

// The caster's RoarBuff owns damage reduction; the opponent's RoarTaunt is
// presentation only in Arena, where a player has no NPC force-target AI.
func (e *arenaHeroChargeRun) impactRoarLocked(source *gameplayPeerSession,
	targets *arenaAbilityTargets, timestamp uint64,
) ([][]byte, error) {
	if e.definition.SecondaryModifierID == 0 || e.definition.TickDuration <= 0 ||
		e.definition.StatusDuration <= 0 || e.runtime.registry.timer == nil {
		return nil, errors.New("arena roar modifier unavailable")
	}
	objectIDs := make([]uint32, 0)
	for _, target := range targets.LiveSnapshots() {
		if target.Plan.Position.Sub(game.Vec3(e.destination)).Length() <= e.definition.Radius {
			objectIDs = append(objectIDs, target.Plan.ObjectID)
		}
	}
	plan := zoneability.AreaPlan{SourceObjectID: e.objectID, AbilityID: e.abilityID,
		Definition: e.definition}
	statusPackets, err := e.runtime.applyArenaTargetStatusLocked(e.sessionKey, e.generation,
		e.objectID, timestamp, plan, objectIDs)
	if err != nil {
		return nil, fmt.Errorf("roarTaunt: %w", err)
	}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("roarBuffAllocate: %w", err)
	}
	status := &arenaHitStatus{runtime: e.runtime, targetKey: e.sessionKey,
		gameID: e.gameID, objectID: e.objectID, index: source.deployedCreatureIndex,
		generation: source.generation, transportGeneration: source.transportGeneration,
		squad: source.squad, noun: source.binding.Creatures[source.deployedCreatureIndex].Noun,
		sourceObjectID: e.objectID, modifierID: e.definition.SecondaryModifierID,
		instanceID: instanceID, kind: sim.AbilityStatusKindTaunt,
		duration: e.definition.StatusDuration, expiresAt: e.runtime.now().Add(e.definition.StatusDuration), revision: 1}
	createPacket, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.objectID, TargetObjectID: e.objectID,
		ModifierID: status.modifierID, InstanceID: instanceID,
		Duration: status.duration, Timestamp: timestamp,
	})
	if err != nil {
		return nil, status.discard("roarBuffCreate", err)
	}
	expiry := arenaHitStatusExpiry{status: status, revision: 1}
	status.cancel, err = e.runtime.registry.timer.Schedule(status.duration, expiry.expire)
	if err == nil && status.cancel == nil {
		err = errors.New("roar buff cancellation unavailable")
	}
	if err != nil {
		return nil, status.discard("roarBuffSchedule", err)
	}
	if source.arenaHitStatuses == nil {
		source.arenaHitStatuses = make(map[arenaHitStatusKey]*arenaHitStatus)
	}
	if previous := source.arenaHitStatuses[status.key()]; previous != nil {
		previous.retire(source)
	}
	source.arenaHitStatuses[status.key()] = status
	source.roarReductionObjectID = e.objectID
	source.roarReductionExpiresAt = status.expiresAt
	status.queue(source, [][]byte{createPacket})
	e.runtime.registry.sessions[e.sessionKey] = *source
	packets := append([][]byte(nil), statusPackets...)
	if e.definition.ActivationEffectName != "" {
		effectPacket, effectErr := raknet.MarshalApplication(raknet.DropPresentationMessage{
			Asset: util.HashID(e.definition.ActivationEffectName), Position: e.destination,
		})
		if effectErr != nil {
			return packets, fmt.Errorf("roarEffect: %w", effectErr)
		}
		packets = append(packets, effectPacket)
	}
	return packets, nil
}
