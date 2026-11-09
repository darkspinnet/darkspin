package gameplay

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonegeometry "github.com/darkspinnet/darkspin/server/zone/geometry"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

const psistormAuraOwnerRole sim.Role = "owner"

// The registry and run locks cover this callback-scoped authority. Lua retains
// target identities and request results; the run owns every accepted handle.
type heroAuraWorld struct {
	run       *heroAuraAreaRun
	schedule  heroAuraAreaSchedule
	member    *gameplayPeerSession
	deadline  time.Duration
	expiresAt time.Time
}

func auraNPCObjectID(role sim.Role) (uint32, error) {
	objectID, err := strconv.ParseUint(string(role), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("auraTarget: %w", err)
	}
	if objectID == 0 {
		return 0, errors.New("zero aura target")
	}
	return uint32(objectID), nil
}

func (e heroAuraWorld) Object(role sim.Role) (sim.LuaObjectInput, error) {
	if e.member == nil || e.member.zone != e.run.originalZone {
		return sim.LuaObjectInput{}, errors.New("aura world unavailable")
	}
	if role == psistormAuraOwnerRole {
		return sim.LuaObjectInput{
			Role: role, Position: sim.Position(e.member.playerPosition),
			Team: int(abilityActorTeam(*e.member)), IsPlayerControlled: true,
			IsAlive: e.member.deployedObjectID == e.schedule.sourceObjectID && e.member.deployedHitPoint() > 0,
		}, nil
	}
	objectID, err := auraNPCObjectID(role)
	if err != nil {
		return sim.LuaObjectInput{}, fmt.Errorf("auraObjectID: %w", err)
	}
	actor, isFound := e.run.npc.NPC(objectID)
	if !isFound {
		return sim.LuaObjectInput{}, errors.New("aura object unavailable")
	}
	return sim.LuaObjectInput{
		Role: role, Position: sim.Position(actor.Plan.Position),
		Team: int(actor.Faction), IsAlive: !actor.IsDefeated && actor.HitPoint > 0,
	}, nil
}

func (e heroAuraWorld) ObjectsInRadius(req sim.LuaRadiusQuery) ([]sim.LuaObjectInput, error) {
	if e.run.npc == nil || !req.IsOrganicDamageableOnly ||
		!zonegeometry.IsFinite(game.Vec3(req.Center)) || !zonegeometry.IsFiniteScalar(req.Radius) || req.Radius < 0 {
		return nil, errors.New("invalid aura query")
	}
	objects := make([]sim.LuaObjectInput, 0)
	for _, actor := range e.run.npc.LiveSnapshots() {
		// Keep the production aura's center-distance policy. Native trigger
		// collider overlap has not been recovered for this family.
		if (actor.Faction != zonenpc.FactionNonPlayerAligned && actor.Faction != zonenpc.FactionPlayerAligned) ||
			zonegeometry.Distance(game.Vec3(req.Center), actor.Plan.Position) > req.Radius {
			continue
		}
		objects = append(objects, sim.LuaObjectInput{
			Role:     sim.Role(strconv.FormatUint(uint64(actor.Plan.ObjectID), 10)),
			Position: sim.Position(actor.Plan.Position), Team: int(actor.Faction),
			IsAlive: !actor.IsDefeated && actor.HitPoint > 0,
		})
	}
	return objects, nil
}

