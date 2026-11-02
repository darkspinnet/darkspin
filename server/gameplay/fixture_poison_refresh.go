package gameplay

import (
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

// Native1039E16C0 case7 retains the first instance/source and running DOT
// coroutine. It refreshes only the start clock (A3), not the tick deadline.
// The caller holds registry.mutex; tick publications use this same lock.
func (e campaignFixtureBlastStep) refreshEggPoisonLocked(member gameplayPeerSession, targetID uint32, now time.Time) ([][]byte, bool, error) {
	for _, candidate := range e.runtime.registry.sessions {
		if candidate.zone != member.zone {
			continue
		}
		for _, run := range candidate.campaignNPCModifiers {
			if run == nil || run.fixtureBurn == nil || run.record.TargetObjectID != targetID || run.record.GUID != util.HashID("EggPoison") || !run.isActive() || run.fixtureBurn.isRetiring || !now.Before(run.fixtureBurn.expiresAt) {
				continue
			}
			packet, err := raknet.MarshalApplication(raknet.ModifierUpdatedMessage{
				TargetID: targetID, InstanceID: run.instanceID, StartMilliseconds: int64(e.timestamp),
				StackCount: run.record.StackCount,
			})
			if err != nil {
				return nil, true, fmt.Errorf("eggRefreshMarshal: %w", err)
			}
			record := run.record
			record.ExpiresAt = now.Add(record.Duration)
			err = run.fixtureBurn.inventory.Update(record)
			if err != nil {
				return nil, true, fmt.Errorf("eggRefreshInventory: %w", err)
			}
			run.record = record
			run.fixtureBurn.expiresAt = record.ExpiresAt
			if !run.fixtureBurn.isCreationPublished {
				// The initial A2 still belongs to its death publication. Queue
				// this A3 only after that owner's create, even across handoff.
				run.fixtureBurn.pendingRefreshes = append(run.fixtureBurn.pendingRefreshes, packet)
				return nil, true, nil
			}
			return [][]byte{packet}, true, nil
		}
	}
	return nil, false, nil
}
