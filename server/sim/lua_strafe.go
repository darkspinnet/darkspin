package sim

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

// LuaStrafeProjection resolves the native closest-position query without
// changing the actor. The caller owns navigation and movement authority.
type LuaStrafeProjection interface {
	ClosestPosition(role Role, position Position) (Position, error)
}

type LuaStrafeInput struct {
	Behavior        string
	Root            LuaBytecode
	Modules         map[string]LuaBytecode
	Random          *SimulatorRandom
	Role            Role
	TargetRole      Role
	Position        Position
	TargetPosition  Position
	FootprintRadius float32
	Projection      LuaStrafeProjection
}

type LuaStrafePlan struct {
	Destination       Position
	TargetRole        Role
	MovementSpeedBuff float32
	NearGoalDistance  float32
	IsMoveRequested   bool
	IsWaiting         bool
}

type LuaStrafeCleanup struct {
	IsStopRequested          bool
	RemovedMovementSpeedBuff float32
}

// LuaStrafeRuntime retains Strafe's actual private table, float slots
// and attribute handle until authority finishes or interrupts its near-goal
// wait. Tick ends immediately after that wait; no coroutine replay is needed.
type LuaStrafeRuntime struct {
	mutex       sync.Mutex
	compiler    *luaCompiler
	input       LuaStrafeInput
	callbacks   [3]*luaClosure
	floats      map[int]float64
	plan        LuaStrafePlan
	cleanup     LuaStrafeCleanup
	attributeID uint32
	isActive    bool
}

func NewLuaStrafeRuntime(req LuaStrafeInput) (runtime *LuaStrafeRuntime, plan LuaStrafePlan, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			runtime, plan = nil, LuaStrafePlan{}
			err = fmt.Errorf("strafePanic: %v", recovered)
		}
	}()
	if req.Behavior == "" {
		req.Behavior = "nBehavior_Strafe"
	}
	if req.Behavior != "nBehavior_Strafe" && req.Behavior != "nBehavior_SmallStrafe" {
		return nil, LuaStrafePlan{}, errors.New("unsupported strafe behavior")
	}
	if req.Role == "" || req.Random == nil || req.Projection == nil ||
		!isLuaStrafePosition(req.Position) || !isLuaStrafePosition(req.TargetPosition) ||
		!isLuaStrafeNumber(float64(req.FootprintRadius)) || req.FootprintRadius < 0 {
		return nil, LuaStrafePlan{}, errors.New("invalid strafe input")
	}
	compiler, err := newLuaCompiler(LuaAbilityInput{
		Root: req.Root, Modules: req.Modules, Random: req.Random, Role: req.Role,
	})
	if err != nil {
		return nil, LuaStrafePlan{}, fmt.Errorf("strafeCompiler: %w", err)
	}
	runtime = &LuaStrafeRuntime{compiler: compiler, input: req, floats: make(map[int]float64)}
	err = runtime.bind()
	if err != nil {
		return nil, LuaStrafePlan{}, fmt.Errorf("strafeBind: %w", err)
	}
	err = compiler.executeChunk(compiler.moduleChunks["$root"])
	if err != nil {
		return nil, LuaStrafePlan{}, fmt.Errorf("strafeRoot: %w", err)
	}
	behavior := compiler.global.get(stringLuaKey(req.Behavior))
	if behavior.kind != luaTableValue || behavior.table == nil {
		return nil, LuaStrafePlan{}, errors.New("strafe behavior missing")
	}
	for index := range runtime.callbacks {
		callback := behavior.table.get(numberLuaKey(float64(index + 1)))
		if callback.kind != luaClosureValue || callback.closure == nil || callback.closure.prototype.ParameterCount != 0 {
			return nil, LuaStrafePlan{}, errors.New("strafe callback invalid")
		}
		runtime.callbacks[index] = callback.closure
	}
	err = runtime.call(0, "Activate")
	if err != nil {
		return nil, LuaStrafePlan{}, fmt.Errorf("strafeActivate: %w", err)
	}
	err = runtime.call(1, "Tick")
	if err != nil {
		return nil, LuaStrafePlan{}, fmt.Errorf("strafeTick: %w", err)
	}
	if runtime.attributeID == 0 || !runtime.plan.IsMoveRequested || !runtime.plan.IsWaiting {
		return nil, LuaStrafePlan{}, errors.New("strafe lifecycle incomplete")
	}
	runtime.isActive = true
	// Navigation and the input bytecode are no longer needed after Activate.
	runtime.input.Projection = nil
	runtime.input.Modules = nil
	runtime.input.Root = LuaBytecode{}
	return runtime, runtime.plan, nil
}

func (e *LuaStrafeRuntime) call(index int, name string) error {
	e.compiler.instructionBudget = defaultLuaInstructionBudget
	e.compiler.program.Provenance.FunctionName = e.input.Behavior + "." + name
	results, err := e.compiler.callClosure(e.callbacks[index], nil)
	if err != nil {
		return fmt.Errorf("strafeCallback: %w", err)
	}
	if len(results) != 0 || len(e.compiler.program.Steps) != 0 {
		return errors.New("unexpected strafe callback result")
	}
	return nil
}