func (e heroAuraWorld) RequestAuraModifier(req sim.LuaAuraModifierRequest) (uint32, error) {
	if e.member == nil || req.OwnerRole != psistormAuraOwnerRole ||
		req.ModifierID != e.schedule.definition.RootModifierID || req.Rank != 1 {
		return 0, errors.New("aura request rejected")
	}
	objectID, err := auraNPCObjectID(req.TargetRole)
	if err != nil {
		return 0, fmt.Errorf("auraRequestID: %w", err)
	}
	if _, isTracked := e.run.targets[objectID]; isTracked {
		return 0, errors.New("duplicate aura request")
	}
	instanceID, err := e.run.modifierPool.Allocate()
	if err != nil {
		return 0, fmt.Errorf("auraAllocate: %w", err)
	}
	packets, err := e.schedule.prepareNPCMembership(objectID, instanceID, e.deadline)
	if err != nil {
		e.run.releaseAuraInstance(instanceID)
		return 0, fmt.Errorf("auraEncode: %w", err)
	}
	reaction, err := e.schedule.runtime.registry.prepareLaserSilenceReactionLocked(e.member, objectID)
	if err != nil {
		e.run.releaseAuraInstance(instanceID)
		return 0, fmt.Errorf("auraReaction: %w", err)
	}
	isApplied, err := e.run.npc.ApplyAuraSilence(objectID, instanceID, e.schedule.runtime.now(), e.expiresAt, luaAuraAdmission{
		modifier: e.run.silenceAdmission, isChainGame: e.schedule.binding.Mode == game.ModeChain,
	})
	if err != nil {
		e.run.releaseAuraInstance(instanceID)
		return 0, fmt.Errorf("auraApply: %w", err)
	}
	if !isApplied {
		e.run.releaseAuraInstance(instanceID)
		return 0, nil
	}
	// Retain the accepted handle before returning to any fallible Lua call.
	e.run.targets[objectID] = heroAuraAreaTarget{instanceID: instanceID, expiresAt: e.expiresAt}
	e.schedule.queueNPCMembershipLocked(e.member, packets, true)
	if len(reaction.packets) != 0 {
		e.schedule.queueNPCMembershipLocked(e.member, reaction.packets, false)
	}
	reaction.commit()
	return instanceID, nil
}

func (e heroAuraWorld) DeleteAuraModifier(req sim.LuaAuraModifierDelete) error {
	if req.InstanceID == 0 {
		return nil
	}
	objectID, err := auraNPCObjectID(req.TargetRole)
	if err != nil {
		return fmt.Errorf("auraDeleteID: %w", err)
	}
	if instanceID, isRetired := e.run.retiredTargets[objectID]; isRetired && instanceID == req.InstanceID {
		delete(e.run.retiredTargets, objectID)
		return nil
	}
	target, isTracked := e.run.targets[objectID]
	if !isTracked || target.instanceID != req.InstanceID {
		return errors.New("aura delete identity mismatch")
	}
	packets, err := marshalAuraAreaDeletes([]heroAuraAreaTargetDelete{{objectID: objectID, instanceID: target.instanceID}})
	if err != nil {
		return fmt.Errorf("auraDeleteEncode: %w", err)
	}
	if e.member != nil {
		e.schedule.queueNPCMembershipLocked(e.member, packets, false)
	} else {
		e.run.queuePresentationLocked(packets, true)
	}
	e.run.clearAuraTargetStatus(objectID, target)
	delete(e.run.targets, objectID)
	e.run.releaseAuraInstance(target.instanceID)
	return nil
}

func (e heroAuraAreaSchedule) reconcileLuaAuraLocked(member *gameplayPeerSession, deadline time.Duration, expiresAt time.Time) error {
	if e.run.aura == nil {
		admission, err := sim.NewLuaPsistormModifier(e.runtime.program.Psistorm)
		if err != nil {
			return fmt.Errorf("auraAdmission: %w", err)
		}
		runtime, err := sim.NewLuaAuraRuntime(sim.LuaAuraInput{
			Root: e.runtime.program.AuraUtils, OwnerRole: psistormAuraOwnerRole,
			ModifierID: e.definition.RootModifierID, Rank: 1,
		})
		if err != nil {
			admission.Stop()
			return fmt.Errorf("auraCreate: %w", err)
		}
		e.run.aura = runtime
		e.run.silenceAdmission = admission
		e.run.isLuaAura = true
	}
	err := e.run.aura.Scan(heroAuraWorld{
		run: e.run, schedule: e, member: member, deadline: deadline, expiresAt: expiresAt,
	}, sim.LuaRadiusQuery{Center: sim.Position(e.center), Radius: e.definition.Radius, IsOrganicDamageableOnly: true})
	if err != nil {
		return fmt.Errorf("auraScan: %w", err)
	}
	return nil
}

func (e *heroAuraAreaRun) clearAuraTargetStatus(objectID uint32, target heroAuraAreaTarget) {
	if e.isSleepingCloud && e.arenaGameID == 0 {
		e.npc.ClearAuraSleep(objectID, target.instanceID)
		target.sleep.Stop()
		return
	}
	if e.isLuaAura {
		e.npc.ClearAuraSilence(objectID, target.instanceID)
		return
	}
	if e.statusKind == sim.AbilityStatusKindSlow && e.arenaGameID == 0 {
		e.npc.ClearSlowModifier(objectID, target.instanceID)
		return
	}
	e.clearStatus(objectID, target.instanceID, target.expiresAt)
}
