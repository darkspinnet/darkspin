package game

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
)

// CampaignScriptRegistration binds one server-assigned replicated object to
// an authored marker callback. Marker IDs are not accepted as object IDs.
type CampaignScriptRegistration struct {
	ObjectID     uint32
	MarkerID     uint32
	CallbackName string
	Position     Vec3
	UseLimit     int32
	Challenge    int32
}

// CampaignScriptInvocation is the immutable script metadata selected by one
// authenticated object interaction. A consumer still owns script admission,
// execution, use limits, rewards, and replication.
type CampaignScriptInvocation struct {
	SourceObjectID uint32
	TargetObjectID uint32
	MarkerID       uint32
	CallbackName   string
	Position       Vec3
	Challenge      int32
	Bindings       []CampaignScriptBinding
}

// CampaignScriptUse is a prepared authoritative use. The caller may encode
// the resulting replication before committing it, so an encoding failure does
// not consume the object.
type CampaignScriptUse struct {
	Invocation CampaignScriptInvocation
	UseCount   int32
	UseLimit   int32
}

type CampaignScriptUseRejection string

const (
	CampaignScriptUseRejectedIdentity      CampaignScriptUseRejection = "identity"
	CampaignScriptUseRejectedConfiguration CampaignScriptUseRejection = "configuration"
	CampaignScriptUseRejectedDistance      CampaignScriptUseRejection = "distance"
	CampaignScriptUseRejectedExhausted     CampaignScriptUseRejection = "exhausted"
)

// CampaignScriptRegistry owns the object-to-marker authority map for one
// campaign match. It does not allocate or create replicated objects.
type CampaignScriptRegistry struct {
	mu                      sync.RWMutex
	director                CampaignDirector
	registrationsByObjectID map[uint32]CampaignScriptRegistration
	useCountsByObjectID     map[uint32]int32
}

func NewCampaignScriptRegistry(director CampaignDirector) (*CampaignScriptRegistry, error) {
	if director.Level == "" {
		return nil, errors.New("create campaign script registry: empty level")
	}
	for index, script := range director.Scripts {
		if script.MarkerID == 0 || script.CallbackName == "" || script.LuaChunkID <= 0 ||
			script.LuaSourceName == "" || script.LuaSHA256 == "" {
			return nil, fmt.Errorf("createScript[%d]: invalid", index)
		}
	}
	return &CampaignScriptRegistry{
		director: director, registrationsByObjectID: make(map[uint32]CampaignScriptRegistration),
		useCountsByObjectID: make(map[uint32]int32),
	}, nil
}

