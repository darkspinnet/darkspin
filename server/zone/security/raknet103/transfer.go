package raknet103

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/sim"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
	zoneteleport "github.com/darkspinnet/darkspin/server/zone/teleport"
	teleportraknet "github.com/darkspinnet/darkspin/server/zone/teleport/raknet103"
)

type ModifierPool interface {
	Allocate() (uint32, error)
	Release(uint32) error
}

type TransferRequest struct {
	Program    sim.Program
	Pool       ModifierPool
	ObjectID   uint32
	Position   raknet.Vector3
	Teleport   zonesecurity.Teleport
	RouteIndex int
	SourceTime uint64
}

type Transfer struct {
	run         *teleportraknet.Run
	pool        ModifierPool
	routeIndex  int
	instanceID  uint32
	destination raknet.Vector3
	sourceTime  uint64
	isReleased  bool
}

func NewTransfer(req TransferRequest) (*Transfer, [][]byte, error) {
	if req.Pool == nil {
		return nil, nil, errors.New("modifier pool missing")
	}
	if req.RouteIndex < 0 || req.RouteIndex >= zonesecurity.CountRoutes() {
		return nil, nil, fmt.Errorf("routeIndex: %d", req.RouteIndex)
	}
	run, packets, err := teleportraknet.NewRun(
		req.Program,
		req.ObjectID,
		sim.Position{
			X: req.Position.X, Y: req.Position.Y, Z: req.Position.Z,
		},
		req.SourceTime,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("teleportCreate: %w", err)
	}
	instanceID, err := req.Pool.Allocate()
	if err != nil {
		run.Abort()
		return nil, nil, fmt.Errorf("instanceAllocate: %w", err)
	}
	created, err := raknet.MarshalApplication(raknet.ModifierCreatedMessage{
		TargetID: req.ObjectID, ModifierGUID: zoneteleport.ModifierGUIDID,
		InstanceID: instanceID, DurationMilliseconds: 0,
		Overdrive: 1, StackCount: 1,
		StartMilliseconds: req.SourceTime, SourceID: 0, IsBound: true,
	})
	if err != nil {
		run.Abort()
		releaseErr := req.Pool.Release(instanceID)
		if releaseErr != nil {
			return nil, nil, fmt.Errorf(
				"modifierMarshal: %w",
				errors.Join(err, fmt.Errorf("instanceRelease: %w", releaseErr)),
			)
		}
		return nil, nil, fmt.Errorf("modifierMarshal: %w", err)
	}
	transfer := &Transfer{
		run: run, pool: req.Pool, routeIndex: req.RouteIndex,
		instanceID: instanceID,
		sourceTime: req.SourceTime,
		destination: raknet.Vector3{
			X: req.Teleport.Destination.X, Y: req.Teleport.Destination.Y,
			Z: req.Teleport.Destination.Z,
		},
	}
	return transfer, append([][]byte{created}, packets...), nil
}

// SourceTime returns the client timeline used to start the transfer.
func (e *Transfer) SourceTime() uint64 {
	if e == nil {
		return 0
	}
	return e.sourceTime
}

func (e *Transfer) Advance(deadline time.Duration) ([][]byte, error) {
	if e == nil || e.run == nil {
		return nil, errors.New("security transfer missing")
	}
	packets, err := e.run.Advance(context.Background(), deadline)
	if err != nil {
		return nil, fmt.Errorf("teleportAdvance: %w", err)
	}
	return packets, nil
}

func (e *Transfer) Complete() ([]byte, error) {
	if e == nil || e.run == nil {
		return nil, errors.New("security transfer missing")
	}
	if e.isReleased {
		return nil, nil
	}
	if e.pool == nil {
		return nil, errors.New("modifier pool missing")
	}
	e.run.SetCancel(nil)
	packet, err := raknet.MarshalApplication(raknet.ModifierDeletedMessage{
		TargetID: e.run.ObjectID(), InstanceID: e.instanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("modifierDeleteMarshal: %w", err)
	}
	err = e.pool.Release(e.instanceID)
	if err != nil {
		return nil, fmt.Errorf("instanceRelease: %w", err)
	}
	e.isReleased = true
	e.instanceID = 0
	return packet, nil
}

func (e *Transfer) Stop() {
	if e == nil {
		return
	}
	if e.run != nil {
		e.run.Stop()
	}
	if !e.isReleased && e.instanceID != 0 && e.pool != nil {
		_ = e.pool.Release(e.instanceID)
		e.instanceID = 0
		e.isReleased = true
	}
}

func (e *Transfer) SetCancel(cancel raknet.CancelSchedule) {
	if e == nil || e.run == nil {
		return
	}
	e.run.SetCancel(cancel)
}

func (e *Transfer) Destination() raknet.Vector3 {
	if e == nil {
		return raknet.Vector3{}
	}
	return e.destination
}

func (e *Transfer) RouteIndex() int {
	if e == nil {
		return -1
	}
	return e.routeIndex
}
