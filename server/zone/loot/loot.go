package loot

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/darkspinnet/darkspin/server/sim"
)

const DNAChanceBasis = uint32(10000)

const dnaChanceThreshold = uint32(5000)
const enemyDNASourceAmount = float64(11)

type NPCDropKind uint8

const (
	NPCDropUnknown NPCDropKind = iota
	NPCDropEquipment
	NPCDropOrb
	NPCDropCrystal
	NPCDropDNA
)

type Rarity int32

const (
	RarityCommon Rarity = iota
	RarityUncommon
	RarityRare
	RarityEpic
)

type EquipmentRollParticipant struct {
	UserID   uint64
	ObjectID uint32
	Slot     uint16
}

type EquipmentRoll struct {
	UserID   uint64
	ObjectID uint32
	Slot     uint16
	Roll     uint32
}

type EquipmentRollResult struct {
	Winner EquipmentRoll
	Rolls  []EquipmentRoll
}

type enemyDropKey struct {
	objectID uint32
	kind     NPCDropKind
}

type Session struct {
	mu             sync.RWMutex
	reservedDrops  map[enemyDropKey]bool
	committedDrops map[enemyDropKey]bool
}

type Reservation struct {
	session *Session
	key     enemyDropKey
	isLive  bool
}

func NewSession() *Session {
	return &Session{
		reservedDrops: make(map[enemyDropKey]bool), committedDrops: make(map[enemyDropKey]bool),
	}
}

// RollEquipment reproduces the build-103 multiplayer equipment policy: every
// simulator participant draws 1..100, the highest roll wins, and an exact tie
// consumes one random bit to choose whether the later participant replaces the
// current winner. Callers provide participants in stable simulator-slot order.
func RollEquipment(
	participants []EquipmentRollParticipant,
	random *sim.SimulatorRandom,
) (EquipmentRollResult, error) {
	if len(participants) == 0 || random == nil {
		return EquipmentRollResult{}, errors.New("invalid equipment roll")
	}
	result := EquipmentRollResult{
		Rolls: make([]EquipmentRoll, 0, len(participants)),
	}
	users := make(map[uint64]struct{}, len(participants))
	slots := make(map[uint16]struct{}, len(participants))
	for index, participant := range participants {
		if participant.UserID == 0 || participant.ObjectID == 0 {
			return EquipmentRollResult{}, fmt.Errorf(
				"equipmentParticipant[%d]: invalid", index,
			)
		}
		if _, isFound := users[participant.UserID]; isFound {
			return EquipmentRollResult{}, fmt.Errorf(
				"equipmentParticipant[%d]: duplicate user", index,
			)
		}
		if _, isFound := slots[participant.Slot]; isFound {
			return EquipmentRollResult{}, fmt.Errorf(
				"equipmentParticipant[%d]: duplicate slot", index,
			)
		}
		users[participant.UserID] = struct{}{}
		slots[participant.Slot] = struct{}{}
		draw, err := random.Index(100)
		if err != nil {
			return EquipmentRollResult{}, fmt.Errorf(
				"equipmentDraw[%d]: %w", index, err,
			)
		}
		roll := EquipmentRoll{
			UserID: participant.UserID, ObjectID: participant.ObjectID,
			Slot: participant.Slot, Roll: draw + 1,
		}
		result.Rolls = append(result.Rolls, roll)
		if result.Winner.UserID == 0 || roll.Roll > result.Winner.Roll {
			result.Winner = roll
			continue
		}
		if roll.Roll != result.Winner.Roll {
			continue
		}
		tie, err := random.Index(2)
		if err != nil {
			return EquipmentRollResult{}, fmt.Errorf(
				"equipmentTie[%d]: %w", index, err,
			)
		}
		if tie == 1 {
			result.Winner = roll
		}
	}
	return result, nil
}

func (s *Session) ReserveNPCDrop(
	objectID uint32, kind NPCDropKind,
) (*Reservation, bool) {
	if s == nil || objectID == 0 || kind == NPCDropUnknown {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := enemyDropKey{objectID: objectID, kind: kind}
	if s.reservedDrops[key] || s.committedDrops[key] {
		return nil, false
	}
	s.reservedDrops[key] = true
	return &Reservation{session: s, key: key, isLive: true}, true
}

func (s *Session) IsCommitted(objectID uint32, kind NPCDropKind) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.committedDrops[enemyDropKey{objectID: objectID, kind: kind}]
}