// Deactivate executes the original Stop and exact attribute removal once.
// The returned cleanup must be committed by the movement owner.
func (e *LuaStrafeRuntime) Deactivate() (cleanup LuaStrafeCleanup, err error) {
	if e == nil {
		return LuaStrafeCleanup{}, errors.New("nil strafe runtime")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			cleanup = LuaStrafeCleanup{}
			err = fmt.Errorf("strafePanic: %v", recovered)
		}
		e.isActive = false
		e.compiler = nil
		e.callbacks = [3]*luaClosure{}
		e.floats = nil
	}()
	if !e.isActive || e.compiler == nil {
		return LuaStrafeCleanup{}, errors.New("strafe runtime stopped")
	}
	err = e.call(2, "Deactivate")
	if err != nil {
		return LuaStrafeCleanup{}, fmt.Errorf("strafeDeactivate: %w", err)
	}
	if e.attributeID != 0 || !e.cleanup.IsStopRequested {
		return LuaStrafeCleanup{}, errors.New("strafe cleanup incomplete")
	}
	return e.cleanup, nil
}

func (e *LuaStrafeRuntime) bind() error {
	bindings := []struct {
		table string
		name  string
		call  func([]luaValue) ([]luaValue, error)
	}{
		{"math", "sqrt", e.sqrt},
		{"nGameObject", "GetPosition", e.position},
		{"nGameObject", "GetFootprintRadius", e.footprint},
		{"nThreadData", "SetFloat", e.setFloat},
		{"nThreadData", "GetFloat", e.getFloat},
		{"nLocomotion", "GetClosestPosition", e.closestPosition},
		{"nLocomotion", "MoveToPointWhileFacingTarget", e.move},
		{"nLocomotion", "Stop", e.stop},
		{"nThread", "WaitForNearGoal", e.wait},
		{"nAttribute", "AddAttributeModifier", e.addAttribute},
		{"nAttribute", "RemoveAttributeModifier", e.removeAttribute},
	}
	for _, binding := range bindings {
		table := e.compiler.global.get(stringLuaKey(binding.table))
		if table.kind != luaTableValue || table.table == nil {
			return fmt.Errorf("strafeTable: %s", binding.table)
		}
		e.compiler.setNative(table.table, binding.name, binding.call)
	}
	agent := newLuaTable(nil)
	e.compiler.setNative(agent, "GetBestTarget", e.bestTarget)
	e.compiler.global.set(stringLuaKey("nAgent"), tableLuaValue(agent))
	return nil
}

func (e *LuaStrafeRuntime) bestTarget(arguments []luaValue) ([]luaValue, error) {
	if !e.isActor(arguments) {
		return nil, errors.New("strafe target arguments")
	}
	if e.input.TargetRole == "" {
		return []luaValue{numberLuaValue(0)}, nil
	}
	return []luaValue{stringLuaValue(string(e.input.TargetRole))}, nil
}

func (e *LuaStrafeRuntime) position(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 1 || arguments[0].kind != luaString {
		return nil, errors.New("strafe position arguments")
	}
	position := e.input.Position
	if arguments[0].text != string(e.input.Role) {
		if e.input.TargetRole == "" || arguments[0].text != string(e.input.TargetRole) {
			return nil, errors.New("strafe position role")
		}
		position = e.input.TargetPosition
	}
	return luaStrafeCoordinates(position), nil
}

func (e *LuaStrafeRuntime) footprint(arguments []luaValue) ([]luaValue, error) {
	if !e.isActor(arguments) {
		return nil, errors.New("strafe footprint arguments")
	}
	return []luaValue{numberLuaValue(float64(e.input.FootprintRadius))}, nil
}

func (e *LuaStrafeRuntime) sqrt(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 1 || arguments[0].kind != luaNumber || !isLuaStrafeNumber(arguments[0].number) || arguments[0].number < 0 {
		return nil, errors.New("strafe sqrt arguments")
	}
	return []luaValue{numberLuaValue(math.Sqrt(arguments[0].number))}, nil
}

func (e *LuaStrafeRuntime) setFloat(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 2 || arguments[0].kind != luaNumber || arguments[1].kind != luaNumber ||
		arguments[0].number != math.Trunc(arguments[0].number) || arguments[0].number < 0 || arguments[0].number > 15 ||
		!isLuaStrafeNumber(arguments[1].number) {
		return nil, errors.New("strafe float set arguments")
	}
	e.floats[int(arguments[0].number)] = arguments[1].number
	return nil, nil
}

