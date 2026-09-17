package result

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/darkspinnet/darkspin/server/game"
)

type Phase uint8

const (
	Pending Phase = iota
	ChainVoting
	ContinueReserved
	ChainCashOut

	// Build 103 clamps the current-run fuel progression to five. Offering a
	// sixth Continue leaves the client repeatedly replacing full dungeon scenes
	// beyond the capacity represented by its planet-screen state.
	maximumChainRunLength = 5
)

// Snapshot freezes the authoritative match identity before the
// client enters its result state. CashOutReceipt retains the committed result
// for deterministic AB presentation and retry while the client owns its later
// local transition from Cash Out to the spaceship.
type Snapshot struct {
	ResultID           uint64
	GameID             uint32
	UserID             uint64
	Level              string
	Difficulty         uint32
	BossObjectID       uint32
	CompletedTime      uint64
	CompletedIndex     uint32
	PlanetsCompleted   uint8
	Phase              Phase
	ContinueSquadID    uint32
	NextLevel          string
	IsTerminal         bool
	EnemyNouns         [6]uint32
	MedalCounts        [4]MedalCount
	StartingExperience uint32
	FinalExperience    uint32
	FinalLevel         uint32
	RewardClassType    string
	RewardElementType  string
	CashOutReceipt     CashOutReceipt
}

type MedalCount struct {
	Gold   uint32
	Silver uint32
	Bronze uint32
}

// Session owns the mutable result lifecycle for one campaign participant.
// Transport sessions retain this narrow reference across chain transitions,
// while callers only receive detached snapshots of its state.
type Session struct {
	mutex               sync.RWMutex
	snapshot            Snapshot
	isCashOutCommitting bool
}

type CashOutPart struct {
	RigblockAssetHash        uint32
	SuffixAssetHash          uint32
	PrefixAssetHash          uint32
	PrefixSecondaryAssetHash uint32
	Level                    uint16
	Rarity                   uint8
}

type CashOutReceipt struct {
	ResultID            uint64
	ChainProgression    uint32
	AccountExperience   uint32
	Parts               []CashOutPart
	IsDailyBonusGranted bool
	IsCommitted         bool
}

func NewSession(snapshot Snapshot) *Session {
	return &Session{snapshot: cloneSnapshot(snapshot)}
}

func (s *Session) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return cloneSnapshot(s.snapshot)
}

func (s *Session) AcceptCashOut() bool {
	if s == nil {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.snapshot.acceptCashOut()
}

func (s *Session) AcceptContinue(squadID uint32, nextLevel string) bool {
	if s == nil {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.snapshot.acceptContinue(squadID, nextLevel)
}

func (s *Session) ReserveCashOutCommit() (Snapshot, bool) {
	if s == nil {
		return Snapshot{}, false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.snapshot.Phase != ChainCashOut ||
		s.snapshot.CashOutReceipt.IsCommitted ||
		s.isCashOutCommitting {
		return cloneSnapshot(s.snapshot), false
	}
	s.isCashOutCommitting = true
	return cloneSnapshot(s.snapshot), true
}

func (s *Session) RollbackCashOutCommit(resultID uint64) bool {
	if s == nil || resultID == 0 {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.snapshot.ResultID != resultID || !s.isCashOutCommitting ||
		s.snapshot.CashOutReceipt.IsCommitted {
		return false
	}
	s.isCashOutCommitting = false
	return true
}

func (s *Session) CommitCashOut(receipt CashOutReceipt) bool {
	if s == nil {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.snapshot.ResultID != receipt.ResultID ||
		s.snapshot.Phase != ChainCashOut ||
		!s.isCashOutCommitting ||
		!receipt.IsCommitted {
		return false
	}
	s.snapshot.CashOutReceipt = cloneCashOutReceipt(receipt)
	s.isCashOutCommitting = false
	return true
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.CashOutReceipt = cloneCashOutReceipt(snapshot.CashOutReceipt)
	return snapshot
}

func cloneCashOutReceipt(receipt CashOutReceipt) CashOutReceipt {
	receipt.Parts = append([]CashOutPart(nil), receipt.Parts...)
	return receipt
}

func NewSnapshot(
	binding game.GameplayBinding, generation uint64, bossObjectID uint32,
	completedTime uint64,
	planetsCompleted uint8, chainLevel []string,
) (Snapshot, error) {
	resultID, err := newCampaignResultID()
	if err != nil {
		return Snapshot{}, fmt.Errorf("campaign result id: %w", err)
	}
	return NewSnapshotWithResultID(
		binding, generation, resultID, bossObjectID, completedTime,
		planetsCompleted, chainLevel,
	)
}

// NewSnapshotWithResultID binds result presentation to a durable zone
// completion identity so a restored process cannot create a second account
// transaction for the same mission.
func NewSnapshotWithResultID(
	binding game.GameplayBinding, generation uint64, resultID uint64,
	bossObjectID uint32, completedTime uint64,
	planetsCompleted uint8, chainLevel []string,
) (Snapshot, error) {
	if binding.Mode != game.ModeChain || binding.GameID == 0 || binding.UserID == 0 ||
		binding.Level == "" || binding.Difficulty < game.MinimumCampaignDifficulty ||
		binding.Difficulty > game.MaximumCampaignDifficulty || generation == 0 ||
		resultID == 0 || bossObjectID == 0 ||
		planetsCompleted == 0 {
		return Snapshot{}, errors.New("campaign result snapshot: invalid")
	}
	offer, err := ResolveOffer(OfferCommand{
		CurrentLevel:    binding.Level,
		ChainLevelIndex: binding.ChainLevelIndex,
		ChainLevel:      chainLevel,
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("campaign result chain: %w", err)
	}
	return Snapshot{
		ResultID: resultID,
		GameID:   binding.GameID, UserID: binding.UserID, Level: binding.Level,
		Difficulty: binding.Difficulty, BossObjectID: bossObjectID,
		CompletedTime: completedTime, Phase: ChainVoting,
		CompletedIndex: offer.CompletedIndex, PlanetsCompleted: planetsCompleted,
		NextLevel:  offer.NextLevel,
		IsTerminal: offer.IsTerminal || planetsCompleted >= maximumChainRunLength,
	}, nil
}

func newCampaignResultID() (uint64, error) {
	var resultBytes [8]byte
	_, err := cryptorand.Read(resultBytes[:])
	if err != nil {
		return 0, fmt.Errorf("random: %w", err)
	}
	resultID := binary.LittleEndian.Uint64(resultBytes[:]) & uint64(math.MaxInt64)
	if resultID == 0 {
		resultID = 1
	}
	return resultID, nil
}

func (s *Snapshot) acceptCashOut() bool {
	if s == nil || s.Phase != ChainVoting {
		return false
	}
	s.Phase = ChainCashOut
	return true
}

func (s *Snapshot) acceptContinue(squadID uint32, nextLevel string) bool {
	if s == nil || s.Phase != ChainVoting || squadID == 0 || nextLevel == "" {
		return false
	}
	s.Phase = ContinueReserved
	s.ContinueSquadID = squadID
	s.NextLevel = nextLevel
	return true
}

func (s Snapshot) IsContinueReplay(squadID uint32, nextLevel string) bool {
	return s.Phase == ContinueReserved && s.ContinueSquadID == squadID &&
		s.NextLevel == nextLevel
}
