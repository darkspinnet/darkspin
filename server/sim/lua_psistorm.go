package sim

import (
	"errors"
	"fmt"
	"sync"
)

// LuaPsistormModifier evaluates the real PsistormDebuff.CanUseModifier
// callback. Status application and its accepted-handle ledger remain in Go.
type LuaPsistormModifier struct {
	mutex     sync.Mutex
	admission *luaControlAdmission
}

func NewLuaPsistormModifier(root LuaBytecode) (runtime *LuaPsistormModifier, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			runtime = nil
			err = fmt.Errorf("psistormPanic: %v", recovered)
		}
	}()
	compiler, err := newLuaCompiler(LuaAbilityInput{
		Root: root, Role: "modifierTarget",
		// These modules provide helpers used only by the unexecuted ability
		// activation. The registered CanUseModifier has no module dependency.
		preloadedModules: map[string]bool{
			"Lua!PrivateTableUtils.lua": true,
			"Lua!AuraUtils.lua":         true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("psistormCompiler: %w", err)
	}
	admission, err := newLuaControlAdmission(compiler)
	if err != nil {
		return nil, fmt.Errorf("psistormBind: %w", err)
	}
	err = compiler.executeChunk(compiler.moduleChunks["$root"])
	if err != nil {
		return nil, fmt.Errorf("psistormRoot: %w", err)
	}
	if compiler.modifierName != "PsistormDebuff" {
		return nil, errors.New("psistorm modifier registration invalid")
	}
	err = admission.prepare()
	if err != nil {
		return nil, fmt.Errorf("psistormAdmission: %w", err)
	}
	return &LuaPsistormModifier{admission: admission}, nil
}

func (e *LuaPsistormModifier) CanUse(req LuaModifierAdmission) (isAccepted bool, err error) {
	if e == nil {
		return false, errors.New("nil psistorm modifier runtime")
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			isAccepted = false
			err = fmt.Errorf("psistormPanic: %v", recovered)
		}
		if err != nil {
			e.admission.stop()
		}
	}()
	isAccepted, err = e.admission.check(req)
	if err != nil {
		return false, fmt.Errorf("psistormCanUse: %w", err)
	}
	return isAccepted, nil
}

func (e *LuaPsistormModifier) Stop() {
	if e == nil {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.admission.stop()
}
