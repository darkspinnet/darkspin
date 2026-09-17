package sim

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const CrystalSlotCount = 9
const crystalPickupRange = float32(2)
const crystalPickupReleaseDelay = time.Second
const crystalFullEventID uint32 = 0x6ea4091e
const crystalLinkBonus = float32(0.5)

var crystalLines = [8][3]int{
	{0, 1, 2}, {3, 4, 5}, {6, 7, 8},
	{0, 3, 6}, {1, 4, 7}, {2, 5, 8},
	{0, 4, 8}, {2, 4, 6},
}

type CrystalPickupAdmission uint8

const (
	CrystalPickupRejected CrystalPickupAdmission = iota
	CrystalPickupPursuit
	CrystalPickupAccepted
)

type CrystalPickupCommand struct {
	PlayerRole         Role
	PlayerIndex        uint8
	AgentRole          Role
	TargetRole         Role
	RangeDistance      float32
	IsAgentOwned       bool
	IsTargetLive       bool
	IsTargetPhaseOwned bool
	IsLootDataPresent  bool
	IsAbleToHit        bool
	SlotCount          int
	IsDiagonalUnlocked bool
}

type CrystalPickupToken struct {
	PlayerRole         Role
	PlayerIndex        uint8
	AgentRole          Role
	TargetRole         Role
	ReleaseAt          time.Duration
	SlotCount          int
	IsDiagonalUnlocked bool
}

type CrystalPickupAdmissionResult struct {
	Kind  CrystalPickupAdmission
	Token CrystalPickupToken
}

func AdmitCrystalPickup(
	now time.Duration, command CrystalPickupCommand,
) (CrystalPickupAdmissionResult, error) {
	if now < 0 || command.PlayerRole == "" || command.AgentRole == "" || command.TargetRole == "" ||
		math.IsNaN(float64(command.RangeDistance)) || math.IsInf(float64(command.RangeDistance), 0) ||
		command.RangeDistance < 0 {
		return CrystalPickupAdmissionResult{}, errors.New("invalid crystal pickup command")
	}
	if command.PlayerIndex >= 8 {
		return CrystalPickupAdmissionResult{}, fmt.Errorf("playerIndex: %d", command.PlayerIndex)
	}
	if command.SlotCount < 1 || command.SlotCount > CrystalSlotCount {
		return CrystalPickupAdmissionResult{}, fmt.Errorf("slotCount: %d", command.SlotCount)
	}
	if !command.IsAgentOwned || !command.IsTargetLive || !command.IsTargetPhaseOwned ||
		!command.IsLootDataPresent || !command.IsAbleToHit {
		return CrystalPickupAdmissionResult{Kind: CrystalPickupRejected}, nil
	}
	if command.RangeDistance > crystalPickupRange {
		return CrystalPickupAdmissionResult{Kind: CrystalPickupPursuit}, nil
	}
	return CrystalPickupAdmissionResult{
		Kind: CrystalPickupAccepted,
		Token: CrystalPickupToken{
			PlayerRole: command.PlayerRole, PlayerIndex: command.PlayerIndex,
			AgentRole: command.AgentRole, TargetRole: command.TargetRole,
			ReleaseAt:          now + crystalPickupReleaseDelay,
			SlotCount:          command.SlotCount,
			IsDiagonalUnlocked: command.IsDiagonalUnlocked,
		},
	}, nil
}

type CrystalSlot struct {
	IsOccupied   bool
	NounName     string
	NounAsset    uint32
	CrystalType  int32
	CrystalLevel int32
	Rarity       int32
}

type CrystalInventory struct {
	Slots              [CrystalSlotCount]CrystalSlot
	IsDiagonalUnlocked bool
}

type CrystalLinks struct {
	AreLinesActive [len(crystalLines)]bool
	LinkCounts     [CrystalSlotCount]int
}

type CrystalPickupObject struct {
	Role              Role
	NounName          string
	NounAsset         uint32
	CrystalType       int32
	CrystalLevel      int32
	Rarity            int32
	Position          Position
	LobEnd            time.Duration
	IsLive            bool
	IsPhaseOwned      bool
	IsLootDataPresent bool
}

type CrystalCollectionActionKind string

const (
	CrystalSlotAssigned      CrystalCollectionActionKind = "slot_assigned"
	CrystalPickupDeleted     CrystalCollectionActionKind = "pickup_deleted"
	CrystalAcquiredPublished CrystalCollectionActionKind = "acquired_published"
	CrystalBonusRecomputed   CrystalCollectionActionKind = "bonus_recomputed"
	CrystalFullPublished     CrystalCollectionActionKind = "full_published"
	CrystalPickupRelaunched  CrystalCollectionActionKind = "pickup_relaunched"
)

