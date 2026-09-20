package squad

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const Size = 3

var (
	ErrCharacterUnavailable = errors.New("squad character unavailable")
	ErrCharacterDead        = errors.New("squad character dead")
	ErrDeployCooldown       = errors.New("squad deployment cooldown active")
	ErrGameOver             = errors.New("squad game over")
)

// Character is transient match state. It deliberately does not contain a
// persistent creature or a protocol object representation.
type Character struct {
	HitPoints   float32
	ManaPoints  float32
	IsAvailable bool
}

// State is the transport-neutral, durable state needed to resume a squad at a
// safe gameplay boundary. Deployment cooldowns and restart reservations are
// deliberately transient and are not restored.
type State struct {
	CharacterIndex uint32
	Characters     [Size]Character
	IsGameOver     bool
}

// Squad owns the three selected match characters and the terminal solo-player
// wipe latch. Only an explicit resurrection can clear it; ordinary health
// changes cannot resume the same match.
type Session struct {
	characterIndex    uint32
	characters        [Size]Character
	deployCooldownEnd time.Time
	isGameOver        bool
	isRestartReserved bool
}

type Snapshot struct {
	State             State
	DeployCooldownEnd time.Time
	IsRestartReserved bool
}

func New(characters [Size]Character, characterIndex uint32) (*Session, error) {
	if characterIndex >= Size {
		return nil, errors.New("character index out of range")
	}
	isAvailable := false
	for index, character := range characters {
		if character.HitPoints < 0 || math.IsNaN(float64(character.HitPoints)) || math.IsInf(float64(character.HitPoints), 0) {
			return nil, fmt.Errorf("character[%d]: invalid hit points", index)
		}
		if character.ManaPoints < 0 || math.IsNaN(float64(character.ManaPoints)) || math.IsInf(float64(character.ManaPoints), 0) {
			return nil, fmt.Errorf("character[%d]: invalid mana points", index)
		}
		isAvailable = isAvailable || character.IsAvailable
	}
	if !isAvailable {
		return nil, errors.New("no available character")
	}
	if !characters[characterIndex].IsAvailable {
		return nil, fmt.Errorf("deployed: %w", ErrCharacterUnavailable)
	}
	return &Session{characters: characters, characterIndex: characterIndex}, nil
}

// Restore rebuilds a squad from a safe checkpoint without carrying transient
// action or presentation state across the process boundary.
func Restore(state State) (*Session, error) {
	session, err := New(state.Characters, state.CharacterIndex)
	if err != nil {
		return nil, fmt.Errorf("squadRestore: %w", err)
	}
	if state.IsGameOver {
		return nil, errors.New("squadRestore: terminal squad")
	}
	return session, nil
}

func (s *Session) State() State {
	if s == nil {
		return State{}
	}
	return State{
		CharacterIndex: s.characterIndex,
		Characters:     s.characters,
		IsGameOver:     s.isGameOver,
	}
}

func (s *Session) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	return Snapshot{
		State: s.State(), DeployCooldownEnd: s.deployCooldownEnd,
		IsRestartReserved: s.isRestartReserved,
	}
}

func (s *Session) Character(index uint32) (Character, bool) {
	if s == nil || index >= Size {
		return Character{}, false
	}
	return s.characters[index], true
}

func (s *Session) DeployedIndex() uint32 {
	if s == nil {
		return 0
	}
	return s.characterIndex
}

func (s *Session) DeployedCharacter() (Character, bool) {
	if s == nil {
		return Character{}, false
	}
	return s.Character(s.characterIndex)
}

func (s *Session) SetAvailable(index uint32, hitPoints float32) error {
	if s == nil {
		return errors.New("nil squad")
	}
	if s.isGameOver {
		return ErrGameOver
	}
	if index >= Size {
		return errors.New("character index out of range")
	}
	if hitPoints < 0 || math.IsNaN(float64(hitPoints)) || math.IsInf(float64(hitPoints), 0) {
		return errors.New("invalid hit points")
	}
	s.characters[index] = Character{HitPoints: hitPoints, IsAvailable: true}
	return nil
}

