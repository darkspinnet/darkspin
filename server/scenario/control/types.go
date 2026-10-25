//go:build scenario

// Package control owns the development scenario session and its inspection state.
// Transport adapters must explicitly map these types to allowlisted wire DTOs.
package control

import "time"

const ProtocolVersion = 1

type SessionState string

const (
	Initializing SessionState = "initializing"
	Running      SessionState = "running"
	Inspection   SessionState = "inspection"
	Stopping     SessionState = "stopping"
	Stopped      SessionState = "stopped"
)

type LeaseOwner string

const (
	LeaseNone        LeaseOwner = "none"
	LeaseScript      LeaseOwner = "script"
	LeaseInteractive LeaseOwner = "interactive"
)

// ActionLease generations invalidate previously issued authority on transfer.
type ActionLease struct {
	Owner      LeaseOwner
	Generation uint64
}

type ScriptIdentity struct {
	RelativePath string
	SHA256       string
}

type ScriptOutcome string

const (
	ScriptUnavailable  ScriptOutcome = "unavailable"
	ScriptPassed       ScriptOutcome = "passed"
	ScriptFailed       ScriptOutcome = "failed"
	ScriptInterrupted  ScriptOutcome = "interrupted"
	ScriptInconclusive ScriptOutcome = "inconclusive"
)

// ScriptResult is final attempt history; inspection never revises its outcome.
type ScriptResult struct {
	Identity   ScriptIdentity
	Outcome    ScriptOutcome
	StartedAt  time.Time
	FinishedAt time.Time
	Failure    *ScenarioError
	Traceback  string
}

// OwnedClientIdentity is public process correlation, never an ownership token.
// The process owner retains its handle and bridge credential outside this view.
type OwnedClientIdentity struct {
	Name                 string
	InstanceID           string
	PID                  uint32
	ConnectionGeneration uint64
	ExecutableSHA256     string
	Architecture         string
	BuildID              string
}

type AvailabilityStatus string

const (
	Available    AvailabilityStatus = "available"
	Unavailable  AvailabilityStatus = "unavailable"
	Stale        AvailabilityStatus = "stale"
	Disconnected AvailabilityStatus = "disconnected"
)

type Availability struct {
	Status     AvailabilityStatus
	Reason     string
	ObservedAt time.Time
	Sequence   uint64
}

// Observation data has meaning only when its availability permits it. Adapters
// must encode unavailable data as null rather than exposing its Go zero value.
type Observation[T any] struct {
	Availability Availability
	Data         T
}

type ClientState struct {
	Identity         OwnedClientIdentity
	ObservedAt       time.Time
	Sequence         uint64
	Bridge           Availability
	FrameSequence    Observation[uint64]
	SceneAssetID     Observation[uint32]
	SceneName        Observation[string]
	ControlledHeroID Observation[string]
	SelectedHeroSlot Observation[uint32]
}

type Session struct {
	ID                  string
	State               SessionState
	Lease               ActionLease
	CreatedAt           time.Time
	InspectionExpiresAt time.Time
	Clients             []ClientState
	ScriptResult        *ScriptResult
}

// InspectionRequest is submitted by the resource owner after action settlement.
// IsActionSettled must not be inferred from script failure or pipe disconnect.
type InspectionRequest struct {
	IsActionSettled bool
	Result          *ScriptResult
	Retention       time.Duration
}

type ReceiptStatus string

const (
	ReceiptQueued         ReceiptStatus = "queued"
	ReceiptDispatched     ReceiptStatus = "dispatched"
	ReceiptRejected       ReceiptStatus = "rejected"
	ReceiptExpired        ReceiptStatus = "expired"
	ReceiptCancelled      ReceiptStatus = "cancelled"
	ReceiptOutcomeUnknown ReceiptStatus = "outcome_unknown"
)

// ReceiptDispatched records adapter delivery, never a successful game outcome.
type CommandReceipt struct {
	RequestID            string
	ClientInstanceID     string
	ConnectionGeneration uint64
	LeaseGeneration      uint64
	Status               ReceiptStatus
	UpdatedAt            time.Time
	Failure              *ScenarioError
}

type Capability struct {
	Name        string
	IsAvailable bool
	Reason      string
}
