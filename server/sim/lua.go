package sim

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	contentlua "github.com/darkspinnet/darkspin/content/lua51"
	"github.com/darkspinnet/darkspin/server/util"
)

const defaultLuaInstructionBudget = 10000
const defaultLuaMemoryBudget = 65536

var luaJobBuiltinModule = map[string]struct{}{
	"Lua!GlobalDefinitions.lua": {},
	"Lua!TargetUtils.lua":       {},
	"Lua!Vector.lua":            {},
}

var luaAbilityDefinitionBuiltinModule = map[string]struct{}{
	"0x3681d755!Global.lua":     {},
	"Lua!Global.lua":            {},
	"Lua!GlobalDefinitions.lua": {},
	"Lua!TargetUtils.lua":       {},
	"Lua!Vector.lua":            {},
}

var errLuaYield = errors.New("lua yield")

// Position is an authoritative world position supplied to a simulation plan.
type Position struct {
	X float32
	Y float32
	Z float32
}

// LuaBytecode is one content.db Lua chunk and its immutable identity.
type LuaBytecode struct {
	ChunkID  int64
	SHA256   string
	Contents []byte
}

type LuaObjectInput struct {
	Role                            Role
	IsAlive                         bool
	Team                            int
	IsInvisibleToSecurityTeleporter bool
}

// LuaAbilityInput supplies the allowlisted bytecode and authoritative values
// needed to compile one ability activation into a typed simulation program.
type LuaAbilityInput struct {
	Root                             LuaBytecode
	Modules                          map[string]LuaBytecode
	Role                             Role
	PlayerRole                       Role
	PlayerIndex                      int
	TargetRole                       Role
	Position                         Position
	InstructionBudget                int
	MemoryBudget                     int
	IsObjectCreationFailed           bool
	preloadedModules                 map[string]bool
	playerRoles                      []Role
	initiatingRole                   Role
	sourceRole                       Role
	initiatingPlayerIndex            int
	areLevelsBeaten                  []bool
	destination                      Position
	triggerRole                      Role
	isTriggerModifierActive          bool
	nearbyObjects                    []LuaObjectInput
	nearbyObjectScans                [][]LuaObjectInput
	nearbyObjectScanIndex            int
	isYieldAfterWait                 bool
	yieldAfterWaitCount              int
	waitCount                        int
	objectiveID                      uint32
	objectiveInteractableCount       int
	objectiveHealthInteractableCount int
	objectiveDestructibleCount       int
	objectiveMajorDifficulty         float64
	objectiveHealthMultiplier        float64
	objectiveCompletionTime          float64
}

// LuaJobInput supplies one allowlisted server job and the stable players that
// job may enumerate. PreloadedModules names dependencies whose behavior is
// provided entirely by typed Go bindings rather than another bytecode chunk.
type LuaJobInput struct {
	Root                   LuaBytecode
	Modules                map[string]LuaBytecode
	EntryGlobal            string
	PlayerRoles            []Role
	PreloadedModules       []string
	InitiatingRole         Role
	SourceRole             Role
	InitiatingPlayerIndex  int
	IsLevelBeaten          []bool
	InstructionBudget      int
	MemoryBudget           int
	IsObjectCreationFailed bool
	ObjectiveID            uint32
}

// LuaObjectiveInput supplies one packaged objective definition and the stable
// identity assigned to its match-session instance.
type LuaObjectiveInput struct {
	Root                    LuaBytecode
	Modules                 map[string]LuaBytecode
	ObjectiveID             uint32
	InteractableCount       int
	HealthInteractableCount int
	DestructibleCount       int
	MajorDifficulty         float64
	HealthMultiplier        float64
	InstructionBudget       int
	MemoryBudget            int
}

type LuaObjectiveEventKind string

const (
	LuaObjectiveEventDeath                LuaObjectiveEventKind = "Death"
	LuaObjectiveEventFullClear            LuaObjectiveEventKind = "FullClear"
	LuaObjectiveEventTouchedObelisk       LuaObjectiveEventKind = "TouchedObelisk"
	LuaObjectiveEventTouchedHealthObelisk LuaObjectiveEventKind = "TouchedHealthObelisk"
	LuaObjectiveEventDamage               LuaObjectiveEventKind = "Damage"
	LuaObjectiveEventHeal                 LuaObjectiveEventKind = "Heal"
	LuaObjectiveEventModifierCreated      LuaObjectiveEventKind = "ModifierCreated"
)

// LuaObjectiveEvent supplies one authoritative event to a retained packaged
// objective instance. KillPercent is expressed as a fraction in [0,1]. An
// accepted obelisk interaction reports whether its use was still available at
// the authority boundary used by HasInteractableUsesLeft.
type LuaObjectiveEvent struct {
	Kind                       LuaObjectiveEventKind
	ObjectID                   uint32
	KillPercent                float64
	IsInteractableUseAvailable bool
	TargetObjectID             uint32
	SourceObjectID             uint32
	Damage                     float64
	Healing                    float64
	DamageInteger              int32
	TargetTeam                 uint8
	SourceTeam                 uint8
	PlayerIndex                uint8
	IsTargetNPC                bool
	IsTargetDestructible       bool
	IsTargetPlayerControlled   bool
	IsSourcePlayerControlled   bool
	TargetMarkerID             uint32
	TargetAssetID              uint32
	ModifierDescriptor         uint32
}

type LuaObjectiveStatus string

const (
	LuaObjectiveStatusGold   LuaObjectiveStatus = "Gold"
	LuaObjectiveStatusSilver LuaObjectiveStatus = "Silver"
	LuaObjectiveStatusBronze LuaObjectiveStatus = "Bronze"
	LuaObjectiveStatusFailed LuaObjectiveStatus = "Failed"
)

// LuaObjectiveStatusInput supplies the authoritative match completion time to
// one packaged ObjectiveStatus callback.
type LuaObjectiveStatusInput struct {
	PlayerIndex    uint8
	CompletionTime time.Duration
	KillPercent    float64
}

// LuaObjectiveStatusResult preserves both the authored medal result and any
// objective-token mutations emitted while calculating it.
type LuaObjectiveStatusResult struct {
	Status  LuaObjectiveStatus
	Program Program
}

// LuaObjectiveRuntime retains authored private tables and closures for one
// match-session objective instance.
type LuaObjectiveRuntime struct {
	mu                sync.Mutex
	compiler          *luaCompiler
	instructionBudget int
	isEventSubscriber bool
}

type LuaModifierInput struct {
	Root                    LuaBytecode
	Modules                 map[string]LuaBytecode
	Role                    Role
	Position                Position
	Destination             Position
	IsTriggerModifierActive bool
	NearbyObjects           []LuaObjectInput
	NearbyObjectScans       [][]LuaObjectInput
	IsYieldAfterWait        bool
	YieldAfterWaitCount     int
	Callback                string
	InstructionBudget       int
	MemoryBudget            int
}

// LuaBehaviorInput supplies one packaged behavior and the stable object whose
// behavior callbacks are being compiled.
type LuaBehaviorInput struct {
	Root              LuaBytecode
	Modules           map[string]LuaBytecode
	EntryGlobal       string
	Role              Role
	InstructionBudget int
	MemoryBudget      int
}

type luaValueKind uint8

const (
	luaNil luaValueKind = iota
	luaBoolean
	luaNumber
	luaString
	luaTableValue
	luaClosureValue
	luaNativeValue
)

type luaValue struct {
	kind    luaValueKind
	isTrue  bool
	number  float64
	text    string
	table   *luaTable
	closure *luaClosure
	native  luaNative
}

type luaKey struct {
	kind   luaValueKind
	number float64
	text   string
	isTrue bool
}

type luaTable struct {
	fields map[luaKey]luaValue
	parent *luaTable
}

type luaClosure struct {
	prototype *contentlua.Prototype
	identity  Provenance
	upvalues  []*luaUpvalue
}

type luaUpvalue struct{ field *luaValue }

type luaNative func([]luaValue) ([]luaValue, error)

type luaCompiler struct {
	input                 LuaAbilityInput
	global                *luaTable
	program               Program
	moduleChunks          map[string]*contentlua.Chunk
	loadedModules         map[string]bool
	instructionBudget     int
	memoryBudget          int
	currentIdentity       Provenance
	currentPC             uint32
	activation            *luaClosure
	registeredAbility     *luaTable
	registeredModifier    *luaTable
	registeredObjective   *luaTable
	objectiveName         string
	modifierName          string
	privateTable          *luaTable
	triggerCallbacks      []*luaClosure
	attributeHandle       uint32
	attributeKinds        map[uint32]AttributeKind
	internalObjectIndexes map[*luaTable]int
	threadInteger         [16]luaValue
	objectiveEvent        LuaObjectiveEvent
}

// CompileLuaAbility compiles an allowlisted Lua 5.1 ability activation into a
// deterministic typed plan. It provides no filesystem, network, persistence,
// standard-library, or process access.
func CompileLuaAbility(input LuaAbilityInput) (program Program, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			program = Program{}
			err = fmt.Errorf("bytecodePanic: %v", recovered)
		}
	}()
	if input.Role == "" {
		return Program{}, errors.New("empty role")
	}
	input.preloadedModules = make(map[string]bool, len(luaAbilityDefinitionBuiltinModule))
	for name := range luaAbilityDefinitionBuiltinModule {
		input.preloadedModules[name] = true
	}
	compiler, err := newLuaCompiler(input)
	if err != nil {
		return Program{}, fmt.Errorf("compilerCreate: %w", err)
	}
	err = compiler.executeChunk(compiler.moduleChunks["$root"])
	if err != nil {
		return Program{}, fmt.Errorf("rootExecute: %w", err)
	}
	if compiler.registeredAbility != nil {
		activation := compiler.registeredAbility.get(numberLuaKey(2))
		if activation.kind == luaClosureValue {
			compiler.activation = activation.closure
		}
	}
	if compiler.activation == nil {
		return Program{}, errors.New("activation missing")
	}
	_, err = compiler.callClosure(compiler.activation, nil)
	if err != nil {
		return Program{}, fmt.Errorf("activationCall: %w", err)
	}
	completion := compiler.program.Provenance
	completion.Confidence = ConfidenceInferred
	compiler.program.Steps = append(compiler.program.Steps, EmitStep{
		Intent: SequenceCompleteIntent{Role: input.Role}, Provenance: &completion,
	})
	return drainLuaProgram(compiler), nil
}

// CompileLuaAbilityDefinition executes only the registration phase of one
// packaged ability and projects its authored rank data into a typed definition.
// Activation callbacks are deliberately not invoked here.
func CompileLuaAbilityDefinition(input LuaAbilityInput) (definition AbilityDefinition, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			definition = AbilityDefinition{}
			err = fmt.Errorf("bytecodePanic: %v", recovered)
		}
	}()
	if input.Role == "" {
		return AbilityDefinition{}, errors.New("empty role")
	}
	input.preloadedModules = make(map[string]bool, len(luaAbilityDefinitionBuiltinModule))
	for name := range luaAbilityDefinitionBuiltinModule {
		input.preloadedModules[name] = true
	}
	compiler, err := newLuaCompiler(input)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("compilerCreate: %w", err)
	}
	err = compiler.executeChunk(compiler.moduleChunks["$root"])
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("rootExecute: %w", err)
	}
	if compiler.registeredAbility == nil {
		return AbilityDefinition{}, errors.New("ability registration missing")
	}
	definition, err = abilityDefinitionFromLua(compiler.registeredAbility, compiler.program.Provenance)
	if err != nil {
		return AbilityDefinition{}, fmt.Errorf("definitionDecode: %w", err)
	}
	if definition.Name != compiler.program.Provenance.FunctionName {
		return AbilityDefinition{}, fmt.Errorf("registrationName: got %s, want %s",
			compiler.program.Provenance.FunctionName, definition.Name)
	}
	return definition, nil
}

// CompileLuaSummonPassiveDefinition executes only registration and projects a
// companion-maintaining passive into a typed rank-one definition.
func CompileLuaSummonPassiveDefinition(
	input LuaAbilityInput,
) (definition SummonPassiveDefinition, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			definition = SummonPassiveDefinition{}
			err = fmt.Errorf("bytecodePanic: %v", recovered)
		}
	}()
	if input.Role == "" {
		return SummonPassiveDefinition{}, errors.New("empty role")
	}
	input.preloadedModules = make(map[string]bool, len(luaAbilityDefinitionBuiltinModule))
	for name := range luaAbilityDefinitionBuiltinModule {
		input.preloadedModules[name] = true
	}
	compiler, err := newLuaCompiler(input)
	if err != nil {
		return SummonPassiveDefinition{}, fmt.Errorf("compilerCreate: %w", err)
	}
	err = compiler.executeChunk(compiler.moduleChunks["$root"])
	if err != nil {
		return SummonPassiveDefinition{}, fmt.Errorf("rootExecute: %w", err)
	}
	if compiler.registeredAbility == nil {
		return SummonPassiveDefinition{}, errors.New("passive registration missing")
	}
	definition, err = summonPassiveDefinitionFromLua(compiler.registeredAbility, compiler.program.Provenance)
	if err != nil {
		return SummonPassiveDefinition{}, fmt.Errorf("definitionDecode: %w", err)
	}
	if definition.Name != compiler.program.Provenance.FunctionName {
		return SummonPassiveDefinition{}, fmt.Errorf("registrationName: got %s, want %s",
			compiler.program.Provenance.FunctionName, definition.Name)
	}
	return definition, nil
}

// CompileLuaJob compiles an allowlisted server job entry into a deterministic
// typed plan without granting the bytecode access to host services.
func CompileLuaJob(input LuaJobInput) (program Program, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			program = Program{}
			err = fmt.Errorf("bytecodePanic: %v", recovered)
		}
	}()
	if input.EntryGlobal == "" {
		return Program{}, errors.New("empty entry global")
	}
	err = validateRoles(input.PlayerRoles)
	if err != nil {
		return Program{}, fmt.Errorf("playerRole: %w", err)
	}
	if len(input.IsLevelBeaten) != 0 && len(input.IsLevelBeaten) != len(input.PlayerRoles) {
		return Program{}, errors.New("level beaten count mismatch")
	}
	if input.InitiatingRole != "" && (input.InitiatingPlayerIndex < 0 ||
		input.InitiatingPlayerIndex >= len(input.PlayerRoles)) {
		return Program{}, errors.New("initiating player index")
	}
	if input.SourceRole != "" && input.InitiatingRole == "" {
		return Program{}, errors.New("source role without initiating role")
	}
	preloadedModules := make(map[string]bool, len(input.PreloadedModules))
	for _, name := range input.PreloadedModules {
		if name == "" {
			return Program{}, errors.New("empty preloaded module")
		}
		if _, isAllowed := luaJobBuiltinModule[name]; !isAllowed {
			return Program{}, fmt.Errorf("preloadedModuleRejected: %s", name)
		}
		if preloadedModules[name] {
			return Program{}, fmt.Errorf("preloadedModuleDuplicate: %s", name)
		}
		preloadedModules[name] = true
	}
	compiler, err := newLuaCompiler(LuaAbilityInput{
		Root: input.Root, Modules: input.Modules, InstructionBudget: input.InstructionBudget,
		MemoryBudget: input.MemoryBudget, IsObjectCreationFailed: input.IsObjectCreationFailed,
		preloadedModules: preloadedModules, playerRoles: append([]Role(nil), input.PlayerRoles...),
		initiatingRole: input.InitiatingRole, initiatingPlayerIndex: input.InitiatingPlayerIndex,
		sourceRole:      input.SourceRole,
		areLevelsBeaten: append([]bool(nil), input.IsLevelBeaten...),
		objectiveID:     input.ObjectiveID,
	})
	if err != nil {
		return Program{}, fmt.Errorf("compilerCreate: %w", err)
	}
	err = compiler.executeChunk(compiler.moduleChunks["$root"])
	if err != nil {
		return Program{}, fmt.Errorf("rootExecute: %w", err)
	}
	entry := compiler.global.get(stringLuaKey(input.EntryGlobal))
	if entry.kind != luaTableValue {
		return Program{}, errors.New("entry table missing")
	}
	main := entry.table.get(stringLuaKey("main"))
	if main.kind != luaClosureValue {
		return Program{}, errors.New("entry main missing")
	}
	compiler.program.Provenance.FunctionName = input.EntryGlobal + ".main"
	var arguments []luaValue
	if input.InitiatingRole != "" {
		source := luaValue{kind: luaNil}
		if input.SourceRole != "" {
			source = stringLuaValue(string(input.SourceRole))
		}
		arguments = []luaValue{source, stringLuaValue(string(input.InitiatingRole))}
	}
	_, err = compiler.callClosure(main.closure, arguments)
	if err != nil {
		return Program{}, fmt.Errorf("entryCall: %w", err)
	}
	return compiler.program, nil
}

