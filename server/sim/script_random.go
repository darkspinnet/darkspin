package sim

import (
	"errors"
	"fmt"
	"math"
)

// ScriptRandom implements the native math.random binding against the caller's
// live simulator stream. It never seeds or copies an RNG. Invalid public
// ranges are rejected before drawing rather than reproducing native overflow.
func ScriptRandom(random *SimulatorRandom, arguments ...float64) (float64, error) {
	if random == nil {
		return 0, errors.New("script random unavailable")
	}
	if len(arguments) == 0 {
		return float64(float32(random.Float64())), nil
	}
	if len(arguments) > 2 {
		return 0, errors.New("script random argument count")
	}
	bounds := [2]int64{}
	for index, argument := range arguments {
		if math.IsNaN(argument) || math.IsInf(argument, 0) {
			return 0, fmt.Errorf("randomBound[%d]: nonfinite", index)
		}
		integer := math.Trunc(argument)
		if integer < math.MinInt32 || integer > math.MaxInt32 {
			return 0, fmt.Errorf("randomBound[%d]: out of range", index)
		}
		bounds[index] = int64(integer)
	}
	minimum := int64(1)
	width := bounds[0]
	if len(arguments) == 1 && width <= 0 {
		return 0, errors.New("script random empty range")
	}
	if len(arguments) == 2 {
		minimum = bounds[0]
		width = bounds[1] - minimum
		if width < 0 {
			return 0, errors.New("script random reversed range")
		}
	}
	// Native Index(0) still advances MT. Keep the general Index API's empty
	// range rejection while preserving that script-only equal-bound branch.
	if width == 0 {
		random.Uint32()
		return float64(minimum), nil
	}
	offset, err := random.Index(uint32(width))
	if err != nil {
		return 0, fmt.Errorf("scriptIndex: %w", err)
	}
	return float64(minimum + int64(offset)), nil
}

func (e *luaCompiler) scriptRandom(arguments []luaValue) ([]luaValue, error) {
	if e.input.waitCount > 0 {
		return nil, errors.New("script random requires execution after wait, not eager compilation")
	}
	if len(arguments) > 2 {
		return nil, errors.New("script random argument count")
	}
	numbers := make([]float64, len(arguments))
	for index, argument := range arguments {
		if argument.kind != luaNumber {
			return nil, fmt.Errorf("randomArgument[%d]: expected number", index)
		}
		numbers[index] = argument.number
	}
	number, err := ScriptRandom(e.input.Random, numbers...)
	if err != nil {
		return nil, fmt.Errorf("luaRandom: %w", err)
	}
	return []luaValue{numberLuaValue(number)}, nil
}
