package sim

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const orbBudgetUnit uint32 = 100

type OrbDropKind uint8

const (
	HealthOrbDrop OrbDropKind = iota
	ManaOrbDrop
	ResurrectionOrbDrop
)

type OrbResourceSample struct {
	HitPoint        float32
	MaximumHitPoint float32
	Mana            float32
	MaximumMana     float32
}

type OrbDropInput struct {
	ScaledBudget          uint32
	SourceRole            Role
	SourcePosition        Position
	SimulationTime        time.Duration
	PickupLifetime        time.Duration
	Roster                []OrbResourceSample
	Destinations          []Position
	PickupRoles           []Role
	Random                *SimulatorRandom
	IsResurrectionEnabled bool
}

type OrbPickupRequest struct {
	Role        Role
	Kind        OrbDropKind
	NounName    string
	EventName   string
	Position    Position
	Destination Position
	Lob         CrystalLob
	ExpiresAt   time.Duration
}

type OrbDropWorldRequest struct {
	SourceRole Role
	Pickups    []OrbPickupRequest
}

type orbDropBuilder struct {
	input     OrbDropInput
	health    float64
	mana      float64
	expiresAt time.Duration
	seenRoles map[Role]struct{}
	request   OrbDropWorldRequest
}

// BuildOrbDropWorldRequest applies the recovered native orb budget and lets
// the caller enable the resurrection override on the health side.
func BuildOrbDropWorldRequest(input OrbDropInput) (OrbDropWorldRequest, error) {
	dropCount := input.ScaledBudget / orbBudgetUnit
	remainder := input.ScaledBudget % orbBudgetUnit
	expiresAt := input.SimulationTime + input.PickupLifetime
	maximumDrop := dropCount
	if remainder != 0 {
		maximumDrop++
	}
	if input.Random == nil || input.SourceRole == "" || input.SimulationTime < 0 ||
		input.PickupLifetime <= 0 || expiresAt <= input.SimulationTime ||
		!isFinitePosition(input.SourcePosition) || len(input.Roster) == 0 ||
		uint32(len(input.Destinations)) < maximumDrop || uint32(len(input.PickupRoles)) < maximumDrop {
		return OrbDropWorldRequest{}, errors.New("invalid orb drop input")
	}
	healthWeight, manaWeight, err := orbResourceWeights(input.Roster)
	if err != nil {
		return OrbDropWorldRequest{}, fmt.Errorf("resourceWeight: %w", err)
	}
	builder := orbDropBuilder{
		input: input, health: healthWeight, mana: manaWeight,
		expiresAt: expiresAt, seenRoles: make(map[Role]struct{}, maximumDrop),
		request: OrbDropWorldRequest{
			SourceRole: input.SourceRole,
			Pickups:    make([]OrbPickupRequest, 0, maximumDrop),
		},
	}
	for index := uint32(0); index < dropCount; index++ {
		err = builder.append(index)
		if err != nil {
			return OrbDropWorldRequest{}, fmt.Errorf("guaranteedDrop: %w", err)
		}
	}
	if remainder == 0 {
		return builder.request, nil
	}
	chance, err := input.Random.Index(orbBudgetUnit)
	if err != nil {
		return OrbDropWorldRequest{}, fmt.Errorf("remainderDraw: %w", err)
	}
	if chance >= remainder {
		return builder.request, nil
	}
	err = builder.append(dropCount)
	if err != nil {
		return OrbDropWorldRequest{}, fmt.Errorf("remainderDrop: %w", err)
	}
	return builder.request, nil
}

func (e *orbDropBuilder) append(index uint32) error {
	role := e.input.PickupRoles[index]
	if role == "" {
		return fmt.Errorf("pickupRole[%d]: empty", index)
	}
	if _, isDuplicate := e.seenRoles[role]; isDuplicate {
		return fmt.Errorf("pickupRole[%d]: duplicate", index)
	}
	e.seenRoles[role] = struct{}{}
	destination := e.input.Destinations[index]
	if !isFinitePosition(destination) {
		return fmt.Errorf("destination[%d]: invalid", index)
	}
	kind := selectOrbDropKind(e.input.Random.Float64(), e.health, e.mana)
	if kind == HealthOrbDrop && e.input.IsResurrectionEnabled {
		kind = ResurrectionOrbDrop
	}
	lob, err := BuildDropLob(
		e.input.SimulationTime, e.input.SourcePosition, destination,
	)
	if err != nil {
		return fmt.Errorf("lob[%d]: %w", index, err)
	}
	nounName := "HealthOrb.Noun"
	eventName := "health_orb_drop.ServerEventDef"
	if kind == ManaOrbDrop {
		nounName = "manaorb.Noun"
		eventName = "mana_orb_drop.ServerEventDef"
	}
	if kind == ResurrectionOrbDrop {
		nounName = "ResurrectOrb.Noun"
		eventName = "resurrect_orb_drop.ServerEventDef"
	}
	e.request.Pickups = append(e.request.Pickups, OrbPickupRequest{
		Role: role, Kind: kind, NounName: nounName, EventName: eventName,
		Position: e.input.SourcePosition, Destination: destination, Lob: lob,
		ExpiresAt: e.expiresAt,
	})
	return nil
}

