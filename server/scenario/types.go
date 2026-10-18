//go:build scenario

// Package scenario owns the bounded development-only normal-speed scenario.
package scenario

import (
	"context"
	"time"
)

const SchemaVersion = 1

type Outcome string

const (
	Passed       Outcome = "passed"
	Failed       Outcome = "failed"
	Inconclusive Outcome = "inconclusive"
	NotReached   Outcome = "not_reached"
)

type StepKind string

const (
	Launch            StepKind = "launch"
	Authenticated     StepKind = "authenticated"
	PrepareAccepted   StepKind = "prepare_accepted"
	NativeLayout      StepKind = "native_layout"
	FixtureTakeover   StepKind = "fixture_takeover"
	WorldReady        StepKind = "world_ready"
	DungeonCommitted  StepKind = "dungeon_committed"
	ControllableHero  StepKind = "controllable_hero"
	SettledEntry      StepKind = "settled_entry"
	FixtureComparison StepKind = "fixture_comparison"
	BeforeTraversal   StepKind = "before_traversal"
	AfterTraversal    StepKind = "after_traversal"
	BossAdmission     StepKind = "boss_admission"
	BossPublication   StepKind = "boss_publication"
	TerminalFailure   StepKind = "terminal_failure"
)

type Milestone struct {
	Kind    StepKind
	Timeout time.Duration
}

// Definition holds requested inputs, never a player aggregate or credentials.
// ProgressionID and SquadID identify fresh configurations within this run.
type Definition struct {
	SchemaVersion        uint32
	ScenarioID           string
	Level                string
	Occurrence           uint32
	Difficulty           uint32
	MapSeed              uint32
	PrepareMask          uint32
	PopulationProvenance string
	ProgressionID        string
	SquadID              string
	HeroIDs              []uint64
	ClientCount          uint32
	Milestones           []Milestone
}

type Artifact struct {
	Role   string
	Path   string
	SHA256 string
}

type RunPaths struct {
	RunID           string
	CacheDirectory  string
	ReportDirectory string
	TraceDirectory  string
	ManifestPath    string
}

type EffectiveInputs struct {
	Level                string
	Occurrence           uint32
	Difficulty           uint32
	MapSeed              uint32
	PrepareMask          uint32
	PopulationProvenance string
	UnknownRNGOwners     []string
}

type Version struct {
	Component string
	BuildID   string
	Artifact  Artifact
}

type Observation struct {
	Outcome        Outcome
	Detail         string
	SessionID      string
	ZoneGeneration uint64
	Effective      *EffectiveInputs
	Artifacts      []Artifact
}

type StepStatus struct {
	Kind        StepKind
	Outcome     Outcome
	StartedAt   time.Time
	FinishedAt  time.Time
	Detail      string
	Observation Observation
}

type Manifest struct {
	SchemaVersion uint32
	RunID         string
	Definition    Definition
	Input         Artifact
	Paths         RunPaths
	StartedAt     time.Time
	FinishedAt    time.Time
	Outcome       Outcome
	Detail        string
	LastReached   StepKind
	Capabilities  []Capability
	Versions      []Version
	Effective     *EffectiveInputs
	Steps         []StepStatus
	Artifacts     []Artifact
}

type RunRequest struct {
	Definition Definition
	Input      Artifact
}

type CapabilityRequest struct {
	RunID      string
	Paths      RunPaths
	Definition Definition
}

// CapabilityPort must obtain live peer capabilities; a locally constructed
// capability does not prove what another process loaded.
type CapabilityPort interface {
	Verify(context.Context, CapabilityRequest) ([]Capability, error)
}

type LaunchRequest struct {
	RunID      string
	Paths      RunPaths
	Definition Definition
	Deadline   time.Time
}

type CloseRequest struct {
	RunID string
	Paths RunPaths
}

type LaunchResult struct {
	Observation Observation
	Versions    []Version
}

// Launcher owns only resources it creates, uses loopback authentication, and
// returns evidence of actual process initialization rather than window creation.
type Launcher interface {
	Launch(context.Context, LaunchRequest) (LaunchResult, error)
	Close(context.Context, CloseRequest) error
}

type ObservationRequest struct {
	RunID      string
	Paths      RunPaths
	Definition Definition
	Milestone  Milestone
}

// Observer performs only the named normal gameplay/observation operations.
// NativeLayout must be observed before any fixture takeover mutation.
type Observer interface {
	Observe(context.Context, ObservationRequest) (Observation, error)
}

type Store interface {
	Allocate(context.Context, Definition) (RunPaths, error)
	Save(context.Context, Manifest) error
	PreserveInput(context.Context, RunPaths, Artifact) (Artifact, error)
	VerifyEvidence(context.Context, RunPaths, []Artifact) error
}