type CrystalCollectionAction struct {
	Kind           CrystalCollectionActionKind
	Slot           int32
	Role           Role
	NounName       string
	NounAsset      uint32
	CrystalType    int32
	CrystalLevel   int32
	ClientEventID  uint32
	PlayerSelector uint8
	Lob            CrystalLob
}

type CrystalCollectionResult struct {
	Actions []CrystalCollectionAction
}

func (i *CrystalInventory) ReleasePickup(
	now time.Duration, token CrystalPickupToken, pickup *CrystalPickupObject,
) (CrystalCollectionResult, error) {
	if i == nil || now < token.ReleaseAt || token.PlayerRole == "" || token.AgentRole == "" ||
		token.TargetRole == "" || token.PlayerIndex >= 8 {
		return CrystalCollectionResult{}, errors.New("invalid crystal pickup release")
	}
	if pickup == nil || !pickup.IsLive || pickup.Role != token.TargetRole {
		return CrystalCollectionResult{}, nil
	}
	if !pickup.IsPhaseOwned || !pickup.IsLootDataPresent || pickup.NounAsset == 0 ||
		!isFinitePosition(pickup.Position) {
		return CrystalCollectionResult{}, errors.New("invalid crystal pickup object")
	}
	i.IsDiagonalUnlocked = token.IsDiagonalUnlocked
	slotCount := min(token.SlotCount, len(i.Slots))
	for index := 0; index < slotCount; index++ {
		if i.Slots[index].IsOccupied {
			continue
		}
		i.Slots[index] = CrystalSlot{
			IsOccupied: true, NounName: pickup.NounName,
			NounAsset: pickup.NounAsset, CrystalType: pickup.CrystalType,
			CrystalLevel: pickup.CrystalLevel, Rarity: pickup.Rarity,
		}
		return CrystalCollectionResult{Actions: []CrystalCollectionAction{
			{Kind: CrystalSlotAssigned, Slot: int32(index), NounName: pickup.NounName,
				NounAsset:   pickup.NounAsset,
				CrystalType: pickup.CrystalType, CrystalLevel: pickup.CrystalLevel},
			{Kind: CrystalPickupDeleted, Role: pickup.Role},
			{Kind: CrystalAcquiredPublished, Slot: int32(index), NounName: pickup.NounName,
				NounAsset:   pickup.NounAsset,
				CrystalType: pickup.CrystalType, CrystalLevel: pickup.CrystalLevel},
			{Kind: CrystalBonusRecomputed},
		}}, nil
	}
	action := make([]CrystalCollectionAction, 0, 2)
	if now >= pickup.LobEnd {
		lob := CrystalLob{
			StartTime: now, Duration: 500 * time.Millisecond, Height: 3,
			PlaneDirectionVelocity: 1, UpLinearParameter: 12, UpQuadraticParameter: -12,
			IsGroundCollisionOnly: true,
		}
		pickup.LobEnd = now + lob.Duration
		action = append(action, CrystalCollectionAction{
			Kind: CrystalPickupRelaunched, Role: pickup.Role, Lob: lob,
		})
	}
	action = append(action, CrystalCollectionAction{
		Kind: CrystalFullPublished, ClientEventID: crystalFullEventID,
		PlayerSelector: ^uint8(1 << token.PlayerIndex),
	})
	return CrystalCollectionResult{Actions: action}, nil
}

// Move swaps an occupied catalyst into one of the player's unlocked slots.
func (i *CrystalInventory) Move(source int, destination int, slotCount int) bool {
	if i == nil || source < 0 || destination < 0 || source >= slotCount ||
		destination >= slotCount || slotCount > len(i.Slots) || !i.Slots[source].IsOccupied {
		return false
	}
	i.Slots[source], i.Slots[destination] = i.Slots[destination], i.Slots[source]
	return true
}

// Remove releases one catalyst from an unlocked slot for a world drop.
func (i *CrystalInventory) Remove(source int, slotCount int) (CrystalSlot, bool) {
	if i == nil || source < 0 || source >= slotCount || slotCount > len(i.Slots) ||
		!i.Slots[source].IsOccupied {
		return CrystalSlot{}, false
	}
	slot := i.Slots[source]
	i.Slots[source] = CrystalSlot{}
	return slot, true
}

// Restore puts a catalyst back if world-drop publication cannot commit.
func (i *CrystalInventory) Restore(slotIndex int, slot CrystalSlot) bool {
	if i == nil || slotIndex < 0 || slotIndex >= len(i.Slots) ||
		i.Slots[slotIndex].IsOccupied || !slot.IsOccupied {
		return false
	}
	i.Slots[slotIndex] = slot
	return true
}

