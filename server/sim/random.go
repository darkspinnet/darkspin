package sim

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

const simulatorRandomWordCount = 624

// SimulatorRandom reproduces the build-103 cGameSimulator MT19937 stream.
// Each draw is serialized so an instance-owned stream remains valid when
// commands from multiple campaign peers arrive concurrently.
type SimulatorRandom struct {
	mu    sync.Mutex
	state [simulatorRandomWordCount]uint32
	index uint32
	draw  uint64
}

// RandomSnapshot preserves the complete MT19937 boundary needed to reproduce
// every later draw rather than only recording how many draws occurred.
type RandomSnapshot struct {
	Words     [simulatorRandomWordCount]uint32
	Index     uint32
	DrawCount uint64
}

type DamageRange struct {
	Minimum float32
	Maximum float32
}

func NewSimulatorRandom(seed uint32) *SimulatorRandom {
	random := &SimulatorRandom{}
	x := seed | 1
	for index := range random.state {
		y := uint32(69069 * x)
		random.state[index] = (x & 0xffff0000) | (y >> 16)
		x = uint32(69069*y + 69070)
	}
	random.twistLocked()
	return random
}

// NewSimulatorRandomFromSnapshot restores the exact point in a simulator
// stream captured at a durable gameplay boundary.
func NewSimulatorRandomFromSnapshot(snapshot RandomSnapshot) (*SimulatorRandom, error) {
	if snapshot.Index > simulatorRandomWordCount {
		return nil, errors.New("simulator random index invalid")
	}
	isStatePresent := false
	for _, word := range snapshot.Words {
		if word != 0 {
			isStatePresent = true
			break
		}
	}
	if !isStatePresent {
		return nil, errors.New("simulator random state unavailable")
	}
	return &SimulatorRandom{
		state: snapshot.Words, index: snapshot.Index, draw: snapshot.DrawCount,
	}, nil
}

func (r *SimulatorRandom) Uint32() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.uint32Locked()
}

func (r *SimulatorRandom) uint32Locked() uint32 {
	if r.index >= simulatorRandomWordCount {
		r.twistLocked()
	}
	y := r.state[r.index]
	r.index++
	r.draw++
	y ^= y >> 11
	y ^= (y << 7) & 0x9d2c5680
	y ^= (y << 15) & 0xefc60000
	y ^= y >> 18
	return y
}

func (r *SimulatorRandom) Index(count uint32) (uint32, error) {
	if r == nil {
		return 0, errors.New("nil simulator random")
	}
	if count == 0 {
		return 0, errors.New("empty random range")
	}
	return uint32((uint64(count) * uint64(r.Uint32())) >> 32), nil
}

func (r *SimulatorRandom) Float64() float64 {
	if r == nil {
		return 0
	}
	unit := float64(int32(r.Uint32()))*math.Ldexp(1, -32) + 0.5
	if unit >= 1 {
		return 0
	}
	return unit
}

func (r *SimulatorRandom) DrawCount() uint64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.draw
}

func (r *SimulatorRandom) Snapshot() RandomSnapshot {
	if r == nil {
		return RandomSnapshot{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return RandomSnapshot{
		Words: r.state, Index: r.index, DrawCount: r.draw,
	}
}

// ValidateRankDamageRange validates the existing discrete selection policy.
// Fixed amounts retain their exact fractional value. Variable fractional bounds
// are quantized outward only when sampled; the lost server's roll distribution
// is unverified, so this preserves the existing compatibility policy.
func ValidateRankDamageRange(damageRange DamageRange) error {
	minimum := float64(damageRange.Minimum)
	maximum := float64(damageRange.Maximum)
	if math.IsNaN(minimum) || math.IsNaN(maximum) ||
		math.IsInf(minimum, 0) || math.IsInf(maximum, 0) || minimum > maximum {
		return errors.New("invalid damage range")
	}
	if minimum < float64(math.MinInt64) || maximum >= float64(math.MaxInt64) {
		return errors.New("damage range overflow")
	}
	if minimum == maximum {
		return nil
	}
	span := math.Ceil(maximum) - math.Floor(minimum) + 1
	if span < 1 || span > math.MaxUint32 {
		return errors.New("damage range overflow")
	}
	return nil
}

func SelectRankDamage(random *SimulatorRandom, damageRange DamageRange) (float32, error) {
	if random == nil {
		return 0, errors.New("nil simulator random")
	}
	err := ValidateRankDamageRange(damageRange)
	if err != nil {
		return 0, fmt.Errorf("damageRange: %w", err)
	}
	if damageRange.Minimum == damageRange.Maximum {
		// Existing fixed integer rolls also consumed one draw. Preserve that
		// stream position without inventing variance in a fixed fractional hit.
		offset, err := random.Index(1)
		if err != nil {
			return 0, fmt.Errorf("fixedIndex: %w", err)
		}
		if offset != 0 {
			return 0, errors.New("fixed damage index invalid")
		}
		return damageRange.Minimum, nil
	}
	minimumInteger := int64(math.Floor(float64(damageRange.Minimum)))
	maximumInteger := int64(math.Ceil(float64(damageRange.Maximum)))
	span := maximumInteger - minimumInteger + 1
	offset, err := random.Index(uint32(span))
	if err != nil {
		return 0, fmt.Errorf("damageIndex: %w", err)
	}
	return float32(minimumInteger + int64(offset)), nil
}

func (r *SimulatorRandom) twistLocked() {
	for index := 0; index < simulatorRandomWordCount; index++ {
		next := (index + 1) % simulatorRandomWordCount
		far := (index + 397) % simulatorRandomWordCount
		y := (r.state[index] & 0x80000000) | (r.state[next] & 0x7fffffff)
		r.state[index] = r.state[far] ^ (y >> 1)
		if y&1 != 0 {
			r.state[index] ^= 0x9908b0df
		}
	}
	r.index = 0
}