// CompileLuaObjective executes a packaged objective's registration phase and
// Init callback. Later event and medal callbacks remain owned by the live
// match-session objective runtime.
func CompileLuaObjective(input LuaObjectiveInput) (program Program, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			program = Program{}
			err = fmt.Errorf("bytecodePanic: %v", recovered)
		}
	}()
	runtime, program, err := NewLuaObjectiveRuntime(input)
	if err != nil {
		return Program{}, err
	}
	_ = runtime
	return program, nil
}

// NewLuaObjectiveRuntime executes registration and Init, returning both the
// initialization plan and a retained instance for later authored callbacks.
func NewLuaObjectiveRuntime(
	input LuaObjectiveInput,
) (*LuaObjectiveRuntime, Program, error) {
	compiler, instructionBudget, err := prepareLuaObjective(input)
	if err != nil {
		return nil, Program{}, err
	}
	eventCallback := compiler.registeredObjective.get(stringLuaKey("HandleEvent"))
	runtime := &LuaObjectiveRuntime{
		compiler: compiler, instructionBudget: instructionBudget,
		isEventSubscriber: eventCallback.kind == luaClosureValue,
	}
	return runtime, drainLuaProgram(compiler), nil
}

func prepareLuaObjective(input LuaObjectiveInput) (*luaCompiler, int, error) {
	if input.ObjectiveID == 0 {
		return nil, 0, errors.New("objective ID missing")
	}
	if input.InteractableCount < 0 || input.HealthInteractableCount < 0 ||
		input.DestructibleCount < 0 ||
		input.MajorDifficulty < 0 ||
		math.IsNaN(input.MajorDifficulty) || math.IsInf(input.MajorDifficulty, 0) ||
		input.HealthMultiplier < 0 || math.IsNaN(input.HealthMultiplier) ||
		math.IsInf(input.HealthMultiplier, 0) {
		return nil, 0, errors.New("objective input invalid")
	}
	instructionBudget := input.InstructionBudget
	if instructionBudget == 0 {
		instructionBudget = defaultLuaInstructionBudget
	}
	compiler, err := newLuaCompiler(LuaAbilityInput{
		Root: input.Root, Modules: input.Modules, InstructionBudget: instructionBudget,
		MemoryBudget:                     input.MemoryBudget,
		preloadedModules:                 map[string]bool{"Lua!GlobalDefinitions.lua": true},
		objectiveID:                      input.ObjectiveID,
		objectiveInteractableCount:       input.InteractableCount,
		objectiveHealthInteractableCount: input.HealthInteractableCount,
		objectiveDestructibleCount:       input.DestructibleCount,
		objectiveMajorDifficulty:         input.MajorDifficulty,
		objectiveHealthMultiplier:        input.HealthMultiplier,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("compilerCreate: %w", err)
	}
	err = compiler.executeChunk(compiler.moduleChunks["$root"])
	if err != nil {
		return nil, 0, fmt.Errorf("rootExecute: %w", err)
	}
	if compiler.registeredObjective == nil || compiler.objectiveName == "" {
		return nil, 0, errors.New("objective registration missing")
	}
	initCallback := compiler.registeredObjective.get(stringLuaKey("Init"))
	if initCallback.kind != luaClosureValue {
		return nil, 0, errors.New("objective init missing")
	}
	compiler.program.Provenance.FunctionName = compiler.objectiveName + ".Init"
	_, err = compiler.callClosure(initCallback.closure, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("objectiveInit: %w", err)
	}
	return compiler, instructionBudget, nil
}

// HandleEvent invokes the packaged HandleEvent closure against retained Lua
// state and returns only the steps emitted by this event.
func (runtime *LuaObjectiveRuntime) HandleEvent(event LuaObjectiveEvent) (Program, error) {
	if runtime == nil || runtime.compiler == nil {
		return Program{}, errors.New("nil objective runtime")
	}
	if event.Kind != LuaObjectiveEventDeath && event.Kind != LuaObjectiveEventFullClear &&
		event.Kind != LuaObjectiveEventTouchedObelisk &&
		event.Kind != LuaObjectiveEventTouchedHealthObelisk &&
		event.Kind != LuaObjectiveEventDamage && event.Kind != LuaObjectiveEventHeal &&
		event.Kind != LuaObjectiveEventModifierCreated {
		return Program{}, fmt.Errorf("objective event kind: %s", event.Kind)
	}
	if math.IsNaN(event.KillPercent) || math.IsInf(event.KillPercent, 0) ||
		event.KillPercent < 0 || event.KillPercent > 1 {
		return Program{}, fmt.Errorf("objective kill percent: %g", event.KillPercent)
	}
	if math.IsNaN(event.Damage) || math.IsInf(event.Damage, 0) || event.Damage < 0 {
		return Program{}, fmt.Errorf("objective damage: %g", event.Damage)
	}
	if math.IsNaN(event.Healing) || math.IsInf(event.Healing, 0) || event.Healing < 0 {
		return Program{}, fmt.Errorf("objective healing: %g", event.Healing)
	}
	if (event.Kind == LuaObjectiveEventDamage || event.Kind == LuaObjectiveEventDeath ||
		event.Kind == LuaObjectiveEventModifierCreated) && event.PlayerIndex >= 4 {
		return Program{}, fmt.Errorf("objective player index: %d", event.PlayerIndex)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	compiler := runtime.compiler
	callback := compiler.registeredObjective.get(stringLuaKey("HandleEvent"))
	if callback.kind != luaClosureValue {
		return Program{}, errors.New("objective event callback missing")
	}
	compiler.objectiveEvent = event
	compiler.instructionBudget = runtime.instructionBudget
	compiler.program.Provenance.FunctionName = compiler.objectiveName + ".HandleEvent"
	_, err := compiler.callClosure(callback.closure, []luaValue{
		stringLuaValue(string(event.Kind)), numberLuaValue(float64(event.ObjectID)),
	})
	if err != nil {
		compiler.program.Steps = nil
		return Program{}, fmt.Errorf("objectiveEvent: %w", err)
	}
	return drainLuaProgram(compiler), nil
}

// IsEventSubscriber reports whether the packaged objective registered the
// optional HandleEvent callback. Objectives without it are driven by their
// dedicated status or timer paths and must not receive generic zone events.
func (runtime *LuaObjectiveRuntime) IsEventSubscriber() bool {
	if runtime == nil {
		return false
	}
	return runtime.isEventSubscriber
}

// Invoke calls one authored custom objective callback with the stable
// objective-instance identity used by SetObjectiveIntDataFromSPID.
func (runtime *LuaObjectiveRuntime) Invoke(callbackName string) (Program, error) {
	if runtime == nil || runtime.compiler == nil {
		return Program{}, errors.New("objective runtime unavailable")
	}
	if callbackName == "" {
		return Program{}, errors.New("objective callback missing")
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	compiler := runtime.compiler
	callback := compiler.registeredObjective.get(stringLuaKey(callbackName))
	if callback.kind != luaClosureValue || callback.closure == nil {
		return Program{}, fmt.Errorf("objective callback unavailable: %s", callbackName)
	}
	compiler.program.Steps = nil
	compiler.instructionBudget = runtime.instructionBudget
	_, err := compiler.callClosure(callback.closure, []luaValue{
		numberLuaValue(float64(compiler.input.objectiveID)),
	})
	if err != nil {
		return Program{}, fmt.Errorf("objectiveCallback[%s]: %w", callbackName, err)
	}
	return compiler.program, nil
}

// EvaluateStatus invokes the packaged ObjectiveStatus closure against retained
// Lua state. Publication of the returned medal remains a gameplay-owner choice.
func (runtime *LuaObjectiveRuntime) EvaluateStatus(
	input LuaObjectiveStatusInput,
) (LuaObjectiveStatusResult, error) {
	if runtime == nil || runtime.compiler == nil {
		return LuaObjectiveStatusResult{}, errors.New("nil objective runtime")
	}
	if input.PlayerIndex >= 4 {
		return LuaObjectiveStatusResult{}, fmt.Errorf("objective player index: %d", input.PlayerIndex)
	}
	if input.CompletionTime < 0 {
		return LuaObjectiveStatusResult{}, fmt.Errorf("objective completion time: %s", input.CompletionTime)
	}
	if math.IsNaN(input.KillPercent) || math.IsInf(input.KillPercent, 0) ||
		input.KillPercent < 0 || input.KillPercent > 1 {
		return LuaObjectiveStatusResult{}, fmt.Errorf("objective kill percent: %g", input.KillPercent)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	compiler := runtime.compiler
	callback := compiler.registeredObjective.get(stringLuaKey("ObjectiveStatus"))
	if callback.kind != luaClosureValue {
		return LuaObjectiveStatusResult{}, errors.New("objective status callback missing")
	}
	compiler.input.objectiveCompletionTime = input.CompletionTime.Seconds()
	compiler.objectiveEvent.KillPercent = input.KillPercent
	compiler.instructionBudget = runtime.instructionBudget
	compiler.program.Provenance.FunctionName = compiler.objectiveName + ".ObjectiveStatus"
	fields, err := compiler.callClosure(callback.closure, []luaValue{
		numberLuaValue(float64(input.PlayerIndex)),
	})
	if err != nil {
		compiler.program.Steps = nil
		return LuaObjectiveStatusResult{}, fmt.Errorf("objectiveStatus: %w", err)
	}
	if len(fields) != 1 || fields[0].kind != luaString {
		compiler.program.Steps = nil
		return LuaObjectiveStatusResult{}, errors.New("objective status result invalid")
	}
	status := LuaObjectiveStatus(fields[0].text)
	if status != LuaObjectiveStatusGold && status != LuaObjectiveStatusSilver &&
		status != LuaObjectiveStatusBronze && status != LuaObjectiveStatusFailed {
		compiler.program.Steps = nil
		return LuaObjectiveStatusResult{}, fmt.Errorf("objective status unknown: %s", fields[0].text)
	}
	return LuaObjectiveStatusResult{Status: status, Program: drainLuaProgram(compiler)}, nil
}

func drainLuaProgram(compiler *luaCompiler) Program {
	program := compiler.program
	program.Steps = append([]Step(nil), compiler.program.Steps...)
	program.InternalObjects = append([]InternalObject(nil), compiler.program.InternalObjects...)
	compiler.program.Steps = nil
	return program
}

// CompileLuaModifier compiles one lifecycle callback independently. The Go
// director remains responsible for deciding when callbacks run.
func CompileLuaModifier(input LuaModifierInput) (program Program, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			program = Program{}
			err = fmt.Errorf("bytecodePanic: %v", recovered)
		}
	}()
	return compileLuaModifierCallbacks(input, []string{input.Callback})
}

// CompileLuaModifierLifecycle compiles ordered lifecycle callbacks against one
// private modifier state. The Go director remains responsible for callback
// timing and cancellation.
func CompileLuaModifierLifecycle(input LuaModifierInput, callbacks []string) (program Program, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			program = Program{}
			err = fmt.Errorf("bytecodePanic: %v", recovered)
		}
	}()
	if len(callbacks) == 0 {
		return Program{}, errors.New("empty callback sequence")
	}
	return compileLuaModifierCallbacks(input, callbacks)
}

// CompileLuaBehaviorLifecycle executes ordered behavior callbacks against one
// compiler-owned thread-data block. WaitForever suspends the lifecycle and
// returns the authored intents accumulated before that suspension.
func CompileLuaBehaviorLifecycle(
	input LuaBehaviorInput, callbacks []string,
) (program Program, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			program = Program{}
			err = fmt.Errorf("bytecodePanic: %v", recovered)
		}
	}()
	if input.Role == "" {
		return Program{}, errors.New("empty role")
	}
	if input.EntryGlobal == "" {
		return Program{}, errors.New("empty entry global")
	}
	if len(callbacks) == 0 {
		return Program{}, errors.New("empty callback sequence")
	}
	for _, callbackName := range callbacks {
		if callbackName != "Activate" && callbackName != "Tick" && callbackName != "Deactivate" {
			return Program{}, fmt.Errorf("callbackRejected: %s", callbackName)
		}
	}
	compiler, err := newLuaCompiler(LuaAbilityInput{
		Root: input.Root, Modules: input.Modules, Role: input.Role,
		InstructionBudget: input.InstructionBudget, MemoryBudget: input.MemoryBudget,
	})
	if err != nil {
		return Program{}, fmt.Errorf("compilerCreate: %w", err)
	}
	err = compiler.executeChunk(compiler.moduleChunks["$root"])
	if err != nil {
		return Program{}, fmt.Errorf("rootExecute: %w", err)
	}
	behavior := compiler.global.get(stringLuaKey(input.EntryGlobal))
	if behavior.kind != luaTableValue || behavior.table == nil {
		return Program{}, errors.New("behavior missing")
	}
	for _, callbackName := range callbacks {
		callback := behavior.table.get(stringLuaKey(callbackName))
		if callback.kind != luaClosureValue {
			return Program{}, fmt.Errorf("callbackMissing: %s", callbackName)
		}
		compiler.program.Provenance.FunctionName = input.EntryGlobal + "." + callbackName
		_, err = compiler.callClosure(callback.closure, nil)
		if errors.Is(err, errLuaYield) {
			return compiler.program, nil
		}
		if err != nil {
			return Program{}, fmt.Errorf("callbackCall[%s]: %w", callbackName, err)
		}
	}
	return compiler.program, nil
}

type LuaTriggerCallback string

const LuaTriggerEnter LuaTriggerCallback = "enter"
const LuaTriggerExit LuaTriggerCallback = "exit"
const LuaTriggerStay LuaTriggerCallback = "stay"

// CompileLuaModifierTriggerCallback reconstructs one callback retained by an
// authored trigger-volume activation. Activation establishes private state;
// only the selected later callback is returned in the resulting program.
func CompileLuaModifierTriggerCallback(
	input LuaModifierInput, callbackKind LuaTriggerCallback, triggerRole Role,
) (program Program, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			program = Program{}
			err = fmt.Errorf("bytecodePanic: %v", recovered)
		}
	}()
	if triggerRole == "" {
		return Program{}, errors.New("empty trigger role")
	}
	callbackIndex := -1
	switch callbackKind {
	case LuaTriggerEnter:
		callbackIndex = 0
	case LuaTriggerExit:
		callbackIndex = 1
	case LuaTriggerStay:
		callbackIndex = 2
	default:
		return Program{}, fmt.Errorf("triggerCallbackRejected: %s", callbackKind)
	}
	compiler, err := createLuaModifierCompiler(input, triggerRole)
	if err != nil {
		return Program{}, err
	}
	err = compiler.callModifierCallback("Activate")
	if err != nil {
		return Program{}, fmt.Errorf("activateCall: %w", err)
	}
	if callbackIndex >= len(compiler.triggerCallbacks) || compiler.triggerCallbacks[callbackIndex] == nil {
		return Program{}, fmt.Errorf("triggerCallbackMissing: %s", callbackKind)
	}
	compiler.program.Steps = nil
	compiler.program.Provenance.FunctionName = compiler.modifierName + ".Trigger" + string(callbackKind)
	_, err = compiler.callClosure(compiler.triggerCallbacks[callbackIndex], []luaValue{
		numberLuaValue(1), stringLuaValue(string(triggerRole)),
	})
	if err != nil {
		return Program{}, fmt.Errorf("triggerCallbackCall: %w", err)
	}
	return compiler.program, nil
}

