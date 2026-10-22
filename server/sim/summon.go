package sim

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/darkspinnet/darkspin/server/util"
)

// SummonPassiveDefinition retains the authored ranks of a companion-maintaining
// passive. The scalar fields are the selected rank's projection.
type SummonPassiveDefinition struct {
	Name             string
	CompanionNoun    string
	MaximumCompanion uint32
	SpawnDelay       time.Duration
	RespawnDelay     time.Duration
	SpawnRadius      float32
	OwnerDistance    float32
	SpawnEffectID    uint32
	SpawnAbilityID   uint32
	BurrowModifierID uint32
	Provenance       Provenance
	Ranks            []SummonPassiveRank
}

type SummonPassiveRank struct {
	MaximumCompanion uint32
	SpawnDelay       time.Duration
	RespawnDelay     time.Duration
}

func (e SummonPassiveDefinition) AtRank(rank uint32) SummonPassiveDefinition {
	if rank == 0 || int(rank) > len(e.Ranks) {
		return e
	}
	profile := e.Ranks[rank-1]
	e.MaximumCompanion = profile.MaximumCompanion
	e.SpawnDelay = profile.SpawnDelay
	e.RespawnDelay = profile.RespawnDelay
	return e
}

func summonPassiveDefinitionFromLua(
	table *luaTable, provenance Provenance,
) (SummonPassiveDefinition, error) {
	if table == nil || provenance.FunctionName == "" {
		return SummonPassiveDefinition{}, errors.New("invalid summon passive registration")
	}
	definition := SummonPassiveDefinition{Name: provenance.FunctionName, Provenance: provenance}
	var err error
	definition.CompanionNoun, err = luaAbilityString(table, "meleeDude")
	if err != nil {
		return SummonPassiveDefinition{}, fmt.Errorf("companionNoun: %w", err)
	}
	for rank := 1; rank <= 4; rank++ {
		maximum, rankErr := luaAbilityRankNumber(table, "maxSaplings", rank)
		if rankErr != nil {
			return SummonPassiveDefinition{}, fmt.Errorf("maximumRank[%d]: %w", rank, rankErr)
		}
		spawn, rankErr := luaAbilityRankNumber(table, "spawnDelay", rank)
		if rankErr != nil {
			return SummonPassiveDefinition{}, fmt.Errorf("spawnRank[%d]: %w", rank, rankErr)
		}
		respawn, rankErr := luaAbilityRankNumber(table, "respawnDelay", rank)
		if rankErr != nil {
			return SummonPassiveDefinition{}, fmt.Errorf("respawnRank[%d]: %w", rank, rankErr)
		}
		if maximum <= 0 || maximum > float64(^uint32(0)) ||
			float64(uint32(maximum)) != maximum || spawn < 0 || respawn <= 0 {
			return SummonPassiveDefinition{}, fmt.Errorf("summonRank[%d]: invalid", rank)
		}
		definition.Ranks = append(definition.Ranks, SummonPassiveRank{
			MaximumCompanion: uint32(maximum), SpawnDelay: luaSeconds(spawn),
			RespawnDelay: luaSeconds(respawn),
		})
	}
	definition = definition.AtRank(1)
	definition.SpawnRadius, err = luaAbilityRankFloat32(table, "spawnRadius", 1)
	if err != nil {
		return SummonPassiveDefinition{}, fmt.Errorf("spawnRadius: %w", err)
	}
	definition.OwnerDistance, err = luaAbilityRankFloat32(table, "ownerDistance", 1)
	if err != nil {
		return SummonPassiveDefinition{}, fmt.Errorf("ownerDistance: %w", err)
	}
	definition.SpawnEffectID, err = luaAbilityUint32(table, "spawnFX")
	if err != nil {
		return SummonPassiveDefinition{}, fmt.Errorf("spawnEffect: %w", err)
	}
	definition.SpawnAbilityID, err = luaAbilityUint32(table, "spawnAbility")
	if err != nil {
		return SummonPassiveDefinition{}, fmt.Errorf("spawnAbility: %w", err)
	}
	definition.BurrowModifierID, err = luaAbilityUint32(table, "burrowModifer")
	if err != nil {
		return SummonPassiveDefinition{}, fmt.Errorf("burrowModifier: %w", err)
	}
	return definition, nil
}

func luaAbilityUint32(table *luaTable, key string) (uint32, error) {
	field := table.get(stringLuaKey(key))
	if field.kind == luaNumber && field.number > 0 && field.number <= float64(^uint32(0)) &&
		float64(uint32(field.number)) == field.number {
		return uint32(field.number), nil
	}
	if field.kind != luaString || field.text == "" {
		return 0, fmt.Errorf("fieldKind: %s", field.kind)
	}
	assetID, err := strconv.ParseUint(field.text, 0, 32)
	if err == nil {
		return uint32(assetID), nil
	}
	var numberErr *strconv.NumError
	if errors.As(err, &numberErr) && errors.Is(numberErr.Err, strconv.ErrRange) {
		return 0, fmt.Errorf("assetParse: %w", err)
	}
	return util.HashID(field.text), nil
}

type SummonPassiveInput struct {
	Definition     SummonPassiveDefinition
	OwnerRole      Role
	CompanionRoles []Role
}

