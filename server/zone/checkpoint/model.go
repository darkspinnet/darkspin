// Package checkpoint owns durable, gameplay-equivalent zone checkpoints.
package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/darkspin/server/squad"
	zonehero "github.com/darkspinnet/darkspin/server/zone/hero"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zoneloot "github.com/darkspinnet/darkspin/server/zone/loot"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
)

const Version = uint32(5)

type Reason string

const (
	ReasonSpawnGroupCleared Reason = "spawn-group-cleared"
	ReasonSafePickup        Reason = "safe-pickup"
	ReasonDeployment        Reason = "deployment"
)

type Member struct {
	UserID       uint64
	Slot         uint16
	AbilityCount uint32
}

type Hero struct {
	UserID           uint64
	ObjectID         uint32
	CreatureIndex    uint32
	Position         game.Vec3
	FootprintRadius  float32
	HitPoint         float32
	ManaPoint        float32
	MaximumHitPoint  float32
	MaximumManaPoint float32
	IsStealthed      bool
}

type NPC struct {
	State zonenpc.Snapshot
}

type Squad struct {
	UserID   uint64
	Position game.Vec3
	State    squad.State
}

type Crystal struct {
	UserID    uint64
	Inventory sim.CrystalInventory
}

type ExperienceAward struct {
	ObjectID       uint32
	BaseExperience uint32
	MemberAwards   map[uint64]uint32
}

type Snapshot struct {
	Version              uint32
	ZoneID               uint64
	RunSeed              uint64
	ZoneGeneration       uint64
	CompletionID         uint64
	Revision             uint64
	Level                string
	Difficulty           uint32
	Reason               Reason
	SavedAt              time.Time
	Elapsed              time.Duration
	Members              []Member
	Heroes               []Hero
	Squads               []Squad
	NPCs                 []NPC
	Hordes               []zonehorde.Snapshot
	Crystals             []Crystal
	ExperienceAwards     []ExperienceAward
	MissionEquipments    []zoneloot.EquipmentInventory
	Security             zonesecurity.Snapshot
	Objectives           []sim.ObjectiveSnapshot
	ScriptUses           []game.CampaignScriptUse
	ClearedSpawnGroupIDs []uint32
	DropRandom           sim.RandomSnapshot
	IsDropRandomSet      bool
}

type Store interface {
	Save(context.Context, Snapshot) error
	Load(context.Context, uint64) (Snapshot, bool, error)
	LoadMember(context.Context, uint64) (Snapshot, bool, error)
	Delete(context.Context, uint64) error
}

type Recorder interface {
	Record(Snapshot)
}

// Repository is the checkpoint boundary consumed by zone orchestration. It
// keeps persistence mechanics out of zone business state while supporting
// restart restoration and terminal invalidation.
type Repository interface {
	Recorder
	Load(context.Context, uint64) (Snapshot, bool, error)
	Discard(uint64)
	ForfeitEquipment(uint64, uint64)
}