func compileLuaModifierCallbacks(input LuaModifierInput, callbacks []string) (Program, error) {
	if input.Role == "" {
		return Program{}, errors.New("empty role")
	}
	for _, callbackName := range callbacks {
		if callbackName != "Activate" && callbackName != "Tick" && callbackName != "Deactivate" {
			return Program{}, fmt.Errorf("callbackRejected: %s", callbackName)
		}
	}
	compiler, err := createLuaModifierCompiler(input, "")
	if err != nil {
		return Program{}, err
	}
	for _, callbackName := range callbacks {
		err = compiler.callModifierCallback(callbackName)
		if err != nil {
			if (input.IsYieldAfterWait || input.YieldAfterWaitCount > 0) && errors.Is(err, errLuaYield) {
				return compiler.program, nil
			}
			return Program{}, err
		}
	}
	return compiler.program, nil
}

func createLuaModifierCompiler(input LuaModifierInput, triggerRole Role) (*luaCompiler, error) {
	if input.Role == "" {
		return nil, errors.New("empty role")
	}
	if input.YieldAfterWaitCount < 0 {
		return nil, errors.New("negative yield wait count")
	}
	nearbyObjectScans := make([][]LuaObjectInput, len(input.NearbyObjectScans))
	for index, nearbyObjects := range input.NearbyObjectScans {
		nearbyObjectScans[index] = append([]LuaObjectInput(nil), nearbyObjects...)
	}
	compiler, err := newLuaCompiler(LuaAbilityInput{
		Root: input.Root, Modules: input.Modules, Role: input.Role, Position: input.Position,
		InstructionBudget: input.InstructionBudget, MemoryBudget: input.MemoryBudget,
		destination: input.Destination, triggerRole: triggerRole,
		isTriggerModifierActive: input.IsTriggerModifierActive,
		nearbyObjects:           append([]LuaObjectInput(nil), input.NearbyObjects...),
		nearbyObjectScans:       nearbyObjectScans,
		isYieldAfterWait:        input.IsYieldAfterWait,
		yieldAfterWaitCount:     input.YieldAfterWaitCount,
	})
	if err != nil {
		return nil, fmt.Errorf("compilerCreate: %w", err)
	}
	err = compiler.executeChunk(compiler.moduleChunks["$root"])
	if err != nil {
		return nil, fmt.Errorf("rootExecute: %w", err)
	}
	if compiler.registeredModifier == nil {
		return nil, errors.New("modifier missing")
	}
	return compiler, nil
}

func (c *luaCompiler) callModifierCallback(callbackName string) error {
	callback := c.registeredModifier.get(stringLuaKey(callbackName))
	if callback.kind == luaNil {
		callback = c.registeredModifier.get(modifierCallbackKey(callbackName))
	}
	if callback.kind != luaClosureValue {
		return fmt.Errorf("callbackMissing: %s", callbackName)
	}
	c.program.Provenance.FunctionName = c.modifierName + "." + callbackName
	_, err := c.callClosure(callback.closure, nil)
	if err != nil {
		return fmt.Errorf("callbackCall[%s]: %w", callbackName, err)
	}
	return nil
}

func modifierCallbackKey(callback string) luaKey {
	switch callback {
	case "Tick":
		return numberLuaKey(1)
	case "Activate":
		return numberLuaKey(2)
	case "Deactivate":
		return numberLuaKey(3)
	default:
		return stringLuaKey(callback)
	}
}

func newLuaCompiler(input LuaAbilityInput) (*luaCompiler, error) {
	if input.PlayerRole != "" {
		if input.PlayerIndex < 0 {
			return nil, errors.New("negative player index")
		}
		if len(input.playerRoles) == 0 {
			input.playerRoles = []Role{input.PlayerRole}
		}
		if input.initiatingRole == "" {
			input.initiatingRole = input.Role
		}
		input.initiatingPlayerIndex = input.PlayerIndex
	}
	budget := input.InstructionBudget
	if budget == 0 {
		budget = defaultLuaInstructionBudget
	}
	if budget < 0 {
		return nil, errors.New("negative instruction budget")
	}
	memoryBudget := input.MemoryBudget
	if memoryBudget == 0 {
		memoryBudget = defaultLuaMemoryBudget
	}
	if memoryBudget < 0 {
		return nil, errors.New("negative memory budget")
	}
	compiler := &luaCompiler{
		input: input, global: newLuaTable(nil), moduleChunks: make(map[string]*contentlua.Chunk),
		loadedModules: make(map[string]bool), instructionBudget: budget, memoryBudget: memoryBudget,
		attributeKinds: make(map[uint32]AttributeKind), internalObjectIndexes: make(map[*luaTable]int),
	}
	for name := range input.preloadedModules {
		compiler.loadedModules[name] = true
	}
	root, err := compiler.decodeBytecode(input.Root)
	if err != nil {
		return nil, fmt.Errorf("rootDecode: %w", err)
	}
	compiler.moduleChunks["$root"] = root
	for name, bytecode := range input.Modules {
		chunk, decodeErr := compiler.decodeBytecode(bytecode)
		if decodeErr != nil {
			return nil, fmt.Errorf("moduleDecode[%s]: %w", name, decodeErr)
		}
		compiler.moduleChunks[name] = chunk
	}
	compiler.registerGlobals()
	compiler.program.Provenance = Provenance{
		LuaChunkID: input.Root.ChunkID, BytecodeSHA256: input.Root.SHA256,
		Confidence: ConfidenceBytecode,
	}
	return compiler, nil
}

func (c *luaCompiler) decodeBytecode(bytecode LuaBytecode) (*contentlua.Chunk, error) {
	if bytecode.ChunkID <= 0 {
		return nil, errors.New("invalid chunk ID")
	}
	if len(bytecode.Contents) == 0 {
		return nil, errors.New("empty bytecode")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(bytecode.Contents))
	if bytecode.SHA256 == "" || digest != bytecode.SHA256 {
		return nil, fmt.Errorf("hashMismatch: got %s, want %s", digest, bytecode.SHA256)
	}
	chunk, err := contentlua.Inspect(bytecode.Contents)
	if err != nil {
		return nil, fmt.Errorf("chunkDecode: %w", err)
	}
	return chunk, nil
}