func (e *LuaStrafeRuntime) getFloat(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 1 || arguments[0].kind != luaNumber ||
		arguments[0].number != math.Trunc(arguments[0].number) || arguments[0].number < 0 || arguments[0].number > 15 {
		return nil, errors.New("strafe float get arguments")
	}
	number, isFound := e.floats[int(arguments[0].number)]
	if !isFound {
		return nil, errors.New("strafe float unavailable")
	}
	return []luaValue{numberLuaValue(number)}, nil
}

func (e *LuaStrafeRuntime) closestPosition(arguments []luaValue) ([]luaValue, error) {
	position, err := e.point(arguments, 4)
	if err != nil {
		return nil, fmt.Errorf("strafeClosestPoint: %w", err)
	}
	position, err = e.input.Projection.ClosestPosition(e.input.Role, position)
	if err != nil {
		return nil, fmt.Errorf("strafeProject: %w", err)
	}
	if !isLuaStrafePosition(position) {
		return nil, errors.New("strafe projection nonfinite")
	}
	return luaStrafeCoordinates(position), nil
}

func (e *LuaStrafeRuntime) move(arguments []luaValue) ([]luaValue, error) {
	position, err := e.point(arguments, 5)
	if err != nil {
		return nil, fmt.Errorf("strafeMovePoint: %w", err)
	}
	isTarget := arguments[4].kind == luaString && arguments[4].text == string(e.input.TargetRole) && e.input.TargetRole != ""
	isNone := arguments[4].kind == luaNumber && arguments[4].number == 0 && e.input.TargetRole == ""
	if e.plan.IsMoveRequested || (!isTarget && !isNone) {
		return nil, errors.New("strafe move target")
	}
	e.plan.Destination, e.plan.TargetRole = position, e.input.TargetRole
	e.plan.IsMoveRequested = true
	return nil, nil
}

func (e *LuaStrafeRuntime) wait(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 2 || !e.isActor(arguments[:1]) || arguments[1].kind != luaNumber ||
		!isLuaStrafeNumber(arguments[1].number) || arguments[1].number < 0 || !e.plan.IsMoveRequested || e.plan.IsWaiting {
		return nil, errors.New("strafe wait arguments")
	}
	e.plan.NearGoalDistance = float32(arguments[1].number)
	e.plan.IsWaiting = true
	return nil, nil
}

func (e *LuaStrafeRuntime) addAttribute(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 3 || !e.isActor(arguments[:1]) || arguments[1].kind != luaNumber ||
		arguments[1].number != 48 || arguments[2].kind != luaNumber || !isLuaStrafeNumber(arguments[2].number) || e.attributeID != 0 {
		return nil, errors.New("strafe attribute add arguments")
	}
	e.attributeID = 1
	e.plan.MovementSpeedBuff = float32(arguments[2].number)
	return []luaValue{numberLuaValue(float64(e.attributeID))}, nil
}

func (e *LuaStrafeRuntime) removeAttribute(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 2 || !e.isActor(arguments[:1]) || arguments[1].kind != luaNumber ||
		e.attributeID == 0 || arguments[1].number != float64(e.attributeID) {
		return nil, errors.New("strafe attribute remove arguments")
	}
	e.cleanup.RemovedMovementSpeedBuff = e.plan.MovementSpeedBuff
	e.attributeID = 0
	return nil, nil
}

func (e *LuaStrafeRuntime) stop(arguments []luaValue) ([]luaValue, error) {
	if !e.isActor(arguments) || e.cleanup.IsStopRequested {
		return nil, errors.New("strafe stop arguments")
	}
	e.cleanup.IsStopRequested = true
	return nil, nil
}

func (e *LuaStrafeRuntime) point(arguments []luaValue, count int) (Position, error) {
	if len(arguments) != count || !e.isActor(arguments[:1]) {
		return Position{}, errors.New("strafe point arguments")
	}
	for _, argument := range arguments[1:4] {
		if argument.kind != luaNumber || !isLuaStrafeNumber(argument.number) {
			return Position{}, errors.New("strafe point nonfinite")
		}
	}
	position := Position{X: float32(arguments[1].number), Y: float32(arguments[2].number), Z: float32(arguments[3].number)}
	if !isLuaStrafePosition(position) {
		return Position{}, errors.New("strafe point overflow")
	}
	return position, nil
}

func (e *LuaStrafeRuntime) isActor(arguments []luaValue) bool {
	return len(arguments) == 1 && arguments[0].kind == luaString && arguments[0].text == string(e.input.Role)
}

func isLuaStrafeNumber(number float64) bool {
	return !math.IsNaN(number) && !math.IsInf(number, 0)
}

func isLuaStrafePosition(position Position) bool {
	return isLuaStrafeNumber(float64(position.X)) && isLuaStrafeNumber(float64(position.Y)) && isLuaStrafeNumber(float64(position.Z))
}

func luaStrafeCoordinates(position Position) []luaValue {
	return []luaValue{numberLuaValue(float64(position.X)), numberLuaValue(float64(position.Y)), numberLuaValue(float64(position.Z))}
}
