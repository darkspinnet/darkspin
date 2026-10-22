package loot

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
)

const DNAChanceBasis = uint32(10000)

const dnaChanceThreshold = uint32(5000)

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
	return randomDraw < float64(threshold), nil
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

type NPCDNAAmountInput struct {
	Stage             uint32
	Challenge         int32
	DroppedMultiplier float32
	RandomDraw        uint32
	Tuning            game.DNADropTuning
}

func NPCDNAAmount(req NPCDNAAmountInput) (uint32, error) {
	err := req.Tuning.Validate()
	if err != nil {
		return 0, fmt.Errorf("dnaTuning: %w", err)
	}
	if req.Stage == 0 || req.DroppedMultiplier < 0 || req.RandomDraw > DNAChanceBasis ||
		math.IsNaN(float64(req.DroppedMultiplier)) || math.IsInf(float64(req.DroppedMultiplier), 0) {
		return 0, errors.New("invalid campaign DNA amount input")
	}
	quarter := (uint64(req.Stage) + 3) / 4
	remainder := (uint64(req.Stage)-1)%4 + 1
	exponent := 10*quarter + remainder
	// Native sub_9CF9A0 stores the later pow result and the >130
	// intermediate product as float32 before multiplying the first segment.
	firstGrowth := math.Pow(float64(req.Tuning.FirstGrowthBase), float64(min(exponent, uint64(70))))
	growth := float32(firstGrowth)
	if exponent > 130 {
		lastGrowth := float32(math.Pow(float64(float32(1.01)), float64(exponent-130)))
		laterGrowth := float32(math.Pow(float64(float32(1.02)), 60) * float64(lastGrowth))
		growth = float32(firstGrowth * float64(laterGrowth))
	} else if exponent > 70 {
		laterGrowth := float32(math.Pow(float64(float32(1.02)), float64(exponent-70)))
		growth = float32(firstGrowth * float64(laterGrowth))
	}
	factorWidth := float32(req.Tuning.AmountFactors[1] - req.Tuning.AmountFactors[0])
	randomFactor := float32(float64(req.Tuning.AmountFactors[0]) +
		float64(factorWidth)*float64(req.RandomDraw)/float64(DNAChanceBasis))
	// Explicit conversions preserve each native float32 rounding boundary and
	// prevent fused operations from changing the final signed truncation.
	amount := float32(1 / float64(req.Tuning.Chance))
	amount = float32(amount * float32(req.Challenge))
	amount = float32(amount * req.Tuning.AmountScalar)
	amount = float32(amount * growth)
	amount = float32(amount * randomFactor)
	amount = float32(amount * float32(req.DroppedMultiplier+1))
	if math.IsNaN(float64(amount)) || math.IsInf(float64(amount), 0) || float64(amount) >= float64(math.MaxInt32)+1 {
		return 0, errors.New("campaign DNA amount overflow")
	}
	if amount < 1 {
		return 1, nil
	}
	return uint32(int32(amount)), nil
}

func EquipmentContainerNoun(rarity Rarity) (string, bool) {
	switch rarity {
	case RarityCommon:
		return "loot_container_white.Noun", true
	case RarityUncommon:
		return "loot_container_green.Noun", true
	case RarityRare:
		return "Loot_Drop.Noun", true
	case RarityEpic:
		return "loot_container_purple.Noun", true
	default:
		return "", false
	}
}

type EquipmentPresentationPolicy uint8

const (
	EquipmentOrdinaryWorldDrop EquipmentPresentationPolicy = iota
	EquipmentInventoryGroundDrop
	EquipmentUniqueRewardGroundDrop
)

func (e EquipmentPresentationPolicy) ContainerNoun(rarity Rarity) (string, bool) {
	switch e {
	case EquipmentOrdinaryWorldDrop:
		return EquipmentContainerNoun(rarity)
	case EquipmentInventoryGroundDrop, EquipmentUniqueRewardGroundDrop:
		// Server policy preserves existing unique-item ground presentation.
		// These three inventory tiers are outside the native ordinary lookup.
		if rarity == 4 || rarity == 5 || rarity == 6 {
			return "loot_container_white.Noun", true
		}
		return EquipmentContainerNoun(rarity)
	default:
		return "", false
	}
}