type SummonPassiveBehavior struct {
	simulator     *Simulator
	scope         CancelScope
	input         SummonPassiveInput
	isAliveByRole map[Role]bool
	elapsed       time.Duration
	refillDelay   time.Duration
	isFull        bool
	isStopped     bool
}

func StartSummonPassive(simulator *Simulator, scope CancelScope, req SummonPassiveInput) (*SummonPassiveBehavior, error) {
	definition := req.Definition
	if simulator == nil || !simulator.isScopeActive(scope) {
		return nil, errors.New("invalid summon passive simulator")
	}
	if req.OwnerRole == "" || definition.Name == "" || definition.CompanionNoun == "" ||
		definition.MaximumCompanion == 0 || len(req.CompanionRoles) != int(definition.MaximumCompanion) ||
		definition.SpawnDelay < 0 || definition.RespawnDelay <= 0 || definition.SpawnRadius <= 0 ||
		definition.OwnerDistance <= 0 || definition.SpawnEffectID == 0 || definition.SpawnAbilityID == 0 ||
		definition.BurrowModifierID == 0 {
		return nil, fmt.Errorf("summon passive input: %#v", req)
	}
	behavior := &SummonPassiveBehavior{
		simulator: simulator, scope: scope, input: req,
		isAliveByRole: make(map[Role]bool, len(req.CompanionRoles)),
		refillDelay:   definition.SpawnDelay,
	}
	for index, role := range req.CompanionRoles {
		if role == "" {
			return nil, fmt.Errorf("companionRole[%d]: empty", index)
		}
		if _, isDuplicate := behavior.isAliveByRole[role]; isDuplicate {
			return nil, fmt.Errorf("companionRole[%d]: duplicate", index)
		}
		behavior.isAliveByRole[role] = false
	}
	return behavior, nil
}

func (e *SummonPassiveBehavior) IsAlive(role Role) bool {
	return e != nil && !e.isStopped && e.isAliveByRole[role]
}

// Tick is chunk 270's shared refill accumulator. Death does not reset it or
// clear the full-population latch; every empty slot joins the same refill.
func (e *SummonPassiveBehavior) Tick(delta time.Duration) error {
	if e == nil || e.simulator == nil || e.isStopped || delta < 0 {
		return errors.New("summon passive tick unavailable")
	}
	liveCount := 0
	for _, isAlive := range e.isAliveByRole {
		if isAlive {
			liveCount++
		}
	}
	if liveCount == int(e.input.Definition.MaximumCompanion) {
		if !e.isFull {
			e.refillDelay = e.input.Definition.RespawnDelay
			e.isFull = true
		}
		e.elapsed = 0
		return nil
	}
	e.elapsed += delta
	// The original branch is strictly delay < elapsed.
	if e.elapsed <= e.refillDelay {
		return nil
	}
	for _, role := range e.input.CompanionRoles {
		if e.isAliveByRole[role] {
			continue
		}
		err := e.spawn(role)
		if err != nil {
			return fmt.Errorf("refillSpawn: %w", err)
		}
	}
	e.elapsed = 0
	return nil
}

func (e *SummonPassiveBehavior) CompanionDied(role Role) error {
	if !e.IsAlive(role) {
		return errors.New("companion death unavailable")
	}
	e.isAliveByRole[role] = false
	return nil
}

// CompanionSeparated mirrors the direct deletion branch: it resets both the
// selected delay and accumulator to spawnDelay, so the next positive dt refills.
func (e *SummonPassiveBehavior) CompanionSeparated(role Role) error {
	if !e.IsAlive(role) {
		return errors.New("companion separation unavailable")
	}
	err := e.simulator.EmitScoped(DespawnIntent{Role: role}, e.input.Definition.Provenance, e.scope)
	if err != nil {
		return fmt.Errorf("separationDespawn: %w", err)
	}
	e.isAliveByRole[role] = false
	e.refillDelay = e.input.Definition.SpawnDelay
	e.elapsed = e.refillDelay
	e.isFull = false
	return nil
}

func (e *SummonPassiveBehavior) spawn(role Role) error {
	definition := e.input.Definition
	err := e.simulator.EmitScoped(CompanionSpawnIntent{
		Role: role, OwnerRole: e.input.OwnerRole, NounName: definition.CompanionNoun,
		SpawnRadius: definition.SpawnRadius, OwnerDistance: definition.OwnerDistance,
		SpawnEffectID: definition.SpawnEffectID, SpawnAbilityID: definition.SpawnAbilityID,
		BurrowModifierID: definition.BurrowModifierID,
	}, definition.Provenance, e.scope)
	if err != nil {
		return fmt.Errorf("spawnEmit: %w", err)
	}
	e.isAliveByRole[role] = true
	return nil
}

func (e *SummonPassiveBehavior) Stop() error {
	if e == nil || e.simulator == nil || e.isStopped {
		return nil
	}
	for _, role := range e.input.CompanionRoles {
		if !e.isAliveByRole[role] {
			continue
		}
		err := e.simulator.EmitScoped(DespawnIntent{Role: role}, e.input.Definition.Provenance, e.scope)
		if err != nil {
			return fmt.Errorf("despawnEmit[%s]: %w", role, err)
		}
		e.isAliveByRole[role] = false
	}
	e.isStopped = true
	return nil
}
