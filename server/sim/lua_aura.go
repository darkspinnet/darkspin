package sim

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

// One bounded scan may run both exit and entry callbacks for 256 occupants.
const auraLuaInstructionBudget = 4 * defaultLuaInstructionBudget

type LuaAuraInput struct {
	Root       LuaBytecode
	OwnerRole  Role
	ModifierID uint32
	Rank       uint32
}

type LuaAuraModifierRequest struct {
	TargetRole Role
	OwnerRole  Role
	ModifierID uint32
	Rank       uint32
}

type LuaAuraModifierDelete struct {
	TargetRole Role
	InstanceID uint32
}

// LuaAuraWorld applies each accepted mutation synchronously, returning the
// actual modifier handle (including rejected handle zero) to authored Lua.
// Implementations own an accepted-handle ledger for cleanup after any error.
type LuaAuraWorld interface {
	LuaWorldQuery
	RequestAuraModifier(req LuaAuraModifierRequest) (uint32, error)
	DeleteAuraModifier(req LuaAuraModifierDelete) error
}

// LuaAuraRuntime retains the packaged SimpleAuraFns closures and private
// membership table. Go owns trigger occupancy, timing and mutation authority;
// the original Lua owns duplicate/rejected membership and modifier cleanup.
type LuaAuraRuntime struct {
	mutex    sync.Mutex
	input    LuaAuraInput
	compiler *luaCompiler
	world    LuaAuraWorld
	enter    *luaClosure
	exit     *luaClosure
	snapshot *luaTable
	roles    []Role
}

func NewLuaAuraRuntime(req LuaAuraInput) (runtime *LuaAuraRuntime, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			runtime = nil
			err = fmt.Errorf("auraPanic: %v", recovered)
		}
	}()
	if req.OwnerRole == "" || req.ModifierID == 0 || req.Rank == 0 {
		return nil, errors.New("aura input invalid")
	}
	compiler, err := newLuaCompiler(LuaAbilityInput{
		Root: req.Root, Role: req.OwnerRole,
		preloadedModules: map[string]bool{"Lua!GlobalDefinitions.lua": true},
	})
	if err != nil {
		return nil, fmt.Errorf("auraCompiler: %w", err)
	}
	err = compiler.executeChunk(compiler.moduleChunks["$root"])
	if err != nil {
		return nil, fmt.Errorf("auraRoot: %w", err)
	}
	runtime = &LuaAuraRuntime{input: req, compiler: compiler}
	err = runtime.bind()
	if err != nil {
		return nil, fmt.Errorf("auraBind: %w", err)
	}
	return runtime, nil
}

func (e *LuaAuraRuntime) bind() error {
	compiler := e.compiler
	privateTable, err := compiler.newRuntimeTable(nil)
	if err != nil {
		return fmt.Errorf("auraPrivate: %w", err)
	}
	compiler.privateTable = privateTable
	e.snapshot, err = compiler.newRuntimeTable(nil)
	if err != nil {
		return fmt.Errorf("auraSnapshot: %w", err)
	}
	object := compiler.global.get(stringLuaKey("nGameObject"))
	modifier := compiler.global.get(stringLuaKey("nModifier"))
	if object.kind != luaTableValue || modifier.kind != luaTableValue {
		return errors.New("aura native table unavailable")
	}
	compiler.setNative(object.table, "GetAttributeSnapshot", e.attributeSnapshot)
	compiler.setNative(object.table, "ValidateHostileTarget", e.hostileTarget)
	compiler.setNative(modifier.table, "RequestModifier", e.requestModifier)
	compiler.setNative(modifier.table, "MarkForDelete", e.deleteModifier)
	compiler.setNative(compiler.global, "pairs", e.pairs)
	return nil
}

