package raknet103

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zoneteleport "github.com/darkspinnet/darkspin/server/zone/teleport"
)

// teleporterHandoff owns the protocol-neutral transition from chunk 144's
// retained trigger callbacks through modifier replication to chunk 349's
// unique modifier run. Socket publication and contact detection remain outside
// this boundary.
type HandoffModifierPool interface {
	Allocate() (uint32, error)
	Release(instanceID uint32) error
}

type Handoff struct {
	security   *SecurityRun
	program    sim.Program
	active     *Run
	pool       HandoffModifierPool
	objectID   uint32
	instanceID uint32
}

func NewHandoff(
	security *SecurityRun, program sim.Program, pool HandoffModifierPool,
) (*Handoff, error) {
	if security == nil || !security.IsReady() {
		return nil, errors.New("security teleporter missing")
	}
	err := ValidateProgram(program)
	if err != nil {
		return nil, fmt.Errorf("teleportValidate: %w", err)
	}
	if pool == nil {
		return nil, errors.New("modifier pool missing")
	}
	return &Handoff{security: security, program: program, pool: pool}, nil
}

func (h *Handoff) AcceptEntrant(
	ctx context.Context, callback sim.LuaTriggerCallback,
	isPlayerControlled bool, isOwnerPlayerControlled bool,
	objectID uint32, position sim.Position, sourceTime uint64,
) (*Run, [][]byte, error) {
	if h == nil || h.security == nil {
		return nil, nil, errors.New("nil teleporter handoff")
	}
	if ctx == nil {
		return nil, nil, errors.New("nil context")
	}
	request, err := h.security.RequestEntrant(
		ctx, callback, isPlayerControlled, isOwnerPlayerControlled, h.active != nil,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("securityRequest: %w", err)
	}
	if request == nil {
		return nil, nil, nil
	}
	prefix := make([][]byte, 0, 3)
	if h.active != nil {
		deleted, err := h.RemoveActive()
		if err != nil {
			return nil, nil, fmt.Errorf("uniqueRemove: %w", err)
		}
		prefix = append(prefix, deleted)
	}
	instanceID, err := h.pool.Allocate()
	if err != nil {
		return nil, nil, fmt.Errorf("instanceAllocate: %w", err)
	}
	run, packets, err := NewRun(h.program, objectID, position, sourceTime)
	if err != nil {
		releaseErr := h.pool.Release(instanceID)
		if releaseErr != nil {
			return nil, nil, fmt.Errorf("teleportCreate: %w", errors.Join(
				err, fmt.Errorf("instanceRelease: %w", releaseErr),
			))
		}
		return nil, nil, fmt.Errorf("teleportCreate: %w", err)
	}
	teleportStep, isTeleportStep := h.program.Steps[4].(sim.EmitStep)
	teleport, isTeleport := teleportStep.Intent.(sim.TeleportIntent)
	if !isTeleportStep || !isTeleport || teleport.Destination != request.Destination {
		run.Abort()
		mismatchErr := fmt.Errorf("destinationMismatch: %#v != %#v", teleport, request.Destination)
		releaseErr := h.pool.Release(instanceID)
		if releaseErr != nil {
			return nil, nil, errors.Join(mismatchErr, fmt.Errorf("instanceRelease: %w", releaseErr))
		}
		return nil, nil, mismatchErr
	}
	created, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: objectID, ModifierGUID: zoneteleport.ModifierGUIDID,
		InstanceID:           instanceID,
		DurationMilliseconds: 0, Overdrive: 1, StackCount: 1,
		StartMilliseconds: sourceTime, SourceID: 0, IsBound: true,
	})
	if err != nil {
		run.Abort()
		releaseErr := h.pool.Release(instanceID)
		if releaseErr != nil {
			return nil, nil, fmt.Errorf("modifierMarshal: %w", errors.Join(
				err, fmt.Errorf("instanceRelease: %w", releaseErr),
			))
		}
		return nil, nil, fmt.Errorf("modifierMarshal: %w", err)
	}
	h.active = run
	h.objectID = objectID
	h.instanceID = instanceID
	prefix = append(prefix, created)
	return run, append(prefix, packets...), nil
}

func (h *Handoff) RemoveActive() ([]byte, error) {
	if h == nil || h.pool == nil {
		return nil, errors.New("nil teleporter handoff")
	}
	if h.active == nil {
		return nil, nil
	}
	h.active.Stop()
	packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
		TargetID: h.objectID, InstanceID: h.instanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("modifierDeleteMarshal: %w", err)
	}
	err = h.pool.Release(h.instanceID)
	if err != nil {
		return nil, fmt.Errorf("instanceRelease: %w", err)
	}
	h.active = nil
	h.objectID = 0
	h.instanceID = 0
	return packet, nil
}

func (h *Handoff) Stop() {
	if h == nil {
		return
	}
	if h.active != nil {
		h.active.Stop()
		releaseErr := h.pool.Release(h.instanceID)
		if releaseErr == nil {
			h.active = nil
			h.objectID = 0
			h.instanceID = 0
		}
	}
	if h.security != nil {
		h.security.Stop()
	}
}

func (h *Handoff) Active() *Run {
	if h == nil {
		return nil
	}
	return h.active
}