// Register records an object only after its replication owner has allocated
// the object ID. The callback must exist exactly on the authored marker.
func (r *CampaignScriptRegistry) Register(registration CampaignScriptRegistration) error {
	if r == nil {
		return errors.New("register campaign script: nil registry")
	}
	if registration.ObjectID == 0 || registration.MarkerID == 0 || registration.CallbackName == "" {
		return errors.New("register campaign script: invalid registration")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, isDuplicate := r.registrationsByObjectID[registration.ObjectID]; isDuplicate {
		return fmt.Errorf("register campaign script: duplicate object %d", registration.ObjectID)
	}
	bindings := r.director.ScriptBindings(registration.MarkerID, registration.CallbackName)
	if len(bindings) == 0 {
		return fmt.Errorf("register campaign script: marker callback missing %d/%s",
			registration.MarkerID, registration.CallbackName)
	}
	r.registrationsByObjectID[registration.ObjectID] = registration
	return nil
}

// PrepareUse validates identity, distance, and remaining uses without mutating
// the registry. Source position must be the server's accepted player position,
// not a client-supplied target pose.
func (r *CampaignScriptRegistry) PrepareUse(
	controlledObjectID, sourceObjectID, targetObjectID uint32, sourcePosition Vec3, maximumDistance float32,
) (CampaignScriptUse, CampaignScriptUseRejection, error) {
	if r == nil {
		return CampaignScriptUse{}, "",
			errors.New("prepare campaign script use: nil registry")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	invocation, isResolved, err := r.resolve(
		controlledObjectID, sourceObjectID, targetObjectID,
	)
	if err != nil {
		return CampaignScriptUse{}, "", fmt.Errorf("prepareResolve: %w", err)
	}
	if !isResolved {
		return CampaignScriptUse{}, CampaignScriptUseRejectedIdentity, nil
	}
	registration := r.registrationsByObjectID[targetObjectID]
	use := CampaignScriptUse{
		Invocation: invocation, UseLimit: registration.UseLimit,
	}
	if registration.UseLimit == 0 || registration.UseLimit < -1 ||
		maximumDistance <= 0 || !isFiniteCampaignPosition(sourcePosition) ||
		math.IsNaN(float64(maximumDistance)) || math.IsInf(float64(maximumDistance), 0) {
		return use, CampaignScriptUseRejectedConfiguration, nil
	}
	deltaX := sourcePosition.X - registration.Position.X
	deltaY := sourcePosition.Y - registration.Position.Y
	deltaZ := sourcePosition.Z - registration.Position.Z
	distanceSquared := deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ
	if distanceSquared > maximumDistance*maximumDistance {
		return use, CampaignScriptUseRejectedDistance, nil
	}
	useCount := r.useCountsByObjectID[targetObjectID]
	if registration.UseLimit >= 0 && useCount >= registration.UseLimit {
		return use, CampaignScriptUseRejectedExhausted, nil
	}
	if useCount == math.MaxInt32 {
		return CampaignScriptUse{}, "", errors.New("prepare campaign script use: count exhausted")
	}
	use.UseCount = useCount + 1
	return use, "", nil
}

// CommitUse consumes the exact prepared count. A stale or forged preparation
// is rejected without changing the current count.
func (r *CampaignScriptRegistry) CommitUse(use CampaignScriptUse) error {
	if r == nil {
		return errors.New("commit campaign script use: nil registry")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	targetObjectID := use.Invocation.TargetObjectID
	registration, isFound := r.registrationsByObjectID[targetObjectID]
	if !isFound || registration.UseLimit != use.UseLimit || use.UseCount <= 0 ||
		r.useCountsByObjectID[targetObjectID] != use.UseCount-1 {
		return errors.New("commit campaign script use: stale preparation")
	}
	if registration.UseLimit >= 0 && use.UseCount > registration.UseLimit {
		return errors.New("commit campaign script use: limit exceeded")
	}
	r.useCountsByObjectID[targetObjectID] = use.UseCount
	return nil
}

func (r *CampaignScriptRegistry) SnapshotUses() []CampaignScriptUse {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	uses := make([]CampaignScriptUse, 0)
	for objectID, registration := range r.registrationsByObjectID {
		useCount := r.useCountsByObjectID[objectID]
		if useCount <= 0 {
			continue
		}
		uses = append(uses, CampaignScriptUse{
			Invocation: CampaignScriptInvocation{
				TargetObjectID: objectID, MarkerID: registration.MarkerID,
				CallbackName: registration.CallbackName,
			},
			UseCount: useCount, UseLimit: registration.UseLimit,
		})
	}
	sort.Slice(uses, func(left int, right int) bool {
		return uses[left].Invocation.TargetObjectID <
			uses[right].Invocation.TargetObjectID
	})
	return uses
}

func (r *CampaignScriptRegistry) RestoreUses(uses []CampaignScriptUse) error {
	if r == nil {
		return errors.New("restore campaign script: nil registry")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	restoredCounts := make(map[uint32]int32, len(uses))
	for index, use := range uses {
		objectID := use.Invocation.TargetObjectID
		registration, isFound := r.registrationsByObjectID[objectID]
		if !isFound || objectID == 0 || use.UseCount <= 0 ||
			registration.MarkerID != use.Invocation.MarkerID ||
			registration.CallbackName != use.Invocation.CallbackName ||
			registration.UseLimit != use.UseLimit ||
			(registration.UseLimit >= 0 && use.UseCount > registration.UseLimit) {
			return fmt.Errorf("restoreScriptUse[%d]: invalid", index)
		}
		if restoredCounts[objectID] != 0 {
			return fmt.Errorf("restoreScriptUse[%d]: duplicate", index)
		}
		restoredCounts[objectID] = use.UseCount
	}
	clear(r.useCountsByObjectID)
	for objectID, useCount := range restoredCounts {
		r.useCountsByObjectID[objectID] = useCount
	}
	return nil
}

// Resolve validates the active source object and resolves a replicated target
// through the server-owned registration. Rejected requests emit no invocation.
func (r *CampaignScriptRegistry) Resolve(
	controlledObjectID, sourceObjectID, targetObjectID uint32,
) (CampaignScriptInvocation, bool, error) {
	if r == nil {
		return CampaignScriptInvocation{}, false, errors.New("resolve campaign script: nil registry")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	invocation, isResolved, err := r.resolve(
		controlledObjectID, sourceObjectID, targetObjectID,
	)
	if err != nil {
		return CampaignScriptInvocation{}, false,
			fmt.Errorf("scriptResolve: %w", err)
	}
	return invocation, isResolved, nil
}

func (r *CampaignScriptRegistry) resolve(
	controlledObjectID, sourceObjectID, targetObjectID uint32,
) (CampaignScriptInvocation, bool, error) {
	if controlledObjectID == 0 || sourceObjectID != controlledObjectID || targetObjectID == 0 {
		return CampaignScriptInvocation{}, false, nil
	}
	registration, isFound := r.registrationsByObjectID[targetObjectID]
	if !isFound {
		return CampaignScriptInvocation{}, false, nil
	}
	bindings := r.director.ScriptBindings(registration.MarkerID, registration.CallbackName)
	if len(bindings) == 0 {
		return CampaignScriptInvocation{}, false,
			fmt.Errorf("resolve campaign script: binding missing %d/%s",
				registration.MarkerID, registration.CallbackName)
	}
	return CampaignScriptInvocation{
		SourceObjectID: sourceObjectID, TargetObjectID: targetObjectID,
		MarkerID: registration.MarkerID, CallbackName: registration.CallbackName,
		Position: registration.Position, Challenge: registration.Challenge, Bindings: bindings,
	}, true, nil
}