// Scan samples current engine trigger occupancy and invokes the original
// closures. No activation, closure or private table is reconstructed per scan.
func (e *LuaAuraRuntime) Scan(world LuaAuraWorld, req LuaRadiusQuery) (err error) {
	if e == nil {
		return errors.New("nil aura runtime")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	defer e.finishCall(&err)
	if e.compiler == nil || world == nil {
		return errors.New("aura runtime unavailable")
	}
	if !isLuaQueryPosition(req.Center) || req.Radius < 0 ||
		math.IsNaN(float64(req.Radius)) || math.IsInf(float64(req.Radius), 0) ||
		!req.IsOrganicDamageableOnly {
		return errors.New("aura radius invalid")
	}
	e.world = world
	e.compiler.input.Query = world
	e.compiler.instructionBudget = auraLuaInstructionBudget
	if e.enter == nil {
		err = e.initialize()
		if err != nil {
			return fmt.Errorf("auraInitialize: %w", err)
		}
	}
	err = e.compiler.chargeWorldQuery()
	if err != nil {
		return fmt.Errorf("auraScanBudget: %w", err)
	}
	objects, err := world.ObjectsInRadius(req)
	if err != nil {
		return fmt.Errorf("auraScan: %w", err)
	}
	if len(objects) > maxLuaRadiusObjects {
		return errors.New("aura object limit exceeded")
	}
	roles := make([]Role, 0, len(objects))
	rolesByName := make(map[Role]struct{}, len(objects))
	for _, object := range objects {
		if object.Role == "" || !isLuaQueryPosition(object.Position) {
			return errors.New("aura candidate invalid")
		}
		if _, isDuplicate := rolesByName[object.Role]; isDuplicate {
			return errors.New("aura candidate duplicate")
		}
		rolesByName[object.Role] = struct{}{}
		roles = append(roles, object.Role)
	}
	previousRolesByName := make(map[Role]struct{}, len(e.roles))
	for _, role := range e.roles {
		previousRolesByName[role] = struct{}{}
		if _, isInside := rolesByName[role]; isInside {
			continue
		}
		err = e.callContact(e.exit, role)
		if err != nil {
			return fmt.Errorf("auraExit: %w", err)
		}
	}
	e.reclaimDepartedFields()
	for _, role := range roles {
		if _, isOverlapping := previousRolesByName[role]; isOverlapping {
			continue
		}
		err = e.callContact(e.enter, role)
		if err != nil {
			return fmt.Errorf("auraEnter: %w", err)
		}
	}
	e.roles = roles
	return nil
}

// Lua nil removes a table entry. The interpreter keeps nil tombstones for
// simpler generic table writes; release those known aura slots here so a
// moving trigger's memory charge follows current membership, not its history.
func (e *LuaAuraRuntime) reclaimDepartedFields() {
	field := e.compiler.privateTable.get(stringLuaKey("auraEffectModifierIds"))
	if field.kind != luaTableValue || field.table == nil {
		return
	}
	for key, member := range field.table.fields {
		if member.kind != luaNil {
			continue
		}
		delete(field.table.fields, key)
		e.compiler.memoryBudget++
	}
}

func (e *LuaAuraRuntime) initialize() error {
	compiler := e.compiler
	isEnemy := compiler.global.get(stringLuaKey("IsEnemy"))
	simpleAura := compiler.global.get(stringLuaKey("SimpleAuraFns"))
	if isEnemy.kind != luaClosureValue || simpleAura.kind != luaClosureValue {
		return errors.New("aura helper missing")
	}
	predicates, err := compiler.callClosure(isEnemy.closure, []luaValue{stringLuaValue(string(e.input.OwnerRole))})
	if err != nil {
		return fmt.Errorf("auraPredicate: %w", err)
	}
	if len(predicates) != 1 || predicates[0].kind != luaClosureValue {
		return errors.New("aura predicate invalid")
	}
	callbacks, err := compiler.callClosure(simpleAura.closure, []luaValue{
		stringLuaValue(string(e.input.OwnerRole)), numberLuaValue(float64(e.input.ModifierID)),
		predicates[0], numberLuaValue(float64(e.input.Rank)),
	})
	if err != nil {
		return fmt.Errorf("auraFactory: %w", err)
	}
	if len(callbacks) != 2 || callbacks[0].kind != luaClosureValue || callbacks[1].kind != luaClosureValue {
		return errors.New("aura callbacks invalid")
	}
	e.enter = callbacks[0].closure
	e.exit = callbacks[1].closure
	return nil
}

func (e *LuaAuraRuntime) callContact(callback *luaClosure, role Role) error {
	results, err := e.compiler.callClosure(callback, []luaValue{numberLuaValue(1), stringLuaValue(string(role))})
	if err != nil {
		return fmt.Errorf("auraContact: %w", err)
	}
	if len(results) != 1 || results[0].kind != luaBoolean {
		return errors.New("aura contact result invalid")
	}
	return nil
}

// Close runs packaged CleanupAuras once, then releases the instance. The
// authority's handle ledger must still clean up after a failed callback.
func (e *LuaAuraRuntime) Close(world LuaAuraWorld) (err error) {
	if e == nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	defer e.stopLocked()
	defer e.finishCall(&err)
	if e.compiler == nil || e.enter == nil {
		return nil
	}
	if world == nil {
		return errors.New("aura cleanup world unavailable")
	}
	e.world = world
	e.compiler.input.Query = world
	e.compiler.instructionBudget = auraLuaInstructionBudget
	cleanup := e.compiler.global.get(stringLuaKey("CleanupAuras"))
	if cleanup.kind != luaClosureValue {
		return errors.New("aura cleanup missing")
	}
	_, err = e.compiler.callClosure(cleanup.closure, nil)
	if err != nil {
		return fmt.Errorf("auraCleanup: %w", err)
	}
	return nil
}

func (e *LuaAuraRuntime) finishCall(err *error) {
	if recovered := recover(); recovered != nil {
		*err = fmt.Errorf("auraPanic: %v", recovered)
	}
	if e.compiler != nil {
		e.compiler.input.Query = nil
	}
	e.world = nil
	if *err != nil {
		e.stopLocked()
	}
}

func (e *LuaAuraRuntime) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.stopLocked()
}