func (s *Session) Deploy(index uint32) error {
	if s == nil {
		return errors.New("nil squad")
	}
	if s.isGameOver {
		return ErrGameOver
	}
	character, isFound := s.Character(index)
	if !isFound {
		return errors.New("character index out of range")
	}
	if !character.IsAvailable {
		return ErrCharacterUnavailable
	}
	if character.HitPoints <= 0 {
		return ErrCharacterDead
	}
	s.characterIndex = index
	return nil
}

func (s *Session) IsDeployReady(now time.Time) bool {
	return s != nil && !now.Before(s.deployCooldownEnd)
}

func (s *Session) DeployWithCooldown(
	index uint32, now time.Time, cooldown time.Duration,
) error {
	if s == nil {
		return errors.New("nil squad")
	}
	if now.IsZero() {
		return errors.New("invalid deployment time")
	}
	if cooldown < 0 {
		return errors.New("invalid deployment cooldown")
	}
	if !s.IsDeployReady(now) {
		return ErrDeployCooldown
	}
	err := s.Deploy(index)
	if err != nil {
		return fmt.Errorf("deploy: %w", err)
	}
	s.deployCooldownEnd = now.Add(cooldown)
	return nil
}

func (s *Session) ResetDeployCooldown() {
	if s == nil {
		return
	}
	s.deployCooldownEnd = time.Time{}
}

// SetHitPoints updates one match-local character and returns true only when the
// update newly latches the all-available-characters-dead condition.
func (s *Session) SetHitPoints(index uint32, hitPoints float32) (bool, error) {
	if s == nil {
		return false, errors.New("nil squad")
	}
	if index >= Size {
		return false, errors.New("character index out of range")
	}
	if hitPoints < 0 || math.IsNaN(float64(hitPoints)) || math.IsInf(float64(hitPoints), 0) {
		return false, errors.New("invalid hit points")
	}
	// Terminal damage callbacks are idempotent. In particular, a duplicate
	// zero-HP continuation must neither publish another game-over transition nor
	// revive a character by mutating the already failed match aggregate.
	if s.isGameOver {
		return false, nil
	}
	character := s.characters[index]
	if !character.IsAvailable {
		return false, ErrCharacterUnavailable
	}
	character.HitPoints = hitPoints
	s.characters[index] = character
	if s.isGameOver || s.LivingCount() != 0 {
		return false, nil
	}
	s.isGameOver = true
	return true, nil
}

func (s *Session) SetManaPoints(index uint32, manaPoints float32) error {
	if s == nil {
		return errors.New("nil squad")
	}
	if s.isGameOver {
		return ErrGameOver
	}
	if index >= Size {
		return errors.New("character index out of range")
	}
	if manaPoints < 0 || math.IsNaN(float64(manaPoints)) || math.IsInf(float64(manaPoints), 0) {
		return errors.New("invalid mana points")
	}
	character := s.characters[index]
	if !character.IsAvailable {
		return ErrCharacterUnavailable
	}
	character.ManaPoints = manaPoints
	s.characters[index] = character
	return nil
}

func (s *Session) LivingCount() int {
	if s == nil {
		return 0
	}
	living := 0
	for _, character := range s.characters {
		if character.IsAvailable && character.HitPoints > 0 {
			living++
		}
	}
	return living
}

func (s *Session) IsGameOver() bool {
	return s != nil && s.isGameOver
}

// ReserveRestart accepts one durable reset attempt for a terminal squad. The
// caller must roll the reservation back when persistence fails; a successful
// reset replaces the complete match aggregate instead of reopening this one.
func (s *Session) ReserveRestart() bool {
	if s == nil || !s.isGameOver || s.isRestartReserved {
		return false
	}
	s.isRestartReserved = true
	return true
}

// RollbackRestart makes a failed durable reset retryable without clearing the
// terminal squad latch.
func (s *Session) RollbackRestart() {
	if s == nil {
		return
	}
	s.isRestartReserved = false
}

func (s *Session) IsRestartReserved() bool {
	return s != nil && s.isRestartReserved
}