func (c *luaCompiler) registerGlobals() {
	c.global.set(stringLuaKey("kObjIDNone"), numberLuaValue(0))
	c.global.set(stringLuaKey("kAllPlayers"), numberLuaValue(255))
	c.global.set(stringLuaKey("kMaxNumPlayers"), numberLuaValue(4))
	c.setNative(c.global, "SPID", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaString {
			return nil, errors.New("spid arguments")
		}
		return []luaValue{numberLuaValue(float64(util.HashID(arguments[0].text)))}, nil
	})
	class := newLuaTable(nil)
	c.setNative(class, "newClass", func([]luaValue) ([]luaValue, error) {
		classInstance, err := c.newRuntimeTable(nil)
		if err != nil {
			return nil, fmt.Errorf("classTable: %w", err)
		}
		c.setNative(classInstance, "new", func(arguments []luaValue) ([]luaValue, error) {
			if len(arguments) != 1 || arguments[0].kind != luaTableValue {
				return nil, errors.New("new arguments")
			}
			instance, createErr := c.newRuntimeTable(arguments[0].table)
			if createErr != nil {
				return nil, fmt.Errorf("instanceTable: %w", createErr)
			}
			return []luaValue{tableLuaValue(instance)}, nil
		})
		return []luaValue{tableLuaValue(classInstance)}, nil
	})
	templateSeed := newLuaTable(nil)
	templateSeed.set(stringLuaKey("Class"), tableLuaValue(class))
	c.global.set(stringLuaKey("Class"), tableLuaValue(class))
	c.global.set(stringLuaKey("nAbility_FirstAggro_Template"), tableLuaValue(templateSeed))
	mathLibrary := newLuaTable(nil)
	c.setNative(mathLibrary, "floor", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaNumber {
			return nil, fmt.Errorf("math floor arguments: %s", luaKinds(arguments))
		}
		return []luaValue{numberLuaValue(math.Floor(arguments[0].number))}, nil
	})
	c.setNative(mathLibrary, "rad", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaNumber {
			return nil, fmt.Errorf("math rad arguments: %s", luaKinds(arguments))
		}
		return []luaValue{numberLuaValue(arguments[0].number * math.Pi / 180)}, nil
	})
	c.global.set(stringLuaKey("math"), tableLuaValue(mathLibrary))

	stackingAttributeCallback := luaValue{kind: luaNativeValue, native: func([]luaValue) ([]luaValue, error) {
		return nil, nil
	}}
	c.setNative(c.global, "CreateStackingAttributeFunctions", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaNumber ||
			arguments[1].kind != luaTableValue || arguments[1].table == nil {
			return nil, fmt.Errorf("stacking attribute arguments: %s", luaKinds(arguments))
		}
		// AttributeUtils returns initialization and update closures. Definition
		// compilation retains their callable shape; live modifier execution owns
		// the attribute handles and stack-count mutations.
		return []luaValue{stackingAttributeCallback, stackingAttributeCallback}, nil
	})

	nBit := newLuaTable(nil)
	c.setNative(nBit, "Or", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) == 0 {
			return nil, errors.New("bit or arguments")
		}
		mask := uint32(0)
		for index, argument := range arguments {
			if argument.kind == luaString {
				// Non-combat enums remain symbolic because their authored masks
				// are not consumed by the constrained simulator.
				continue
			}
			if argument.kind != luaNumber || argument.number < 0 ||
				argument.number > float64(^uint32(0)) || argument.number != math.Trunc(argument.number) {
				return nil, fmt.Errorf("bit or argument[%d]: %s", index, argument.kind)
			}
			mask |= uint32(argument.number)
		}
		return []luaValue{numberLuaValue(float64(mask))}, nil
	})
	c.setNative(nBit, "And", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) == 0 {
			return nil, errors.New("bit and arguments")
		}
		mask := ^uint32(0)
		for index, argument := range arguments {
			if argument.kind != luaNumber || argument.number < 0 ||
				argument.number > float64(^uint32(0)) ||
				argument.number != math.Trunc(argument.number) {
				return nil, fmt.Errorf("bit and argument[%d]: %s", index, argument.kind)
			}
			mask &= uint32(argument.number)
		}
		return []luaValue{numberLuaValue(float64(mask))}, nil
	})
	c.global.set(stringLuaKey("nBit"), tableLuaValue(nBit))

	nObjective := newLuaTable(nil)
	c.setNative(nObjective, "RegisterObjective", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaString ||
			arguments[0].text == "" || arguments[1].kind != luaTableValue {
			return nil, fmt.Errorf("register objective arguments: %s", luaKinds(arguments))
		}
		if c.registeredObjective != nil {
			return nil, errors.New("objective already registered")
		}
		c.objectiveName = arguments[0].text
		c.registeredObjective = arguments[1].table
		return nil, nil
	})
	c.setNative(nObjective, "GetRegisteredDestructibles", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 0 {
			return nil, fmt.Errorf("registered destructibles arguments: %s", luaKinds(arguments))
		}
		return []luaValue{
			numberLuaValue(float64(c.input.objectiveDestructibleCount)),
		}, nil
	})
	c.setNative(nObjective, "GetObjectiveEventGUIDData", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaNumber ||
			arguments[0].number != float64(c.objectiveEvent.ObjectID) ||
			arguments[1].kind != luaNumber {
			return nil, fmt.Errorf("objective event GUID arguments: %s", luaKinds(arguments))
		}
		dataIndex := int(arguments[1].number)
		if arguments[1].number != float64(dataIndex) || dataIndex < 0 {
			return nil, fmt.Errorf("objective event GUID index: %g", arguments[1].number)
		}
		switch c.objectiveEvent.Kind {
		case LuaObjectiveEventTouchedObelisk, LuaObjectiveEventTouchedHealthObelisk:
			if dataIndex == 0 {
				return []luaValue{numberLuaValue(float64(c.objectiveEvent.ObjectID))}, nil
			}
		case LuaObjectiveEventDamage, LuaObjectiveEventHeal, LuaObjectiveEventDeath:
			if dataIndex == 1 {
				return []luaValue{numberLuaValue(float64(c.objectiveEvent.TargetObjectID))}, nil
			}
			if dataIndex == 2 {
				return []luaValue{numberLuaValue(float64(c.objectiveEvent.SourceObjectID))}, nil
			}
		case LuaObjectiveEventModifierCreated:
			if dataIndex == 0 {
				return []luaValue{numberLuaValue(float64(c.objectiveEvent.SourceObjectID))}, nil
			}
			if dataIndex == 1 {
				return []luaValue{numberLuaValue(float64(c.objectiveEvent.TargetObjectID))}, nil
			}
		case LuaObjectiveEventFullClear:
			return []luaValue{numberLuaValue(float64(c.objectiveEvent.ObjectID))}, nil
		}
		return nil, fmt.Errorf("objective event GUID index: %d", dataIndex)
	})
	c.setNative(nObjective, "GetObjectiveEventFloatData", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaNumber ||
			arguments[0].number != float64(c.objectiveEvent.ObjectID) ||
			arguments[1].kind != luaNumber || arguments[1].number != 3 ||
			(c.objectiveEvent.Kind != LuaObjectiveEventDamage &&
				c.objectiveEvent.Kind != LuaObjectiveEventHeal) {
			return nil, fmt.Errorf("objective event float arguments: %s", luaKinds(arguments))
		}
		if c.objectiveEvent.Kind == LuaObjectiveEventHeal {
			return []luaValue{numberLuaValue(c.objectiveEvent.Healing)}, nil
		}
		return []luaValue{numberLuaValue(c.objectiveEvent.Damage)}, nil
	})
	c.setNative(nObjective, "GetObjectiveEventIntData", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaNumber ||
			arguments[0].number != float64(c.objectiveEvent.ObjectID) ||
			arguments[1].kind != luaNumber {
			return nil, fmt.Errorf("objective event integer arguments: %s", luaKinds(arguments))
		}
		if c.objectiveEvent.Kind == LuaObjectiveEventModifierCreated &&
			arguments[1].number == 3 {
			return []luaValue{
				numberLuaValue(float64(c.objectiveEvent.ModifierDescriptor)),
			}, nil
		}
		if c.objectiveEvent.Kind == LuaObjectiveEventDeath &&
			arguments[1].number == 3 {
			return []luaValue{numberLuaValue(float64(c.objectiveEvent.DamageInteger))}, nil
		}
		if c.objectiveEvent.Kind != LuaObjectiveEventDamage ||
			arguments[1].number != 4 {
			return nil, fmt.Errorf("objective event integer index: %g", arguments[1].number)
		}
		return []luaValue{numberLuaValue(float64(c.objectiveEvent.DamageInteger))}, nil
	})
	c.setNative(nObjective, "SetObjectiveIntData", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 5 || arguments[0].kind != luaNumber ||
			arguments[1].kind != luaNumber || arguments[2].kind != luaNumber ||
			arguments[3].kind != luaBoolean || arguments[4].kind != luaBoolean {
			return nil, fmt.Errorf("objective integer data arguments: %s", luaKinds(arguments))
		}
		if c.input.objectiveID == 0 {
			return nil, errors.New("objective ID missing")
		}
		playerIndex := int(arguments[0].number)
		if arguments[0].number != float64(playerIndex) ||
			(playerIndex != 255 && (playerIndex < 0 || playerIndex > 3)) {
			return nil, fmt.Errorf("objective player index: %g", arguments[0].number)
		}
		tokenIndex := int(arguments[1].number)
		if arguments[1].number != float64(tokenIndex) || tokenIndex < 0 || tokenIndex > 2 {
			return nil, fmt.Errorf("objective token index: %g", arguments[1].number)
		}
		if math.IsNaN(arguments[2].number) || math.IsInf(arguments[2].number, 0) ||
			arguments[2].number < -9223372036854775808.0 ||
			arguments[2].number >= 9223372036854775808.0 {
			return nil, fmt.Errorf("objective integer data: %g", arguments[2].number)
		}
		// Build 103 first casts the Lua number to signed int64, then passes the
		// low 32 bits to the objective store. Go's conversion has the same
		// truncation-toward-zero behavior for this validated range.
		integer := int32(int64(arguments[2].number))
		c.emit(ObjectiveDataIntent{
			ObjectiveID: c.input.objectiveID,
			PlayerIndex: uint8(playerIndex),
			TokenIndex:  uint8(tokenIndex),
			Integer:     integer,
			Flags:       [2]bool{arguments[3].isTrue, arguments[4].isTrue},
		})
		return nil, nil
	})
	c.setNative(nObjective, "SetObjectiveIntDataFromSPID", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 6 || arguments[0].kind != luaNumber ||
			arguments[0].number != float64(c.input.objectiveID) {
			return nil, fmt.Errorf("objective SPID data arguments: %s", luaKinds(arguments))
		}
		return nObjective.get(stringLuaKey("SetObjectiveIntData")).native(arguments[1:])
	})
	c.global.set(stringLuaKey("nObjective"), tableLuaValue(nObjective))
	registerLuaEnum := func(name string, fields ...string) {
		table := newLuaTable(nil)
		for _, field := range fields {
			table.set(stringLuaKey(field), stringLuaValue(field))
		}
		c.global.set(stringLuaKey(name), tableLuaValue(table))
	}
	registerLuaNumberEnum := func(name string, field map[string]uint32) {
		table := newLuaTable(nil)
		for key, number := range field {
			table.set(stringLuaKey(key), numberLuaValue(float64(number)))
		}
		c.global.set(stringLuaKey(name), tableLuaValue(table))
	}
	registerLuaNumberEnum("nDescriptors", map[string]uint32{
		"IsMelee": 1 << 0, "IsBasic": 1 << 1, "IsDoT": 1 << 2,
		"IsAoE": 1 << 3, "IsBuff": 1 << 4, "IsDebuff": 1 << 5,
		"IsPhysicalDamage": 1 << 6, "IsEnergyDamage": 1 << 7,
		"IsCosmetic": 1 << 8, "IsHaste": 1 << 9, "IsChannel": 1 << 10,
		"HitReactNone": 1 << 11, "IsHoT": 1 << 12, "IsProjectile": 1 << 13,
		"IgnorePlayerCount": 1 << 14, "IgnoreDifficulty": 1 << 15,
		"IsSelfResurrect": 1 << 16, "IsInteract": 1 << 17, "IsThorns": 1 << 18,
	})
	nDebuffDescriptors := newLuaTable(nil)
	nDebuffDescriptors.set(stringLuaKey("IsRoot"), numberLuaValue(1))
	nDebuffDescriptors.set(stringLuaKey("IsSlow"), numberLuaValue(2))
	nDebuffDescriptors.set(stringLuaKey("IsStun"), numberLuaValue(64))
	nDebuffDescriptors.set(stringLuaKey("IsSilence"), numberLuaValue(256))
	c.global.set(stringLuaKey("nDebuffDescriptors"), tableLuaValue(nDebuffDescriptors))
	registerLuaEnum("nNPCType", "Destructible")
	registerLuaNumberEnum("nDamageTypes", map[string]uint32{
		"Technology": 0, "Spacetime": 1, "Life": 2, "Elements": 3,
		"Supernatural": 4, "Generic": 5,
	})
	registerLuaNumberEnum("nDamageSources", map[string]uint32{"Physical": 0, "Energy": 1})
	nAbilityPrimaryStat := newLuaTable(nil)
	nAbilityPrimaryStat.set(stringLuaKey("Damage"), numberLuaValue(1))
	nAbilityPrimaryStat.set(stringLuaKey("DamagePerSecond"), stringLuaValue("DamagePerSecond"))
	nAbilityPrimaryStat.set(stringLuaKey("Duration"), stringLuaValue("Duration"))
	nAbilityPrimaryStat.set(stringLuaKey("SingleDamage"), numberLuaValue(13))
	nAbilityPrimaryStat.set(stringLuaKey("SingleHealing"), numberLuaValue(14))
	c.global.set(stringLuaKey("nAbilityPrimaryStat"), tableLuaValue(nAbilityPrimaryStat))
	registerLuaNumberEnum("nCooldownType", map[string]uint32{"Manual": 0})
	registerLuaNumberEnum("nTargetType", map[string]uint32{
		"Enemies": 0, "Allies": 1, "Both": 2, "None": 3,
	})
	registerLuaNumberEnum("nAbilityInterfaceType", map[string]uint32{
		"Position": 0, "Targeted": 1, "SelfCast": 2, "AutoTarget": 3,
		"CreatureTarget": 4, "TerrainPoint": 5, "CreatureTargetDefaultSelf": 6,
	})
	registerLuaEnum("nAbilityAnimationSelection", "Sequence")
	registerLuaEnum("nObjectiveFns", "Init", "HandleEvent", "ObjectiveStatus")
	registerLuaEnum(
		"nObjectiveEvents", "FullClear", "Death", "TouchedObelisk", "Damage",
		"ModifierCreated", "ActivateAffix", "Heal", "TouchedHealthObelisk",
	)
	registerLuaEnum("nObjectiveStatus", "Gold", "Silver", "Bronze", "Failed")
	c.global.set(stringLuaKey("kInvalidGUID"), numberLuaValue(0))

	nAbility := newLuaTable(nil)
	c.setNative(nAbility, "GetAgentID", func([]luaValue) ([]luaValue, error) {
		return []luaValue{stringLuaValue(string(c.input.Role))}, nil
	})
	c.setNative(nAbility, "GetAgentAttributeSnapshot", func([]luaValue) ([]luaValue, error) {
		return []luaValue{{kind: luaNil}}, nil
	})
	c.setNative(nAbility, "GetTargetID", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 0 || c.input.TargetRole == "" {
			return nil, errors.New("target ID arguments")
		}
		return []luaValue{stringLuaValue(string(c.input.TargetRole))}, nil
	})
	c.setNative(nAbility, "IsNPC", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaNumber ||
			arguments[0].number != float64(c.objectiveEvent.TargetObjectID) ||
			(c.objectiveEvent.Kind != LuaObjectiveEventDamage &&
				c.objectiveEvent.Kind != LuaObjectiveEventDeath) {
			return nil, fmt.Errorf("NPC arguments: %s", luaKinds(arguments))
		}
		return []luaValue{booleanLuaValue(c.objectiveEvent.IsTargetNPC)}, nil
	})
	c.setNative(nAbility, "PreloadAnimation", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) < 1 || arguments[0].kind != luaString {
			return nil, errors.New("preload animation arguments")
		}
		return []luaValue{arguments[0]}, nil
	})
	c.setNative(nAbility, "PreloadAsset", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaString || arguments[1].kind != luaString {
			return nil, errors.New("preload asset arguments")
		}
		return []luaValue{arguments[0]}, nil
	})
	c.setNative(nAbility, "PreloadModifier", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 ||
			(arguments[0].kind != luaString && arguments[0].kind != luaNumber) ||
			arguments[1].kind != luaString {
			return nil, errors.New("preload modifier arguments")
		}
		return []luaValue{arguments[0]}, nil
	})
	c.setNative(nAbility, "RegisterAbility", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaString || arguments[1].kind != luaTableValue {
			return nil, errors.New("register ability arguments")
		}
		c.registeredAbility = arguments[1].table
		c.program.Provenance.FunctionName = arguments[0].text
		return nil, nil
	})
	c.global.set(stringLuaKey("nAbility"), tableLuaValue(nAbility))

	nModifier := newLuaTable(nil)
	c.setNative(nModifier, "RegisterModifier", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaString || arguments[1].kind != luaTableValue {
			return nil, fmt.Errorf("register modifier arguments: %s", luaKinds(arguments))
		}
		c.modifierName = arguments[0].text
		c.registeredModifier = arguments[1].table
		return nil, nil
	})
	c.setNative(nModifier, "GetMyAgentID", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 0 {
			return nil, errors.New("modifier agent arguments")
		}
		return []luaValue{stringLuaValue(string(c.input.Role))}, nil
	})
	c.setNative(nModifier, "GetModifierInstanceID", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 0 {
			return nil, errors.New("modifier instance arguments")
		}
		return []luaValue{numberLuaValue(1)}, nil
	})
	c.setNative(nModifier, "GetFloatProperty", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaNumber || arguments[0].number != 1 ||
			arguments[1].kind != luaNumber {
			return nil, fmt.Errorf("modifier float property arguments: %s", luaKinds(arguments))
		}
		propertyIndex := int(arguments[1].number)
		if arguments[1].number != float64(propertyIndex) {
			return nil, errors.New("modifier float property index")
		}
		switch propertyIndex {
		case 0:
			return []luaValue{numberLuaValue(float64(c.input.destination.X))}, nil
		case 1:
			return []luaValue{numberLuaValue(float64(c.input.destination.Y))}, nil
		case 2:
			return []luaValue{numberLuaValue(float64(c.input.destination.Z))}, nil
		default:
			return nil, fmt.Errorf("modifier float property index: %d", propertyIndex)
		}
	})
	c.setNative(nModifier, "RequestModifier", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 8 || arguments[0].kind != luaString ||
			arguments[0].text != string(c.input.triggerRole) || arguments[1].kind != luaString ||
			arguments[1].text != string(c.input.Role) || arguments[2].kind != luaString ||
			arguments[3].kind != luaNumber || arguments[3].number != 0 ||
			arguments[4].kind != luaNumber || arguments[4].number < 0 ||
			arguments[5].kind != luaNumber || arguments[6].kind != luaNumber ||
			arguments[7].kind != luaNumber {
			return nil, fmt.Errorf("modifier request arguments: %s", luaKinds(arguments))
		}
		rank := uint32(arguments[4].number)
		if arguments[4].number != float64(rank) {
			return nil, errors.New("modifier request rank")
		}
		c.emit(ModifierRequestIntent{
			TargetRole: c.input.triggerRole, InitiatorRole: c.input.Role,
			ModifierGUID: arguments[2].text, Rank: rank,
			Destination: Position{
				X: float32(arguments[5].number), Y: float32(arguments[6].number),
				Z: float32(arguments[7].number),
			},
		})
		return nil, nil
	})
	c.setNative(nModifier, "GetFirstModifierByGUID", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaString ||
			arguments[0].text != string(c.input.triggerRole) || arguments[1].kind != luaString ||
			arguments[1].text != "0x502f1932" {
			return nil, fmt.Errorf("modifier lookup arguments: %s", luaKinds(arguments))
		}
		if c.input.isTriggerModifierActive {
			return []luaValue{numberLuaValue(1)}, nil
		}
		return []luaValue{numberLuaValue(0)}, nil
	})
	c.global.set(stringLuaKey("nModifier"), tableLuaValue(nModifier))

	registerLuaNumberEnum("nActivationType", map[string]uint32{
		"Default": 0, "Unique": 1, "CasterUnique": 2,
		"CasterUniqueIrreplaceable": 3, "Stacks": 4, "StacksAndCasterUnique": 5,
		"UniqueIrreplaceable": 6, "UniqueResets": 7,
	})
	nModifierPriorities := newLuaTable(nil)
	for name, priority := range map[string]float64{
		"Recall": 2000, "Banished": 1500, "Caged": 1250, "RaisedIntoTheAir": 975,
		"Knockedback": 950, "Pulled": 950, "Teleported": 950, "HitReact": 900,
		"Stunned": 850, "Shocked": 850, "Slept": 800, "Terrified": 750,
		"Taunted": 700, "Silenced": 650, "HealingReduction": 650, "Enraged": 625,
		"Cursed": 600, "Weakened": 600, "Vulnerable": 600, "Poisoned": 550,
		"Diseased": 550, "Burning": 550, "Rooted": 500, "Slowed": 450,
		"Snared": 400, "Haste": 350, "ProjectilesSlowed": 350, "Resurrection": 350,
		"FollowingOwner": 300, "Aura": 250,
	} {
		nModifierPriorities.set(stringLuaKey(name), numberLuaValue(priority))
	}
	c.global.set(stringLuaKey("nModifierPriorities"), tableLuaValue(nModifierPriorities))
	nAbilityEventFlags := newLuaTable(nil)
	nAbilityEventFlags.set(stringLuaKey("StackModifier"), numberLuaValue(32))
	c.global.set(stringLuaKey("nAbilityEventFlags"), tableLuaValue(nAbilityEventFlags))
	registerLuaNumberEnum("nDeactivationType", map[string]uint32{
		"OnAgentDestroyed": 0, "OnAgentDeath": 1,
	})
	nAbilityFns := newLuaTable(nil)
	for _, name := range []string{"Activate", "Tick", "Deactivate"} {
		nAbilityFns.set(stringLuaKey(name), stringLuaValue(name))
	}
	c.global.set(stringLuaKey("nAbilityFns"), tableLuaValue(nAbilityFns))

	nGameObject := newLuaTable(nil)
	c.setNative(nGameObject, "GetPosition", func(arguments []luaValue) ([]luaValue, error) {
		if err := c.requireRole(arguments); err != nil {
			return nil, fmt.Errorf("getPositionRole: %w", err)
		}
		return []luaValue{
			numberLuaValue(float64(c.input.Position.X)),
			numberLuaValue(float64(c.input.Position.Y)),
			numberLuaValue(float64(c.input.Position.Z)),
		}, nil
	})
	c.setNative(nGameObject, "GetTeleporterDestination", func(arguments []luaValue) ([]luaValue, error) {
		err := c.requireRole(arguments)
		if err != nil {
			return nil, fmt.Errorf("teleporterDestinationRole: %w", err)
		}
		return []luaValue{
			numberLuaValue(float64(c.input.destination.X)),
			numberLuaValue(float64(c.input.destination.Y)),
			numberLuaValue(float64(c.input.destination.Z)),
		}, nil
	})
	c.setNative(nGameObject, "GetOwnerID", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaString {
			return nil, errors.New("owner arguments")
		}
		return []luaValue{{kind: luaNil}}, nil
	})
	c.setNative(nGameObject, "IsAlive", func(arguments []luaValue) ([]luaValue, error) {
		object, err := c.luaObjectArgument(arguments)
		if err != nil {
			return nil, fmt.Errorf("aliveObject: %w", err)
		}
		return []luaValue{booleanLuaValue(object.IsAlive)}, nil
	})
	c.setNative(nGameObject, "GetTeam", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) == 1 && arguments[0].kind == luaNumber &&
			(c.objectiveEvent.Kind == LuaObjectiveEventDamage ||
				c.objectiveEvent.Kind == LuaObjectiveEventDeath) {
			if arguments[0].number == float64(c.objectiveEvent.TargetObjectID) {
				return []luaValue{numberLuaValue(float64(c.objectiveEvent.TargetTeam))}, nil
			}
			if arguments[0].number == float64(c.objectiveEvent.SourceObjectID) {
				return []luaValue{numberLuaValue(float64(c.objectiveEvent.SourceTeam))}, nil
			}
			return nil, fmt.Errorf("team objective object: %g", arguments[0].number)
		}
		object, err := c.luaObjectArgument(arguments)
		if err != nil {
			return nil, fmt.Errorf("teamObject: %w", err)
		}
		return []luaValue{numberLuaValue(float64(object.Team))}, nil
	})
	c.setNative(nGameObject, "HasInteractableUsesLeft", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaNumber ||
			arguments[0].number != float64(c.objectiveEvent.ObjectID) {
			return nil, fmt.Errorf("interactable uses arguments: %s", luaKinds(arguments))
		}
		return []luaValue{booleanLuaValue(c.objectiveEvent.IsInteractableUseAvailable)}, nil
	})
	c.setNative(nGameObject, "GetNPCType", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaNumber ||
			arguments[0].number != float64(c.objectiveEvent.TargetObjectID) ||
			c.objectiveEvent.Kind != LuaObjectiveEventDeath {
			return nil, fmt.Errorf("npc type arguments: %s", luaKinds(arguments))
		}
		if !c.objectiveEvent.IsTargetDestructible {
			return []luaValue{{kind: luaNil}}, nil
		}
		return []luaValue{stringLuaValue("Destructible")}, nil
	})
	c.setNative(nGameObject, "GetMarkerID", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaNumber ||
			arguments[0].number != float64(c.objectiveEvent.TargetObjectID) ||
			c.objectiveEvent.Kind != LuaObjectiveEventDeath {
			return nil, fmt.Errorf("marker ID arguments: %s", luaKinds(arguments))
		}
		return []luaValue{numberLuaValue(float64(c.objectiveEvent.TargetMarkerID))}, nil
	})
	c.setNative(nGameObject, "GetAssetNameWithType", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaNumber ||
			arguments[0].number != float64(c.objectiveEvent.TargetObjectID) ||
			c.objectiveEvent.Kind != LuaObjectiveEventDeath ||
			c.objectiveEvent.TargetAssetID == 0 {
			return nil, fmt.Errorf("asset name objective arguments: %s", luaKinds(arguments))
		}
		return []luaValue{numberLuaValue(float64(c.objectiveEvent.TargetAssetID))}, nil
	})
	c.setNative(nGameObject, "IncrementNumTimesUsed", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaNumber ||
			arguments[0].number != float64(c.objectiveEvent.ObjectID) {
			return nil, fmt.Errorf("interactable increment arguments: %s", luaKinds(arguments))
		}
		// Gameplay authority has already consumed the use. The packaged
		// objective invokes this native before advancing its retained counter.
		return nil, nil
	})
	c.setNative(nGameObject, "SetIsVisible", func(arguments []luaValue) ([]luaValue, error) {
		if err := c.requireRole(arguments); err != nil {
			return nil, fmt.Errorf("visibilityRole: %w", err)
		}
		if len(arguments) != 2 || arguments[1].kind != luaBoolean {
			return nil, errors.New("visibility arguments")
		}
		c.emit(VisibilityIntent{Role: c.input.Role, IsVisible: arguments[1].isTrue})
		return nil, nil
	})
	c.setNative(nGameObject, "SetStealthType", func(arguments []luaValue) ([]luaValue, error) {
		if err := c.requireRole(arguments); err != nil {
			return nil, fmt.Errorf("stealthRole: %w", err)
		}
		return nil, nil
	})
	c.setNative(nGameObject, "SetAnimationState", func(arguments []luaValue) ([]luaValue, error) {
		if err := c.requireRole(arguments); err != nil {
			return nil, fmt.Errorf("animationRole: %w", err)
		}
		if len(arguments) != 2 || arguments[1].kind != luaString {
			return nil, errors.New("animation arguments")
		}
		c.emit(AnimationIntent{Role: c.input.Role, AnimationName: arguments[1].text})
		return nil, nil
	})
	c.setNative(nGameObject, "TeleportObject", func(arguments []luaValue) ([]luaValue, error) {
		err := c.requireRole(arguments)
		if err != nil {
			return nil, fmt.Errorf("teleportRole: %w", err)
		}
		if len(arguments) != 4 || arguments[1].kind != luaNumber ||
			arguments[2].kind != luaNumber || arguments[3].kind != luaNumber {
			return nil, fmt.Errorf("teleport arguments: %s", luaKinds(arguments))
		}
		destination := Position{
			X: float32(arguments[1].number),
			Y: float32(arguments[2].number),
			Z: float32(arguments[3].number),
		}
		if math.IsNaN(float64(destination.X)) || math.IsInf(float64(destination.X), 0) ||
			math.IsNaN(float64(destination.Y)) || math.IsInf(float64(destination.Y), 0) ||
			math.IsNaN(float64(destination.Z)) || math.IsInf(float64(destination.Z), 0) {
			return nil, errors.New("teleport destination non-finite")
		}
		c.emit(TeleportIntent{Role: c.input.Role, Destination: destination})
		return nil, nil
	})
	c.setNative(nGameObject, "ResetAnimationState", func(arguments []luaValue) ([]luaValue, error) {
		if err := c.requireRole(arguments); err != nil {
			return nil, fmt.Errorf("animationResetRole: %w", err)
		}
		c.emit(AnimationResetIntent{Role: c.input.Role})
		return nil, nil
	})
	c.setNative(nGameObject, "AddEffect", func(arguments []luaValue) ([]luaValue, error) {
		if err := c.requireRole(arguments); err != nil {
			return nil, fmt.Errorf("effectAddRole: %w", err)
		}
		if len(arguments) != 2 || arguments[1].kind != luaString {
			return nil, fmt.Errorf("effect add arguments: %s", luaKinds(arguments))
		}
		c.emit(EffectIntent{Role: c.input.Role, EffectName: arguments[1].text})
		return []luaValue{arguments[1]}, nil
	})
	c.setNative(nGameObject, "RemoveEffect", func(arguments []luaValue) ([]luaValue, error) {
		if err := c.requireRole(arguments); err != nil {
			return nil, fmt.Errorf("effectRemoveRole: %w", err)
		}
		if len(arguments) != 2 || arguments[1].kind != luaString {
			return nil, fmt.Errorf("effect remove arguments: %s", luaKinds(arguments))
		}
		c.emit(EffectIntent{
			Role: c.input.Role, EffectName: arguments[1].text, IsStopped: true,
		})
		return nil, nil
	})
	c.setNative(nGameObject, "RemoveEffectIndex", func(arguments []luaValue) ([]luaValue, error) {
		err := c.requireRole(arguments)
		if err != nil {
			return nil, fmt.Errorf("effectRemoveIndexRole: %w", err)
		}
		if len(arguments) != 2 || arguments[1].kind != luaString {
			return nil, fmt.Errorf("effect remove index arguments: %s", luaKinds(arguments))
		}
		c.emit(EffectIntent{
			Role: c.input.Role, EffectName: arguments[1].text, IsStopped: true,
		})
		return nil, nil
	})
	c.global.set(stringLuaKey("nGameObject"), tableLuaValue(nGameObject))

	nLocomotion := newLuaTable(nil)
	c.setNative(nLocomotion, "Stop", func(arguments []luaValue) ([]luaValue, error) {
		if err := c.requireRole(arguments); err != nil {
			return nil, fmt.Errorf("locomotionStopRole: %w", err)
		}
		c.emit(LocomotionStopIntent{Role: c.input.Role})
		return nil, nil
	})
	nLocomotion.set(stringLuaKey("TeleportObject"), nGameObject.get(stringLuaKey("TeleportObject")))
	c.global.set(stringLuaKey("nLocomotion"), tableLuaValue(nLocomotion))

	nAttributeType := newLuaTable(nil)
	nAttributeType.set(stringLuaKey("AttackSpeedScale"), numberLuaValue(23))
	nAttributeType.set(stringLuaKey("MovementSpeedBuff"), numberLuaValue(48))
	nAttributeType.set(stringLuaKey("AoERadius"), numberLuaValue(62))
	nAttributeType.set(stringLuaKey("ImmuneToStunned"), numberLuaValue(73))
	nAttributeType.set(stringLuaKey("DirectAttackDamage"), numberLuaValue(108))
	nAttributeType.set(stringLuaKey("BodyScale"), numberLuaValue(113))
	nAttributeType.set(stringLuaKey("Immobilized"), stringLuaValue(string(AttributeImmobilized)))
	nAttributeType.set(stringLuaKey("Intangible"), stringLuaValue(string(AttributeIntangible)))
	nAttributeType.set(
		stringLuaKey("InvisibleToSecurityTeleporters"),
		stringLuaValue(string(AttributeInvisibleToSecurityTeleporter)),
	)
	c.global.set(stringLuaKey("nAttributeType"), tableLuaValue(nAttributeType))
	nAttribute := newLuaTable(nil)
	c.setNative(nAttribute, "GetHealthMultiplierForDifficulty", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 0 {
			return nil, errors.New("health multiplier arguments")
		}
		return []luaValue{numberLuaValue(c.input.objectiveHealthMultiplier)}, nil
	})
	c.setNative(nAttribute, "AddAttributeModifier", func(arguments []luaValue) ([]luaValue, error) {
		if err := c.requireRole(arguments); err != nil {
			return nil, fmt.Errorf("attributeRole: %w", err)
		}
		if len(arguments) != 3 || arguments[1].kind != luaString || arguments[2].kind != luaNumber {
			return nil, fmt.Errorf("attribute arguments: %s", luaKinds(arguments))
		}
		attributeKinds := AttributeKind(arguments[1].text)
		if attributeKinds != AttributeImmobilized && attributeKinds != AttributeIntangible &&
			attributeKinds != AttributeInvisibleToSecurityTeleporter {
			return nil, fmt.Errorf("attribute kind: %s", arguments[1].text)
		}
		c.emit(AttributeModifierIntent{
			Role: c.input.Role, AttributeKind: attributeKinds, Amount: float32(arguments[2].number),
		})
		c.attributeHandle++
		c.attributeKinds[c.attributeHandle] = attributeKinds
		return []luaValue{numberLuaValue(float64(c.attributeHandle))}, nil
	})
	c.setNative(nAttribute, "RemoveAttributeModifier", func(arguments []luaValue) ([]luaValue, error) {
		if err := c.requireRole(arguments); err != nil {
			return nil, fmt.Errorf("attributeRemoveRole: %w", err)
		}
		if len(arguments) != 2 || arguments[1].kind != luaNumber {
			return nil, fmt.Errorf("attribute remove arguments: %s", luaKinds(arguments))
		}
		handle := uint32(arguments[1].number)
		if arguments[1].number != float64(handle) || handle == 0 {
			return nil, fmt.Errorf("attribute remove handle: %g", arguments[1].number)
		}
		attributeKinds, isFound := c.attributeKinds[handle]
		if !isFound {
			return nil, fmt.Errorf("attribute remove unknown handle: %d", handle)
		}
		delete(c.attributeKinds, handle)
		c.emit(AttributeModifierIntent{Role: c.input.Role, AttributeKind: attributeKinds, Amount: -1})
		return nil, nil
	})
	c.setNative(nAttribute, "GetAttributeValue", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[1].kind != luaString ||
			arguments[1].text != string(AttributeInvisibleToSecurityTeleporter) {
			return nil, fmt.Errorf("attribute value arguments: %s", luaKinds(arguments))
		}
		object, err := c.luaObjectArgument(arguments[:1])
		if err != nil {
			return nil, fmt.Errorf("attributeObject: %w", err)
		}
		if object.IsInvisibleToSecurityTeleporter {
			return []luaValue{numberLuaValue(1)}, nil
		}
		return []luaValue{numberLuaValue(0)}, nil
	})
	c.global.set(stringLuaKey("nAttribute"), tableLuaValue(nAttribute))

	nBehaviorTree := newLuaTable(nil)
	c.setNative(nBehaviorTree, "GetMyObjectID", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 0 {
			return nil, errors.New("behavior object arguments")
		}
		return []luaValue{stringLuaValue(string(c.input.Role))}, nil
	})
	c.global.set(stringLuaKey("nBehaviorTree"), tableLuaValue(nBehaviorTree))

	nGameSimulator := newLuaTable(nil)
	c.setNative(nGameSimulator, "GetMajorDifficulty", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 0 {
			return nil, errors.New("major difficulty arguments")
		}
		return []luaValue{numberLuaValue(c.input.objectiveMajorDifficulty)}, nil
	})
	c.setNative(nGameSimulator, "GetGameObjectiveCompletionTime", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 0 {
			return nil, errors.New("objective completion time arguments")
		}
		return []luaValue{numberLuaValue(c.input.objectiveCompletionTime)}, nil
	})
	c.setNative(nGameSimulator, "StartCinematic", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 5 {
			return nil, errors.New("cinematic arguments")
		}
		for index, argument := range arguments {
			if argument.kind != luaNumber {
				return nil, fmt.Errorf("cinematicNumber[%d]", index)
			}
		}
		c.emit(CinematicIntent{
			FocusRole: c.input.Role,
			Duration:  luaSeconds(arguments[0].number),
			Radius:    float32(arguments[4].number),
		})
		return nil, nil
	})
	c.global.set(stringLuaKey("nGameSimulator"), tableLuaValue(nGameSimulator))

	nThread := newLuaTable(nil)
	c.setNative(nThread, "CreateThreadForObject", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) < 2 {
			return nil, errors.New("thread arguments")
		}
		if arguments[0].kind != luaTableValue || arguments[1].kind != luaClosureValue {
			return nil, nil
		}
		objectIndex, isOwned := c.internalObjectIndexes[arguments[0].table]
		if !isOwned || c.program.InternalObjects[objectIndex].IsMarkedForDelete {
			return nil, nil
		}
		results, err := c.callClosure(arguments[1].closure, arguments[2:])
		if err != nil {
			return nil, fmt.Errorf("threadCall: %w", err)
		}
		return results, nil
	})
	c.setNative(nThread, "WaitForXSeconds", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) < 1 || arguments[0].kind != luaNumber || arguments[0].number < 0 {
			return nil, errors.New("wait arguments")
		}
		c.wait(luaSeconds(arguments[0].number))
		c.input.waitCount++
		yieldWaitCount := c.input.yieldAfterWaitCount
		if yieldWaitCount == 0 && c.input.isYieldAfterWait {
			yieldWaitCount = 1
		}
		if yieldWaitCount > 0 && c.input.waitCount >= yieldWaitCount {
			return nil, errLuaYield
		}
		return nil, nil
	})
	c.setNative(nThread, "WaitForever", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 0 {
			return nil, errors.New("wait forever arguments")
		}
		return nil, errLuaYield
	})
	c.global.set(stringLuaKey("nThread"), tableLuaValue(nThread))

	nThreadData := newLuaTable(nil)
	c.setNative(nThreadData, "CreatePrivateTable", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 0 {
			return nil, errors.New("private table create arguments")
		}
		privateTable, err := c.newRuntimeTable(nil)
		if err != nil {
			return nil, fmt.Errorf("privateTable: %w", err)
		}
		c.privateTable = privateTable
		return []luaValue{tableLuaValue(privateTable)}, nil
	})
	c.setNative(nThreadData, "GetPrivateTable", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 0 {
			return nil, errors.New("private table get arguments")
		}
		if c.privateTable == nil {
			return []luaValue{{kind: luaNil}}, nil
		}
		return []luaValue{tableLuaValue(c.privateTable)}, nil
	})
	c.setNative(nThreadData, "SetInt", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaNumber || arguments[1].kind != luaNumber {
			return nil, fmt.Errorf("thread integer set arguments: %s", luaKinds(arguments))
		}
		index := int(arguments[0].number)
		if arguments[0].number != float64(index) || index < 0 || index >= len(c.threadInteger) {
			return nil, fmt.Errorf("thread integer set index: %g", arguments[0].number)
		}
		c.threadInteger[index] = arguments[1]
		return nil, nil
	})
	c.setNative(nThreadData, "GetInt", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaNumber {
			return nil, fmt.Errorf("thread integer get arguments: %s", luaKinds(arguments))
		}
		index := int(arguments[0].number)
		if arguments[0].number != float64(index) || index < 0 || index >= len(c.threadInteger) {
			return nil, fmt.Errorf("thread integer get index: %g", arguments[0].number)
		}
		field := c.threadInteger[index]
		if field.kind == luaNil {
			return []luaValue{numberLuaValue(0)}, nil
		}
		return []luaValue{field}, nil
	})
	c.global.set(stringLuaKey("nThreadData"), tableLuaValue(nThreadData))

	nUtil := newLuaTable(nil)
	c.setNative(nUtil, "SPID", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaString {
			return nil, errors.New("spid arguments")
		}
		return []luaValue{numberLuaValue(float64(util.HashID(arguments[0].text)))}, nil
	})
	c.setNative(nUtil, "GetAsset", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || (arguments[0].kind != luaString && arguments[0].kind != luaNumber) {
			return nil, errors.New("asset arguments")
		}
		return []luaValue{arguments[0]}, nil
	})
	c.setNative(nUtil, "ToGUID", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaString {
			return nil, errors.New("guid arguments")
		}
		return []luaValue{arguments[0]}, nil
	})
	c.global.set(stringLuaKey("nUtil"), tableLuaValue(nUtil))

	c.setNative(c.global, "ipairs", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaTableValue {
			return nil, errors.New("ipairs arguments")
		}
		iterator := luaValue{kind: luaNativeValue, native: func(iteratorArguments []luaValue) ([]luaValue, error) {
			if len(iteratorArguments) != 2 || iteratorArguments[0].kind != luaTableValue ||
				iteratorArguments[1].kind != luaNumber {
				return nil, errors.New("ipairs iterator arguments")
			}
			nextIndex := int(iteratorArguments[1].number) + 1
			if iteratorArguments[1].number != float64(nextIndex-1) || nextIndex < 1 {
				return nil, errors.New("ipairs iterator index")
			}
			field := iteratorArguments[0].table.get(numberLuaKey(float64(nextIndex)))
			if field.kind == luaNil {
				return []luaValue{{kind: luaNil}, {kind: luaNil}}, nil
			}
			return []luaValue{numberLuaValue(float64(nextIndex)), field}, nil
		}}
		return []luaValue{iterator, arguments[0], numberLuaValue(0)}, nil
	})

	nSporeLabs := newLuaTable(nil)
	nSporeLabs.set(stringLuaKey("organicDamageableObjectTypes"), tableLuaValue(newLuaTable(nil)))
	c.global.set(stringLuaKey("nSporeLabs"), tableLuaValue(nSporeLabs))

	nObjectManager := newLuaTable(nil)
	c.setNative(nObjectManager, "GetNumInteractableObjects", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaNumber {
			return nil, fmt.Errorf("interactable count arguments: %s", luaKinds(arguments))
		}
		abilityID := arguments[0].number
		switch abilityID {
		case float64(util.HashID("InteractWithObelisk")):
			return []luaValue{
				numberLuaValue(float64(c.input.objectiveInteractableCount)),
			}, nil
		case float64(util.HashID("InteractHealthObelisk")):
			return []luaValue{
				numberLuaValue(float64(c.input.objectiveHealthInteractableCount)),
			}, nil
		default:
			return nil, fmt.Errorf("interactable count ability: %#x", uint32(abilityID))
		}
	})
	c.setNative(nObjectManager, "CreateObject", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || (arguments[0].kind != luaString && arguments[0].kind != luaNumber) {
			return nil, errors.New("create object arguments")
		}
		if c.input.IsObjectCreationFailed {
			return []luaValue{numberLuaValue(0)}, nil
		}
		object, err := c.newRuntimeTable(nil)
		if err != nil {
			return nil, fmt.Errorf("objectTable: %w", err)
		}
		nounReference := ""
		var nounID uint32
		if arguments[0].kind == luaString {
			nounReference = arguments[0].text
			nounID = util.HashID(nounReference)
		} else {
			if arguments[0].number < 0 || arguments[0].number > math.MaxUint32 ||
				arguments[0].number != math.Trunc(arguments[0].number) {
				return nil, errors.New("create object noun ID")
			}
			nounID = uint32(arguments[0].number)
		}
		objectID := uint32(len(c.program.InternalObjects) + 1)
		c.program.InternalObjects = append(c.program.InternalObjects, InternalObject{
			ID: objectID, NounReference: nounReference, NounID: nounID,
		})
		if c.internalObjectIndexes == nil {
			c.internalObjectIndexes = make(map[*luaTable]int)
		}
		c.internalObjectIndexes[object] = len(c.program.InternalObjects) - 1
		return []luaValue{tableLuaValue(object)}, nil
	})
	c.setNative(nObjectManager, "CreateTriggerVolume", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 7 || arguments[0].kind != luaNumber ||
			arguments[1].kind != luaNumber || arguments[2].kind != luaNumber ||
			arguments[3].kind != luaNumber || arguments[4].kind != luaClosureValue ||
			arguments[5].kind != luaClosureValue || arguments[6].kind != luaClosureValue {
			return nil, fmt.Errorf("trigger volume arguments: %s", luaKinds(arguments))
		}
		center := Position{
			X: float32(arguments[0].number),
			Y: float32(arguments[1].number),
			Z: float32(arguments[2].number),
		}
		c.emit(TriggerVolumeIntent{
			Role: c.input.Role, Center: center, Radius: float32(arguments[3].number),
		})
		c.triggerCallbacks = []*luaClosure{
			arguments[4].closure, arguments[5].closure, arguments[6].closure,
		}
		return []luaValue{numberLuaValue(1)}, nil
	})
	c.setNative(nObjectManager, "GetObjectsInRadius", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 5 || arguments[0].kind != luaNumber ||
			arguments[1].kind != luaNumber || arguments[2].kind != luaNumber ||
			arguments[3].kind != luaNumber || arguments[4].kind != luaTableValue {
			return nil, fmt.Errorf("objects radius arguments: %s", luaKinds(arguments))
		}
		objects, err := c.newRuntimeTable(nil)
		if err != nil {
			return nil, fmt.Errorf("objectsTable: %w", err)
		}
		nearbyObjects := c.input.nearbyObjects
		if len(c.input.nearbyObjectScans) != 0 {
			scanIndex := min(c.input.nearbyObjectScanIndex, len(c.input.nearbyObjectScans)-1)
			nearbyObjects = c.input.nearbyObjectScans[scanIndex]
			c.input.nearbyObjectScanIndex++
		}
		for index, object := range nearbyObjects {
			if object.Role == "" {
				return nil, fmt.Errorf("nearbyRole[%d]", index)
			}
			err = c.setRuntimeField(
				objects, numberLuaKey(float64(index+1)), stringLuaValue(string(object.Role)),
			)
			if err != nil {
				return nil, fmt.Errorf("nearbyField[%d]: %w", index, err)
			}
		}
		return []luaValue{tableLuaValue(objects)}, nil
	})
	c.setNative(nObjectManager, "DestroyTriggerVolume", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaNumber || arguments[0].number != 1 {
			return nil, fmt.Errorf("destroy trigger volume arguments: %s", luaKinds(arguments))
		}
		c.emit(DestroyTriggerVolumeIntent{Role: c.input.Role})
		return nil, nil
	})
	c.global.set(stringLuaKey("nObjectManager"), tableLuaValue(nObjectManager))

	nPlayer := newLuaTable(nil)
	c.setNative(nPlayer, "IsPlayerControlledObject", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) == 1 && arguments[0].kind == luaNumber &&
			(c.objectiveEvent.Kind == LuaObjectiveEventDamage ||
				c.objectiveEvent.Kind == LuaObjectiveEventHeal ||
				c.objectiveEvent.Kind == LuaObjectiveEventDeath ||
				c.objectiveEvent.Kind == LuaObjectiveEventModifierCreated) {
			if arguments[0].number == float64(c.objectiveEvent.TargetObjectID) {
				return []luaValue{
					booleanLuaValue(c.objectiveEvent.IsTargetPlayerControlled),
				}, nil
			}
			if arguments[0].number == float64(c.objectiveEvent.SourceObjectID) {
				return []luaValue{
					booleanLuaValue(c.objectiveEvent.IsSourcePlayerControlled),
				}, nil
			}
			return nil, fmt.Errorf("player controlled objective object: %g", arguments[0].number)
		}
		if len(arguments) != 1 || arguments[0].kind != luaString {
			return nil, errors.New("player controlled arguments")
		}
		return []luaValue{booleanLuaValue(arguments[0].text == string(c.input.triggerRole))}, nil
	})
	c.setNative(nPlayer, "GetPlayerIds", func([]luaValue) ([]luaValue, error) {
		playerTable, err := c.newRuntimeTable(nil)
		if err != nil {
			return nil, fmt.Errorf("playerTable: %w", err)
		}
		for index, role := range c.input.playerRoles {
			if role == "" {
				return nil, fmt.Errorf("playerRole[%d]", index)
			}
			err = c.setRuntimeField(
				playerTable, numberLuaKey(float64(index+1)), stringLuaValue(string(role)),
			)
			if err != nil {
				return nil, fmt.Errorf("playerField[%d]: %w", index, err)
			}
		}
		return []luaValue{tableLuaValue(playerTable)}, nil
	})
	c.setNative(nPlayer, "GetPlayerIdForObject", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) == 1 && arguments[0].kind == luaNumber &&
			(c.objectiveEvent.Kind == LuaObjectiveEventDamage ||
				c.objectiveEvent.Kind == LuaObjectiveEventDeath ||
				c.objectiveEvent.Kind == LuaObjectiveEventModifierCreated) {
			if arguments[0].number != float64(c.objectiveEvent.SourceObjectID) &&
				arguments[0].number != float64(c.objectiveEvent.TargetObjectID) {
				return nil, fmt.Errorf("player objective object: %g", arguments[0].number)
			}
			return []luaValue{numberLuaValue(float64(c.objectiveEvent.PlayerIndex))}, nil
		}
		if len(arguments) != 1 || arguments[0].kind != luaString ||
			arguments[0].text != string(c.input.initiatingRole) {
			return nil, errors.New("player object arguments")
		}
		return []luaValue{numberLuaValue(float64(c.input.initiatingPlayerIndex))}, nil
	})
	c.setNative(nPlayer, "HasBeatenThisLevel", func(arguments []luaValue) ([]luaValue, error) {
		playerIndex, err := c.playerIndexArgument(arguments)
		if err != nil {
			return nil, fmt.Errorf("level beaten player: %w", err)
		}
		isBeaten := false
		if len(c.input.areLevelsBeaten) != 0 {
			isBeaten = c.input.areLevelsBeaten[playerIndex]
		}
		return []luaValue{booleanLuaValue(isBeaten)}, nil
	})
	c.setNative(nPlayer, "UnlockNextAbility", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaNumber {
			kinds := make([]luaValueKind, len(arguments))
			for index, argument := range arguments {
				kinds[index] = argument.kind
			}
			return nil, fmt.Errorf("unlock next ability arguments: %v", kinds)
		}
		playerIndex := int(arguments[0].number)
		if arguments[0].number != float64(playerIndex) || playerIndex < 0 ||
			playerIndex >= len(c.input.playerRoles) {
			return nil, fmt.Errorf("unlock next ability player: %g", arguments[0].number)
		}
		role := c.input.playerRoles[playerIndex]
		c.emit(UnlockIntent{PlayerRole: role, UnlockKind: UnlockNextAbility, Count: 1})
		return nil, nil
	})
	c.setNative(nPlayer, "UnlockSecondCreature", func(arguments []luaValue) ([]luaValue, error) {
		role, err := c.playerRoleArgument(arguments)
		if err != nil {
			return nil, fmt.Errorf("unlock second creature player: %w", err)
		}
		c.emit(UnlockIntent{PlayerRole: role, UnlockKind: UnlockSecondCreature, Count: 1})
		return nil, nil
	})
	c.setNative(nPlayer, "GetPlayerControlledObjectID", func(arguments []luaValue) ([]luaValue, error) {
		role, err := c.playerRoleArgument(arguments)
		if err != nil {
			return nil, fmt.Errorf("controlled object player: %w", err)
		}
		return []luaValue{stringLuaValue(string(role))}, nil
	})
	c.setNative(nPlayer, "UnlockOverdrive", func(arguments []luaValue) ([]luaValue, error) {
		role, err := c.playerRoleArgument(arguments)
		if err != nil {
			return nil, fmt.Errorf("unlock overdrive player: %w", err)
		}
		c.emit(UnlockIntent{PlayerRole: role, UnlockKind: UnlockOverdrive, Count: 1})
		return nil, nil
	})
	c.setNative(nPlayer, "UnlockCrystals", func(arguments []luaValue) ([]luaValue, error) {
		role, err := c.playerRoleArgument(arguments)
		if err != nil {
			return nil, fmt.Errorf("unlock crystals player: %w", err)
		}
		c.emit(UnlockIntent{PlayerRole: role, UnlockKind: UnlockCrystals, Count: 1})
		return nil, nil
	})
	c.setNative(nPlayer, "DropCrystals", func(arguments []luaValue) ([]luaValue, error) {
		role, err := c.playerRoleArgument(arguments)
		if err != nil {
			return nil, fmt.Errorf("drop crystals player: %w", err)
		}
		c.emit(CrystalDropIntent{PlayerRole: role})
		return nil, nil
	})
	c.setNative(nPlayer, "PickupCrystal", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaNumber || arguments[1].kind != luaString {
			return nil, fmt.Errorf("pickup crystal arguments: %s", luaKinds(arguments))
		}
		playerIndex := int(arguments[0].number)
		if arguments[0].number != float64(playerIndex) || playerIndex < 0 ||
			playerIndex >= len(c.input.playerRoles) {
			return nil, fmt.Errorf("pickup crystal player: %g", arguments[0].number)
		}
		if arguments[1].text != string(c.input.TargetRole) {
			return nil, fmt.Errorf("pickup crystal target: %s", arguments[1].text)
		}
		c.emit(CrystalPickupIntent{
			PlayerRole: c.input.playerRoles[playerIndex], AgentRole: c.input.Role,
			TargetRole: c.input.TargetRole,
		})
		return nil, nil
	})
	c.global.set(stringLuaKey("nPlayer"), tableLuaValue(nPlayer))

	nGameDirector := newLuaTable(nil)
	c.setNative(nGameDirector, "GetKillPercent", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 0 {
			return nil, errors.New("kill percent arguments")
		}
		return []luaValue{numberLuaValue(c.objectiveEvent.KillPercent)}, nil
	})
	c.setNative(nGameDirector, "ActivateHordeSpawn", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 2 || arguments[0].kind != luaString ||
			arguments[0].text != string(c.input.sourceRole) || arguments[1].kind != luaString {
			return nil, fmt.Errorf("activate horde arguments: %s", luaKinds(arguments))
		}
		isControlledRole := false
		for _, role := range c.input.playerRoles {
			isControlledRole = isControlledRole || arguments[1].text == string(role)
		}
		if !isControlledRole {
			return nil, errors.New("activate horde controlled object")
		}
		// Build 103 resolves argument one and calls sub_A23560, whose body is
		// a literal no-op. Do not synthesize a horde event from this native.
		return nil, nil
	})
	c.global.set(stringLuaKey("nGameDirector"), tableLuaValue(nGameDirector))

	nEvent := newLuaTable(nil)
	c.setNative(nEvent, "Notify", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaTableValue {
			return nil, errors.New("notify arguments")
		}
		eventTable := arguments[0].table
		if len(eventTable.fields) != 1 {
			return nil, fmt.Errorf("notify field count: %d", len(eventTable.fields))
		}
		clientEventID := eventTable.get(stringLuaKey("clientEventID"))
		if clientEventID.kind != luaNumber || clientEventID.number !=
			float64(ClientEventPlayerUnlockedSecondCreatureID) {
			return nil, fmt.Errorf("notify client event ID: %g", clientEventID.number)
		}
		c.emit(ClientEventIntent{
			EventKind: ClientEventPlayerUnlockedSecondCreature,
			EventID:   ClientEventPlayerUnlockedSecondCreatureID,
		})
		return nil, nil
	})
	c.global.set(stringLuaKey("nEvent"), tableLuaValue(nEvent))

	c.setNative(nGameObject, "MarkForDelete", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaTableValue {
			return nil, errors.New("mark delete arguments")
		}
		objectIndex, isOwned := c.internalObjectIndexes[arguments[0].table]
		if !isOwned {
			return nil, nil
		}
		c.program.InternalObjects[objectIndex].IsMarkedForDelete = true
		return nil, nil
	})

	nStealthTypes := newLuaTable(nil)
	nStealthTypes.set(stringLuaKey("None"), numberLuaValue(0))
	nStealthTypes.set(stringLuaKey("Technology"), numberLuaValue(1))
	nStealthTypes.set(stringLuaKey("Supernatural"), numberLuaValue(2))
	nStealthTypes.set(stringLuaKey("FullyInvisible"), numberLuaValue(3))
	c.global.set(stringLuaKey("nStealthTypes"), tableLuaValue(nStealthTypes))

	c.setNative(c.global, "require", func(arguments []luaValue) ([]luaValue, error) {
		if len(arguments) != 1 || arguments[0].kind != luaString {
			return nil, errors.New("require arguments")
		}
		name := arguments[0].text
		if c.loadedModules[name] {
			return nil, nil
		}
		chunk := c.moduleChunks[name]
		if chunk == nil {
			return nil, fmt.Errorf("moduleMissing: %s", name)
		}
		c.loadedModules[name] = true
		err := c.executeChunk(chunk)
		if err != nil {
			return nil, fmt.Errorf("moduleExecute[%s]: %w", name, err)
		}
		return nil, nil
	})
}

