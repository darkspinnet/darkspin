package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/zone"
	zoneaction "github.com/darkspinnet/darkspin/server/zone/action"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

type npcStrafeProjection struct {
	member *gameplayPeerSession
	source zonenpc.Snapshot
}

func (e npcStrafeProjection) ClosestPosition(role sim.Role, position sim.Position) (sim.Position, error) {
	if role != "actor" {
		return sim.Position{}, errors.New("strafe projection actor invalid")
	}
	destination, isFound, err := zoneaction.NPCProjectPosition(e.member.zone.Navigation(),
		game.Vec3(position), e.source.NavigationRadius(), e.source.Navigation)
	if err != nil {
		return sim.Position{}, fmt.Errorf("strafeProject: %w", err)
	}
	if !isFound {
		return sim.Position{}, errors.New("strafe destination unavailable")
	}
	return sim.Position(destination), nil
}

func (e *campaignNPCBehaviorRun) startLuaStrafeLocked(member *gameplayPeerSession,
	source zonenpc.Snapshot, target zone.NPCTarget, isTargetFound bool, now time.Time,
) ([][]byte, error) {
	input := e.runtime.program.Strafe
	if e.run.Choice.Behavior == "nBehavior_SmallStrafe" {
		input = e.runtime.program.SmallStrafe
	}
	targetRole := sim.Role("")
	if isTargetFound {
		targetRole = "target"
	}
	runtime, plan, err := sim.NewLuaStrafeRuntime(sim.LuaStrafeInput{
		Behavior: e.run.Choice.Behavior,
		Root:     input.Root, Modules: input.Modules, Random: member.zone.NPCRandom(),
		Role: "actor", TargetRole: targetRole, Position: sim.Position(source.Plan.Position),
		TargetPosition: sim.Position(target.Position), FootprintRadius: source.Plan.ActorFootprintRadius(),
		Projection: npcStrafeProjection{member: member, source: source},
	})
	if err != nil {
		return nil, fmt.Errorf("strafeLua: %w", err)
	}
	e.strafe, e.strafePlan = runtime, plan
	if isTargetFound {
		e.strafeTargetID = target.ObjectID
	}
	packets, updated, err := e.setMovementBuffLocked(member, source, plan.MovementSpeedBuff, now)
	if err != nil {
		return nil, fmt.Errorf("strafeBuff: %w", err)
	}
	if updated.IsDefeated {
		return nil, errors.New("strafe actor defeated")
	}
	return packets, nil
}
