package sim

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/darkspinnet/darkspin/server/util"
)

// SummonPassiveDefinition is the rank-one authored projection of a companion-
// maintaining passive modifier.
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
	maximumCompanion, err := luaAbilityRankNumber(table, "maxSaplings", 1)
	if err != nil {
		return SummonPassiveDefinition{}, fmt.Errorf("maximumCompanion: %w", err)
	}
	if maximumCompanion <= 0 || maximumCompanion > float64(^uint32(0)) ||
		float64(uint32(maximumCompanion)) != maximumCompanion {
		return SummonPassiveDefinition{}, errors.New("maximumCompanion: invalid")
	}
	definition.MaximumCompanion = uint32(maximumCompanion)
	spawnDelay, err := luaAbilityRankNumber(table, "spawnDelay", 1)
	if err != nil {
		return SummonPassiveDefinition{}, fmt.Errorf("spawnDelay: %w", err)
	}
	definition.SpawnDelay = luaSeconds(spawnDelay)
	respawnDelay, err := luaAbilityRankNumber(table, "respawnDelay", 1)
	if err != nil {
		return SummonPassiveDefinition{}, fmt.Errorf("respawnDelay: %w", err)
	}
	definition.RespawnDelay = luaSeconds(respawnDelay)
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
	tasks         map[Role]TaskID
	isAliveByRole map[Role]bool
	isStopped     bool
}

func StartSummonPassive(
	simulator *Simulator, scope CancelScope, input SummonPassiveInput,
) (*SummonPassiveBehavior, error) {
	definition := input.Definition
	if simulator == nil || !simulator.isScopeActive(scope) {
		return nil, errors.New("invalid summon passive simulator")
	}
	if input.OwnerRole == "" || definition.Name == "" || definition.CompanionNoun == "" ||
		definition.MaximumCompanion == 0 || len(input.CompanionRoles) != int(definition.MaximumCompanion) ||
		definition.SpawnDelay < 0 || definition.RespawnDelay <= 0 || definition.SpawnRadius <= 0 ||
		definition.OwnerDistance <= 0 || definition.SpawnEffectID == 0 || definition.SpawnAbilityID == 0 ||
		definition.BurrowModifierID == 0 {
		return nil, fmt.Errorf("summon passive input: %#v", input)
	}
	behavior := &SummonPassiveBehavior{
		simulator: simulator, scope: scope, input: input,
		tasks:         make(map[Role]TaskID, len(input.CompanionRoles)),
		isAliveByRole: make(map[Role]bool, len(input.CompanionRoles)),
	}
	seen := make(map[Role]struct{}, len(input.CompanionRoles))
	for index, role := range input.CompanionRoles {
		if role == "" {
			return nil, fmt.Errorf("companionRole[%d]: empty", index)
		}
		if _, isDuplicate := seen[role]; isDuplicate {
			return nil, fmt.Errorf("companionRole[%d]: duplicate", index)
		}
		seen[role] = struct{}{}
	}
	for index, role := range input.CompanionRoles {
		err := behavior.schedule(role, definition.SpawnDelay)
		if err != nil {
			for _, taskID := range behavior.tasks {
				simulator.Cancel(taskID)
			}
			return nil, fmt.Errorf("initialSchedule[%d]: %w", index, err)
		}
	}
	return behavior, nil
}

func (b *SummonPassiveBehavior) CompanionDied(role Role) error {
	if b == nil || b.simulator == nil || b.isStopped || !b.isAliveByRole[role] {
		return errors.New("companion death unavailable")
	}
	b.isAliveByRole[role] = false
	err := b.schedule(role, b.input.Definition.RespawnDelay)
	if err != nil {
		return fmt.Errorf("respawnSchedule: %w", err)
	}
	return nil
}

// RespawnCompanion executes one externally elapsed respawn deadline without
// advancing unrelated companion slots. Gameplay owns the wall-clock timer;
// the simulator still owns the resulting authored spawn intent.
func (b *SummonPassiveBehavior) RespawnCompanion(role Role) error {
	if b == nil || b.simulator == nil || b.isStopped || b.isAliveByRole[role] {
		return errors.New("companion respawn unavailable")
	}
	taskID, isScheduled := b.tasks[role]
	if !isScheduled {
		return errors.New("companion respawn not scheduled")
	}
	b.simulator.Cancel(taskID)
	delete(b.tasks, role)
	err := b.spawn(role)
	if err != nil {
		return fmt.Errorf("respawnCompanion: %w", err)
	}
	return nil
}

// CompanionSeparated applies chunk 270's owner-distance branch: the live
// companion is marked for deletion and its stable slot enters the ordinary
// respawn delay.
func (b *SummonPassiveBehavior) CompanionSeparated(role Role) error {
	if b == nil || b.simulator == nil || b.isStopped || !b.isAliveByRole[role] {
		return errors.New("companion separation unavailable")
	}
	err := b.simulator.EmitScoped(
		DespawnIntent{Role: role}, b.input.Definition.Provenance, b.scope,
	)
	if err != nil {
		return fmt.Errorf("separationDespawn: %w", err)
	}
	b.isAliveByRole[role] = false
	err = b.schedule(role, b.input.Definition.RespawnDelay)
	if err != nil {
		return fmt.Errorf("separationRespawn: %w", err)
	}
	return nil
}

func (b *SummonPassiveBehavior) spawn(role Role) error {
	if b.isStopped || b.isAliveByRole[role] {
		return nil
	}
	delete(b.tasks, role)
	definition := b.input.Definition
	err := b.simulator.EmitScoped(CompanionSpawnIntent{
		Role: role, OwnerRole: b.input.OwnerRole, NounName: definition.CompanionNoun,
		SpawnRadius: definition.SpawnRadius, OwnerDistance: definition.OwnerDistance,
		SpawnEffectID: definition.SpawnEffectID, SpawnAbilityID: definition.SpawnAbilityID,
		BurrowModifierID: definition.BurrowModifierID,
	}, definition.Provenance, b.scope)
	if err != nil {
		return fmt.Errorf("spawnEmit: %w", err)
	}
	b.isAliveByRole[role] = true
	return nil
}

func (b *SummonPassiveBehavior) schedule(role Role, delay time.Duration) error {
	if _, isScheduled := b.tasks[role]; isScheduled {
		return errors.New("companion already scheduled")
	}
	taskID, err := b.simulator.Schedule(delay, b.scope, func(*Simulator) error {
		err := b.spawn(role)
		if err != nil {
			return fmt.Errorf("summonContinue[%s]: %w", role, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("simSchedule: %w", err)
	}
	b.tasks[role] = taskID
	return nil
}

func (b *SummonPassiveBehavior) Stop() error {
	if b == nil || b.simulator == nil || b.isStopped {
		return nil
	}
	for _, taskID := range b.tasks {
		b.simulator.Cancel(taskID)
	}
	b.tasks = nil
	for _, role := range b.input.CompanionRoles {
		if !b.isAliveByRole[role] {
			continue
		}
		err := b.simulator.EmitScoped(DespawnIntent{Role: role}, b.input.Definition.Provenance, b.scope)
		if err != nil {
			return fmt.Errorf("despawnEmit[%s]: %w", role, err)
		}
		b.isAliveByRole[role] = false
	}
	b.isStopped = true
	return nil
}
