package game

import (
	"sync"

	"github.com/darkspinnet/darkspin/server/sporenet"
	"github.com/darkspinnet/darkspin/server/util"
)

type Character struct {
	Creature   *sporenet.Creature
	Object     *Object
	IsDeployed bool
	IsDead     bool
}

type Player struct {
	mu            sync.RWMutex
	User          *sporenet.User
	Index         uint8
	Status        uint32
	Progress      float32
	Characters    [3]*Character
	DeployedIndex uint32
	Catalysts     [9]Catalyst
	UpdateBits    uint32
}

func NewPlayer(user *sporenet.User, index uint8, objects *ObjectManager) *Player {
	player := &Player{User: user, Index: index}
	if user == nil {
		return player
	}
	for slot := range player.Characters {
		if slot >= len(user.Creatures) {
			break
		}
		creature := user.Creatures[slot]
		object := objects.Create(creature.Noun())
		object.IsPlayerControlled = true
		object.PlayerIndex = index
		object.Team = index
		player.Characters[slot] = &Character{Creature: creature, Object: object, IsDeployed: slot == 0}
	}
	return player
}

func (p *Player) Character(index uint32) *Character {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if index >= uint32(len(p.Characters)) {
		return nil
	}
	return p.Characters[index]
}
func (p *Player) DeployedCharacter() *Character { return p.Character(p.DeployedIndex) }
func (p *Player) Deploy(index uint32) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index >= uint32(len(p.Characters)) || p.Characters[index] == nil || p.Characters[index].IsDead {
		return false
	}
	if current := p.Characters[p.DeployedIndex]; current != nil {
		current.IsDeployed = false
	}
	p.DeployedIndex = index
	p.Characters[index].IsDeployed = true
	p.UpdateBits |= 1 << index
	return true
}
func (p *Player) SetStatus(status uint32, progress float32) {
	p.mu.Lock()
	p.Status = status
	p.Progress = progress
	p.mu.Unlock()
}
func (p *Player) SetCatalyst(slot uint32, catalyst Catalyst) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if slot >= uint32(len(p.Catalysts)) {
		return false
	}
	p.Catalysts[slot] = catalyst
	p.UpdateBits |= 1 << (slot + 3)
	return true
}

type Party struct {
	mu      sync.RWMutex
	ID      uint32
	players map[int64]*Player
}

func NewParty(id uint32) *Party { return &Party{ID: id, players: make(map[int64]*Player)} }
func (p *Party) Add(player *Player) {
	if player == nil || player.User == nil {
		return
	}
	p.mu.Lock()
	p.players[player.User.Account.ID] = player
	p.mu.Unlock()
}
func (p *Party) Remove(id int64) { p.mu.Lock(); delete(p.players, id); p.mu.Unlock() }
func (p *Party) Players() []*Player {
	p.mu.RLock()
	values := make([]*Player, 0, len(p.players))
	for _, player := range p.players {
		values = append(values, player)
	}
	p.mu.RUnlock()
	return values
}

type PlanetData struct {
	Kills, Unknown                                         uint32
	DamageDealt, DamageTaken, HealingDone, HealingReceived float32
	PlayerIndex                                            uint8
}
type ChainVoteData struct {
	PlanetData                                [4]PlanetData
	Summary                                   [4]PlanetData
	EnemyNouns                                [6]uint32
	LevelNouns                                [2]uint32
	TimeRemaining                             float32
	Level, LevelIndex, StarLevel, PlayerAsset uint32
	Progression                               uint8
	IsCompleted                               bool
}

func (c *ChainVoteData) SetEnemyNoun(index uint32, name string) bool {
	if index >= uint32(len(c.EnemyNouns)) {
		return false
	}
	c.EnemyNouns[index] = util.HashID(name)
	return true
}
func (c *ChainVoteData) DifficultyName() string {
	return "difficulty_" + string(rune('0'+min(c.StarLevel, 9)))
}

type ClientEventID uint32
type ServerEvent struct {
	Asset, ObjectID, SecondaryObjectID, AttackerID              uint32
	Position, Facing, TargetPoint                               Vec3
	EffectIndex                                                 uint8
	IsRemovalRequested, IsHardStop, IsForceAttached, IsCritical bool
}
type ClientEvent struct {
	ID       ClientEventID
	ObjectID uint32
	Text     string
	Value    int32
}
type CombatEvent struct {
	Flags                         uint32
	DeltaHealth, AbsorbedAmount   float32
	TargetID, SourceID, AbilityID uint32
	DamageDirection               Vec3
	IntegerHPChange               int32
}
