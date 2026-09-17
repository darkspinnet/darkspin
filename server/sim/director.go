package sim

import (
	"errors"
	"fmt"
)

// Fact is a stable scenario-state measurement used by phase conditions.
type Fact string

// Comparison defines how a fact count satisfies a condition.
type Comparison uint8

const (
	ComparisonEqual Comparison = iota
	ComparisonAtLeast
	ComparisonAtMost
)

// Condition is a deterministic phase gate over one scenario fact.
type Condition struct {
	Fact       Fact
	Comparison Comparison
	Count      int64
}

// PhaseDefinition is the reusable state-machine contract for one scenario
// phase.
type PhaseDefinition struct {
	Name                 Phase
	EntryConditions      []Condition
	AcceptedCommands     []Command
	ActiveRoles          []Role
	CompletionConditions []Condition
	NextPhases           []Phase
}

// Director applies phase policy over one deterministic simulator.
type Director struct {
	simulator   *Simulator
	definitions map[Phase]PhaseDefinition
	activeRoles map[Role]struct{}
	facts       map[Fact]int64
}

// NewDirector validates the route and enters its initial phase.
func NewDirector(definitions []PhaseDefinition, initialPhase Phase) (*Director, error) {
	director, err := NewDirectorWithFacts(definitions, initialPhase, nil)
	if err != nil {
		return nil, fmt.Errorf("directorCreate: %w", err)
	}
	return director, nil
}

// NewDirectorWithFacts validates a route and enters its initial phase using a
// copied initial fact set.
func NewDirectorWithFacts(definitions []PhaseDefinition, initialPhase Phase, facts map[Fact]int64) (*Director, error) {
	if len(definitions) == 0 {
		return nil, errors.New("empty definitions")
	}
	director := &Director{
		simulator:   New(initialPhase),
		definitions: make(map[Phase]PhaseDefinition, len(definitions)),
		activeRoles: make(map[Role]struct{}),
		facts:       make(map[Fact]int64, len(facts)),
	}
	for fact, count := range facts {
		director.facts[fact] = count
	}
	for index, definition := range definitions {
		if definition.Name == "" {
			return nil, fmt.Errorf("emptyPhase[%d]", index)
		}
		if _, isFound := director.definitions[definition.Name]; isFound {
			return nil, fmt.Errorf("duplicatePhase[%s]", definition.Name)
		}
		err := validateConditions(definition.EntryConditions)
		if err != nil {
			return nil, fmt.Errorf("entryCondition[%s]: %w", definition.Name, err)
		}
		err = validateConditions(definition.CompletionConditions)
		if err != nil {
			return nil, fmt.Errorf("completionCondition[%s]: %w", definition.Name, err)
		}
		err = validateRoles(definition.ActiveRoles)
		if err != nil {
			return nil, fmt.Errorf("activeRole[%s]: %w", definition.Name, err)
		}
		director.definitions[definition.Name] = definition
	}
	initial, isFound := director.definitions[initialPhase]
	if !isFound {
		return nil, fmt.Errorf("initialPhaseMissing: %s", initialPhase)
	}
	if !director.conditionsMet(initial.EntryConditions) {
		return nil, fmt.Errorf("initialEntryRejected: %s", initialPhase)
	}
	for _, definition := range definitions {
		for _, nextPhase := range definition.NextPhases {
			if _, isFound := director.definitions[nextPhase]; !isFound {
				return nil, fmt.Errorf("nextPhaseMissing[%s]: %s", definition.Name, nextPhase)
			}
		}
	}
	director.activatePhaseRoles(initial)
	return director, nil
}

func validateRoles(roles []Role) error {
	seen := make(map[Role]struct{}, len(roles))
	for index, role := range roles {
		if role == "" {
			return fmt.Errorf("emptyRole[%d]", index)
		}
		if _, isFound := seen[role]; isFound {
			return fmt.Errorf("duplicateRole[%s]", role)
		}
		seen[role] = struct{}{}
	}
	return nil
}

func (d *Director) Simulator() *Simulator { return d.simulator }

// IsRoleActive reports whether a stable role belongs to the current phase.
func (d *Director) IsRoleActive(role Role) bool {
	_, isActive := d.activeRoles[role]
	return isActive
}

// SetFact replaces one deterministic scenario measurement.
func (d *Director) SetFact(fact Fact, count int64) error {
	if fact == "" {
		return errors.New("empty fact")
	}
	d.facts[fact] = count
	return nil
}

