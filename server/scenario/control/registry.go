//go:build scenario

package control

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const InspectionRetention = 30 * time.Minute

// Registry owns a single session's public state. Process handles and credentials
// stay with the concrete resource owner; this registry cannot discover or kill a
// process. Its zero value is not ready for use; construct it with NewRegistry.
type Registry struct {
	mutex               sync.RWMutex
	id                  string
	state               SessionState
	lease               ActionLease
	createdAt           time.Time
	inspectionExpiresAt time.Time
	clientsByName       map[string]ClientState
	scriptResult        *ScriptResult
}

func NewRegistry(id string) (*Registry, error) {
	if strings.TrimSpace(id) == "" {
		return nil, &ScenarioError{Code: InvalidRequest, Context: "sessionID", Message: "session ID is required"}
	}
	return &Registry{
		id: id, state: Initializing, createdAt: time.Now().UTC(),
		lease:         ActionLease{Owner: LeaseNone, Generation: 1},
		clientsByName: make(map[string]ClientState),
	}, nil
}

// BeginScript grants the sole script lease before any source operation runs.
func (e *Registry) BeginScript() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.state != Initializing && e.state != Inspection {
		return &ScenarioError{Code: Conflict, Context: "scriptState", Message: "wait for the current script to finish before running another"}
	}
	e.state = Running
	e.scriptResult = nil
	e.inspectionExpiresAt = time.Time{}
	e.lease = ActionLease{Owner: LeaseScript, Generation: e.lease.Generation + 1}
	return nil
}

// ForgetClient releases a name only after the process owner confirms closure.
func (e *Registry) ForgetClient(name string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	delete(e.clientsByName, name)
}

// Snapshot returns a copy. Ordinary polling never extends inspection retention.
func (e *Registry) Snapshot() Session {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	clients := make([]ClientState, 0, len(e.clientsByName))
	for _, client := range e.clientsByName {
		clients = append(clients, client)
	}
	sort.Slice(clients, func(i, j int) bool { return clients[i].Identity.Name < clients[j].Identity.Name })
	return Session{
		ID: e.id, State: e.state, Lease: e.lease, CreatedAt: e.createdAt,
		InspectionExpiresAt: e.inspectionExpiresAt, Clients: clients,
		ScriptResult: cloneScriptResult(e.scriptResult),
	}
}

func (e *Registry) RegisterClient(req OwnedClientIdentity) error {
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.InstanceID) == "" ||
		req.PID == 0 || req.ConnectionGeneration == 0 {
		return &ScenarioError{Code: InvalidRequest, Context: "clientIdentity", Message: "client name, instance, owned PID, and connection generation are required"}
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.state == Stopping || e.state == Stopped {
		return &ScenarioError{Code: Conflict, Context: "clientRegister", Message: "session is stopping or stopped"}
	}
	_, isRegistered := e.clientsByName[req.Name]
	if isRegistered {
		return &ScenarioError{Code: Conflict, Context: "clientName", Message: "client name is already registered"}
	}
	for _, client := range e.clientsByName {
		if client.Identity.InstanceID == req.InstanceID || client.Identity.PID == req.PID {
			return &ScenarioError{Code: Conflict, Context: "clientInstance", Message: "client instance or PID is already registered"}
		}
	}
	unavailable := Availability{Status: Unavailable, Reason: "no observation received"}
	e.clientsByName[req.Name] = ClientState{
		Identity: req, Bridge: unavailable,
		FrameSequence:    Observation[uint64]{Availability: unavailable},
		SceneAssetID:     Observation[uint32]{Availability: unavailable},
		SceneName:        Observation[string]{Availability: unavailable},
		ControlledHeroID: Observation[string]{Availability: unavailable},
		SelectedHeroSlot: Observation[uint32]{Availability: unavailable},
	}
	return nil
}

