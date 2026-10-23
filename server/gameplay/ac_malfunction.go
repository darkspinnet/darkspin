package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// AcMalfunction waits six seconds, warns for two, then applies AcFreeze to
// hostile heroes within eight units. The modifier's authored life is 3.5s.
const acMalfunctionRest = 6 * time.Second
const acMalfunctionQueue = 2 * time.Second
const acMalfunctionFreeze = 3500 * time.Millisecond
const acMalfunctionRadius = float32(8)

type acMalfunctionCycle struct {
	warningAt      time.Time
	dischargeAt    time.Time
	completedAt    time.Time
	cycle          uint64
	completedCycle uint64
}

func (e *acMalfunctionCycle) rest(now time.Time) {
	e.warningAt = now.Add(acMalfunctionRest)
	e.dischargeAt = e.warningAt.Add(acMalfunctionQueue)
	e.cycle++
}

func (e gameplayPendingRuntime) pollACMalfunctions(packet raknet.Packet) ([][]byte, error) {
	sessionKey := packet.Address.String()
	e.registry.mutex.Lock()
	member, isFound := e.registry.sessions[sessionKey]
	if !isFound || member.transportGeneration != packet.TransportGeneration || member.zone == nil ||
		member.isRejoinPending || !member.stage.IsDungeon() || !member.dungeonSetup.IsCommitted() ||
		member.isZoneTerminal() || member.zone.NPCs() == nil {
		e.registry.mutex.Unlock()
		return nil, nil
	}
	now := e.now()
	packets, plans, err := e.updateACMalfunctionsLocked(&member, now)
	if err == nil {
		e.registry.sessions[sessionKey] = member
	}
	e.registry.mutex.Unlock()
	if err != nil {
		return nil, fmt.Errorf("acMalfunctionCycle: %w", err)
	}
	peerPackets := make([][]byte, 0)
	for index, plan := range plans {
		modifierPackets, modifierErr := e.damage.npc.applyCampaignNPCTimedModifier(
			packet, sessionKey, member.generation, plan, packet.SourceTime,
		)
		if modifierErr != nil {
			return nil, fmt.Errorf("acMalfunctionFreeze[%d]: %w", index, modifierErr)
		}
		peerPackets = append(peerPackets, modifierPackets...)
	}
	err = publishCampaignPeersAfterCommit(e.registry, packet, peerPackets)
	if err != nil {
		return nil, fmt.Errorf("acMalfunctionPublish: %w", err)
	}
	return append(packets, peerPackets...), nil
}

func (e gameplayPendingRuntime) updateACMalfunctionsLocked(
	member *gameplayPeerSession, now time.Time,
) ([][]byte, []zonenpc.AttackPlan, error) {
	if member.acMalfunctions == nil {
		member.acMalfunctions = make(map[uint32]*acMalfunctionCycle)
		member.acMalfunctionWarnings = make(map[uint32]uint64)
		member.acMalfunctionDischarges = make(map[uint32]uint64)
	}
	packets := make([][]byte, 0)
	plans := make([]zonenpc.AttackPlan, 0)
	for _, source := range member.zone.NPCs().Snapshots() {
		if !zonenpc.IsCryosMalfunction(source.Plan) || !source.IsPublished || source.IsDefeated {
			continue
		}
		objectID := source.Plan.ObjectID
		state := member.acMalfunctions[objectID]
		if state == nil {
			for _, ally := range e.registry.sessions {
				if ally.zone == member.zone && ally.acMalfunctions[objectID] != nil {
					state = ally.acMalfunctions[objectID]
					break
				}
			}
			if state == nil {
				state = &acMalfunctionCycle{}
				state.rest(now)
			}
			member.acMalfunctions[objectID] = state
		}
		if !now.Before(state.warningAt) && member.acMalfunctionWarnings[objectID] != state.cycle {
			warning, err := acMalfunctionEffect(objectID, "cryos_ac_jet_snow_side_venting_gentle_effect.ServerEventDef")
			if err != nil {
				return nil, nil, fmt.Errorf("acMalfunctionWarning: %w", err)
			}
			packets = append(packets, warning)
			member.acMalfunctionWarnings[objectID] = state.cycle
		}
		if !now.Before(state.dischargeAt) {
			state.completedCycle = state.cycle
			state.completedAt = now
			state.rest(now)
		}
		if state.completedCycle == 0 || member.acMalfunctionDischarges[objectID] == state.completedCycle ||
			!now.Before(state.completedAt.Add(time.Second)) {
			continue
		}
		wave, err := acMalfunctionEffect(objectID, "cryos_ac_jet_snow_side_venting_effect.ServerEventDef")
		if err != nil {
			return nil, nil, fmt.Errorf("acMalfunctionWave: %w", err)
		}
		packets = append(packets, wave)
		member.acMalfunctionDischarges[objectID] = state.completedCycle
		plan, isTargetFound, err := e.acMalfunctionTargetLocked(member, source, now)
		if err != nil {
			return nil, nil, fmt.Errorf("acMalfunctionTarget: %w", err)
		}
		if isTargetFound {
			plans = append(plans, plan)
		}
	}
	return packets, plans, nil
}

func (e gameplayPendingRuntime) acMalfunctionTargetLocked(
	member *gameplayPeerSession, source zonenpc.Snapshot, now time.Time,
) (zonenpc.AttackPlan, bool, error) {
	if member.deployedObjectID == 0 || member.deployedHitPoint() <= 0 ||
		member.isHeroSelectionPending || e.damage.npc.isTargetStatusImmuneLocked(member, member.deployedObjectID) {
		return zonenpc.AttackPlan{}, false, nil
	}
	position := game.Vec3(member.playerPosition)
	if member.playerMotion != nil {
		sampled, err := member.playerMotion.SamplePosition(now)
		if err != nil {
			return zonenpc.AttackPlan{}, false, fmt.Errorf("acMalfunctionPosition: %w", err)
		}
		position = game.Vec3(sampled)
	}
	if position.Sub(source.Plan.Position).Length() > acMalfunctionRadius {
		return zonenpc.AttackPlan{}, false, nil
	}
	return zonenpc.AttackPlan{
		SourceObjectID: source.Plan.ObjectID, TargetObjectID: member.deployedObjectID,
		Profile: zonenpc.ActionProfile{ModifierName: "AcFreeze", ModifierDuration: acMalfunctionFreeze},
	}, true, nil
}

func acMalfunctionEffect(objectID uint32, name string) ([]byte, error) {
	asset := util.HashID(name)
	packet, err := raknet.MarshalApplication(raknet.ServerEventContractMessage{Asset: &asset, ObjectID: &objectID})
	if err != nil {
		return nil, fmt.Errorf("acMalfunctionEvent: %w", err)
	}
	return packet, nil
}
