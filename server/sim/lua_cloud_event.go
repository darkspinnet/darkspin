package sim

import (
	"errors"
	"fmt"
	"sync"
)

// LuaSleepingCloudAbility retains the caster ability's registered HandleEvent
// closure. It is distinct from the SleepingCloud_SleepModifier child, whose
// TookDamage subscription belongs to each sleeping recipient.
type LuaSleepingCloudAbility struct {
	mutex    sync.Mutex
	compiler *luaCompiler
	callback *luaClosure
}

func NewLuaSleepingCloudAbility(root LuaBytecode) (runtime *LuaSleepingCloudAbility, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			runtime = nil
			err = fmt.Errorf("cloudEventPanic: %v", recovered)
		}
	}()
	compiler, err := newLuaCompiler(LuaAbilityInput{Root: root, Role: "cloudCaster"})
	if err != nil {
		return nil, fmt.Errorf("cloudEventCompiler: %w", err)
	}
	runtime = &LuaSleepingCloudAbility{compiler: compiler}
	err = runtime.bind()
	if err != nil {
		return nil, fmt.Errorf("cloudEventBind: %w", err)
	}
	err = compiler.executeChunk(compiler.moduleChunks["$root"])
	if err != nil {
		return nil, fmt.Errorf("cloudEventRoot: %w", err)
	}
	ability := compiler.global.get(stringLuaKey("nAbility_SleepingCloud"))
	if ability.kind != luaTableValue || ability.table == nil || compiler.registeredAbility != ability.table {
		return nil, errors.New("cloud ability registration invalid")
	}
	subscription := ability.table.get(stringLuaKey("handledEvents"))
	if subscription.kind != luaString || subscription.text != "StartedNewAction" {
		return nil, errors.New("cloud ability event subscription invalid")
	}
	// Build-103398 registers the caster callback in numeric slot4, while the
	// child modifier uses the separate nAbilityFns.HandleEvent table key.
	callback := ability.table.get(numberLuaKey(4))
	if callback.kind != luaClosureValue || callback.closure == nil ||
		callback.closure.prototype.ParameterCount != 1 || len(callback.closure.upvalues) != 0 {
		return nil, errors.New("cloud ability event callback invalid")
	}
	runtime.callback = callback.closure
	return runtime, nil
}

func (e *LuaSleepingCloudAbility) bind() error {
	ability := e.compiler.global.get(stringLuaKey("nAbility"))
	eventEnum := e.compiler.global.get(stringLuaKey("nAbilityEventFlags"))
	callbackEnum := e.compiler.global.get(stringLuaKey("nAbilityFns"))
	debuffEnum := e.compiler.global.get(stringLuaKey("nDebuffDescriptors"))
	if ability.kind != luaTableValue || eventEnum.kind != luaTableValue ||
		callbackEnum.kind != luaTableValue || debuffEnum.kind != luaTableValue {
		return errors.New("cloud event native table unavailable")
	}
	// Event identity is opaque at this callback boundary. No fabricated native
	// numeric event mask or unused event-data slots enter the authored code.
	eventEnum.table.set(stringLuaKey("StartedNewAction"), stringLuaValue("StartedNewAction"))
	eventEnum.table.set(stringLuaKey("TookDamage"), stringLuaValue("TookDamage"))
	callbackEnum.table.set(stringLuaKey("HandleEvent"), stringLuaValue("HandleEvent"))
	debuffEnum.table.set(stringLuaKey("IsSleep"), stringLuaValue("IsSleep"))
	e.compiler.setNative(ability.table, "GetAbilityEventType", e.eventType)
	return nil
}

// StartedNewAction delivers an already accepted caster action to the original
// callback and returns its boolean. False asks gameplay to end this exact
// ability run. The caller commits cleanup and then Stop; this stateless event
// can be retried after a failed packet encoding without replaying activation.
func (e *LuaSleepingCloudAbility) StartedNewAction() (isRunning bool, err error) {
	if e == nil {
		return false, errors.New("nil cloud ability runtime")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			isRunning = false
			err = fmt.Errorf("cloudEventPanic: %v", recovered)
		}
		if err != nil {
			e.stopLocked()
		}
	}()
	if e.compiler == nil || e.callback == nil {
		return false, errors.New("cloud ability runtime stopped")
	}
	e.compiler.instructionBudget = defaultLuaInstructionBudget
	e.compiler.program.Provenance.FunctionName = "nAbility_SleepingCloud.HandleEvent"
	results, err := e.compiler.callClosure(e.callback, []luaValue{stringLuaValue("cloudStartedNewAction")})
	if err != nil {
		return false, fmt.Errorf("cloudEventCall: %w", err)
	}
	if len(results) != 1 || results[0].kind != luaBoolean || len(e.compiler.program.Steps) != 0 {
		return false, errors.New("cloud ability event result invalid")
	}
	return results[0].isTrue, nil
}

func (e *LuaSleepingCloudAbility) eventType(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 1 || arguments[0].kind != luaString || arguments[0].text != "cloudStartedNewAction" {
		return nil, errors.New("cloud event type arguments")
	}
	return []luaValue{stringLuaValue("StartedNewAction")}, nil
}

func (e *LuaSleepingCloudAbility) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.stopLocked()
}

func (e *LuaSleepingCloudAbility) stopLocked() {
	e.compiler = nil
	e.callback = nil
}
