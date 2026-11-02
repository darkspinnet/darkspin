package gameplay

import (
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
	zoneability "github.com/darkspinnet/darkspin/server/zone/ability"
)

// The registry is locked and the current source is published before entering.
// Build-103 CreateFlak destroys hostile projectiles and requests RocketSlow
// before TakeDamage; a defended hit must not suppress that modifier request.
func (e campaignAreaHitStep) prepareArenaFlakLocked(member *gameplayPeerSession, plan zoneability.AreaPlan) ([][]byte, error) {
	presentation, presentationErr := e.cursorGroundEffect(plan)
	if presentationErr != nil {
		return nil, fmt.Errorf("flakPresentation: %w", presentationErr)
	}
	packets := [][]byte{presentation}
	if !e.isRetained {
		muzzle, muzzleErr := raknet.MarshalApplication(raknet.ObjectPresentationMessage{
			Asset: util.HashID(plan.Definition.MuzzleEffectName), ObjectID: e.command.Common.ObjectID,
		})
		if muzzleErr != nil {
			return nil, fmt.Errorf("flakMuzzle: %w", muzzleErr)
		}
		packets = append([][]byte{muzzle}, packets...)
	}
	e.runtime.registry.queueFieldMedicRealmPacketsLocked(nil, member.binding.GameID, packets)
	*member = e.runtime.registry.sessions[e.sessionKey]
	packets, destroyedCount, err := destroyHostileProjectilesLocked(e.runtime, member, e.sessionKey,
		plan.Center, plan.Definition.Radius, e.runtime.now())
	if err != nil {
		return nil, fmt.Errorf("flakProjectiles: %w", err)
	}
	flakPackets, err := e.runtime.applyMissileFlakLocked(e.packet, member, e.sessionKey,
		e.generation, e.abilityRank, destroyedCount, e.hitTimestamp)
	if err != nil {
		return nil, fmt.Errorf("flakBuff: %w", err)
	}
	packets = append(packets, flakPackets...)
	e.runtime.registry.sessions[e.sessionKey] = *member
	objectIDs := make([]uint32, 0, len(plan.Target))
	for _, target := range plan.Target {
		objectIDs = append(objectIDs, target.Plan.ObjectID)
	}
	statusPackets, err := e.runtime.applyArenaTargetStatusLocked(e.sessionKey, e.generation,
		e.command.Common.ObjectID, e.hitTimestamp, plan, objectIDs)
	if err != nil {
		return nil, fmt.Errorf("flakSlow: %w", err)
	}
	*member = e.runtime.registry.sessions[e.sessionKey]
	return append(packets, statusPackets...), nil
}
