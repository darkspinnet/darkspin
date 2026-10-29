package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

// The finite cage survives caster release and target changes. Chunk426 still
// fences its recipient/initiator life, connected-player count, and Horde state.
type nomadFrogCage struct {
	connectedPlayerCount int
	effects              *channelEffects
}

func (e gameplayPeerSession) isNomadFrogCageActive(at time.Time) bool {
	run := e.operativeCage
	if run == nil || run.frogCage == nil || !run.isActive() || e.zone == nil || !at.Before(run.record.ExpiresAt) {
		return false
	}
	source, isFound := e.zone.NPCs().NPC(run.record.SourceObjectID)
	return isFound && !source.IsDefeated && source.HitPoint > 0
}

// The caller holds registry.mutex and the modifier is already in inventory.
func (e campaignNPCActionRuntime) bindNomadFrogCage(source gameplayPeerSession, run *campaignNPCModifierRun) ([][]byte, bool, error) {
	for sessionKey, target := range e.registry.sessions {
		if target.zone != source.zone || target.deployedObjectID != run.record.TargetObjectID || target.operativeCage != nil || target.isRejoinPending || target.deployedHitPoint() <= 0 || !e.registry.hasOperativeRescuerLocked(target) {
			continue
		}
		packets, err := stopEnemyControlledHeroMovement(&target, e.now())
		if err != nil {
			return nil, false, fmt.Errorf("frogCageStop: %w", err)
		}
		target.basicSequenceSession().ReleaseHeld()
		target.campaignPlayerPursuitSession().Cancel()
		target.followTargetUserID = 0
		effects, err := e.registry.allocateChannelEffectsLocked(source.zone, e.effectPool, []uint32{run.record.TargetObjectID})
		if err != nil {
			return nil, false, fmt.Errorf("frogCageEffect: %w", err)
		}
		slots := effects.Slots()
		attachment, err := raknet.MarshalApplication(raknet.AttachedEffectMessage{ObjectID: run.record.TargetObjectID, Slot: slots[0].slot + 1, IsForceAttached: true, Asset: util.HashID("nomad_lieu_lf_4_gooTrap.ServerEventDef")})
		if err != nil {
			effects.Retire()
			return nil, false, fmt.Errorf("frogCageMarshal: %w", err)
		}
		run.frogCage = &nomadFrogCage{connectedPlayerCount: e.registry.operativePlayerCountLocked(source.zone), effects: effects}
		target.operativeCage = run
		e.registry.sessions[sessionKey] = target
		err = e.registry.publishChannelEffectsLocked(effects, [][]byte{attachment})
		if err != nil {
			target.operativeCage = nil
			e.registry.sessions[sessionKey] = target
			effects.Retire()
			return nil, false, fmt.Errorf("frogCagePublish: %w", err)
		}
		return packets, true, nil
	}
	return nil, false, nil
}

func (e gameplayPeerSession) isCageContinuationValid(registry *gameplaySessionRegistry, at time.Time) bool {
	if e.operativeCage == nil || !e.isOperativeCaged(at) {
		return false
	}
	if e.operativeCage.frogCage != nil {
		return e.operativeCage.frogCage.connectedPlayerCount == registry.operativePlayerCountLocked(e.zone)
	}
	return e.operativeCage.operativeCage != nil && registry.hasOperativeRescuerLocked(e) && e.operativeCage.operativeCage.connectedPlayerCount == registry.operativePlayerCountLocked(e.zone)
}
