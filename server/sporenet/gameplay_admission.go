package sporenet

import (
	"context"
	"errors"
	"log"

	"github.com/darkspinnet/darkspin/server/admission"
)

// SetGameplayAdmissionGate is independent of the account ban/credential policy:
// panel credential verification remains available during maintenance.
func (e *UserManager) SetGameplayAdmissionGate(gate *admission.Gate) {
	e.mu.Lock()
	e.gameplayAdmissionGate = gate
	e.mu.Unlock()
}

func (e *UserManager) enterGameplayAdmission(ctx context.Context) (*admission.Lease, LoginResult) {
	e.mu.RLock()
	gate := e.gameplayAdmissionGate
	e.mu.RUnlock()
	lease, err := gate.Enter(ctx)
	if err == nil {
		return lease, LoginResult{}
	}
	var closed *admission.ClosedError
	if errors.As(err, &closed) {
		return nil, LoginResult{
			IsAdmissionClosed: true, AdmissionReason: closed.State.Reason, AdmissionDeadline: closed.State.Deadline,
		}
	}
	return nil, LoginResult{}
}

func closeGameplayAdmission(lease *admission.Lease) {
	err := lease.Close()
	if err != nil {
		log.Printf("Gameplay admission lease close failed: %v", err)
	}
}
