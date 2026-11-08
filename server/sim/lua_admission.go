package sim

import (
	"errors"
	"fmt"
	"math"
)

// LuaModifierAdmission is captured by the authority while its target is
// locked. Lua decides admission; the caller commits the accepted status under
// that same lock so an immunity change cannot race the decision.
type LuaModifierAdmission struct {
	ImmuneToSleep   float64
	ImmuneToSilence float64
	IsChainGame     bool
}

// luaControlAdmission supplies the two authored control-modifier predicates.
// It does not run their activation, Tick, or mutation callbacks.
type luaControlAdmission struct {
	compiler *luaCompiler
	callback *luaClosure
	snapshot LuaModifierAdmission
}

func newLuaControlAdmission(compiler *luaCompiler) (*luaControlAdmission, error) {
	attribute := compiler.global.get(stringLuaKey("nAttribute"))
	attributeEnum := compiler.global.get(stringLuaKey("nAttributeType"))
	game := compiler.global.get(stringLuaKey("nGameSimulator"))
	if attribute.kind != luaTableValue || attributeEnum.kind != luaTableValue || game.kind != luaTableValue {
		return nil, errors.New("modifier admission native table unavailable")
	}
	admission := &luaControlAdmission{compiler: compiler}
	// The callback consumes identities, not native attribute offsets. The
	// authority supplies actual target attributes in the typed snapshot.
	attributeEnum.table.set(stringLuaKey("ImmuneToSleep"), stringLuaValue("ImmuneToSleep"))
	attributeEnum.table.set(stringLuaKey("ImmuneToSilence"), stringLuaValue("ImmuneToSilence"))
	compiler.setNative(attribute.table, "GetAttributeValue", admission.attribute)
	compiler.setNative(game.table, "IsChainGame", admission.isChainGame)
	return admission, nil
}

func (e *luaControlAdmission) prepare() error {
	if e.compiler.registeredModifier == nil {
		return errors.New("modifier admission registration unavailable")
	}
	callback := e.compiler.registeredModifier.get(stringLuaKey("CanUseModifier"))
	if callback.kind != luaClosureValue || callback.closure == nil ||
		callback.closure.prototype.ParameterCount != 3 || len(callback.closure.upvalues) != 0 {
		return errors.New("modifier admission callback invalid")
	}
	e.callback = callback.closure
	return nil
}

func (e *luaControlAdmission) check(req LuaModifierAdmission) (bool, error) {
	if e == nil || e.compiler == nil || e.callback == nil {
		return false, errors.New("modifier admission stopped")
	}
	if math.IsNaN(req.ImmuneToSleep) || math.IsInf(req.ImmuneToSleep, 0) ||
		math.IsNaN(req.ImmuneToSilence) || math.IsInf(req.ImmuneToSilence, 0) {
		return false, errors.New("modifier admission attribute invalid")
	}
	e.snapshot = req
	defer e.clearSnapshot()
	e.compiler.instructionBudget = defaultLuaInstructionBudget
	e.compiler.program.Provenance.FunctionName = e.compiler.modifierName + ".CanUseModifier"
	// Both verified callbacks read only the first (target) argument. The
	// target is an opaque handle resolved exclusively against this snapshot.
	results, err := e.compiler.callClosure(e.callback, []luaValue{
		stringLuaValue("modifierTarget"), {kind: luaNil}, {kind: luaNil},
	})
	if err != nil {
		return false, fmt.Errorf("modifierAdmission: %w", err)
	}
	if len(results) != 1 || results[0].kind != luaBoolean || len(e.compiler.program.Steps) != 0 {
		return false, errors.New("modifier admission result invalid")
	}
	return results[0].isTrue, nil
}

func (e *luaControlAdmission) clearSnapshot() {
	e.snapshot = LuaModifierAdmission{}
}

func (e *luaControlAdmission) stop() {
	if e == nil {
		return
	}
	e.compiler = nil
	e.callback = nil
	e.clearSnapshot()
}

func (e *luaControlAdmission) attribute(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 2 || arguments[0].kind != luaString || arguments[0].text != "modifierTarget" ||
		arguments[1].kind != luaString {
		return nil, errors.New("modifier admission attribute arguments")
	}
	switch arguments[1].text {
	case "ImmuneToSleep":
		return []luaValue{numberLuaValue(e.snapshot.ImmuneToSleep)}, nil
	case "ImmuneToSilence":
		return []luaValue{numberLuaValue(e.snapshot.ImmuneToSilence)}, nil
	default:
		return nil, fmt.Errorf("modifierAdmissionAttribute: %s", arguments[1].text)
	}
}

func (e *luaControlAdmission) isChainGame(arguments []luaValue) ([]luaValue, error) {
	if len(arguments) != 0 {
		return nil, errors.New("modifier admission game arguments")
	}
	return []luaValue{booleanLuaValue(e.snapshot.IsChainGame)}, nil
}
