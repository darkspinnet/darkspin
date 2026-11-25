package gameplay

import (
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/sim"
	zoneteleport "github.com/darkspinnet/darkspin/server/zone/teleport"
)

// tutorialTeleporterQuery is scoped to one authoritative callback. It reads
// the current peer actor rather than retaining a copied world in Lua state.
type tutorialTeleporterQuery struct {
	member *gameplayPeerSession
}

func (e tutorialTeleporterQuery) Object(role sim.Role) (sim.LuaObjectInput, error) {
	if e.member == nil || e.member.zone == nil {
		return sim.LuaObjectInput{}, errors.New("tutorial world unavailable")
	}
	switch role {
	case zoneteleport.SecurityRole:
		return sim.LuaObjectInput{
			Role: role, IsAlive: true, Position: sim.Position(tutorialBossTeleport.Source),
		}, nil
	case zoneteleport.EntrantRole:
		if e.member.deployedObjectID == 0 {
			return sim.LuaObjectInput{}, errors.New("tutorial entrant unavailable")
		}
		return sim.LuaObjectInput{
			Role: role, Position: sim.Position(e.member.playerPosition),
			IsAlive:            e.member.deployedHitPoint() > 0,
			IsPlayerControlled: e.member.deployedObjectID != 0 && !e.member.isZoneTerminal(),
		}, nil
	default:
		return sim.LuaObjectInput{}, fmt.Errorf("tutorialRole: %s", role)
	}
}

func (e tutorialTeleporterQuery) ObjectsInRadius(sim.LuaRadiusQuery) ([]sim.LuaObjectInput, error) {
	// This pinned boss family registers only contact callbacks. Threat scans
	// belong to the separate security teleporter template and Go threat gate.
	return nil, errors.New("tutorial boss radius query unsupported")
}

func (e *gameplayPeerSession) prepareTutorialTeleporterModifier(program zoneteleport.Simulation) error {
	if e.tutorialTeleporterModifier != nil {
		return nil
	}
	req := program.SecurityInput
	if req.Root.ChunkID != zoneteleport.SecurityChunkID ||
		req.Root.SHA256 != zoneteleport.SecuritySHA256 ||
		req.Role != zoneteleport.SecurityRole ||
		req.Position != sim.Position(tutorialBossTeleport.Source) ||
		req.Destination != sim.Position(tutorialBossTeleport.Destination) {
		return errors.New("tutorial boss modifier identity invalid")
	}
	req.Query = tutorialTeleporterQuery{member: e}
	runtime, activation, err := sim.NewLuaModifierRuntime(req)
	if err != nil {
		return fmt.Errorf("tutorialModifierCreate: %w", err)
	}
	// Presentation is already owned by securityraknet.State. Only this exact
	// authored initialization is accepted before retaining its private state.
	if len(activation.Steps) != 2 {
		runtime.Stop()
		return errors.New("tutorial modifier activation invalid")
	}
	effectStep, isEffectStep := activation.Steps[0].(sim.EmitStep)
	triggerStep, isTriggerStep := activation.Steps[1].(sim.EmitStep)
	if !isEffectStep || !isTriggerStep ||
		effectStep.Intent != (sim.EffectIntent{Role: zoneteleport.SecurityRole, EffectName: "zelem_boss_teleporter.ServerEventDef"}) ||
		triggerStep.Intent != (sim.TriggerVolumeIntent{Role: zoneteleport.SecurityRole, Center: req.Position, Radius: 2}) {
		runtime.Stop()
		return errors.New("tutorial modifier initialization rejected")
	}
	e.tutorialTeleporterModifier = runtime
	return nil
}

func (e *gameplayPeerSession) admitTutorialTeleporter() (isAccepted bool, err error) {
	if e.tutorialTeleporterModifier == nil {
		return false, errors.New("tutorial modifier unavailable")
	}
	defer func() {
		if err != nil {
			e.tutorialTeleporterModifier.Stop()
		}
	}()
	program, err := e.tutorialTeleporterModifier.Trigger(sim.LuaModifierTrigger{
		Kind: sim.LuaTriggerEnter, Role: zoneteleport.EntrantRole,
		IsModifierActive: e.isTutorialTeleporterUsed,
		Query:            tutorialTeleporterQuery{member: e},
	})
	if err != nil {
		return false, fmt.Errorf("tutorialTrigger: %w", err)
	}
	if len(program.Steps) == 0 {
		return false, nil
	}
	if len(program.Steps) != 1 {
		return false, errors.New("tutorial trigger step count invalid")
	}
	emit, isEmit := program.Steps[0].(sim.EmitStep)
	request, isRequest := emit.Intent.(sim.ModifierRequestIntent)
	if !isEmit || !isRequest || request != (sim.ModifierRequestIntent{
		TargetRole: zoneteleport.EntrantRole, InitiatorRole: zoneteleport.SecurityRole,
		ModifierGUID: zoneteleport.ModifierGUID, Rank: 1,
		Destination: sim.Position(tutorialBossTeleport.Destination),
	}) {
		return false, errors.New("tutorial trigger request rejected")
	}
	return true, nil
}