func (c *luaCompiler) executeChunk(chunk *contentlua.Chunk) error {
	if chunk == nil || chunk.Main == nil {
		return errors.New("nil chunk")
	}
	err := c.reserveMemory(1, "chunkClosure")
	if err != nil {
		return err
	}
	identity := c.identityForChunk(chunk)
	closure := &luaClosure{prototype: chunk.Main, identity: identity}
	_, err = c.callClosure(closure, nil)
	if err != nil {
		return fmt.Errorf("chunkCall: %w", err)
	}
	return nil
}

func (c *luaCompiler) identityForChunk(chunk *contentlua.Chunk) Provenance {
	for name, candidate := range c.moduleChunks {
		if candidate != chunk {
			continue
		}
		bytecode := c.input.Root
		if name != "$root" {
			bytecode = c.input.Modules[name]
		}
		return Provenance{
			LuaChunkID: bytecode.ChunkID, BytecodeSHA256: bytecode.SHA256,
			Confidence: ConfidenceBytecode,
		}
	}
	return Provenance{Confidence: ConfidenceUnknown}
}

func (c *luaCompiler) callClosure(closure *luaClosure, arguments []luaValue) ([]luaValue, error) {
	prototype := closure.prototype
	err := c.reserveMemory(int(prototype.MaxStackSize), "registerFrame")
	if err != nil {
		return nil, err
	}
	registers := make([]luaValue, int(prototype.MaxStackSize))
	parameterCount := min(int(prototype.ParameterCount), len(arguments))
	copy(registers, arguments[:parameterCount])
	vararguments := arguments[parameterCount:]
	stackTop := parameterCount
	isStackTopOpen := false
	openUpvalue := make(map[int]*luaUpvalue)
	previousIdentity := c.currentIdentity
	previousPC := c.currentPC
	c.currentIdentity = closure.identity
	defer func() {
		c.currentIdentity = previousIdentity
		c.currentPC = previousPC
	}()
	for pc := 0; pc < len(prototype.Instructions); {
		if c.instructionBudget == 0 {
			return nil, errors.New("instruction budget exhausted")
		}
		c.instructionBudget--
		instruction := prototype.Instructions[pc]
		c.currentPC = uint32(pc)
		nextPC := pc + 1
		switch instruction.Opcode() {
		case contentlua.OpcodeMove:
			registers[instruction.A()] = registers[instruction.B()]
		case contentlua.OpcodeLoadK:
			registers[instruction.A()] = constantLuaValue(prototype.Constants[instruction.Bx()])
		case contentlua.OpcodeLoadBool:
			registers[instruction.A()] = booleanLuaValue(instruction.B() != 0)
			if instruction.C() != 0 {
				nextPC++
			}
		case contentlua.OpcodeLoadNil:
			for index := int(instruction.A()); index <= int(instruction.B()); index++ {
				registers[index] = luaValue{kind: luaNil}
			}
		case contentlua.OpcodeGetUpval:
			upvalueIndex := int(instruction.B())
			if upvalueIndex >= len(closure.upvalues) || closure.upvalues[upvalueIndex] == nil ||
				closure.upvalues[upvalueIndex].field == nil {
				return nil, fmt.Errorf("getUpvalue[%d]: %d", pc, upvalueIndex)
			}
			registers[instruction.A()] = *closure.upvalues[upvalueIndex].field
		case contentlua.OpcodeSetUpval:
			upvalueIndex := int(instruction.B())
			if upvalueIndex >= len(closure.upvalues) || closure.upvalues[upvalueIndex] == nil ||
				closure.upvalues[upvalueIndex].field == nil {
				return nil, fmt.Errorf("setUpvalue[%d]: %d", pc, upvalueIndex)
			}
			*closure.upvalues[upvalueIndex].field = registers[instruction.A()]
		case contentlua.OpcodeGetGlobal:
			key, err := prototypeString(prototype, instruction.Bx())
			if err != nil {
				return nil, fmt.Errorf("getGlobal[%d]: %w", pc, err)
			}
			registers[instruction.A()] = c.global.get(stringLuaKey(key))
		case contentlua.OpcodeSetGlobal:
			key, err := prototypeString(prototype, instruction.Bx())
			if err != nil {
				return nil, fmt.Errorf("setGlobal[%d]: %w", pc, err)
			}
			err = c.setRuntimeField(c.global, stringLuaKey(key), registers[instruction.A()])
			if err != nil {
				return nil, fmt.Errorf("setGlobal[%d]: %w", pc, err)
			}
		case contentlua.OpcodeGetTable:
			table := registers[instruction.B()]
			key := c.rk(prototype, registers, instruction.C())
			if table.kind != luaTableValue {
				return nil, fmt.Errorf("getTable[%d]: got %s for key %s", pc, table.kind, luaFieldText(key))
			}
			registers[instruction.A()] = table.table.get(valueLuaKey(key))
		case contentlua.OpcodeSetTable:
			table := registers[instruction.A()]
			if table.kind != luaTableValue {
				return nil, fmt.Errorf("setTable[%d]: got %s", pc, table.kind)
			}
			err = c.setRuntimeField(
				table.table, valueLuaKey(c.rk(prototype, registers, instruction.B())),
				c.rk(prototype, registers, instruction.C()),
			)
			if err != nil {
				return nil, fmt.Errorf("setTable[%d]: %w", pc, err)
			}
		case contentlua.OpcodeNewTable:
			table, tableErr := c.newRuntimeTable(nil)
			if tableErr != nil {
				return nil, fmt.Errorf("newTable[%d]: %w", pc, tableErr)
			}
			registers[instruction.A()] = tableLuaValue(table)
		case contentlua.OpcodeSetList:
			table := registers[instruction.A()]
			if table.kind != luaTableValue || table.table == nil {
				return nil, fmt.Errorf("setListTable[%d]: %s", pc, table.kind)
			}
			fieldCount := int(instruction.B())
			if fieldCount == 0 {
				if !isStackTopOpen {
					return nil, fmt.Errorf("variableSetListTop[%d]", pc)
				}
				fieldCount = stackTop - int(instruction.A()) - 1
				if fieldCount < 0 {
					return nil, fmt.Errorf("variableSetListRange[%d]: %d", pc, fieldCount)
				}
			}
			block := int(instruction.C())
			if block == 0 {
				if nextPC >= len(prototype.Instructions) {
					return nil, fmt.Errorf("setListBlock[%d]", pc)
				}
				block = int(uint32(prototype.Instructions[nextPC]))
				nextPC++
			}
			base := int(instruction.A())
			if base+fieldCount >= len(registers) || block <= 0 {
				return nil, fmt.Errorf("setListRange[%d]: %d/%d", pc, fieldCount, block)
			}
			firstIndex := (block-1)*50 + 1
			for index := 0; index < fieldCount; index++ {
				err = c.setRuntimeField(table.table, numberLuaKey(float64(firstIndex+index)),
					registers[base+1+index])
				if err != nil {
					return nil, fmt.Errorf("setListField[%d:%d]: %w", pc, index, err)
				}
			}
			isStackTopOpen = false
		case contentlua.OpcodeSelf:
			table := registers[instruction.B()]
			if table.kind != luaTableValue {
				return nil, fmt.Errorf("selfTable[%d]: got %s", pc, table.kind)
			}
			registers[int(instruction.A())+1] = table
			registers[instruction.A()] = table.table.get(valueLuaKey(c.rk(prototype, registers, instruction.C())))
		case contentlua.OpcodeAdd:
			left := c.rk(prototype, registers, instruction.B())
			right := c.rk(prototype, registers, instruction.C())
			if left.kind != luaNumber || right.kind != luaNumber {
				return nil, fmt.Errorf("addNumber[%d]", pc)
			}
			registers[instruction.A()] = numberLuaValue(left.number + right.number)
		case contentlua.OpcodeSub:
			left := c.rk(prototype, registers, instruction.B())
			right := c.rk(prototype, registers, instruction.C())
			if left.kind != luaNumber || right.kind != luaNumber {
				return nil, fmt.Errorf("subNumber[%d]", pc)
			}
			registers[instruction.A()] = numberLuaValue(left.number - right.number)
		case contentlua.OpcodeMul:
			left := c.rk(prototype, registers, instruction.B())
			right := c.rk(prototype, registers, instruction.C())
			if left.kind != luaNumber || right.kind != luaNumber {
				return nil, fmt.Errorf("mulNumber[%d]", pc)
			}
			registers[instruction.A()] = numberLuaValue(left.number * right.number)
		case contentlua.OpcodeDiv:
			left := c.rk(prototype, registers, instruction.B())
			right := c.rk(prototype, registers, instruction.C())
			if left.kind != luaNumber || right.kind != luaNumber {
				return nil, fmt.Errorf("divNumber[%d]", pc)
			}
			registers[instruction.A()] = numberLuaValue(left.number / right.number)
		case contentlua.OpcodeMod:
			left := c.rk(prototype, registers, instruction.B())
			right := c.rk(prototype, registers, instruction.C())
			if left.kind != luaNumber || right.kind != luaNumber {
				return nil, fmt.Errorf("modNumber[%d]", pc)
			}
			registers[instruction.A()] = numberLuaValue(left.number - math.Floor(left.number/right.number)*right.number)
		case contentlua.OpcodePow:
			left := c.rk(prototype, registers, instruction.B())
			right := c.rk(prototype, registers, instruction.C())
			if left.kind != luaNumber || right.kind != luaNumber {
				return nil, fmt.Errorf("powNumber[%d]", pc)
			}
			registers[instruction.A()] = numberLuaValue(math.Pow(left.number, right.number))
		case contentlua.OpcodeUnm:
			field := registers[instruction.B()]
			if field.kind != luaNumber {
				return nil, fmt.Errorf("unmNumber[%d]", pc)
			}
			registers[instruction.A()] = numberLuaValue(-field.number)
		case contentlua.OpcodeNot:
			registers[instruction.A()] = booleanLuaValue(!isLuaTruthy(registers[instruction.B()]))
		case contentlua.OpcodeLen:
			field := registers[instruction.B()]
			switch field.kind {
			case luaString:
				registers[instruction.A()] = numberLuaValue(float64(len(field.text)))
			case luaTableValue:
				registers[instruction.A()] = numberLuaValue(float64(field.table.sequenceLength()))
			default:
				return nil, fmt.Errorf("lengthType[%d]: %s", pc, field.kind)
			}
		case contentlua.OpcodeConcat:
			first := int(instruction.B())
			last := int(instruction.C())
			if first > last || last >= len(registers) {
				return nil, fmt.Errorf("concatRange[%d]: %d/%d", pc, first, last)
			}
			var builder strings.Builder
			for index := first; index <= last; index++ {
				text, textErr := luaConcatText(registers[index])
				if textErr != nil {
					return nil, fmt.Errorf("concatField[%d:%d]: %w", pc, index, textErr)
				}
				builder.WriteString(text)
			}
			registers[instruction.A()] = stringLuaValue(builder.String())
		case contentlua.OpcodeTest:
			if isLuaTruthy(registers[instruction.A()]) != (instruction.C() != 0) {
				nextPC++
			}
		case contentlua.OpcodeEq:
			left := c.rk(prototype, registers, instruction.B())
			right := c.rk(prototype, registers, instruction.C())
			if luaEqual(left, right) != (instruction.A() != 0) {
				nextPC++
			}
		case contentlua.OpcodeLt:
			left := c.rk(prototype, registers, instruction.B())
			right := c.rk(prototype, registers, instruction.C())
			if left.kind != luaNumber || right.kind != luaNumber {
				return nil, fmt.Errorf("lessThanNumber[%d]: %s/%s", pc, left.kind, right.kind)
			}
			if (left.number < right.number) != (instruction.A() != 0) {
				nextPC++
			}
		case contentlua.OpcodeLe:
			left := c.rk(prototype, registers, instruction.B())
			right := c.rk(prototype, registers, instruction.C())
			isLessOrEqual := false
			switch {
			case left.kind == luaNumber && right.kind == luaNumber:
				isLessOrEqual = left.number <= right.number
			case left.kind == luaString && right.kind == luaString:
				isLessOrEqual = left.text <= right.text
			default:
				return nil, fmt.Errorf("lessEqualType[%d]: %s/%s", pc, left.kind, right.kind)
			}
			if isLessOrEqual != (instruction.A() != 0) {
				nextPC++
			}
		case contentlua.OpcodeTestSet:
			field := registers[instruction.B()]
			if isLuaTruthy(field) == (instruction.C() != 0) {
				registers[instruction.A()] = field
			} else {
				nextPC++
			}
		case contentlua.OpcodeJmp:
			nextPC += int(instruction.SBx())
		case contentlua.OpcodeCall:
			base := int(instruction.A())
			argumentCount := int(instruction.B()) - 1
			if instruction.B() == 0 {
				if !isStackTopOpen {
					return nil, fmt.Errorf("variableCallTop[%d]", pc)
				}
				argumentCount = stackTop - base - 1
			}
			if argumentCount < 0 || base+1+argumentCount > len(registers) {
				return nil, fmt.Errorf("callArgumentRange[%d]: %d/%d", pc, base, argumentCount)
			}
			callArguments := append([]luaValue(nil),
				registers[base+1:base+1+argumentCount]...)
			results, err := c.callValue(registers[base], callArguments)
			if err != nil {
				return nil, fmt.Errorf("call[%d]: %w", pc, err)
			}
			if instruction.C() == 0 {
				if base+len(results) > len(registers) {
					return nil, fmt.Errorf("callResultRange[%d]: %d/%d", pc, base, len(results))
				}
				copy(registers[base:], results)
				stackTop = base + len(results)
				isStackTopOpen = true
				break
			}
			resultCount := int(instruction.C()) - 1
			for index := 0; index < resultCount; index++ {
				result := luaValue{kind: luaNil}
				if index < len(results) {
					result = results[index]
				}
				registers[base+index] = result
			}
			isStackTopOpen = false
		case contentlua.OpcodeTailCall:
			base := int(instruction.A())
			argumentCount := int(instruction.B()) - 1
			if instruction.B() == 0 {
				if !isStackTopOpen {
					return nil, fmt.Errorf("variableTailCallTop[%d]", pc)
				}
				argumentCount = stackTop - base - 1
			}
			if argumentCount < 0 || base+1+argumentCount > len(registers) {
				return nil, fmt.Errorf("tailCallArgumentRange[%d]: %d/%d", pc, base, argumentCount)
			}
			callArguments := append([]luaValue(nil),
				registers[base+1:base+1+argumentCount]...)
			results, err := c.callValue(registers[base], callArguments)
			if err != nil {
				return nil, fmt.Errorf("tailCall[%d]: %w", pc, err)
			}
			closeLuaUpvalues(openUpvalue, 0)
			return results, nil
		case contentlua.OpcodeReturn:
			base := int(instruction.A())
			resultCount := int(instruction.B()) - 1
			if instruction.B() == 0 {
				if !isStackTopOpen {
					return nil, fmt.Errorf("variableReturnTop[%d]", pc)
				}
				resultCount = stackTop - base
			}
			if resultCount < 0 || base+resultCount > len(registers) {
				return nil, fmt.Errorf("returnRange[%d]: %d/%d", pc, base, resultCount)
			}
			results := append([]luaValue(nil), registers[base:base+resultCount]...)
			closeLuaUpvalues(openUpvalue, 0)
			return results, nil
		case contentlua.OpcodeForLoop:
			base := int(instruction.A())
			if base+3 >= len(registers) {
				return nil, fmt.Errorf("forLoopRegister[%d]: %d", pc, base)
			}
			index := registers[base]
			limit := registers[base+1]
			step := registers[base+2]
			if index.kind != luaNumber || limit.kind != luaNumber || step.kind != luaNumber {
				return nil, fmt.Errorf("forLoopNumber[%d]", pc)
			}
			index.number += step.number
			registers[base] = index
			isContinuing := index.number <= limit.number
			if step.number <= 0 {
				isContinuing = limit.number <= index.number
			}
			if isContinuing {
				registers[base+3] = index
				nextPC += int(instruction.SBx())
			}
		case contentlua.OpcodeForPrep:
			base := int(instruction.A())
			if base+2 >= len(registers) {
				return nil, fmt.Errorf("forPrepRegister[%d]: %d", pc, base)
			}
			index := registers[base]
			step := registers[base+2]
			if index.kind != luaNumber || step.kind != luaNumber {
				return nil, fmt.Errorf("forPrepNumber[%d]", pc)
			}
			registers[base] = numberLuaValue(index.number - step.number)
			nextPC += int(instruction.SBx())
		case contentlua.OpcodeTForLoop:
			base := int(instruction.A())
			resultCount := int(instruction.C())
			if base+2+resultCount >= len(registers) {
				return nil, fmt.Errorf("tForLoopRegister[%d]: %d", pc, base)
			}
			results, callErr := c.callValue(registers[base], []luaValue{
				registers[base+1], registers[base+2],
			})
			if callErr != nil {
				return nil, fmt.Errorf("tForLoopCall[%d]: %w", pc, callErr)
			}
			for index := 0; index < resultCount; index++ {
				result := luaValue{kind: luaNil}
				if index < len(results) {
					result = results[index]
				}
				registers[base+3+index] = result
			}
			if registers[base+3].kind == luaNil {
				nextPC++
			} else {
				registers[base+2] = registers[base+3]
			}
		case contentlua.OpcodeClosure:
			if int(instruction.Bx()) >= len(prototype.Prototypes) {
				return nil, fmt.Errorf("closurePrototype[%d]: %d", pc, instruction.Bx())
			}
			child := prototype.Prototypes[instruction.Bx()]
			err = c.reserveMemory(1+int(child.UpvalueCount), "closure")
			if err != nil {
				return nil, fmt.Errorf("closureMemory[%d]: %w", pc, err)
			}
			childClosure := &luaClosure{
				prototype: child, identity: closure.identity,
				upvalues: make([]*luaUpvalue, int(child.UpvalueCount)),
			}
			for upvalueIndex := range childClosure.upvalues {
				bindingPC := pc + 1 + upvalueIndex
				if bindingPC >= len(prototype.Instructions) {
					return nil, fmt.Errorf("closureBinding[%d]: missing %d", pc, upvalueIndex)
				}
				if c.instructionBudget == 0 {
					return nil, errors.New("instruction budget exhausted")
				}
				c.instructionBudget--
				binding := prototype.Instructions[bindingPC]
				switch binding.Opcode() {
				case contentlua.OpcodeMove:
					registerIndex := int(binding.B())
					if registerIndex >= len(registers) {
						return nil, fmt.Errorf("closureMove[%d]: %d", bindingPC, registerIndex)
					}
					upvalue := openUpvalue[registerIndex]
					if upvalue == nil {
						upvalue = &luaUpvalue{field: &registers[registerIndex]}
						openUpvalue[registerIndex] = upvalue
					}
					childClosure.upvalues[upvalueIndex] = upvalue
				case contentlua.OpcodeGetUpval:
					parentIndex := int(binding.B())
					if parentIndex >= len(closure.upvalues) || closure.upvalues[parentIndex] == nil {
						return nil, fmt.Errorf("closureUpvalue[%d]: %d", bindingPC, parentIndex)
					}
					childClosure.upvalues[upvalueIndex] = closure.upvalues[parentIndex]
				default:
					return nil, fmt.Errorf("closureBindingOpcode[%d]: %s", bindingPC, binding.Opcode())
				}
			}
			registers[instruction.A()] = closureLuaValue(childClosure)
			nextPC += int(child.UpvalueCount)
		case contentlua.OpcodeClose:
			closeLuaUpvalues(openUpvalue, int(instruction.A()))
		case contentlua.OpcodeVararg:
			if instruction.B() == 0 {
				base := int(instruction.A())
				if base+len(vararguments) > len(registers) {
					return nil, fmt.Errorf("variableVarargRange[%d]: %d/%d", pc, base, len(vararguments))
				}
				copy(registers[base:], vararguments)
				stackTop = base + len(vararguments)
				isStackTopOpen = true
				break
			}
			resultCount := int(instruction.B()) - 1
			for index := 0; index < resultCount; index++ {
				field := luaValue{kind: luaNil}
				if index < len(vararguments) {
					field = vararguments[index]
				}
				registers[int(instruction.A())+index] = field
			}
			isStackTopOpen = false
		default:
			return nil, fmt.Errorf("opcodeUnsupported[%d]: %s", pc, instruction.Opcode())
		}
		if nextPC < 0 || nextPC > len(prototype.Instructions) {
			return nil, fmt.Errorf("programCounter[%d]: %d", pc, nextPC)
		}
		pc = nextPC
	}
	closeLuaUpvalues(openUpvalue, 0)
	return nil, nil
}