func (s *Session) SuppressNPCDrops(objectID uint32) error {
	if s == nil || objectID == 0 {
		return errors.New("invalid enemy drop suppression")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for kind := NPCDropEquipment; kind <= NPCDropDNA; kind++ {
		key := enemyDropKey{objectID: objectID, kind: kind}
		delete(s.reservedDrops, key)
		s.committedDrops[key] = true
	}
	return nil
}

func (r *Reservation) Commit() error {
	if r == nil || r.session == nil {
		return errors.New("invalid enemy drop reservation")
	}
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	if !r.isLive || !r.session.reservedDrops[r.key] {
		return errors.New("invalid enemy drop reservation")
	}
	delete(r.session.reservedDrops, r.key)
	r.session.committedDrops[r.key] = true
	r.isLive = false
	return nil
}

func (r *Reservation) Release() bool {
	if r == nil || r.session == nil {
		return false
	}
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	if !r.isLive || !r.session.reservedDrops[r.key] {
		return false
	}
	delete(r.session.reservedDrops, r.key)
	r.isLive = false
	return true
}

func IsEquipmentDrop(challenge int32, chanceScale float32, randomDraw float64) (bool, error) {
	if randomDraw < 0 || randomDraw >= 1 ||
		math.IsNaN(randomDraw) || math.IsInf(randomDraw, 0) {
		return false, errors.New("invalid equipment random draw")
	}
	threshold, err := sim.EquipmentDropThreshold(1, challenge, 0.45, chanceScale)
	if err != nil {
		return false, fmt.Errorf("equipmentThreshold: %w", err)
	}
	return float32(randomDraw) < threshold, nil
}

func IsCrystalDrop(challenge int32, chanceScale float32, randomDraw uint32) (bool, error) {
	if randomDraw >= 100 {
		return false, errors.New("invalid crystal random draw")
	}
	threshold, err := sim.CrystalDropThreshold(challenge, chanceScale)
	if err != nil {
		return false, fmt.Errorf("crystalThreshold: %w", err)
	}
	return float32(randomDraw) < threshold, nil
}

func IsNPCDNADrop(randomDraw uint32) (bool, error) {
	if randomDraw >= DNAChanceBasis {
		return false, errors.New("invalid DNA chance draw")
	}
	return randomDraw < dnaChanceThreshold, nil
}

func NPCDNAAmount(difficulty uint32, droppedMultiplier float32, randomDraw uint32) (uint32, error) {
	if difficulty == 0 || droppedMultiplier < 0 || randomDraw > DNAChanceBasis ||
		math.IsNaN(float64(droppedMultiplier)) || math.IsInf(float64(droppedMultiplier), 0) {
		return 0, errors.New("invalid campaign DNA amount input")
	}
	quarter := (difficulty + 3) / 4
	remainder := (difficulty-1)%4 + 1
	exponent := 10*quarter + remainder
	growth := math.Pow(1.03, float64(min(exponent, uint32(70))))
	if exponent > 70 {
		growth *= math.Pow(1.02, float64(min(exponent-70, uint32(60))))
	}
	if exponent > 130 {
		growth *= math.Pow(1.01, float64(exponent-130))
	}
	randomFactor := 0.5 + float64(randomDraw)/float64(DNAChanceBasis)
	amount := math.Trunc(
		0.4 * enemyDNASourceAmount * growth * randomFactor *
			(float64(droppedMultiplier) + 1),
	)
	if amount < 1 {
		return 1, nil
	}
	if amount > math.MaxUint32 {
		return 0, errors.New("campaign DNA amount overflow")
	}
	return uint32(amount), nil
}

func EquipmentContainerNoun(rarity Rarity) string {
	switch rarity {
	case RarityUncommon:
		return "loot_container_green.Noun"
	case RarityRare:
		return "Loot_Drop.Noun"
	case RarityEpic:
		return "loot_container_purple.Noun"
	default:
		return "loot_container_white.Noun"
	}
}
