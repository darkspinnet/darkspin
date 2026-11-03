package blaze

import (
	"context"
	"errors"
	"fmt"

	"github.com/darkspinnet/darkspin/server/admission"
	"github.com/darkspinnet/darkspin/server/blaze/tdf"
	"github.com/darkspinnet/darkspin/server/databuffer"
	"github.com/darkspinnet/darkspin/server/sporenet"
)

func (e *Registry) UseAdmissionGate(gate *admission.Gate) {
	e.mu.Lock()
	e.admissionGate = gate
	e.mu.Unlock()
}

func (e *Registry) enterGameplayAdmission(ctx context.Context) (*admission.Lease, error) {
	e.mu.RLock()
	gate := e.admissionGate
	e.mu.RUnlock()
	lease, err := gate.Enter(ctx)
	if err != nil {
		return nil, fmt.Errorf("gameplayEntry: %w", err)
	}
	return lease, nil
}

func isGameAdmissionFrame(frame Frame) bool {
	if frame.Component != GameManagerComponentID {
		return false
	}
	switch frame.Command {
	case 0x01, 0x09, 0x0d, 0x0f, 0x19:
		return true
	default:
		return false
	}
}

func closeBlazeAdmission(lease *admission.Lease, session *Session) {
	err := lease.Close()
	if err != nil && session != nil && session.server != nil {
		session.server.logger.Printf("Blaze admission lease close failed session=%d: %v", session.ID, err)
	}
}

func admissionLoginFailure(state admission.State) *Response {
	response := loginFailureResponse("server-maintenance", state.Reason)
	if !state.Deadline.IsZero() {
		response.Fields = append(response.Fields, tdf.FieldNamed("DUTC", tdf.IntegerValue(uint64(state.Deadline.Unix()))))
	}
	return response
}

func admissionFrameFailure(reply Frame, err error) dispatchResult {
	reply.Type = ErrorReply
	reply.ErrorCode = ErrorSystem
	var closed *admission.ClosedError
	if !errors.As(err, &closed) {
		return dispatchResult{reply: reply}
	}
	response := admissionLoginFailure(closed.State)
	payload := databuffer.New()
	encodeErr := tdf.Encode(payload, response.Fields)
	if encodeErr != nil {
		// Preserve the numeric rejection if its optional explanation cannot encode.
		return dispatchResult{reply: reply}
	}
	reply.Payload = payload.Bytes()
	return dispatchResult{reply: reply}
}

// Already-admitted games can reconnect during a drain. Lobby-only token/express
// paths must hold the same read lease through binding their TCP session.
func enterBlazeLoginAdmission(ctx context.Context, request *Request, user *sporenet.User) (*admission.Lease, *Response) {
	if user == nil || user.CurrentGameID() != 0 || request == nil || request.Session == nil || request.Session.server == nil {
		return &admission.Lease{}, nil
	}
	lease, err := request.Session.server.registry.enterGameplayAdmission(ctx)
	if err == nil {
		return lease, nil
	}
	var closed *admission.ClosedError
	if errors.As(err, &closed) {
		return nil, admissionLoginFailure(closed.State)
	}
	return nil, &Response{ErrorCode: authInvalidUser}
}