// AddFact increments one deterministic scenario measurement.
func (d *Director) AddFact(fact Fact, count int64) error {
	if fact == "" {
		return errors.New("empty fact")
	}
	d.facts[fact] += count
	return nil
}

// FactCount returns the current count for a scenario fact.
func (d *Director) FactCount(fact Fact) int64 { return d.facts[fact] }

// IsPhaseComplete reports whether every completion condition is met.
func (d *Director) IsPhaseComplete() bool {
	definition := d.definitions[d.simulator.Phase()]
	return d.conditionsMet(definition.CompletionConditions)
}

// Reset starts a fresh simulation epoch at an explicitly selected route entry.
// It bypasses ordinary next-phase edges but still enforces entry conditions.
func (d *Director) Reset(initialPhase Phase, facts map[Fact]int64) error {
	initial, isFound := d.definitions[initialPhase]
	if !isFound {
		return fmt.Errorf("resetPhaseMissing: %s", initialPhase)
	}
	nextFacts := make(map[Fact]int64, len(facts))
	for fact, count := range facts {
		nextFacts[fact] = count
	}
	if !conditionsMet(nextFacts, initial.EntryConditions) {
		return fmt.Errorf("resetEntryRejected: %s", initialPhase)
	}
	d.simulator.Reset(initialPhase)
	d.facts = nextFacts
	d.activeRoles = make(map[Role]struct{})
	d.activatePhaseRoles(initial)
	return nil
}

// IsCommandAccepted reports phase policy without mutating the simulation.
func (d *Director) IsCommandAccepted(command Command) bool {
	definition := d.definitions[d.simulator.Phase()]
	for _, acceptedCommand := range definition.AcceptedCommands {
		if command == acceptedCommand {
			return true
		}
	}
	return false
}

// Transition enters an allowlisted next phase and invalidates continuations
// owned by the old phase or roles that are no longer active.
func (d *Director) Transition(nextPhase Phase) error {
	current := d.definitions[d.simulator.Phase()]
	if !d.conditionsMet(current.CompletionConditions) {
		return fmt.Errorf("phaseIncomplete: %s", current.Name)
	}
	isAllowed := false
	for _, allowedPhase := range current.NextPhases {
		if nextPhase == allowedPhase {
			isAllowed = true
			break
		}
	}
	if !isAllowed {
		return fmt.Errorf("phaseRejected: %s -> %s", current.Name, nextPhase)
	}
	next, isFound := d.definitions[nextPhase]
	if !isFound {
		return fmt.Errorf("phaseMissing: %s", nextPhase)
	}
	if !d.conditionsMet(next.EntryConditions) {
		return fmt.Errorf("phaseEntryRejected: %s", nextPhase)
	}
	d.simulator.transition(nextPhase)
	d.activatePhaseRoles(next)
	return nil
}

func validateConditions(conditions []Condition) error {
	for index, condition := range conditions {
		if condition.Fact == "" {
			return fmt.Errorf("emptyFact[%d]", index)
		}
		if condition.Comparison > ComparisonAtMost {
			return fmt.Errorf("comparison[%d]: %d", index, condition.Comparison)
		}
	}
	return nil
}

func (d *Director) conditionsMet(conditions []Condition) bool {
	return conditionsMet(d.facts, conditions)
}

func conditionsMet(facts map[Fact]int64, conditions []Condition) bool {
	for _, condition := range conditions {
		count := facts[condition.Fact]
		switch condition.Comparison {
		case ComparisonEqual:
			if count != condition.Count {
				return false
			}
		case ComparisonAtLeast:
			if count < condition.Count {
				return false
			}
		case ComparisonAtMost:
			if count > condition.Count {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (d *Director) activatePhaseRoles(definition PhaseDefinition) {
	nextRoles := make(map[Role]struct{}, len(definition.ActiveRoles))
	for _, role := range definition.ActiveRoles {
		nextRoles[role] = struct{}{}
		if _, isActive := d.activeRoles[role]; !isActive {
			d.simulator.ActivateRole(role)
		}
	}
	for role := range d.activeRoles {
		if _, isActive := nextRoles[role]; isActive {
			continue
		}
		d.simulator.InvalidateRole(role)
	}
	d.activeRoles = nextRoles
}
