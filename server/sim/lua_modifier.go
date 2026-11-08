package sim

import (
	"errors"
	"fmt"
	"sync"
)

// LuaModifierRuntime owns one live modifier's private tables and retained
// trigger closures. It does not schedule callbacks or apply game mutations.
type LuaModifierRuntime struct {
	mutex             sync.Mutex
	compiler          *luaCompiler
	instructionBudget int
}

// LuaModifierTrigger supplies current authority at the callback boundary.
type LuaModifierTrigger struct {
	Kind             LuaTriggerCallback
	Role             Role
	IsModifierActive bool
	Query            LuaWorldQuery
}

// NewLuaModifierRuntime runs Activate once. Each live instance must have its
// own runtime; the returned program still requires authoritative dispatch.
func NewLuaModifierRuntime(req LuaModifierInput) (runtime *LuaModifierRuntime, program Program, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			runtime = nil
			program = Program{}
			err = fmt.Errorf("modifierPanic: %v", recovered)
		}
	}()
	if req.IsYieldAfterWait || req.YieldAfterWaitCount != 0 {
		return nil, Program{}, errors.New("retained modifier yield unsupported")
	}
	compiler, err := createLuaModifierCompiler(req, "")
	if err != nil {
		return nil, Program{}, fmt.Errorf("modifierCreate: %w", err)
	}
	budget := req.InstructionBudget
	if budget == 0 {
		budget = defaultLuaInstructionBudget
	}
	err = compiler.callModifierCallback("Activate")
	if err != nil {
		return nil, Program{}, fmt.Errorf("modifierActivate: %w", err)
	}
	err = validateRetainedModifierProgram(compiler.program)
	if err != nil {
		return nil, Program{}, fmt.Errorf("activateProgram: %w", err)
	}
	compiler.input.Query = nil
	return &LuaModifierRuntime{compiler: compiler, instructionBudget: budget}, drainLuaProgram(compiler), nil
}

// Trigger executes the actual retained callback, without replaying Activate.
// A failed callback permanently retires the instance because private state may
// have changed before the error. The caller must also Stop on dispatch failure.
func (e *LuaModifierRuntime) Trigger(req LuaModifierTrigger) (program Program, err error) {
	if e == nil {
		return Program{}, errors.New("nil modifier runtime")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			program = Program{}
			err = fmt.Errorf("modifierPanic: %v", recovered)
		}
		if err != nil {
			e.compiler = nil
		}
	}()
	if e.compiler == nil {
		return Program{}, errors.New("modifier runtime stopped")
	}
	if req.Role == "" {
		return Program{}, errors.New("empty trigger role")
	}
	index := -1
	switch req.Kind {
	case LuaTriggerEnter:
		index = 0
	case LuaTriggerExit:
		index = 1
	case LuaTriggerStay:
		index = 2
	default:
		return Program{}, fmt.Errorf("triggerKind: %s", req.Kind)
	}
	compiler := e.compiler
	defer func() { compiler.input.Query = nil }()
	if index >= len(compiler.triggerCallbacks) || compiler.triggerCallbacks[index] == nil {
		return Program{}, fmt.Errorf("triggerMissing: %s", req.Kind)
	}
	compiler.input.triggerRole = req.Role
	compiler.input.isTriggerModifierActive = req.IsModifierActive
	compiler.input.Query = req.Query
	compiler.instructionBudget = e.instructionBudget
	compiler.program.Provenance.FunctionName = compiler.modifierName + ".Trigger" + string(req.Kind)
	_, err = compiler.callClosure(compiler.triggerCallbacks[index], []luaValue{
		numberLuaValue(1), stringLuaValue(string(req.Role)),
	})
	if err != nil {
		return Program{}, fmt.Errorf("triggerCall: %w", err)
	}
	err = validateRetainedModifierProgram(compiler.program)
	if err != nil {
		return Program{}, fmt.Errorf("triggerProgram: %w", err)
	}
	return drainLuaProgram(compiler), nil
}

func validateRetainedModifierProgram(program Program) error {
	for _, step := range program.Steps {
		if _, isWait := step.(WaitStep); isWait {
			return errors.New("retained modifier wait unsupported")
		}
	}
	return nil
}

// Stop releases private state and makes later callbacks fail closed.
func (e *LuaModifierRuntime) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	e.compiler = nil
	e.mutex.Unlock()
}