func closeLuaUpvalues(openUpvalue map[int]*luaUpvalue, base int) {
	for registerIndex, upvalue := range openUpvalue {
		if registerIndex < base {
			continue
		}
		closedField := *upvalue.field
		upvalue.field = &closedField
		delete(openUpvalue, registerIndex)
	}
}

func (c *luaCompiler) callValue(function luaValue, arguments []luaValue) ([]luaValue, error) {
	switch function.kind {
	case luaNativeValue:
		results, err := function.native(arguments)
		if err != nil {
			return nil, fmt.Errorf("nativeCall: %w", err)
		}
		return results, nil
	case luaClosureValue:
		results, err := c.callClosure(function.closure, arguments)
		if err != nil {
			return nil, fmt.Errorf("closureCall: %w", err)
		}
		return results, nil
	default:
		return nil, fmt.Errorf("notCallable: %s", function.kind)
	}
}

func (c *luaCompiler) rk(prototype *contentlua.Prototype, registers []luaValue, operand uint16) luaValue {
	if operand >= 256 {
		return constantLuaValue(prototype.Constants[operand-256])
	}
	return registers[operand]
}

func (c *luaCompiler) setNative(table *luaTable, name string, native luaNative) {
	table.set(stringLuaKey(name), luaValue{kind: luaNativeValue, native: native})
}