// DroppedOrbLifetime owns one content-supplied pickup expiry on the monotonic
// simulator clock. Collection cancels the deadline because its own transaction
// publishes deletion. Explicit teardown publishes one despawn while the scope
// is active; invalidated phase/session scopes suppress stale cleanup.
type DroppedOrbLifetime struct {
	simulator  *Simulator
	scope      CancelScope
	role       Role
	provenance Provenance
	taskID     TaskID
	isLive     bool
}

func StartDroppedOrbLifetime(
	simulator *Simulator, scope CancelScope, request OrbPickupRequest, provenance Provenance,
) (*DroppedOrbLifetime, error) {
	if simulator == nil {
		return nil, errors.New("nil simulator")
	}
	if !simulator.isScopeActive(scope) || request.Role == "" || scope.role != request.Role ||
		request.ExpiresAt <= simulator.Now() {
		return nil, errors.New("invalid dropped orb lifetime")
	}
	lifetime := &DroppedOrbLifetime{
		simulator: simulator, scope: scope, role: request.Role,
		provenance: provenance, isLive: true,
	}
	taskID, err := simulator.Schedule(request.ExpiresAt-simulator.Now(), scope, func(*Simulator) error {
		if !lifetime.isLive {
			return nil
		}
		err := simulator.EmitScoped(DespawnIntent{Role: lifetime.role}, lifetime.provenance, lifetime.scope)
		if err != nil {
			return fmt.Errorf("orbDespawn: %w", err)
		}
		lifetime.taskID = 0
		lifetime.isLive = false
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("expirySchedule: %w", err)
	}
	lifetime.taskID = taskID
	return lifetime, nil
}

// Collect retires the expiry without emitting a second deletion.
func (l *DroppedOrbLifetime) Collect() bool {
	if l == nil || !l.isLive {
		return false
	}
	l.simulator.Cancel(l.taskID)
	l.taskID = 0
	l.isLive = false
	return true
}

func (l *DroppedOrbLifetime) Cancel() error {
	if l == nil || !l.isLive {
		return nil
	}
	l.simulator.Cancel(l.taskID)
	l.taskID = 0
	l.isLive = false
	if !l.simulator.isScopeActive(l.scope) {
		return nil
	}
	err := l.simulator.EmitScoped(DespawnIntent{Role: l.role}, l.provenance, l.scope)
	if err != nil {
		return fmt.Errorf("cancelDespawn: %w", err)
	}
	return nil
}

func orbResourceWeights(roster []OrbResourceSample) (float64, float64, error) {
	var healthFraction float64
	var manaFraction float64
	for index, sample := range roster {
		if sample.MaximumHitPoint <= 0 || sample.MaximumMana <= 0 || sample.HitPoint < 0 || sample.Mana < 0 ||
			sample.HitPoint > sample.MaximumHitPoint || sample.Mana > sample.MaximumMana ||
			math.IsNaN(float64(sample.HitPoint)) || math.IsNaN(float64(sample.MaximumHitPoint)) ||
			math.IsNaN(float64(sample.Mana)) || math.IsNaN(float64(sample.MaximumMana)) ||
			math.IsInf(float64(sample.HitPoint), 0) || math.IsInf(float64(sample.MaximumHitPoint), 0) ||
			math.IsInf(float64(sample.Mana), 0) || math.IsInf(float64(sample.MaximumMana), 0) {
			return 0, 0, fmt.Errorf("roster[%d]: invalid", index)
		}
		healthFraction += float64(sample.HitPoint / sample.MaximumHitPoint)
		manaFraction += float64(sample.Mana / sample.MaximumMana)
	}
	healthFraction /= float64(len(roster))
	manaFraction /= float64(len(roster))
	return 2 + 5*(1-healthFraction), 2 + 5*(1-manaFraction), nil
}

func selectOrbDropKind(choice float64, healthWeight float64, manaWeight float64) OrbDropKind {
	if choice < healthWeight/(healthWeight+manaWeight) {
		return HealthOrbDrop
	}
	return ManaOrbDrop
}
