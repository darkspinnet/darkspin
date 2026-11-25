package sim

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

// LuaSleepDamage represents an already accepted TookDamage event. The
// packaged sleep callback reads descriptor slot three; it does not read the
// resolved damage amount or decide whether combat should emit that event.
type LuaSleepDamage struct {
	DescriptorMask uint32
}

// LuaSleepingCloudModifier retains the registered sleep modifier and its real
// HandleEvent closure for one accepted sleep membership. Activation/status
// authority remains with Go; this callback has no private-table dependency.
type LuaSleepingCloudModifier struct {
	mutex     sync.Mutex
	compiler  *luaCompiler
	callback  *luaClosure
	damage    LuaSleepDamage
	admission *luaControlAdmission
}

func NewLuaSleepingCloudModifier(root LuaBytecode) (runtime *LuaSleepingCloudModifier, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			runtime = nil
			err = fmt.Errorf("sleepPanic: %v", recovered)
		}
	}()
	compiler, err := newLuaCompiler(LuaAbilityInput{Root: root, Role: "sleepTarget"})
	if err != nil {
		return nil, fmt.Errorf("sleepCompiler: %w", err)
	}
	runtime = &LuaSleepingCloudModifier{compiler: compiler}
	err = runtime.bind()
	if err != nil {
		return nil, fmt.Errorf("sleepBind: %w", err)
	}
	runtime.admission, err = newLuaControlAdmission(compiler)
	if err != nil {
		return nil, fmt.Errorf("sleepAdmissionBind: %w", err)
	}
	err = compiler.executeChunk(compiler.moduleChunks["$root"])
	if err != nil {
		return nil, fmt.Errorf("sleepRoot: %w", err)
	}
	if compiler.modifierName != "SleepingCloud_SleepModifier" || compiler.registeredModifier == nil {
		return nil, errors.New("sleep modifier registration invalid")
	}
	callback := compiler.registeredModifier.get(stringLuaKey("HandleEvent"))
	if callback.kind != luaClosureValue || callback.closure == nil ||
		callback.closure.prototype.ParameterCount != 1 || len(callback.closure.upvalues) != 0 {
		return nil, errors.New("sleep event callback invalid")
	}
	event := compiler.registeredModifier.get(stringLuaKey("handledEvents"))
	if event.kind != luaString || event.text != "TookDamage" {
		return nil, errors.New("sleep event subscription invalid")
	}
	runtime.callback = callback.closure
	err = runtime.admission.prepare()
	if err != nil {
		return nil, fmt.Errorf("sleepAdmission: %w", err)
	}
	return runtime, nil
}

func (e *LuaSleepingCloudModifier) CanUse(req LuaModifierAdmission) (isAccepted bool, err error) {
	if e == nil {
		return false, errors.New("nil sleep modifier runtime")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			isAccepted = false
			err = fmt.Errorf("sleepPanic: %v", recovered)
		}
		if err != nil {
			e.stopLocked()
		}
	}()
	isAccepted, err = e.admission.check(req)
	if err != nil {
		return false, fmt.Errorf("sleepCanUse: %w", err)
	}
	return isAccepted, nil
}

func (e *LuaSleepingCloudModifier) bind() error {
	compiler := e.compiler
	ability := compiler.global.get(stringLuaKey("nAbility"))
	callbackEnum := compiler.global.get(stringLuaKey("nAbilityFns"))
	eventEnum := compiler.global.get(stringLuaKey("nAbilityEventFlags"))
	debuffEnum := compiler.global.get(stringLuaKey("nDebuffDescriptors"))
	if ability.kind != luaTableValue || callbackEnum.kind != luaTableValue ||
		eventEnum.kind != luaTableValue || debuffEnum.kind != luaTableValue {
		return errors.New("sleep native table unavailable")
	}
	// These enum identities are opaque to this callback-only boundary. Keeping
	// them symbolic avoids inventing native numeric event/callback constants.
	callbackEnum.table.set(stringLuaKey("HandleEvent"), stringLuaValue("HandleEvent"))
	eventEnum.table.set(stringLuaKey("TookDamage"), stringLuaValue("TookDamage"))
	// The root also registers presentation metadata for IsSleep. Its numeric
	// debuff mask is not consumed by this event runtime.
	debuffEnum.table.set(stringLuaKey("IsSleep"), stringLuaValue("IsSleep"))
	compiler.setNative(ability.table, "GetAbilityEventType", e.eventType)
	compiler.setNative(ability.table, "GetAbilityEventIntData", e.eventIntData)
	compiler.setNative(ability.table, "CheckDescriptors_AnyMatch", e.descriptorMatch)
	return nil
}

// Damage returns the original HandleEvent boolean: false asks the authority
// to end this exact modifier. The caller commits its mutation and then Stop;
// a failed packet encoding may retry this stateless callback safely.
func (e *LuaSleepingCloudModifier) Damage(req LuaSleepDamage) (isActive bool, err error) {
	if e == nil {
		return false, errors.New("nil sleep modifier runtime")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			isActive = false
			err = fmt.Errorf("sleepPanic: %v", recovered)
		}
		e.damage = LuaSleepDamage{}
		if err != nil {
			e.stopLocked()
		}
	}()
	if e.compiler == nil || e.callback == nil {
		return false, errors.New("sleep modifier runtime stopped")
	}
	e.damage = req
	e.compiler.instructionBudget = defaultLuaInstructionBudget
	e.compiler.program.Provenance.FunctionName = e.compiler.modifierName + ".HandleEvent"
	results, err := e.compiler.callClosure(e.callback, []luaValue{stringLuaValue("sleepDamageEvent")})
	if err != nil {
		return false, fmt.Errorf("sleepEvent: %w", err)
	}
	if len(results) != 1 || results[0].kind != luaBoolean || len(e.compiler.program.Steps) != 0 {
		return false, errors.New("sleep event result invalid")
	}
	return results[0].isTrue, nil
}

func (e *LuaSleepingCloudModifier) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.stopLocked()
}

func (e *LuaSleepingCloudModifier) stopLocked() {
	e.admission.stop()
	e.compiler = nil
	e.callback = nil
	e.damage = LuaSleepDamage{}
}

func (e *LuaSleepingCloudModifier) eventType(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 1 || !isLuaSleepEvent(arguments[0]) {
		return nil, errors.New("sleep event type arguments")
	}
	return []luaValue{stringLuaValue("TookDamage")}, nil
}

func (e *LuaSleepingCloudModifier) eventIntData(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 2 || !isLuaSleepEvent(arguments[0]) ||
		arguments[1].kind != luaNumber || arguments[1].number != 3 {
		return nil, errors.New("sleep event data arguments")
	}
	return []luaValue{numberLuaValue(float64(e.damage.DescriptorMask))}, nil
}

func isLuaSleepEvent(event luaValue) bool {
	return event.kind == luaString && event.text == "sleepDamageEvent"
}

func (e *LuaSleepingCloudModifier) descriptorMatch(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 2 {
		return nil, errors.New("sleep descriptor arguments")
	}
	for _, argument := range arguments {
		if argument.kind != luaNumber || argument.number < 0 ||
			argument.number > float64(^uint32(0)) || argument.number != math.Trunc(argument.number) {
			return nil, errors.New("sleep descriptor mask invalid")
		}
	}
	isMatch := uint32(arguments[0].number)&uint32(arguments[1].number) != 0
	return []luaValue{booleanLuaValue(isMatch)}, nil
}