// Links derives the eight replicated line flags and each slot's overlapping
// link count from one authoritative grid calculation.
func (e CrystalInventory) Links() CrystalLinks {
	links := CrystalLinks{}
	lineCount := 6
	if e.IsDiagonalUnlocked {
		lineCount = len(crystalLines)
	}
	for lineIndex := 0; lineIndex < lineCount; lineIndex++ {
		line := crystalLines[lineIndex]
		if !crystalRowMatches(e.Slots[line[0]], e.Slots[line[1]], e.Slots[line[2]]) {
			continue
		}
		links.AreLinesActive[lineIndex] = true
		for _, slotIndex := range line {
			links.LinkCounts[slotIndex]++
		}
	}
	return links
}

// Attributes derives server-owned catalyst contributions and matching-line
// bonuses using the build-103 item attribute indices consumed by gameplay.
func (i CrystalInventory) Attributes() map[int]float32 {
	links := i.Links()
	attributes := make(map[int]float32)
	for index, slot := range i.Slots {
		if !slot.IsOccupied {
			continue
		}
		attributeIndex, isPercentage, isFound := crystalAttribute(slot.CrystalType)
		if !isFound {
			continue
		}
		amount := float32(max(slot.CrystalLevel, int32(1)))
		if isPercentage {
			amount *= 0.01
		}
		amount *= 1 + 0.5*float32(max(slot.Rarity, int32(0)))
		amount *= 1 + crystalLinkBonus*float32(links.LinkCounts[index])
		attributes[attributeIndex] += amount
	}
	return attributes
}

func crystalRowMatches(first CrystalSlot, second CrystalSlot, third CrystalSlot) bool {
	if !first.IsOccupied || !second.IsOccupied || !third.IsOccupied {
		return false
	}
	for color := int32(0); color <= 4; color++ {
		if color == 1 {
			continue
		}
		if crystalMatchesColor(first, color) && crystalMatchesColor(second, color) &&
			crystalMatchesColor(third, color) {
			return true
		}
	}
	return false
}

func crystalMatchesColor(slot CrystalSlot, color int32) bool {
	isPrismatic := strings.HasPrefix(strings.ToLower(slot.NounName), "crystal_wild_")
	return isPrismatic || CrystalColorForType(slot.CrystalType) == color
}

// CrystalColorForType maps the server-owned stat identity to the five-way
// catalyst color consumed by build 103's crystal HUD packet.
func CrystalColorForType(crystalType int32) int32 {
	switch crystalType {
	case 0, 1, 5, 6, 7, 9, 19, 21, 30:
		return 3
	case 2, 8, 12, 24, 28, 29, 32:
		return 2
	case 3, 10, 11, 13, 14, 15, 16, 17, 18, 22, 33, 34:
		return 0
	default:
		return 4
	}
}

// CrystalColorForNoun preserves the prismatic class authored by wild nouns;
// ordinary nouns use the color associated with their stat identity.
func CrystalColorForNoun(nounName string, crystalType int32) int32 {
	name := strings.ToLower(strings.TrimSpace(nounName))
	if strings.HasPrefix(name, "crystal_wild_") {
		return 1
	}
	return CrystalColorForType(crystalType)
}

func crystalAttribute(crystalType int32) (int, bool, bool) {
	switch crystalType {
	case 0:
		return 37, true, true
	case 1:
		return 23, true, true
	case 2:
		return 50, true, true
	case 3:
		return 93, true, true
	case 4:
		return 24, true, true
	case 5:
		return 10, false, true
	case 6:
		return 109, true, true
	case 7:
		return 109, true, true
	case 8:
		return 51, true, true
	case 9:
		return 53, true, true
	case 10:
		return 7, false, true
	case 11:
		return 27, true, true
	case 12:
		return 1, false, true
	case 13:
		return 7, false, true
	case 14:
		return 4, false, true
	case 15:
		return 80, false, true
	case 16:
		return 75, false, true
	case 17:
		return 83, false, true
	case 18:
		return 73, false, true
	case 20:
		return 35, true, true
	case 21:
		return 5, false, true
	case 22:
		return 24, true, true
	case 23:
		return 52, true, true
	case 24:
		return 2, false, true
	case 25:
		return 48, true, true
	case 26:
		return 68, true, true
	case 27:
		return 69, true, true
	case 28:
		return 63, true, true
	case 29:
		return 64, true, true
	case 30:
		return 26, true, true
	case 31:
		return 67, true, true
	case 32:
		return 0, false, true
	case 33:
		return 72, true, true
	case 34:
		return 38, true, true
	default:
		return 0, false, false
	}
}