func Validate(
	snapshot Snapshot, zoneID uint64, level string, difficulty uint32,
	userID uint64,
) error {
	if snapshot.Version != Version {
		return fmt.Errorf("checkpointVersion[%d]: unsupported", snapshot.Version)
	}
	if snapshot.IsDropRandomSet {
		restoredRandom, err := sim.NewSimulatorRandomFromSnapshot(snapshot.DropRandom)
		if err != nil {
			return fmt.Errorf("checkpointDropRandom: %w", err)
		}
		if restoredRandom == nil {
			return errors.New("checkpoint drop random unavailable")
		}
	}
	if snapshot.ZoneID == 0 || snapshot.ZoneID != zoneID {
		return errors.New("checkpoint zone mismatch")
	}
	if snapshot.ZoneGeneration == 0 || snapshot.CompletionID == 0 ||
		snapshot.Revision == 0 ||
		snapshot.SavedAt.IsZero() || snapshot.Elapsed < 0 {
		return errors.New("checkpoint identity invalid")
	}
	if strings.TrimSpace(snapshot.Level) == "" {
		return errors.New("checkpoint level unavailable")
	}
	if !strings.EqualFold(snapshot.Level, level) ||
		snapshot.Difficulty != difficulty {
		return errors.New("checkpoint mission mismatch")
	}
	if snapshot.Reason != ReasonSpawnGroupCleared &&
		snapshot.Reason != ReasonSafePickup && snapshot.Reason != ReasonDeployment {
		return fmt.Errorf("checkpointReason: %q", snapshot.Reason)
	}
	err := zonesecurity.ValidateSnapshot(snapshot.Security)
	if err != nil {
		return fmt.Errorf("checkpointSecurity: %w", err)
	}
	err = zonehorde.ValidateCompletedSnapshots(snapshot.Hordes)
	if err != nil {
		return fmt.Errorf("checkpointHorde: %w", err)
	}
	if len(snapshot.Members) == 0 ||
		len(snapshot.Members) > int(game.MaxGamePlayers) {
		return errors.New("checkpoint member count invalid")
	}
	memberIDs := make(map[uint64]struct{}, len(snapshot.Members))
	memberSlots := make(map[uint16]struct{}, len(snapshot.Members))
	isMemberFound := false
	for index, member := range snapshot.Members {
		if member.UserID == 0 || member.Slot >= game.MaxGamePlayers {
			return fmt.Errorf("checkpointMember[%d]: invalid", index)
		}
		if _, isDuplicate := memberIDs[member.UserID]; isDuplicate {
			return fmt.Errorf("checkpointMember[%d]: duplicate user", index)
		}
		if _, isDuplicate := memberSlots[member.Slot]; isDuplicate {
			return fmt.Errorf("checkpointMember[%d]: duplicate slot", index)
		}
		memberIDs[member.UserID] = struct{}{}
		memberSlots[member.Slot] = struct{}{}
		if member.UserID == userID {
			isMemberFound = true
		}
	}
	npcsByObjectID := make(map[uint32]zonenpc.Snapshot, len(snapshot.NPCs))
	equipmentUserIDs := make(map[uint64]struct{}, len(snapshot.MissionEquipments))
	equipmentObjectIDs := make(map[uint32]struct{})
	for _, inventory := range snapshot.MissionEquipments {
		if _, isMember := memberIDs[inventory.UserID]; !isMember {
			return errors.New("checkpoint loot owner unavailable")
		}
		if _, isDuplicate := equipmentUserIDs[inventory.UserID]; isDuplicate {
			return errors.New("checkpoint loot owner duplicated")
		}
		equipmentUserIDs[inventory.UserID] = struct{}{}
		if inventory.IsForfeited && len(inventory.Equipments) != 0 {
			return errors.New("checkpoint forfeited loot retained")
		}
		for _, equipment := range inventory.Equipments {
			if equipment.ObjectID == 0 || equipment.RigblockID == 0 {
				return errors.New("checkpoint loot item invalid")
			}
			if _, isDuplicate := equipmentObjectIDs[equipment.ObjectID]; isDuplicate {
				return errors.New("checkpoint loot item duplicated")
			}
			equipmentObjectIDs[equipment.ObjectID] = struct{}{}
		}
	}
	for index, npc := range snapshot.NPCs {
		objectID := npc.State.Plan.ObjectID
		if objectID == 0 {
			return fmt.Errorf("checkpointNPC[%d]: object invalid", index)
		}
		if _, isDuplicate := npcsByObjectID[objectID]; isDuplicate {
			return fmt.Errorf("checkpointNPC[%d]: duplicate object", index)
		}
		npcsByObjectID[objectID] = npc.State
	}
	experienceObjectIDs := make(map[uint32]struct{}, len(snapshot.ExperienceAwards))
	experienceTotals := make(map[uint64]uint32, len(snapshot.Members))
	for index, award := range snapshot.ExperienceAwards {
		if award.ObjectID == 0 || award.BaseExperience == 0 ||
			len(award.MemberAwards) == 0 {
			return fmt.Errorf("checkpointExperience[%d]: invalid", index)
		}
		if _, isDuplicate := experienceObjectIDs[award.ObjectID]; isDuplicate {
			return fmt.Errorf("checkpointExperience[%d]: duplicate object", index)
		}
		npc, isNPCFound := npcsByObjectID[award.ObjectID]
		if !isNPCFound || !npc.IsDefeated || npc.HitPoint > 0 ||
			npc.Plan.IsFixture || npc.Plan.IsRewardSuppressed ||
			npc.Plan.Experience != award.BaseExperience {
			return fmt.Errorf("checkpointExperience[%d]: npc mismatch", index)
		}
		for awardUserID, experience := range award.MemberAwards {
			if experience == 0 {
				return fmt.Errorf("checkpointExperience[%d]: amount invalid", index)
			}
			if _, isMember := memberIDs[awardUserID]; !isMember {
				return fmt.Errorf("checkpointExperience[%d]: member unavailable", index)
			}
			if experience > ^uint32(0)-experienceTotals[awardUserID] {
				return fmt.Errorf("checkpointExperience[%d]: total overflow", index)
			}
			experienceTotals[awardUserID] += experience
		}
		experienceObjectIDs[award.ObjectID] = struct{}{}
	}
	crystalUserIDs := make(map[uint64]struct{}, len(snapshot.Crystals))
	for index, crystal := range snapshot.Crystals {
		if _, isMember := memberIDs[crystal.UserID]; !isMember {
			return fmt.Errorf("checkpointCrystal[%d]: member unavailable", index)
		}
		if _, isDuplicate := crystalUserIDs[crystal.UserID]; isDuplicate {
			return fmt.Errorf("checkpointCrystal[%d]: duplicate user", index)
		}
		for slotIndex, slot := range crystal.Inventory.Slots {
			isSlotValid := (slot.IsOccupied && slot.NounAsset != 0) ||
				(!slot.IsOccupied && slot.NounAsset == 0 && slot.CrystalType == 0)
			if !isSlotValid {
				return fmt.Errorf(
					"checkpointCrystal[%d].slot[%d]: invalid", index, slotIndex,
				)
			}
		}
		crystalUserIDs[crystal.UserID] = struct{}{}
	}
	for index, member := range snapshot.Members {
		if _, isFound := crystalUserIDs[member.UserID]; !isFound {
			return fmt.Errorf("checkpointMember[%d]: crystal unavailable", index)
		}
	}
	if !isMemberFound {
		return errors.New("checkpoint member unavailable")
	}
	squadIDs := make(map[uint64]struct{}, len(snapshot.Squads))
	for index, restoredSquad := range snapshot.Squads {
		if _, isMember := memberIDs[restoredSquad.UserID]; !isMember {
			return fmt.Errorf("checkpointSquad[%d]: member unavailable", index)
		}
		if _, isDuplicate := squadIDs[restoredSquad.UserID]; isDuplicate {
			return fmt.Errorf("checkpointSquad[%d]: duplicate", index)
		}
		if !isFiniteCheckpointPosition(restoredSquad.Position) {
			return fmt.Errorf("checkpointSquad[%d]: position invalid", index)
		}
		_, err = squad.Restore(restoredSquad.State)
		if err != nil {
			return fmt.Errorf("checkpointSquad[%d]: %w", index, err)
		}
		squadIDs[restoredSquad.UserID] = struct{}{}
	}
	for index, member := range snapshot.Members {
		if _, isFound := squadIDs[member.UserID]; !isFound {
			return fmt.Errorf("checkpointMember[%d]: squad unavailable", index)
		}
	}
	heroUserIDs := make(map[uint64]struct{}, len(snapshot.Heroes))
	heroObjectIDs := make(map[uint32]struct{}, len(snapshot.Heroes))
	for index, hero := range snapshot.Heroes {
		if _, isMember := memberIDs[hero.UserID]; !isMember {
			return fmt.Errorf("checkpointHero[%d]: member unavailable", index)
		}
		if hero.ObjectID == 0 || hero.CreatureIndex >= squad.Size ||
			!isFiniteCheckpointPosition(hero.Position) ||
			!isFiniteCheckpointScalar(hero.FootprintRadius) ||
			hero.FootprintRadius <= 0 ||
			!isCheckpointResource(hero.HitPoint, hero.MaximumHitPoint) ||
			!isCheckpointResource(hero.ManaPoint, hero.MaximumManaPoint) {
			return fmt.Errorf("checkpointHero[%d]: invalid", index)
		}
		if _, isDuplicate := heroUserIDs[hero.UserID]; isDuplicate {
			return fmt.Errorf("checkpointHero[%d]: duplicate user", index)
		}
		if _, isDuplicate := heroObjectIDs[hero.ObjectID]; isDuplicate {
			return fmt.Errorf("checkpointHero[%d]: duplicate object", index)
		}
		memberSlot := uint16(0)
		for _, member := range snapshot.Members {
			if member.UserID == hero.UserID {
				memberSlot = member.Slot
				break
			}
		}
		if hero.ObjectID != zonehero.ObjectID(memberSlot, hero.CreatureIndex) {
			return fmt.Errorf("checkpointHero[%d]: object mismatch", index)
		}
		restoredSquad := snapshot.Squads[0]
		for _, candidate := range snapshot.Squads {
			if candidate.UserID == hero.UserID {
				restoredSquad = candidate
				break
			}
		}
		if restoredSquad.State.CharacterIndex != hero.CreatureIndex {
			return fmt.Errorf("checkpointHero[%d]: deployed mismatch", index)
		}
		heroUserIDs[hero.UserID] = struct{}{}
		heroObjectIDs[hero.ObjectID] = struct{}{}
	}
	for index, member := range snapshot.Members {
		if _, isFound := heroUserIDs[member.UserID]; !isFound {
			return fmt.Errorf("checkpointMember[%d]: hero unavailable", index)
		}
	}
	objectiveIDs := make(map[uint32]struct{}, len(snapshot.Objectives))
	for index, objective := range snapshot.Objectives {
		if objective.ObjectiveID == 0 {
			return fmt.Errorf("checkpointObjective[%d]: invalid", index)
		}
		if _, isDuplicate := objectiveIDs[objective.ObjectiveID]; isDuplicate {
			return fmt.Errorf("checkpointObjective[%d]: duplicate", index)
		}
		for playerIndex, medal := range objective.State {
			_, isMember := memberSlots[uint16(playerIndex)]
			if medal > 4 || (isMember && medal == 0) ||
				(!isMember && medal != 0) {
				return fmt.Errorf(
					"checkpointObjective[%d].player[%d]: invalid",
					index, playerIndex,
				)
			}
		}
		objectiveIDs[objective.ObjectiveID] = struct{}{}
	}
	clearedSpawnGroupIDs := make(map[uint32]struct{}, len(snapshot.ClearedSpawnGroupIDs))
	for index, spawnGroupID := range snapshot.ClearedSpawnGroupIDs {
		if spawnGroupID == 0 {
			return fmt.Errorf("checkpointSpawnGroup[%d]: invalid", index)
		}
		if _, isDuplicate := clearedSpawnGroupIDs[spawnGroupID]; isDuplicate {
			return fmt.Errorf("checkpointSpawnGroup[%d]: duplicate", index)
		}
		clearedSpawnGroupIDs[spawnGroupID] = struct{}{}
	}
	return nil
}

func isFiniteCheckpointPosition(position game.Vec3) bool {
	return isFiniteCheckpointScalar(position.X) &&
		isFiniteCheckpointScalar(position.Y) &&
		isFiniteCheckpointScalar(position.Z)
}

func isFiniteCheckpointScalar(scalar float32) bool {
	return !math.IsNaN(float64(scalar)) && !math.IsInf(float64(scalar), 0)
}

func isCheckpointResource(current float32, maximum float32) bool {
	return isFiniteCheckpointScalar(current) &&
		isFiniteCheckpointScalar(maximum) &&
		current >= 0 && maximum > 0 && current <= maximum
}
