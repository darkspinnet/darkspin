package gameplay

import (
	"fmt"
	"sort"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/util"
)

func (e *omniShieldRun) presentationPackets(at time.Time, timestamp uint64) ([][]byte, error) {
	remaining := e.expiresAt.Sub(at)
	if remaining <= 0 {
		return nil, nil
	}
	packets := make([][]byte, 0, 2)
	for _, message := range []raknet.ApplicationMessage{
		raknet.ModifierCreatedMessage{TargetID: e.objectID, NPCAffixAssetID: 0, ModifierGUID: e.definition.RootModifierID,
			InstanceID: e.instanceID, StackCount: 1, StartMilliseconds: timestamp, DurationMilliseconds: uint32(remaining / time.Millisecond)},
		raknet.AttachedEffectMessage{ObjectID: e.objectID, Slot: e.effectSlot + 1, Asset: util.HashID(e.definition.ActivationEffectName), IsForceAttached: true},
	} {
		packet, err := raknet.MarshalApplication(message)
		if err != nil {
			return nil, fmt.Errorf("omniEffectMarshal: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}

func (e *gameplaySessionRegistry) omniShieldReplay(member gameplayPeerSession, at time.Time, timestamp uint64) ([][]byte, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	runs := make(map[uint32]*omniShieldRun)
	for _, candidate := range e.sessions {
		if !isOmniReplayPeer(candidate, member) {
			continue
		}
		for id, run := range candidate.omniShields {
			runs[id] = run
		}
	}
	for _, retained := range e.retainedSessions {
		if !isOmniReplayPeer(retained.peerSession, member) {
			continue
		}
		for id, run := range retained.peerSession.omniShields {
			runs[id] = run
		}
	}
	// Disconnect may temporarily carry the original aggregate outside both
	// endpoint indexes. Read its shared run identities without mutating that
	// snapshot or changing any combat target lookup.
	for _, handover := range e.voodooCharms.handovers {
		if !isOmniReplayPeer(handover.member, member) {
			continue
		}
		for id, run := range handover.member.omniShields {
			runs[id] = run
		}
	}
	ids := make([]uint32, 0, len(runs))
	for id := range runs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	packets := make([][]byte, 0)
	for _, id := range ids {
		run := runs[id]
		if !run.isActive || run.isRetired || !at.Before(run.expiresAt) {
			continue
		}
		created, err := run.presentationPackets(at, timestamp)
		if err != nil {
			return nil, fmt.Errorf("omniReplay: %w", err)
		}
		packets = append(packets, created...)
	}
	// A handover can briefly have no registry aggregate. Deletion leases stay
	// semantic-member-owned and are queued when that member is visible again.
	for _, removal := range e.omniShieldRemovals {
		if removal.world != member.zone {
			continue
		}
		delivery := removal.delivery(member.binding.UserID, member.generation)
		if delivery == nil || delivery.isQueued || delivery.isCommitted {
			continue
		}
		for key, candidate := range e.sessions {
			if candidate.zone == member.zone && candidate.binding.UserID == member.binding.UserID && candidate.generation == member.generation {
				candidate.queueOmniShieldDeletion(removal.packets, delivery)
				e.sessions[key] = candidate
				break
			}
		}
	}
	return packets, nil
}

func isOmniReplayPeer(candidate, member gameplayPeerSession) bool {
	if member.binding.Mode == game.ModeArena {
		return candidate.binding.Mode == game.ModeArena && candidate.binding.GameID == member.binding.GameID
	}
	return member.zone != nil && candidate.zone == member.zone
}
