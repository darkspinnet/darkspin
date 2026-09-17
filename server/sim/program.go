package sim

import (
	"errors"
	"fmt"
	"time"
)

// Step is one normalized operation recovered from a packaged Lua chunk.
type Step interface {
	programStep()
	instructionOffset() uint32
	isInstructionOffsetKnown() bool
}

type WaitStep struct {
	Duration      time.Duration
	Offset        uint32
	IsOffsetKnown bool
	Provenance    *Provenance
}

func (WaitStep) programStep()                   {}
func (step WaitStep) instructionOffset() uint32 { return step.Offset }
func (step WaitStep) isInstructionOffsetKnown() bool {
	return step.IsOffsetKnown
}

type EmitStep struct {
	Intent        Intent
	Offset        uint32
	IsOffsetKnown bool
	Provenance    *Provenance
}

func (EmitStep) programStep()                   {}
func (step EmitStep) instructionOffset() uint32 { return step.Offset }
func (step EmitStep) isInstructionOffsetKnown() bool {
	return step.IsOffsetKnown
}

// InternalObject records compiler-local object ownership used by authored Lua
// jobs. These objects own worker threads but are not replicated world spawns.
type InternalObject struct {
	ID                uint32
	NounReference     string
	NounID            uint32
	IsMarkedForDelete bool
}

// Program is an allowlisted, instruction-derived simulation plan for one Lua
// function. It retains bytecode provenance but contains no executable host
// access.
type Program struct {
	Provenance      Provenance
	Steps           []Step
	InternalObjects []InternalObject
}

// RunProgram starts a normalized Lua program in the supplied cancellation
// scope.
func RunProgram(simulator *Simulator, scope CancelScope, program Program) error {
	if simulator == nil {
		return errors.New("nil simulator")
	}
	err := validateProgram(program)
	if err != nil {
		return fmt.Errorf("programValidate: %w", err)
	}
	if !simulator.isScopeActive(scope) {
		return errors.New("inactive scope")
	}
	err = runProgramSteps(simulator, scope, program, 0)
	if err != nil {
		return fmt.Errorf("programRun: %w", err)
	}
	return nil
}

func validateProgram(program Program) error {
	for index, step := range program.Steps {
		if step == nil {
			return fmt.Errorf("nilStep[%d]", index)
		}
		switch instruction := step.(type) {
		case WaitStep:
			if instruction.Duration < 0 {
				return fmt.Errorf("negativeWait[%d]", index)
			}
		case EmitStep:
			if instruction.Intent == nil {
				return fmt.Errorf("nilIntent[%d]", index)
			}
		default:
			return fmt.Errorf("stepUnsupported[%d]: %T", index, step)
		}
	}
	return nil
}

func runProgramSteps(simulator *Simulator, scope CancelScope, program Program, start int) error {
	for index := start; index < len(program.Steps); index++ {
		step := program.Steps[index]
		if step == nil {
			return fmt.Errorf("nilStep[%d]", index)
		}
		provenance := program.Provenance
		switch instruction := step.(type) {
		case WaitStep:
			if instruction.Provenance != nil {
				provenance = *instruction.Provenance
			}
		case EmitStep:
			if instruction.Provenance != nil {
				provenance = *instruction.Provenance
			}
		}
		provenance.InstructionOffset = step.instructionOffset()
		provenance.IsInstructionOffsetKnown = step.isInstructionOffsetKnown()
		switch instruction := step.(type) {
		case WaitStep:
			if instruction.Duration < 0 {
				return fmt.Errorf("negativeWait[%d]", index)
			}
			err := simulator.EmitScoped(WaitIntent{Duration: instruction.Duration}, provenance, scope)
			if err != nil {
				return fmt.Errorf("waitEmit[%d]: %w", index, err)
			}
			_, err = simulator.Schedule(instruction.Duration, scope, func(current *Simulator) error {
				err := runProgramSteps(current, scope, program, index+1)
				if err != nil {
					return fmt.Errorf("programResume[%d]: %w", index, err)
				}
				return nil
			})
			if err != nil {
				return fmt.Errorf("waitSchedule[%d]: %w", index, err)
			}
			return nil
		case EmitStep:
			err := simulator.EmitScoped(instruction.Intent, provenance, scope)
			if err != nil {
				return fmt.Errorf("intentEmit[%d]: %w", index, err)
			}
		default:
			return fmt.Errorf("stepUnsupported[%d]: %T", index, step)
		}
	}
	return nil
}