// PublishState accepts only a complete, identified observation from the current
// owned connection. Zero/old sequences cannot overwrite a newer observation.
func (e *Registry) PublishState(req ClientState) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.state == Stopping || e.state == Stopped {
		return &ScenarioError{Code: Conflict, Context: "statePublish", Message: "session is stopping or stopped"}
	}
	client, isRegistered := e.clientsByName[req.Identity.Name]
	if !isRegistered {
		return &ScenarioError{Code: NotFound, Context: "stateClient", Message: "client is not registered"}
	}
	if req.Identity != client.Identity {
		return &ScenarioError{Code: OwnershipLost, Context: "stateIdentity", Message: "observation does not match the registered client connection"}
	}
	if req.Sequence == 0 || req.Sequence <= client.Sequence || req.ObservedAt.IsZero() {
		return &ScenarioError{Code: Conflict, Context: "stateSequence", Message: "observation requires a timestamp and a newer nonzero sequence"}
	}
	err := validateAvailability(req.Bridge)
	if err != nil {
		return fmt.Errorf("bridgeState: %w", err)
	}
	observations := []Availability{
		req.FrameSequence.Availability, req.SceneAssetID.Availability,
		req.SceneName.Availability, req.ControlledHeroID.Availability,
		req.SelectedHeroSlot.Availability,
	}
	for index, observation := range observations {
		err = validateAvailability(observation)
		if err != nil {
			return fmt.Errorf("stateField[%d]: %w", index, err)
		}
	}
	e.clientsByName[req.Identity.Name] = req
	return nil
}

func (e *Registry) Client(name string) (ClientState, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	client, isRegistered := e.clientsByName[name]
	if !isRegistered {
		return ClientState{}, &ScenarioError{Code: NotFound, Context: "clientRead", Message: "client is not registered"}
	}
	return client, nil
}

// EnterInspection transfers authority only after the resource owner confirms
// that all prior actions are settled. It does not stop or settle native input.
func (e *Registry) EnterInspection(req InspectionRequest) error {
	if !req.IsActionSettled {
		return &ScenarioError{Code: Conflict, Context: "actionSettlement", Message: "actions must be settled before interactive inspection"}
	}
	retention := req.Retention
	if retention == 0 {
		retention = InspectionRetention
	}
	if retention < 0 || retention > InspectionRetention {
		return &ScenarioError{Code: InvalidRequest, Context: "inspectionRetention", Message: "inspection retention must be within 30 minutes"}
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.state != Initializing && e.state != Running {
		return &ScenarioError{Code: Conflict, Context: "inspectionState", Message: "session has already left initialization"}
	}
	e.state = Inspection
	e.lease = ActionLease{Owner: LeaseInteractive, Generation: e.lease.Generation + 1}
	e.inspectionExpiresAt = time.Now().UTC().Add(retention)
	e.scriptResult = cloneScriptResult(req.Result)
	return nil
}

// BeginStop revokes action authority. The process owner performs cleanup next.
func (e *Registry) BeginStop() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.state == Stopped || e.state == Stopping {
		return nil
	}
	e.state = Stopping
	e.lease = ActionLease{Owner: LeaseNone, Generation: e.lease.Generation + 1}
	return nil
}

// CompleteStop records the owner's completed cleanup, never mere submission.
func (e *Registry) CompleteStop() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.state == Stopped {
		return nil
	}
	if e.state != Stopping {
		return &ScenarioError{Code: Conflict, Context: "stopCompletion", Message: "session stop has not begun"}
	}
	e.state = Stopped
	e.inspectionExpiresAt = time.Time{}
	return nil
}

func validateAvailability(availability Availability) error {
	switch availability.Status {
	case Available:
		if availability.ObservedAt.IsZero() || availability.Sequence == 0 {
			return &ScenarioError{Code: InvalidRequest, Context: "availableObservation", Message: "available observations require a timestamp and nonzero sequence"}
		}
	case Unavailable, Stale, Disconnected:
		if strings.TrimSpace(availability.Reason) == "" {
			return &ScenarioError{Code: InvalidRequest, Context: "unavailableReason", Message: "unavailable observations require a reason"}
		}
	default:
		return &ScenarioError{Code: InvalidRequest, Context: "availabilityStatus", Message: "observation availability is required"}
	}
	return nil
}

func cloneScriptResult(result *ScriptResult) *ScriptResult {
	if result == nil {
		return nil
	}
	copyResult := *result
	copyResult.Failure = cloneScenarioError(result.Failure)
	return &copyResult
}

func cloneScenarioError(failure *ScenarioError) *ScenarioError {
	if failure == nil {
		return nil
	}
	copyFailure := *failure
	cause, isScenarioError := failure.Cause.(*ScenarioError)
	if isScenarioError {
		copyFailure.Cause = cloneScenarioError(cause)
	}
	return &copyFailure
}
