package gameplay

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/zone"
	zonecheckpoint "github.com/darkspinnet/darkspin/server/zone/checkpoint"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
)

type ownedPartDetailConverter interface {
	ConvertPartsToDetail(context.Context, *sporenet.User, []uint64) ([]sporenet.Part, error)
}

type gameplayPartDetailConverter struct {
	registry    *gameplaySessionRegistry
	progression ownedPartDetailConverter
}

func newPartDetailConverter(registry *gameplaySessionRegistry, progression Progression) game.PartDetailConverter {
	converter, isSupported := progression.(ownedPartDetailConverter)
	if !isSupported || registry == nil {
		return nil
	}
	return gameplayPartDetailConverter{registry: registry, progression: converter}
}

func (e Lifecycle) ConvertPartsToDetail(ctx context.Context, user *sporenet.User, partIDs []uint64) error {
	if e.partDetailConverter == nil {
		return errors.New("part detail service unavailable")
	}
	err := e.partDetailConverter.ConvertPartsToDetail(ctx, user, partIDs)
	if err != nil {
		return fmt.Errorf("detailGameplay: %w", err)
	}
	return nil
}

type gameplayOwnedPartDetailStore struct {
	converter ownedPartDetailConverter
	user      *sporenet.User
}

func (e gameplayOwnedPartDetailStore) ConvertOwnedPartsToDetail(ctx context.Context, partIDs []uint64) error {
	parts, err := e.converter.ConvertPartsToDetail(ctx, e.user, partIDs)
	if err != nil {
		return fmt.Errorf("detailSave: %w", err)
	}
	if len(parts) != len(partIDs) {
		return errors.New("owned detail result incomplete")
	}
	return nil
}

func (e gameplayPartDetailConverter) ConvertPartsToDetail(
	ctx context.Context, user *sporenet.User, partIDs []uint64,
) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("detailContext: %w", err)
	}
	if user == nil || user.AccountID() <= 0 || len(partIDs) == 0 {
		return errors.New("part detail actor unavailable")
	}
	userID := uint64(user.AccountID())
	gameID := user.CurrentGameID()
	current, err := e.convertSelection(ctx, user, userID, gameID, partIDs)
	if err != nil {
		return fmt.Errorf("detailSelection: %w", err)
	}
	if current != nil {
		isCheckpointSaved := current.SaveCheckpointIfSafe(zonecheckpoint.ReasonSafePickup)
		if !isCheckpointSaved {
			// Combat defers the durable checkpoint; the zone retains the detail
			// flags for its next safe checkpoint or successful reward claim.
			return nil
		}
	}
	return nil
}

type missionPartDetailMember struct {
	zone   *zone.Zone
	member zone.Member
}

func (e gameplayPartDetailConverter) missionMember(userID uint64, gameID uint32) (missionPartDetailMember, bool, error) {
	e.registry.mutex.Lock()
	defer e.registry.mutex.Unlock()
	selected := missionPartDetailMember{}
	isMemberFound := false
	for _, candidate := range e.registry.sessions {
		if candidate.binding.UserID != userID || candidate.binding.GameID != gameID ||
			candidate.zone == nil || !candidate.stage.IsDungeon() || candidate.isZoneTerminal() {
			continue
		}
		if isMemberFound && candidate.zone != selected.zone {
			return missionPartDetailMember{}, false, errors.New("part detail membership ambiguous")
		}
		if isMemberFound && candidate.generation <= selected.member.PeerGeneration {
			continue
		}
		selected = missionPartDetailMember{zone: candidate.zone, member: zone.Member{
			UserID: userID, PeerGeneration: candidate.generation,
		}}
		isMemberFound = true
	}
	return selected, isMemberFound, nil
}

func (e gameplayPartDetailConverter) convertSelection(
	ctx context.Context, user *sporenet.User, userID uint64, gameID uint32, partIDs []uint64,
) (*zone.Zone, error) {
	selected, isMemberFound, err := e.missionMember(userID, gameID)
	if err != nil {
		return nil, fmt.Errorf("detailMember: %w", err)
	}
	req := zoneloot.EquipmentDetailRequest{}
	seenIDs := make(map[uint64]bool, len(partIDs))
	for _, partID := range partIDs {
		if partID == 0 || partID == campaignEquipmentTransientIDMask || seenIDs[partID] {
			return nil, errors.New("part detail identity invalid")
		}
		seenIDs[partID] = true
		objectID, isMissionID := missionLootDropObjectID(partID)
		if isMissionID && gameID != 0 && !isMemberFound {
			return nil, errors.New("mission detail session unavailable")
		}
		if isMemberFound && isMissionID {
			// In an active mission this namespace belongs to collected drops.
			// An unknown mission identity must never fall through to an unrelated
			// account part with the same low bits.
			req.ObjectIDs = append(req.ObjectIDs, objectID)
			continue
		}
		// Preserve the established ship inventory aliases, including bit 31's
		// Create Detail identities. During a mission bit 32 explicitly names
		// an account part, so mixed account/mission batches remain unambiguous.
		if partID >= uint64(1)<<32 && partID < uint64(1)<<33 {
			partID -= uint64(1) << 32
		} else if isMissionID {
			partID -= campaignEquipmentTransientIDMask
		}
		req.OwnedPartIDs = append(req.OwnedPartIDs, partID)
	}
	store := gameplayOwnedPartDetailStore{converter: e.progression, user: user}
	if len(req.ObjectIDs) == 0 {
		err := store.ConvertOwnedPartsToDetail(ctx, req.OwnedPartIDs)
		if err != nil {
			return nil, fmt.Errorf("detailAccount: %w", err)
		}
		return nil, nil
	}
	err = selected.zone.ConvertMissionEquipmentToDetail(ctx, selected.member, req, store)
	if err != nil {
		return nil, fmt.Errorf("detailMission: %w", err)
	}
	return selected.zone, nil
}
