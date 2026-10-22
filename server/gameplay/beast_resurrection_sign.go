package gameplay

import (
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/util"
	zoneeffect "github.com/darkspinnet/darkspin/server/zone/effect"
	effectraknet "github.com/darkspinnet/darkspin/server/zone/effect/raknet103"
)

func (e *beastPetResurrectionRun) syncSign(isEligible bool, at time.Time) ([][]byte, error) {
	if !isEligible || e.signInstanceID != 0 {
		if !isEligible {
			packets, err := e.removeSign()
			if err != nil {
				return nil, fmt.Errorf("ineligibleSign: %w", err)
			}
			return packets, nil
		}
		return nil, nil
	}
	instanceID, err := e.runtime.modifierPool.Allocate()
	if err != nil {
		return nil, fmt.Errorf("signAllocate: %w", err)
	}
	packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
		SourceObjectID: e.petObjectID, TargetObjectID: e.ownerObjectID,
		ModifierID: util.HashID("BeastPetResSign"), InstanceID: instanceID, Timestamp: e.timestamp(at), IsIndefinite: true,
	})
	if err != nil {
		releaseErr := e.runtime.modifierPool.Release(instanceID)
		return nil, fmt.Errorf("signCreate: %w", errors.Join(err, releaseErr))
	}
	e.signInstanceID = instanceID
	err = e.zone.Effect().Put(zoneeffect.Modifier{InstanceID: instanceID, GUID: util.HashID("BeastPetResSign"), SourceObjectID: e.petObjectID, TargetObjectID: e.ownerObjectID, Rank: 1, Kind: zoneeffect.ModifierKindBuff, IsIndefinite: true, StackCount: 1})
	if err != nil {
		releaseErr := e.runtime.modifierPool.Release(instanceID)
		e.signInstanceID = 0
		return nil, fmt.Errorf("signRecord: %w", errors.Join(err, releaseErr))
	}
	return [][]byte{packet}, nil
}

func (e *beastPetResurrectionRun) removeSign() ([][]byte, error) {
	if e.signInstanceID == 0 {
		return nil, nil
	}
	for _, modifier := range e.zone.Effect().Snapshot() {
		if modifier.InstanceID == e.signInstanceID &&
			(modifier.TargetObjectID != e.ownerObjectID || modifier.SourceObjectID != e.petObjectID || modifier.GUID != util.HashID("BeastPetResSign")) {
			return nil, errors.New("beast resurrection sign identity changed")
		}
	}
	packet, err := effectraknet.ModifierDelete(e.ownerObjectID, e.signInstanceID)
	if err != nil {
		return nil, fmt.Errorf("signDelete: %w", err)
	}
	err = e.runtime.modifierPool.Release(e.signInstanceID)
	if err != nil {
		return nil, fmt.Errorf("signRelease: %w", err)
	}
	removed, isRemoved := e.zone.Effect().Remove(e.signInstanceID)
	if isRemoved && removed.TargetObjectID != e.ownerObjectID {
		return nil, errors.New("beast resurrection sign target changed")
	}
	e.signInstanceID = 0
	return [][]byte{packet}, nil
}

// Replay only markers whose retained pet and owner still match. The sign is
// indefinite and belongs to the owner object even while that owner is dead.
func marshalBeastResurrectionSigns(peerSession gameplayPeerSession, ownerUserID uint64) ([][]byte, error) {
	packets := make([][]byte, 0)
	for _, modifier := range peerSession.zone.Effect().Snapshot() {
		if modifier.GUID != util.HashID("BeastPetResSign") || !modifier.IsIndefinite {
			continue
		}
		pet, isPetFound := peerSession.zone.Companion().Snapshot(modifier.SourceObjectID)
		owner, isOwnerFound := peerSession.zone.Hero().SnapshotByObjectID(modifier.TargetObjectID)
		if !isPetFound || !isOwnerFound || pet.HitPoint <= 0 || pet.OwnerObjectID != owner.ObjectID ||
			pet.UserID != owner.UserID || pet.PeerGeneration != owner.PeerGeneration ||
			(ownerUserID != 0 && pet.UserID != ownerUserID) {
			continue
		}
		packet, err := effectraknet.ModifierCreate(effectraknet.ModifierCreateRequest{
			SourceObjectID: pet.ObjectID, TargetObjectID: owner.ObjectID, ModifierID: modifier.GUID,
			InstanceID: modifier.InstanceID, StackCount: 1, IsIndefinite: true,
		})
		if err != nil {
			return nil, fmt.Errorf("signBaseline: %w", err)
		}
		packets = append(packets, packet)
	}
	return packets, nil
}
