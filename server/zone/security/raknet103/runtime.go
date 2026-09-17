package raknet103

import (
	"fmt"
	"log"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	zoneteleport "github.com/darkspinnet/darkspin/server/zone/teleport"
)

type TransferAdvanceRequest struct {
	SessionKey string
	Generation uint64
	Transfer   *Transfer
	Deadline   time.Duration
	Now        time.Time
}

type TransferCancelRequest struct {
	SessionKey string
	Generation uint64
	Transfer   *Transfer
}

// TransferAuthority is the narrow connection boundary consumed by the
// build-103 transfer scheduler.
type TransferAuthority interface {
	AdvanceSecurityTransfer(req TransferAdvanceRequest) ([][]byte, error)
	CancelSecurityTransfer(req TransferCancelRequest) bool
}

type TransferRuntime struct {
	authority TransferAuthority
	now       func() time.Time
	logger    *log.Logger
}

func NewTransferRuntime(
	authority TransferAuthority,
	now func() time.Time,
	logger *log.Logger,
) *TransferRuntime {
	return &TransferRuntime{authority: authority, now: now, logger: logger}
}

type transferStep struct {
	runtime    *TransferRuntime
	sessionKey string
	generation uint64
	transfer   *Transfer
	deadline   time.Duration
}

func (e transferStep) produce() ([][]byte, error) {
	if e.runtime == nil || e.runtime.authority == nil {
		return nil, fmt.Errorf("securityTransferAuthority: unavailable")
	}
	now := time.Now()
	if e.runtime.now != nil {
		now = e.runtime.now()
	}
	packets, err := e.runtime.authority.AdvanceSecurityTransfer(
		TransferAdvanceRequest{
			SessionKey: e.sessionKey,
			Generation: e.generation,
			Transfer:   e.transfer,
			Deadline:   e.deadline,
			Now:        now,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("securityTransferAdvance[%s]: %w", e.deadline, err)
	}
	return packets, nil
}

type TransferFailure struct {
	runtime    *TransferRuntime
	sessionKey string
	generation uint64
	transfer   *Transfer
}

func (e TransferFailure) Handle(scheduleErr error) {
	if e.runtime == nil || e.runtime.authority == nil {
		return
	}
	isCanceled := e.runtime.authority.CancelSecurityTransfer(
		TransferCancelRequest{
			SessionKey: e.sessionKey,
			Generation: e.generation,
			Transfer:   e.transfer,
		},
	)
	if isCanceled && e.runtime.logger != nil {
		e.runtime.logger.Printf(
			"RakNet campaign security transfer canceled for %s after schedule failure: %v",
			e.sessionKey, scheduleErr,
		)
	}
}

func (e *TransferRuntime) Producers(
	sessionKey string,
	generation uint64,
	transfer *Transfer,
) []raknet.ScheduledPacketProducer {
	producers := make([]raknet.ScheduledPacketProducer, 0, len(zoneteleport.Deadline))
	for _, deadline := range zoneteleport.Deadline {
		step := transferStep{
			runtime: e, sessionKey: sessionKey, generation: generation,
			transfer: transfer, deadline: deadline,
		}
		producers = append(producers, raknet.ScheduledPacketProducer{
			Delay: deadline, Produce: step.produce,
		})
	}
	return producers
}

func (e *TransferRuntime) Failure(
	sessionKey string,
	generation uint64,
	transfer *Transfer,
) TransferFailure {
	return TransferFailure{
		runtime: e, sessionKey: sessionKey,
		generation: generation, transfer: transfer,
	}
}