func (c *luaCompiler) emit(intent Intent) {
	identity := c.currentIdentity
	identity.FunctionName = c.program.Provenance.FunctionName
	identity.InstructionOffset = c.currentPC
	identity.IsInstructionOffsetKnown = true
	c.program.Steps = append(c.program.Steps, EmitStep{
		Intent: intent, Offset: c.currentPC, IsOffsetKnown: true, Provenance: &identity,
	})
}

func (c *luaCompiler) wait(duration time.Duration) {
	identity := c.currentIdentity
	identity.FunctionName = c.program.Provenance.FunctionName
	identity.InstructionOffset = c.currentPC
	identity.IsInstructionOffsetKnown = true
	c.program.Steps = append(c.program.Steps, WaitStep{
		Duration: duration, Offset: c.currentPC, IsOffsetKnown: true, Provenance: &identity,
	})
}

func (c *luaCompiler) requireRole(arguments []luaValue) error {
	if len(arguments) < 1 || arguments[0].kind != luaString || arguments[0].text != string(c.input.Role) {
		return errors.New("role mismatch")
	}
	return nil
}

func (c *luaCompiler) luaObjectArgument(arguments []luaValue) (LuaObjectInput, error) {
	if len(arguments) != 1 || arguments[0].kind != luaString {
		return LuaObjectInput{}, errors.New("object role required")
	}
	for _, object := range c.input.nearbyObjects {
		if string(object.Role) == arguments[0].text {
			return object, nil
		}
	}
	for _, nearbyObjects := range c.input.nearbyObjectScans {
		for _, object := range nearbyObjects {
			if string(object.Role) == arguments[0].text {
				return object, nil
			}
		}
	}
	return LuaObjectInput{}, fmt.Errorf("object role: %s", arguments[0].text)
}