func (e *LuaAuraRuntime) stopLocked() {
	e.compiler = nil
	e.world = nil
	e.enter = nil
	e.exit = nil
	e.snapshot = nil
	e.roles = nil
}

func (e *LuaAuraRuntime) attributeSnapshot(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 1 || arguments[0].kind != luaString || arguments[0].text != string(e.input.OwnerRole) {
		return nil, errors.New("aura snapshot arguments")
	}
	return []luaValue{tableLuaValue(e.snapshot)}, nil
}

func (e *LuaAuraRuntime) hostileTarget(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 2 || arguments[0].kind != luaNumber || arguments[1].kind != luaString {
		return nil, errors.New("aura hostile arguments")
	}
	object, err := e.compiler.queryObject(Role(arguments[1].text))
	if err != nil {
		return nil, fmt.Errorf("auraHostileObject: %w", err)
	}
	return []luaValue{booleanLuaValue(object.IsAlive && float64(object.Team) != arguments[0].number)}, nil
}

func (e *LuaAuraRuntime) requestModifier(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 5 || arguments[0].kind != luaString ||
		arguments[1].kind != luaString || arguments[1].text != string(e.input.OwnerRole) ||
		arguments[2].kind != luaNumber || arguments[2].number != float64(e.input.ModifierID) ||
		arguments[3].kind != luaTableValue || arguments[3].table != e.snapshot ||
		arguments[4].kind != luaNumber || arguments[4].number != float64(e.input.Rank) {
		return nil, errors.New("aura modifier arguments")
	}
	err := e.compiler.chargeWorldQuery()
	if err != nil {
		return nil, fmt.Errorf("auraRequestBudget: %w", err)
	}
	instanceID, err := e.world.RequestAuraModifier(LuaAuraModifierRequest{
		TargetRole: Role(arguments[0].text), OwnerRole: e.input.OwnerRole,
		ModifierID: e.input.ModifierID, Rank: e.input.Rank,
	})
	if err != nil {
		return nil, fmt.Errorf("auraRequest: %w", err)
	}
	return []luaValue{numberLuaValue(float64(instanceID))}, nil
}

func (e *LuaAuraRuntime) deleteModifier(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 2 || arguments[0].kind != luaString || arguments[1].kind != luaNumber ||
		arguments[1].number < 0 || arguments[1].number > float64(^uint32(0)) ||
		arguments[1].number != math.Trunc(arguments[1].number) {
		return nil, errors.New("aura delete arguments")
	}
	err := e.compiler.chargeWorldQuery()
	if err != nil {
		return nil, fmt.Errorf("auraDeleteBudget: %w", err)
	}
	err = e.world.DeleteAuraModifier(LuaAuraModifierDelete{
		TargetRole: Role(arguments[0].text), InstanceID: uint32(arguments[1].number),
	})
	if err != nil {
		return nil, fmt.Errorf("auraDelete: %w", err)
	}
	return nil, nil
}

func (e *LuaAuraRuntime) pairs(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 1 || arguments[0].kind != luaTableValue || arguments[0].table == nil {
		return nil, errors.New("aura pairs arguments")
	}
	return []luaValue{{kind: luaNativeValue, native: e.nextPair}, arguments[0], {kind: luaNil}}, nil
}

func (e *LuaAuraRuntime) nextPair(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 2 || arguments[0].kind != luaTableValue ||
		(arguments[1].kind != luaNil && arguments[1].kind != luaString) {
		return nil, errors.New("aura iterator arguments")
	}
	nextKey := ""
	isFound := false
	for key, field := range arguments[0].table.fields {
		if field.kind == luaNil {
			continue
		}
		if key.kind != luaString {
			return nil, errors.New("aura iterator key invalid")
		}
		if arguments[1].kind != luaNil && key.text <= arguments[1].text {
			continue
		}
		if !isFound || key.text < nextKey {
			nextKey = key.text
			isFound = true
		}
	}
	if !isFound {
		return []luaValue{{kind: luaNil}}, nil
	}
	key := stringLuaValue(nextKey)
	return []luaValue{key, arguments[0].table.get(valueLuaKey(key))}, nil
}