func (c *luaCompiler) playerRoleArgument(arguments []luaValue) (Role, error) {
	playerIndex, err := c.playerIndexArgument(arguments)
	if err != nil {
		return "", err
	}
	return c.input.playerRoles[playerIndex], nil
}

func (c *luaCompiler) playerIndexArgument(arguments []luaValue) (int, error) {
	if len(arguments) != 1 || arguments[0].kind != luaNumber {
		return 0, errors.New("numeric player slot required")
	}
	playerIndex := int(arguments[0].number)
	if arguments[0].number != float64(playerIndex) || playerIndex < 0 ||
		playerIndex >= len(c.input.playerRoles) {
		return 0, fmt.Errorf("player slot: %g", arguments[0].number)
	}
	return playerIndex, nil
}

func (c *luaCompiler) reserveMemory(unitCount int, allocationKind string) error {
	if unitCount < 0 {
		return fmt.Errorf("memoryUnits[%s]: %d", allocationKind, unitCount)
	}
	if unitCount > c.memoryBudget {
		return fmt.Errorf("memory budget exhausted: %s needs %d, remaining %d",
			allocationKind, unitCount, c.memoryBudget)
	}
	c.memoryBudget -= unitCount
	return nil
}

func (c *luaCompiler) newRuntimeTable(parent *luaTable) (*luaTable, error) {
	err := c.reserveMemory(1, "table")
	if err != nil {
		return nil, err
	}
	return newLuaTable(parent), nil
}

func (c *luaCompiler) setRuntimeField(table *luaTable, key luaKey, field luaValue) error {
	if table == nil {
		return errors.New("nil table")
	}
	if !table.has(key) {
		err := c.reserveMemory(1, "tableField")
		if err != nil {
			return err
		}
	}
	table.set(key, field)
	return nil
}

func newLuaTable(parent *luaTable) *luaTable {
	return &luaTable{fields: make(map[luaKey]luaValue), parent: parent}
}

func (t *luaTable) get(key luaKey) luaValue {
	field, isFound := t.fields[key]
	if isFound {
		return field
	}
	if t.parent != nil {
		return t.parent.get(key)
	}
	return luaValue{kind: luaNil}
}

func (t *luaTable) set(key luaKey, field luaValue) { t.fields[key] = field }

func (t *luaTable) has(key luaKey) bool {
	_, isFound := t.fields[key]
	return isFound
}

func (t *luaTable) sequenceLength() int {
	for index := 1; ; index++ {
		if t.get(numberLuaKey(float64(index))).kind == luaNil {
			return index - 1
		}
	}
}

func prototypeString(prototype *contentlua.Prototype, index uint32) (string, error) {
	if int(index) >= len(prototype.Constants) || prototype.Constants[index].Kind != contentlua.ConstantString {
		return "", fmt.Errorf("constantString: %d", index)
	}
	return prototype.Constants[index].String, nil
}

func constantLuaValue(constant contentlua.Constant) luaValue {
	switch constant.Kind {
	case contentlua.ConstantNil:
		return luaValue{kind: luaNil}
	case contentlua.ConstantBoolean:
		return booleanLuaValue(constant.IsTrue)
	case contentlua.ConstantNumber:
		return numberLuaValue(constant.Number)
	case contentlua.ConstantString:
		return stringLuaValue(constant.String)
	default:
		return luaValue{kind: luaNil}
	}
}

func valueLuaKey(field luaValue) luaKey {
	return luaKey{kind: field.kind, number: field.number, text: field.text, isTrue: field.isTrue}
}

func stringLuaKey(text string) luaKey        { return valueLuaKey(stringLuaValue(text)) }
func numberLuaKey(number float64) luaKey     { return valueLuaKey(numberLuaValue(number)) }
func stringLuaValue(text string) luaValue    { return luaValue{kind: luaString, text: text} }
func numberLuaValue(number float64) luaValue { return luaValue{kind: luaNumber, number: number} }
func booleanLuaValue(isTrue bool) luaValue   { return luaValue{kind: luaBoolean, isTrue: isTrue} }
func tableLuaValue(table *luaTable) luaValue { return luaValue{kind: luaTableValue, table: table} }
func closureLuaValue(closure *luaClosure) luaValue {
	return luaValue{kind: luaClosureValue, closure: closure}
}

func isLuaTruthy(field luaValue) bool {
	return field.kind != luaNil && (field.kind != luaBoolean || field.isTrue)
}

func luaEqual(left, right luaValue) bool {
	if left.kind != right.kind {
		return false
	}
	switch left.kind {
	case luaNil:
		return true
	case luaBoolean:
		return left.isTrue == right.isTrue
	case luaNumber:
		return left.number == right.number
	case luaString:
		return left.text == right.text
	case luaTableValue:
		return left.table == right.table
	case luaClosureValue:
		return left.closure == right.closure
	default:
		return false
	}
}

func luaConcatText(field luaValue) (string, error) {
	switch field.kind {
	case luaString:
		return field.text, nil
	case luaNumber:
		return strconv.FormatFloat(field.number, 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("got %s", field.kind)
	}
}

func luaSeconds(seconds float64) time.Duration {
	microseconds := math.Round(seconds * float64(time.Second/time.Microsecond))
	return time.Duration(microseconds) * time.Microsecond
}

func (kind luaValueKind) String() string {
	switch kind {
	case luaNil:
		return "nil"
	case luaBoolean:
		return "boolean"
	case luaNumber:
		return "number"
	case luaString:
		return "string"
	case luaTableValue:
		return "table"
	case luaClosureValue:
		return "closure"
	case luaNativeValue:
		return "native"
	default:
		return fmt.Sprintf("kind_%d", kind)
	}
}

func luaKinds(arguments []luaValue) string {
	kinds := make([]luaValueKind, len(arguments))
	for index, argument := range arguments {
		kinds[index] = argument.kind
	}
	return fmt.Sprint(kinds)
}

func luaFieldText(field luaValue) string {
	switch field.kind {
	case luaString:
		return field.text
	case luaNumber:
		return fmt.Sprintf("%g", field.number)
	default:
		return field.kind.String()
	}
}
